package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
)

// newFakeSourceServer serves /ok (a normal article page) and /dead (404),
// standing in for the external press sites release_submit's source
// checker visits.
func newFakeSourceServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>A real article about the story.</body></html>"))
		case "/dead":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newFakeWaybackServer stands in for the Wayback Machine's Save Page Now
// endpoint. Archiving fails (500) for any target URL containing "/dead",
// so tests can exercise the "broken link with no archive copy" rejection.
func newFakeWaybackServer(t *testing.T) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/save/"):
			target := strings.TrimPrefix(r.URL.Path, "/save/")
			if strings.Contains(target, "/dead") {
				http.Error(w, "archiving failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Location", "/web/20261024000000/"+target)
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/web/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// uploadFixture gets a presigned URL via media_upload_url and PUTs a small
// fixed body to it, returning the resulting object key.
func uploadFixture(t *testing.T, svc *service.Service, developmentID, filename string) string {
	t.Helper()
	ctx := context.Background()

	resp, err := svc.MediaUploadURL(ctx, service.MediaUploadURLRequest{
		DevelopmentID: developmentID,
		Filename:      filename,
		ContentType:   "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("MediaUploadURL: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, resp.UploadURL, bytes.NewReader([]byte("fixture content")))
	if err != nil {
		t.Fatalf("building PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT upload: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode/100 != 2 {
		t.Fatalf("PUT upload status %d: %s", httpResp.StatusCode, body)
	}
	return resp.ObjectKey
}

func registerDevelopment(t *testing.T, svc *service.Service, channelID, title string) string {
	t.Helper()
	resp, err := svc.StoryRegister(context.Background(), service.StoryRegisterRequest{
		ChannelID:          channelID,
		Title:              title,
		Summary:            "A summary that is long enough to pass the story_register validation.",
		DevelopmentSummary: "A development summary that is long enough to pass validation checks.",
		ReportedAt:         time.Now().UTC(),
		SeedURLs:           []string{"https://example.com/seed"},
	})
	if err != nil {
		t.Fatalf("StoryRegister: %v", err)
	}
	return resp.DevelopmentID
}

func dossierAndScript(sourceURL string) (json.RawMessage, json.RawMessage) {
	dossier := fmt.Sprintf(`{
		"facts": [
			{"id": "F1", "primary_source": {"url": %q, "outlet": "Example Wire", "published_at": "2026-10-24T00:00:00Z"}, "sources": []}
		]
	}`, sourceURL)
	script := `{"scenes": [{"text": "hook", "fact_ids": ["F1"]}]}`
	return json.RawMessage(dossier), json.RawMessage(script)
}

const passValidationReport = `{"verdict": "pass"}`
const passLegalReview = `{"mode": "final", "verdict": "pass", "platforms": {"youtube": {"verdict": "pass"}, "tiktok": {"verdict": "exclude"}}}`

func TestReleaseSubmit_HappyPath(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	sourceSrv := newFakeSourceServer(t)
	waybackSrv := newFakeWaybackServer(t)
	checker := sources.NewChecker(5 * time.Second)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second)

	svc := service.New(st, media, checker, wayback, slog.Default())
	devID := registerDevelopment(t, svc, "test-channel", "Happy path story")

	video := uploadFixture(t, svc, devID, "video.mp4")
	subtitles := uploadFixture(t, svc, devID, "subtitles.srt")
	cover := uploadFixture(t, svc, devID, "cover.png")

	dossier, script := dossierAndScript(sourceSrv.URL + "/ok")

	resp, err := svc.ReleaseSubmit(context.Background(), service.ReleaseManifest{
		DevelopmentID:    devID,
		ChannelID:        "test-channel",
		Media:            service.MediaManifest{Video: video, Subtitles: subtitles, Cover: cover},
		ResearchDossier:  dossier,
		Script:           script,
		VideoPackage:     json.RawMessage(`{}`),
		ValidationReport: json.RawMessage(passValidationReport),
		LegalReview:      json.RawMessage(passLegalReview),
	})
	if err != nil {
		t.Fatalf("ReleaseSubmit: %v", err)
	}
	if resp.Status != "submitted" {
		t.Errorf("Status = %q, want submitted", resp.Status)
	}
	if len(resp.AllowedPlatforms) != 1 || resp.AllowedPlatforms[0] != "youtube" {
		t.Errorf("AllowedPlatforms = %v, want [youtube]", resp.AllowedPlatforms)
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(resp.Sources))
	}
	src := resp.Sources[0]
	if src.CheckStatus != "ok" {
		t.Errorf("CheckStatus = %q, want ok", src.CheckStatus)
	}
	if src.ArchiveURL == "" {
		t.Errorf("ArchiveURL is empty, want an archived snapshot URL")
	}

	// release_get must show the same release with its sources.
	got, err := svc.ReleaseGet(context.Background(), service.ReleaseGetRequest{ReleaseID: resp.ReleaseID})
	if err != nil {
		t.Fatalf("ReleaseGet: %v", err)
	}
	if got.Status != "submitted" || len(got.Sources) != 1 {
		t.Fatalf("ReleaseGet = %+v", got)
	}

	// A second submission for the same development must be refused while
	// this release is still active (not rejected/changes_requested/failed).
	_, err = svc.ReleaseSubmit(context.Background(), service.ReleaseManifest{
		DevelopmentID:    devID,
		ChannelID:        "test-channel",
		Media:            service.MediaManifest{Video: video, Subtitles: subtitles, Cover: cover},
		ResearchDossier:  dossier,
		Script:           script,
		VideoPackage:     json.RawMessage(`{}`),
		ValidationReport: json.RawMessage(passValidationReport),
		LegalReview:      json.RawMessage(passLegalReview),
	})
	if err == nil {
		t.Fatalf("expected a second submission for the same development to be rejected")
	}

	// releases_recent must show it.
	recent, err := svc.ReleasesRecent(context.Background(), service.ReleasesRecentRequest{ChannelID: "test-channel"})
	if err != nil {
		t.Fatalf("ReleasesRecent: %v", err)
	}
	if len(recent.Releases) != 1 || recent.Releases[0].ReleaseID != resp.ReleaseID {
		t.Fatalf("ReleasesRecent = %+v", recent)
	}
}

func TestReleaseSubmit_DeadSourceWithoutArchive(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	sourceSrv := newFakeSourceServer(t)
	waybackSrv := newFakeWaybackServer(t)
	checker := sources.NewChecker(5 * time.Second)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second)

	svc := service.New(st, media, checker, wayback, slog.Default())
	devID := registerDevelopment(t, svc, "test-channel", "Dead source story")

	video := uploadFixture(t, svc, devID, "video.mp4")
	subtitles := uploadFixture(t, svc, devID, "subtitles.srt")
	cover := uploadFixture(t, svc, devID, "cover.png")

	deadURL := sourceSrv.URL + "/dead"
	dossier, script := dossierAndScript(deadURL)

	_, err := svc.ReleaseSubmit(context.Background(), service.ReleaseManifest{
		DevelopmentID:    devID,
		ChannelID:        "test-channel",
		Media:            service.MediaManifest{Video: video, Subtitles: subtitles, Cover: cover},
		ResearchDossier:  dossier,
		Script:           script,
		VideoPackage:     json.RawMessage(`{}`),
		ValidationReport: json.RawMessage(passValidationReport),
		LegalReview:      json.RawMessage(passLegalReview),
	})
	if err == nil {
		t.Fatalf("expected ReleaseSubmit to reject a dead source with no archive copy")
	}
	if !strings.Contains(err.Error(), deadURL) {
		t.Fatalf("error %q does not mention the broken URL %q", err.Error(), deadURL)
	}
}

func TestReleaseSubmit_MissingObjectKey(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	sourceSrv := newFakeSourceServer(t)
	waybackSrv := newFakeWaybackServer(t)
	checker := sources.NewChecker(5 * time.Second)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second)

	svc := service.New(st, media, checker, wayback, slog.Default())
	devID := registerDevelopment(t, svc, "test-channel", "Missing object story")

	subtitles := uploadFixture(t, svc, devID, "subtitles.srt")
	cover := uploadFixture(t, svc, devID, "cover.png")
	missingKey := "releases/" + devID + "/does-not-exist.mp4"

	dossier, script := dossierAndScript(sourceSrv.URL + "/ok")

	_, err := svc.ReleaseSubmit(context.Background(), service.ReleaseManifest{
		DevelopmentID:    devID,
		ChannelID:        "test-channel",
		Media:            service.MediaManifest{Video: missingKey, Subtitles: subtitles, Cover: cover},
		ResearchDossier:  dossier,
		Script:           script,
		VideoPackage:     json.RawMessage(`{}`),
		ValidationReport: json.RawMessage(passValidationReport),
		LegalReview:      json.RawMessage(passLegalReview),
	})
	if err == nil {
		t.Fatalf("expected ReleaseSubmit to reject a missing S3 object key")
	}
	if !strings.Contains(err.Error(), missingKey) {
		t.Fatalf("error %q does not mention the missing key %q", err.Error(), missingKey)
	}
}

func TestReleaseSubmit_ValidationFail(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	sourceSrv := newFakeSourceServer(t)
	waybackSrv := newFakeWaybackServer(t)
	checker := sources.NewChecker(5 * time.Second)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second)

	svc := service.New(st, media, checker, wayback, slog.Default())
	devID := registerDevelopment(t, svc, "test-channel", "Validation fail story")

	video := uploadFixture(t, svc, devID, "video.mp4")
	subtitles := uploadFixture(t, svc, devID, "subtitles.srt")
	cover := uploadFixture(t, svc, devID, "cover.png")

	dossier, script := dossierAndScript(sourceSrv.URL + "/ok")

	_, err := svc.ReleaseSubmit(context.Background(), service.ReleaseManifest{
		DevelopmentID:    devID,
		ChannelID:        "test-channel",
		Media:            service.MediaManifest{Video: video, Subtitles: subtitles, Cover: cover},
		ResearchDossier:  dossier,
		Script:           script,
		VideoPackage:     json.RawMessage(`{}`),
		ValidationReport: json.RawMessage(`{"verdict": "fail"}`),
		LegalReview:      json.RawMessage(passLegalReview),
	})
	if err == nil {
		t.Fatalf("expected ReleaseSubmit to reject validation_report.verdict = fail")
	}
}

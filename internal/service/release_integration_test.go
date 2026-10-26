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

// newFakeAvailabilityServer stands in for the Wayback Availability JSON
// API (https://archive.org/wayback/available). It reports no existing
// snapshot for every URL; these tests' "dead" sources need exactly that
// (no pre-existing archive copy, so the policy decision in
// countries/us/policy.md "Источники в ролике" rejects them), and their
// "ok" sources never consult it at all.
func newFakeAvailabilityServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"` + target + `","archived_snapshots":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitForArchiveURL polls release_get until every "ok" source of releaseID
// has a non-empty archive_url, or fails the test after a timeout. "ok"
// sources are archived by a background job after release_submit returns
// (task E1 revision point 4), so asserting on their archive_url requires
// waiting for that job rather than checking immediately.
func waitForArchiveURL(t *testing.T, svc *service.Service, releaseID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := svc.ReleaseGet(context.Background(), service.ReleaseGetRequest{ReleaseID: releaseID})
		if err != nil {
			t.Fatalf("ReleaseGet: %v", err)
		}
		allArchived := true
		for _, src := range got.Sources {
			if src.CheckStatus == "ok" && src.ArchiveURL == "" {
				allArchived = false
			}
		}
		if allArchived {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background archiving of release %s did not complete in time: %+v", releaseID, got.Sources)
		}
		time.Sleep(50 * time.Millisecond)
	}
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
	availabilitySrv := newFakeAvailabilityServer(t)
	checker := sources.NewChecker(5*time.Second, true)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second, "", "", true)
	availability := sources.NewAvailabilityClient(availabilitySrv.URL, 5*time.Second, true)

	svc := service.New(service.Deps{Store: st, Media: media, Checker: checker, Wayback: wayback, Availability: availability, Log: slog.Default()})
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
	// An "ok" source's archive copy is made in the background after
	// release_submit returns (task E1 revision point 4), so it is not
	// required to be set yet here.
	if src.ArchiveURL != "" {
		t.Errorf("ArchiveURL = %q, want empty immediately after submit (archiving is async)", src.ArchiveURL)
	}

	// release_get must show the same release with its sources, and the
	// background job must fill in the archive URL shortly after.
	got, err := svc.ReleaseGet(context.Background(), service.ReleaseGetRequest{ReleaseID: resp.ReleaseID})
	if err != nil {
		t.Fatalf("ReleaseGet: %v", err)
	}
	if got.Status != "submitted" || len(got.Sources) != 1 {
		t.Fatalf("ReleaseGet = %+v", got)
	}

	waitForArchiveURL(t, svc, resp.ReleaseID)

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
	availabilitySrv := newFakeAvailabilityServer(t)
	checker := sources.NewChecker(5*time.Second, true)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second, "", "", true)
	availability := sources.NewAvailabilityClient(availabilitySrv.URL, 5*time.Second, true)

	svc := service.New(service.Deps{Store: st, Media: media, Checker: checker, Wayback: wayback, Availability: availability, Log: slog.Default()})
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
	availabilitySrv := newFakeAvailabilityServer(t)
	checker := sources.NewChecker(5*time.Second, true)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second, "", "", true)
	availability := sources.NewAvailabilityClient(availabilitySrv.URL, 5*time.Second, true)

	svc := service.New(service.Deps{Store: st, Media: media, Checker: checker, Wayback: wayback, Availability: availability, Log: slog.Default()})
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
	availabilitySrv := newFakeAvailabilityServer(t)
	checker := sources.NewChecker(5*time.Second, true)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second, "", "", true)
	availability := sources.NewAvailabilityClient(availabilitySrv.URL, 5*time.Second, true)

	svc := service.New(service.Deps{Store: st, Media: media, Checker: checker, Wayback: wayback, Availability: availability, Log: slog.Default()})
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

// newSlowBotProtected403Server serves 403 on every path, so every source
// candidate's Checker.Check returns bot_protected and release_submit must
// synchronously call Save Page Now to decide pass/fail (task E1 revision
// point 2).
func newSlowBotProtected403Server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newSlowWaybackServer stands in for Save Page Now, but sleeps delay before
// answering every /save/ request successfully, to exercise release_submit's
// concurrency limit (task E1 revision point 4: 40 sources at 5s/call must
// still finish well under 30s with bounded parallelism).
func newSlowWaybackServer(t *testing.T, delay time.Duration) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/save/"):
			time.Sleep(delay)
			target := strings.TrimPrefix(r.URL.Path, "/save/")
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

// dossierWithNSources builds a research_dossier referencing n distinct
// source URLs on baseURL, and a script that references no fact_ids
// (ExtractCandidates then uses every source in the dossier).
func dossierWithNSources(baseURL string, n int) (json.RawMessage, json.RawMessage) {
	var facts []string
	for i := 0; i < n; i++ {
		facts = append(facts, fmt.Sprintf(
			`{"id": "F%d", "primary_source": {"url": %q, "outlet": "Example Wire", "published_at": "2026-10-24T00:00:00Z"}, "sources": []}`,
			i, fmt.Sprintf("%s/blocked/%d", baseURL, i),
		))
	}
	dossier := fmt.Sprintf(`{"facts": [%s]}`, strings.Join(facts, ","))
	script := `{"scenes": [{"text": "hook"}]}`
	return json.RawMessage(dossier), json.RawMessage(script)
}

// TestReleaseSubmit_ManySourcesFinishWithinDeadline covers revision
// request point 4: checking/archiving 40 sources that each need a
// synchronous, 5-second Save Page Now call to resolve must still complete
// release_submit well under 30 seconds, because they run with bounded
// concurrency rather than one at a time (40 sources / 8 at a time * 5s ≈
// 25s).
func TestReleaseSubmit_ManySourcesFinishWithinDeadline(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	sourceSrv := newSlowBotProtected403Server(t)
	waybackSrv := newSlowWaybackServer(t, 5*time.Second)
	availabilitySrv := newFakeAvailabilityServer(t)
	checker := sources.NewChecker(10*time.Second, true)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 10*time.Second, "", "", true)
	availability := sources.NewAvailabilityClient(availabilitySrv.URL, 10*time.Second, true)

	svc := service.New(service.Deps{
		Store: st, Media: media, Checker: checker, Wayback: wayback, Availability: availability, Log: slog.Default(),
		SourcesCheckDeadline: 29 * time.Second,
	})
	devID := registerDevelopment(t, svc, "test-channel", "Many sources story")

	video := uploadFixture(t, svc, devID, "video.mp4")
	subtitles := uploadFixture(t, svc, devID, "subtitles.srt")
	cover := uploadFixture(t, svc, devID, "cover.png")

	dossier, script := dossierWithNSources(sourceSrv.URL, 40)

	start := time.Now()
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
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ReleaseSubmit: %v (after %s)", err, elapsed)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("ReleaseSubmit took %s, want under 30s", elapsed)
	}
	if len(resp.Sources) != 40 {
		t.Fatalf("len(Sources) = %d, want 40", len(resp.Sources))
	}
	for _, src := range resp.Sources {
		if src.CheckStatus != "bot_protected" {
			t.Errorf("source %s CheckStatus = %q, want bot_protected", src.URL, src.CheckStatus)
		}
		if src.ArchiveURL == "" {
			t.Errorf("source %s ArchiveURL is empty, want the synchronous Save Page Now result", src.URL)
		}
	}
}

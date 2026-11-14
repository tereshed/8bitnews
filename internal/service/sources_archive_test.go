package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
)

// saveRecorder is newFakeWaybackServer that also records every URL sent to
// Save Page Now and, while gate is open, holds each save until it is closed.
type saveRecorder struct {
	srv  *httptest.Server
	mu   sync.Mutex
	urls []string
}

func newSaveRecorder(t *testing.T, gate <-chan struct{}) *saveRecorder {
	fake := newFakeWaybackServer(t)
	r := &saveRecorder{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if target, ok := strings.CutPrefix(req.URL.Path, "/save/"); ok {
			r.mu.Lock()
			r.urls = append(r.urls, target)
			r.mu.Unlock()
			if gate != nil {
				<-gate
			}
		}
		fake.Config.Handler.ServeHTTP(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *saveRecorder) saved() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := slices.Clone(r.urls)
	slices.Sort(out)
	return out
}

// newArchiveService builds a Service for SourcesArchive alone: no store and
// no media, since it needs neither, and a clock the test moves by hand.
func newArchiveService(t *testing.T, waybackURL string) (*service.Service, *atomic.Int64) {
	t.Helper()
	svc := service.New(service.Deps{
		Checker: sources.NewChecker(5*time.Second, true),
		Wayback: sources.NewWaybackClient(waybackURL, 5*time.Second, "", "", true),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	var clock atomic.Int64
	clock.Store(time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC).UnixNano())
	svc.SetSourcesArchiveClock(func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	return svc, &clock
}

// closedServerURL is the address of a server that is gone: connecting to it is
// refused, so the checker says unreachable.
func closedServerURL(t *testing.T) string {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

func archive(t *testing.T, svc *service.Service, dossier string) service.SourcesArchiveResponse {
	t.Helper()
	resp, err := svc.SourcesArchive(service.SourcesArchiveRequest{ResearchDossier: json.RawMessage(dossier)})
	if err != nil {
		t.Fatalf("SourcesArchive: %v", err)
	}
	return resp
}

// An approved dossier's sources are saved hours before release_submit checks
// them, from every fact, local group and world cluster, since no script says
// yet which ones the video will cite. Only the statuses resolveSource would
// save are saved: a dead page would only archive its 404, and a blocked URL is
// one hub refused to dial. A save that failed is tried again on the next call;
// one that worked is not, for 12 hours.
func TestSourcesArchive_SavesWhatReleaseSubmitWouldAndRemembersIt(t *testing.T) {
	sourceSrv := newFakeSourceServer(t)
	walled := newSlowBotProtected403Server(t)
	down := closedServerURL(t)
	wayback := newSaveRecorder(t, nil)
	svc, clock := newArchiveService(t, wayback.srv.URL)

	ok := sourceSrv.URL + "/ok"
	gone := sourceSrv.URL + "/dead"
	botWall := walled.URL + "/article"
	unreachable := down + "/article"
	unreachableSaveFails := down + "/dead-end" // the fake archive fails every URL with "/dead" in it
	blocked := "ftp://example.com/report.pdf"
	dossier := fmt.Sprintf(`{
		"facts": [
			{"id": "F1", "primary_source": {"url": %q}, "sources": [{"url": %q}]},
			{"id": "F2", "sources": [{"url": %q}]}
		],
		"local_groups": [{"id": "L1", "representatives": [{"url": %q}]}],
		"world_clusters": [{"id": "W1", "representatives": [{"url": %q}, {"url": %q}]}]
	}`, ok, unreachable, botWall, gone, blocked, unreachableSaveFails)

	if got := archive(t, svc, dossier); got != (service.SourcesArchiveResponse{Queued: 6}) {
		t.Fatalf("first call = %+v, want 6 queued", got)
	}
	svc.WaitSourcesArchive()
	want := []string{botWall, ok, unreachable, unreachableSaveFails}
	slices.Sort(want)
	if got := wayback.saved(); !slices.Equal(got, want) {
		t.Fatalf("sent to Save Page Now: %q, want %q", got, want)
	}

	// Saved ones are left out; the dead, the blocked and the failed save go
	// through the check again.
	if got := archive(t, svc, dossier); got != (service.SourcesArchiveResponse{Queued: 3, SkippedRecent: 3}) {
		t.Fatalf("second call = %+v, want 3 queued, 3 skipped", got)
	}
	svc.WaitSourcesArchive()
	if got := len(wayback.saved()); got != 5 {
		t.Fatalf("Save Page Now calls after the second call = %d, want 5 (only the failed save again)", got)
	}

	clock.Add(int64(12*time.Hour + time.Minute))
	if got := archive(t, svc, dossier); got != (service.SourcesArchiveResponse{Queued: 6}) {
		t.Fatalf("call 12 h later = %+v, want all 6 queued again", got)
	}
	svc.WaitSourcesArchive()
}

// A URL still being saved is left out of a second call too, and stays left out
// once saved.
func TestSourcesArchive_SkipsSourcesStillBeingSaved(t *testing.T) {
	sourceSrv := newFakeSourceServer(t)
	gate := make(chan struct{})
	wayback := newSaveRecorder(t, gate)
	openGate := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(openGate) // before the recorder closes, which waits for its held saves
	svc, _ := newArchiveService(t, wayback.srv.URL)
	dossier := fmt.Sprintf(`{"facts": [{"id": "F1", "primary_source": {"url": %q}}]}`, sourceSrv.URL+"/ok")

	if got := archive(t, svc, dossier); got != (service.SourcesArchiveResponse{Queued: 1}) {
		t.Fatalf("first call = %+v, want 1 queued", got)
	}
	if got := archive(t, svc, dossier); got != (service.SourcesArchiveResponse{SkippedRecent: 1}) {
		t.Fatalf("call while saving = %+v, want 1 skipped", got)
	}
	openGate()
	svc.WaitSourcesArchive()
	if got := archive(t, svc, dossier); got != (service.SourcesArchiveResponse{SkippedRecent: 1}) {
		t.Fatalf("call after saving = %+v, want 1 skipped", got)
	}
	if got := len(wayback.saved()); got != 1 {
		t.Fatalf("Save Page Now calls = %d, want 1", got)
	}
}

func TestSourcesArchive_RefusesAMissingOrInvalidDossier(t *testing.T) {
	svc, _ := newArchiveService(t, "http://127.0.0.1:1")
	for _, dossier := range []string{"", "null", " ", `"a dossier"`, `[{"facts": []}]`, `{"facts": "F1"}`} {
		_, err := svc.SourcesArchive(service.SourcesArchiveRequest{ResearchDossier: json.RawMessage(dossier)})
		var svcErr *service.Error
		if !errors.As(err, &svcErr) || svcErr.Kind != service.KindInvalid {
			t.Errorf("dossier %q: error %v, want an invalid-input error", dossier, err)
		}
	}
	if got := archive(t, svc, `{"facts": []}`); got != (service.SourcesArchiveResponse{}) {
		t.Errorf("dossier with no sources = %+v, want nothing queued", got)
	}
}

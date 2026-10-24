package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// newFakeAvailability mimics https://archive.org/wayback/available: /found
// returns a 200-status snapshot, /found-404 returns a snapshot whose
// recorded status is not 200 (the archive only captured the page's own
// error response), and anything else returns an empty result.
func newFakeAvailability(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("url")
		w.Header().Set("Content-Type", "application/json")
		switch target {
		case "https://example.com/found":
			_, _ = w.Write([]byte(`{"url":"https://example.com/found","archived_snapshots":{"closest":{"available":true,"url":"http://web.archive.org/web/20200101000000/https://example.com/found","timestamp":"20200101000000","status":"200"}}}`))
		case "https://example.com/found-404":
			_, _ = w.Write([]byte(`{"url":"https://example.com/found-404","archived_snapshots":{"closest":{"available":true,"url":"http://web.archive.org/web/20200101000000/https://example.com/found-404","timestamp":"20200101000000","status":"404"}}}`))
		default:
			_, _ = w.Write([]byte(`{"url":"` + target + `","archived_snapshots":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAvailabilityClient_Found200(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	archiveURL, found, err := client.Closest(context.Background(), "https://example.com/found")
	if err != nil {
		t.Fatalf("Closest: %v", err)
	}
	if !found {
		t.Fatalf("expected a snapshot to be found")
	}
	if archiveURL == "" {
		t.Fatalf("expected a non-empty archive URL")
	}
}

func TestAvailabilityClient_SnapshotOfErrorPageIsNotFound(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	_, found, err := client.Closest(context.Background(), "https://example.com/found-404")
	if err != nil {
		t.Fatalf("Closest: %v", err)
	}
	if found {
		t.Fatalf("a snapshot whose recorded status is 404 must not count as found")
	}
}

func TestAvailabilityClient_NoSnapshot(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	_, found, err := client.Closest(context.Background(), "https://example.com/never-archived")
	if err != nil {
		t.Fatalf("Closest: %v", err)
	}
	if found {
		t.Fatalf("expected no snapshot to be found")
	}
}

func TestAvailabilityClient_BlocksPrivateBaseURL(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 2*time.Second, false)

	_, _, err := client.Closest(context.Background(), "https://example.com/found")
	if err == nil {
		t.Fatalf("expected Closest against a loopback base URL to fail when allowPrivate=false")
	}
}

func TestAvailabilityClient_EncodesTargetURL(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"archived_snapshots":{}}`))
	}))
	t.Cleanup(srv.Close)

	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)
	target := "https://example.com/a?b=c&d=e"
	if _, _, err := client.Closest(context.Background(), target); err != nil {
		t.Fatalf("Closest: %v", err)
	}
	q, err := url.ParseQuery(gotRawQuery)
	if err != nil {
		t.Fatalf("parsing request query: %v", err)
	}
	if q.Get("url") != target {
		t.Fatalf("server saw url=%q, want %q", q.Get("url"), target)
	}
}

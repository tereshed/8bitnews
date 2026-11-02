package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// newFakeAvailability mimics https://web.archive.org/cdx/search/cdx with
// filter=statuscode:200: /found has two captures with HTTP 200, of which
// limit=-1 returns the last; /found-404 has only a capture of its own error
// response, which the filter removes; anything else has none.
func newFakeAvailability(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/cdx/search/cdx" || q.Get("filter") != "statuscode:200" || q.Get("limit") != "-1" ||
			q.Get("fl") != "timestamp,original" || q.Get("output") != "json" {
			http.Error(w, "unexpected request: "+r.URL.String(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch q.Get("url") {
		case "https://example.com/found":
			_, _ = w.Write([]byte(`[["timestamp","original"],["20200102000000","https://example.com/found"]]`))
		default:
			_, _ = w.Write([]byte(`[]`))
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
	if want := srv.URL + "/web/20200102000000/https://example.com/found"; archiveURL != want {
		t.Fatalf("archive URL = %q, want %q", archiveURL, want)
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
		t.Fatalf("a page with only a capture of its 404 must not count as found")
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
		_, _ = w.Write([]byte(`[]`))
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

func TestAvailabilityClient_EmptyBodyMeansNoSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	if _, found, err := client.Closest(context.Background(), "https://example.com/x"); err != nil || found {
		t.Fatalf("found=%v err=%v, want no snapshot and no error", found, err)
	}
}

func TestAvailabilityClient_ReportsAFailedLookup(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"rate limited": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"not JSON":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>busy</html>`)) },
		"short row":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[["timestamp"],["2020"]]`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			t.Cleanup(srv.Close)
			client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

			if _, found, err := client.Closest(context.Background(), "https://example.com/x"); err == nil || found {
				t.Fatalf("found=%v err=%v, want an error", found, err)
			}
		})
	}
}

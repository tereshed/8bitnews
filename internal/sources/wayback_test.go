package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newFakeWayback mimics the two calls Save Page Now makes in the happy
// path: GET /save/<url> redirects to the archived snapshot, which serves
// 200. URLs containing "archive-fail" make /save/ return a server error, so
// tests can exercise the "archiving failed" path.
func newFakeWayback(t *testing.T) *httptest.Server {
	// A bare handler, not http.ServeMux: ServeMux redirects to a "cleaned"
	// path when it sees a double slash, which the embedded target URL's
	// "https://" always contains, breaking the save/<url> shape.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/save/"):
			target := strings.TrimPrefix(r.URL.Path, "/save/")
			if strings.Contains(target, "archive-fail") {
				http.Error(w, "archiving failed", http.StatusInternalServerError)
				return
			}
			if strings.Contains(target, "archive-pending") {
				// Save Page Now accepted the job but has not produced a
				// snapshot yet: a plain 200 with no redirect to a /web/...
				// path. This must not be mistaken for a successful archive.
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("job queued"))
				return
			}
			// Not http.Redirect: it runs the Location through path.Clean,
			// which collapses the "//" in the embedded "https://" target.
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

func TestWaybackClient_Save(t *testing.T) {
	srv := newFakeWayback(t)
	client := NewWaybackClient(srv.URL, 5*time.Second, "", "", true)

	archiveURL, err := client.Save(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.Contains(archiveURL, "/web/20261024000000/https://example.com/article") {
		t.Fatalf("archiveURL = %q, want it to contain the snapshot path", archiveURL)
	}
}

func TestWaybackClient_SaveFails(t *testing.T) {
	srv := newFakeWayback(t)
	client := NewWaybackClient(srv.URL, 5*time.Second, "", "", true)

	_, err := client.Save(context.Background(), "https://example.com/archive-fail")
	if err == nil {
		t.Fatalf("expected an error when Save Page Now rejects the request")
	}
}

// TestWaybackClient_SavePendingIsNotSuccess covers revision request point
// 3: a response that is not an error but also never redirected to a real
// /web/<timestamp>/ snapshot path (e.g. the job is merely queued) must not
// be reported as a successful archive.
func TestWaybackClient_SavePendingIsNotSuccess(t *testing.T) {
	srv := newFakeWayback(t)
	client := NewWaybackClient(srv.URL, 5*time.Second, "", "", true)

	_, err := client.Save(context.Background(), "https://example.com/archive-pending")
	if err == nil {
		t.Fatalf("expected an error when Save Page Now has not produced a snapshot yet")
	}
}

// TestWaybackClient_SendsAccessKeyAuth covers revision request point 3: SPN
// credentials, when configured, are sent as "Authorization: LOW
// <key>:<secret>".
func TestWaybackClient_SendsAccessKeyAuth(t *testing.T) {
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/save/"):
			gotAuth = r.Header.Get("Authorization")
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

	client := NewWaybackClient(srv.URL, 5*time.Second, "my-access-key", "my-secret-key", true)
	if _, err := client.Save(context.Background(), "https://example.com/article"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if gotAuth != "LOW my-access-key:my-secret-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "LOW my-access-key:my-secret-key")
	}
}

// TestWaybackClient_BlocksPrivateBaseURL covers revision request point 1
// for WaybackClient: with allowPrivate=false it must refuse to connect to
// a loopback base URL, the same as Checker.
func TestWaybackClient_BlocksPrivateBaseURL(t *testing.T) {
	srv := newFakeWayback(t)
	client := NewWaybackClient(srv.URL, 2*time.Second, "", "", false)

	_, err := client.Save(context.Background(), "https://example.com/article")
	if err == nil {
		t.Fatalf("expected Save against a loopback base URL to fail when allowPrivate=false")
	}
}

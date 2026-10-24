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
	client := NewWaybackClient(srv.URL, 5*time.Second)

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
	client := NewWaybackClient(srv.URL, 5*time.Second)

	_, err := client.Save(context.Background(), "https://example.com/archive-fail")
	if err == nil {
		t.Fatalf("expected an error when Save Page Now rejects the request")
	}
}

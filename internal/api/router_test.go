package api_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tereshed/8bitnews/internal/api"
	"github.com/tereshed/8bitnews/internal/observability"
)

func TestTheAdminPageHasItsOwnLogin(t *testing.T) {
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("admin")) })
	router := api.NewRouter(nil, http.NotFoundHandler(), nil, admin, observability.NewMetrics(), slog.Default(), map[string]bool{"k": true}, "", true)
	for path, want := range map[string]int{
		"/admin":                      http.StatusOK,
		"/admin/r/x":                  http.StatusOK,
		"/adminfoo":                   http.StatusUnauthorized,
		"/api/v1/releases/x":          http.StatusUnauthorized,
		"/mcp":                        http.StatusUnauthorized,
		"/metrics":                    http.StatusUnauthorized,
		"/s/f874ebb1a0":               http.StatusUnauthorized, // no pages in this router
		"/admin/../api/v1/releases/x": http.StatusUnauthorized, // the mux cleans the path to the API's
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		got := rec.Code
		if got == http.StatusMovedPermanently || got == http.StatusTemporaryRedirect { // the mux's redirect to the clean path carries no data
			req := httptest.NewRequest(http.MethodGet, rec.Header().Get("Location"), nil)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			got = rec.Code
		}
		if got != want {
			t.Errorf("GET %s: %d, want %d", path, got, want)
		}
	}
}

func TestThePublicPagesNeedNoKey(t *testing.T) {
	pages := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("page " + r.URL.Path)) })
	router := api.NewRouter(nil, http.NotFoundHandler(), pages, nil, observability.NewMetrics(), slog.Default(), map[string]bool{"k": true}, "", true)
	for path, want := range map[string]int{
		"/s/f874ebb1a0":                 http.StatusOK,
		"/c/8bitnews-us":                http.StatusOK,
		"/healthz":                      http.StatusOK,
		"/api/v1/releases/f874ebb1-a01": http.StatusUnauthorized,
		"/mcp":                          http.StatusUnauthorized,
		"/metrics":                      http.StatusUnauthorized,
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/s/f874ebb1a0", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST to a page: %d, want it to fall through to the API's auth", rec.Code)
	}

	// Metrics count routes, not URLs: a page per release would be a series per release.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer k")
	router.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `route="GET /s/"`) || strings.Contains(body, "f874ebb1a0") {
		t.Errorf("metrics:\n%s", body)
	}
}

// The pipeline's agents read web pages and hold the ordinary key: it must not
// open the routes that decide what goes out. The handlers are never reached in
// the refused cases, so a nil service is enough.
func TestThePublishingRoutesTakeOnlyThePublishersKey(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/publishing/queue"},
		{http.MethodPost, "/api/v1/publishing/releases/x/slot"},
		{http.MethodPut, "/api/v1/publishing/releases/x/youtube"},
		{http.MethodDelete, "/api/v1/publishing/releases/x/youtube"},
		{http.MethodPost, "/api/v1/publishing/releases/x/hold"},
		{http.MethodDelete, "/api/v1/publishing/releases/x/hold"},
	}
	for _, publisherKey := range []string{"publisher", ""} {
		keys := map[string]bool{"k": true}
		if publisherKey != "" {
			keys[publisherKey] = true
		}
		router := api.NewRouter(nil, http.NotFoundHandler(), nil, nil, observability.NewMetrics(), slog.Default(), keys, publisherKey, true)
		for _, r := range routes {
			for key, want := range map[string]int{"k": http.StatusForbidden, "": http.StatusUnauthorized, "nope": http.StatusUnauthorized} {
				req := httptest.NewRequest(r.method, r.path, nil)
				if key != "" {
					req.Header.Set("Authorization", "Bearer "+key)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != want {
					t.Errorf("publisher key %q: %s %s with key %q: %d, want %d", publisherKey, r.method, r.path, key, rec.Code, want)
				}
			}
		}
	}
}

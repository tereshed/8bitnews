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

func TestThePublicPagesNeedNoKey(t *testing.T) {
	pages := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("page " + r.URL.Path)) })
	router := api.NewRouter(nil, http.NotFoundHandler(), pages, observability.NewMetrics(), slog.Default(), map[string]bool{"k": true}, true)
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

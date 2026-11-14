package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/api"
	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
)

// POST /api/v1/sources/archive takes the pipeline's key, answers 202 with the
// counts before anything is fetched, and 400 without a dossier. Its only
// source here is one hub refuses to dial, so the background job ends at the
// check without reaching the network.
func TestSourcesArchiveRoute(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.New(service.Deps{
		Checker: sources.NewChecker(time.Second, false),
		Wayback: sources.NewWaybackClient("http://127.0.0.1:1", time.Second, "", "", false),
		Log:     quiet,
	})
	router := api.NewRouter(svc, http.NotFoundHandler(), nil, nil, observability.NewMetrics(), quiet, map[string]bool{"k": true}, "", true)

	for _, tc := range []struct {
		name, key, body string
		status          int
		answer          string
	}{
		{"no key", "", `{"research_dossier": {}}`, http.StatusUnauthorized, ""},
		{"accepted", "k", `{"research_dossier": {"facts": [{"id": "F1", "primary_source": {"url": "ftp://example.com/a"}}]}}`,
			http.StatusAccepted, `{"queued":1,"skipped_recent":0}`},
		{"no dossier", "k", `{}`, http.StatusBadRequest, ""},
		{"not an object", "k", `{"research_dossier": "F1"}`, http.StatusBadRequest, ""},
		{"not JSON", "k", `{"research_dossier": `, http.StatusBadRequest, ""},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sources/archive", strings.NewReader(tc.body))
		if tc.key != "" {
			req.Header.Set("Authorization", "Bearer "+tc.key)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s: %d %s, want %d", tc.name, rec.Code, rec.Body, tc.status)
		}
		if tc.answer != "" && strings.TrimSpace(rec.Body.String()) != tc.answer {
			t.Errorf("%s: body %s, want %s", tc.name, rec.Body, tc.answer)
		}
	}
}

package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/service"
)

// maxRequestBodyBytes caps every request body the hub accepts, REST and
// MCP alike (task E1 revision point 6). release_submit's manifest is the
// largest body in practice (research_dossier can be a sizeable JSON
// document), but there is no legitimate reason for any request to this API
// to exceed a few MB; capping it keeps a misbehaving or malicious client
// from holding the server's memory/IO on an unbounded upload.
const maxRequestBodyBytes = 5 << 20 // 5 MiB

// NewRouter builds the hub's full HTTP surface: REST under /api/v1, the
// MCP endpoint at /mcp, and the unauthenticated /healthz. apiKeys is the
// set of accepted bearer tokens (task E1 §4); empty means no key will ever
// be accepted. When requireMetricsAuth is true, /metrics is mounted here
// behind the same bearer-key auth as every other route; when false, the
// caller is serving /metrics unauthenticated on a separate address instead
// (HUB_METRICS_ADDR), so it is not mounted here at all.
func NewRouter(svc *service.Service, mcpHandler http.Handler, metrics *observability.Metrics, logger *slog.Logger, apiKeys map[string]bool, requireMetricsAuth bool) http.Handler {
	h := &handler{svc: svc, log: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	if requireMetricsAuth {
		mux.Handle("GET /metrics", metrics.Handler())
	}

	mux.HandleFunc("GET /api/v1/channels/{channel_id}", h.handleChannelGet)
	mux.HandleFunc("GET /api/v1/channels/{channel_id}/releases/recent", h.handleReleasesRecent)
	mux.HandleFunc("POST /api/v1/stories/find-similar", h.handleStoryFindSimilar)
	mux.HandleFunc("POST /api/v1/stories/register", h.handleStoryRegister)
	mux.HandleFunc("POST /api/v1/experiments/assign", h.handleExperimentAssign)
	mux.HandleFunc("POST /api/v1/media/upload-url", h.handleMediaUploadURL)
	mux.HandleFunc("POST /api/v1/media/download-url", h.handleMediaDownloadURL)
	mux.HandleFunc("POST /api/v1/releases", h.handleReleaseSubmit)
	mux.HandleFunc("GET /api/v1/releases/{release_id}", h.handleReleaseGet)
	mux.Handle("/mcp", mcpHandler)

	authed := requireAPIKey(apiKeys, mux, "/healthz")
	limited := limitRequestBody(authed)
	return observability.RequestLogging(logger)(metrics.Middleware(limited))
}

// limitRequestBody caps every request body at maxRequestBodyBytes.
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// requireAPIKey enforces `Authorization: Bearer <key>` against apiKeys for
// every request except the exempt paths (task E1 §4: /healthz needs no
// auth).
func requireAPIKey(apiKeys map[string]bool, next http.Handler, exempt ...string) http.Handler {
	exemptPaths := make(map[string]bool, len(exempt))
	for _, p := range exempt {
		exemptPaths[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		const prefix = "Bearer "
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, prefix) {
			writeErrorMessage(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}
		key := strings.TrimPrefix(authHeader, prefix)
		if key == "" || !apiKeys[key] {
			writeErrorMessage(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

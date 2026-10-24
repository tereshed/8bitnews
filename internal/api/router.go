package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/service"
)

// NewRouter builds the hub's full HTTP surface: REST under /api/v1, the
// MCP endpoint at /mcp, and the unauthenticated /healthz and /metrics.
// apiKeys is the set of accepted bearer tokens (task E1 §4); empty means no
// key will ever be accepted.
func NewRouter(svc *service.Service, mcpHandler http.Handler, metrics *observability.Metrics, logger *slog.Logger, apiKeys map[string]bool) http.Handler {
	h := &handler{svc: svc, log: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.Handle("GET /metrics", metrics.Handler())

	mux.HandleFunc("GET /api/v1/channels/{channel_id}", h.handleChannelGet)
	mux.HandleFunc("GET /api/v1/channels/{channel_id}/releases/recent", h.handleReleasesRecent)
	mux.HandleFunc("POST /api/v1/stories/find-similar", h.handleStoryFindSimilar)
	mux.HandleFunc("POST /api/v1/stories/register", h.handleStoryRegister)
	mux.HandleFunc("POST /api/v1/experiments/assign", h.handleExperimentAssign)
	mux.HandleFunc("POST /api/v1/media/upload-url", h.handleMediaUploadURL)
	mux.HandleFunc("POST /api/v1/releases", h.handleReleaseSubmit)
	mux.HandleFunc("GET /api/v1/releases/{release_id}", h.handleReleaseGet)
	mux.Handle("/mcp", mcpHandler)

	authed := requireAPIKey(apiKeys, mux, "/healthz", "/metrics")
	return observability.RequestLogging(logger)(metrics.Middleware(authed))
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

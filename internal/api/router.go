package api

import (
	"crypto/subtle"
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
// MCP endpoint at /mcp, and, with no API key, /healthz, the public pages
// (a video's sources at /s/, a channel's videos at /c/; nil: none) and the
// admin page at /admin, which has its own login (nil: none). apiKeys
// is the set of accepted bearer tokens (task E1 §4); empty means no key
// will ever be accepted. publisherKey alone opens the publishing routes;
// empty leaves them shut. When requireMetricsAuth is true, /metrics is
// mounted here behind the same bearer-key auth as every other route; when
// false, the caller is serving /metrics unauthenticated on a separate
// address instead (HUB_METRICS_ADDR), so it is not mounted here at all.
func NewRouter(svc *service.Service, mcpHandler, pages, admin http.Handler, metrics *observability.Metrics, logger *slog.Logger, apiKeys map[string]bool, publisherKey string, requireMetricsAuth bool) http.Handler {
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
	// The publishing routes take the publisher's key only: the pipeline's agents
	// read web pages and hold the other one.
	publisher := func(next http.HandlerFunc) http.HandlerFunc { return requirePublisher(publisherKey, next) }
	mux.HandleFunc("GET /api/v1/publishing/queue", publisher(h.handlePublishQueue))
	mux.HandleFunc("POST /api/v1/publishing/releases/{release_id}/slot", publisher(h.handleReleaseSlot))
	mux.HandleFunc("PUT /api/v1/publishing/releases/{release_id}/youtube", publisher(h.handleYouTubePublicationSet))
	mux.HandleFunc("DELETE /api/v1/publishing/releases/{release_id}/youtube", publisher(h.handleYouTubePublicationForget))
	mux.HandleFunc("POST /api/v1/publishing/releases/{release_id}/hold", publisher(h.handleReleaseHold))
	mux.HandleFunc("DELETE /api/v1/publishing/releases/{release_id}/hold", publisher(h.handleReleaseHoldLift))
	mux.Handle("/mcp", mcpHandler)

	authed := requireAPIKey(apiKeys, mux, "/healthz")
	if pages != nil || admin != nil {
		public := http.NewServeMux()
		if pages != nil {
			public.Handle("GET /s/", pages)
			public.Handle("GET /c/", pages)
		}
		if admin != nil {
			public.Handle("/admin", admin)
			public.Handle("/admin/", admin)
		}
		public.Handle("/", authed)
		authed = public
	}
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

// requirePublisher lets through only the publisher's key.
func requirePublisher(key string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeErrorMessage(w, http.StatusForbidden, "this route takes the publisher's key")
			return
		}
		next(w, r)
	}
}

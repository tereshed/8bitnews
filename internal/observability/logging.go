// Package observability provides the hub's slog JSON logging (with a
// request id on every HTTP log line) and its Prometheus metrics.
package observability

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// NewLogger returns the hub's structured logger: JSON lines on stdout.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

type requestIDKey struct{}

// RequestIDFromContext returns the request id stored by RequestLogging, or
// "" if there is none (e.g. outside an HTTP request).
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush makes statusRecorder transparent to http.Flusher. Without it, every
// handler wrapped by RequestLogging/Metrics.Middleware would lose
// streaming: net/http's SSE and chunked-streaming helpers flush after each
// write, and that call silently no-ops when the ResponseWriter they were
// given doesn't implement http.Flusher — which is exactly what the MCP
// streamable HTTP transport's standalone SSE stream depends on to make the
// response headers (and each subsequent event) reach the client instead of
// sitting in a buffer until the handler returns.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// RequestLogging assigns every request a request id (propagated via
// context and echoed in the X-Request-Id response header) and logs method,
// path, status and duration as JSON through logger.
func RequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := r.Header.Get("X-Request-Id")
			if reqID == "" {
				reqID = uuid.NewString()
			}
			w.Header().Set("X-Request-Id", reqID)

			ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))
			duration := time.Since(start)

			logger.LogAttrs(ctx, slog.LevelInfo, "http_request",
				slog.String("request_id", reqID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", duration),
			)
		})
	}
}

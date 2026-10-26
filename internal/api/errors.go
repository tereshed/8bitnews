package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/tereshed/8bitnews/internal/service"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErrorMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeServiceError maps a service.Error to the HTTP status its Kind
// implies; anything else is an unexpected failure, logged and hidden from
// the client behind a generic 500.
func writeServiceError(w http.ResponseWriter, log *slog.Logger, err error) {
	var svcErr *service.Error
	if errors.As(err, &svcErr) {
		status := http.StatusInternalServerError
		switch svcErr.Kind {
		case service.KindNotFound:
			status = http.StatusNotFound
		case service.KindInvalid:
			status = http.StatusBadRequest
		case service.KindConflict:
			status = http.StatusConflict
		}
		writeErrorMessage(w, status, svcErr.Message)
		return
	}
	log.Error("unhandled service error", "error", err)
	writeErrorMessage(w, http.StatusInternalServerError, "internal error")
}

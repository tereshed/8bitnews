// Package api implements the hub's REST transport (/api/v1) on top of
// internal/service; internal/mcpapi implements the same operations over
// MCP so the two transports cannot drift apart.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/tereshed/8bitnews/internal/service"
)

type handler struct {
	svc *service.Service
	log *slog.Logger
}

func decodeJSON[T any](r *http.Request) (T, error) {
	var v T
	err := json.NewDecoder(r.Body).Decode(&v)
	return v, err
}

func (h *handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) handleChannelGet(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ChannelGet(r.Context(), service.ChannelGetRequest{
		ChannelID: r.PathValue("channel_id"),
	})
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleStoryFindSimilar(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.StoryFindSimilarRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.StoryFindSimilar(r.Context(), req)
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleStoryRegister(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.StoryRegisterRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.StoryRegister(r.Context(), req)
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleExperimentAssign(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.ExperimentAssignRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.ExperimentAssign(r.Context(), req)
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleMediaUploadURL(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.MediaUploadURLRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.MediaUploadURL(r.Context(), req)
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleReleaseSubmit(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.ReleaseManifest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.ReleaseSubmit(r.Context(), req)
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleReleaseGet(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ReleaseGet(r.Context(), service.ReleaseGetRequest{
		ReleaseID: r.PathValue("release_id"),
	})
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleReleasesRecent(w http.ResponseWriter, r *http.Request) {
	n := 0
	if raw := r.URL.Query().Get("n"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, "invalid n: "+err.Error())
			return
		}
		n = parsed
	}
	resp, err := h.svc.ReleasesRecent(r.Context(), service.ReleasesRecentRequest{
		ChannelID: r.PathValue("channel_id"),
		N:         n,
	})
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

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

func (h *handler) handleMediaDownloadURL(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.MediaDownloadURLRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.MediaDownloadURL(r.Context(), req)
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

// handleSourcesArchive answers 202 at once: the sources are checked and saved
// in the background.
func (h *handler) handleSourcesArchive(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.SourcesArchiveRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	resp, err := h.svc.SourcesArchive(req)
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
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

func (h *handler) handlePublishQueue(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.PublishQueue(r.Context())
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleReleaseSlot(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ReserveReleaseSlot(r.Context(), r.PathValue("release_id"))
	if err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleYouTubePublicationSet(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[service.YouTubePublicationRequest](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := h.svc.SetYouTubePublication(r.Context(), r.PathValue("release_id"), req); err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleYouTubePublicationForget(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ForgetYouTubePublication(r.Context(), r.PathValue("release_id")); err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleReleaseHoldLift(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ReleaseHold(r.Context(), r.PathValue("release_id")); err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleReleaseHold(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[struct {
		Reason string `json:"reason"`
	}](r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := h.svc.HoldRelease(r.Context(), r.PathValue("release_id"), req.Reason); err != nil {
		writeServiceError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

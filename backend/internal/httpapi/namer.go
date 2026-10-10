package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
)

// namerMatch enqueues a NAMER background job. The job parses the filename,
// searches the configured providers, and either applies a confident match or stores
// candidates on the job result for the admin UI to pick up.
func (a *API) namerMatch(w http.ResponseWriter, r *http.Request) {
	mediaFileID, err := pathInt64(r, "mediaFileID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media file id")
		return
	}
	a.enqueueNamerJob(w, r, mediaFileID, "", "")
}

type namerSelectRequest struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"externalId"`
	UUID       string `json:"uuid"` // legacy alias for externalId
}

// namerSelectCandidate finalizes a human-chosen candidate from a prior
// namerMatch's manual_required response.
func (a *API) namerSelectCandidate(w http.ResponseWriter, r *http.Request) {
	mediaFileID, err := pathInt64(r, "mediaFileID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media file id")
		return
	}
	var req namerSelectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.ExternalID == "" {
		req.ExternalID = req.UUID
	}
	if req.Provider == "" || req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "provider and externalId are required")
		return
	}
	a.enqueueNamerJob(w, r, mediaFileID, req.Provider, req.ExternalID)
}

func (a *API) enqueueNamerJob(w http.ResponseWriter, r *http.Request, mediaFileID int64, providerID, externalID string) {
	if a.Namer == nil || !a.Namer.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "no metadata provider is configured")
		return
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}

	var existingID int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'NAMER' AND status IN ('PENDING', 'RUNNING')
		  AND payload->>'mediaFileId' = $1
		LIMIT 1
	`, strconv.FormatInt(mediaFileID, 10)).Scan(&existingID)
	if err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "a match is already running for this file",
			"backgroundJobId": existingID,
		})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check for an in-progress match")
		return
	}

	payload := map[string]any{"mediaFileId": mediaFileID}
	if externalID != "" {
		payload["provider"] = providerID
		payload["externalId"] = externalID
	}
	jobID, err := a.Jobs.Enqueue(r.Context(), "NAMER", payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue namer match")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

func (a *API) getBackgroundJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	var job backgroundJob
	var result []byte
	err = a.DB.QueryRow(r.Context(), `
		SELECT id, kind, status, error, attempts, created_at::text, started_at::text, finished_at::text, result
		FROM background_job WHERE id = $1
	`, id).Scan(&job.ID, &job.Kind, &job.Status, &job.Error, &job.Attempts, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load background job")
		return
	}
	body := map[string]any{
		"id": job.ID, "kind": job.Kind, "status": job.Status, "error": job.Error,
		"attempts": job.Attempts, "createdAt": job.CreatedAt, "startedAt": job.StartedAt, "finishedAt": job.FinishedAt,
	}
	if len(result) > 0 {
		var parsed any
		if err := json.Unmarshal(result, &parsed); err == nil {
			body["result"] = parsed
		}
	}
	writeJSON(w, http.StatusOK, body)
}

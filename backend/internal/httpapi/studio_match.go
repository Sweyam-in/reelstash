package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"

	"reelstash/backend/internal/provider"
)

func (a *API) searchStudioProviders(w http.ResponseWriter, r *http.Request) {
	if !a.providersReady(w, provider.KindStudio) {
		return
	}
	if _, err := pathInt64(r, "id"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	items, err := a.searchProviders(r.Context(), provider.KindStudio, splitCSV(r.URL.Query().Get("providers")), query, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search failed")
		return
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) matchStudioProvider(w http.ResponseWriter, r *http.Request) {
	if !a.providersReady(w, provider.KindStudio) {
		return
	}
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	var req matchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	providerID, externalID, ok := req.resolve(w, a, provider.KindStudio)
	if !ok {
		return
	}
	var existing int64
	err = a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'STUDIO_MATCH' AND status IN ('PENDING', 'RUNNING')
		  AND payload->>'studioId' = $1
		ORDER BY id DESC LIMIT 1
	`, strconv.FormatInt(id, 10)).Scan(&existing)
	if err == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": existing, "alreadyRunning": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check for an in-progress match")
		return
	}
	jobID, err := a.Jobs.Enqueue(r.Context(), "STUDIO_MATCH", map[string]any{
		"studioId":   id,
		"provider":   providerID,
		"externalId": externalID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue studio match")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

func (a *API) getStudioMatchJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "jobId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	var kind, status string
	var jobError *string
	var result []byte
	err = a.DB.QueryRow(r.Context(), `
		SELECT kind, status, error, result FROM background_job WHERE id = $1
	`, id).Scan(&kind, &status, &jobError, &result)
	if errors.Is(err, pgx.ErrNoRows) || kind != "STUDIO_MATCH" {
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	body := map[string]any{"id": id, "status": status, "error": jobError}
	if len(result) > 0 {
		var parsed any
		if json.Unmarshal(result, &parsed) == nil {
			body["result"] = parsed
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (a *API) startStudioSync(w http.ResponseWriter, r *http.Request) {
	if !a.providersReady(w, provider.KindStudio) {
		return
	}
	var existing int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'STUDIO_SYNC' AND status IN ('PENDING', 'RUNNING')
		ORDER BY id DESC LIMIT 1
	`).Scan(&existing)
	if err == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": existing, "alreadyRunning": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check for an in-progress sync")
		return
	}
	jobID, err := a.Jobs.Enqueue(r.Context(), "STUDIO_SYNC", map[string]any{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue studio sync")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

func (a *API) studioSyncStatus(w http.ResponseWriter, r *http.Request) {
	var id int64
	var status string
	var jobError *string
	var result []byte
	err := a.DB.QueryRow(r.Context(), `
		SELECT id, status, error, result FROM background_job
		WHERE kind = 'STUDIO_SYNC'
		ORDER BY id DESC LIMIT 1
	`).Scan(&id, &status, &jobError, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load studio sync")
		return
	}
	job := map[string]any{"id": id, "status": status, "error": jobError}
	if len(result) > 0 {
		var parsed any
		if json.Unmarshal(result, &parsed) == nil {
			job["result"] = parsed
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func studioHitJSON(hit provider.StudioResult) map[string]any {
	item := map[string]any{"provider": hit.Ref.Provider, "id": hit.Ref.ExternalID, "name": hit.Name}
	if hit.Network != "" {
		item["disambiguation"] = hit.Network
	}
	if hit.ImageURL != "" {
		item["image"] = hit.ImageURL
	}
	return item
}

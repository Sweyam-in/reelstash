package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"reelstash/backend/internal/provider"
)

var externalIDPattern = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,80}$`)

// providersReady reports whether a provider can match kind and jobs can run.
func (a *API) providersReady(w http.ResponseWriter, kind provider.Kind) bool {
	if a.Namer == nil || !a.Namer.Providers.AnyEnabled(kind) {
		writeError(w, http.StatusServiceUnavailable, "no metadata provider is configured for this (add an API key under Metadata → Providers)")
		return false
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner is not available")
		return false
	}
	return true
}

// searchProviders fans a name search out to the chosen providers (all enabled
// ones for kind when ids is empty) and returns hits tagged with their provider.
// It fails only when every provider failed.
func (a *API) searchProviders(ctx context.Context, kind provider.Kind, ids []string, query string,
	rejected map[string]bool) ([]map[string]any, error) {
	var providers []provider.Provider
	if len(ids) == 0 {
		providers = a.Namer.Providers.Enabled(kind)
	} else {
		for _, id := range ids {
			if p, ok := a.Namer.Providers.Get(id); ok && p.Enabled() && p.Capabilities().Supports(kind) {
				providers = append(providers, p)
			}
		}
	}
	if len(providers) == 0 {
		return nil, errors.New("no matching provider is configured")
	}
	type outcome struct {
		items []map[string]any
		err   error
	}
	results := make([]outcome, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func(i int, p provider.Provider) {
			defer wg.Done()
			var items []map[string]any
			var err error
			switch kind {
			case provider.KindPerformer:
				var hits []provider.PerformerResult
				if hits, err = p.SearchPerformers(ctx, query); err == nil {
					for _, h := range hits {
						items = append(items, performerHitJSON(h))
					}
				}
			case provider.KindStudio:
				var hits []provider.StudioResult
				if hits, err = p.SearchStudios(ctx, query); err == nil {
					for _, h := range hits {
						items = append(items, studioHitJSON(h))
					}
				}
			}
			results[i] = outcome{items, err}
		}(i, p)
	}
	wg.Wait()

	var all []map[string]any
	var firstErr error
	failed := 0
	for _, r := range results {
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		perProvider := 0
		for _, item := range r.items {
			if item["id"] == "" {
				continue
			}
			if rejected[item["provider"].(string)+"\x00"+item["id"].(string)] {
				item["rejected"] = true
			}
			all = append(all, item)
			if perProvider++; perProvider == 8 {
				break
			}
		}
	}
	if failed == len(providers) && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func (a *API) searchPerformerProviders(w http.ResponseWriter, r *http.Request) {
	if !a.providersReady(w, provider.KindPerformer) {
		return
	}
	performerID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	items, err := a.searchProviders(r.Context(), provider.KindPerformer, splitCSV(r.URL.Query().Get("providers")), query,
		a.Namer.RejectedPerformers(r.Context(), performerID))
	if err != nil {
		writeError(w, http.StatusBadGateway, "search failed")
		return
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// matchRequest names one provider record.
type matchRequest struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"externalId"`
}

// resolve returns the provider and record id, validated, or writes a 400.
func (m matchRequest) resolve(w http.ResponseWriter, a *API, kind provider.Kind) (string, string, bool) {
	p, ok := a.Namer.Providers.Get(m.Provider)
	if !ok || !p.Enabled() || !p.Capabilities().Supports(kind) {
		writeError(w, http.StatusBadRequest, "unknown or unconfigured provider")
		return "", "", false
	}
	if !externalIDPattern.MatchString(m.ExternalID) || strings.Contains(m.ExternalID, "..") {
		writeError(w, http.StatusBadRequest, "externalId is required")
		return "", "", false
	}
	return m.Provider, m.ExternalID, true
}

func (a *API) matchPerformerProvider(w http.ResponseWriter, r *http.Request) {
	if !a.providersReady(w, provider.KindPerformer) {
		return
	}
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	var req matchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	providerID, externalID, ok := req.resolve(w, a, provider.KindPerformer)
	if !ok {
		return
	}

	var existing int64
	err = a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'PERFORMER_MATCH' AND status IN ('PENDING', 'RUNNING')
		  AND payload->>'performerId' = $1
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

	jobID, err := a.Jobs.Enqueue(r.Context(), "PERFORMER_MATCH", map[string]any{
		"performerId": id,
		"provider":    providerID,
		"externalId":  externalID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue performer match")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

func (a *API) getPerformerMatchJob(w http.ResponseWriter, r *http.Request) {
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
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	if kind != "PERFORMER_MATCH" {
		writeError(w, http.StatusNotFound, "job not found")
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

func (a *API) startPerformerSync(w http.ResponseWriter, r *http.Request) {
	if !a.providersReady(w, provider.KindPerformer) {
		return
	}
	var req struct {
		RetryUnresolved bool `json:"retryUnresolved"`
	}
	_ = decodeJSON(r, &req)
	var existing int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'PERFORMER_SYNC' AND status IN ('PENDING', 'RUNNING')
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
	jobID, err := a.Jobs.Enqueue(r.Context(), "PERFORMER_SYNC", map[string]any{"retryUnresolved": req.RetryUnresolved})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue performer sync")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

func (a *API) performerSyncStatus(w http.ResponseWriter, r *http.Request) {
	var id int64
	var status string
	var jobError *string
	var result []byte
	err := a.DB.QueryRow(r.Context(), `
		SELECT id, status, error, result FROM background_job
		WHERE kind = 'PERFORMER_SYNC'
		ORDER BY id DESC LIMIT 1
	`).Scan(&id, &status, &jobError, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performer sync")
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

func performerHitJSON(hit provider.PerformerResult) map[string]any {
	item := map[string]any{
		"provider": hit.Ref.Provider,
		"id":       hit.Ref.ExternalID,
		"name":     hit.Name,
	}
	if hit.Disambiguation != "" {
		item["disambiguation"] = hit.Disambiguation
	}
	if hit.ImageURL != "" {
		item["image"] = hit.ImageURL
	}
	if len(hit.Aliases) > 0 {
		item["aliases"] = hit.Aliases
	}
	return item
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/provider"
)

func (a *API) providerRegistry() *provider.Registry {
	if a.Namer == nil {
		return nil
	}
	return a.Namer.Providers
}

func providerKinds(c provider.Caps) []string {
	kinds := []string{}
	if c.Media {
		kinds = append(kinds, string(provider.KindMedia))
	}
	if c.Performer {
		kinds = append(kinds, string(provider.KindPerformer))
	}
	if c.Studio {
		kinds = append(kinds, string(provider.KindStudio))
	}
	return kinds
}

// listProviders is what the match modals use: every provider with the kinds it
// supports and whether it is configured. Modals offer only enabled ones as
// search choices but use the full list to name existing links.
func (a *API) listProviders(w http.ResponseWriter, r *http.Request) {
	items := []map[string]any{}
	if reg := a.providerRegistry(); reg != nil {
		for _, p := range reg.All() {
			kinds := providerKinds(p.Capabilities())
			if _, ok := p.(provider.SeriesProvider); ok {
				kinds = append(kinds, "series")
			}
			items = append(items, map[string]any{"id": p.ID(), "name": p.Name(), "kinds": kinds, "enabled": p.Enabled()})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// adminProviders is the full Settings view: every provider with its fields
// (secrets masked), plus the priority order and matching mode.
func (a *API) adminProviders(w http.ResponseWriter, r *http.Request) {
	reg := a.providerRegistry()
	items := []map[string]any{}
	if reg != nil {
		for _, p := range reg.All() {
			cfg := provider.LoadConfig(r.Context(), a.DB, p.ID())
			fields := []map[string]any{}
			for _, f := range p.ConfigFields() {
				value := cfg[f.Key]
				if value == "" && f.Type == "bool" {
					value = f.Default
				}
				if f.Secret {
					value = maskToken(value)
				}
				fields = append(fields, map[string]any{
					"key": f.Key, "label": f.Label, "secret": f.Secret,
					"placeholder": f.Placeholder, "type": f.Type, "value": value, "set": cfg[f.Key] != "",
				})
			}
			notice := ""
			if n, ok := p.(provider.Noticer); ok {
				notice = n.Notice()
			}
			item := map[string]any{
				"id": p.ID(), "name": p.Name(), "kinds": providerKinds(p.Capabilities()),
				"enabled": p.Enabled(), "fields": fields, "notice": notice,
			}
			if ext, ok := p.(interface{ BaseURL() string }); ok {
				item["external"] = true
				item["baseUrl"] = ext.BaseURL()
				if sp, ok := p.(interface{ Spec() extprovider.Spec }); ok {
					item["switchedOff"] = sp.Spec().Disabled
					item["bundle"] = sp.Spec().Bundle
				}
			}
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"mode":      provider.LoadMode(r.Context(), a.DB),
		"autoMatch": provider.LoadAutoMatch(r.Context(), a.DB),
	})
}

// updateProviderConfig merges the submitted fields into the saved settings (a
// field that is absent is left alone, an empty string clears it) and applies
// them to the live provider without a restart.
func (a *API) updateProviderConfig(w http.ResponseWriter, r *http.Request) {
	reg := a.providerRegistry()
	p, ok := providerFromRequest(reg, r)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown provider")
		return
	}
	var req map[string]string
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	allowed := map[string]bool{}
	for _, f := range p.ConfigFields() {
		allowed[f.Key] = true
	}
	merged := provider.LoadConfig(r.Context(), a.DB, p.ID())
	applied := map[string]string{}
	for k, v := range req {
		if !allowed[k] {
			writeError(w, http.StatusBadRequest, "unknown setting "+k)
			return
		}
		merged[k] = v
		applied[k] = v
	}
	if err := p.Configure(applied); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := provider.SaveConfig(r.Context(), a.DB, p.ID(), merged); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID(), "enabled": p.Enabled()})
}

func (a *API) deleteProviderConfig(w http.ResponseWriter, r *http.Request) {
	p, ok := providerFromRequest(a.providerRegistry(), r)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown provider")
		return
	}
	reset := map[string]string{}
	for _, f := range p.ConfigFields() {
		reset[f.Key] = ""
	}
	_ = p.Configure(reset)
	if err := provider.ClearConfig(r.Context(), a.DB, p.ID()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID(), "enabled": p.Enabled()})
}

// testProvider validates the saved settings against the live API.
func (a *API) testProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := providerFromRequest(a.providerRegistry(), r)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown provider")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := p.Test(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// updateProviderOrder saves the provider priority and the matching mode.
func (a *API) updateProviderOrder(w http.ResponseWriter, r *http.Request) {
	reg := a.providerRegistry()
	if reg == nil {
		writeError(w, http.StatusServiceUnavailable, "providers are not available")
		return
	}
	var req struct {
		Order []string `json:"order"`
		Mode  string   `json:"mode"`
		// AutoMatch switches automatic matching per kind; kinds left out are unchanged.
		AutoMatch map[string]bool `json:"autoMatch"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != "" && req.Mode != "priority" && req.Mode != "best" {
		writeError(w, http.StatusBadRequest, `mode must be "priority" or "best"`)
		return
	}
	for kind := range req.AutoMatch {
		if !slices.Contains(provider.AutoMatchKinds, kind) {
			writeError(w, http.StatusBadRequest, "unknown automatic matching kind "+kind)
			return
		}
	}
	for _, id := range req.Order {
		if _, ok := reg.Get(id); !ok {
			writeError(w, http.StatusBadRequest, "unknown provider "+id)
			return
		}
	}
	if req.Order != nil {
		if err := provider.SaveOrder(r.Context(), a.DB, req.Order); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save order")
			return
		}
		reg.SetOrder(req.Order)
	}
	if req.Mode != "" {
		if err := provider.SaveMode(r.Context(), a.DB, req.Mode); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save mode")
			return
		}
	}
	for kind, on := range req.AutoMatch {
		if err := provider.SaveAutoMatch(r.Context(), a.DB, kind, on); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save automatic matching")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func providerFromRequest(reg *provider.Registry, r *http.Request) (provider.Provider, bool) {
	if reg == nil {
		return nil, false
	}
	return reg.Get(chi.URLParam(r, "id"))
}

// loadLinks returns an entity's provider links as JSON: [{provider, externalId}].
func (a *API) loadLinks(ctx context.Context, kind string, id int64) json.RawMessage {
	var raw []byte
	err := a.DB.QueryRow(ctx, `
		SELECT COALESCE(json_agg(json_build_object('provider', provider, 'externalId', external_id) ORDER BY matched_at), '[]'::json)
		FROM match_link WHERE kind = $1 AND entity_id = $2
	`, kind, id).Scan(&raw)
	if err != nil {
		return nil
	}
	return raw
}

// startLinkRecovery enqueues a job that finds provider links for titles that
// were matched (for example from an older NFO) but carry none. It only records
// links; no file or title details change.
func (a *API) startLinkRecovery(w http.ResponseWriter, r *http.Request) {
	if a.Namer == nil || !a.Namer.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "no metadata provider is configured")
		return
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner is not available")
		return
	}
	var existing int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job WHERE kind = 'LINK_RECOVERY' AND status IN ('PENDING', 'RUNNING') ORDER BY id DESC LIMIT 1
	`).Scan(&existing)
	if err == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": existing, "alreadyRunning": true})
		return
	}
	jobID, err := a.Jobs.Enqueue(r.Context(), "LINK_RECOVERY", map[string]any{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue link recovery")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

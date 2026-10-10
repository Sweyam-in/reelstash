package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/provider"
)

type externalRequest struct {
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
}

// addExternalProvider connects a plugin: it reads the service's manifest,
// remembers it, and makes it available for matching right away.
func (a *API) addExternalProvider(w http.ResponseWriter, r *http.Request) {
	reg := a.providerRegistry()
	if reg == nil {
		writeError(w, http.StatusServiceUnavailable, "providers are not available")
		return
	}
	var req externalRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.BaseURL) == "" {
		writeError(w, http.StatusBadRequest, "baseUrl is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	spec, err := extprovider.Discover(ctx, req.BaseURL, strings.TrimSpace(req.Token))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not connect: "+err.Error())
		return
	}
	prev, existed := reg.Get(spec.ID)
	spec.Disabled = switchedOff(prev) // connecting again keeps the on/off choice
	p := extprovider.NewProvider(spec, "")
	if token := strings.TrimSpace(req.Token); token != "" {
		_ = p.Configure(map[string]string{"token": token})
		cfg := provider.LoadConfig(r.Context(), a.DB, spec.ID)
		cfg["token"] = token
		if err := provider.SaveConfig(r.Context(), a.DB, spec.ID, cfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save the token")
			return
		}
	} else if cfg := provider.LoadConfig(r.Context(), a.DB, spec.ID); cfg["token"] != "" {
		_ = p.Configure(cfg) // reconnecting keeps the token already saved
	}
	if err := extprovider.UpsertSpec(r.Context(), a.DB, spec); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the provider")
		return
	}
	reg.Register(p)
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"id": spec.ID, "name": spec.Name, "kinds": providerKinds(p.Capabilities())})
}

func (a *API) externalFromRequest(w http.ResponseWriter, r *http.Request) (*provider.Registry, provider.Provider, bool) {
	reg := a.providerRegistry()
	id := chi.URLParam(r, "id")
	if reg == nil || !strings.HasPrefix(id, extprovider.IDPrefix) {
		writeError(w, http.StatusNotFound, "unknown provider plugin")
		return nil, nil, false
	}
	p, ok := reg.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown provider plugin")
		return nil, nil, false
	}
	return reg, p, true
}

// refreshExternalProvider re-reads a plugin's manifest, for when it gained
// capabilities or moved its image host.
func (a *API) refreshExternalProvider(w http.ResponseWriter, r *http.Request) {
	reg, p, ok := a.externalFromRequest(w, r)
	if !ok {
		return
	}
	ext, isExt := p.(interface{ BaseURL() string })
	if !isExt {
		writeError(w, http.StatusNotFound, "unknown provider plugin")
		return
	}
	if isBundledProvider(p) {
		writeError(w, http.StatusBadRequest, "this provider runs from a plugin bundle; restart it under Plugins")
		return
	}
	cfg := provider.LoadConfig(r.Context(), a.DB, p.ID())
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	spec, err := extprovider.Discover(ctx, ext.BaseURL(), cfg["token"])
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not connect: "+err.Error())
		return
	}
	if spec.ID != p.ID() {
		writeError(w, http.StatusConflict, "the service now identifies as a different provider; remove this one and add it again")
		return
	}
	spec.Disabled = switchedOff(p)
	np := extprovider.NewProvider(spec, cfg["token"])
	if err := extprovider.UpsertSpec(r.Context(), a.DB, spec); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the provider")
		return
	}
	reg.Register(np)
	writeJSON(w, http.StatusOK, map[string]any{"id": spec.ID, "name": spec.Name, "kinds": providerKinds(np.Capabilities())})
}

// removeExternalProvider disconnects a plugin. Links it made stay on titles
// (they only name the provider) but it is no longer searched, and libraries
// stop listing it.
func (a *API) removeExternalProvider(w http.ResponseWriter, r *http.Request) {
	reg, p, ok := a.externalFromRequest(w, r)
	if !ok {
		return
	}
	if isBundledProvider(p) {
		writeError(w, http.StatusBadRequest, "this provider runs from a plugin bundle; uninstall it under Plugins")
		return
	}
	id := p.ID()
	reg.Unregister(id)
	extprovider.ClearCalls(id)
	if err := extprovider.RemoveSpec(r.Context(), a.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the provider")
		return
	}
	_ = provider.ClearConfig(r.Context(), a.DB, id)
	_, _ = a.DB.Exec(r.Context(), `DELETE FROM library_provider WHERE provider = $1`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// switchedOff reports whether p is a plugin an admin switched off.
func switchedOff(p provider.Provider) bool {
	sp, ok := p.(interface{ Spec() extprovider.Spec })
	return ok && sp.Spec().Disabled
}

func isBundledProvider(p provider.Provider) bool {
	b, ok := p.(interface{ Bundle() string })
	return ok && b.Bundle() != ""
}

// setExternalProviderEnabled switches a connected plugin off (kept, but never
// used for matching) or back on. A bundled plugin is also stopped or started.
func (a *API) setExternalProviderEnabled(w http.ResponseWriter, r *http.Request) {
	_, p, ok := a.externalFromRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if b, ok := p.(interface{ Bundle() string }); ok && b.Bundle() != "" {
		if a.Plugins == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin bundles are not available")
			return
		}
		if err := a.Plugins.SetEnabled(r.Context(), b.Bundle(), req.Enabled); err != nil {
			writePluginError(w, err, "could not change the plugin")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": p.ID(), "enabled": req.Enabled})
		return
	}
	ext, isExt := p.(interface {
		SetDisabled(bool)
		Spec() extprovider.Spec
	})
	if !isExt {
		writeError(w, http.StatusNotFound, "unknown provider plugin")
		return
	}
	ext.SetDisabled(!req.Enabled)
	if err := extprovider.UpsertSpec(r.Context(), a.DB, ext.Spec()); err != nil {
		ext.SetDisabled(req.Enabled)
		writeError(w, http.StatusInternalServerError, "failed to save the provider")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID(), "enabled": req.Enabled})
}

// externalProviderLog is the plugin's recent requests, newest first.
func (a *API) externalProviderLog(w http.ResponseWriter, r *http.Request) {
	_, p, ok := a.externalFromRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": extprovider.RecentCalls(p.ID())})
}

func (a *API) clearExternalProviderLog(w http.ResponseWriter, r *http.Request) {
	_, p, ok := a.externalFromRequest(w, r)
	if !ok {
		return
	}
	extprovider.ClearCalls(p.ID())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

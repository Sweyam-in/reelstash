package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/plugins"
)

// Admin → Plugins: install, configure, switch off and remove plugin bundles
// (docs/plugin-bundles.md). A bundle is code the server runs, so every route
// here is admin only.

func (a *API) pluginsOrUnavailable(w http.ResponseWriter) (*plugins.Manager, bool) {
	if a.Plugins == nil {
		writeError(w, http.StatusServiceUnavailable, "plugin bundles are not available")
		return nil, false
	}
	return a.Plugins, true
}

func writePluginError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, plugins.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such plugin bundle")
	case errors.Is(err, plugins.ErrDisabled):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusBadRequest, fallback+": "+err.Error())
	}
}

func (a *API) pluginJSON(r *http.Request, info plugins.Info) map[string]any {
	saved := a.Plugins.Config(r.Context(), info.ID)
	fields := []map[string]any{}
	for _, f := range info.Config {
		value := saved[f.Key]
		if f.Secret {
			value = maskToken(value)
		}
		fields = append(fields, map[string]any{
			"key": f.Key, "label": f.Label, "secret": f.Secret, "required": f.Required,
			"placeholder": f.Placeholder, "default": f.Default, "value": value, "set": saved[f.Key] != "",
		})
	}
	missing := info.Missing
	if missing == nil {
		missing = []string{}
	}
	item := map[string]any{
		"id": info.ID, "name": info.Name, "version": info.Version, "kind": info.Kind,
		"description": info.Description, "homepage": info.Homepage, "enabled": info.Enabled,
		"status": info.Status, "error": info.Error, "restarts": info.Restarts, "startedAt": info.StartedAt,
		"fields": fields, "missing": missing, "command": info.Run.Command,
	}
	if info.ProviderID != "" {
		item["providerId"] = info.ProviderID
	}
	item["updateUrl"] = a.Plugins.UpdateSource(r.Context(), info.ID)
	if info.Previous != "" {
		item["previousVersion"] = info.Previous
	}
	if info.Kind == plugins.KindAuth {
		item["callbackUrl"] = a.authCallbackURL(r, info.ID)
	}
	return item
}

// listPlugins shows what is installed.
func (a *API) listPlugins(w http.ResponseWriter, r *http.Request) {
	if a.Plugins == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "items": []any{}})
		return
	}
	items := []map[string]any{}
	for _, info := range a.Plugins.List(r.Context()) {
		items = append(items, a.pluginJSON(r, info))
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": a.Plugins.Enabled(), "items": items})
}

func (a *API) respondInstalled(w http.ResponseWriter, r *http.Request, info plugins.Info) {
	writeJSON(w, http.StatusCreated, a.pluginJSON(r, info))
}

// installPluginUpload installs a bundle from an uploaded .zip (form field "bundle").
func (a *API) installPluginUpload(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, plugins.MaxArchive+(1<<20))
	file, _, err := r.FormFile("bundle")
	if err != nil {
		writeError(w, http.StatusBadRequest, "attach the bundle as a .zip in the form field \"bundle\"")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, plugins.MaxArchive+1))
	if err != nil || len(data) > plugins.MaxArchive {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the bundle is larger than %d MB", plugins.MaxArchive>>20))
		return
	}
	info, err := mgr.InstallZip(r.Context(), data)
	if err != nil {
		writePluginError(w, err, "could not install the bundle")
		return
	}
	a.respondInstalled(w, r, info)
}

// installPluginURL downloads a .zip bundle from an address and installs it.
func (a *API) installPluginURL(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	data, err := plugins.Download(r.Context(), req.URL)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, plugins.ErrBadAddress) || errors.Is(err, plugins.ErrTooLarge) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "could not download the bundle: "+err.Error())
		return
	}
	info, err := mgr.InstallZip(r.Context(), data)
	if err != nil {
		writePluginError(w, err, "could not install the bundle")
		return
	}
	a.respondInstalled(w, r, info)
}

func (a *API) setPluginEnabled(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
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
	id := chi.URLParam(r, "id")
	if err := mgr.SetEnabled(r.Context(), id, req.Enabled); err != nil {
		writePluginError(w, err, "could not change the plugin")
		return
	}
	info, _ := mgr.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, a.pluginJSON(r, info))
}

// updatePluginConfig merges settings into a bundle's and restarts it.
func (a *API) updatePluginConfig(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	var req map[string]string
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := chi.URLParam(r, "id")
	if err := mgr.SetConfig(r.Context(), id, req); err != nil {
		writePluginError(w, err, "could not save the settings")
		return
	}
	info, _ := mgr.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, a.pluginJSON(r, info))
}

func (a *API) restartPlugin(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := mgr.Restart(r.Context(), id); err != nil {
		writePluginError(w, err, "could not restart the plugin")
		return
	}
	info, _ := mgr.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, a.pluginJSON(r, info))
}

func (a *API) pluginLog(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	lines, err := mgr.Logs(chi.URLParam(r, "id"))
	if err != nil {
		writePluginError(w, err, "could not read the log")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// removePlugin uninstalls a bundle. A provider bundle's links on titles stay
// (they only name the provider) but libraries stop listing it.
func (a *API) removePlugin(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	info, err := mgr.Remove(r.Context(), chi.URLParam(r, "id"))
	if err != nil && !errors.Is(err, plugins.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "could not remove the plugin: "+err.Error())
		return
	}
	if err != nil {
		writePluginError(w, err, "")
		return
	}
	if info.ProviderID != "" {
		_, _ = a.DB.Exec(r.Context(), `DELETE FROM library_provider WHERE provider = $1`, info.ProviderID)
		extprovider.ClearCalls(info.ProviderID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// authCallbackURL is the address to register with the provider for a sign-in
// plugin: the plugin's own callbackPath once it is running, else the default.
func (a *API) authCallbackURL(r *http.Request, id string) string {
	if p, ok := a.AuthPlugins.Get(id); ok {
		return pluginCallbackURL(r, p)
	}
	return requestOrigin(r) + "/auth/plugin/" + id + "/callback"
}

// setPluginUpdateSource saves where a plugin looks for newer versions.
func (a *API) setPluginUpdateSource(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := chi.URLParam(r, "id")
	if err := mgr.SetUpdateSource(r.Context(), id, req.URL); err != nil {
		writePluginError(w, err, "could not save the update source")
		return
	}
	info, _ := mgr.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, a.pluginJSON(r, info))
}

// checkPluginUpdate asks the update source if a newer version is published.
func (a *API) checkPluginUpdate(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	res, err := mgr.CheckUpdate(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, plugins.ErrNotFound) {
			writePluginError(w, err, "")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// updatePlugin installs the latest published version, keeping settings and data.
func (a *API) updatePlugin(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	info, err := mgr.Update(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, plugins.ErrNotFound) || errors.Is(err, plugins.ErrDisabled) {
			writePluginError(w, err, "")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.pluginJSON(r, info))
}

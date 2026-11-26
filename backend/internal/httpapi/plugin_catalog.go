package httpapi

import "net/http"

// listPluginCatalog reads every saved plugin catalog (index.json) and lists
// what each offers, with the version installed here.
func (a *API) listPluginCatalog(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": mgr.Enabled(),
		"sources": mgr.BrowseCatalogs(r.Context()),
	})
}

// setPluginCatalogs replaces the saved catalog addresses.
func (a *API) setPluginCatalogs(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	var req struct {
		Sources []string `json:"sources"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	saved, err := mgr.SetCatalogs(r.Context(), req.Sources)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not save the catalogs: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": saved})
}

// installFromPluginCatalog installs the latest version of a plugin a saved
// catalog lists, checked against its sha256.
func (a *API) installFromPluginCatalog(w http.ResponseWriter, r *http.Request) {
	mgr, ok := a.pluginsOrUnavailable(w)
	if !ok {
		return
	}
	var req struct {
		Source string `json:"source"`
		ID     string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Source == "" || req.ID == "" {
		writeError(w, http.StatusBadRequest, "source and id are required")
		return
	}
	info, err := mgr.InstallFromCatalog(r.Context(), req.Source, req.ID)
	if err != nil {
		writePluginError(w, err, "could not install "+req.ID)
		return
	}
	writeJSON(w, http.StatusCreated, a.pluginJSON(r, info))
}

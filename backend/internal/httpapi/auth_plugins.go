package httpapi

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/authplugin"
)

// Sign-in through auth plugins (see docs/auth-plugins.md). A plugin proves who
// the visitor is; ReelStash keeps the user, the role and the session.

// signInPlugins is what the sign-in screen lists: one entry per running plugin.
func (a *API) signInPlugins() []map[string]any {
	out := []map[string]any{}
	for _, p := range a.AuthPlugins.List() {
		out = append(out, map[string]any{"id": p.ID(), "name": p.Name(), "type": p.Manifest().Login.Type, "label": p.Label()})
	}
	return out
}

func (a *API) authPluginFromRequest(w http.ResponseWriter, r *http.Request) (*authplugin.Plugin, bool) {
	p, ok := a.AuthPlugins.Get(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "this sign-in method is not available")
		return nil, false
	}
	return p, true
}

// pluginCallbackURL is where the plugin's identity provider sends the browser
// back, on the host the visitor used (so a LAN visit returns to the LAN).
func pluginCallbackURL(r *http.Request, p *authplugin.Plugin) string {
	return requestOrigin(r) + callbackPathFor(p)
}

func callbackPathFor(p *authplugin.Plugin) string {
	if cp := p.Manifest().CallbackPath; cp != "" {
		return cp
	}
	return "/auth/plugin/" + p.ID() + "/callback"
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if secureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// handlePluginLogin starts a redirect sign-in.
func (a *API) handlePluginLogin(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authPluginFromRequest(w, r)
	if !ok {
		return
	}
	a.startPluginLogin(w, r, p)
}

func (a *API) startPluginLogin(w http.ResponseWriter, r *http.Request, p *authplugin.Plugin) {
	target, err := p.Start(r.Context(), pluginCallbackURL(r, p), safeRedirect(r.URL.Query().Get("redirect_to")))
	if err != nil {
		log.Printf("auth plugin %s: start: %v", p.ID(), err)
		authErrorRedirect(w, r, "login_failed")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleDefaultLogin is /auth/login, where links and bookmarks from before
// sign-in plugins land: it starts the sign-in of the first redirect plugin.
func (a *API) handleDefaultLogin(w http.ResponseWriter, r *http.Request) {
	for _, p := range a.AuthPlugins.List() {
		if p.Manifest().Login.Type == authplugin.LoginRedirect {
			a.startPluginLogin(w, r, p)
			return
		}
	}
	writeError(w, http.StatusServiceUnavailable, "no sign-in plugin is installed")
}

// handlePluginCallbackAt serves the callback address a plugin declared with
// callbackPath (for example /auth/callback).
func (a *API) handlePluginCallbackAt(w http.ResponseWriter, r *http.Request) {
	path := "/auth/" + chi.URLParam(r, "*")
	for _, p := range a.AuthPlugins.List() {
		if p.Manifest().CallbackPath == path {
			a.finishPluginLogin(w, r, p)
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

// handlePluginCallback finishes a redirect sign-in.
func (a *API) handlePluginCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authPluginFromRequest(w, r)
	if !ok {
		return
	}
	a.finishPluginLogin(w, r, p)
}

func (a *API) finishPluginLogin(w http.ResponseWriter, r *http.Request, p *authplugin.Plugin) {
	res, err := p.Finish(r.Context(), pluginCallbackURL(r, p), r.URL.Query())
	if err != nil {
		log.Printf("auth plugin %s: finish: %v", p.ID(), err)
		authErrorRedirect(w, r, "login_failed")
		return
	}
	if res.Error != "" {
		authErrorRedirect(w, r, knownAuthError(res.Error))
		return
	}
	a.completePluginLogin(w, r, p, res)
}

// handlePluginPassword signs in with a username and password the plugin checks.
func (a *API) handlePluginPassword(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authPluginFromRequest(w, r)
	if !ok {
		return
	}
	var req nativeLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ip := clientIP(r)
	throttleName := p.ID() + ":" + req.Username
	if a.throttle.blocked(r.Context(), a.DB, ip, throttleName) {
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again in a few minutes")
		return
	}
	res, err := p.Password(r.Context(), req.Username, req.Password)
	if err != nil {
		log.Printf("auth plugin %s: login: %v", p.ID(), err)
		writeError(w, http.StatusBadGateway, "the sign-in service is not answering")
		return
	}
	switch res.Error {
	case "":
	case "not_authorized", "account_disabled":
		writeError(w, http.StatusForbidden, "you do not have access")
		return
	default:
		a.throttle.fail(r.Context(), a.DB, ip, throttleName)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	user, err := a.upsertPluginUser(r, p, res)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign in")
		return
	}
	if user.Disabled {
		writeError(w, http.StatusForbidden, "this account is disabled")
		return
	}
	if err := a.startSessionVia(w, r, user, p.ID()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	a.throttle.succeed(r.Context(), a.DB, ip, throttleName)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) upsertPluginUser(r *http.Request, p *authplugin.Plugin, res authplugin.Result) (auth.User, error) {
	id := res.Identity
	return auth.UpsertIdentityUser(r.Context(), a.DB, auth.Claims{
		Subject: p.SubjectKey(id.Subject), Email: id.Email, DisplayName: id.DisplayName, AvatarURL: id.AvatarURL,
	}, id.Role)
}

func (a *API) completePluginLogin(w http.ResponseWriter, r *http.Request, p *authplugin.Plugin, res authplugin.Result) {
	user, err := a.upsertPluginUser(r, p, res)
	if err != nil {
		log.Printf("auth plugin %s: upsert user: %v", p.ID(), err)
		authErrorRedirect(w, r, "server_error")
		return
	}
	if user.Disabled {
		authErrorRedirect(w, r, "account_disabled")
		return
	}
	if err := a.startSessionVia(w, r, user, p.ID()); err != nil {
		log.Printf("auth plugin %s: create session: %v", p.ID(), err)
		authErrorRedirect(w, r, "server_error")
		return
	}
	http.Redirect(w, r, safeRedirect(res.RedirectTo), http.StatusFound)
}

// knownAuthError limits a plugin's error word to the ones the sign-in screen
// has a page for.
func knownAuthError(code string) string {
	switch code {
	case "cancelled", "not_authorized", "account_disabled", "login_failed", "server_error":
		return code
	}
	return "login_failed"
}

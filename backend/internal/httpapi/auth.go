package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/authplugin"
)

type ctxKey string

const userCtxKey ctxKey = "reelstash.user"

func UserFromContext(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(userCtxKey).(auth.User)
	return u, ok
}

// handleAuthConfig is public (no session required) so the login screen
// knows which sign-in option(s) to offer before the user is authenticated.
func (a *API) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	mediaURL, adminURL := appURLsFor(a.Cfg.MediaURLs, a.Cfg.AdminURLs, r.Host)
	writeJSON(w, http.StatusOK, map[string]any{
		"localAuthEnabled": a.Cfg.EmergencyLocalAuth,
		"nativeEnabled":    a.nativeAllowed(r.Context()) && auth.NativeUserCount(r.Context(), a.DB) > 0,
		"needsSetup":       a.needsSetup(r.Context()),
		"plugins":          a.signInPlugins(),
		"mediaUrl":         mediaURL,
		"adminUrl":         adminURL,
		"colorTheme":       a.mediaColorTheme(r.Context()),
	})
}

// appURLsFor returns the media/admin origin pair whose either side is served
// on host, so the public site links to the public admin and the LAN site to
// the LAN admin. Neither found (or unpaired) returns "", "": same origin.
func appURLsFor(media, admin []string, host string) (string, string) {
	for i := 0; i < len(media) && i < len(admin); i++ {
		for _, raw := range []string{media[i], admin[i]} {
			if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Host, host) {
				return strings.TrimRight(media[i], "/"), strings.TrimRight(admin[i], "/")
			}
		}
	}
	return "", ""
}

// secureRequest reports whether the browser reached us over HTTPS — directly
// or via the Cloudflare tunnel (which sets X-Forwarded-Proto / Cf-Visitor).
// Cookies are only marked Secure then: a Secure cookie set over plain-HTTP
// LAN access (http://media.local:8000) is dropped by the browser, which
// would make login impossible there.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") ||
		strings.Contains(r.Header.Get("Cf-Visitor"), `"https"`)
}

func (a *API) setSessionCookie(w http.ResponseWriter, r *http.Request, id string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    id,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// requireAuth enforces that every /api request carries a valid session.
// Authorization is decided here, server-side, on every request — the React
// UI hiding a nav item is presentation only, never the security boundary.
func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, bearer := sessionToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		user, err := auth.LoadSessionUser(r.Context(), a.DB, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired or invalid")
			return
		}
		if user.Disabled {
			writeError(w, http.StatusForbidden, "account disabled")
			return
		}
		if bearer {
			auth.TouchDeviceSession(r.Context(), a.DB, token)
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		ctx = context.WithValue(ctx, sessionCtxKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok || user.Role != "ADMIN" {
			writeError(w, http.StatusForbidden, "admin privileges required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                user.ID,
		"email":             user.Email,
		"displayName":       user.DisplayName,
		"avatarUrl":         user.AvatarURL,
		"role":              user.Role,
		"isAdmin":           user.Role == "ADMIN",
		"canChangePassword": auth.PasswordHashFor(r.Context(), a.DB, user.ID) != "",
	})
}

// startSession opens a server-side session for user and sets its cookie.
func (a *API) startSession(w http.ResponseWriter, r *http.Request, user auth.User) error {
	return a.startSessionVia(w, r, user, "")
}

// startSessionVia is startSession for a user a sign-in plugin authenticated.
func (a *API) startSessionVia(w http.ResponseWriter, r *http.Request, user auth.User, plugin string) error {
	sessionID, expiresAt, err := auth.CreateSession(r.Context(), a.DB, user.ID, plugin)
	if err != nil {
		return err
	}
	a.setSessionCookie(w, r, sessionID, expiresAt)
	return nil
}

// safeRedirect keeps post-login redirects on this site: a path, never another
// host ("//evil.test", "/\\evil.test") or a scheme.
func safeRedirect(to string) string {
	if !strings.HasPrefix(to, "/") || strings.HasPrefix(to, "//") || strings.HasPrefix(to, "/\\") {
		return "/"
	}
	return to
}

// authErrorRedirect sends a browser that failed a sign-in back to the
// UI's login screen, which renders a friendly page for the given reason code.
func authErrorRedirect(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/?auth_error="+url.QueryEscape(reason), http.StatusFound)
}

type localLoginRequest struct {
	Password string `json:"password"`
}

// handleLocalLogin is the emergency self-hosted fallback, disabled unless
// EMERGENCY_LOCAL_AUTH=true. It never runs when a sign-in plugin is the intended
// identity provider for the deployment.
func (a *API) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	if !a.Cfg.EmergencyLocalAuth {
		writeError(w, http.StatusServiceUnavailable, "emergency local auth is disabled")
		return
	}
	var req localLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(a.Cfg.EmergencyLocalPassword)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	user, err := auth.UpsertLocalAdmin(r.Context(), a.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create local session")
		return
	}
	if err := a.startSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLogout ends the session. When the person signed in through a plugin
// that names a logout page on the host they are visiting, that address comes
// back so the UI can end the provider's session too.
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	via := ""
	if token, _ := sessionToken(r); token != "" {
		via = auth.SessionPlugin(r.Context(), a.DB, token)
		auth.DeleteSession(r.Context(), a.DB, token)
	}
	a.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "logoutUrl": a.pluginLogoutURL(via, r.Host)})
}

// pluginLogoutURL is the plugin's logout page, if it has one on host (the
// public site's page is not offered to a LAN visitor). A session that predates
// recorded plugins uses the only plugin that has one.
func (a *API) pluginLogoutURL(via, host string) string {
	var candidates []*authplugin.Plugin
	if p, ok := a.AuthPlugins.Get(via); ok {
		candidates = []*authplugin.Plugin{p}
	} else if via == "" {
		candidates = a.AuthPlugins.List()
	}
	found := ""
	for _, p := range candidates {
		raw := p.Manifest().LogoutURL
		if raw == "" {
			continue
		}
		if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Host, host) {
			if found != "" {
				return "" // ambiguous
			}
			found = raw
		}
	}
	return found
}

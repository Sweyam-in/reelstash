package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"reelstash/backend/internal/auth"
)

// Apps (Android now, iOS and TV later) sign in with a bearer token: a device
// session. They get one by password (POST /auth/token) or, for any sign-in
// method including plugins, by pairing: the app shows a code and a signed-in
// person enters it in the web UI.

// Version is the server build, set with -ldflags "-X .../httpapi.Version=…".
var Version = "dev"

// apiVersion rises when the app-facing API changes incompatibly.
const apiVersion = 1

// pairingPollSeconds is how often an app should poll a pending pairing.
const pairingPollSeconds = 3

const sessionCtxKey ctxKey = "reelstash.session"

// sessionToken returns the request's session id and whether it came as a
// bearer token (an app) rather than the browser cookie.
func sessionToken(r *http.Request) (token string, bearer bool) {
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	if c, err := r.Cookie(auth.CookieName); err == nil {
		return c.Value, false
	}
	return "", false
}

// handleServerInfo is public: an app checks an address with it before
// signing in, and learns which sign-in methods to offer.
func (a *API) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"product":       "reelstash",
		"name":          a.Cfg.ServerName,
		"version":       Version,
		"apiVersion":    apiVersion,
		"passwordLogin": a.nativeAllowed(r.Context()) && auth.NativeUserCount(r.Context(), a.DB) > 0,
		"pairing":       true,
		"plugins":       a.signInPlugins(),
	})
}

type deviceRequest struct {
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

func (d deviceRequest) device() auth.Device {
	return auth.Device{Name: d.DeviceName, Platform: d.Platform}
}

func userJSON(u auth.User) map[string]any {
	return map[string]any{
		"id":          u.ID,
		"displayName": u.DisplayName,
		"email":       u.Email,
		"avatarUrl":   u.AvatarURL,
		"isAdmin":     u.Role == "ADMIN",
	}
}

func writeDeviceSession(w http.ResponseWriter, token string, expiresAt time.Time, user auth.User) {
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expiresAt,
		"user":      userJSON(user),
	})
}

// handleTokenLogin signs an app in with a username and password.
func (a *API) handleTokenLogin(w http.ResponseWriter, r *http.Request) {
	if !a.nativeAllowed(r.Context()) {
		writeError(w, http.StatusForbidden, "password sign-in is turned off")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		deviceRequest
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, ok := a.authenticatePassword(w, r, req.Username, req.Password)
	if !ok {
		return
	}
	token, exp, err := auth.CreateDeviceSession(r.Context(), a.DB, user.ID, "", req.device())
	if err != nil {
		log.Printf("token login: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeDeviceSession(w, token, exp, user)
}

// handlePairingStart begins pairing: the app shows userCode and polls with deviceCode.
func (a *API) handlePairingStart(w http.ResponseWriter, r *http.Request) {
	var req deviceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := auth.StartPairing(r.Context(), a.DB, req.device())
	if err != nil {
		log.Printf("pairing start: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start pairing")
		return
	}
	link := "/link"
	if media, _ := appURLsFor(a.Cfg.MediaURLs, a.Cfg.AdminURLs, r.Host); media != "" {
		link = media + link
	} else {
		link = requestOrigin(r) + link
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceCode":      p.DeviceCode,
		"userCode":        p.UserCode,
		"verificationUrl": link,
		"expiresIn":       int(time.Until(p.ExpiresAt).Seconds()),
		"interval":        pairingPollSeconds,
	})
}

// handlePairingPoll answers 202 while the pairing waits for approval, 404 once
// it expired, and the device session when it was approved.
func (a *API) handlePairingPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := decodeJSON(r, &req); err != nil || req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, exp, user, err := auth.ClaimPairing(r.Context(), a.DB, req.DeviceCode)
	switch {
	case errors.Is(err, auth.ErrPairingPending):
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "pending"})
	case errors.Is(err, auth.ErrPairingNotFound):
		writeError(w, http.StatusNotFound, "pairing code expired")
	case err != nil:
		log.Printf("pairing poll: %v", err)
		writeError(w, http.StatusInternalServerError, "pairing failed")
	case user.Disabled:
		_ = auth.DeleteSession(r.Context(), a.DB, token)
		writeError(w, http.StatusForbidden, "this account is disabled")
	default:
		writeDeviceSession(w, token, exp, user)
	}
}

// lookupPairing shows the signed-in person which device a code belongs to,
// before they approve it.
func (a *API) lookupPairing(w http.ResponseWriter, r *http.Request) {
	p, err := auth.LookupPairing(r.Context(), a.DB, r.URL.Query().Get("code"))
	if errors.Is(err, auth.ErrPairingNotFound) {
		writeError(w, http.StatusNotFound, "that code is wrong or has expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up code")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// approvePairing signs the device showing the code in as the current user.
func (a *API) approvePairing(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := auth.ApprovePairing(r.Context(), a.DB, req.Code, user.ID)
	if errors.Is(err, auth.ErrPairingNotFound) {
		writeError(w, http.StatusNotFound, "that code is wrong or has expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve device")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// listDevices lists the current user's signed-in apps.
func (a *API) listDevices(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	current, _ := r.Context().Value(sessionCtxKey).(string)
	devices, err := auth.ListDeviceSessions(r.Context(), a.DB, user.ID, current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

// revokeDevice signs one of the current user's apps out.
func (a *API) revokeDevice(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return
	}
	ok, err := auth.RevokeDeviceSession(r.Context(), a.DB, user.ID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign the device out")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

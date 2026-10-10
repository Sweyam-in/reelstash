package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/auth"
)

// Built-in username/password sign-in: it needs no identity provider, so a
// plain self-hosted install works out of the box.

const nativeOffSetting = "auth_native_disabled"

// nativeAllowed reports whether password sign-in is switched on (the default).
func (a *API) nativeAllowed(ctx context.Context) bool {
	var v string
	err := a.DB.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, nativeOffSetting).Scan(&v)
	return err != nil || v != "true"
}

// otherSignIn reports whether people can sign in some way other than a
// password: the emergency password, or a sign-in plugin.
func (a *API) otherSignIn() bool {
	return a.Cfg.EmergencyLocalAuth || len(a.AuthPlugins.List()) > 0
}

// needsSetup is true on a fresh install: nobody can sign in yet, so the sign-in
// screen offers to create the first admin.
func (a *API) needsSetup(ctx context.Context) bool {
	return a.nativeAllowed(ctx) && !a.otherSignIn() && !auth.AdminExists(ctx, a.DB)
}

// loginThrottle slows password guessing: too many failures from one address
// for one username, from one address overall, or against one username from
// anywhere, and further attempts are refused for a while.
type loginThrottle struct {
	mu      sync.Mutex
	entries map[string]*throttleEntry
	now     func() time.Time
}

type throttleEntry struct {
	fails int
	until time.Time // end of the window
}

const (
	throttleWindow  = 15 * time.Minute
	limitPerPair    = 8
	limitPerIP      = 40
	limitPerUser    = 60
	maxThrottleKeys = 10000
)

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{entries: map[string]*throttleEntry{}, now: time.Now}
}

func throttleKeys(ip, username string) (pair, byIP, byUser string) {
	u := strings.ToLower(strings.TrimSpace(username))
	if len(u) > 256 {
		u = u[:256]
	}
	return "pair|" + ip + "|" + u, "ip|" + ip, "user|" + u
}

func (t *loginThrottle) blocked(ctx context.Context, db *pgxpool.Pool, ip, username string) bool {
	pair, byIP, byUser := throttleKeys(ip, username)
	limits := map[string]int{pair: limitPerPair, byIP: limitPerIP, byUser: limitPerUser}
	if db != nil {
		rows, err := db.Query(ctx, `SELECT bucket, fails, until_at FROM auth_throttle WHERE bucket = ANY($1)`, []string{pair, byIP, byUser})
		if err == nil {
			defer rows.Close()
			now := t.now()
			for rows.Next() {
				var bucket string
				var fails int
				var until time.Time
				if rows.Scan(&bucket, &fails, &until) == nil && now.Before(until) && fails >= limits[bucket] {
					return true
				}
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for key, limit := range limits {
		if e := t.entries[key]; e != nil && now.Before(e.until) && e.fails >= limit {
			return true
		}
	}
	return false
}

func (t *loginThrottle) fail(ctx context.Context, db *pgxpool.Pool, ip, username string) {
	pair, byIP, byUser := throttleKeys(ip, username)
	t.mu.Lock()
	now := t.now()
	for k, e := range t.entries { // forget expired windows
		if now.After(e.until) {
			delete(t.entries, k)
		}
	}
	for _, key := range []string{pair, byIP, byUser} {
		e := t.entries[key]
		if e == nil {
			if len(t.entries) >= maxThrottleKeys {
				continue
			}
			e = &throttleEntry{until: now.Add(throttleWindow)}
			t.entries[key] = e
		}
		e.fails++
	}
	t.mu.Unlock()
	if db == nil {
		return
	}
	for _, key := range []string{pair, byIP, byUser} {
		_, _ = db.Exec(ctx, `
			INSERT INTO auth_throttle (bucket, fails, until_at)
			VALUES ($1, 1, now() + interval '15 minutes')
			ON CONFLICT (bucket) DO UPDATE SET
				fails = CASE WHEN auth_throttle.until_at < now() THEN 1 ELSE auth_throttle.fails + 1 END,
				until_at = CASE WHEN auth_throttle.until_at < now() THEN now() + interval '15 minutes' ELSE auth_throttle.until_at END
		`, key)
	}
}

func (t *loginThrottle) succeed(ctx context.Context, db *pgxpool.Pool, ip, username string) {
	pair, _, _ := throttleKeys(ip, username)
	t.mu.Lock()
	delete(t.entries, pair)
	t.mu.Unlock()
	if db != nil {
		_, _ = db.Exec(ctx, `DELETE FROM auth_throttle WHERE bucket = $1`, pair)
	}
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

type nativeLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleNativeLogin signs in with a ReelStash username and password.
func (a *API) handleNativeLogin(w http.ResponseWriter, r *http.Request) {
	if !a.nativeAllowed(r.Context()) {
		writeError(w, http.StatusForbidden, "password sign-in is turned off")
		return
	}
	var req nativeLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, ok := a.authenticatePassword(w, r, req.Username, req.Password)
	if !ok {
		return
	}
	if err := a.startSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// authenticatePassword checks a username and password with throttling, and
// writes the error response itself when they are refused.
func (a *API) authenticatePassword(w http.ResponseWriter, r *http.Request, username, password string) (auth.User, bool) {
	ip := clientIP(r)
	if a.throttle.blocked(r.Context(), a.DB, ip, username) {
		w.Header().Set("Retry-After", strconv.Itoa(int(throttleWindow.Seconds())))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again in a few minutes")
		return auth.User{}, false
	}
	user, err := auth.AuthenticateNative(r.Context(), a.DB, username, password)
	if errors.Is(err, auth.ErrBadCredentials) {
		a.throttle.fail(r.Context(), a.DB, ip, username)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return auth.User{}, false
	}
	if err != nil {
		log.Printf("native login: %v", err)
		writeError(w, http.StatusInternalServerError, "sign-in failed")
		return auth.User{}, false
	}
	if user.Disabled {
		writeError(w, http.StatusForbidden, "this account is disabled")
		return auth.User{}, false
	}
	a.throttle.succeed(r.Context(), a.DB, ip, username)
	return user, true
}

type setupRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

// handleSetup creates the first admin on a fresh install and signs them in.
// It works once: as soon as any admin exists, or any other sign-in method is
// configured, it refuses.
func (a *API) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !a.needsSetup(r.Context()) {
		writeError(w, http.StatusConflict, "ReelStash is already set up")
		return
	}
	user, err := auth.CreateFirstAdmin(r.Context(), a.DB, req.Username, req.Password, req.DisplayName)
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, http.StatusConflict, "ReelStash is already set up")
		return
	case isInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		log.Printf("setup: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create the admin account")
		return
	}
	if err := a.startSession(w, r, user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func isInputError(err error) bool {
	return errors.Is(err, auth.ErrInvalidUsername) || errors.Is(err, auth.ErrWeakPassword) || errors.Is(err, auth.ErrUsernameTaken)
}

type changePasswordRequest struct {
	Current  string `json:"current"`
	Password string `json:"password"`
}

// changeMyPassword lets a native account change its own password. Every other
// session of the account is signed out.
func (a *API) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	hash := auth.PasswordHashFor(r.Context(), a.DB, user.ID)
	if hash == "" {
		writeError(w, http.StatusBadRequest, auth.ErrNotNative.Error())
		return
	}
	ip := clientIP(r)
	if a.throttle.blocked(r.Context(), a.DB, ip, "pw:"+strconv.FormatInt(user.ID, 10)) {
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again in a few minutes")
		return
	}
	if !auth.CheckPassword(hash, req.Current) {
		a.throttle.fail(r.Context(), a.DB, ip, "pw:"+strconv.FormatInt(user.ID, 10))
		writeError(w, http.StatusForbidden, "the current password is not right")
		return
	}
	keep := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		keep = c.Value
	}
	if err := auth.SetPassword(r.Context(), a.DB, user.ID, req.Password, keep); err != nil {
		if isInputError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to change the password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

// createUser adds a password account (admin only).
func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role != "ADMIN" && req.Role != "USER" {
		writeError(w, http.StatusBadRequest, "role must be ADMIN or USER")
		return
	}
	user, err := auth.CreateNativeUser(r.Context(), a.DB, req.Username, req.Password, req.DisplayName, req.Role)
	switch {
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, err.Error())
		return
	case isInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		log.Printf("create user: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create the user")
		return
	}
	noteAudit(r, req.Username, map[string]any{"role": req.Role})
	writeJSON(w, http.StatusCreated, map[string]any{"id": user.ID})
}

// resetUserPassword sets a new password for a password account (admin only)
// and signs that account out everywhere.
func (a *API) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	keep := ""
	if actor, ok := UserFromContext(r.Context()); ok && actor.ID == id {
		if c, err := r.Cookie(auth.CookieName); err == nil {
			keep = c.Value
		}
	}
	switch err := auth.SetPassword(r.Context(), a.DB, id, req.Password, keep); {
	case errors.Is(err, auth.ErrNotNative):
		writeError(w, http.StatusBadRequest, err.Error())
	case isInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to set the password")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
	}
}

// getAuthSettings tells the admin how people can sign in right now.
func (a *API) getAuthSettings(w http.ResponseWriter, r *http.Request) {
	plugins := []map[string]any{}
	for _, p := range a.AuthPlugins.List() {
		plugins = append(plugins, map[string]any{"id": p.ID(), "name": p.Name(), "type": p.Manifest().Login.Type})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nativeEnabled": a.nativeAllowed(r.Context()),
		"nativeUsers":   auth.NativeUserCount(r.Context(), a.DB),
		"emergency":     a.Cfg.EmergencyLocalAuth,
		"plugins":       plugins,
	})
}

// updateAuthSettings switches password sign-in on or off. It cannot be turned
// off while it is the only way in, which would lock everyone out.
func (a *API) updateAuthSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NativeEnabled bool `json:"nativeEnabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.NativeEnabled && !a.otherSignIn() {
		writeError(w, http.StatusConflict, "password sign-in is the only way in; set up another sign-in method first")
		return
	}
	value := "false"
	if !req.NativeEnabled {
		value = "true"
	}
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, nativeOffSetting, value); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nativeEnabled": req.NativeEnabled})
}

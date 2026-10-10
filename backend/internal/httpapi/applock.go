package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App lock: an optional 6-digit PIN per user, enforced server-side. Once a PIN
// exists, every /api request except /api/me and /api/applock/* must carry a
// valid unlock cookie, otherwise the API answers 423 Locked. The cookie is an
// HMAC-signed, sliding-expiry token bound to the user and the current PIN hash
// (changing or removing the PIN invalidates it). It is a cookie rather than a
// header because <video>/<img> requests cannot send custom headers.

const (
	unlockCookieName   = "rs_unlock"
	unlockHeader       = "X-Reelstash-Unlock"
	pinPBKDF2Iters     = 600_000
	maxPinAttempts     = 5
	pinLockoutDuration = 15 * time.Minute
	defaultIdleMinutes = 30
)

var allowedIdleMinutes = map[int]bool{5: true, 15: true, 30: true, 60: true, 120: true, 240: true}

// Random per process: a restart simply re-locks everyone.
var unlockSecret = func() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}()

type lockInfo struct {
	pinHash     string
	idleMinutes int
	// libraryIDs limits the lock to these libraries; nil locks the whole app.
	libraryIDs []int64
	fetchedAt  time.Time
}

// lockedLibrariesCtxKey carries the libraries a request may not open because
// their PIN lock is engaged (only set for a library-scoped lock).
const lockedLibrariesCtxKey ctxKey = "reelstash.lockedLibraries"

func lockedLibrariesFromContext(ctx context.Context) []int64 {
	ids, _ := ctx.Value(lockedLibrariesCtxKey).([]int64)
	return ids
}

var lockCache sync.Map // int64 userID -> lockInfo (nil-hash entry means "no PIN")

func invalidateLockCache(userID int64) { lockCache.Delete(userID) }

// loadLock returns the user's lock row; hasPin=false when none exists.
func (a *API) loadLock(ctx context.Context, userID int64) (info lockInfo, hasPin bool, err error) {
	if v, ok := lockCache.Load(userID); ok {
		c := v.(lockInfo)
		if time.Since(c.fetchedAt) < 15*time.Second {
			return c, c.pinHash != "", nil
		}
	}
	err = a.DB.QueryRow(ctx, `SELECT pin_hash, idle_minutes, library_ids FROM app_lock WHERE user_id = $1`, userID).
		Scan(&info.pinHash, &info.idleMinutes, &info.libraryIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
		info = lockInfo{}
	}
	if err != nil {
		return lockInfo{}, false, err
	}
	info.fetchedAt = time.Now()
	lockCache.Store(userID, info)
	return info, info.pinHash != "", nil
}

func hashPin(pin string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, pin, salt, pinPBKDF2Iters, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", pinPBKDF2Iters, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(dk)), nil
}

func checkPin(stored, pin string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pin, salt, iters, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validPin(pin string) bool {
	if len(pin) != 6 {
		return false
	}
	for _, c := range pin {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func signUnlock(userID int64, expiry int64, pinHash string) string {
	mac := hmac.New(sha256.New, unlockSecret)
	fmt.Fprintf(mac, "%d\x00%d\x00%s", userID, expiry, pinHash)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// unlockRemaining returns how long the request's unlock cookie stays valid
// (0 when missing, malformed, expired or forged).
func unlockRemaining(r *http.Request, userID int64, pinHash string) time.Duration {
	value := r.Header.Get(unlockHeader)
	if value == "" {
		c, err := r.Cookie(unlockCookieName)
		if err != nil {
			return 0
		}
		value = c.Value
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return 0
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0
	}
	if subtle.ConstantTimeCompare([]byte(signUnlock(userID, expiry, pinHash)), []byte(parts[1])) != 1 {
		return 0
	}
	left := time.Until(time.Unix(expiry, 0))
	if left < 0 {
		return 0
	}
	return left
}

// setUnlockCookie issues a fresh unlock token as a cookie for browsers, and
// also in the X-Reelstash-Unlock response header for apps, which send it back
// in the same request header. It returns the token.
func (a *API) setUnlockCookie(w http.ResponseWriter, r *http.Request, userID int64, pinHash string, ttl time.Duration) string {
	exp := time.Now().Add(ttl)
	value := fmt.Sprintf("%d.%s", exp.Unix(), signUnlock(userID, exp.Unix(), pinHash))
	w.Header().Set(unlockHeader, value)
	http.SetCookie(w, &http.Cookie{
		Name:     unlockCookieName,
		Value:    value,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
	return value
}

func (a *API) clearUnlockCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: unlockCookieName, Value: "", Path: "/", Expires: time.Unix(0, 0),
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode,
	})
}

// requireUnlocked runs after requireAuth. Users without a PIN pass straight
// through; users with one need a live unlock cookie, which slides forward
// (so active use, including video streaming, keeps the app unlocked).
func (a *API) requireUnlocked(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" || strings.HasPrefix(r.URL.Path, "/api/applock") {
			next.ServeHTTP(w, r)
			return
		}
		user, ok := UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		info, hasPin, err := a.loadLock(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check app lock")
			return
		}
		if hasPin {
			left := unlockRemaining(r, user.ID, info.pinHash)
			if left == 0 && info.libraryIDs != nil {
				// Only some libraries lock: the rest of the app stays open and
				// withLibraryScope treats those libraries as out of reach.
				ctx := context.WithValue(r.Context(), lockedLibrariesCtxKey, info.libraryIDs)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if left == 0 {
				writeError(w, http.StatusLocked, "app is locked")
				return
			}
			ttl := time.Duration(info.idleMinutes) * time.Minute
			if left < ttl/2 {
				a.setUnlockCookie(w, r, user.ID, info.pinHash, ttl)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) handleAppLockStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	info, hasPin, err := a.loadLock(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app lock")
		return
	}
	out := map[string]any{"hasPin": hasPin, "locked": false, "librariesLocked": false, "idleMinutes": defaultIdleMinutes, "scope": "app", "libraryIds": []int64{}}
	if hasPin {
		expired := unlockRemaining(r, user.ID, info.pinHash) == 0
		out["idleMinutes"] = info.idleMinutes
		if info.libraryIDs != nil {
			// A library lock never covers the whole app; the lock screen shows
			// when a locked library is opened (its requests answer 423).
			out["scope"] = "libraries"
			out["libraryIds"] = info.libraryIDs
			out["librariesLocked"] = expired
		} else {
			out["locked"] = expired
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// verifyAgainstStored checks pin with server-side throttling. On a wrong PIN
// it writes the error response itself and returns false.
func (a *API) verifyAgainstStored(w http.ResponseWriter, r *http.Request, userID int64, pin string) (pinHash string, idle int, ok bool) {
	var failed int
	var lockedUntil *time.Time
	err := a.DB.QueryRow(r.Context(), `SELECT pin_hash, idle_minutes, failed_attempts, locked_until FROM app_lock WHERE user_id = $1`, userID).
		Scan(&pinHash, &idle, &failed, &lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no PIN is set")
		return "", 0, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app lock")
		return "", 0, false
	}
	if lockedUntil != nil && time.Now().Before(*lockedUntil) {
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(*lockedUntil).Seconds())+1))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, try again in %d min", int(time.Until(*lockedUntil).Minutes())+1))
		return "", 0, false
	}
	if !validPin(pin) || !checkPin(pinHash, pin) {
		failed++
		if failed >= maxPinAttempts {
			_, _ = a.DB.Exec(r.Context(), `UPDATE app_lock SET failed_attempts = 0, locked_until = now() + $2::interval WHERE user_id = $1`, userID, fmt.Sprintf("%d seconds", int(pinLockoutDuration.Seconds())))
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, try again in %d min", int(pinLockoutDuration.Minutes())))
		} else {
			_, _ = a.DB.Exec(r.Context(), `UPDATE app_lock SET failed_attempts = $2 WHERE user_id = $1`, userID, failed)
			writeError(w, http.StatusUnauthorized, fmt.Sprintf("incorrect PIN, %d attempts remaining", maxPinAttempts-failed))
		}
		return "", 0, false
	}
	_, _ = a.DB.Exec(r.Context(), `UPDATE app_lock SET failed_attempts = 0, locked_until = NULL WHERE user_id = $1`, userID)
	return pinHash, idle, true
}

func (a *API) handleAppLockVerify(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var body struct {
		Pin string `json:"pin"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pinHash, idle, ok := a.verifyAgainstStored(w, r, user.ID, body.Pin)
	if !ok {
		return
	}
	token := a.setUnlockCookie(w, r, user.ID, pinHash, time.Duration(idle)*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "unlockToken": token})
}

func (a *API) handleAppLockLock(w http.ResponseWriter, r *http.Request) {
	a.clearUnlockCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"locked": true})
}

// handleAppLockSetPin sets a first PIN, or replaces one when currentPin is right.
func (a *API) handleAppLockSetPin(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var body struct {
		Pin        string `json:"pin"`
		CurrentPin string `json:"currentPin"`
	}
	if err := decodeJSON(r, &body); err != nil || !validPin(body.Pin) {
		writeError(w, http.StatusBadRequest, "PIN must be exactly 6 digits")
		return
	}
	_, hasPin, err := a.loadLock(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app lock")
		return
	}
	idle := defaultIdleMinutes
	if hasPin {
		var ok bool
		if _, idle, ok = a.verifyAgainstStored(w, r, user.ID, body.CurrentPin); !ok {
			return
		}
	}
	hashed, err := hashPin(body.Pin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash PIN")
		return
	}
	_, err = a.DB.Exec(r.Context(), `
		INSERT INTO app_lock (user_id, pin_hash, idle_minutes) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET pin_hash = EXCLUDED.pin_hash, failed_attempts = 0, locked_until = NULL, updated_at = now()`,
		user.ID, hashed, idle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save PIN")
		return
	}
	invalidateLockCache(user.ID)
	a.setUnlockCookie(w, r, user.ID, hashed, time.Duration(idle)*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"hasPin": true})
}

func (a *API) handleAppLockRemovePin(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var body struct {
		Pin string `json:"pin"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, _, ok := a.verifyAgainstStored(w, r, user.ID, body.Pin); !ok {
		return
	}
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM app_lock WHERE user_id = $1`, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove PIN")
		return
	}
	invalidateLockCache(user.ID)
	a.clearUnlockCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"hasPin": false})
}

// handleAppLockSettings changes the idle auto-lock window and what the lock
// covers (requires unlocked). scope "app" locks everything; "libraries" locks
// only libraryIds. Omitted fields keep their value.
func (a *API) handleAppLockSettings(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	var body struct {
		IdleMinutes int     `json:"idleMinutes"`
		Scope       string  `json:"scope"`
		LibraryIDs  []int64 `json:"libraryIds"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.IdleMinutes != 0 && !allowedIdleMinutes[body.IdleMinutes] {
		writeError(w, http.StatusBadRequest, "idleMinutes must be one of 5, 15, 30, 60, 120, 240")
		return
	}
	if body.Scope != "" && body.Scope != "app" && body.Scope != "libraries" {
		writeError(w, http.StatusBadRequest, `scope must be "app" or "libraries"`)
		return
	}
	if body.Scope == "libraries" && len(body.LibraryIDs) == 0 {
		writeError(w, http.StatusBadRequest, "choose at least one library to lock")
		return
	}
	info, hasPin, err := a.loadLock(r.Context(), user.ID)
	if err != nil || !hasPin {
		writeError(w, http.StatusBadRequest, "no PIN is set")
		return
	}
	if unlockRemaining(r, user.ID, info.pinHash) == 0 {
		writeError(w, http.StatusLocked, "app is locked")
		return
	}
	if body.IdleMinutes == 0 {
		body.IdleMinutes = info.idleMinutes
	}
	scopeSQL := `library_ids`
	args := []any{user.ID, body.IdleMinutes}
	switch body.Scope {
	case "app":
		scopeSQL = `NULL`
	case "libraries":
		args = append(args, body.LibraryIDs)
		scopeSQL = `$3::bigint[]`
	}
	if _, err := a.DB.Exec(r.Context(), `UPDATE app_lock SET idle_minutes = $2, library_ids = `+scopeSQL+`, updated_at = now() WHERE user_id = $1`, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	invalidateLockCache(user.ID)
	a.setUnlockCookie(w, r, user.ID, info.pinHash, time.Duration(body.IdleMinutes)*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"idleMinutes": body.IdleMinutes})
}

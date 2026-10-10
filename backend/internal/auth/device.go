package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Device sessions are web_session rows an app holds as a bearer token. They
// last longer than browser sessions and slide forward while the app is used,
// so a phone or TV stays signed in until someone revokes it.
const DeviceSessionTTL = 180 * 24 * time.Hour

// deviceTouchEvery bounds how often use of a device session is written back.
const deviceTouchEvery = time.Hour

// PairingTTL is how long a pairing code can be approved.
const PairingTTL = 10 * time.Minute

var (
	ErrPairingNotFound = errors.New("pairing code not found or expired")
	ErrPairingPending  = errors.New("pairing not approved yet")
)

// Device describes the app asking for a session.
type Device struct {
	Name     string
	Platform string
}

// Normalize trims the name and platform to sane, non-empty values.
func (d Device) Normalize() Device {
	d.Name = clip(strings.TrimSpace(d.Name), 80)
	if d.Name == "" {
		d.Name = "Unnamed device"
	}
	d.Platform = strings.ToLower(clip(strings.TrimSpace(d.Platform), 20))
	if d.Platform == "" {
		d.Platform = "unknown"
	}
	return d
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// CreateDeviceSession opens a long-lived session for an app and returns its token.
func CreateDeviceSession(ctx context.Context, db *pgxpool.Pool, userID int64, plugin string, d Device) (token string, expiresAt time.Time, err error) {
	d = d.Normalize()
	token, err = randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(DeviceSessionTTL)
	_, err = db.Exec(ctx, `
		INSERT INTO web_session (id, user_id, expires_at, auth_plugin, device_name, device_platform, last_seen_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, now())`,
		token, userID, expiresAt, plugin, d.Name, d.Platform)
	return token, expiresAt, err
}

// TouchDeviceSession records use of a device session and slides its expiry,
// writing at most once per deviceTouchEvery. Browser sessions are left alone.
func TouchDeviceSession(ctx context.Context, db *pgxpool.Pool, token string) {
	_, _ = db.Exec(ctx, `
		UPDATE web_session SET last_seen_at = now(), expires_at = now() + make_interval(secs => $2)
		WHERE id = $1 AND device_name IS NOT NULL
		  AND (last_seen_at IS NULL OR last_seen_at < now() - make_interval(secs => $3))`,
		token, DeviceSessionTTL.Seconds(), deviceTouchEvery.Seconds())
}

// DeviceSession is one signed-in app, as its owner sees it.
type DeviceSession struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	Current    bool       `json:"current"`
}

// ListDeviceSessions returns a user's live app sessions, newest use first.
// current is the token of the request, so the caller's own device is marked.
func ListDeviceSessions(ctx context.Context, db *pgxpool.Pool, userID int64, current string) ([]DeviceSession, error) {
	rows, err := db.Query(ctx, `
		SELECT public_id, device_name, COALESCE(device_platform, 'unknown'), created_at, last_seen_at, expires_at, id = $2
		FROM web_session
		WHERE user_id = $1 AND device_name IS NOT NULL AND expires_at > now()
		ORDER BY COALESCE(last_seen_at, created_at) DESC`, userID, current)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeviceSession, error) {
		var s DeviceSession
		err := row.Scan(&s.ID, &s.Name, &s.Platform, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.Current)
		return s, err
	})
}

// RevokeDeviceSession signs one of the user's apps out. It reports whether a
// session was removed.
func RevokeDeviceSession(ctx context.Context, db *pgxpool.Pool, userID, publicID int64) (bool, error) {
	tag, err := db.Exec(ctx, `DELETE FROM web_session WHERE user_id = $1 AND public_id = $2 AND device_name IS NOT NULL`, userID, publicID)
	return tag.RowsAffected() > 0, err
}

// userCodeAlphabet leaves out vowels (no accidental words) and look-alikes
// (0/O, 1/I/L) so a code read off a TV is easy to type.
const userCodeAlphabet = "BCDFGHJKMNPQRSTVWXZ23456789"

func newUserCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range raw {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(userCodeAlphabet[int(c)%len(userCodeAlphabet)])
	}
	return b.String(), nil
}

// NormalizeUserCode accepts a code as people type it: any case, with or
// without the dash or spaces.
func NormalizeUserCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(userCodeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

// Pairing is a started pairing: the secret the app polls with and the code
// the person enters.
type Pairing struct {
	DeviceCode string
	UserCode   string
	ExpiresAt  time.Time
}

// StartPairing registers a new pairing request for a device.
func StartPairing(ctx context.Context, db *pgxpool.Pool, d Device) (Pairing, error) {
	d = d.Normalize()
	_, _ = db.Exec(ctx, `DELETE FROM device_pairing WHERE expires_at < now()`)
	deviceCode, err := randomToken()
	if err != nil {
		return Pairing{}, err
	}
	p := Pairing{DeviceCode: deviceCode, ExpiresAt: time.Now().Add(PairingTTL)}
	for attempt := 0; attempt < 5; attempt++ {
		if p.UserCode, err = newUserCode(); err != nil {
			return Pairing{}, err
		}
		_, err = db.Exec(ctx, `
			INSERT INTO device_pairing (device_code, user_code, device_name, device_platform, expires_at)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (user_code) DO NOTHING`,
			p.DeviceCode, p.UserCode, d.Name, d.Platform, p.ExpiresAt)
		if err != nil {
			return Pairing{}, err
		}
		var ok bool
		_ = db.QueryRow(ctx, `SELECT true FROM device_pairing WHERE device_code = $1`, p.DeviceCode).Scan(&ok)
		if ok {
			return p, nil
		}
	}
	return Pairing{}, errors.New("could not allocate a pairing code")
}

// PendingPairing is what the approving person is shown before they confirm.
type PendingPairing struct {
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

// LookupPairing finds an unexpired, unapproved pairing by the code a person typed.
func LookupPairing(ctx context.Context, db *pgxpool.Pool, userCode string) (PendingPairing, error) {
	var p PendingPairing
	err := db.QueryRow(ctx, `
		SELECT device_name, device_platform FROM device_pairing
		WHERE user_code = $1 AND expires_at > now() AND approved_user_id IS NULL`,
		NormalizeUserCode(userCode)).Scan(&p.DeviceName, &p.Platform)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrPairingNotFound
	}
	return p, err
}

// ApprovePairing lets userID's account sign in the device that shows userCode.
func ApprovePairing(ctx context.Context, db *pgxpool.Pool, userCode string, userID int64) (PendingPairing, error) {
	var p PendingPairing
	err := db.QueryRow(ctx, `
		UPDATE device_pairing SET approved_user_id = $2
		WHERE user_code = $1 AND expires_at > now() AND approved_user_id IS NULL
		RETURNING device_name, device_platform`,
		NormalizeUserCode(userCode), userID).Scan(&p.DeviceName, &p.Platform)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrPairingNotFound
	}
	return p, err
}

// ClaimPairing is the app's poll. Once the pairing is approved it is consumed
// and a device session for the approving user is returned.
func ClaimPairing(ctx context.Context, db *pgxpool.Pool, deviceCode string) (token string, expiresAt time.Time, user User, err error) {
	var d Device
	var userID *int64
	err = db.QueryRow(ctx, `
		SELECT device_name, device_platform, approved_user_id FROM device_pairing
		WHERE device_code = $1 AND expires_at > now()`, deviceCode).Scan(&d.Name, &d.Platform, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, user, ErrPairingNotFound
	}
	if err != nil {
		return "", time.Time{}, user, err
	}
	if userID == nil {
		return "", time.Time{}, user, ErrPairingPending
	}
	// Consume first, so two concurrent polls cannot both get a session.
	tag, err := db.Exec(ctx, `DELETE FROM device_pairing WHERE device_code = $1`, deviceCode)
	if err != nil {
		return "", time.Time{}, user, err
	}
	if tag.RowsAffected() == 0 {
		return "", time.Time{}, user, ErrPairingNotFound
	}
	if err = db.QueryRow(ctx, `SELECT `+userColumns+` FROM app_user WHERE id = $1`, *userID).
		Scan(&user.ID, &user.Subject, &user.Email, &user.DisplayName, &user.AvatarURL, &user.Role, &user.Disabled); err != nil {
		return "", time.Time{}, user, err
	}
	token, expiresAt, err = CreateDeviceSession(ctx, db, user.ID, "", d)
	return token, expiresAt, user, err
}

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SessionTTL = 30 * 24 * time.Hour

const CookieName = "reelstash_session"

type User struct {
	ID          int64
	Subject     string
	Email       string
	DisplayName string
	AvatarURL   string
	Role        string
	Disabled    bool
}

// UpsertIdentityUser creates or updates the local app_user row for an identity
// a sign-in plugin verified. The plugin owns the credential; only profile
// fields and role are kept here, and an existing admin is never demoted.
func UpsertIdentityUser(ctx context.Context, db *pgxpool.Pool, claims Claims, role string) (User, error) {
	var u User
	err := db.QueryRow(ctx, `
		INSERT INTO app_user (auth_subject, email, display_name, avatar_url, role, last_login_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (auth_subject) DO UPDATE SET
			email = EXCLUDED.email,
			display_name = EXCLUDED.display_name,
			avatar_url = EXCLUDED.avatar_url,
			role = CASE WHEN app_user.role = 'ADMIN' THEN app_user.role ELSE EXCLUDED.role END,
			last_login_at = now(),
			updated_at = now()
		RETURNING id, auth_subject, COALESCE(email, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), role, disabled
	`, claims.Subject, claims.Email, claims.DisplayName, claims.AvatarURL, role).
		Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role, &u.Disabled)
	return u, err
}

// UpsertLocalAdmin creates or reuses the single emergency-local-auth admin
// identity. This path is only reachable when EMERGENCY_LOCAL_AUTH=true.
func UpsertLocalAdmin(ctx context.Context, db *pgxpool.Pool) (User, error) {
	var u User
	err := db.QueryRow(ctx, `
		INSERT INTO app_user (auth_subject, display_name, role, last_login_at)
		VALUES ('local-admin', 'Local Admin', 'ADMIN', now())
		ON CONFLICT (auth_subject) DO UPDATE SET last_login_at = now(), updated_at = now()
		RETURNING id, auth_subject, COALESCE(email, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), role, disabled
	`).Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role, &u.Disabled)
	return u, err
}

// CreateSession opens a session for a user. plugin names the sign-in plugin
// that authenticated them ("" for ReelStash's own accounts), so sign-out can
// send them to that provider's logout page.
func CreateSession(ctx context.Context, db *pgxpool.Pool, userID int64, plugin string) (id string, expiresAt time.Time, err error) {
	id, err = randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(SessionTTL)
	_, err = db.Exec(ctx, `INSERT INTO web_session (id, user_id, expires_at, auth_plugin) VALUES ($1, $2, $3, NULLIF($4, ''))`, id, userID, expiresAt, plugin)
	return id, expiresAt, err
}

func DeleteSession(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `DELETE FROM web_session WHERE id = $1`, id)
	return err
}

var ErrSessionNotFound = pgx.ErrNoRows

func LoadSessionUser(ctx context.Context, db *pgxpool.Pool, sessionID string) (User, error) {
	var u User
	err := db.QueryRow(ctx, `
		SELECT u.id, u.auth_subject, COALESCE(u.email, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''), u.role, u.disabled
		FROM web_session s
		JOIN app_user u ON u.id = s.user_id
		WHERE s.id = $1 AND s.expires_at > now()
	`, sessionID).Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role, &u.Disabled)
	return u, err
}

// Claims is a verified identity as ReelStash stores it: a stable subject plus
// profile fields. Sign-in plugins provide them.
type Claims struct {
	Subject     string
	Email       string
	DisplayName string
	AvatarURL   string
}

func randomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// SessionPlugin is the sign-in plugin that opened a session, or "" (password
// accounts, and sessions from before plugins were recorded).
func SessionPlugin(ctx context.Context, db *pgxpool.Pool, sessionID string) string {
	var plugin *string
	_ = db.QueryRow(ctx, `SELECT auth_plugin FROM web_session WHERE id = $1`, sessionID).Scan(&plugin)
	if plugin == nil {
		return ""
	}
	return *plugin
}

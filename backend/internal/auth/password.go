package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Native accounts sign in with a username and password that ReelStash keeps
// (hashed) in app_user. They exist so a self-hosted install works without any
// external identity provider.

const (
	hashScheme = "pbkdf2-sha256"
	// hashIterations follows OWASP's guidance for PBKDF2-HMAC-SHA256.
	hashIterations = 600000
	hashKeyLen     = 32
	saltLen        = 16

	MinPasswordLen = 8
	MaxPasswordLen = 256

	// LocalSubjectPrefix namespaces native accounts in app_user.auth_subject.
	LocalSubjectPrefix = "local:"
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{2,63}$`)

	ErrInvalidUsername = errors.New("the username must be 3-64 characters: letters, digits and . _ @ -")
	ErrWeakPassword    = fmt.Errorf("the password must be %d-%d characters", MinPasswordLen, MaxPasswordLen)
	ErrUsernameTaken   = errors.New("that username is already taken")
	ErrBadCredentials  = errors.New("invalid username or password")
)

// ValidateUsername reports whether name can be used.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return ErrInvalidUsername
	}
	return nil
}

// ValidatePassword reports whether password is acceptable.
func ValidatePassword(password string) error {
	if n := len([]rune(password)); n < MinPasswordLen || n > MaxPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword returns a salted hash: "pbkdf2-sha256$<iterations>$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return encodeHash(password, salt, hashIterations)
}

func encodeHash(password string, salt []byte, iterations int) (string, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, hashKeyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return strings.Join([]string{hashScheme, strconv.Itoa(iterations), enc.EncodeToString(salt), enc.EncodeToString(key)}, "$"), nil
}

// CheckPassword reports whether password matches a hash made by HashPassword.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > 10_000_000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// A hash nobody's password matches, checked when a username does not exist so
// that "no such user" and "wrong password" take the same time.
var decoyHash, _ = encodeHash("decoy", []byte("reelstash-decoy!"), hashIterations)

const userColumns = `id, auth_subject, COALESCE(email, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), role, disabled`

func scanUser(row pgx.Row, u *User) error {
	return row.Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role, &u.Disabled)
}

// CreateNativeUser adds a username/password account.
func CreateNativeUser(ctx context.Context, db *pgxpool.Pool, username, password, displayName, role string) (User, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	if role != "ADMIN" {
		role = "USER"
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = username
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	var u User
	err = scanUser(db.QueryRow(ctx, `
		INSERT INTO app_user (auth_subject, username, password_hash, display_name, role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+userColumns,
		LocalSubjectPrefix+strings.ToLower(username), username, hash, strings.TrimSpace(displayName), role), &u)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrUsernameTaken
	}
	return u, err
}

// AuthenticateNative checks a username and password. Unknown users, wrong
// passwords and accounts without a native password all give ErrBadCredentials.
// A disabled account is returned with Disabled set so the caller can say so.
func AuthenticateNative(ctx context.Context, db *pgxpool.Pool, username, password string) (User, error) {
	var u User
	var hash *string
	err := db.QueryRow(ctx, `SELECT `+userColumns+`, password_hash FROM app_user WHERE lower(username) = lower($1)`,
		strings.TrimSpace(username)).Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role, &u.Disabled, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	stored := decoyHash
	if err == nil && hash != nil {
		stored = *hash
	}
	if !CheckPassword(stored, password) || err != nil || hash == nil {
		return User{}, ErrBadCredentials
	}
	_, _ = db.Exec(ctx, `UPDATE app_user SET last_login_at = now() WHERE id = $1`, u.ID)
	return u, nil
}

// SetPassword replaces a native account's password and signs it out everywhere
// except the session named keep ("" signs out everywhere).
func SetPassword(ctx context.Context, db *pgxpool.Pool, userID int64, password, keepSession string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tag, err := db.Exec(ctx, `UPDATE app_user SET password_hash = $2, updated_at = now() WHERE id = $1 AND username IS NOT NULL`, userID, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotNative
	}
	_, err = db.Exec(ctx, `DELETE FROM web_session WHERE user_id = $1 AND id <> $2`, userID, keepSession)
	return err
}

// ErrNotNative is returned when a password is set on an account that signs in some other way.
var ErrNotNative = errors.New("this account does not sign in with a ReelStash password")

// PasswordHashFor returns the stored hash for a user, "" when it has none.
func PasswordHashFor(ctx context.Context, db *pgxpool.Pool, userID int64) string {
	var hash *string
	_ = db.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE id = $1`, userID).Scan(&hash)
	if hash == nil {
		return ""
	}
	return *hash
}

// NativeUserCount is how many accounts can sign in with a password.
func NativeUserCount(ctx context.Context, db *pgxpool.Pool) int {
	var n int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM app_user WHERE password_hash IS NOT NULL AND NOT disabled`).Scan(&n)
	return n
}

// AdminExists reports whether any admin account exists.
func AdminExists(ctx context.Context, db *pgxpool.Pool) bool {
	var n int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM app_user WHERE role = 'ADMIN'`).Scan(&n)
	return n > 0
}

// CreateFirstAdmin creates the first admin account, and only if no admin
// exists yet; the check and the insert are one transaction under a lock, so two
// visitors cannot both win. It returns ErrSetupDone otherwise.
func CreateFirstAdmin(ctx context.Context, db *pgxpool.Pool, username, password, displayName string) (User, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = username
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7283561)`); err != nil {
		return User{}, err
	}
	var admins int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM app_user WHERE role = 'ADMIN'`).Scan(&admins); err != nil {
		return User{}, err
	}
	if admins > 0 {
		return User{}, ErrSetupDone
	}
	var u User
	err = scanUser(tx.QueryRow(ctx, `
		INSERT INTO app_user (auth_subject, username, password_hash, display_name, role, last_login_at)
		VALUES ($1, $2, $3, $4, 'ADMIN', now())
		RETURNING `+userColumns,
		LocalSubjectPrefix+strings.ToLower(username), username, hash, strings.TrimSpace(displayName)), &u)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit(ctx)
}

// ErrSetupDone is returned by CreateFirstAdmin once an admin exists.
var ErrSetupDone = errors.New("setup is already done")

// EnsureAdmin creates an admin from the environment (REELSTASH_ADMIN_USERNAME /
// _PASSWORD) when that username does not exist yet. An existing account is
// never touched, so changing the password in the app sticks.
func EnsureAdmin(ctx context.Context, db *pgxpool.Pool, username, password string) (created bool, err error) {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE lower(username) = lower($1))`, username).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if _, err := CreateNativeUser(ctx, db, username, password, "", "ADMIN"); err != nil {
		return false, err
	}
	return true, nil
}

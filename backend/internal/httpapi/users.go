package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type appUser struct {
	ID          int64   `json:"id"`
	Username    *string `json:"username,omitempty"`
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
	Role        string  `json:"role"`
	Disabled    bool    `json:"disabled"`
	LastLoginAt *string `json:"lastLoginAt,omitempty"`
	LibraryIDs  []int64 `json:"libraryIds"`
	// PasswordLogin is true for accounts that sign in with a ReelStash password.
	PasswordLogin bool `json:"passwordLogin"`
}

const userColumns = `
	u.id, u.username, u.email, u.display_name, u.avatar_url, u.role, u.disabled,
	to_char(u.last_login_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	COALESCE((SELECT array_agg(library_id ORDER BY library_id) FROM library_access WHERE user_id = u.id), '{}'),
	(u.password_hash IS NOT NULL)`

func scanUser(row interface{ Scan(...any) error }) (appUser, error) {
	var u appUser
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role, &u.Disabled, &u.LastLoginAt, &u.LibraryIDs, &u.PasswordLogin)
	return u, err
}

// listUsers reports every locally-known identity that has ever signed in to ReelStash. Role/disabled
// here govern ReelStash access only. Without limit it returns everyone; with limit/offset it pages
// on the server and honours q (name, username, email), role (ADMIN|USER), status (active|never|disabled),
// method (password|sso), sort (name|role|login|libraries) and dir (asc|desc). The response always
// carries total (after filters) and counts (before filters) so the UI can show summaries.
func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	like := ""
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		like = likeContains(s)
	}
	order := map[string]string{
		"name":      `lower(COALESCE(NULLIF(u.display_name, ''), u.username, u.email, ''))`,
		"role":      `u.role`,
		"login":     `u.last_login_at`,
		"libraries": `(SELECT COUNT(*) FROM library_access WHERE user_id = u.id)`,
	}[q.Get("sort")]
	if order == "" {
		order = "u.last_login_at"
	}
	dir := "DESC NULLS LAST"
	if q.Get("dir") == "asc" {
		dir = "ASC NULLS LAST"
	}
	where := `
		WHERE ($1 = '' OR u.display_name ILIKE $1 ESCAPE '\' OR u.username ILIKE $1 ESCAPE '\' OR u.email ILIKE $1 ESCAPE '\')
		  AND ($2 = '' OR u.role = $2)
		  AND ($3 = '' OR ($3 = 'disabled' AND u.disabled) OR ($3 = 'never' AND NOT u.disabled AND u.last_login_at IS NULL) OR ($3 = 'active' AND NOT u.disabled AND u.last_login_at IS NOT NULL))
		  AND ($4 = '' OR ($4 = 'password' AND u.password_hash IS NOT NULL) OR ($4 = 'sso' AND u.password_hash IS NULL))`
	role := strings.ToUpper(q.Get("role"))
	args := []any{like, role, q.Get("status"), q.Get("method")}

	var total int64
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM app_user u`+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	sql := `SELECT ` + userColumns + ` FROM app_user u` + where + ` ORDER BY ` + order + ` ` + dir + `, u.id`
	if q.Get("limit") != "" {
		limit, offset := parseLimitOffset(r)
		sql += ` LIMIT $5 OFFSET $6`
		args = append(args, limit, offset)
	}
	rows, err := a.DB.Query(r.Context(), sql, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	defer rows.Close()
	items := []appUser{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read user")
			return
		}
		items = append(items, u)
	}
	rows.Close()

	var counts struct {
		Total, Admins, Disabled, Recent int64
	}
	_ = a.DB.QueryRow(r.Context(), `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE role = 'ADMIN'), COUNT(*) FILTER (WHERE disabled),
		       COUNT(*) FILTER (WHERE last_login_at > now() - interval '30 days')
		FROM app_user`).Scan(&counts.Total, &counts.Admins, &counts.Disabled, &counts.Recent)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "counts": map[string]int64{
		"total": counts.Total, "admins": counts.Admins, "disabled": counts.Disabled, "signedInRecently": counts.Recent,
	}})
}

// getUser returns one user.
func (a *API) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	u, err := scanUser(a.DB.QueryRow(r.Context(), `SELECT `+userColumns+` FROM app_user u WHERE u.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type updateUserRequest struct {
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

// lastEnabledAdmin reports whether id is the only enabled administrator.
func (a *API) lastEnabledAdmin(ctx context.Context, id int64) bool {
	var others int64
	_ = a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM app_user WHERE role = 'ADMIN' AND NOT disabled AND id <> $1`, id).Scan(&others)
	return others == 0
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil || (req.Role != "ADMIN" && req.Role != "USER") {
		writeError(w, http.StatusBadRequest, "role must be ADMIN or USER")
		return
	}
	actor, _ := UserFromContext(r.Context())
	if actor.ID == id && req.Role != "ADMIN" {
		writeError(w, http.StatusBadRequest, "you cannot demote your own account")
		return
	}
	if actor.ID == id && req.Disabled {
		writeError(w, http.StatusBadRequest, "you cannot disable your own account")
		return
	}
	var role string
	var disabled bool
	if err := a.DB.QueryRow(r.Context(), `SELECT role, disabled FROM app_user WHERE id = $1`, id).Scan(&role, &disabled); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if role == "ADMIN" && !disabled && (req.Role != "ADMIN" || req.Disabled) && a.lastEnabledAdmin(r.Context(), id) {
		writeError(w, http.StatusConflict, "this is the only enabled administrator; make someone else an administrator first")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `UPDATE app_user SET role = $2, disabled = $3, updated_at = now() WHERE id = $1`, id, req.Role, req.Disabled); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	noteAudit(r, "", map[string]any{"role": map[string]string{"from": role, "to": req.Role}, "disabled": map[string]bool{"from": disabled, "to": req.Disabled}})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// deleteUser removes an account and, by cascade, its sessions, history and library access.
func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	actor, _ := UserFromContext(r.Context())
	if actor.ID == id {
		writeError(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	var role string
	var disabled bool
	if err := a.DB.QueryRow(r.Context(), `SELECT role, disabled FROM app_user WHERE id = $1`, id).Scan(&role, &disabled); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if role == "ADMIN" && !disabled && a.lastEnabledAdmin(r.Context(), id) {
		writeError(w, http.StatusConflict, "this is the only enabled administrator; make someone else an administrator first")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM app_user WHERE id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

type sessionView struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"` // browser | device
	Device     string     `json:"device,omitempty"`
	Platform   string     `json:"platform,omitempty"`
	Plugin     string     `json:"plugin,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

// listUserSessions shows a user's live sessions by public id; session secrets are never exposed.
func (a *API) listUserSessions(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT public_id, device_name, device_platform, auth_plugin, created_at, expires_at, last_seen_at
		FROM web_session WHERE user_id = $1 AND expires_at > now() ORDER BY COALESCE(last_seen_at, created_at) DESC
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}
	defer rows.Close()
	items := []sessionView{}
	for rows.Next() {
		var v sessionView
		var device, platform, plugin *string
		if err := rows.Scan(&v.ID, &device, &platform, &plugin, &v.CreatedAt, &v.ExpiresAt, &v.LastSeenAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read session")
			return
		}
		v.Kind = "browser"
		if device != nil && *device != "" {
			v.Kind, v.Device = "device", *device
		}
		if platform != nil {
			v.Platform = *platform
		}
		if plugin != nil {
			v.Plugin = *plugin
		}
		items = append(items, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// revokeUserSessions signs a user out: every session, or one by its public id. Revoking your own
// current session is refused; use Sign out.
func (a *API) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	keep := ""
	if actor, ok := UserFromContext(r.Context()); ok && actor.ID == id {
		keep, _ = sessionToken(r)
	}
	var tag pgconn.CommandTag
	if raw := chi.URLParam(r, "sessionID"); raw != "" {
		sid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		tag, err = a.DB.Exec(r.Context(), `DELETE FROM web_session WHERE user_id = $1 AND public_id = $2 AND id <> $3`, id, sid, keep)
		if err == nil && tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "session not found, or it is the one you are using")
			return
		}
	} else {
		tag, err = a.DB.Exec(r.Context(), `DELETE FROM web_session WHERE user_id = $1 AND id <> $2`, id, keep)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign the user out")
		return
	}
	noteAudit(r, "", map[string]any{"revoked": tag.RowsAffected()})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": tag.RowsAffected()})
}

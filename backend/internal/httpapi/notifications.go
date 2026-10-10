package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reelstash/backend/internal/notify"
)

// listNotifications returns the newest notifications this user may see and
// how many are unread.
func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	isAdmin := user.Role == "ADMIN"
	limit, _ := parseLimitOffset(r)

	rows, err := a.DB.Query(r.Context(), `
		SELECT id, kind, title, COALESCE(body, ''), COALESCE(link, ''), created_at
		FROM notification WHERE ($1 OR NOT admin_only)
		ORDER BY id DESC LIMIT $2
	`, isAdmin, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notifications")
		return
	}
	defer rows.Close()
	items := []notify.Row{}
	for rows.Next() {
		var n notify.Row
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read notification")
			return
		}
		items = append(items, n)
	}
	rows.Close()

	var lastRead int64
	_ = a.DB.QueryRow(r.Context(), `SELECT last_read_id FROM notification_state WHERE user_id = $1`, user.ID).Scan(&lastRead)
	var unread int
	_ = a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM notification WHERE id > $1 AND ($2 OR NOT admin_only)`, lastRead, isAdmin).Scan(&unread)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

// markNotificationsRead marks everything up to now as read for this user.
func (a *API) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	_, err := a.DB.Exec(r.Context(), `
		INSERT INTO notification_state (user_id, last_read_id)
		VALUES ($1, COALESCE((SELECT MAX(id) FROM notification), 0))
		ON CONFLICT (user_id) DO UPDATE SET last_read_id = GREATEST(notification_state.last_read_id, EXCLUDED.last_read_id)
	`, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark notifications read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": 0})
}

type webhookView struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	HasSecret      bool       `json:"hasSecret"`
	Events         []string   `json:"events"`
	Enabled        bool       `json:"enabled"`
	LastStatus     *int32     `json:"lastStatus,omitempty"`
	LastError      *string    `json:"lastError,omitempty"`
	LastDeliveryAt *time.Time `json:"lastDeliveryAt,omitempty"`
}

const webhookColumns = `id, name, url, COALESCE(secret, '') <> '', events, enabled, last_status, last_error, last_delivery_at`

func scanWebhook(row pgx.Row) (webhookView, error) {
	var v webhookView
	err := row.Scan(&v.ID, &v.Name, &v.URL, &v.HasSecret, &v.Events, &v.Enabled, &v.LastStatus, &v.LastError, &v.LastDeliveryAt)
	if v.Events == nil {
		v.Events = []string{}
	}
	return v, err
}

func (a *API) listWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT `+webhookColumns+` FROM webhook ORDER BY id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load webhooks")
		return
	}
	defer rows.Close()
	items := []webhookView{}
	for rows.Next() {
		v, err := scanWebhook(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read webhook")
			return
		}
		items = append(items, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "kinds": notify.Kinds})
}

// cleanEvents validates event names; an empty list means "all events".
func cleanEvents(events []string) ([]string, bool) {
	seen := map[string]bool{}
	out := []string{}
	for _, e := range events {
		e = strings.TrimSpace(e)
		if !notify.KnownKind(e) {
			return nil, false
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, true
}

func defaultWebhookName(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return "Webhook"
}

func (a *API) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string   `json:"name"`
		URL     string   `json:"url"`
		Secret  string   `json:"secret"`
		Events  []string `json:"events"`
		Enabled *bool    `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if err := notify.ValidateURL(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	events, ok := cleanEvents(req.Events)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown event")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultWebhookName(req.URL)
	}
	enabled := req.Enabled == nil || *req.Enabled
	v, err := scanWebhook(a.DB.QueryRow(r.Context(), `
		INSERT INTO webhook (name, url, secret, events, enabled) VALUES ($1, $2, NULLIF($3, ''), $4, $5)
		RETURNING `+webhookColumns, name, req.URL, req.Secret, events, enabled))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save webhook")
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func webhookIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return 0, false
	}
	return id, true
}

// updateWebhook is a partial edit. A nil secret keeps the stored one; an
// empty string clears it.
func (a *API) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := webhookIDParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    *string   `json:"name"`
		URL     *string   `json:"url"`
		Secret  *string   `json:"secret"`
		Events  *[]string `json:"events"`
		Enabled *bool     `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL != nil {
		trimmed := strings.TrimSpace(*req.URL)
		if err := notify.ValidateURL(trimmed); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.URL = &trimmed
	}
	var events []string
	if req.Events != nil {
		cleaned, ok := cleanEvents(*req.Events)
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown event")
			return
		}
		events = cleaned
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		req.Name = &trimmed
	}
	v, err := scanWebhook(a.DB.QueryRow(r.Context(), `
		UPDATE webhook SET
			name = COALESCE($2, name),
			url = COALESCE($3, url),
			secret = CASE WHEN $4::text IS NULL THEN secret ELSE NULLIF($4, '') END,
			events = CASE WHEN $5::boolean THEN $6::text[] ELSE events END,
			enabled = COALESCE($7, enabled)
		WHERE id = $1
		RETURNING `+webhookColumns, id, req.Name, req.URL, req.Secret, req.Events != nil, events, req.Enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update webhook")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := webhookIDParam(w, r)
	if !ok {
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM webhook WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testWebhook sends a sample event and reports what the endpoint answered.
func (a *API) testWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := webhookIDParam(w, r)
	if !ok {
		return
	}
	if a.Notify == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications are not available")
		return
	}
	hook := notify.Webhook{ID: id}
	var secret *string
	err := a.DB.QueryRow(r.Context(), `SELECT name, url, secret, events FROM webhook WHERE id = $1`, id).Scan(&hook.Name, &hook.URL, &secret, &hook.Events)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load webhook")
		return
	}
	if secret != nil {
		hook.Secret = *secret
	}
	status, sendErr := a.Notify.Test(r.Context(), hook)
	resp := map[string]any{"ok": sendErr == nil, "status": status}
	if sendErr != nil {
		resp["error"] = sendErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// listWebhookDeliveries is the latest deliveries (up to 50) of one webhook, newest first.
func (a *API) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	var exists bool
	if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM webhook WHERE id = $1)`, id).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT id, at, kind, status, COALESCE(error, ''), duration_ms FROM webhook_delivery WHERE webhook_id = $1 ORDER BY id DESC LIMIT 50
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deliveries")
		return
	}
	defer rows.Close()
	type delivery struct {
		ID         int64     `json:"id"`
		At         time.Time `json:"at"`
		Kind       string    `json:"kind"`
		Status     *int32    `json:"status,omitempty"`
		Error      string    `json:"error,omitempty"`
		DurationMs int32     `json:"durationMs"`
	}
	items := []delivery{}
	for rows.Next() {
		var d delivery
		if err := rows.Scan(&d.ID, &d.At, &d.Kind, &d.Status, &d.Error, &d.DurationMs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read delivery")
			return
		}
		items = append(items, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

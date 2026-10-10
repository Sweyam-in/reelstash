package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// The admin audit trail: every successful admin change is recorded with who made it, what it was
// done to and, when the admin gave one, why. Reads are never recorded, so polling the console does
// not grow the log. The reason travels in the X-Admin-Reason header so every write can carry one.

const reasonHeader = "X-Admin-Reason"
const maxReasonLen = 500

type auditNoteKey struct{}

// auditNote is what a handler adds to its own audit event.
type auditNote struct {
	label  string
	detail map[string]any
}

// noteAudit lets a handler name the target and add detail to the event the middleware will record.
func noteAudit(r *http.Request, label string, detail map[string]any) {
	if n, ok := r.Context().Value(auditNoteKey{}).(*auditNote); ok {
		if label != "" {
			n.label = label
		}
		if len(detail) > 0 {
			if n.detail == nil {
				n.detail = map[string]any{}
			}
			for k, v := range detail {
				n.detail[k] = v
			}
		}
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// auditAdmin records every successful non-read admin request.
func (a *API) auditAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		note := &auditNote{}
		ctx := context.WithValue(r.Context(), auditNoteKey{}, note)
		// Name the target before the handler can delete it.
		kind, id, raw := auditTarget(r)
		label := a.auditLabel(r.Context(), kind, id, raw)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))
		if rec.status >= 400 {
			return
		}
		pattern := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			pattern = rc.RoutePattern()
		}
		if strings.HasSuffix(pattern, "/test") {
			return
		}
		if note.label != "" {
			label = note.label
		}
		actor, _ := UserFromContext(r.Context())
		name := actor.DisplayName
		if name == "" {
			name = actor.Email
		}
		reason := strings.TrimSpace(r.Header.Get(reasonHeader))
		if len([]rune(reason)) > maxReasonLen {
			reason = string([]rune(reason)[:maxReasonLen])
		}
		var detail []byte
		if len(note.detail) > 0 {
			detail, _ = json.Marshal(note.detail)
		}
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var targetID any
		if id != 0 {
			targetID = id
		}
		if _, err := a.DB.Exec(bg, `
			INSERT INTO admin_audit_event (actor_id, actor_name, action, method, route, target_kind, target_id, target_label, reason, detail)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, actor.ID, name, auditAction(r.Method, pattern), r.Method, pattern, kind, targetID, label, reason, detail); err != nil {
			log.Printf("audit: %v", err)
		}
	})
}

// auditAction turns "PUT /api/admin/users/{id}/password" into "users.password" and
// "DELETE /api/admin/users/{id}" into "users.delete".
func auditAction(method, pattern string) string {
	var parts []string
	lastParam := false
	for _, seg := range strings.Split(strings.TrimPrefix(pattern, "/api/admin/"), "/") {
		if seg == "" {
			continue
		}
		lastParam = strings.HasPrefix(seg, "{")
		if !lastParam {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return strings.ToLower(method)
	}
	verb := map[string]string{http.MethodPost: "create", http.MethodPut: "update", http.MethodDelete: "delete", http.MethodPatch: "update"}[method]
	// A resource itself (…/users, …/users/{id}) gets a verb; a sub-action (…/scan, …/password) names itself.
	if lastParam || len(parts) == 1 || method == http.MethodDelete {
		parts = append(parts, verb)
	}
	return strings.Join(parts, ".")
}

// auditTarget names what the request acts on: the section of the console and the id in the path. It reads
// the path itself because this middleware runs before the inner router has matched.
func auditTarget(r *http.Request) (kind string, id int64, raw string) {
	segs := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/"), "/"), "/")
	kind = segs[0]
	if len(segs) > 1 {
		raw = segs[1]
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			id = n
		}
	}
	return kind, id, raw
}

// auditLabel is a readable name for the target, looked up before a change can remove it.
func (a *API) auditLabel(ctx context.Context, kind string, id int64, raw string) string {
	if kind == "plugins" || (kind == "providers" && id == 0 && raw != "order" && raw != "external" && raw != "recover-links") {
		return raw
	}
	if id == 0 {
		return ""
	}
	q := map[string]string{
		"users":     `SELECT COALESCE(NULLIF(display_name, ''), NULLIF(email, ''), auth_subject) FROM app_user WHERE id = $1`,
		"libraries": `SELECT name FROM library WHERE id = $1`,
		"organizer": `SELECT name FROM library WHERE id = $1`,
		"scans":     `SELECT name FROM storage_location WHERE id = $1`,
		"storage":   `SELECT name FROM storage_location WHERE id = $1`,
		"webhooks":  `SELECT name FROM webhook WHERE id = $1`,
	}[kind]
	if q == "" {
		return ""
	}
	var label string
	_ = a.DB.QueryRow(ctx, q, id).Scan(&label)
	return label
}

type auditEventView struct {
	ID          int64           `json:"id"`
	At          time.Time       `json:"at"`
	ActorID     *int64          `json:"actorId,omitempty"`
	ActorName   string          `json:"actorName"`
	Action      string          `json:"action"`
	Method      string          `json:"method"`
	Route       string          `json:"route"`
	TargetKind  string          `json:"targetKind"`
	TargetID    *int64          `json:"targetId,omitempty"`
	TargetLabel string          `json:"targetLabel"`
	Reason      string          `json:"reason"`
	Detail      json.RawMessage `json:"detail,omitempty"`
}

// listAuditEvents pages the audit trail, newest first. Filters: q (action, actor, target or reason),
// actor (user id), action (prefix, e.g. "users."), targetKind, targetId, from/to (RFC 3339).
func (a *API) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := parseLimitOffset(r)
	actor, _ := strconv.ParseInt(q.Get("actor"), 10, 64)
	targetID, _ := strconv.ParseInt(q.Get("targetId"), 10, 64)
	var from, to any
	if t, err := time.Parse(time.RFC3339, q.Get("from")); err == nil {
		from = t
	}
	if t, err := time.Parse(time.RFC3339, q.Get("to")); err == nil {
		to = t
	}
	like := ""
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		like = likeContains(s)
	}
	const where = `
		WHERE ($1 = '' OR action ILIKE $1 ESCAPE '\' OR actor_name ILIKE $1 ESCAPE '\' OR target_label ILIKE $1 ESCAPE '\' OR reason ILIKE $1 ESCAPE '\')
		  AND ($2::bigint = 0 OR actor_id = $2)
		  AND ($3 = '' OR action LIKE $3 || '%')
		  AND ($4 = '' OR target_kind = $4)
		  AND ($5::bigint = 0 OR target_id = $5)
		  AND ($6::timestamptz IS NULL OR at >= $6)
		  AND ($7::timestamptz IS NULL OR at <= $7)`
	args := []any{like, actor, q.Get("action"), q.Get("targetKind"), targetID, from, to}
	var total int64
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM admin_audit_event`+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the audit log")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT id, at, actor_id, actor_name, action, method, route, target_kind, target_id, target_label, reason, detail
		FROM admin_audit_event`+where+` ORDER BY at DESC, id DESC LIMIT $8 OFFSET $9`, append(args, limit, offset)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the audit log")
		return
	}
	defer rows.Close()
	items := []auditEventView{}
	for rows.Next() {
		var e auditEventView
		if err := rows.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorName, &e.Action, &e.Method, &e.Route, &e.TargetKind, &e.TargetID, &e.TargetLabel, &e.Reason, &e.Detail); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read the audit log")
			return
		}
		items = append(items, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

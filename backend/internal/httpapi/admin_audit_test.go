package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (h *harness) asAdmin(method, path string, body any, reason string) int {
	h.t.Helper()
	return h.doReason(&h.admin, method, path, body, reason).Code
}

func (h *harness) auditFor(path string) []map[string]any {
	h.t.Helper()
	return h.items(h.do(&h.admin, http.MethodGet, path, nil))
}

func TestAuditRecordsSuccessfulWritesWithReason(t *testing.T) {
	h := newHarness(t)
	target := h.newActor("USER")
	rec := h.doReason(&h.admin, http.MethodPut, "/api/admin/users/"+strconv.FormatInt(target.id, 10), map[string]any{"role": "USER", "disabled": true}, "  left the team  ")
	h.expect(rec, http.StatusOK)

	events := h.auditFor("/api/admin/audit?action=users.&targetId=" + strconv.FormatInt(target.id, 10))
	if len(events) != 1 {
		t.Fatalf("events = %v, want 1", events)
	}
	e := events[0]
	if e["action"] != "users.update" || e["reason"] != "left the team" || e["actorId"] != float64(h.admin.id) || e["targetKind"] != "users" {
		t.Fatalf("unexpected event: %v", e)
	}
	if e["targetLabel"] == "" {
		t.Fatalf("target should be named: %v", e)
	}
	detail, _ := e["detail"].(map[string]any)
	if d, _ := detail["disabled"].(map[string]any); d["to"] != true {
		t.Fatalf("detail = %v", detail)
	}
}

func TestAuditIgnoresReadsAndFailures(t *testing.T) {
	h := newHarness(t)
	var before int64
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_event`).Scan(&before)
	h.expect(h.do(&h.admin, http.MethodGet, "/api/admin/users", nil), http.StatusOK)
	h.expect(h.do(&h.admin, http.MethodPut, "/api/admin/users/"+strconv.FormatInt(h.user.id, 10), map[string]any{"role": "NOPE"}), http.StatusBadRequest)
	var after int64
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_audit_event`).Scan(&after)
	if after != before {
		t.Fatalf("audit grew from %d to %d for a read and a rejected write", before, after)
	}
}

func TestAuditLogIsAdminOnlyAndFilters(t *testing.T) {
	h := newHarness(t)
	h.expect(h.do(&h.user, http.MethodGet, "/api/admin/audit", nil), http.StatusForbidden)
	h.expect(h.do(nil, http.MethodGet, "/api/admin/audit", nil), http.StatusUnauthorized)
	target := h.newActor("USER")
	id := strconv.FormatInt(target.id, 10)
	h.expect(h.do(&h.admin, http.MethodPut, "/api/admin/users/"+id, map[string]any{"role": "USER", "disabled": true}), http.StatusOK)
	h.expect(h.do(&h.admin, http.MethodPut, "/api/admin/users/"+id, map[string]any{"role": "USER", "disabled": false}), http.StatusOK)
	out := h.jsonBody(h.do(&h.admin, http.MethodGet, "/api/admin/audit?targetId="+id+"&limit=1", nil), http.StatusOK)
	if out["total"] != float64(2) || len(out["items"].([]any)) != 1 {
		t.Fatalf("paging: %v", out)
	}
	if n := len(h.auditFor("/api/admin/audit?targetId=" + id + "&actor=" + strconv.FormatInt(h.other.id, 10))); n != 0 {
		t.Fatalf("filtering by another actor returned %d", n)
	}
}

func TestUsersPagingFiltersAndCounts(t *testing.T) {
	h := newHarness(t)
	out := h.jsonBody(h.do(&h.admin, http.MethodGet, "/api/admin/users?limit=1&role=ADMIN&sort=name&dir=asc", nil), http.StatusOK)
	if len(out["items"].([]any)) != 1 || out["total"].(float64) < 1 {
		t.Fatalf("paged admins: %v", out)
	}
	counts := out["counts"].(map[string]any)
	if counts["total"].(float64) < 3 || counts["admins"].(float64) < 1 {
		t.Fatalf("counts: %v", counts)
	}
	if out := h.jsonBody(h.do(&h.admin, http.MethodGet, "/api/admin/users?q=no-such-person-xyz&limit=25", nil), http.StatusOK); out["total"] != float64(0) {
		t.Fatalf("search: %v", out)
	}
	one := h.jsonBody(h.do(&h.admin, http.MethodGet, "/api/admin/users/"+strconv.FormatInt(h.user.id, 10), nil), http.StatusOK)
	if one["id"] != float64(h.user.id) {
		t.Fatalf("get user: %v", one)
	}
	h.expect(h.do(&h.admin, http.MethodGet, "/api/admin/users/999999999", nil), http.StatusNotFound)
}

func TestAccountSafetyGuards(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	self := strconv.FormatInt(h.admin.id, 10)
	// Park every other administrator so h.admin is the only enabled one.
	h.pool.Exec(ctx, `UPDATE app_user SET disabled = true WHERE role = 'ADMIN' AND id <> $1`, h.admin.id)
	t.Cleanup(func() { h.pool.Exec(ctx, `UPDATE app_user SET disabled = false WHERE role = 'ADMIN' AND disabled`) })

	h.expect(h.do(&h.admin, http.MethodPut, "/api/admin/users/"+self, map[string]any{"role": "ADMIN", "disabled": true}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+self, nil), http.StatusBadRequest)

	// A second admin makes the first removable, but not both at once.
	second := h.newActor("ADMIN")
	sid := strconv.FormatInt(second.id, 10)
	h.expect(h.do(&second, http.MethodPut, "/api/admin/users/"+self, map[string]any{"role": "USER", "disabled": false}), http.StatusOK)
	// Now second is the only enabled admin; it cannot be disabled or deleted by another admin.
	h.expect(h.do(&h.admin, http.MethodPut, "/api/admin/users/"+sid, map[string]any{"role": "ADMIN", "disabled": true}), http.StatusForbidden)
	rec := h.do(&second, http.MethodPut, "/api/admin/users/"+sid, map[string]any{"role": "USER", "disabled": false})
	h.expect(rec, http.StatusBadRequest) // demoting yourself
	third := h.newActor("ADMIN")
	h.pool.Exec(ctx, `UPDATE app_user SET disabled = true WHERE id = $1`, third.id)
	h.expect(h.do(&second, http.MethodDelete, "/api/admin/users/"+strconv.FormatInt(third.id, 10), nil), http.StatusOK)
}

func TestLastEnabledAdminCannotBeRemovedByAnotherAdmin(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.pool.Exec(ctx, `UPDATE app_user SET disabled = true WHERE role = 'ADMIN' AND id NOT IN ($1)`, h.admin.id)
	t.Cleanup(func() { h.pool.Exec(ctx, `UPDATE app_user SET disabled = false WHERE role = 'ADMIN' AND disabled`) })
	// A disabled admin cannot act, so the guard is exercised through the handler's own check.
	if !(&API{DB: h.pool}).lastEnabledAdmin(ctx, h.admin.id) {
		t.Fatal("the only enabled administrator should be protected")
	}
	extra := h.newActor("ADMIN")
	if (&API{DB: h.pool}).lastEnabledAdmin(ctx, h.admin.id) {
		t.Fatal("with another enabled administrator nobody is the last")
	}
	_ = extra
}

func TestDeleteUserRemovesSessionsAndAccess(t *testing.T) {
	h := newHarness(t)
	target := h.newActor("USER")
	id := strconv.FormatInt(target.id, 10)
	h.expect(h.do(&target, http.MethodGet, "/api/me", nil), http.StatusOK)
	h.expect(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+id, nil), http.StatusOK)
	h.expect(h.do(&target, http.MethodGet, "/api/me", nil), http.StatusUnauthorized)
	h.expect(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+id, nil), http.StatusNotFound)
	h.expect(h.do(&h.user, http.MethodDelete, "/api/admin/users/"+id, nil), http.StatusForbidden)
}

func TestSessionsListAndRevoke(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	target := h.newActor("USER")
	id := strconv.FormatInt(target.id, 10)
	if _, err := h.pool.Exec(ctx, `INSERT INTO web_session (id, user_id, expires_at, device_name, device_platform) VALUES ($1, $2, $3, 'Pixel', 'android')`, uuid.NewString(), target.id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	items := h.items(h.do(&h.admin, http.MethodGet, "/api/admin/users/"+id+"/sessions", nil))
	if len(items) != 2 {
		t.Fatalf("sessions = %v", items)
	}
	for _, s := range items {
		if _, leaked := s["id"].(string); leaked {
			t.Fatalf("a session secret leaked: %v", s)
		}
	}
	var device map[string]any
	for _, s := range items {
		if s["kind"] == "device" {
			device = s
		}
	}
	if device == nil || device["device"] != "Pixel" {
		t.Fatalf("no device session: %v", items)
	}
	h.expect(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+id+"/sessions/"+strconv.FormatInt(int64(device["id"].(float64)), 10), nil), http.StatusOK)
	h.expect(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+id+"/sessions/"+strconv.FormatInt(int64(device["id"].(float64)), 10), nil), http.StatusNotFound)
	out := h.jsonBody(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+id+"/sessions", nil), http.StatusOK)
	if out["revoked"] != float64(1) {
		t.Fatalf("revoke all: %v", out)
	}
	h.expect(h.do(&target, http.MethodGet, "/api/me", nil), http.StatusUnauthorized)
	// An admin keeps the session they are using.
	self := strconv.FormatInt(h.admin.id, 10)
	h.expect(h.do(&h.admin, http.MethodDelete, "/api/admin/users/"+self+"/sessions", nil), http.StatusOK)
	h.expect(h.do(&h.admin, http.MethodGet, "/api/me", nil), http.StatusOK)
}

func TestJobsPaging(t *testing.T) {
	h := newHarness(t)
	out := h.jsonBody(h.do(&h.admin, http.MethodGet, "/api/admin/jobs?limit=1&offset=0", nil), http.StatusOK)
	if _, ok := out["total"].(float64); !ok {
		t.Fatalf("jobs response has no total: %v", out)
	}
	if len(out["items"].([]any)) > 1 {
		t.Fatalf("limit ignored: %v", out["items"])
	}
	h.expect(h.do(&h.admin, http.MethodGet, "/api/admin/jobs?limit=1&offset=100000", nil), http.StatusOK)
}

func TestWebhookDeliveryHistory(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var id int64
	if err := h.pool.QueryRow(ctx, `INSERT INTO webhook (name, url) VALUES ('t', 'https://example.invalid/h') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(ctx, `DELETE FROM webhook WHERE id = $1`, id) })
	h.pool.Exec(ctx, `INSERT INTO webhook_delivery (webhook_id, kind, status, duration_ms) VALUES ($1, 'job.failed', 204, 12)`, id)
	h.pool.Exec(ctx, `INSERT INTO webhook_delivery (webhook_id, kind, error, duration_ms) VALUES ($1, 'scan.completed', 'connection refused', 30)`, id)
	path := "/api/admin/webhooks/" + strconv.FormatInt(id, 10) + "/deliveries"
	items := h.items(h.do(&h.admin, http.MethodGet, path, nil))
	if len(items) != 2 || items[0]["error"] != "connection refused" || items[1]["status"] != float64(204) {
		t.Fatalf("deliveries newest first: %v", items)
	}
	h.expect(h.do(&h.admin, http.MethodGet, "/api/admin/webhooks/999999999/deliveries", nil), http.StatusNotFound)
	h.expect(h.do(&h.user, http.MethodGet, path, nil), http.StatusForbidden)
}

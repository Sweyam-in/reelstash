package httpapi

import (
	"context"
	"net/http"
	"testing"
)

func TestScheduleLifecycle(t *testing.T) {
	h := newHarness(t)
	lib := h.library(map[int64]string{h.storage(t.TempDir()): ""})

	id := h.createdID(&h.admin, "/api/admin/schedules", map[string]any{"libraryId": lib, "cron": "*/30 * * * *"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM scan_schedule WHERE id = $1`, id) })
	find := func() map[string]any {
		out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/automation", nil), http.StatusOK)
		for _, s := range out["schedules"].([]any) {
			if m := s.(map[string]any); int64(m["id"].(float64)) == id {
				return m
			}
		}
		t.Fatal("schedule missing from /automation")
		return nil
	}
	s := find()
	if s["cron"] != "*/30 * * * *" || s["enabled"] != true || s["nextRunAt"] == nil || s["jobKind"] != "SCAN" || int64(s["libraryId"].(float64)) != lib || s["libraryName"] == nil {
		t.Errorf("schedule = %v", s)
	}

	h.expect(h.do(&h.admin, "PUT", "/api/admin/schedules/"+itoa(id), map[string]any{"enabled": false}), http.StatusOK)
	if s := find(); s["enabled"] != false || s["nextRunAt"] != nil {
		t.Errorf("disabled schedule = %v", s)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/schedules/"+itoa(id), map[string]any{"cron": "0 3 * * *", "allLibraries": true}), http.StatusOK)
	if s := find(); s["cron"] != "0 3 * * *" || s["libraryId"] != nil {
		t.Errorf("edited schedule = %v", s)
	}

	for _, bad := range []map[string]any{
		{"cron": "every tuesday"},
		{"cron": "0 25 * * *"},
		{"cron": "0 3 * * *", "libraryId": 999999999},
		{"cron": "0 3 * * *", "jobKind": "FORMAT_DISK"},
		{},
	} {
		if rec := h.do(&h.admin, "POST", "/api/admin/schedules", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status = %d, want 400", bad, rec.Code)
		}
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/schedules/"+itoa(id), map[string]any{"cron": "nope"}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/schedules/999999999", map[string]any{"enabled": true}), http.StatusNotFound)

	h.expect(h.do(&h.admin, "DELETE", "/api/admin/schedules/"+itoa(id), nil), http.StatusOK)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/schedules/"+itoa(id), nil), http.StatusNotFound)
}

func TestDuplicateScanSchedulesCoverEverything(t *testing.T) {
	h := newHarness(t)
	lib := h.library(map[int64]string{h.storage(t.TempDir()): ""})
	id := h.createdID(&h.admin, "/api/admin/schedules", map[string]any{"jobKind": "DUPLICATE_SCAN", "libraryId": lib, "cron": "@weekly"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM scan_schedule WHERE id = $1`, id) })
	var got *int64
	h.pool.QueryRow(context.Background(), `SELECT library_id FROM scan_schedule WHERE id = $1`, id).Scan(&got)
	if got != nil {
		t.Errorf("a duplicate scan schedule should not be tied to a library, got %d", *got)
	}
}

func TestLibraryWatchToggleAndAutomationStatus(t *testing.T) {
	h := newHarness(t)
	lib := h.library(map[int64]string{h.storage(t.TempDir()): ""})
	watchState := func() (enabled bool, found bool) {
		out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/automation", nil), http.StatusOK)
		for _, l := range out["libraries"].([]any) {
			if m := l.(map[string]any); int64(m["id"].(float64)) == lib {
				return m["watchEnabled"].(bool), true
			}
		}
		return false, false
	}
	if on, ok := watchState(); !ok || on {
		t.Fatalf("a new library is listed with watching off, got on=%v found=%v", on, ok)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/libraries/"+itoa(lib)+"/watch", map[string]any{"enabled": true}), http.StatusOK)
	if on, _ := watchState(); !on {
		t.Error("watching should be on")
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/libraries/"+itoa(lib)+"/watch", map[string]any{}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/libraries/999999999/watch", map[string]any{"enabled": true}), http.StatusNotFound)
}

func TestAutomationIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.expect(h.do(&h.user, "GET", "/api/admin/automation", nil), http.StatusForbidden)
	h.expect(h.do(&h.user, "POST", "/api/admin/schedules", map[string]any{"cron": "@daily"}), http.StatusForbidden)
	h.expect(h.do(&h.user, "PUT", "/api/admin/libraries/1/watch", map[string]any{"enabled": true}), http.StatusForbidden)
}

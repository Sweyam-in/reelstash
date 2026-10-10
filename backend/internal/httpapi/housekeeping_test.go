package httpapi

import (
	"net/http"
	"testing"

	"reelstash/backend/internal/config"
)

func TestHousekeepingAPI(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{ThumbnailDir: t.TempDir()}, nil)

	out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/housekeeping", nil), http.StatusOK)
	tasks, _ := out["tasks"].([]any)
	if len(tasks) == 0 || out["settings"] == nil || out["active"] != nil {
		t.Fatalf("housekeeping = %v", out)
	}
	h.expect(h.do(&h.user, "GET", "/api/admin/housekeeping", nil), http.StatusForbidden)

	h.expect(h.do(&h.admin, "PUT", "/api/admin/housekeeping/settings", map[string]any{"jobDays": 0, "notificationDays": 90, "capacityDays": 365, "cacheDays": 30}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/housekeeping/settings", map[string]any{"jobDays": 7, "notificationDays": 90, "capacityDays": 365, "cacheDays": 14}), http.StatusOK)
	settings := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/housekeeping", nil), http.StatusOK)["settings"].(map[string]any)
	if settings["jobDays"] != 7.0 || settings["cacheDays"] != 14.0 {
		t.Errorf("settings = %v", settings)
	}

	h.expect(h.do(&h.admin, "POST", "/api/admin/housekeeping/run", map[string]any{"tasks": []string{"everything"}}), http.StatusBadRequest)

	// Housekeeping can be scheduled; it always covers everything.
	lib := h.library(map[int64]string{h.storage(t.TempDir()): ""})
	id := h.createdID(&h.admin, "/api/admin/schedules", map[string]any{"jobKind": "HOUSEKEEPING", "libraryId": lib, "cron": "0 4 * * 0"})
	for _, s := range h.jsonBody(h.do(&h.admin, "GET", "/api/admin/automation", nil), http.StatusOK)["schedules"].([]any) {
		if m := s.(map[string]any); int64(m["id"].(float64)) == id && (m["jobKind"] != "HOUSEKEEPING" || m["libraryId"] != nil) {
			t.Errorf("schedule = %v", m)
		}
	}
}

package httpapi

import (
	"net/http"
	"testing"
)

// The organizer previews and cleans a whole library, across every disk it
// has folders on.
func TestOrganizerPreviewSpansTheLibrarysDisks(t *testing.T) {
	h := newHarness(t)
	diskA, diskB := h.storage(t.TempDir()), h.storage(t.TempDir())
	lib := h.library(map[int64]string{diskA: "", diskB: "Movies"})
	h.addFile(h.newTitle(uniq("A")), diskA, "a.mkv")
	h.addFile(h.newTitle(uniq("B")), diskB, "Movies/b.mkv")
	h.addFile(h.newTitle(uniq("Outside")), diskB, "Other/c.mkv")

	out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/organizer/"+itoa(lib)+"/preview", nil), http.StatusOK)
	plan := out["plan"].(map[string]any)
	if plan["total"].(float64) != 2 {
		t.Fatalf("plan total = %v, want the 2 library files on both disks", plan["total"])
	}
	disks := map[float64]bool{}
	for _, m := range plan["moves"].([]any) {
		mv := m.(map[string]any)
		if mv["storageLocation"] == "" || mv["storageLocation"] == nil {
			t.Errorf("move without its disk: %v", mv)
		}
		disks[mv["storageLocationId"].(float64)] = true
	}
	if len(plan["moves"].([]any)) > 0 && len(disks) == 0 {
		t.Errorf("moves carry no disk ids: %v", plan["moves"])
	}

	h.expect(h.do(&h.admin, "GET", "/api/admin/organizer/"+itoa(lib)+"/cleanup", nil), http.StatusOK)
	h.expect(h.do(&h.admin, "GET", "/api/admin/organizer/999999999/preview", nil), http.StatusNotFound)

	empty := h.library(nil)
	h.expect(h.do(&h.admin, "POST", "/api/admin/organizer/"+itoa(empty)+"/run", nil), http.StatusBadRequest)
}

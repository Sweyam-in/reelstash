package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupStatusAndCompletion(t *testing.T) {
	h := newIsolatedHarness(t, configWithData(t), nil)
	st := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/setup", nil), 200)
	if st["completed"] != false || st["storageCount"].(float64) != 0 || st["libraryCount"].(float64) != 0 || st["defaultMode"] != "entertainment" {
		t.Errorf("fresh install = %v", st)
	}
	h.expect(h.do(&h.admin, "POST", "/api/admin/setup/complete", nil), 200)
	if st := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/setup", nil), 200); st["completed"] != true {
		t.Errorf("after complete = %v", st)
	}
}

func TestExistingInstallsCountAsSetUp(t *testing.T) {
	h := newIsolatedHarness(t, configWithData(t), nil)
	disk := h.storage(t.TempDir())
	_ = disk
	h.pool.Exec(context.Background(), `INSERT INTO library (name, mode) VALUES ('Existing', 'private')`)
	if st := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/setup", nil), 200); st["completed"] != true {
		t.Errorf("an install with a disk and a library must not be sent through the wizard: %v", st)
	}
}

func TestCheckPathCountsVideosAndExplainsProblems(t *testing.T) {
	h := newIsolatedHarness(t, configWithData(t), nil)
	root := t.TempDir()
	for _, f := range []string{"a.mkv", "Show/S01E01.MP4", "Show/notes.txt", ".hidden/x.mkv", "@eaDir/y.mkv", "b.jpg"} {
		full := filepath.Join(root, f)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	check := func(path string) map[string]any {
		return h.jsonBody(h.do(&h.admin, "POST", "/api/admin/setup/check-path", map[string]string{"path": path}), 200)
	}

	ok := check(root)
	if ok["exists"] != true || ok["isDir"] != true || ok["readable"] != true || ok["videoCount"].(float64) != 2 || ok["error"] != nil {
		t.Errorf("folder = %v, want 2 videos (hidden and system folders are skipped)", ok)
	}
	if samples := ok["samples"].([]any); len(samples) != 2 {
		t.Errorf("samples = %v", samples)
	}
	if empty := check(t.TempDir()); empty["readable"] != true || empty["videoCount"].(float64) != 0 {
		t.Errorf("empty folder = %v", empty)
	}
	if missing := check(filepath.Join(root, "nope")); missing["exists"] != false || missing["error"] == nil {
		t.Errorf("missing = %v", missing)
	}
	if file := check(filepath.Join(root, "a.mkv")); file["isDir"] != false || file["error"] == nil {
		t.Errorf("file = %v", file)
	}

	// A path that is already a disk is recognised.
	h.pool.Exec(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ('Mine', 'LOCAL', $1)`, root)
	if again := check(root); again["existingName"] != "Mine" || again["existingLocationId"] == nil {
		t.Errorf("existing disk not recognised: %v", again)
	}

	h.expect(h.do(&h.admin, "POST", "/api/admin/setup/check-path", map[string]string{"path": "relative/dir"}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "POST", "/api/admin/setup/check-path", map[string]string{"path": " "}), http.StatusBadRequest)
}

func TestSetupRoutesAreAdminOnly(t *testing.T) {
	h := newIsolatedHarness(t, configWithData(t), nil)
	h.expect(h.do(&h.user, "GET", "/api/admin/setup", nil), http.StatusForbidden)
	h.expect(h.do(&h.user, "POST", "/api/admin/setup/check-path", map[string]string{"path": "/"}), http.StatusForbidden)
	h.expect(h.do(&h.user, "POST", "/api/admin/setup/complete", nil), http.StatusForbidden)
}

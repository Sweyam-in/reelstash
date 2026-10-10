package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/config"
	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

func TestLibraryAdminLifecycle(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)
	var disk int64
	h.pool.QueryRow(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ('d', 'LOCAL', $1) RETURNING id`, t.TempDir()).Scan(&disk)

	for name, body := range map[string]map[string]any{
		"blank name":      {"name": "  "},
		"bad mode":        {"name": "X", "mode": "secret"},
		"dotdot path":     {"name": "X", "roots": []map[string]any{{"storageLocationId": disk, "path": "../etc"}}},
		"unknown storage": {"name": "X", "roots": []map[string]any{{"storageLocationId": 999999, "path": "a"}}},
	} {
		if rec := h.do(&h.admin, "POST", "/api/admin/libraries", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}

	lib := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/libraries", map[string]any{
		"name": "Films", "mode": "entertainment", "defaultAccess": false,
		"roots": []map[string]any{{"storageLocationId": disk, "path": " /movies/ "}},
	}), 200)
	id := int64(lib["id"].(float64))
	if lib["name"] != "Films" || lib["mode"] != "entertainment" {
		t.Errorf("created = %v", lib)
	}
	// Trimmed path stored.
	var path string
	h.pool.QueryRow(context.Background(), `SELECT path FROM library_root WHERE library_id = $1`, id).Scan(&path)
	if path != "movies" {
		t.Errorf("root path = %q, want movies", path)
	}
	// Default providers for an entertainment library.
	if n := count(t, h.pool, `SELECT COUNT(*) FROM library_provider WHERE library_id = $1 AND provider = 'tmdb'`, id); n != 1 {
		t.Errorf("default provider rows = %d", n)
	}

	// Names are unique, and a folder can belong to only one library.
	if rec := h.do(&h.admin, "POST", "/api/admin/libraries", map[string]any{"name": "Films"}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: %d, want 409", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/libraries", map[string]any{"name": "Films 2", "roots": []map[string]any{{"storageLocationId": disk, "path": "movies"}}}); rec.Code != http.StatusConflict {
		t.Errorf("same folder in two libraries: %d, want 409", rec.Code)
	}

	// A private library gets the default providers.
	priv := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/libraries", map[string]any{"name": "Vault", "mode": "private"}), 200)
	privID := int64(priv["id"].(float64))
	if n := count(t, h.pool, `SELECT COUNT(*) FROM library_provider WHERE library_id = $1`, privID); n != 1 {
		t.Errorf("private defaults = %d providers, want 1 (tmdb)", n)
	}

	// Updating with nil roots keeps them; with an empty list clears them.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/libraries/"+itoa(id), map[string]any{"name": "Films renamed", "mode": "entertainment"}), 200)
	if n := count(t, h.pool, `SELECT COUNT(*) FROM library_root WHERE library_id = $1`, id); n != 1 {
		t.Errorf("roots after a partial update = %d, want 1", n)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/libraries/"+itoa(id), map[string]any{"name": "Films renamed", "roots": []map[string]any{}}), 200)
	if n := count(t, h.pool, `SELECT COUNT(*) FROM library_root WHERE library_id = $1`, id); n != 0 {
		t.Errorf("roots after clearing = %d", n)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/libraries/999999999", map[string]any{"name": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("update missing: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/libraries/abc", map[string]any{"name": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("update bad id: %d", rec.Code)
	}

	// User access: set via the user endpoint, reflected in the user's own list.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.user.id)+"/libraries", map[string]any{"libraryIds": []int64{id}}), 200)
	mine := h.items(h.do(&h.user, "GET", "/api/libraries", nil))
	if !hasID(mine, id) || hasID(mine, privID) {
		t.Errorf("user libraries = %v, want only Films", mine)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.user.id)+"/libraries", map[string]any{"libraryIds": []int64{}}), 200)
	if mine = h.items(h.do(&h.user, "GET", "/api/libraries", nil)); hasID(mine, id) {
		t.Error("access should be revoked")
	}

	h.expect(h.do(&h.admin, "PUT", "/api/admin/libraries/order", map[string]any{"ids": []int64{privID, id}}), 200)
	var first, second int
	h.pool.QueryRow(context.Background(), `SELECT position FROM library WHERE id = $1`, privID).Scan(&first)
	h.pool.QueryRow(context.Background(), `SELECT position FROM library WHERE id = $1`, id).Scan(&second)
	if first != 0 || second != 1 {
		t.Errorf("positions = %d, %d", first, second)
	}

	h.expect(h.do(&h.admin, "DELETE", "/api/admin/libraries/"+itoa(id), nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/libraries/"+itoa(id), nil), http.StatusNotFound)
}

// Deleting a library leaves its titles indexed (unassigned, admin-visible).
func TestDeletingLibraryKeepsTitles(t *testing.T) {
	h := newIsolatedHarness(t, config.Config{}, nil)
	mount := t.TempDir()
	var disk, item int64
	ctx := context.Background()
	h.pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ('d', 'LOCAL', $1) RETURNING id`, mount).Scan(&disk)
	lib := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/libraries", map[string]any{"name": "L", "roots": []map[string]any{{"storageLocationId": disk, "path": "a"}}}), 200)
	libID := int64(lib["id"].(float64))
	h.pool.QueryRow(ctx, `INSERT INTO media_item (title) VALUES ('T') RETURNING id`).Scan(&item)
	h.pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size) VALUES ($1, $2, 'a/t.mp4', 't.mp4', 'mp4', 1)`, item, disk)

	var assigned *int64
	h.pool.QueryRow(ctx, `SELECT library_id FROM media_item WHERE id = $1`, item).Scan(&assigned)
	if assigned == nil || *assigned != libID {
		t.Fatalf("title should be assigned to the library by folder, got %v", assigned)
	}
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/libraries/"+itoa(libID), nil), http.StatusNoContent)
	h.pool.QueryRow(ctx, `SELECT library_id FROM media_item WHERE id = $1`, item).Scan(&assigned)
	if assigned != nil {
		t.Errorf("title should be unassigned, got library %d", *assigned)
	}
	h.expect(h.do(&h.admin, "GET", "/api/media/"+itoa(item), nil), 200)
	h.expect(h.do(&h.user, "GET", "/api/media/"+itoa(item), nil), http.StatusNotFound)
}

func jobsRig(t *testing.T) (*harness, *jobs.Runner) {
	t.Helper()
	pool := testdb.Isolated(t)
	runner := jobs.NewRunner(pool, 2)
	t.Cleanup(func() { runner.Shutdown(time.Second) })
	h := newHarnessOn(t, pool, config.Config{}, nil)
	h.handler = NewRouter(pool, h.cfg, runner, nil, nil, nil, nil)
	return h, runner
}

func waitJob(t *testing.T, pool *pgxpool.Pool, id int64, status string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var s string
		pool.QueryRow(context.Background(), `SELECT status FROM background_job WHERE id = $1`, id).Scan(&s)
		if s == status {
			return
		}
	}
	t.Fatalf("job %d never reached %s", id, status)
}

func TestBackgroundJobsAPI(t *testing.T) {
	h, runner := jobsRig(t)
	done := make(chan struct{})
	runner.Register("DUPLICATE_SCAN", func(ctx context.Context, _ *pgxpool.Pool, _ int64, _ json.RawMessage) (any, error) {
		<-done
		return map[string]int{"groups": 2}, nil
	})
	runner.Register("STUDIO_SYNC", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) {
		return nil, context.DeadlineExceeded
	})

	for name, body := range map[string]any{"no kind": map[string]any{}, "bad body": "{x"} {
		if rec := h.do(&h.admin, "POST", "/api/admin/jobs/run", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "FORMAT_DISK"}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown kind: %d, want 400", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "ORGANIZE"}); rec.Code != http.StatusBadRequest {
		t.Errorf("organize without location: %d, want 400", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "ORGANIZE", "storageLocationId": 999999}); rec.Code != http.StatusNotFound {
		t.Errorf("organize unknown location: %d, want 404", rec.Code)
	}

	out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "DUPLICATE_SCAN"}), http.StatusAccepted)
	id := int64(out["backgroundJobId"].(float64))
	waitJob(t, h.pool, id, "RUNNING")
	// The same job can't be queued twice.
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "DUPLICATE_SCAN"}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate queue: %d, want 409", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/"+itoa(id)+"/retry", nil); rec.Code != http.StatusConflict {
		t.Errorf("retry of an active job: %d, want 409", rec.Code)
	}

	list := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/jobs", nil), 200)
	if counts := list["counts"].(map[string]any); counts["RUNNING"] != float64(1) {
		t.Errorf("counts = %v", counts)
	}
	close(done)
	waitJob(t, h.pool, id, "COMPLETED")

	failed := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "STUDIO_SYNC"}), http.StatusAccepted)
	failedID := int64(failed["backgroundJobId"].(float64))
	waitJob(t, h.pool, failedID, "FAILED")

	// Filters.
	byStatus := h.items(h.do(&h.admin, "GET", "/api/admin/jobs?status=FAILED", nil))
	if len(byStatus) != 1 || byStatus[0]["kind"] != "STUDIO_SYNC" {
		t.Errorf("status filter = %v", byStatus)
	}
	byKind := h.items(h.do(&h.admin, "GET", "/api/admin/jobs?kind=DUPLICATE_SCAN", nil))
	if len(byKind) != 1 || byKind[0]["status"] != "COMPLETED" {
		t.Errorf("kind filter = %v", byKind)
	}

	// Single job and hidden housekeeping kinds.
	one := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/jobs/"+itoa(id), nil), 200)
	if one["kind"] != "DUPLICATE_SCAN" {
		t.Errorf("get job = %v", one)
	}
	h.pool.Exec(context.Background(), `INSERT INTO background_job (kind, status) VALUES ('THUMBNAIL', 'COMPLETED')`)
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/admin/jobs", nil)) {
		if it["kind"] == "THUMBNAIL" {
			t.Error("housekeeping jobs must be hidden from the default list")
		}
	}
	h.expect(h.do(&h.admin, "GET", "/api/admin/jobs/999999999", nil), http.StatusNotFound)

	// Retry a failed job queues a fresh one.
	retry := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/jobs/"+itoa(failedID)+"/retry", nil), http.StatusAccepted)
	if int64(retry["backgroundJobId"].(float64)) == failedID {
		t.Error("retry must create a new job")
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/999999999/retry", nil); rec.Code != http.StatusNotFound {
		t.Errorf("retry unknown: %d", rec.Code)
	}
	// Cancelling a finished job conflicts.
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/"+itoa(id)+"/cancel", nil); rec.Code != http.StatusConflict {
		t.Errorf("cancel finished: %d, want 409", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/999999999/cancel", nil); rec.Code != http.StatusNotFound {
		t.Errorf("cancel unknown: %d", rec.Code)
	}

	// Clearing history removes finished jobs but keeps active ones.
	time.Sleep(50 * time.Millisecond)
	cleared := h.jsonBody(h.do(&h.admin, "DELETE", "/api/admin/jobs/finished", nil), 200)
	if cleared["deleted"].(float64) < 2 {
		t.Errorf("deleted = %v", cleared["deleted"])
	}
	if n := count(t, h.pool, `SELECT COUNT(*) FROM background_job WHERE id = $1`, id); n != 0 {
		t.Error("finished job should be gone")
	}
	if n := count(t, h.pool, `SELECT COUNT(*) FROM background_job WHERE kind = 'THUMBNAIL'`); n != 1 {
		t.Error("hidden kinds are not cleared with the visible history")
	}
}

func TestJobsUnavailableWithoutRunner(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/run", map[string]any{"kind": "DUPLICATE_SCAN"}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("run: %d, want 503", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", "/api/admin/jobs/1/cancel", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("cancel: %d, want 503", rec.Code)
	}
}

func TestStreamServesRangesAndScopesAccess(t *testing.T) {
	f := newLibraryFixture(t)
	body := strings.Repeat("0123456789", 10)
	if err := os.WriteFile(filepath.Join(f.mount, "movies", "a.mp4"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f.pool.Exec(context.Background(), `UPDATE media_file SET file_size = $2 WHERE media_item_id = $1`, f.movieItem, len(body))
	path := "/api/media/" + itoa(f.movieItem) + "/stream"

	rec := f.get(t, f.userCookie, path)
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("full stream: %d len=%d", rec.Code, rec.Body.Len())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Errorf("Accept-Ranges = %q", rec.Header().Get("Accept-Ranges"))
	}

	rec = f.get(t, f.userCookie, path, "Range", "bytes=10-19")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != body[10:20] {
		t.Errorf("range: %d %q", rec.Code, rec.Body.String())
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 10-19/100" {
		t.Errorf("Content-Range = %q", cr)
	}
	rec = f.get(t, f.userCookie, path, "Range", "bytes=95-")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != body[95:] {
		t.Errorf("open-ended range: %d %q", rec.Code, rec.Body.String())
	}
	if rec = f.get(t, f.userCookie, path, "Range", "bytes=500-600"); rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("unsatisfiable range: %d, want 416", rec.Code)
	}

	// The other library's file is off limits to this user, but not the admin.
	priv := "/api/media/" + itoa(f.privItem) + "/stream"
	if rec = f.get(t, f.userCookie, priv); rec.Code != http.StatusNotFound {
		t.Errorf("stream outside library: %d, want 404", rec.Code)
	}
	if rec = f.get(t, f.adminCookie, priv); rec.Code != http.StatusOK {
		t.Errorf("admin stream: %d", rec.Code)
	}
	// A file that vanished from disk reports missing, not a server error.
	os.Remove(filepath.Join(f.mount, "movies", "a.mp4"))
	if rec = f.get(t, f.userCookie, path); rec.Code != http.StatusNotFound {
		t.Errorf("missing file: %d, want 404", rec.Code)
	}
}

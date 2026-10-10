package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reelstash/backend/internal/jobs"
)

// secondLocation adds another storage location (another "disk") with an
// incoming/ folder and returns its id and mount.
func (f fileRig) secondLocation() (int64, string) {
	f.t.Helper()
	mount := f.t.TempDir()
	os.MkdirAll(filepath.Join(mount, "incoming"), 0o755)
	var id int64
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, uniq("files2"), mount).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM media_file WHERE storage_location_id = $1`, id)
		f.pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, id)
	})
	return id, mount
}

func TestFileTransferJobEndToEnd(t *testing.T) {
	f := newFileRig(t)
	runner := jobs.NewRunner(f.pool, 1)
	runner.Register(FileTransferJobKind, FileTransferJob)
	t.Cleanup(func() { runner.Shutdown(5 * time.Second) })
	f.handler = NewRouter(f.pool, f.cfg, runner, nil, nil, nil, nil, WithPlugins(nil, f.authPlugins))
	loc2, other := f.secondLocation()
	fileID := f.track("movies/a.mp4")

	start := func(body map[string]any) (int, map[string]any) {
		rec := f.do(&f.admin, "POST", "/api/admin/files/transfer", body)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	for _, bad := range []map[string]any{
		{"mode": "move", "storageLocationId": f.loc, "dir": "", "items": []string{}, "destPath": "docs"},
		{"mode": "zap", "storageLocationId": f.loc, "dir": "", "items": []string{"root.txt"}, "destPath": "docs"},
		{"mode": "move", "storageLocationId": f.loc, "dir": "", "items": []string{"../x"}, "destPath": "docs"},
		{"mode": "move", "storageLocationId": f.loc, "dir": "", "items": []string{"nope"}, "destPath": "docs"},
		{"mode": "move", "storageLocationId": f.loc, "dir": "movies", "items": []string{"a.mp4"}, "destPath": "movies"},
		{"mode": "copy", "storageLocationId": f.loc, "dir": "", "items": []string{"root.txt"}, "destPath": "nowhere", "destStorageLocationId": loc2},
	} {
		if code, _ := start(bad); code < 400 || code >= 500 {
			t.Errorf("%v: %d, want 4xx", bad, code)
		}
	}
	if rec := f.do(&f.user, "POST", "/api/admin/files/transfer", map[string]any{"mode": "copy", "storageLocationId": f.loc, "items": []string{"root.txt"}, "destPath": "docs"}); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Errorf("non-admin: %d", rec.Code)
	}

	code, out := start(map[string]any{"mode": "move", "storageLocationId": f.loc, "dir": "", "items": []string{"movies", "root.txt"}, "destPath": "incoming", "destStorageLocationId": loc2})
	if code != http.StatusAccepted {
		t.Fatalf("start: %d %v", code, out)
	}
	jobID := int64(out["backgroundJobId"].(float64))
	var status string
	var result []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		f.pool.QueryRow(context.Background(), `SELECT status, COALESCE(result::text, '') FROM background_job WHERE id = $1`, jobID).Scan(&status, &result)
		if status == "COMPLETED" || status == "FAILED" {
			break
		}
	}
	if status != "COMPLETED" {
		t.Fatalf("job status = %s", status)
	}
	var res struct {
		Transferred int      `json:"transferred"`
		Landed      []string `json:"landed"`
	}
	json.Unmarshal(result, &res)
	if res.Transferred != 2 || len(res.Landed) != 2 {
		t.Errorf("result = %s", result)
	}
	if _, err := os.Stat(filepath.Join(other, "incoming", "movies", "a.mp4")); err != nil || f.exists("movies") || f.exists("root.txt") {
		t.Errorf("files not moved: %v", err)
	}
	var gotLoc int64
	var rel string
	f.pool.QueryRow(context.Background(), `SELECT storage_location_id, relative_path FROM media_file WHERE id = $1`, fileID).Scan(&gotLoc, &rel)
	if gotLoc != loc2 || rel != "incoming/movies/a.mp4" {
		t.Errorf("row = loc %d %q", gotLoc, rel)
	}
}

// A resumed job skips the items its payload already lists as done.
func TestFileTransferJobSkipsDoneItems(t *testing.T) {
	f := newFileRig(t)
	payload, _ := json.Marshal(fileTransferPayload{Mode: "copy", StorageLocationID: f.loc, Items: []string{"root.txt", "docs"}, DestPath: "movies", Done: map[string]string{"root.txt": "root.txt"}})
	out, err := FileTransferJob(context.Background(), f.pool, 0, payload)
	if err != nil {
		t.Fatal(err)
	}
	if f.exists("movies/root.txt") || !f.exists("movies/docs/note.txt") {
		t.Errorf("copied the wrong items")
	}
	if got := out.(map[string]any)["transferred"]; got != 2 {
		t.Errorf("transferred = %v", got)
	}
}

// A cancelled job stops and leaves no partial copy behind.
func TestFileTransferJobCancelled(t *testing.T) {
	f := newFileRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	payload, _ := json.Marshal(fileTransferPayload{Mode: "copy", StorageLocationID: f.loc, Items: []string{"movies"}, DestPath: "docs"})
	if _, err := FileTransferJob(ctx, f.pool, 0, payload); err == nil {
		t.Fatal("want an error from a cancelled job")
	}
	if f.exists("docs/movies") {
		t.Error("partial copy left behind")
	}
}

// Every item failing fails the job with the reasons.
func TestFileTransferJobAllFailed(t *testing.T) {
	f := newFileRig(t)
	payload, _ := json.Marshal(fileTransferPayload{Mode: "move", StorageLocationID: f.loc, Items: []string{"gone.mkv"}, DestPath: "docs"})
	if _, err := FileTransferJob(context.Background(), f.pool, 0, payload); err == nil {
		t.Fatal("want an error")
	}
}

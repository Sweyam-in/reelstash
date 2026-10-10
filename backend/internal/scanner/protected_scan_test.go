package scanner

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// TestScanSkipsLockedFolder: files in the downloads in-progress folder are
// never indexed, whatever the scan rules say, and an indexed file is left
// alone (not marked missing) once its folder becomes locked.
func TestScanSkipsLockedFolder(t *testing.T) {
	db := testdb.Isolated(t)
	ctx := context.Background()
	disk := t.TempDir()
	for _, rel := range []string{"Movies/a.mp4", "downloads/incomplete/half.mkv", "downloads/done.mp4"} {
		b := make([]byte, 2<<20)
		rand.Read(b)
		p := filepath.Join(disk, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var locID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ('Disk', 'LOCAL', $1) RETURNING id`, disk).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO protected_path (path) VALUES ($1)`, filepath.Join(disk, "downloads", "incomplete")); err != nil {
		t.Fatal(err)
	}
	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 2)
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}
	indexed := map[string]bool{}
	rows, err := db.Query(ctx, `SELECT relative_path, available FROM media_file WHERE storage_location_id = $1`, locID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var rel string
		var available bool
		rows.Scan(&rel, &available)
		indexed[rel] = available
	}
	rows.Close()
	if !indexed["Movies/a.mp4"] || !indexed["downloads/done.mp4"] {
		t.Fatalf("indexed = %v", indexed)
	}
	if _, ok := indexed["downloads/incomplete/half.mkv"]; ok {
		t.Fatalf("indexed a file in the locked folder: %v", indexed)
	}

	// Locking a folder later leaves what was already indexed there alone.
	if _, err := db.Exec(ctx, `INSERT INTO protected_path (path) VALUES ($1)`, filepath.Join(disk, "Movies")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}
	var available bool
	if err := db.QueryRow(ctx, `SELECT available FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/a.mp4'`, locID).Scan(&available); err != nil || !available {
		t.Fatalf("file under a newly locked folder: available=%v err=%v", available, err)
	}
}

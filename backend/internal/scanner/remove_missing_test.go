package scanner

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// TestRemoveMissingJob covers the cleanup of files a scan flagged missing:
// gone files and their empty titles are dropped, a title with another live
// copy keeps that copy, a file that came back is kept, and a location whose
// mount vanished is skipped.
func TestRemoveMissingJob(t *testing.T) {
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	if err := testdb.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	lib := t.TempDir()
	put := func(rel string) {
		t.Helper()
		b := make([]byte, 3<<20)
		rand.Read(b)
		p := filepath.Join(lib, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"keep.mp4", "gone.mp4", "back.mp4"} {
		put(f)
	}
	var locID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"RemoveMissing "+lib, lib).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM media_file WHERE storage_location_id = $1`, locID)
		db.Exec(ctx, `DELETE FROM scan_job WHERE storage_location_id = $1`, locID)
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, locID)
	})
	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 2)
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}

	os.Remove(filepath.Join(lib, "gone.mp4"))
	os.Remove(filepath.Join(lib, "back.mp4"))
	if sum, err := s.ScanStorageLocation(ctx, 0, locID); err != nil || sum.Missing != 2 {
		t.Fatalf("rescan: %+v, %v (want 2 missing)", sum, err)
	}
	put("back.mp4") // returns after the scan flagged it

	run := func() RemoveMissingResult {
		t.Helper()
		out, err := s.handleRemoveMissingJob(ctx, db, 0, []byte(`{"storageLocationId":`+strconv.FormatInt(locID, 10)+`}`))
		if err != nil {
			t.Fatal(err)
		}
		return out.(RemoveMissingResult)
	}
	count := func(rel string) (files, items int) {
		db.QueryRow(ctx, `SELECT count(*) FROM media_file WHERE storage_location_id = $1 AND relative_path = $2`, locID, rel).Scan(&files)
		db.QueryRow(ctx, `SELECT count(*) FROM media_item mi WHERE mi.title = $1 AND NOT EXISTS (SELECT 1 FROM media_file f WHERE f.media_item_id = mi.id AND f.storage_location_id <> $2)`, rel[:len(rel)-4], locID).Scan(&items)
		return
	}

	// An unreadable mount is skipped, and nothing is removed.
	os.Rename(lib, lib+".away")
	if res := run(); res.RemovedFiles != 0 || res.SkippedLocationsN == 0 {
		t.Fatalf("missing mount: %+v", res)
	}
	os.Rename(lib+".away", lib)

	res := run()
	if res.RemovedFiles != 1 || res.RemovedTitles != 1 || res.Kept != 1 {
		t.Fatalf("result = %+v, want 1 file, 1 title removed, 1 kept", res)
	}
	if f, i := count("gone.mp4"); f != 0 || i != 0 {
		t.Errorf("gone.mp4: %d files, %d titles left", f, i)
	}
	if f, i := count("back.mp4"); f != 1 || i != 1 {
		t.Errorf("back.mp4 (file returned) was removed: %d files, %d titles", f, i)
	}
	if f, i := count("keep.mp4"); f != 1 || i != 1 {
		t.Errorf("keep.mp4 touched: %d files, %d titles", f, i)
	}
}

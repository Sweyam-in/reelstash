package scanner

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// TestLibraryScan checks a library scan walks only the library's folders,
// skips a nested folder owned by another library, and never marks files
// outside the library missing.
//
// Requires a real Postgres reachable at REELSTASH_TEST_DATABASE_URL.
func TestLibraryScan(t *testing.T) {
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

	disk := t.TempDir()
	put := func(rel string) {
		t.Helper()
		b := make([]byte, 1<<20)
		rand.Read(b)
		p := filepath.Join(disk, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("Movies/m1.mp4")
	put("Movies/Kids/k1.mp4")
	put("Other/o1.mp4")

	var locID, moviesID, kidsID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"LibraryScanTest "+disk, disk).Scan(&locID); err != nil {
		t.Fatalf("insert storage_location: %v", err)
	}
	for name, id := range map[string]*int64{"Movies": &moviesID, "Kids": &kidsID} {
		if err := db.QueryRow(ctx, `INSERT INTO library (name) VALUES ($1) RETURNING id`, name+" "+disk).Scan(id); err != nil {
			t.Fatalf("insert library: %v", err)
		}
	}
	db.Exec(ctx, `INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $3, 'Movies'), ($2, $3, 'Movies/Kids')`, moviesID, kidsID, locID)
	t.Cleanup(func() {
		db.Exec(ctx, `
			WITH f AS (DELETE FROM media_file WHERE storage_location_id = $1 RETURNING media_item_id)
			DELETE FROM media_item WHERE id IN (SELECT media_item_id FROM f)`, locID)
		db.Exec(ctx, `DELETE FROM library WHERE id = ANY($1)`, []int64{moviesID, kidsID})
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, locID)
	})

	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 2)
	available := func() map[string]bool {
		t.Helper()
		rows, err := db.Query(ctx, `SELECT relative_path, available FROM media_file WHERE storage_location_id = $1`, locID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]bool{}
		for rows.Next() {
			var rel string
			var ok bool
			rows.Scan(&rel, &ok)
			got[rel] = ok
		}
		return got
	}

	sum, err := s.scan(ctx, 0, locID, moviesID)
	if err != nil || sum.Discovered != 1 {
		t.Fatalf("library scan: %+v err=%v", sum, err)
	}
	if got := available(); len(got) != 1 || !got["Movies/m1.mp4"] {
		t.Fatalf("library scan indexed %v, want only Movies/m1.mp4", got)
	}
	var scanLibrary *int64
	db.QueryRow(ctx, `SELECT library_id FROM scan_job WHERE storage_location_id = $1 ORDER BY id DESC LIMIT 1`, locID).Scan(&scanLibrary)
	if scanLibrary == nil || *scanLibrary != moviesID {
		t.Fatalf("scan_job.library_id = %v, want %d", scanLibrary, moviesID)
	}

	if sum, err = s.ScanStorageLocation(ctx, 0, locID); err != nil || sum.Discovered != 2 {
		t.Fatalf("disk scan: %+v err=%v", sum, err)
	}

	// Files outside the library vanish: a library scan leaves them alone.
	os.Remove(filepath.Join(disk, "Other", "o1.mp4"))
	os.Remove(filepath.Join(disk, "Movies", "Kids", "k1.mp4"))
	os.Remove(filepath.Join(disk, "Movies", "m1.mp4"))
	put("Movies/m2.mp4")
	var m1Item int64
	db.QueryRow(ctx, `SELECT media_item_id FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/m1.mp4'`, locID).Scan(&m1Item)
	if sum, err = s.scan(ctx, 0, locID, moviesID); err != nil || sum.Discovered != 1 || sum.Missing != 1 || sum.RemovedFiles != 0 || sum.RemovedTitles != 0 {
		t.Fatalf("library rescan: %+v err=%v", sum, err)
	}
	// The first time a library file disappears it is flagged, not deleted,
	// so a bad mount cannot take the title and its watch history with it.
	// Missing files of other libraries are left for their own scans.
	got := available()
	if avail, still := got["Movies/m1.mp4"]; !still || avail || !got["Movies/m2.mp4"] || !got["Other/o1.mp4"] || !got["Movies/Kids/k1.mp4"] {
		t.Fatalf("after library rescan: %v", got)
	}
	var titleLeft bool
	db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media_item WHERE id = $1)`, m1Item).Scan(&titleLeft)
	if !titleLeft {
		t.Error("the missing file's title was deleted on the first scan that missed it")
	}
	if sum, err = s.scan(ctx, 0, locID, moviesID); err != nil || sum.RemovedFiles != 1 || sum.RemovedTitles != 1 {
		t.Fatalf("confirming library rescan: %+v err=%v", sum, err)
	}
	got = available()
	if _, still := got["Movies/m1.mp4"]; still || !got["Movies/m2.mp4"] {
		t.Fatalf("after confirming rescan: %v", got)
	}
	db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media_item WHERE id = $1)`, m1Item).Scan(&titleLeft)
	if titleLeft {
		t.Error("the missing file's title was kept after a second scan still could not see it")
	}

	// A file a whole-disk scan only flagged missing is deleted by the next
	// library scan.
	if _, err = s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}
	if got := available(); got["Movies/Kids/k1.mp4"] {
		t.Fatalf("disk scan should flag k1 missing: %v", got)
	}
	if sum, err = s.scan(ctx, 0, locID, kidsID); err != nil || sum.RemovedFiles != 1 {
		t.Fatalf("kids library scan: %+v err=%v", sum, err)
	}
	if _, still := available()["Movies/Kids/k1.mp4"]; still {
		t.Fatal("kids library scan kept its flagged-missing file")
	}

	// The folder is still there but contains no videos. That is how a share
	// looks before it finishes mounting, so the scan must not wipe the index.
	if err := os.Remove(filepath.Join(disk, "Movies", "m2.mp4")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.scan(ctx, 0, locID, moviesID); err == nil {
		t.Fatal("scan of an empty library folder succeeded")
	}
	if !available()["Movies/m2.mp4"] {
		t.Fatal("empty library folder wiped its indexed file")
	}

	// A library folder that is gone fails the scan instead of emptying it.
	os.RemoveAll(filepath.Join(disk, "Movies"))
	if _, err = s.scan(ctx, 0, locID, moviesID); err == nil {
		t.Fatal("scan of a missing library folder succeeded")
	}
	if !available()["Movies/m2.mp4"] {
		t.Fatal("missing library folder marked its files missing")
	}
}

// TestLibraryScanDropsMissingCopy checks that a library scan which finds a
// title at a new path deletes the old index row when that file is gone, and
// leaves a second copy that is still on disk. A row whose disk looks
// unmounted (an empty mount directory) is kept.
func TestLibraryScanDropsMissingCopy(t *testing.T) {
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

	diskA, diskB := t.TempDir(), t.TempDir()
	offline := t.TempDir()
	body := make([]byte, 1<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(diskA, "Movies", "old.mp4"), body)
	write(filepath.Join(diskA, "Movies", "still.mp4"), body)

	var locA, locB, locOff, libraryID int64
	insertLoc := func(name, mount string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, name, mount).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	locA = insertLoc("DropCopyA "+diskA, diskA)
	locB = insertLoc("DropCopyB "+diskB, diskB)
	locOff = insertLoc("DropCopyOff "+offline, offline)
	if err := db.QueryRow(ctx, `INSERT INTO library (name) VALUES ($1) RETURNING id`, "DropCopy "+diskA).Scan(&libraryID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO library_root (library_id, storage_location_id, path)
		VALUES ($1, $2, 'Movies'), ($1, $3, 'Movies')
	`, libraryID, locA, locB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(ctx, `
			WITH f AS (DELETE FROM media_file WHERE storage_location_id = ANY($1) RETURNING media_item_id)
			DELETE FROM media_item WHERE id IN (SELECT media_item_id FROM f)`, []int64{locA, locB, locOff})
		db.Exec(ctx, `DELETE FROM library WHERE id = $1`, libraryID)
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = ANY($1)`, []int64{locA, locB, locOff})
	})

	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 1)
	if sum, err := s.scan(ctx, 0, locA, libraryID); err != nil || sum.Discovered != 2 {
		t.Fatalf("scan disk A: %+v err=%v", sum, err)
	}
	var itemID int64
	if err := db.QueryRow(ctx, `SELECT media_item_id FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/old.mp4'`, locA).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size, available)
		VALUES ($1, $2, 'Movies/ghost.mp4', 'ghost.mp4', 'mp4', $3, TRUE)
	`, itemID, locOff, len(body)); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(diskA, "Movies", "old.mp4")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(diskB, "Movies", "new.mp4"), body)

	sum, err := s.scan(ctx, 0, locB, libraryID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.RemovedFiles != 1 {
		t.Fatalf("removed files = %d, want 1 (the missing old path)", sum.RemovedFiles)
	}

	var oldLeft, stillLeft, ghostLeft, newItem int64
	db.QueryRow(ctx, `SELECT COUNT(*) FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/old.mp4'`, locA).Scan(&oldLeft)
	db.QueryRow(ctx, `SELECT COUNT(*) FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/still.mp4'`, locA).Scan(&stillLeft)
	db.QueryRow(ctx, `SELECT COUNT(*) FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/ghost.mp4'`, locOff).Scan(&ghostLeft)
	db.QueryRow(ctx, `SELECT media_item_id FROM media_file WHERE storage_location_id = $1 AND relative_path = 'Movies/new.mp4'`, locB).Scan(&newItem)
	if oldLeft != 0 || stillLeft != 1 || ghostLeft != 1 || newItem != itemID {
		t.Fatalf("old=%d still=%d ghost=%d newItem=%d want old=0 still=1 ghost=1 item=%d", oldLeft, stillLeft, ghostLeft, newItem, itemID)
	}
	var titleLeft bool
	db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media_item WHERE id = $1)`, itemID).Scan(&titleLeft)
	if !titleLeft {
		t.Fatal("the title was deleted with the old path")
	}
}

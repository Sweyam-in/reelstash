package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// TestScanRecordsDVD checks a scan notices VIDEO_TS folders, ignores menus and
// loose VOB files, does not index them as library videos, and does not queue
// a conversion. A later scan drops a disc that was removed, and a library
// scan of another folder leaves discs outside that folder alone.
func TestScanRecordsDVD(t *testing.T) {
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

	root := t.TempDir()
	write := func(rel string, body []byte) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("DADDY_RIDE_ALONG/VIDEO_TS/VIDEO_TS.VOB", []byte("menu"))
	write("DADDY_RIDE_ALONG/VIDEO_TS/VTS_01_0.VOB", []byte("menu"))
	write("DADDY_RIDE_ALONG/VIDEO_TS/VTS_01_1.VOB", []byte("title"))
	write("OTHER/VIDEO_TS/VTS_03_1.VOB", []byte("aaaa"))
	write("OTHER/VIDEO_TS/VTS_04_1.VOB", []byte("bbbbbbbb"))
	write("MENU/VIDEO_TS/VTS_01_0.VOB", []byte("menu"))
	write("loose/VTS_01_1.VOB", []byte("nope"))
	if err := os.MkdirAll(filepath.Join(root, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}

	var locID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"DVDScan "+root, root).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(context.Background(), `DELETE FROM scan_job WHERE storage_location_id = $1`, locID)
		db.Exec(context.Background(), `DELETE FROM background_job WHERE payload->>'storageLocationId' = $1`, strconv.FormatInt(locID, 10))
		db.Exec(context.Background(), `DELETE FROM library_root WHERE storage_location_id = $1`, locID)
		db.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, locID)
	})

	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 2)
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}

	type disc struct {
		path   string
		titles int
		size   int64
	}
	load := func() map[string]disc {
		t.Helper()
		rows, err := db.Query(ctx, `SELECT relative_path, title_count, file_size FROM dvd_disc WHERE storage_location_id = $1`, locID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]disc{}
		for rows.Next() {
			var d disc
			if err := rows.Scan(&d.path, &d.titles, &d.size); err != nil {
				t.Fatal(err)
			}
			got[d.path] = d
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := load()
	if len(got) != 2 {
		t.Fatalf("discs = %+v", got)
	}
	if d := got["DADDY_RIDE_ALONG/VIDEO_TS"]; d.titles != 1 || d.size != 5 {
		t.Fatalf("daddy = %+v", d)
	}
	if d := got["OTHER/VIDEO_TS"]; d.titles != 2 || d.size != 12 {
		t.Fatalf("other = %+v", d)
	}
	var files int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM media_file WHERE storage_location_id = $1`, locID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("VOB files were indexed as videos: %d", files)
	}
	var queued bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM background_job WHERE kind = 'DVD_CONVERT' AND payload::text LIKE $1)`, "%DADDY_RIDE_ALONG/VIDEO_TS%").Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Fatal("scan queued a DVD conversion")
	}

	var libraryID int64
	if err := db.QueryRow(ctx, `INSERT INTO library (name) VALUES ($1) RETURNING id`, "Elsewhere "+root).Scan(&libraryID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(context.Background(), `DELETE FROM library WHERE id = $1`, libraryID)
	})
	if _, err := db.Exec(ctx, `INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $2, 'elsewhere')`, libraryID, locID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.scan(ctx, 0, locID, libraryID); err != nil {
		t.Fatal(err)
	}
	if len(load()) != 2 {
		t.Fatalf("library scan of another folder dropped discs: %+v", load())
	}

	if err := os.RemoveAll(filepath.Join(root, "OTHER")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}
	got = load()
	if len(got) != 1 || got["DADDY_RIDE_ALONG/VIDEO_TS"].titles != 1 {
		t.Fatalf("after removal = %+v", got)
	}
}

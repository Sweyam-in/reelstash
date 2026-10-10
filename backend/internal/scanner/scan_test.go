package scanner

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// TestScanStorageLocation covers the incremental scan end to end: NFO uid
// stubs, no-op rescans, in-place edits, move+remux, unreadable folders, junk
// folders, an unmounted (empty) disk and a rebuilt database.
//
// Requires a real Postgres reachable at REELSTASH_TEST_DATABASE_URL.
func TestScanStorageLocation(t *testing.T) {
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
	put := func(rel string, n int) {
		t.Helper()
		b := make([]byte, n)
		rand.Read(b)
		p := filepath.Join(lib, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("a.mp4", 3<<20)
	put("b.mp4", 3<<20)
	put("Studio/c.mkv", 3<<20)
	put("Locked/d.mp4", 3<<20)
	put(".Trash-1000/files/deleted.mp4", 1<<20)
	put("@eaDir/a.mp4/SYNOVIDEO.mp4", 1<<20)
	os.WriteFile(filepath.Join(lib, "b.nfo"), []byte("<?xml version=\"1.0\"?>\n<movie>\n  <title>Bee</title>\n  <extra>keep</extra>\n</movie>\n"), 0o644)

	var locID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"ScanTest "+lib, lib).Scan(&locID); err != nil {
		t.Fatalf("insert storage_location: %v", err)
	}
	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 4)
	scan := func() (ScanSummary, error) { return s.ScanStorageLocation(ctx, 0, locID) }
	type row struct {
		itemID    int64
		uid, src  string
		available bool
	}
	get := func(rel string) row {
		t.Helper()
		var r row
		if err := db.QueryRow(ctx, `
			SELECT mi.id, mi.uid::text, mi.metadata_source, mf.available
			FROM media_file mf JOIN media_item mi ON mi.id = mf.media_item_id
			WHERE mf.storage_location_id = $1 AND mf.relative_path = $2`, locID, rel).Scan(&r.itemID, &r.uid, &r.src, &r.available); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return r
	}
	count := func() int {
		var n int
		db.QueryRow(ctx, `SELECT count(*) FROM media_file WHERE storage_location_id = $1`, locID).Scan(&n)
		return n
	}

	// First scan: 4 videos, junk folders skipped, NFOs carry the uids.
	sum, err := scan()
	if err != nil || sum.Discovered != 4 || count() != 4 {
		t.Fatalf("first scan: %+v err=%v rows=%d", sum, err, count())
	}
	a, b := get("a.mp4"), get("b.mp4")
	if NFOUID(filepath.Join(lib, "a.nfo")) != a.uid || a.src != "FILENAME" {
		t.Fatalf("a: stub uid %q vs %q, src %s", NFOUID(filepath.Join(lib, "a.nfo")), a.uid, a.src)
	}
	bNFO, _ := os.ReadFile(filepath.Join(lib, "b.nfo"))
	if NFOUID(filepath.Join(lib, "b.nfo")) != b.uid || !strings.Contains(string(bNFO), "<extra>keep</extra>") || b.src != "NFO" {
		t.Fatalf("b nfo: %s (src %s)", bNFO, b.src)
	}

	// No-op rescan: nothing re-processed, no NFO rewritten.
	before, _ := os.Stat(filepath.Join(lib, "b.nfo"))
	if sum, err = scan(); err != nil || sum.Discovered != 0 || sum.Changed != 0 || sum.Missing != 0 {
		t.Fatalf("rescan: %+v err=%v", sum, err)
	}
	if after, _ := os.Stat(filepath.Join(lib, "b.nfo")); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("no-op rescan rewrote b.nfo")
	}

	// In-place edit with its NFO gone: still the same title.
	os.Remove(filepath.Join(lib, "Studio", "c.nfo"))
	cItem := get("Studio/c.mkv").itemID
	put("Studio/c.mkv", 3<<20+7)
	// Move + remux a, carrying its NFO. Lock a folder. Delete b's NFO.
	os.Remove(filepath.Join(lib, "a.mp4"))
	put("Moved/renamed.mkv", 4<<20)
	os.Rename(filepath.Join(lib, "a.nfo"), filepath.Join(lib, "Moved", "renamed.nfo"))
	os.Remove(filepath.Join(lib, "b.nfo"))
	if err := os.Chmod(filepath.Join(lib, "Locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(lib, "Locked"), 0o755)

	sum, err = scan()
	if err != nil {
		t.Fatal(err)
	}
	if sum.Missing != 0 {
		t.Fatalf("unreadable folder or moved file counted missing: %+v", sum)
	}
	if got := get("Studio/c.mkv").itemID; got != cItem {
		t.Fatalf("in-place edit moved c to item %d, want %d", got, cItem)
	}
	if r := get("Moved/renamed.mkv"); r.itemID != a.itemID || r.uid != a.uid {
		t.Fatalf("move+remux got item %d, want %d", r.itemID, a.itemID)
	}
	if !get("Locked/d.mp4").available {
		t.Fatal("file in unreadable folder marked unavailable")
	}
	if count() != 4 {
		t.Fatalf("stale row left behind: %d rows", count())
	}
	if NFOUID(filepath.Join(lib, "b.nfo")) != b.uid {
		t.Fatal("deleted b.nfo not restored")
	}
	os.Chmod(filepath.Join(lib, "Locked"), 0o755)

	// Unmounted disk (empty mountpoint): the scan fails, nothing goes missing.
	hidden := lib + ".away"
	if err := os.Rename(lib, hidden); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(lib, 0o755)
	if _, err := scan(); err == nil {
		t.Fatal("scan of an empty mountpoint succeeded")
	}
	var unavailable int
	db.QueryRow(ctx, `SELECT count(*) FROM media_file WHERE storage_location_id = $1 AND NOT available`, locID).Scan(&unavailable)
	if unavailable != 0 {
		t.Fatalf("%d files marked missing on an empty mountpoint", unavailable)
	}
	os.Remove(lib)
	if err := os.Rename(hidden, lib); err != nil {
		t.Fatal(err)
	}

	// Rebuilt database: the NFOs give every title its old uid back.
	uids := map[string]string{}
	for _, rel := range []string{"b.mp4", "Studio/c.mkv", "Moved/renamed.mkv"} {
		uids[rel] = get(rel).uid
	}
	db.Exec(ctx, `
		WITH f AS (DELETE FROM media_file WHERE storage_location_id = $1 RETURNING media_item_id)
		DELETE FROM media_item WHERE id IN (SELECT media_item_id FROM f)`, locID)
	if _, err := scan(); err != nil {
		t.Fatal(err)
	}
	for rel, uid := range uids {
		if got := get(rel).uid; got != uid {
			t.Fatalf("%s: uid %s after rebuild, want %s", rel, got, uid)
		}
	}
}

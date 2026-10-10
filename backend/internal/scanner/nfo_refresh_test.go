package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/testdb"
)

// A bare stub has no nfo_metadata row (so the auto-matcher still treats the
// title as unnamed); once an edit fills it in, RefreshNFORecord records it.
func TestRefreshNFORecordCreatesRowOnlyOnceStubHasMetadata(t *testing.T) {
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := testdb.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	var locID, itemID, fileID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, "Refresh "+dir, dir).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO media_item (title, sort_title, metadata_source) VALUES ('x', 'x', 'FILENAME') RETURNING id`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `
		INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
		VALUES ($1, $2, 'x.mp4', 'x.mp4', 'mp4', 1) RETURNING id`, itemID, locID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM media_item WHERE id = $1`, itemID)
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, locID)
	})

	nfo := filepath.Join(dir, "x.nfo")
	if err := writeStubNFO(nfo, testUID); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		db.QueryRow(ctx, `SELECT COUNT(*) FROM nfo_metadata WHERE media_file_id = $1`, fileID).Scan(&n)
		return n
	}

	if err := RefreshNFORecord(ctx, db, fileID, nfo); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("a bare stub must not get an nfo_metadata row")
	}

	if _, err := EditNFO(nfo, NFOFields{Studio: ptr("Acme")}); err != nil {
		t.Fatal(err)
	}
	if err := RefreshNFORecord(ctx, db, fileID, nfo); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatal("an edited stub should be recorded")
	}
}

func ptr[T any](v T) *T { return &v }

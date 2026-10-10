package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// TestReconcileRenamedFile exercises the namer/Provider bridge's DB-side
// integration: given a file the (external, Flask) namer engine has already
// renamed and written an .nfo for, ReconcileRenamedFile must update the
// existing media_file in place and pull the NFO's metadata into media_item.
//
// Requires a real Postgres reachable at REELSTASH_TEST_DATABASE_URL; skipped
// otherwise (this is an integration test, not a unit test).
func TestReconcileRenamedFile(t *testing.T) {
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := testdb.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mountDir := t.TempDir()
	studioDir := filepath.Join(mountDir, "StudioA")
	if err := os.MkdirAll(studioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldAbsPath := filepath.Join(studioDir, "random-name.mp4")
	if err := os.WriteFile(oldAbsPath, []byte("fake video bytes for reconcile test"), 0o644); err != nil {
		t.Fatal(err)
	}

	var storageLocationID, mediaItemID, mediaFileID int64
	if err := pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"ReconcileTestDisk-"+uuid.NewString(), mountDir).Scan(&storageLocationID); err != nil {
		t.Fatalf("insert storage_location: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO media_item (title, sort_title, metadata_source) VALUES ('random-name', 'random-name', 'FILENAME') RETURNING id`).
		Scan(&mediaItemID); err != nil {
		t.Fatalf("insert media_item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
		VALUES ($1, $2, 'StudioA/random-name.mp4', 'random-name.mp4', 'mp4', 36)
		RETURNING id
	`, mediaItemID, storageLocationID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert media_file: %v", err)
	}

	// Simulate what the Flask namer engine does: rename the file in place
	// and write an adjacent Kodi-style .nfo with Provider-sourced metadata.
	newAbsPath := filepath.Join(studioDir, "Studio A - 2024-01-15 - Great Scene.mp4")
	if err := os.Rename(oldAbsPath, newAbsPath); err != nil {
		t.Fatal(err)
	}
	nfoAbsPath := filepath.Join(studioDir, "Studio A - 2024-01-15 - Great Scene.nfo")
	nfoContent := `<movie>
  <title>Great Scene</title>
  <plot>From Provider via the namer bridge</plot>
  <studio>Studio A</studio>
  <premiered>2024-01-15</premiered>
  <year>2024</year>
  <genre>Testing</genre>
  <actor><name>Bridge Performer</name></actor>
</movie>`
	if err := os.WriteFile(nfoAbsPath, []byte(nfoContent), 0o644); err != nil {
		t.Fatal(err)
	}

	jobRunner := jobs.NewRunner(pool, 1)
	thumbnailJobRunner := jobs.NewRunner(pool, 1)
	sc := New(pool, jobRunner, thumbnailJobRunner, t.TempDir(), 1)

	if err := sc.ReconcileRenamedFile(ctx, mediaFileID, newAbsPath, nfoAbsPath); err != nil {
		t.Fatalf("ReconcileRenamedFile: %v", err)
	}

	var relativePath, fileName string
	if err := pool.QueryRow(ctx, `SELECT relative_path, file_name FROM media_file WHERE id = $1`, mediaFileID).
		Scan(&relativePath, &fileName); err != nil {
		t.Fatalf("read back media_file: %v", err)
	}
	if relativePath != "StudioA/Studio A - 2024-01-15 - Great Scene.mp4" {
		t.Errorf("relative_path not updated, got %q", relativePath)
	}
	if fileName != "Studio A - 2024-01-15 - Great Scene.mp4" {
		t.Errorf("file_name not updated, got %q", fileName)
	}

	var title, metadataSource string
	var studioName *string
	if err := pool.QueryRow(ctx, `
		SELECT mi.title, mi.metadata_source, s.name
		FROM media_item mi LEFT JOIN studio s ON s.id = mi.studio_id
		WHERE mi.id = $1
	`, mediaItemID).Scan(&title, &metadataSource, &studioName); err != nil {
		t.Fatalf("read back media_item: %v", err)
	}
	if title != "Great Scene" {
		t.Errorf("expected NFO title to be applied, got %q", title)
	}
	if metadataSource != "NFO" {
		t.Errorf("expected metadata_source NFO, got %q", metadataSource)
	}
	if studioName == nil || *studioName != "Studio A" {
		t.Errorf("expected studio 'Studio A' from NFO, got %v", studioName)
	}

	var performerName string
	if err := pool.QueryRow(ctx, `
		SELECT p.name FROM media_item_performer mip
		JOIN performer p ON p.id = mip.performer_id
		WHERE mip.media_item_id = $1
	`, mediaItemID).Scan(&performerName); err != nil {
		t.Fatalf("expected performer from NFO to be attached: %v", err)
	}
	if performerName != "Bridge Performer" {
		t.Errorf("expected performer 'Bridge Performer', got %q", performerName)
	}
}

package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"reelstash/backend/internal/database"
	"reelstash/backend/internal/testdb"
)

func TestSQLiteCreatesCurrentSchema(t *testing.T) {
	ctx := context.Background()
	store, err := database.OpenSQLite(ctx, filepath.Join(t.TempDir(), "reelstash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := database.Migrate(ctx, store); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, store); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var tables int
	if err := store.QueryRow(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 68 {
		t.Errorf("SQLite tables = %d, want 68", tables)
	}
}

func TestSQLitePreservesNumberedParameterOrder(t *testing.T) {
	ctx := context.Background()
	store, err := database.OpenSQLite(ctx, filepath.Join(t.TempDir(), "parameters.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Exec(ctx, `CREATE TABLE item (id INTEGER PRIMARY KEY, status TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(ctx, `INSERT INTO item (id, status) VALUES ($1, $2)`, 1, "RUNNING"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(ctx, `UPDATE item SET status = $2 WHERE id = $1`, 1, "COMPLETED"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := store.QueryRow(ctx, `SELECT status FROM item WHERE id = $1`, 1).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "COMPLETED" {
		t.Fatalf("status = %q, want COMPLETED", status)
	}
}

func TestSQLiteSearchFunctions(t *testing.T) {
	ctx := context.Background()
	store, err := database.OpenSQLite(ctx, filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var word, full float64
	if err := store.QueryRow(ctx, `SELECT word_similarity('matrix', 'The Matrix'), similarity('Matrix', 'matrix')`).Scan(&word, &full); err != nil {
		t.Fatal(err)
	}
	if word < 0.9 || full < 0.9 {
		t.Fatalf("search scores too low: word=%f full=%f", word, full)
	}
	var ordered string
	if err := store.QueryRow(ctx, `SELECT array_agg(value ORDER BY value) FROM (SELECT 'b' AS value UNION ALL SELECT 'a')`).Scan(&ordered); err != nil {
		t.Fatal(err)
	}
	if ordered != `["a","b"]` {
		t.Fatalf("ordered aggregate = %q", ordered)
	}
}

func TestMigrateBuildsSchemaAndIsIdempotent(t *testing.T) {
	pool := testdb.Isolated(t) // already migrated once
	ctx := context.Background()

	if err := testdb.Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate must be a no-op: %v", err)
	}
	for _, table := range []string{
		"app_user", "web_session", "media_item", "media_file", "storage_location", "performer", "studio", "series",
		"category", "tag", "collection", "playback_state", "background_job", "scan_job", "app_setting", "webhook",
		"notification", "library", "library_root", "library_access", "library_provider", "match_link",
		"media_item_favorite", "performer_favorite", "scan_rule", "organizer_rule",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s missing after migrating", table)
		}
	}
}

// Every migration file must be re-runnable: Migrate reapplies them all on each
// startup.
func TestMigrationsAreRepeatable(t *testing.T) {
	pool := testdb.Isolated(t)
	for i := 0; i < 3; i++ {
		if err := testdb.Migrate(context.Background(), pool); err != nil {
			t.Fatalf("run %d: %v", i+2, err)
		}
	}
}

func TestConnect(t *testing.T) {
	ctx := context.Background()
	if _, err := database.Connect(ctx, "::not a url::", 0); err == nil {
		t.Error("malformed URL must fail")
	}
	if _, err := database.Connect(ctx, "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", 0); err == nil {
		t.Error("unreachable server must fail at ping")
	}

	pool, err := database.Connect(ctx, testdb.URL(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if got := pool.Config().MaxConns; got != 3 {
		t.Errorf("MaxConns = %d, want 3", got)
	}
	pool2, _ := database.Connect(ctx, testdb.URL(t), 0)
	defer pool2.Close()
	if pool2.Config().MaxConns < 4 {
		t.Errorf("maxConns 0 should keep the pgx default, got %d", pool2.Config().MaxConns)
	}
}

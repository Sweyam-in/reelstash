package database_test

import (
	"context"
	"testing"

	"reelstash/backend/internal/database"
	"reelstash/backend/internal/testdb"
)

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

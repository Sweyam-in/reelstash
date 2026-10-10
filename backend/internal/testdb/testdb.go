// Package testdb gives integration tests a Postgres connection. It is only
// imported by _test files.
//
// REELSTASH_TEST_DATABASE_URL must point at a scratch database (never the one
// you use for real). Tests skip when it is unset. Because several packages
// migrate the same schema, run the suite with `go test -p 1 ./...`.
package testdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/database"
)

const envVar = "REELSTASH_TEST_DATABASE_URL"

// URL returns the test database URL, skipping the test when it is unset.
func URL(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv(envVar)
	if dsn == "" {
		t.Skip(envVar + " not set; skipping Postgres integration test")
	}
	return dsn
}

// migrateLockKey serializes schema setup across test binaries.
const migrateLockKey = 74_110_001

// migrationsHash fingerprints the migration files on disk.
func migrationsHash() string {
	_, self, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(self), "..", "database", "migrations")
	entries, _ := os.ReadDir(dir)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	h := sha256.New()
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		h.Write([]byte(e.Name()))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Migrate brings the shared schema up to date, at most once per change to the
// migration files.
//
// database.Migrate replays every file on each call, and those files drop and
// recreate triggers and functions. Test binaries run in parallel under
// `go test ./...`, so replaying them while another package is writing rows
// deadlocks. Recording a hash of the applied files makes every call after the
// first a cheap no-op, and an advisory lock serializes the one that does work.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrateLockKey)

	want := migrationsHash()
	// Qualify with the connection's own schema so an isolated schema tracks
	// its own state.
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS testdb_migrated (hash text NOT NULL)`); err != nil {
		return err
	}
	var have string
	_ = conn.QueryRow(ctx, `SELECT hash FROM testdb_migrated LIMIT 1`).Scan(&have)
	if have == want {
		return nil
	}
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `DELETE FROM testdb_migrated`); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `INSERT INTO testdb_migrated (hash) VALUES ($1)`, want)
	return err
}

// Shared returns a migrated pool on the shared test schema. Tests using it
// must namespace their rows and clean up after themselves.
func Shared(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

// Isolated returns a pool bound to a brand-new schema (with public still on
// the search path for extensions) that is fully migrated and dropped when the
// test ends. Use it when a test must see only its own rows — for example the
// job runner, which consumes every PENDING job it can see.
func Isolated(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	dsn := URL(t)

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	// pg_trgm and friends live in public; make sure they exist before any
	// isolated schema tries to use them.
	if err := Migrate(ctx, admin); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	schema := "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	return pool
}

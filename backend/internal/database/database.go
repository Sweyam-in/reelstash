// Package database owns the Postgres connection pool and applies the
// ReelStash schema on startup.
package database

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the query surface used by ReelStash. A pgx pool satisfies it, and
// the SQLite adapter implements the same methods for local installations.
type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
	Ping(context.Context) error
	Stat() *pgxpool.Stat
	Close()
}

//go:embed migrations/postgres.sql migrations/sqlite.sql
var migrationsFS embed.FS

// Open selects PostgreSQL or SQLite from DATABASE_URL. SQLite URLs use the
// sqlite:///absolute/path syntax; a plain path is also accepted.
func Open(ctx context.Context, databaseURL string, maxConns int32) (DB, error) {
	if strings.HasPrefix(databaseURL, "sqlite://") {
		return OpenSQLite(ctx, strings.TrimPrefix(databaseURL, "sqlite://"))
	}
	return Connect(ctx, databaseURL, maxConns)
}

// Connect opens the pool. maxConns overrides pgx's default sizing
// (max(4, NumCPU)) when > 0 — useful on small boxes where ffmpeg-heavy
// scanning/transcoding and concurrent API/streaming traffic compete for the
// same handful of connections.
func Connect(ctx context.Context, databaseURL string, maxConns int32) (*pgxpool.Pool, error) {
	pgCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		pgCfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// Migrate applies the consolidated PostgreSQL schema. It preserves the
// historical upgrade steps so existing installations and fresh databases
// reach the same schema. The script is safe to replay at startup.
func Migrate(ctx context.Context, pool DB) error {
	if sqlite, ok := pool.(*SQLite); ok {
		contents, err := migrationsFS.ReadFile("migrations/sqlite.sql")
		if err != nil {
			return fmt.Errorf("read SQLite schema: %w", err)
		}
		if _, err := sqlite.db.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("apply SQLite schema: %w", err)
		}
		return nil
	}
	contents, err := migrationsFS.ReadFile("migrations/postgres.sql")
	if err != nil {
		return fmt.Errorf("read PostgreSQL schema: %w", err)
	}
	if _, err := pool.Exec(ctx, string(contents)); err != nil {
		return fmt.Errorf("apply PostgreSQL schema: %w", err)
	}
	return nil
}

// Package database owns the Postgres connection pool and applies the
// ReelStash schema on startup.
package database

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/postgres.sql
var migrationsFS embed.FS

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
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	contents, err := migrationsFS.ReadFile("migrations/postgres.sql")
	if err != nil {
		return fmt.Errorf("read PostgreSQL schema: %w", err)
	}
	if _, err := pool.Exec(ctx, string(contents)); err != nil {
		return fmt.Errorf("apply PostgreSQL schema: %w", err)
	}
	return nil
}

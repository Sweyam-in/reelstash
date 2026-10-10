package plugins

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store keeps what ReelStash remembers about bundles between restarts.
type Store interface {
	Get(ctx context.Context, key string) (string, bool)
	Put(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
}

// DBStore keeps settings in app_setting.
type DBStore struct{ DB *pgxpool.Pool }

func (s DBStore) Get(ctx context.Context, key string) (string, bool) {
	var v string
	if err := s.DB.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s DBStore) Put(ctx context.Context, key, value string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	return err
}

func (s DBStore) Delete(ctx context.Context, key string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM app_setting WHERE key = $1`, key)
	return err
}

// MemStore is a Store in memory, for tests.
type MemStore struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemStore() *MemStore { return &MemStore{m: map[string]string{}} }

func (s *MemStore) Get(_ context.Context, key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok
}

func (s *MemStore) Put(_ context.Context, key, value string) error {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
	return nil
}

func (s *MemStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
	return nil
}

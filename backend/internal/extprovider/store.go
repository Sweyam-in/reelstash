package extprovider

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/provider"
)

const settingKey = "external_providers"

// LoadSpecs returns the saved plugin descriptions.
func LoadSpecs(ctx context.Context, db *pgxpool.Pool) []Spec {
	var raw string
	if err := db.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, settingKey).Scan(&raw); err != nil {
		return nil
	}
	var specs []Spec
	_ = json.Unmarshal([]byte(raw), &specs)
	return specs
}

// SaveSpecs replaces the saved plugin descriptions.
func SaveSpecs(ctx context.Context, db *pgxpool.Pool, specs []Spec) error {
	raw, _ := json.Marshal(specs)
	_, err := db.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, settingKey, string(raw))
	return err
}

// UpsertSpec adds a plugin, or replaces the one with the same id.
func UpsertSpec(ctx context.Context, db *pgxpool.Pool, spec Spec) error {
	specs := LoadSpecs(ctx, db)
	for i := range specs {
		if specs[i].ID == spec.ID {
			specs[i] = spec
			return SaveSpecs(ctx, db, specs)
		}
	}
	return SaveSpecs(ctx, db, append(specs, spec))
}

// RemoveSpec forgets a plugin.
func RemoveSpec(ctx context.Context, db *pgxpool.Pool, id string) error {
	specs := LoadSpecs(ctx, db)
	kept := specs[:0]
	for _, s := range specs {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	return SaveSpecs(ctx, db, kept)
}

// RegisterSaved registers every saved plugin. Call before Registry.Bootstrap,
// which then applies each plugin's saved token.
func RegisterSaved(ctx context.Context, db *pgxpool.Pool, reg *provider.Registry) {
	for _, spec := range LoadSpecs(ctx, db) {
		if _, exists := reg.Get(spec.ID); exists {
			continue
		}
		reg.Register(NewProvider(spec, ""))
		log.Printf("provider plugin %q registered (%s)", spec.ID, spec.BaseURL)
	}
}

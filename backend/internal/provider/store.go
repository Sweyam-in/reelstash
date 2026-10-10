package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	settingOrder = "provider_order"
	settingMode  = "match_mode"
)

func configKey(id string) string { return "provider." + id + ".config" }

func putSetting(ctx context.Context, db *pgxpool.Pool, key, value string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	return err
}

func getSetting(ctx context.Context, db *pgxpool.Pool, key string) (string, bool) {
	var v string
	if err := db.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

// LoadConfig returns a provider's saved settings.
func LoadConfig(ctx context.Context, db *pgxpool.Pool, id string) map[string]string {
	cfg := map[string]string{}
	if raw, ok := getSetting(ctx, db, configKey(id)); ok {
		_ = json.Unmarshal([]byte(raw), &cfg)
		return cfg
	}
	return cfg
}

// SaveConfig replaces a provider's saved settings. Empty values are dropped.
func SaveConfig(ctx context.Context, db *pgxpool.Pool, id string, cfg map[string]string) error {
	clean := map[string]string{}
	for k, v := range cfg {
		if strings.TrimSpace(v) != "" {
			clean[k] = strings.TrimSpace(v)
		}
	}
	raw, _ := json.Marshal(clean)
	return putSetting(ctx, db, configKey(id), string(raw))
}

// ClearConfig removes a provider's saved settings.
func ClearConfig(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `DELETE FROM app_setting WHERE key = $1`, configKey(id))
	return err
}

// LoadOrder returns the saved priority order, if any.
func LoadOrder(ctx context.Context, db *pgxpool.Pool) []string {
	var order []string
	if raw, ok := getSetting(ctx, db, settingOrder); ok {
		_ = json.Unmarshal([]byte(raw), &order)
	}
	return order
}

func SaveOrder(ctx context.Context, db *pgxpool.Pool, order []string) error {
	raw, _ := json.Marshal(order)
	return putSetting(ctx, db, settingOrder, string(raw))
}

// LoadMode returns "priority" (default) or "best".
func LoadMode(ctx context.Context, db *pgxpool.Pool) string {
	if v, _ := getSetting(ctx, db, settingMode); v == "best" {
		return "best"
	}
	return "priority"
}

func SaveMode(ctx context.Context, db *pgxpool.Pool, mode string) error {
	return putSetting(ctx, db, settingMode, mode)
}

// AutoMatch says, per kind, whether a confident provider match is applied on
// its own or only suggested for review. Titles default to review; performers
// and studios (an exact, unambiguous name hit) default to applying.
type AutoMatch struct {
	Titles     bool `json:"titles"`
	Performers bool `json:"performers"`
	Studios    bool `json:"studios"`
}

// AutoMatchKinds are the kinds AutoMatch covers, in the order the API lists them.
var AutoMatchKinds = []string{"titles", "performers", "studios"}

var autoMatchDefaults = map[string]bool{"titles": false, "performers": true, "studios": true}

func autoMatchKey(kind string) string { return "auto_match." + kind }

// AutoMatchEnabled reads one kind's setting, falling back to its default.
func AutoMatchEnabled(ctx context.Context, db *pgxpool.Pool, kind string) bool {
	if v, ok := getSetting(ctx, db, autoMatchKey(kind)); ok {
		return v == "true"
	}
	return autoMatchDefaults[kind]
}

func LoadAutoMatch(ctx context.Context, db *pgxpool.Pool) AutoMatch {
	return AutoMatch{
		Titles:     AutoMatchEnabled(ctx, db, "titles"),
		Performers: AutoMatchEnabled(ctx, db, "performers"),
		Studios:    AutoMatchEnabled(ctx, db, "studios"),
	}
}

func SaveAutoMatch(ctx context.Context, db *pgxpool.Pool, kind string, on bool) error {
	if _, ok := autoMatchDefaults[kind]; !ok {
		return fmt.Errorf("unknown automatic matching kind %q", kind)
	}
	return putSetting(ctx, db, autoMatchKey(kind), strconv.FormatBool(on))
}

// Bootstrap applies saved settings and priority order to every registered
// provider. Startup passes env-derived defaults through the providers' own
// constructors; saved settings override them.
func (r *Registry) Bootstrap(ctx context.Context, db *pgxpool.Pool) {
	for _, p := range r.All() {
		if cfg := LoadConfig(ctx, db, p.ID()); len(cfg) > 0 {
			_ = p.Configure(cfg)
		}
	}
	r.SetOrder(LoadOrder(ctx, db))
}

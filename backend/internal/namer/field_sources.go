package namer

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PerformerField is a performer detail the user can take from one provider.
type PerformerField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// PerformerFields lists the details that can be sourced per provider.
var PerformerFields = []PerformerField{
	{"bio", "Bio"}, {"gender", "Gender"}, {"birthdate", "Birthdate"}, {"birthplace", "Birthplace"},
	{"nationality", "Nationality"}, {"ethnicity", "Ethnicity"}, {"height", "Height"},
	{"measurements", "Measurements"}, {"hairColor", "Hair colour"}, {"eyeColor", "Eye colour"},
}

func performerField(m *storedPerformerMetadata, key string) *string {
	switch key {
	case "bio":
		return &m.Bio
	case "gender":
		return &m.Gender
	case "birthdate":
		return &m.Birthdate
	case "birthplace":
		return &m.Birthplace
	case "nationality":
		return &m.Nationality
	case "ethnicity":
		return &m.Ethnicity
	case "height":
		return &m.Height
	case "measurements":
		return &m.Measurements
	case "hairColor":
		return &m.HairColor
	case "eyeColor":
		return &m.EyeColor
	}
	return nil
}

// dbtx is satisfied by both the pool and a transaction.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// LinkedPerformerMetadata is each linked provider's own copy of the details,
// newest match first.
type LinkedPerformerMetadata struct {
	Provider string
	Values   map[string]string
	meta     storedPerformerMetadata
}

func loadPerformerLinks(ctx context.Context, db dbtx, performerID int64) ([]LinkedPerformerMetadata, error) {
	rows, err := db.Query(ctx, `
		SELECT provider, remote_metadata FROM match_link
		WHERE kind = 'performer' AND entity_id = $1 AND remote_metadata IS NOT NULL
		ORDER BY matched_at DESC`, performerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinkedPerformerMetadata
	for rows.Next() {
		var l LinkedPerformerMetadata
		var raw []byte
		if err := rows.Scan(&l.Provider, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &l.meta) != nil {
			continue
		}
		l.Values = map[string]string{}
		for _, f := range PerformerFields {
			if v := *performerField(&l.meta, f.Key); v != "" {
				l.Values[f.Key] = v
			}
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PerformerLinks returns each linked provider's details and the saved choices.
func PerformerLinks(ctx context.Context, db dbtx, performerID int64) ([]LinkedPerformerMetadata, map[string]string, error) {
	links, err := loadPerformerLinks(ctx, db, performerID)
	if err != nil {
		return nil, nil, err
	}
	sources := map[string]string{}
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT field_sources FROM performer WHERE id = $1`, performerID).Scan(&raw); err != nil {
		return nil, nil, err
	}
	_ = json.Unmarshal(raw, &sources)
	return links, sources, nil
}

// buildPerformerMetadata combines the linked providers' details: the newest
// match wins, gaps are filled from the others, and a field with a chosen
// source takes that provider's value when it has one.
func buildPerformerMetadata(links []LinkedPerformerMetadata, sources map[string]string) storedPerformerMetadata {
	var out storedPerformerMetadata
	for i, l := range links {
		if i == 0 {
			out = l.meta
			out.Candidates = nil
			continue
		}
		out = mergePerformerMetadata(out, l.meta)
	}
	for key, providerID := range sources {
		dst := performerField(&out, key)
		if dst == nil {
			continue
		}
		for _, l := range links {
			if l.Provider == providerID && l.Values[key] != "" {
				*dst = l.Values[key]
			}
		}
	}
	return out
}

// RebuildPerformerMetadata recomputes a performer's displayed details from its
// provider links and field choices. A performer with no linked details keeps
// what it has.
func RebuildPerformerMetadata(ctx context.Context, db dbtx, performerID int64) error {
	links, sources, err := PerformerLinks(ctx, db, performerID)
	if err != nil || len(links) == 0 {
		return err
	}
	raw, err := json.Marshal(buildPerformerMetadata(links, sources))
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `UPDATE performer SET remote_metadata = $2, updated_at = now() WHERE id = $1`, performerID, raw)
	return err
}

// SetPerformerFieldSources saves which provider each field comes from (an
// empty provider clears the choice) and rebuilds the displayed details.
func SetPerformerFieldSources(ctx context.Context, db dbtx, performerID int64, sources map[string]string) error {
	clean := map[string]string{}
	for key, providerID := range sources {
		if providerID != "" && performerField(&storedPerformerMetadata{}, key) != nil {
			clean[key] = providerID
		}
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	tag, err := db.Exec(ctx, `UPDATE performer SET field_sources = $2 WHERE id = $1`, performerID, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return RebuildPerformerMetadata(ctx, db, performerID)
}

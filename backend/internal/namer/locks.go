package namer

import (
	"context"
	"sort"

	"reelstash/backend/internal/database"
)

// LockableFields are the parts of a title a person can ask ReelStash to keep ("Keep my edits").
var LockableFields = []string{"title", "description", "studio", "categories", "performers"}

// KeptValues is what a title held in its locked fields before a match was applied.
type KeptValues struct {
	Fields         []string
	Title          *string
	Description    *string
	StudioID       *int64
	StudioName     *string
	CategoryIDs    []int64
	CategoryNames  []string
	PerformerIDs   []int64
	PerformerNames []string
}

func (k *KeptValues) has(field string) bool {
	for _, f := range k.Fields {
		if f == field {
			return true
		}
	}
	return false
}

// LockedFields returns the fields locked on a title, in LockableFields order.
func LockedFields(ctx context.Context, db database.DB, mediaItemID int64) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT field FROM media_item_field_lock WHERE media_item_id = $1`, mediaItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		set[f] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []string{}
	for _, f := range LockableFields {
		if set[f] {
			out = append(out, f)
		}
	}
	return out, nil
}

// SetLockedFields replaces a title's locks with fields (unknown names are ignored).
func SetLockedFields(ctx context.Context, db database.DB, mediaItemID int64, fields []string) error {
	if _, err := db.Exec(ctx, `DELETE FROM media_item_field_lock WHERE media_item_id = $1`, mediaItemID); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range LockableFields {
		known[f] = true
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !known[f] || seen[f] {
			continue
		}
		seen[f] = true
		if _, err := db.Exec(ctx, `INSERT INTO media_item_field_lock (media_item_id, field) VALUES ($1, $2)`, mediaItemID, f); err != nil {
			return err
		}
	}
	return nil
}

// snapshotKept reads the current values of a title's locked fields; nil when nothing is locked.
func (e *Engine) snapshotKept(ctx context.Context, mediaItemID int64) (*KeptValues, error) {
	fields, err := LockedFields(ctx, e.DB, mediaItemID)
	if err != nil || len(fields) == 0 {
		return nil, err
	}
	k := &KeptValues{Fields: fields}
	if err := e.DB.QueryRow(ctx, `
		SELECT mi.title, mi.description, mi.studio_id, s.name
		FROM media_item mi LEFT JOIN studio s ON s.id = mi.studio_id WHERE mi.id = $1
	`, mediaItemID).Scan(&k.Title, &k.Description, &k.StudioID, &k.StudioName); err != nil {
		return nil, err
	}
	if k.has("categories") {
		rows, err := e.DB.Query(ctx, `SELECT c.id, c.name FROM media_item_category mic JOIN category c ON c.id = mic.category_id WHERE mic.media_item_id = $1 ORDER BY c.name`, mediaItemID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			k.CategoryIDs = append(k.CategoryIDs, id)
			k.CategoryNames = append(k.CategoryNames, name)
		}
		rows.Close()
	}
	if k.has("performers") {
		rows, err := e.DB.Query(ctx, `SELECT p.id, p.name FROM media_item_performer mip JOIN performer p ON p.id = mip.performer_id WHERE mip.media_item_id = $1 ORDER BY mip.position, p.id`, mediaItemID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			k.PerformerIDs = append(k.PerformerIDs, id)
			k.PerformerNames = append(k.PerformerNames, name)
		}
		rows.Close()
	}
	return k, nil
}

// restoreKept writes the locked fields back after a match and keeps the title marked as hand-edited,
// so later scans and automatic matching leave it alone.
func (e *Engine) restoreKept(ctx context.Context, mediaItemID int64, k *KeptValues) error {
	if k == nil {
		return nil
	}
	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if k.has("title") && k.Title != nil {
		if _, err := tx.Exec(ctx, `UPDATE media_item SET title = $2, sort_title = $2 WHERE id = $1`, mediaItemID, *k.Title); err != nil {
			return err
		}
	}
	if k.has("description") {
		if _, err := tx.Exec(ctx, `UPDATE media_item SET description = $2 WHERE id = $1`, mediaItemID, k.Description); err != nil {
			return err
		}
	}
	if k.has("studio") {
		if _, err := tx.Exec(ctx, `UPDATE media_item SET studio_id = $2 WHERE id = $1`, mediaItemID, k.StudioID); err != nil {
			return err
		}
	}
	if k.has("categories") {
		if _, err := tx.Exec(ctx, `DELETE FROM media_item_category WHERE media_item_id = $1`, mediaItemID); err != nil {
			return err
		}
		for _, id := range k.CategoryIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO media_item_category (media_item_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, mediaItemID, id); err != nil {
				return err
			}
		}
	}
	if k.has("performers") {
		if _, err := tx.Exec(ctx, `DELETE FROM media_item_performer WHERE media_item_id = $1`, mediaItemID); err != nil {
			return err
		}
		for i, id := range k.PerformerIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id, position) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, mediaItemID, id, i); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE media_item SET metadata_source = 'MANUAL', updated_at = now() WHERE id = $1`, mediaItemID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// sortedFields is used by tests and logs to compare field sets.
func sortedFields(fields []string) []string {
	out := append([]string(nil), fields...)
	sort.Strings(out)
	return out
}

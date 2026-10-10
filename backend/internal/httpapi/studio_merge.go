package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"reelstash/backend/internal/scanner"
)

// mergeStudio folds studio {sourceId} into studio {id}: its titles, series,
// aliases, logo, organizer rule and provider matches move across, the source
// name becomes an alias, the source record is deleted, and the NFO sidecars of
// the moved titles are rewritten so a rescan cannot bring the old name back.
func (a *API) mergeStudio(w http.ResponseWriter, r *http.Request) {
	targetID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	var req struct {
		SourceID int64 `json:"sourceId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SourceID < 1 {
		writeError(w, http.StatusBadRequest, "sourceId is required")
		return
	}
	if req.SourceID == targetID {
		writeError(w, http.StatusBadRequest, "choose two different studios")
		return
	}
	ctx := r.Context()
	merged, ok := a.applyStudioMerge(ctx, w, targetID, req.SourceID)
	if !ok {
		return
	}
	if merged.staleKey != "" && filepath.Base(merged.staleKey) == merged.staleKey {
		os.Remove(filepath.Join(a.Cfg.PosterDir, merged.staleKey))
	}
	updated, failed := 0, 0
	for _, itemID := range merged.itemIDs {
		u, f := a.writeItemNFOs(ctx, itemID, scanner.NFOFields{Studio: &merged.targetName}, false)
		updated += u
		failed += f
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         targetID,
		"titles":     len(merged.itemIDs),
		"nfoUpdated": updated,
		"nfoFailed":  failed,
	})
}

type studioMergeOutcome struct {
	itemIDs    []int64
	targetName string
	staleKey   string
}

// applyStudioMerge runs the studio merge transaction. false means the
// response was already written.
func (a *API) applyStudioMerge(ctx context.Context, w http.ResponseWriter, targetID, sourceID int64) (studioMergeOutcome, bool) {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start merge")
		return studioMergeOutcome{}, false
	}
	defer tx.Rollback(ctx)

	type row struct {
		name    string
		logo    *int64
		logoKey string
		linked  bool
	}
	// Lock in id order so two concurrent merges cannot deadlock.
	first, second := targetID, sourceID
	if first > second {
		first, second = second, first
	}
	rows := map[int64]row{}
	for _, id := range []int64{first, second} {
		var s row
		err := tx.QueryRow(ctx, `
			SELECT s.name, s.logo_image_id, COALESCE(ar.storage_key, ''), EXISTS (SELECT 1 FROM match_link ml WHERE ml.kind = 'studio' AND ml.entity_id = s.id)
			FROM studio s LEFT JOIN artwork ar ON ar.id = s.logo_image_id WHERE s.id = $1 FOR UPDATE OF s
		`, id).Scan(&s.name, &s.logo, &s.logoKey, &s.linked)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "studio not found")
			return studioMergeOutcome{}, false
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load studio")
			return studioMergeOutcome{}, false
		}
		rows[id] = s
	}
	target, source := rows[targetID], rows[sourceID]

	fail := func(msg string, err error) {
		log.Printf("studio merge %d into %d: %s: %v", sourceID, targetID, msg, err)
		writeError(w, http.StatusInternalServerError, msg)
	}

	var itemIDs []int64
	itemRows, err := tx.Query(ctx, `SELECT id FROM media_item WHERE studio_id = $1`, sourceID)
	if err != nil {
		fail("failed to load titles", err)
		return studioMergeOutcome{}, false
	}
	for itemRows.Next() {
		var id int64
		if err := itemRows.Scan(&id); err != nil {
			itemRows.Close()
			fail("failed to load titles", err)
			return studioMergeOutcome{}, false
		}
		itemIDs = append(itemIDs, id)
	}
	itemRows.Close()

	steps := []struct {
		msg  string
		sql  string
		args []any
	}{
		{"failed to move titles", `UPDATE media_item SET studio_id = $1, updated_at = now() WHERE studio_id = $2`, []any{targetID, sourceID}},
		// Series names are unique per studio; a series the survivor already has
		// by that name keeps its own record and the other loses its studio.
		{"failed to move series", `
			UPDATE series s SET studio_id = $1, updated_at = now()
			WHERE s.studio_id = $2 AND NOT EXISTS (SELECT 1 FROM series t WHERE t.studio_id = $1 AND t.name = s.name)`, []any{targetID, sourceID}},
		{"failed to move aliases", `
			INSERT INTO studio_alias (studio_id, alias)
			SELECT $1, alias FROM studio_alias WHERE studio_id = $2 AND alias <> $3
			ON CONFLICT DO NOTHING`, []any{targetID, sourceID, target.name}},
		{"failed to record alias", `
			INSERT INTO studio_alias (studio_id, alias) VALUES ($1, $2) ON CONFLICT DO NOTHING`, []any{targetID, source.name}},
		{"failed to move the organizer rule", `
			UPDATE organizer_rule SET studio_id = $1, updated_at = now()
			WHERE studio_id = $2 AND NOT EXISTS (SELECT 1 FROM organizer_rule WHERE studio_id = $1)`, []any{targetID, sourceID}},
		{"failed to merge details", `
			UPDATE studio t SET
				description = COALESCE(t.description, s.description),
				website = COALESCE(t.website, s.website),
				updated_at = now()
			FROM studio s WHERE t.id = $1 AND s.id = $2`, []any{targetID, sourceID}},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			fail(step.msg, err)
			return studioMergeOutcome{}, false
		}
	}

	// A survivor with no provider match takes the source's match state; the
	// links themselves move below.
	if !target.linked && source.linked {
		if _, err := tx.Exec(ctx, `
			UPDATE studio t SET match_status = s.match_status,
				remote_metadata = s.remote_metadata, matched_at = s.matched_at
			FROM studio s WHERE t.id = $1 AND s.id = $2`, targetID, sourceID); err != nil {
			fail("failed to move the provider match", err)
			return studioMergeOutcome{}, false
		}
	}

	// Links to other providers move to the survivor too, unless it already has
	// a link for that provider.
	if _, err := tx.Exec(ctx, `
		UPDATE match_link SET entity_id = $1
		WHERE kind = 'studio' AND entity_id = $2
		  AND provider NOT IN (SELECT provider FROM match_link WHERE kind = 'studio' AND entity_id = $1)
	`, targetID, sourceID); err != nil {
		fail("failed to move the provider matches", err)
		return studioMergeOutcome{}, false
	}
	if _, err := tx.Exec(ctx, `
		UPDATE studio t SET match_status = 'matched', remote_metadata = s.remote_metadata, matched_at = s.matched_at
		FROM studio s
		WHERE t.id = $1 AND s.id = $2 AND t.match_status <> 'matched'
		  AND EXISTS (SELECT 1 FROM match_link WHERE kind = 'studio' AND entity_id = $1)
	`, targetID, sourceID); err != nil {
		fail("failed to move the provider matches", err)
		return studioMergeOutcome{}, false
	}

	// The survivor takes the source's logo when it has none; otherwise the
	// source's logo goes with it.
	staleKey := ""
	if source.logo != nil {
		if _, err := tx.Exec(ctx, `UPDATE studio SET logo_image_id = NULL WHERE id = $1`, sourceID); err != nil {
			fail("failed to move the logo", err)
			return studioMergeOutcome{}, false
		}
		if target.logo == nil {
			if _, err := tx.Exec(ctx, `UPDATE studio SET logo_image_id = $2 WHERE id = $1`, targetID, *source.logo); err != nil {
				fail("failed to move the logo", err)
				return studioMergeOutcome{}, false
			}
		} else {
			if _, err := tx.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, *source.logo); err != nil {
				fail("failed to remove the old logo", err)
				return studioMergeOutcome{}, false
			}
			staleKey = source.logoKey
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM studio WHERE id = $1`, sourceID); err != nil {
		fail("failed to remove the merged studio", err)
		return studioMergeOutcome{}, false
	}
	if err := tx.Commit(ctx); err != nil {
		fail("failed to save merge", err)
		return studioMergeOutcome{}, false
	}
	return studioMergeOutcome{itemIDs: itemIDs, targetName: target.name, staleKey: staleKey}, true
}

// listStudioDuplicates suggests other studios that look like the same one:
// names that differ only by case, spacing or punctuation, an alias that equals
// the name, or near-identical spelling.
func (a *API) listStudioDuplicates(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		WITH me AS (
			SELECT id, name, regexp_replace(lower(name), '[^[:alnum:]]', '', 'g') AS norm FROM studio WHERE id = $1
		), candidates AS (
			SELECT s.id,
			       CASE
			         WHEN regexp_replace(lower(s.name), '[^[:alnum:]]', '', 'g') = me.norm THEN 1.0
			         WHEN EXISTS (SELECT 1 FROM studio_alias sa WHERE (sa.studio_id = s.id AND lower(sa.alias) = lower(me.name))
			                                                    OR (sa.studio_id = me.id AND lower(sa.alias) = lower(s.name))) THEN 0.95
			         ELSE similarity(s.name, me.name) * 0.75
			       END AS score,
			       CASE
			         WHEN regexp_replace(lower(s.name), '[^[:alnum:]]', '', 'g') = me.norm THEN 'Same name apart from case, spacing or punctuation'
			         WHEN EXISTS (SELECT 1 FROM studio_alias sa WHERE (sa.studio_id = s.id AND lower(sa.alias) = lower(me.name))
			                                                    OR (sa.studio_id = me.id AND lower(sa.alias) = lower(s.name))) THEN 'Listed as an alias'
			         ELSE 'Very similar spelling'
			       END AS reason
			FROM studio s, me
			WHERE s.id <> me.id
		)
		SELECT s.id, s.name, s.logo_image_id, s.match_status,
		       (SELECT COUNT(*) FROM media_item mi WHERE mi.studio_id = s.id), c.score::float8, c.reason
		FROM candidates c JOIN studio s ON s.id = c.id
		WHERE c.score >= 0.375
		ORDER BY c.score DESC, s.name
		LIMIT 20
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look for duplicates")
		return
	}
	defer rows.Close()
	type suggestion struct {
		studio
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}
	items := []suggestion{}
	for rows.Next() {
		var s suggestion
		if err := rows.Scan(&s.ID, &s.Name, &s.LogoImageID, &s.MatchStatus, &s.MediaCount, &s.Score, &s.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read studio")
			return
		}
		items = append(items, s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// linkStudioMedia moves a title to this studio and writes the studio into the
// title's NFO when it has one.
func (a *API) linkStudioMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	var req struct {
		MediaItemID int64 `json:"mediaItemId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.MediaItemID < 1 {
		writeError(w, http.StatusBadRequest, "mediaItemId is required")
		return
	}
	var name string
	if err := a.DB.QueryRow(r.Context(), `SELECT name FROM studio WHERE id = $1`, id).Scan(&name); err != nil {
		writeError(w, http.StatusNotFound, "studio not found")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `UPDATE media_item SET studio_id = $2, updated_at = now() WHERE id = $1`, req.MediaItemID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link title")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}
	updated, failed := a.writeItemNFOs(r.Context(), req.MediaItemID, scanner.NFOFields{Studio: &name}, false)
	writeJSON(w, http.StatusOK, map[string]any{"nfoUpdated": updated, "nfoFailed": failed})
}

// unlinkStudioMedia clears the studio of a title and blanks it in the title's NFO.
func (a *API) unlinkStudioMedia(w http.ResponseWriter, r *http.Request) {
	id, err1 := pathInt64(r, "id")
	itemID, err2 := pathInt64(r, "mediaItemId")
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `UPDATE media_item SET studio_id = NULL, updated_at = now() WHERE id = $1 AND studio_id = $2`, itemID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink title")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "title is not linked to this studio")
		return
	}
	empty := ""
	updated, failed := a.writeItemNFOs(r.Context(), itemID, scanner.NFOFields{Studio: &empty}, false)
	writeJSON(w, http.StatusOK, map[string]any{"nfoUpdated": updated, "nfoFailed": failed})
}

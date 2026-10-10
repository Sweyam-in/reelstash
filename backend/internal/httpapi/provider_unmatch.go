package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reelstash/backend/internal/namer"
	"reelstash/backend/internal/scanner"
)

type unmatchRequest struct {
	// MarkWrong remembers the provider record as a wrong match so automatic
	// jobs never propose or apply it again, and stops automatic lookups for
	// this record until the flag is cleared.
	MarkWrong bool `json:"markWrong"`
	// Provider limits a title unlink to one provider's link ("tmdb", ...).
	// The title's metadata is only reset when no other provider link remains.
	// Empty unlinks every provider.
	Provider string `json:"provider"`
	// RemovePhoto (performers only) also deletes the photo that came with the match.
	RemovePhoto bool `json:"removePhoto"`
}

// providerLink is one provider record a performer or studio is linked to.
type providerLink struct {
	provider, externalID string
	metadata             []byte
}

// splitLinks loads an entity's links and splits them into the ones a request
// removes (all of them, or just the named provider's) and the ones that stay.
func splitLinks(ctx context.Context, tx pgx.Tx, kind string, id int64, providerID string) (removed, kept []providerLink, err error) {
	rows, err := tx.Query(ctx, `SELECT provider, external_id, remote_metadata FROM match_link WHERE kind = $1 AND entity_id = $2`, kind, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l providerLink
		if err := rows.Scan(&l.provider, &l.externalID, &l.metadata); err != nil {
			return nil, nil, err
		}
		if providerID == "" || l.provider == providerID {
			removed = append(removed, l)
		} else {
			kept = append(kept, l)
		}
	}
	return removed, kept, rows.Err()
}

func linkAliases(links []providerLink) map[string]bool {
	set := map[string]bool{}
	for _, l := range links {
		var m struct {
			Aliases []string `json:"aliases"`
		}
		if len(l.metadata) > 0 && json.Unmarshal(l.metadata, &m) == nil {
			for _, alias := range m.Aliases {
				set[alias] = true
			}
		}
	}
	return set
}

// unmatchPerformer removes a performer's match. With a provider only that
// provider's link goes (the performer keeps what other providers supplied);
// otherwise every link goes. Without markWrong the performer returns to "not
// checked"; with it the removed records are flagged so bulk matching never
// picks them again.
func (a *API) unmatchPerformer(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	var req unmatchRequest
	_ = decodeJSON(r, &req)
	ctx := r.Context()

	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start")
		return
	}
	defer tx.Rollback(ctx)

	var metadata []byte
	if err := tx.QueryRow(ctx, `SELECT remote_metadata FROM performer WHERE id = $1 FOR UPDATE`, id).Scan(&metadata); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load performer")
		return
	}
	removed, kept, err := splitLinks(ctx, tx, "performer", id, req.Provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load links")
		return
	}
	if req.Provider != "" && len(removed) == 0 {
		writeError(w, http.StatusNotFound, "this performer is not linked to that provider")
		return
	}
	for _, l := range removed {
		if req.MarkWrong {
			if _, err := tx.Exec(ctx, `INSERT INTO performer_rejected_match (performer_id, provider, external_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, l.provider, l.externalID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record the wrong match")
				return
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM match_link WHERE kind = 'performer' AND entity_id = $1 AND provider = $2`, id, l.provider); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove the link")
			return
		}
	}

	if len(kept) > 0 {
		// Still linked elsewhere: drop only the aliases that came from the
		// removed records, and fall back to a remaining provider's metadata.
		gone, stay := linkAliases(removed), linkAliases(kept)
		var drop []string
		for alias := range gone {
			if !stay[alias] {
				drop = append(drop, alias)
			}
		}
		if len(drop) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM performer_alias WHERE performer_id = $1 AND alias = ANY($2)`, id, drop); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to remove aliases")
				return
			}
		}
		if err := namer.RebuildPerformerMetadata(ctx, tx, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update the performer")
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": "matched", "provider": req.Provider, "linksRemaining": len(kept)})
		return
	}

	// The aliases came from the rejected profile, so they go with it.
	var stored struct {
		Aliases []string `json:"aliases"`
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &stored)
	}
	if len(stored.Aliases) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM performer_alias WHERE performer_id = $1 AND alias = ANY($2)`, id, stored.Aliases); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove aliases")
			return
		}
	}
	status := "unmatched"
	if req.MarkWrong {
		status = "rejected"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE performer SET remote_metadata = NULL, matched_at = NULL,
			match_status = $2, updated_at = now()
		WHERE id = $1
	`, id, status); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the match")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	if req.RemovePhoto { // photos from the unlinked providers; uploads stay
		providers := make([]string, 0, len(removed))
		for _, l := range removed {
			providers = append(providers, l.provider)
		}
		if len(kept) == 0 { // photos saved before galleries recorded their provider
			providers = append(providers, "provider")
		}
		if _, err := a.removePerformerPhotos(ctx, id, `pp.source = ANY($2)`, providers); err != nil {
			log.Printf("unmatch performer %d: remove photos: %v", id, err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": status})
}

// effectiveMatchStatus treats a title whose metadata came from an NFO as
// matched even when it was matched before scene ids were recorded.
const effectiveMatchStatus = `CASE WHEN match_status = 'unmatched' AND metadata_source = 'NFO' THEN 'matched' ELSE match_status END`

// mediaMatchState reports whether a title is matched, or flagged as wrongly matched.
func (a *API) mediaMatchState(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	a.importNFOLinksIfMissing(r, id)
	var status string
	var rejected int
	var links, suggestions []byte
	err = a.DB.QueryRow(r.Context(), `
		SELECT `+effectiveMatchStatus+`,
		       (SELECT COUNT(*) FROM media_rejected_match WHERE media_item_id = $1),
		       COALESCE((SELECT json_agg(json_build_object('provider', provider, 'externalId', external_id) ORDER BY matched_at)
		                 FROM match_link WHERE kind = 'media' AND entity_id = $1), '[]'::json),
		       COALESCE((SELECT candidates FROM media_match_suggestion WHERE media_item_id = $1), '[]'::jsonb)
		FROM media_item WHERE id = $1
	`, id).Scan(&status, &rejected, &links, &suggestions)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load match state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "rejectedScenes": rejected, "links": json.RawMessage(links), "suggestions": json.RawMessage(suggestions)})
}

// unmatchMedia undoes a provider match on a title: the scene's title, studio,
// description, date, performers, categories and poster are cleared, the title
// goes back to its file name, and the NFO is set aside as <name>.nfo.wrong-match
// so a rescan cannot import it again. The file goes back to the name it had before
// its first match, when that is known.
func (a *API) unmatchMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req unmatchRequest
	_ = decodeJSON(r, &req)
	ctx := r.Context()

	type fileRow struct {
		id            int64
		mount, rel    string
		fileName, ext string
		original      string
	}
	rows, err := a.DB.Query(ctx, `
		SELECT mf.id, sl.mount_path, mf.relative_path, mf.file_name, mf.extension, COALESCE(mf.original_file_name, '')
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = $1
		ORDER BY mf.available DESC, mf.height DESC NULLS LAST, mf.file_size DESC
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load files")
		return
	}
	var files []fileRow
	for rows.Next() {
		var f fileRow
		if rows.Scan(&f.id, &f.mount, &f.rel, &f.fileName, &f.ext, &f.original) == nil {
			files = append(files, f)
		}
	}
	rows.Close()
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}
	titleSource := files[0].fileName
	if files[0].original != "" {
		titleSource = files[0].original
	}
	title := strings.TrimSuffix(titleSource, filepath.Ext(titleSource))
	if title == "" {
		title = titleSource
	}

	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start")
		return
	}
	defer tx.Rollback(ctx)

	var posterID *int64
	var current string
	if err := tx.QueryRow(ctx, `SELECT poster_image_id, `+effectiveMatchStatus+` FROM media_item WHERE id = $1 FOR UPDATE`, id).Scan(&posterID, &current); err != nil {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}

	// Which provider links this request removes, and how many stay.
	type link struct{ provider, externalID string }
	var removed []link
	remaining := 0
	linkRows, err := tx.Query(ctx, `SELECT provider, external_id FROM match_link WHERE kind = 'media' AND entity_id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load links")
		return
	}
	for linkRows.Next() {
		var l link
		if linkRows.Scan(&l.provider, &l.externalID) != nil {
			continue
		}
		if req.Provider == "" || l.provider == req.Provider {
			removed = append(removed, l)
		} else {
			remaining++
		}
	}
	linkRows.Close()
	if req.Provider != "" && len(removed) == 0 {
		writeError(w, http.StatusNotFound, "this title is not linked to that provider")
		return
	}
	if req.MarkWrong {
		for _, l := range removed {
			if _, err := tx.Exec(ctx, `INSERT INTO media_rejected_match (media_item_id, provider, scene_uuid) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, l.provider, l.externalID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record the wrong match")
				return
			}
		}
	}
	for _, l := range removed {
		if _, err := tx.Exec(ctx, `DELETE FROM match_link WHERE kind = 'media' AND entity_id = $1 AND provider = $2`, id, l.provider); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove the link")
			return
		}
	}
	if remaining > 0 {
		// Another provider still matches this title, so its metadata stays; only
		// the one provider's link is gone.
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": "matched", "provider": req.Provider, "linksRemaining": remaining})
		return
	}
	if current != "matched" {
		// Nothing was matched, so only the wrong-match flag changes and the
		// title's own metadata is left alone.
		status := "unmatched"
		if req.MarkWrong {
			status = "rejected"
		}
		if _, err := tx.Exec(ctx, `UPDATE media_item SET match_status = $2 WHERE id = $1`, id, status); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update the flag")
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": status})
		return
	}
	status := "unmatched"
	if req.MarkWrong {
		status = "rejected"
	}
	var posterKey string
	posterKept := false
	if posterID != nil {
		var posterCustom bool
		_ = tx.QueryRow(ctx, `SELECT storage_key, custom FROM artwork WHERE id = $1 AND kind = 'POSTER'`, *posterID).Scan(&posterKey, &posterCustom)
		if posterCustom { // a poster the user set by hand stays
			posterKey, posterID, posterKept = "", nil, true
		}
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM media_item_performer WHERE media_item_id = $1`, []any{id}},
		{`DELETE FROM media_item_category WHERE media_item_id = $1`, []any{id}},
		{`UPDATE media_item SET title = $2, sort_title = $2, description = NULL, studio_id = NULL,
			release_date = NULL, production_year = NULL, metadata_source = 'FILENAME',
			match_status = $3, poster_image_id = CASE WHEN $4::boolean THEN poster_image_id ELSE NULL END, updated_at = now()
			WHERE id = $1`, []any{id, title, status, posterKept}},
		{`DELETE FROM nfo_metadata WHERE media_file_id IN (SELECT id FROM media_file WHERE media_item_id = $1)`, []any{id}},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			log.Printf("unmatch media %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "failed to remove the match")
			return
		}
	}
	if posterKey != "" {
		_, _ = tx.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, *posterID)
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	if posterKey != "" && filepath.Base(posterKey) == posterKey {
		os.Remove(filepath.Join(a.Cfg.PosterDir, posterKey))
	}
	setAside, failed, restored := 0, 0, 0
	for _, f := range files {
		abs := filepath.Join(f.mount, filepath.FromSlash(f.rel))
		if nfo, ok := scanner.FindAdjacentNFO(abs); ok {
			if err := os.Rename(nfo, nfo+".wrong-match"); err != nil {
				failed++
			} else {
				setAside++
			}
		}
		// Put the file back under the name it came with.
		if f.original == "" || f.original == f.fileName {
			continue
		}
		target := filepath.Join(filepath.Dir(abs), f.original)
		if _, err := os.Stat(target); err == nil {
			log.Printf("unmatch media %d: not restoring %s, %s already exists", id, f.original, target)
			continue
		}
		if err := os.Rename(abs, target); err != nil {
			log.Printf("unmatch media %d: restore name: %v", id, err)
			continue
		}
		if err := a.Scanner.ReconcileRenamedFile(ctx, f.id, target, ""); err != nil {
			log.Printf("unmatch media %d: reconcile restored name: %v", id, err)
			continue
		}
		restored++
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": status, "nfoSetAside": setAside, "nfoFailed": failed, "namesRestored": restored})
}

// unmatchStudio removes a studio's provider match and the details that came
// with it, returning it to "not checked". With removePhoto the logo goes too,
// unless the user set it by hand.
func (a *API) unmatchStudio(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	var req unmatchRequest
	_ = decodeJSON(r, &req)
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start")
		return
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `SELECT id FROM studio WHERE id = $1 FOR UPDATE`, id).Scan(&id); err != nil {
		writeError(w, http.StatusNotFound, "studio not found")
		return
	}
	removed, kept, err := splitLinks(ctx, tx, "studio", id, req.Provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load links")
		return
	}
	if req.Provider != "" && len(removed) == 0 {
		writeError(w, http.StatusNotFound, "this studio is not linked to that provider")
		return
	}
	for _, l := range removed {
		if _, err := tx.Exec(ctx, `DELETE FROM match_link WHERE kind = 'studio' AND entity_id = $1 AND provider = $2`, id, l.provider); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove the link")
			return
		}
	}
	if len(kept) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE studio SET updated_at = now(),
				remote_metadata = COALESCE((SELECT remote_metadata FROM match_link
					WHERE kind = 'studio' AND entity_id = $1 AND remote_metadata IS NOT NULL
					ORDER BY matched_at DESC LIMIT 1), remote_metadata)
			WHERE id = $1
		`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update the studio")
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": "matched", "provider": req.Provider, "linksRemaining": len(kept)})
		return
	}
	if _, err := tx.Exec(ctx, `
		UPDATE studio SET remote_metadata = NULL, matched_at = NULL,
			match_status = 'unmatched', updated_at = now()
		WHERE id = $1
	`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the match")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	if req.RemovePhoto {
		var custom bool
		_ = a.DB.QueryRow(ctx, `SELECT COALESCE(ar.custom, false) FROM studio s JOIN artwork ar ON ar.id = s.logo_image_id WHERE s.id = $1`, id).Scan(&custom)
		if !custom {
			var imageID *int64
			var key string
			if err := a.DB.QueryRow(ctx, `SELECT s.logo_image_id, COALESCE(ar.storage_key, '') FROM studio s LEFT JOIN artwork ar ON ar.id = s.logo_image_id WHERE s.id = $1`, id).Scan(&imageID, &key); err == nil && imageID != nil {
				a.DB.Exec(ctx, `UPDATE studio SET logo_image_id = NULL, updated_at = now() WHERE id = $1`, id)
				a.DB.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, *imageID)
				if key != "" && filepath.Base(key) == key {
					os.Remove(filepath.Join(a.Cfg.PosterDir, key))
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "matchStatus": "unmatched"})
}

// importNFOLinksIfMissing reads provider ids (e.g. from a Jellyfin match) out
// of a title's NFO when it has no link yet, so they show up without waiting
// for a rescan. Existing links are never replaced.
func (a *API) importNFOLinksIfMissing(r *http.Request, id int64) {
	var mount, rel string
	var fileID int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT mf.id, sl.mount_path, mf.relative_path
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = $1 AND mf.available
		  AND NOT EXISTS (SELECT 1 FROM match_link WHERE kind = 'media' AND entity_id = $1)
		ORDER BY mf.height DESC NULLS LAST, mf.file_size DESC LIMIT 1
	`, id).Scan(&fileID, &mount, &rel)
	if err != nil {
		return
	}
	if nfo, ok := scanner.FindAdjacentNFO(filepath.Join(mount, filepath.FromSlash(rel))); ok {
		if err := scanner.RefreshNFOLinksFromFile(r.Context(), a.DB, fileID, id, nfo); err != nil {
			log.Printf("media %d: import NFO provider links: %v", id, err)
		}
	}
}

// mediaLinkedRecords returns the provider record behind each of a title's links.
func (a *API) mediaLinkedRecords(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	if a.Namer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := a.Namer.LinkedRecords(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load linked records")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

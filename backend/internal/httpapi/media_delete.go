package httpapi

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"reelstash/backend/internal/scanner"
)

// deleteMediaItem permanently removes a title: its video files (and adjacent
// NFOs) are deleted from disk, then the library entry and its uploaded poster
// go with them. Admin-only; the UI asks for confirmation first.
func (a *API) deleteMediaItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	ctx := r.Context()

	rows, err := a.DB.Query(ctx, `
		SELECT sl.mount_path, mf.relative_path
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = $1
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load files")
		return
	}
	var paths []string
	for rows.Next() {
		var mount, rel string
		if rows.Scan(&mount, &rel) == nil {
			paths = append(paths, filepath.Join(mount, filepath.FromSlash(rel)))
		}
	}
	rows.Close()
	for _, p := range paths {
		if a.refuseChange(w, r, p) {
			return
		}
	}

	var posterID, backdropID *int64
	var posterKey, backdropKey string
	if err := a.DB.QueryRow(ctx, `SELECT poster_image_id, backdrop_image_id FROM media_item WHERE id = $1`, id).Scan(&posterID, &backdropID); err != nil {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}
	if posterID != nil {
		_ = a.DB.QueryRow(ctx, `SELECT storage_key FROM artwork WHERE id = $1`, *posterID).Scan(&posterKey)
	}
	if backdropID != nil {
		_ = a.DB.QueryRow(ctx, `SELECT storage_key FROM artwork WHERE id = $1`, *backdropID).Scan(&backdropKey)
	}

	for _, p := range paths {
		if nfo, ok := scanner.FindAdjacentNFO(p); ok {
			if err := os.Remove(nfo); err != nil && !os.IsNotExist(err) {
				log.Printf("delete media %d: remove nfo %s: %v", id, nfo, err)
			}
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusInternalServerError, "failed to delete "+filepath.Base(p)+": "+err.Error())
			return
		}
	}

	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM media_file WHERE media_item_id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove files from the library")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM media_item WHERE id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the title")
		return
	}
	if posterID != nil {
		_, _ = tx.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, *posterID)
	}
	if backdropID != nil {
		_, _ = tx.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, *backdropID)
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	if posterKey != "" && filepath.Base(posterKey) == posterKey {
		os.Remove(filepath.Join(a.Cfg.PosterDir, posterKey))
	}
	if backdropKey != "" && filepath.Base(backdropKey) == backdropKey {
		os.Remove(filepath.Join(a.Cfg.PosterDir, backdropKey))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

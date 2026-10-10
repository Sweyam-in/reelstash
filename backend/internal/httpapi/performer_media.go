package httpapi

import (
	"net/http"
	"path/filepath"

	"reelstash/backend/internal/scanner"
)

type performerTitle struct {
	ID              int64    `json:"id"`
	Title           string   `json:"title"`
	StudioName      *string  `json:"studioName,omitempty"`
	DurationSeconds *float64 `json:"durationSeconds,omitempty"`
}

func (a *API) listPerformerMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT mi.id, mi.title, s.name, mi.duration_seconds
		FROM media_item_performer mip
		JOIN media_item mi ON mi.id = mip.media_item_id
		LEFT JOIN studio s ON s.id = mi.studio_id
		WHERE mip.performer_id = $1
		ORDER BY mi.title
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load titles")
		return
	}
	defer rows.Close()
	items := []performerTitle{}
	for rows.Next() {
		var t performerTitle
		if err := rows.Scan(&t.ID, &t.Title, &t.StudioName, &t.DurationSeconds); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read title")
			return
		}
		items = append(items, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// nfoPathsForItem lists the NFO sidecars that exist next to a title's files.
func (a *API) nfoPathsForItem(r *http.Request, mediaItemID int64) []string {
	rows, err := a.DB.Query(r.Context(), `
		SELECT sl.mount_path, mf.relative_path
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = $1
	`, mediaItemID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var mount, rel string
		if rows.Scan(&mount, &rel) != nil {
			continue
		}
		if nfo, ok := scanner.FindAdjacentNFO(filepath.Join(mount, filepath.FromSlash(rel))); ok {
			paths = append(paths, nfo)
		}
	}
	return paths
}

// linkPerformerMedia attaches a title to a performer and adds them to the
// title's NFO when it has one.
func (a *API) linkPerformerMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
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
	if err := a.DB.QueryRow(r.Context(), `SELECT name FROM performer WHERE id = $1`, id).Scan(&name); err != nil {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO media_item_performer (media_item_id, performer_id, position)
		SELECT $1, $2, COALESCE(MAX(position) + 1, 0) FROM media_item_performer WHERE media_item_id = $1
		ON CONFLICT DO NOTHING
	`, req.MediaItemID, id); err != nil {
		writeError(w, http.StatusBadRequest, "title not found")
		return
	}
	failed := 0
	for _, path := range a.nfoPathsForItem(r, req.MediaItemID) {
		if _, err := scanner.AddNFOPerformer(path, name); err != nil {
			failed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nfoFailed": failed})
}

// unlinkPerformerMedia detaches a title from a performer and removes them from the title's NFO.
func (a *API) unlinkPerformerMedia(w http.ResponseWriter, r *http.Request) {
	id, err1 := pathInt64(r, "id")
	itemID, err2 := pathInt64(r, "mediaItemId")
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var name string
	if err := a.DB.QueryRow(r.Context(), `SELECT name FROM performer WHERE id = $1`, id).Scan(&name); err != nil {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM media_item_performer WHERE performer_id = $1 AND media_item_id = $2`, id, itemID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink title")
		return
	}
	failed := 0
	for _, path := range a.nfoPathsForItem(r, itemID) {
		if _, err := scanner.RemoveNFOPerformer(path, name); err != nil {
			failed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nfoFailed": failed})
}

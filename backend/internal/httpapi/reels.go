package httpapi

import (
	"net/http"
)

// reelDetected is true when a title's video is portrait. The file judged is
// the one playback picks (resolveMediaFile), so a reel always plays vertical;
// the EXISTS check lets the partial index skip most titles cheaply.
func reelDetected(alias string) string {
	return `(CASE WHEN NOT EXISTS (SELECT 1 FROM media_file rf WHERE rf.media_item_id = ` + alias + `.id AND rf.portrait AND rf.available) THEN FALSE
		ELSE COALESCE((SELECT pf.portrait FROM media_file pf WHERE pf.media_item_id = ` + alias + `.id AND pf.available
			ORDER BY pf.height DESC NULLS LAST, pf.file_size DESC LIMIT 1), FALSE) END)`
}

// reelCond is true for titles shown under Reels (they stay in the library
// too): the admin's choice when set, otherwise the detected shape.
func reelCond(alias string) string {
	return `(CASE WHEN ` + alias + `.reel_override IS NOT NULL THEN ` + alias + `.reel_override ELSE ` + reelDetected(alias) + ` END)`
}

// setMediaReel sets or clears (null) the admin's reel override for a title.
func (a *API) setMediaReel(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req struct {
		Override *bool `json:"override"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `UPDATE media_item SET reel_override = $2, updated_at = now() WHERE id = $1`, id, req.Override)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update title")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "media item not found")
		return
	}
	m, err := a.loadMediaItemDetail(r, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load media item")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

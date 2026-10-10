package httpapi

import (
	"net/http"
)

// The Watch later list itself is served by GET /api/media?watchLater=true so
// it pages, filters and sorts like every other title list.

func watchLaterID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathInt64(r, "mediaItemID")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return 0, false
	}
	return id, true
}

func (a *API) addWatchLater(w http.ResponseWriter, r *http.Request) {
	id, ok := watchLaterID(w, r)
	if !ok {
		return
	}
	user, _ := UserFromContext(r.Context())
	tag, err := a.DB.Exec(r.Context(), `
		INSERT INTO watch_later (user_id, media_item_id)
		SELECT $1, mi.id FROM media_item mi WHERE mi.id = $2
		ON CONFLICT DO NOTHING
	`, user.ID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save to watch later")
		return
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		_ = a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM media_item WHERE id = $1)`, id).Scan(&exists)
		if !exists {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"watchLater": true})
}

func (a *API) removeWatchLater(w http.ResponseWriter, r *http.Request) {
	id, ok := watchLaterID(w, r)
	if !ok {
		return
	}
	user, _ := UserFromContext(r.Context())
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM watch_later WHERE user_id = $1 AND media_item_id = $2`, user.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove from watch later")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"watchLater": false})
}

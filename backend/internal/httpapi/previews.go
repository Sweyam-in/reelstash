package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
)

// mediaPreview serves the short hover-preview clip for the title's video.
func (a *API) mediaPreview(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	var key string
	err := a.DB.QueryRow(r.Context(), `SELECT storage_key FROM preview_clip WHERE media_file_id = $1`, src.FileID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load preview")
		return
	}
	serveStoredFile(w, r, a.Cfg.PreviewDir, key, "video/mp4")
}

// requestPreview queues building the hover preview if it is missing.
func (a *API) requestPreview(w http.ResponseWriter, r *http.Request) {
	a.requestGenerated(w, r, "PREVIEW", func(fileID int64) (bool, error) {
		var exists bool
		err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM preview_clip WHERE media_file_id = $1)`, fileID).Scan(&exists)
		return exists, err
	})
}

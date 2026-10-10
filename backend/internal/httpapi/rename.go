package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"

	"reelstash/backend/internal/rename"
)

// listRenameQueue is the Admin "Rename Pipeline" queue: every available
// media file whose current name doesn't match what the active template
// would produce from its (NFO/manual) metadata. ?libraryId= narrows it to
// one library and uses that library's template.
func (a *API) listRenameQueue(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r)
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("libraryId"), 10, 64)
	template := rename.Template(r.Context(), a.DB, libraryID, a.Cfg.RenameTemplate)
	pending, err := rename.PendingQueue(r.Context(), a.DB, libraryID, a.Cfg.RenameTemplate, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build rename queue")
		return
	}
	if pending == nil {
		pending = []rename.Target{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": pending, "template": template})
}

func (a *API) previewRename(w http.ResponseWriter, r *http.Request) {
	mediaFileID, err := pathInt64(r, "mediaFileID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media file id")
		return
	}
	target, err := rename.Preview(r.Context(), a.DB, rename.FileTemplate(r.Context(), a.DB, mediaFileID, a.Cfg.RenameTemplate), mediaFileID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "media file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to preview rename")
		return
	}
	writeJSON(w, http.StatusOK, target)
}

// applyRename performs the actual on-disk rename plus the media_file
// update. It never overwrites an existing file at the target path and
// never moves a file to a different storage_location.
func (a *API) applyRename(w http.ResponseWriter, r *http.Request) {
	mediaFileID, err := pathInt64(r, "mediaFileID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media file id")
		return
	}
	target, err := rename.Apply(r.Context(), a.DB, rename.FileTemplate(r.Context(), a.DB, mediaFileID, a.Cfg.RenameTemplate), mediaFileID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "media file not found")
			return
		}
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, target)
}

type reviewItem struct {
	MediaItemID int64  `json:"mediaItemId"`
	Title       string `json:"title"`
	FileCount   int64  `json:"fileCount"`
}

// listReviewQueue surfaces media items whose metadata was only ever
// inferred from a filename — nothing has confirmed it's correct yet.
// Confirming moves the item to MANUAL, which also protects it from being
// silently overwritten by a later NFO import.
func (a *API) listReviewQueue(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("libraryId"), 10, 64)
	rows, err := a.DB.Query(r.Context(), `
		SELECT mi.id, mi.title, COUNT(mf.id)
		FROM media_item mi
		LEFT JOIN media_file mf ON mf.media_item_id = mi.id AND mf.available
		WHERE mi.metadata_source = 'FILENAME' AND ($1::bigint = 0 OR mi.library_id = $1)
		GROUP BY mi.id
		ORDER BY mi.date_added DESC
		LIMIT 100
	`, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load review queue")
		return
	}
	defer rows.Close()

	items := []reviewItem{}
	for rows.Next() {
		var it reviewItem
		if err := rows.Scan(&it.MediaItemID, &it.Title, &it.FileCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read review item")
			return
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) confirmReview(w http.ResponseWriter, r *http.Request) {
	mediaItemID, err := pathInt64(r, "mediaItemID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE media_item SET metadata_source = 'MANUAL', updated_at = now() WHERE id = $1
	`, mediaItemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm media item")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "media item not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

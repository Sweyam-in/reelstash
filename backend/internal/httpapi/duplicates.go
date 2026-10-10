package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
)

type duplicateFile struct {
	MediaFileID     int64    `json:"mediaFileId"`
	MediaItemID     int64    `json:"mediaItemId"`
	MediaItemTitle  string   `json:"mediaItemTitle"`
	StorageLocation string   `json:"storageLocation"`
	RelativePath    string   `json:"relativePath"`
	FileSize        int64    `json:"fileSize"`
	Width           *int     `json:"width,omitempty"`
	Height          *int     `json:"height,omitempty"`
	VideoCodec      *string  `json:"videoCodec,omitempty"`
	DurationSeconds *float64 `json:"durationSeconds,omitempty"`
}

type duplicateGroup struct {
	ID         int64           `json:"id"`
	Kind       string          `json:"kind"`
	Confidence *float64        `json:"confidence,omitempty"`
	Files      []duplicateFile `json:"files"`
}

// listDuplicateGroups never deletes anything — the Duplicate Center is a
// review surface only; destructive file actions stay separate admin
// operations that a human must confirm.
//
// With ?libraryId= only the files of that library are shown, and only groups
// that still have two or more of them.
func (a *API) listDuplicateGroups(w http.ResponseWriter, r *http.Request) {
	libraryID, _ := strconv.ParseInt(r.URL.Query().Get("libraryId"), 10, 64)
	rows, err := a.DB.Query(r.Context(), `
		SELECT g.id, g.kind, g.confidence FROM duplicate_group g
		WHERE NOT g.dismissed
		  AND ($1::bigint = 0 OR (
		      SELECT COUNT(*) FROM duplicate_group_file dgf JOIN media_file mf ON mf.id = dgf.media_file_id
		      WHERE dgf.duplicate_group_id = g.id AND mf.library_id = $1) >= 2)
		ORDER BY g.id DESC
		LIMIT 100
	`, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load duplicate groups")
		return
	}
	defer rows.Close()

	groups := []duplicateGroup{}
	groupIndex := map[int64]*duplicateGroup{}
	for rows.Next() {
		var g duplicateGroup
		if err := rows.Scan(&g.ID, &g.Kind, &g.Confidence); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read duplicate group")
			return
		}
		g.Files = []duplicateFile{}
		groups = append(groups, g)
	}
	for i := range groups {
		groupIndex[groups[i].ID] = &groups[i]
	}
	if len(groups) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"items": groups})
		return
	}

	ids := make([]int64, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}

	fileRows, err := a.DB.Query(r.Context(), `
		SELECT dgf.duplicate_group_id, mf.id, mf.media_item_id, mi.title, sl.name, mf.relative_path,
		       mf.file_size, mf.width, mf.height, mf.video_codec, mf.duration_seconds
		FROM duplicate_group_file dgf
		JOIN media_file mf ON mf.id = dgf.media_file_id
		JOIN media_item mi ON mi.id = mf.media_item_id
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE dgf.duplicate_group_id = ANY($1) AND ($2::bigint = 0 OR mf.library_id = $2)
	`, ids, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load duplicate files")
		return
	}
	defer fileRows.Close()

	for fileRows.Next() {
		var groupID int64
		var f duplicateFile
		if err := fileRows.Scan(&groupID, &f.MediaFileID, &f.MediaItemID, &f.MediaItemTitle, &f.StorageLocation,
			&f.RelativePath, &f.FileSize, &f.Width, &f.Height, &f.VideoCodec, &f.DurationSeconds); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read duplicate file")
			return
		}
		if g, ok := groupIndex[groupID]; ok {
			g.Files = append(g.Files, f)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": groups})
}

func (a *API) dismissDuplicateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid duplicate group id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `UPDATE duplicate_group SET dismissed = TRUE WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss duplicate group")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "duplicate group not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type duplicateScanJob struct {
	ID          int64   `json:"id"`
	Status      string  `json:"status"`
	Error       *string `json:"error,omitempty"`
	Phase       string  `json:"phase,omitempty"`
	Hashed      int     `json:"hashed"`
	Failed      int     `json:"failed"`
	Total       int     `json:"total"`
	CurrentPath string  `json:"currentPath,omitempty"`
}

func (a *API) duplicateScanStatus(w http.ResponseWriter, r *http.Request) {
	var job duplicateScanJob
	var result []byte
	err := a.DB.QueryRow(r.Context(), `
		SELECT id, status, error, result
		FROM background_job
		WHERE kind = 'DUPLICATE_SCAN'
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&job.ID, &job.Status, &job.Error, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load duplicate scan")
		return
	}
	if len(result) > 0 {
		_ = json.Unmarshal(result, &job)
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

// startDuplicateScan enqueues the optional full-file duplicate scan. Library
// scans do not do this work. A second scan is refused while one is already
// pending or running.
func (a *API) startDuplicateScan(w http.ResponseWriter, r *http.Request) {
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	var existingID int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'DUPLICATE_SCAN' AND status IN ('PENDING', 'RUNNING')
		LIMIT 1
	`).Scan(&existingID)
	if err == nil {
		writeError(w, http.StatusConflict, "a duplicate scan is already running")
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check for an in-progress duplicate scan")
		return
	}
	jobID, err := a.Jobs.Enqueue(r.Context(), "DUPLICATE_SCAN", map[string]any{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue duplicate scan")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

func (a *API) cancelDuplicateScan(w http.ResponseWriter, r *http.Request) {
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'DUPLICATE_SCAN' AND status IN ('PENDING', 'RUNNING')
		ORDER BY id
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up the duplicate scan")
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to look up the duplicate scan")
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up the duplicate scan")
		return
	}
	if len(ids) == 0 {
		writeError(w, http.StatusNotFound, "no duplicate scan is running")
		return
	}
	for _, id := range ids {
		if _, err := a.Jobs.Cancel(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to cancel duplicate scan: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

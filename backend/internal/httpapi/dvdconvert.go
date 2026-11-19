package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"reelstash/backend/internal/database"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/scanner"
)

// listDVDs returns VIDEO_TS folders a scan has recorded. Conversion is a
// separate request; this list does not start one.
func (a *API) listDVDs(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT d.id, d.storage_location_id, sl.name, sl.mount_path, d.relative_path, d.title_count, d.file_size
		FROM dvd_disc d
		JOIN storage_location sl ON sl.id = d.storage_location_id
		ORDER BY sl.name, d.relative_path
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list DVDs")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, storageID, size int64
		var titles int
		var name, mount, rel string
		if err := rows.Scan(&id, &storageID, &name, &mount, &rel, &titles, &size); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list DVDs")
			return
		}
		onDisk := false
		ready, total := 0, titles
		abs := filepath.Join(mount, filepath.FromSlash(rel))
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			onDisk = true
			if gotReady, gotTotal := scanner.DVDMP4Status(abs, mount); gotTotal > 0 {
				ready, total = gotReady, gotTotal
			}
		}
		items = append(items, map[string]any{
			"id":                id,
			"storageLocationId": storageID,
			"storageLocation":   name,
			"path":              rel,
			"name":              scanner.DVDMovieName(rel),
			"titles":            total,
			"size":              size,
			"mp4Ready":          ready,
			"mp4Total":          total,
			"onDisk":            onDisk,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list DVDs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// convertDVDs queues MP4 conversion for discs a scan has already recorded.
func (a *API) convertDVDs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "choose a DVD")
		return
	}
	ids := uniquePositive(req.IDs)
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "choose a DVD")
		return
	}
	if len(ids) > 50 {
		writeError(w, http.StatusBadRequest, "convert at most 50 DVDs at a time")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT id, storage_location_id, relative_path
		FROM dvd_disc WHERE id = ANY($1) AND available
	`, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load DVDs")
		return
	}
	defer rows.Close()
	found := map[int64]scanner.DVDConvertItem{}
	for rows.Next() {
		var id, storageID int64
		var rel string
		if err := rows.Scan(&id, &storageID, &rel); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load DVDs")
			return
		}
		found[id] = scanner.DVDConvertItem{StorageLocationID: storageID, Path: rel}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load DVDs")
		return
	}
	if len(found) != len(ids) {
		writeError(w, http.StatusNotFound, "that DVD was not found. Scan the disk again.")
		return
	}

	guard := pathguard.Load(r.Context(), a.DB)
	items := make([]scanner.DVDConvertItem, 0, len(ids))
	for _, id := range ids {
		item := found[id]
		mount, abs, err := resolveStorageIn(r.Context(), a.DB, item.StorageLocationID, item.Path)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "storage location not found")
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateStoragePath(mount, abs, false); err != nil {
			writeError(w, http.StatusNotFound, "that DVD is no longer on disk. Scan the disk again.")
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			writeError(w, http.StatusNotFound, "that DVD is no longer on disk. Scan the disk again.")
			return
		}
		videoDir, err := scanner.FindDVDVideo(abs)
		if err != nil {
			writeError(w, http.StatusBadRequest, scanner.DVDMovieName(item.Path)+" no longer has DVD video. Scan the disk again.")
			return
		}
		if blocked, why := guard.Blocked(videoDir); blocked {
			writeError(w, http.StatusConflict, why)
			return
		}
		items = append(items, item)
	}
	id, status, msg := a.enqueueDVDConvert(r.Context(), items)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": id})
}

func uniquePositive(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// enqueueDVDConvert queues one DVD_CONVERT. A second conversion, or a scan
// or organize on the same disks, is refused so the two cannot rewrite the
// same folder.
func (a *API) enqueueDVDConvert(ctx context.Context, items []scanner.DVDConvertItem) (int64, int, string) {
	if a.Jobs == nil {
		return 0, http.StatusServiceUnavailable, "job runner not available"
	}
	if len(items) == 0 {
		return 0, http.StatusBadRequest, "choose a VIDEO_TS folder"
	}
	if active, err := dvdConvertActive(ctx, a.DB); err != nil {
		return 0, http.StatusInternalServerError, "failed to check for a running conversion"
	} else if active {
		return 0, http.StatusConflict, "a DVD conversion is already queued or running"
	}
	if busy, err := dvdDisksBusy(ctx, a.DB, items); err != nil {
		return 0, http.StatusInternalServerError, "failed to check for a scan"
	} else if busy {
		return 0, http.StatusConflict, "a scan or the organizer is running; convert this DVD once it finishes"
	}
	id, err := a.Jobs.Enqueue(ctx, scanner.DVDConvertJobKind, scanner.DVDConvertPayload{Items: items})
	if err != nil {
		return 0, http.StatusInternalServerError, "failed to queue the conversion"
	}
	return id, 0, ""
}

func dvdConvertActive(ctx context.Context, db database.DB) (bool, error) {
	var active bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM background_job WHERE kind = 'DVD_CONVERT' AND status IN ('PENDING','RUNNING'))`).Scan(&active)
	return active, err
}

func dvdDisksBusy(ctx context.Context, db database.DB, items []scanner.DVDConvertItem) (bool, error) {
	ids := make([]int64, 0, len(items))
	seen := map[int64]bool{}
	for _, item := range items {
		if item.StorageLocationID > 0 && !seen[item.StorageLocationID] {
			seen[item.StorageLocationID] = true
			ids = append(ids, item.StorageLocationID)
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	var busy bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM scan_job WHERE completed_at IS NULL AND storage_location_id = ANY($1))
		    OR EXISTS (
		        SELECT 1 FROM background_job
		        WHERE kind = 'ORGANIZE' AND status IN ('PENDING','RUNNING')
		          AND payload->>'storageLocationId' = ANY (SELECT x::text FROM unnest($1::bigint[]) AS x)
		    )
	`, ids).Scan(&busy)
	return busy, err
}

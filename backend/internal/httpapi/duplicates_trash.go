package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type duplicateCandidate struct {
	fileID, itemID, locationID int64
	mount, rel, title          string
	height                     int
	bitrate, size              int64
}

// keepDuplicate moves every file in the group except the chosen one into
// .reelstash-trash on the same disk. The kept file stays where it is. A
// later restore puts a trashed file back; a library scan indexes it again.
func (a *API) keepDuplicate(w http.ResponseWriter, r *http.Request) {
	groupID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid duplicate group id")
		return
	}
	var req struct {
		MediaFileID int64 `json:"mediaFileId"`
	}
	_ = decodeJSON(r, &req)
	rows, err := a.DB.Query(r.Context(), `
		SELECT mf.id, mf.media_item_id, mf.storage_location_id, sl.mount_path, mf.relative_path, mi.title,
		       COALESCE(mf.height, 0), COALESCE(mf.overall_bitrate, 0), COALESCE(mf.file_size, 0)
		FROM duplicate_group_file dgf
		JOIN media_file mf ON mf.id = dgf.media_file_id
		JOIN media_item mi ON mi.id = mf.media_item_id
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE dgf.duplicate_group_id = $1
	`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the duplicate group")
		return
	}
	defer rows.Close()
	var files []duplicateCandidate
	for rows.Next() {
		var f duplicateCandidate
		if err := rows.Scan(&f.fileID, &f.itemID, &f.locationID, &f.mount, &f.rel, &f.title, &f.height, &f.bitrate, &f.size); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read the duplicate group")
			return
		}
		files = append(files, f)
	}
	if len(files) < 2 {
		writeError(w, http.StatusNotFound, "that duplicate group has nothing left to compare")
		return
	}
	keep := req.MediaFileID
	if keep == 0 {
		best := files[0]
		for _, f := range files[1:] {
			if f.height > best.height || (f.height == best.height && f.bitrate > best.bitrate) || (f.height == best.height && f.bitrate == best.bitrate && f.size > best.size) {
				best = f
			}
		}
		keep = best.fileID
	}
	found := false
	for _, f := range files {
		if f.fileID == keep {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusBadRequest, "that file is not in this duplicate group")
		return
	}
	moved := 0
	for _, f := range files {
		if f.fileID == keep {
			continue
		}
		if err := a.trashFile(r, f); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		moved++
	}
	_, _ = a.DB.Exec(r.Context(), `UPDATE duplicate_group SET dismissed = TRUE WHERE id = $1`, groupID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kept": keep, "trashed": moved})
}

func (a *API) trashFile(r *http.Request, f duplicateCandidate) error {
	src := filepath.Join(f.mount, filepath.FromSlash(f.rel))
	relTrash := filepath.ToSlash(filepath.Join(".reelstash-trash", fmt.Sprintf("%d", time.Now().UnixNano()), f.rel))
	dest := filepath.Join(f.mount, filepath.FromSlash(relTrash))
	mount := filepath.Clean(f.mount)
	if !strings.HasPrefix(filepath.Clean(dest), mount+string(os.PathSeparator)) || !strings.HasPrefix(filepath.Clean(src), mount+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to move %s outside its disk", f.rel)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("could not create the trash folder: %w", err)
	}
	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("could not move %s to trash: %w", f.rel, err)
	}
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO trash_item (storage_location_id, original_path, trash_path, title, file_size)
		VALUES ($1, $2, $3, $4, $5)
	`, f.locationID, f.rel, relTrash, f.title, f.size); err != nil {
		_ = os.Rename(dest, src)
		return fmt.Errorf("could not record the trash entry: %w", err)
	}
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM media_file WHERE id = $1`, f.fileID); err != nil {
		return err
	}
	_, _ = a.DB.Exec(r.Context(), `
		DELETE FROM media_item WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM media_file WHERE media_item_id = $1)
	`, f.itemID)
	return nil
}

func (a *API) listTrash(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT t.id, t.title, t.original_path, t.file_size, t.created_at, sl.name
		FROM trash_item t JOIN storage_location sl ON sl.id = t.storage_location_id
		ORDER BY t.id DESC LIMIT 200
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load trash")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, size int64
		var title, path, storage string
		var created time.Time
		if err := rows.Scan(&id, &title, &path, &size, &created, &storage); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read trash")
			return
		}
		items = append(items, map[string]any{"id": id, "title": title, "path": path, "fileSize": size, "createdAt": created, "storage": storage})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) restoreTrash(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "trashId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trash id")
		return
	}
	var mount, original, trashPath string
	err = a.DB.QueryRow(r.Context(), `
		SELECT sl.mount_path, t.original_path, t.trash_path
		FROM trash_item t JOIN storage_location sl ON sl.id = t.storage_location_id
		WHERE t.id = $1
	`, id).Scan(&mount, &original, &trashPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "that trash entry is gone")
		return
	}
	src := filepath.Join(mount, filepath.FromSlash(trashPath))
	dest := filepath.Join(mount, filepath.FromSlash(original))
	if _, err := os.Stat(dest); err == nil {
		writeError(w, http.StatusConflict, "a file is already at the original path")
		return
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "could not recreate the original folder")
		return
	}
	if err := os.Rename(src, dest); err != nil {
		writeError(w, http.StatusConflict, "the trashed file is no longer where ReelStash left it")
		return
	}
	_, _ = a.DB.Exec(r.Context(), `DELETE FROM trash_item WHERE id = $1`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": original})
}

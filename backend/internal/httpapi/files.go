package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/pathguard"
)

type fileEntry struct {
	Name        string `json:"name"`
	IsDir       bool   `json:"isDir"`
	Size        int64  `json:"size,omitempty"`
	ModTime     string `json:"modTime,omitempty"`
	MediaFileID *int64 `json:"mediaFileId,omitempty"`
	MediaItemID *int64 `json:"mediaItemId,omitempty"`
	// Protected is "locked" (inside the downloads in-progress folder) or
	// "torrent" (data qBittorrent is using); such entries can't be changed.
	Protected string `json:"protected,omitempty"`
}

func (a *API) resolveStorageDir(r *http.Request, storageLocationID int64, relPath string) (mountPath, absPath string, err error) {
	return resolveStorageIn(r.Context(), a.DB, storageLocationID, relPath)
}

// resolveStorageIn is resolveStorageDir for code without a request, such as
// background jobs.
func resolveStorageIn(ctx context.Context, db *pgxpool.Pool, storageLocationID int64, relPath string) (mountPath, absPath string, err error) {
	if err = db.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, storageLocationID).Scan(&mountPath); err != nil {
		return "", "", err
	}
	cleanRel := filepath.Clean("/" + relPath)
	absPath = filepath.Join(mountPath, cleanRel)
	if !isWithinDir(absPath, filepath.Clean(mountPath)) && absPath != filepath.Clean(mountPath) {
		return "", "", errors.New("path escapes storage location")
	}
	return mountPath, absPath, nil
}

// validateStoragePath checks resolved paths as well as lexical paths. When a
// caller will create a new leaf, its parent must already resolve inside root.
// errPathNotFound is validateStoragePath's answer for a target that does not exist.
var errPathNotFound = errors.New("path not found")

func validateStoragePath(mountPath, target string, allowMissingLeaf bool) error {
	root, err := filepath.EvalSymlinks(mountPath)
	if err != nil {
		return errors.New("storage location unavailable")
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err == nil {
		if !isWithinDir(resolved, root) {
			return errors.New("path escapes storage location")
		}
		return nil
	}
	if !allowMissingLeaf || !os.IsNotExist(err) {
		return errPathNotFound
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil || !isWithinDir(parent, root) {
		return errors.New("path escapes storage location")
	}
	return nil
}

// isStorageRoot reports whether absPath is the storage location's own root
// folder ("", ".", "/" and "a/.." all clean to it). Deleting, renaming or
// moving the root would take the whole disk mount with it, so those
// operations refuse it.
func isStorageRoot(mountPath, absPath string) bool {
	return filepath.Clean(absPath) == filepath.Clean(mountPath)
}

// listFileLocations exposes only the information needed to choose a library
// volume. Mount paths remain available only on the admin storage endpoint.
func (a *API) listFileLocations(w http.ResponseWriter, r *http.Request) {
	query := `SELECT id, name, type, online FROM storage_location`
	args := []any{}
	// Non-admins see only disks holding a folder of one of their libraries.
	if scope := scopeFromContext(r.Context()); !scope.All {
		query += ` WHERE id IN (SELECT storage_location_id FROM library_root WHERE library_id = ANY($1))`
		args = append(args, scope.Accessible)
	}
	rows, err := a.DB.Query(r.Context(), query+` ORDER BY name`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load storage locations")
		return
	}
	defer rows.Close()
	type location struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Online bool   `json:"online"`
	}
	items := []location{}
	for rows.Next() {
		var item location
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Online); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read storage location")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load storage locations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// listFiles is the File Browser's directory listing: filesystem-driven
// (unlike Library, which is metadata-driven), scoped to a registered
// storage_location so paths can never escape it.
func (a *API) listFiles(w http.ResponseWriter, r *http.Request) {
	storageLocationID, err := strconv.ParseInt(r.URL.Query().Get("storageLocationId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "storageLocationId is required")
		return
	}
	relPath := r.URL.Query().Get("path")

	mountPath, absPath, err := a.resolveStorageDir(r, storageLocationID, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStoragePath(mountPath, absPath, false); err != nil {
		writeError(w, http.StatusNotFound, "directory not found")
		return
	}
	// Outside a library folder a non-admin only sees the directories leading
	// into one.
	var via map[string]bool
	if roots, all := a.fileRoots(r, storageLocationID); !all {
		inside, leading := fileAccess(roots, cleanRelPath(relPath))
		if !inside {
			if len(leading) == 0 {
				writeError(w, http.StatusNotFound, "directory not found")
				return
			}
			via = leading
		}
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "directory not found")
		return
	}

	items := []fileEntry{}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && (via == nil || !via[e.Name()]) {
			paths = append(paths, filepath.ToSlash(filepath.Join(relPath, e.Name())))
		}
	}
	tracked := make(map[string][2]int64, len(paths))
	if len(paths) > 0 {
		rows, qerr := a.DB.Query(r.Context(), `SELECT relative_path, id, media_item_id FROM media_file WHERE storage_location_id = $1 AND relative_path = ANY($2)`, storageLocationID, paths)
		if qerr == nil {
			for rows.Next() {
				var path string
				var fileID, itemID int64
				if rows.Scan(&path, &fileID, &itemID) == nil {
					tracked[path] = [2]int64{fileID, itemID}
				}
			}
			rows.Close()
		}
	}
	guard := pathguard.Load(r.Context(), a.DB)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || via != nil && !(e.IsDir() && via[e.Name()]) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fe := fileEntry{Name: e.Name(), IsDir: e.IsDir(), ModTime: info.ModTime().UTC().Format(time.RFC3339),
			Protected: guardMark(guard, filepath.Join(absPath, e.Name()))}
		if !e.IsDir() {
			fe.Size = info.Size()

			childRel := filepath.ToSlash(filepath.Join(relPath, e.Name()))
			if ids, ok := tracked[childRel]; ok {
				mediaFileID, mediaItemID := ids[0], ids[1]
				fe.MediaFileID = &mediaFileID
				fe.MediaItemID = &mediaItemID
			}
		}
		items = append(items, fe)
	}

	writeJSON(w, http.StatusOK, map[string]any{"path": relPath, "items": items, "protected": guardMark(guard, absPath)})
}

// downloadFileEntry serves one authenticated file from a registered volume.
func (a *API) downloadFileEntry(w http.ResponseWriter, r *http.Request) {
	storageLocationID, err := strconv.ParseInt(r.URL.Query().Get("storageLocationId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "storageLocationId is required")
		return
	}
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if !a.fileAllowed(r, storageLocationID, relPath) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	mountPath, absPath, err := a.resolveStorageDir(r, storageLocationID, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resolvedRoot, err := filepath.EvalSymlinks(mountPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "storage location unavailable")
		return
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil || !isWithinDir(resolvedPath, resolvedRoot) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	info, err := os.Stat(resolvedPath)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(resolvedPath)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, resolvedPath)
}

type renameFileRequest struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"`
	NewName           string `json:"newName"`
}

// renameFileEntry renames a single filesystem entry in place (no template,
// unlike /api/admin/rename) — a file or a folder — and keeps any tracked
// media_file rows' relative_path/file_name in sync. Renaming a folder
// rewrites the relative_path prefix of every indexed file beneath it.
func (a *API) renameFileEntry(w http.ResponseWriter, r *http.Request) {
	var req renameFileRequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" || req.NewName == "" || strings.ContainsAny(req.NewName, "/\\") {
		writeError(w, http.StatusBadRequest, "path and a valid newName are required")
		return
	}

	mountPath, absPath, err := a.resolveStorageDir(r, req.StorageLocationID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStoragePath(mountPath, absPath, false); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPathNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	if isStorageRoot(mountPath, absPath) {
		writeError(w, http.StatusBadRequest, "the storage location's root folder can't be renamed")
		return
	}
	if a.refuseChange(w, r, absPath) {
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	newAbsPath := filepath.Join(filepath.Dir(absPath), req.NewName)
	if _, statErr := os.Stat(newAbsPath); statErr == nil {
		writeError(w, http.StatusConflict, "a file already exists with that name")
		return
	}
	if err := os.Rename(absPath, newAbsPath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rename: "+err.Error())
		return
	}

	oldRel := filepath.ToSlash(req.Path)
	newRel := filepath.ToSlash(filepath.Join(filepath.Dir(req.Path), req.NewName))
	if info.IsDir() {
		a.DB.Exec(r.Context(), `
			UPDATE media_file SET relative_path = $3 || substring(relative_path from length($2)+1), updated_at = now()
			WHERE storage_location_id = $1 AND (relative_path = $2 OR relative_path LIKE $2 || '/%')
		`, req.StorageLocationID, oldRel, newRel)
	} else {
		a.DB.Exec(r.Context(), `
			UPDATE media_file SET relative_path = $3, file_name = $4, updated_at = now()
			WHERE storage_location_id = $1 AND relative_path = $2
		`, req.StorageLocationID, oldRel, newRel, req.NewName)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "newPath": newRel})
}

type moveFileRequest struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"`     // source file or folder, relative
	DestPath          string `json:"destPath"` // destination folder, relative ("" = root)
	// DestStorageLocationID targets another storage location (another disk);
	// 0 means the same location as the source.
	DestStorageLocationID int64 `json:"destStorageLocationId,omitempty"`
}

// fileTransfer is a validated move/copy: the source entry and the folder it
// is going into, possibly on a different storage location.
type fileTransfer struct {
	srcLoc, destLoc int64
	absSrc          string
	absDestDir      string
	info            os.FileInfo
}

// resolveTransfer validates a move/copy request. verb ("moved"/"copied") is
// only used in error messages. On failure it has already written the error.
func (a *API) resolveTransfer(w http.ResponseWriter, r *http.Request, req moveFileRequest, verb string) (fileTransfer, bool) {
	destLoc := req.DestStorageLocationID
	if destLoc == 0 {
		destLoc = req.StorageLocationID
	}
	t, status, msg := checkTransfer(r.Context(), a.DB, req.StorageLocationID, req.Path, destLoc, req.DestPath, verb)
	if msg != "" {
		writeError(w, status, msg)
		return t, false
	}
	return t, true
}

// checkTransfer resolves and validates moving/copying srcRel on srcLoc into
// the folder destRel on destLoc. A non-empty msg is the reason it can't,
// with the HTTP status to report it under.
func checkTransfer(ctx context.Context, db *pgxpool.Pool, srcLoc int64, srcRel string, destLoc int64, destRel string, verb string) (t fileTransfer, status int, msg string) {
	t = fileTransfer{srcLoc: srcLoc, destLoc: destLoc}
	srcMount, absSrc, err := resolveStorageIn(ctx, db, srcLoc, srcRel)
	if err != nil {
		return t, http.StatusBadRequest, err.Error()
	}
	if isStorageRoot(srcMount, absSrc) {
		return t, http.StatusBadRequest, "the storage location's root folder can't be " + verb
	}
	destMount, absDestDir, err := resolveStorageIn(ctx, db, destLoc, destRel)
	if err != nil {
		return t, http.StatusBadRequest, "destination: " + err.Error()
	}
	if err := validateStoragePath(srcMount, absSrc, false); err != nil {
		return t, http.StatusBadRequest, err.Error()
	}
	if err := validateStoragePath(destMount, absDestDir, false); err != nil {
		return t, http.StatusBadRequest, "destination: " + err.Error()
	}
	info, err := os.Lstat(absSrc)
	if err != nil {
		return t, http.StatusNotFound, "file not found"
	}
	destInfo, err := os.Stat(absDestDir)
	if err != nil || !destInfo.IsDir() {
		return t, http.StatusBadRequest, "destination folder not found"
	}
	if info.IsDir() {
		// Compare resolved paths: two storage locations can share a disk or
		// even overlap, so a cross-location paste can still land inside itself.
		realSrc, _ := filepath.EvalSymlinks(absSrc)
		realDest, _ := filepath.EvalSymlinks(absDestDir)
		if realSrc != "" && realDest != "" && (realDest == realSrc || isWithinDir(realDest, realSrc)) {
			return t, http.StatusBadRequest, "a folder can't be " + verb + " into itself or one of its own subfolders"
		}
	}
	guard := pathguard.Load(ctx, db)
	if verb == "moved" {
		if blocked, why := guard.Blocked(absSrc); blocked {
			return t, http.StatusConflict, why
		}
	}
	if guard.Locked(absDestDir) {
		return t, http.StatusConflict, "the destination is inside the downloads in-progress folder, which ReelStash never changes"
	}
	t.absSrc, t.absDestDir, t.info = absSrc, absDestDir, info
	return t, 0, ""
}

// moveFileEntry relocates a file or folder to another folder, on the same
// storage location or a different one, keeping its name. Tracked media_file
// rows follow it (location and relative_path prefix), and the library
// triggers reassign their library.
func (a *API) moveFileEntry(w http.ResponseWriter, r *http.Request) {
	var req moveFileRequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	t, ok := a.resolveTransfer(w, r, req, "moved")
	if !ok {
		return
	}

	name := filepath.Base(t.absSrc)
	newAbsPath := filepath.Join(t.absDestDir, name)
	if newAbsPath == t.absSrc {
		writeError(w, http.StatusBadRequest, "the item is already in that folder")
		return
	}
	if _, statErr := os.Lstat(newAbsPath); statErr == nil {
		writeError(w, http.StatusConflict, "a file already exists with that name in the destination")
		return
	}
	if err := moveAcrossDevices(r.Context(), t.absSrc, newAbsPath, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move: "+err.Error())
		return
	}

	newRel := filepath.ToSlash(filepath.Join(req.DestPath, name))
	followMovedRows(r.Context(), a.DB, t, filepath.ToSlash(filepath.Clean(req.Path)), newRel)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "newPath": newRel})
}

// followMovedRows points tracked media_file rows at an entry's new place
// (location and relative_path prefix); the library triggers then reassign
// their library.
func followMovedRows(ctx context.Context, db *pgxpool.Pool, t fileTransfer, oldRel, newRel string) {
	if t.info.IsDir() {
		db.Exec(ctx, `
			UPDATE media_file SET storage_location_id = $4, relative_path = $3 || substring(relative_path from length($2)+1), updated_at = now()
			WHERE storage_location_id = $1 AND (relative_path = $2 OR relative_path LIKE $2 || '/%')
		`, t.srcLoc, oldRel, newRel, t.destLoc)
		return
	}
	db.Exec(ctx, `
		UPDATE media_file SET storage_location_id = $4, relative_path = $3, updated_at = now()
		WHERE storage_location_id = $1 AND relative_path = $2
	`, t.srcLoc, oldRel, newRel, t.destLoc)
}

// moveAcrossDevices renames src to dst, falling back to copy-then-delete when
// they sit on different filesystems (rename fails with EXDEV there). If the
// copy fails partway or ctx is cancelled, the partial copy is removed and src
// is left untouched. onBytes (optional) is told how much has been copied.
func moveAcrossDevices(ctx context.Context, src, dst string, onBytes func(int64)) error {
	err := os.Rename(src, dst)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(ctx, src, dst, true, onBytes); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// uniqueCopyName returns name if dir has no entry called that, else
// "name copy.ext", "name copy 2.ext", … so pasting never overwrites.
func uniqueCopyName(dir, name string) string {
	if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if ext == name { // dotfile like ".env"
		base, ext = name, ""
	}
	for i := 1; ; i++ {
		suffix := " copy"
		if i > 1 {
			suffix = " copy " + strconv.Itoa(i)
		}
		candidate := base + suffix + ext
		if _, err := os.Lstat(filepath.Join(dir, candidate)); err != nil {
			return candidate
		}
	}
}

// copyTree copies a file or folder. Symlinks are skipped rather than followed
// so a link can't smuggle in data from outside the storage location.
// keepTimes carries modification times over, so a cross-disk move looks
// unchanged to the scanner. Cancelling ctx stops it between reads; onBytes
// (optional) is told how many bytes each read copied.
func copyTree(ctx context.Context, src, dst string, keepTimes bool, onBytes func(int64)) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return nil
	case info.IsDir():
		if err := os.Mkdir(dst, info.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(ctx, filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), keepTimes, onBytes); err != nil {
				return err
			}
		}
		if keepTimes {
			os.Chtimes(dst, info.ModTime(), info.ModTime())
		}
		return nil
	case !info.Mode().IsRegular():
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.CopyBuffer(out, progressReader{ctx: ctx, r: in, onBytes: onBytes}, make([]byte, 1<<20)); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	if keepTimes {
		os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	return nil
}

// progressReader stops a copy once ctx is cancelled and reports each read.
type progressReader struct {
	ctx     context.Context
	r       io.Reader
	onBytes func(int64)
}

func (p progressReader) Read(buf []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(buf)
	if n > 0 && p.onBytes != nil {
		p.onBytes(int64(n))
	}
	return n, err
}

// copyFileEntry duplicates a file or folder into another folder, on the same
// storage location or a different one. A name clash gets a "copy" suffix
// instead of failing. The copy is not indexed here; the next scan picks it up.
func (a *API) copyFileEntry(w http.ResponseWriter, r *http.Request) {
	var req moveFileRequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	t, ok := a.resolveTransfer(w, r, req, "copied")
	if !ok {
		return
	}
	name := uniqueCopyName(t.absDestDir, filepath.Base(t.absSrc))
	if err := copyTree(r.Context(), t.absSrc, filepath.Join(t.absDestDir, name), false, nil); err != nil {
		os.RemoveAll(filepath.Join(t.absDestDir, name))
		writeError(w, http.StatusInternalServerError, "failed to copy: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "newPath": filepath.ToSlash(filepath.Join(req.DestPath, name))})
}

type createFileDirRequest struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"` // parent folder, relative ("" = root)
	Name              string `json:"name"`
}

// createFileDir makes a new, empty folder inside a registered volume.
func (a *API) createFileDir(w http.ResponseWriter, r *http.Request) {
	var req createFileDirRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || strings.ContainsAny(req.Name, "/\\") {
		writeError(w, http.StatusBadRequest, "a valid name is required")
		return
	}

	mountPath, parentAbs, err := a.resolveStorageDir(r, req.StorageLocationID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStoragePath(mountPath, parentAbs, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newAbs := filepath.Join(parentAbs, req.Name)
	if err := validateStoragePath(mountPath, newAbs, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.refuseLocked(w, r, newAbs) {
		return
	}
	if _, statErr := os.Stat(newAbs); statErr == nil {
		writeError(w, http.StatusConflict, "a file or folder already exists with that name")
		return
	}
	if err := os.Mkdir(newAbs, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder: "+err.Error())
		return
	}

	newRel := filepath.ToSlash(filepath.Join(req.Path, req.Name))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": newRel})
}

// uploadFiles saves one or more uploaded files into a folder in a registered
// volume. Existing files are never overwritten — a name collision skips
// that file rather than clobbering what's on disk.
func (a *API) uploadFiles(w http.ResponseWriter, r *http.Request) {
	const maxUploadBytes = 20 << 30 // 20 GiB per request
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload request (it may be too large)")
		return
	}

	storageLocationID, err := strconv.ParseInt(r.FormValue("storageLocationId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "storageLocationId is required")
		return
	}
	relPath := r.FormValue("path")

	mountPath, destDir, err := a.resolveStorageDir(r, storageLocationID, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStoragePath(mountPath, destDir, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if info, statErr := os.Stat(destDir); statErr != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "destination folder not found")
		return
	}
	if a.refuseLocked(w, r, destDir) {
		return
	}

	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, "no files were provided")
		return
	}

	uploaded := []string{}
	skipped := []string{}
	for _, fh := range headers {
		name := filepath.Base(filepath.Clean(fh.Filename))
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "\\") {
			skipped = append(skipped, fh.Filename)
			continue
		}
		destPath := filepath.Join(destDir, name)
		if err := validateStoragePath(mountPath, destPath, true); err != nil {
			skipped = append(skipped, name)
			continue
		}
		if !isWithinDir(destPath, destDir) {
			skipped = append(skipped, name)
			continue
		}
		src, err := fh.Open()
		if err != nil {
			skipped = append(skipped, name)
			continue
		}
		dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			src.Close()
			skipped = append(skipped, name)
			continue
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()
		if copyErr != nil {
			os.Remove(destPath)
			skipped = append(skipped, name)
			continue
		}
		uploaded = append(uploaded, name)
	}

	if len(uploaded) == 0 {
		writeError(w, http.StatusConflict, "no files were uploaded (names may already exist in this folder)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uploaded": uploaded, "skipped": skipped})
}

// deleteFileEntry permanently deletes a file, or a folder when
// recursive=true is passed, from disk. This is an explicit, admin-only,
// one-item-at-a-time action — never automatic (see
// the admin duplicate scan, which only ever flags duplicates for
// human review).
func (a *API) deleteFileEntry(w http.ResponseWriter, r *http.Request) {
	storageLocationID, err := strconv.ParseInt(r.URL.Query().Get("storageLocationId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "storageLocationId is required")
		return
	}
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	recursive := r.URL.Query().Get("recursive") == "true"

	mountPath, absPath, err := a.resolveStorageDir(r, storageLocationID, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStoragePath(mountPath, absPath, false); err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if isStorageRoot(mountPath, absPath) {
		writeError(w, http.StatusBadRequest, "the storage location's root folder can't be deleted")
		return
	}
	if a.refuseChange(w, r, absPath) {
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	cleanRel := filepath.ToSlash(relPath)
	if info.IsDir() {
		if !recursive {
			writeError(w, http.StatusBadRequest, "pass recursive=true to delete a non-empty folder")
			return
		}
		if err := os.RemoveAll(absPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete: "+err.Error())
			return
		}
		a.DB.Exec(r.Context(), `
			UPDATE media_file SET available = FALSE, missing_since = now()
			WHERE storage_location_id = $1 AND (relative_path = $2 OR relative_path LIKE $2 || '/%')
		`, storageLocationID, cleanRel)
	} else {
		if err := os.Remove(absPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete: "+err.Error())
			return
		}
		a.DB.Exec(r.Context(), `
			UPDATE media_file SET available = FALSE, missing_since = now()
			WHERE storage_location_id = $1 AND relative_path = $2
		`, storageLocationID, cleanRel)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// getFileNFO returns the raw contents of a file's adjacent .nfo, if any.
func (a *API) getFileNFO(w http.ResponseWriter, r *http.Request) {
	storageLocationID, err := strconv.ParseInt(r.URL.Query().Get("storageLocationId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "storageLocationId is required")
		return
	}
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	if !a.fileAllowed(r, storageLocationID, relPath) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	mountPath, absPath, err := a.resolveStorageDir(r, storageLocationID, relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	nfoPath := strings.TrimSuffix(absPath, filepath.Ext(absPath)) + ".nfo"
	if _, statErr := os.Lstat(nfoPath); os.IsNotExist(statErr) {
		writeJSON(w, http.StatusOK, map[string]any{"content": ""})
		return
	}
	if err := validateStoragePath(mountPath, nfoPath, false); err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	content, err := readNFO(nfoPath)
	if err != nil {
		if errors.Is(err, errNFOTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "NFO file is too large")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": string(content)})
}

type saveNFORequest struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"` // the media file; its adjacent .nfo is written
	Content           string `json:"content"`
}

// saveFileNFO overwrites (or creates) the .nfo adjacent to a media file.
func (a *API) saveFileNFO(w http.ResponseWriter, r *http.Request) {
	var req saveNFORequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path and content are required")
		return
	}
	mountPath, absPath, err := a.resolveStorageDir(r, req.StorageLocationID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	nfoPath := strings.TrimSuffix(absPath, filepath.Ext(absPath)) + ".nfo"
	if err := validateStoragePath(mountPath, nfoPath, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.refuseLocked(w, r, nfoPath) {
		return
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(nfoPath); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(nfoPath, []byte(req.Content), mode); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save NFO: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

const maxNFOBytes = 1 << 20

var errNFOTooLarge = errors.New("NFO file is too large")

func readNFO(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxNFOBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxNFOBytes {
		return nil, errNFOTooLarge
	}
	return b, nil
}

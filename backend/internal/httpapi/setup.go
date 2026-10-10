package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reelstash/backend/internal/scanner"
)

const setupSetting = "setup_completed"

// getSetupStatus tells the setup wizard where things stand. An install that
// already has a disk and a library counts as set up, so upgrades are never
// nagged; the wizard stays reachable for adding more.
func (a *API) getSetupStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var storages, libraries, scans, providers int
	_ = a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM storage_location`).Scan(&storages)
	_ = a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM library`).Scan(&libraries)
	_ = a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM scan_job WHERE completed_at IS NOT NULL`).Scan(&scans)
	if reg := a.providerRegistry(); reg != nil {
		for _, p := range reg.All() {
			if p.Enabled() {
				providers++
			}
		}
	}
	marked, _ := a.getSettingValue(w, r, setupSetting)
	writeJSON(w, http.StatusOK, map[string]any{
		"completed":     marked == "true" || (storages > 0 && libraries > 0),
		"storageCount":  storages,
		"libraryCount":  libraries,
		"scanCount":     scans,
		"providerCount": providers,
		"defaultMode":   "entertainment",
	})
}

// completeSetup records that the wizard was finished (or dismissed).
func (a *API) completeSetup(w http.ResponseWriter, r *http.Request) {
	if _, err := a.DB.Exec(r.Context(), `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, 'true', now())
		ON CONFLICT (key) DO UPDATE SET value = 'true', updated_at = now()
	`, setupSetting); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

const (
	previewMaxEntries = 20000
	previewMaxSamples = 5
	previewBudget     = 4 * time.Second
)

type pathCheck struct {
	Path       string   `json:"path"`
	Exists     bool     `json:"exists"`
	IsDir      bool     `json:"isDir"`
	Readable   bool     `json:"readable"`
	Error      string   `json:"error,omitempty"`
	VideoCount int      `json:"videoCount"`
	Truncated  bool     `json:"truncated"` // stopped counting early: there are at least VideoCount
	Samples    []string `json:"samples"`
	// ExistingLocationID is set when this exact path is already a storage location.
	ExistingLocationID *int64 `json:"existingLocationId,omitempty"`
	ExistingName       string `json:"existingName,omitempty"`
}

// checkSetupPath looks at a folder the admin typed, from the server's point of
// view (which, in Docker, is the path inside the container), and counts the
// videos a scan would find. Bounded in entries and time.
func (a *API) checkSetupPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	path := filepath.Clean(strings.TrimSpace(req.Path))
	if !filepath.IsAbs(path) {
		writeError(w, http.StatusBadRequest, "use the full path, starting with /")
		return
	}
	res := pathCheck{Path: path, Samples: []string{}}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		res.Error = "This folder does not exist where ReelStash runs. With Docker, use the path inside the container (the right-hand side of the volume)."
	case errors.Is(err, fs.ErrPermission):
		res.Exists = true
		res.Error = "ReelStash is not allowed to open this folder."
	case err != nil:
		res.Error = err.Error()
	default:
		res.Exists = true
		res.IsDir = info.IsDir()
		if !res.IsDir {
			res.Error = "This is a file, not a folder."
		}
	}
	if res.Exists && res.IsDir {
		if _, err := os.ReadDir(path); err != nil {
			res.Error = "ReelStash is not allowed to read this folder."
		} else {
			res.Readable = true
			countVideos(r.Context(), path, &res)
		}
	}
	var id int64
	var name string
	if err := a.DB.QueryRow(r.Context(), `SELECT id, name FROM storage_location WHERE mount_path = $1`, path).Scan(&id, &name); err == nil {
		res.ExistingLocationID, res.ExistingName = &id, name
	}
	writeJSON(w, http.StatusOK, res)
}

func countVideos(ctx context.Context, root string, res *pathCheck) {
	ctx, cancel := context.WithTimeout(ctx, previewBudget)
	defer cancel()
	entries := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			res.Truncated = true
			return fs.SkipAll
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && scanner.SkipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if entries++; entries > previewMaxEntries {
			res.Truncated = true
			return fs.SkipAll
		}
		if scanner.IsVideoExt(filepath.Ext(d.Name())) {
			res.VideoCount++
			if len(res.Samples) < previewMaxSamples {
				if rel, err := filepath.Rel(root, p); err == nil {
					res.Samples = append(res.Samples, filepath.ToSlash(rel))
				}
			}
		}
		return nil
	})
}

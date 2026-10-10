package housekeeping

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reelstash/backend/internal/scanner"
)

// Files beside videos that belong to one: subtitles, NFOs and Kodi/Jellyfin
// style "<video>-poster.jpg" images. Other images are left alone; they may
// be someone's photos.
var sidecarExts = map[string]bool{
	".nfo": true, ".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true, ".idx": true, ".sup": true,
}
var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".tbn": true}
var imageRoles = []string{"-poster", "-thumb", "-fanart", "-landscape", "-banner", "-clearlogo", "-backdrop", "-disc", "-clearart"}

// Folder-level metadata that does not name a video.
var folderMeta = map[string]bool{"movie.nfo": true, "tvshow.nfo": true, "season.nfo": true, "index.nfo": true}

// Folders holding a parent folder's subtitles.
var subtitleFolders = map[string]bool{"subs": true, "subtitles": true, "sub": true}

// Clutter operating systems leave in folders they browse.
func isJunk(name string) bool {
	switch strings.ToLower(name) {
	case ".ds_store", "thumbs.db", "ehthumbs.db", "desktop.ini":
		return true
	}
	return strings.HasPrefix(name, "._")
}

func isSidecar(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if sidecarExts[ext] {
		return true
	}
	if imageExts[ext] {
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		for _, role := range imageRoles {
			if strings.HasSuffix(stem, role) {
				return true
			}
		}
	}
	return false
}

// ownedBy reports whether a sidecar's name starts with one of stems followed
// by "." or "-" ("Film.en.srt", "Film-poster.jpg").
func ownedBy(name string, stems []string) bool {
	for _, s := range stems {
		if len(name) > len(s) && strings.HasPrefix(name, s) && (name[len(s)] == '.' || name[len(s)] == '-') {
			return true
		}
	}
	return false
}

type mediaFile struct {
	label, path string
	size        int64
}

// mediaWalk is one pass over every library folder, shared by the library tasks.
type mediaWalk struct {
	sidecars, junk []mediaFile
	skipped        []string
}

func (r *runner) walkLibraries() (*mediaWalk, error) {
	if r.media != nil {
		return r.media, nil
	}
	w := &mediaWalk{}
	rows, err := r.db().Query(r.ctx, `
		SELECT sl.id, sl.name, sl.mount_path, lr.path,
		       EXISTS (SELECT 1 FROM scan_job sj WHERE sj.storage_location_id = sl.id AND sj.completed_at IS NULL)
		       OR EXISTS (SELECT 1 FROM background_job bj WHERE bj.kind IN ('SCAN', 'ORGANIZE', 'MP4_REPAIR')
		                  AND bj.status IN ('PENDING', 'RUNNING')
		                  AND (bj.payload->>'storageLocationId' = sl.id::text OR bj.kind = 'MP4_REPAIR'))
		FROM library_root lr JOIN storage_location sl ON sl.id = lr.storage_location_id
		ORDER BY sl.id, lr.path`)
	if err != nil {
		return nil, err
	}
	type root struct {
		disk, mount, path string
		busy              bool
	}
	var roots []root
	for rows.Next() {
		var rt root
		var id int64
		if err := rows.Scan(&id, &rt.disk, &rt.mount, &rt.path, &rt.busy); err != nil {
			rows.Close()
			return nil, err
		}
		roots = append(roots, rt)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	busy := map[string]bool{}
	for _, rt := range roots {
		if rt.busy {
			if !busy[rt.disk] {
				w.skipped = append(w.skipped, fmt.Sprintf("%s: a scan or organizer run is in progress", rt.disk))
			}
			busy[rt.disk] = true
			continue
		}
		abs := filepath.Join(rt.mount, filepath.FromSlash(rt.path))
		if err := readableDir(abs); err != nil {
			w.skipped = append(w.skipped, fmt.Sprintf("%s: %s is not readable", rt.disk, abs))
			continue
		}
		if err := r.walkFolder(w, rt.disk, rt.mount, abs, nil, seen); err != nil {
			return nil, err
		}
	}
	r.media = w
	return w, nil
}

// walkFolder classifies one folder's files and descends. parentStems are the
// owning stems of the parent folder, used for Subs/ folders.
func (r *runner) walkFolder(w *mediaWalk, disk, mount, dir string, parentStems []string, seen map[string]bool) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if seen[dir] {
		return nil // nested library folders are walked once
	}
	seen[dir] = true
	// Torrent data and the downloads in-progress folder are left alone.
	if r.guard().Inside(dir) {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// Any file that is neither a sidecar nor clutter can own sidecars; that
	// covers videos in formats the scanner does not index too.
	var stems []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && !isSidecar(n) && !isJunk(n) {
			stems = append(stems, strings.TrimSuffix(n, filepath.Ext(n)))
		}
	}
	owners := stems
	if subtitleFolders[strings.ToLower(filepath.Base(dir))] {
		owners = append(append([]string{}, stems...), parentStems...)
	}
	label := func(p string) string {
		rel, err := filepath.Rel(mount, p)
		if err != nil {
			rel = p
		}
		return disk + ": " + filepath.ToSlash(rel)
	}
	hasVideo := false
	for _, e := range entries {
		if !e.IsDir() && scanner.IsVideoExt(filepath.Ext(e.Name())) {
			hasVideo = true
		}
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if scanner.SkipDir(name) {
				continue
			}
			if err := r.walkFolder(w, disk, mount, filepath.Join(dir, name), stems, seen); err != nil {
				return err
			}
			continue
		}
		p := filepath.Join(dir, name)
		switch {
		case isJunk(name):
			if info, err := e.Info(); err == nil && r.settled(info) {
				w.junk = append(w.junk, mediaFile{label(p), p, info.Size()})
			}
		case isSidecar(name):
			if ownedBy(name, owners) || (hasVideo && folderMeta[strings.ToLower(name)]) {
				continue
			}
			if info, err := e.Info(); err == nil && r.settled(info) {
				w.sidecars = append(w.sidecars, mediaFile{label(p), p, info.Size()})
			}
		}
	}
	return nil
}

func (r *runner) libraryTask(f *Finding, pick func(*mediaWalk) []mediaFile) error {
	w, err := r.walkLibraries()
	if err != nil {
		return err
	}
	f.Skipped = append(f.Skipped, w.skipped...)
	for _, m := range pick(w) {
		f.add(m.label, m.size)
		r.remove(f, m.path, false)
	}
	return nil
}

// orphanSidecars removes NFOs, subtitles and artwork images whose video is
// no longer beside them.
func orphanSidecars(r *runner, f *Finding) error {
	return r.libraryTask(f, func(w *mediaWalk) []mediaFile { return w.sidecars })
}

// osJunk removes .DS_Store, Thumbs.db, desktop.ini and macOS "._" files.
func osJunk(r *runner, f *Finding) error {
	return r.libraryTask(f, func(w *mediaWalk) []mediaFile { return w.junk })
}

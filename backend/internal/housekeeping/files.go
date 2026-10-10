package housekeeping

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// artifact is a data folder whose files are named by a table's storage_key.
type artifact struct {
	role, label, table string
	dir                func(Dirs) string
}

var artifacts = []artifact{
	{"thumbnails", "thumbnail", "thumbnail", func(d Dirs) string { return d.Thumbnails }},
	{"sprites", "scrub preview", "scrub_sprite", func(d Dirs) string { return d.Sprites }},
	{"previews", "hover preview", "preview_clip", func(d Dirs) string { return d.Previews }},
	{"posters", "artwork", "artwork", func(d Dirs) string { return d.Posters }},
}

// cacheRoles are folders whose files are not named by any table.
var cacheRoles = []string{"transcode", "remux", "subtitles", "plugins"}

// tempSuffixes mark files a writer had not finished.
var tempSuffixes = []string{".part", ".tmp", ".partial"}

func isTemp(name string) bool {
	for _, s := range tempSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

func (r *runner) keys(table string) (map[string]bool, error) {
	rows, err := r.db().Query(r.ctx, fmt.Sprintf(`SELECT storage_key FROM %s`, pgx.Identifier{table}.Sanitize()))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, k := range list {
		out[filepath.ToSlash(k)] = true
	}
	return out, nil
}

// orphanFiles removes files in the thumbnail, preview and artwork folders
// that no row names. Folders that share a path are judged together, and any
// other ReelStash folder nested inside one is left alone.
func orphanFiles(r *runner, f *Finding) error {
	managed := r.s.managedDirs()
	done := map[string]bool{}
	for _, a := range artifacts {
		dir := managed[a.role]
		if dir == "" || done[dir] {
			continue
		}
		done[dir] = true
		if shared := sharesCache(managed, dir); shared != "" {
			f.skip(fmt.Sprintf("%s folder is also the %s folder", a.label, shared))
			continue
		}
		if err := readableDir(dir); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				f.skip(fmt.Sprintf("%s folder unreadable: %v", a.label, err))
			}
			continue
		}
		known := map[string]bool{}
		for _, b := range artifacts {
			if managed[b.role] != dir {
				continue
			}
			keys, err := r.keys(b.table)
			if err != nil {
				return err
			}
			for k := range keys {
				known[k] = true
			}
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || r.ctx.Err() != nil {
				return r.ctx.Err()
			}
			if d.IsDir() {
				if p != dir && isManaged(managed, p) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			if known[filepath.ToSlash(rel)] || isTemp(d.Name()) {
				return nil
			}
			info, err := d.Info()
			if err != nil || !r.settled(info) {
				return nil
			}
			f.add(filepath.Join(filepath.Base(dir), rel), info.Size())
			r.remove(f, p, false)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// sharesCache names the cache folder dir is (or sits in), if any: files
// there are not named by rows, so orphan detection would wipe the cache.
func sharesCache(managed map[string]string, dir string) string {
	for _, role := range cacheRoles {
		if c := managed[role]; c != "" && within(dir, c) {
			return role
		}
	}
	return ""
}

func isManaged(managed map[string]string, p string) bool {
	for _, d := range managed {
		if d == p {
			return true
		}
	}
	return false
}

// leadingID reads the media_file id a cache entry is named after.
var leadingID = regexp.MustCompile(`^(\d+)`)

func entryFileID(name string) (int64, bool) {
	m := leadingID.FindString(name)
	if m == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(m, 10, 64)
	return id, err == nil
}

// transcodeCache removes HLS segment folders for files that are gone, and
// folders nothing has played from for a day (the player's own pruning drops
// their segments but leaves the folders).
func transcodeCache(r *runner, f *Finding) error {
	dir := r.s.managedDirs()["transcode"]
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) || dir == "" {
		return nil
	} else if err != nil {
		return err
	}
	ids, err := r.mediaFileIDs()
	if err != nil {
		return err
	}
	cutoff := r.now.Add(-24 * time.Hour)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		size, newest := treeStat(p)
		if r.now.Sub(newest) < grace {
			continue
		}
		id, named := entryFileID(e.Name())
		switch {
		case named && !ids[id]:
			f.add(e.Name()+" (file removed from library)", size)
		case newest.Before(cutoff):
			f.add(e.Name()+" (unused for a day)", size)
		default:
			continue
		}
		r.remove(f, p, true)
	}
	return nil
}

// treeStat totals the bytes under p and finds its newest modification.
func treeStat(p string) (int64, time.Time) {
	var size int64
	var newest time.Time
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil {
			if !d.IsDir() {
				size += info.Size()
			}
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
		return nil
	})
	return size, newest
}

// remuxCache removes remuxed copies of files that are gone or that nobody
// has played for CacheDays. Each is a full copy of a video, so they add up.
func remuxCache(r *runner, f *Finding) error {
	dir := r.s.managedDirs()["remux"]
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) || dir == "" {
		return nil
	} else if err != nil {
		return err
	}
	ids, err := r.mediaFileIDs()
	if err != nil {
		return err
	}
	cutoff := r.days(r.settings.CacheDays)
	for _, e := range entries {
		if e.IsDir() || isTemp(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !r.settled(info) {
			continue
		}
		id, named := entryFileID(e.Name())
		switch {
		case named && !ids[id]:
			f.add(e.Name()+" (file removed from library)", info.Size())
		case info.ModTime().Before(cutoff):
			f.add(fmt.Sprintf("%s (unused for %d days)", e.Name(), r.settings.CacheDays), info.Size())
		default:
			continue
		}
		r.remove(f, filepath.Join(dir, e.Name()), false)
	}
	return nil
}

// subtitleName is "<fileID>-<track>-<source mtime>.vtt".
var subtitleName = regexp.MustCompile(`^(\d+)-(.+)-(\d+)\.vtt$`)

// subtitleCache removes converted subtitles for files that are gone, those
// replaced by a newer conversion after the source changed, and those unused
// for CacheDays.
func subtitleCache(r *runner, f *Finding) error {
	dir := r.s.managedDirs()["subtitles"]
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) || dir == "" {
		return nil
	} else if err != nil {
		return err
	}
	ids, err := r.mediaFileIDs()
	if err != nil {
		return err
	}
	newest := map[string]int64{} // "<id>-<track>" -> newest source stamp
	for _, e := range entries {
		if m := subtitleName.FindStringSubmatch(e.Name()); m != nil {
			stamp, _ := strconv.ParseInt(m[3], 10, 64)
			if k := m[1] + "-" + m[2]; stamp > newest[k] {
				newest[k] = stamp
			}
		}
	}
	cutoff := r.days(r.settings.CacheDays)
	for _, e := range entries {
		if e.IsDir() || isTemp(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !r.settled(info) {
			continue
		}
		m := subtitleName.FindStringSubmatch(e.Name())
		var reason string
		if m != nil {
			id, _ := strconv.ParseInt(m[1], 10, 64)
			stamp, _ := strconv.ParseInt(m[3], 10, 64)
			switch {
			case !ids[id]:
				reason = "file removed from library"
			case stamp < newest[m[1]+"-"+m[2]]:
				reason = "replaced by a newer copy"
			}
		}
		if reason == "" && info.ModTime().Before(cutoff) {
			reason = fmt.Sprintf("unused for %d days", r.settings.CacheDays)
		}
		if reason == "" {
			continue
		}
		f.add(e.Name()+" ("+reason+")", info.Size())
		r.remove(f, filepath.Join(dir, e.Name()), false)
	}
	return nil
}

// tempFiles removes half-written files and empty folders from ReelStash's
// data folders (not plugin folders, which belong to the plugins).
func tempFiles(r *runner, f *Finding) error {
	managed := r.s.managedDirs()
	roots := map[string]bool{}
	for role, d := range managed {
		if role != "plugins" {
			roots[d] = true
		}
	}
	var ordered []string
	for d := range roots {
		ordered = append(ordered, d)
	}
	sort.Strings(ordered)
	for _, root := range ordered {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		var dirs []string
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || r.ctx.Err() != nil {
				return r.ctx.Err()
			}
			if d.IsDir() {
				if p != root && isManaged(managed, p) {
					return filepath.SkipDir // judged as its own root
				}
				if p != root {
					dirs = append(dirs, p)
				}
				return nil
			}
			if !isTemp(d.Name()) {
				return nil
			}
			if info, err := d.Info(); err == nil && r.settled(info) {
				f.add(relTo(root, p), info.Size())
				r.remove(f, p, false)
			}
			return nil
		})
		// Deepest first, so a folder emptied by removing its children goes too.
		sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
		for _, d := range dirs {
			entries, err := os.ReadDir(d)
			if err != nil || len(entries) > 0 {
				continue
			}
			if info, err := os.Stat(d); err == nil && r.settled(info) {
				f.add(relTo(root, d)+"/ (empty folder)", 0)
				r.remove(f, d, false)
			}
		}
	}
	return nil
}

func relTo(root, p string) string {
	rel, err := filepath.Rel(filepath.Dir(root), p)
	if err != nil {
		return p
	}
	return rel
}

// pluginFiles removes the staging copies a plugin install or update leaves
// when it is interrupted.
func pluginFiles(r *runner, f *Finding) error {
	root := r.s.managedDirs()["plugins"]
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) || root == "" {
		return nil
	} else if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, leftover := range []string{"app.new", "app.old"} {
			p := filepath.Join(root, e.Name(), leftover)
			info, err := os.Stat(p)
			if err != nil || !r.settled(info) {
				continue
			}
			size, _ := treeStat(p)
			f.add(filepath.Join(e.Name(), leftover), size)
			r.remove(f, p, true)
		}
	}
	return nil
}

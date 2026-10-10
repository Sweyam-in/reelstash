// Package pathguard says which places on disk ReelStash must leave alone:
//
//   - locked folders (protected_path), such as the folder qBittorrent keeps
//     unfinished downloads in. Scans, the watcher, the organizer, housekeeping
//     and Files never read into or change anything inside one;
//   - files in use by a download client, such as torrents still seeding.
//     They are scanned like any other file, but nothing moves, renames or
//     deletes them, since that would break the torrent.
package pathguard

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InUse is a file or folder a download client is using.
type InUse struct {
	Path string // absolute, as ReelStash sees it
	Name string // shown in messages, such as the torrent name
}

var inUseSource atomic.Value // func() []InUse

// SetInUseSource registers what reports the paths in use (the downloads
// service). nil clears it.
func SetInUseSource(fn func() []InUse) { inUseSource.Store(fn) }

func currentInUse() []InUse {
	fn, _ := inUseSource.Load().(func() []InUse)
	if fn == nil {
		return nil
	}
	return fn()
}

// Guard is a snapshot of the locked folders and paths in use.
type Guard struct {
	locked []string
	inUse  []InUse
}

// Load reads the locked folders and the paths in use now. A database error
// yields a guard with only the paths in use.
func Load(ctx context.Context, db *pgxpool.Pool) *Guard {
	g := &Guard{inUse: cleanInUse(currentInUse())}
	if db == nil {
		return g
	}
	rows, err := db.Query(ctx, `SELECT path FROM protected_path`)
	if err != nil {
		return g
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && strings.TrimSpace(p) != "" {
			g.locked = append(g.locked, filepath.Clean(p))
		}
	}
	return g
}

// New builds a guard from explicit lists (tests, callers with their own data).
func New(locked []string, inUse []InUse) *Guard {
	g := &Guard{inUse: cleanInUse(inUse)}
	for _, p := range locked {
		if strings.TrimSpace(p) != "" {
			g.locked = append(g.locked, filepath.Clean(p))
		}
	}
	return g
}

func cleanInUse(in []InUse) []InUse {
	out := make([]InUse, 0, len(in))
	for _, u := range in {
		if strings.TrimSpace(u.Path) != "" {
			out = append(out, InUse{Path: filepath.Clean(u.Path), Name: u.Name})
		}
	}
	return out
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	if p == dir {
		return true
	}
	if dir == string(filepath.Separator) {
		return strings.HasPrefix(p, dir)
	}
	return strings.HasPrefix(p, dir+string(filepath.Separator))
}

// Locked reports whether abs is a locked folder or inside one.
func (g *Guard) Locked(abs string) bool {
	if g == nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, l := range g.locked {
		if within(abs, l) {
			return true
		}
	}
	return false
}

// Inside reports whether abs is in a locked folder or is (or is inside)
// something in use. Unlike Blocked it ignores folders that merely hold one.
func (g *Guard) Inside(abs string) bool {
	if g == nil {
		return false
	}
	abs = filepath.Clean(abs)
	if g.Locked(abs) {
		return true
	}
	for _, u := range g.inUse {
		if within(abs, u.Path) {
			return true
		}
	}
	return false
}

// LockedFolders returns the locked folders.
func (g *Guard) LockedFolders() []string {
	if g == nil {
		return nil
	}
	return append([]string(nil), g.locked...)
}

// Blocked reports whether moving, renaming or deleting abs would touch a
// locked folder or something in use: abs is inside one, or abs is a folder
// that holds one. The reason is written for people.
func (g *Guard) Blocked(abs string) (bool, string) {
	if g == nil {
		return false, ""
	}
	abs = filepath.Clean(abs)
	for _, l := range g.locked {
		if within(abs, l) {
			return true, "this is inside the downloads in-progress folder, which ReelStash never changes"
		}
		if within(l, abs) {
			return true, "this folder holds the downloads in-progress folder, which ReelStash never changes"
		}
	}
	for _, u := range g.inUse {
		if within(abs, u.Path) {
			return true, "this belongs to the torrent \"" + u.Name + "\", which qBittorrent is still using; move or remove it from Downloads instead"
		}
		if within(u.Path, abs) {
			return true, "this folder holds the torrent \"" + u.Name + "\", which qBittorrent is still using; move or remove it from Downloads instead"
		}
	}
	return false, ""
}

// LockedRel returns the locked folders inside mount as paths relative to it
// (slash-separated, "" when mount itself is locked).
func (g *Guard) LockedRel(mount string) []string {
	if g == nil {
		return nil
	}
	mount = filepath.Clean(mount)
	var out []string
	for _, l := range g.locked {
		switch {
		case within(mount, l):
			out = append(out, "")
		case within(l, mount):
			rel, err := filepath.Rel(mount, l)
			if err == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	return out
}

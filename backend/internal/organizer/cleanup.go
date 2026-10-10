package organizer

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/scanner"
)

// CleanupItem is one leftover folder (with everything in it) that holds no
// videos.
type CleanupItem struct {
	Path  string `json:"path"` // relative to the storage location
	Files int    `json:"files"`
	Dirs  int    `json:"dirs"` // folders inside Path, not counting Path itself
	Bytes int64  `json:"bytes"`
	// Set when a library-wide plan merges the plans of several disks.
	StorageLocationID int64  `json:"storageLocationId,omitempty"`
	StorageLocation   string `json:"storageLocation,omitempty"`
}

type CleanupPlan struct {
	Items []CleanupItem `json:"items"`
	Files int           `json:"files"`
	Dirs  int           `json:"dirs"`
	Bytes int64         `json:"bytes"`
}

type CleanupResult struct {
	Files  int      `json:"files"`
	Dirs   int      `json:"dirs"`
	Bytes  int64    `json:"bytes"`
	Errors []string `json:"errors,omitempty"`
}

type cleanNode struct {
	keep        bool
	files, dirs int
	bytes       int64
}

type cleaner struct {
	mount     string
	roots     []string        // the library's roots on the disk; only folders inside them are offered
	skip      []string        // the categorized and uncategorized folders (relative, slash-separated)
	protected map[string]bool // lowercased folders holding available library files
	guard     *pathguard.Guard
	plan      CleanupPlan
}

// BuildCleanup finds the folders outside the categorized and uncategorized
// folders that hold no videos. It only reads; nothing is deleted. A folder
// is left alone when it (or anything under it) contains a video, when a
// media_file row points into it, when it is hidden, or when it can't be read
// completely. Symlinks are never followed or removed, and files sitting
// directly in the storage root are never touched.
func BuildCleanup(ctx context.Context, db *pgxpool.Pool, libraryID, storageLocationID int64) (CleanupPlan, string, error) {
	var mount string
	if err := db.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, storageLocationID).Scan(&mount); err != nil {
		return CleanupPlan{}, "", fmt.Errorf("load storage location: %w", err)
	}
	settings := LoadSettings(ctx, db, libraryID)
	roots, err := libraryRoots(ctx, db, libraryID, storageLocationID)
	if err != nil {
		return CleanupPlan{}, mount, err
	}
	c := &cleaner{
		mount:     filepath.Clean(mount),
		roots:     roots,
		protected: map[string]bool{},
		guard:     pathguard.Load(ctx, db),
		plan:      CleanupPlan{Items: []CleanupItem{}},
	}
	for _, r := range roots {
		c.skip = append(c.skip, path.Join(r, settings.CategorizedDir), path.Join(r, settings.UncategorizedDir))
	}

	rows, err := db.Query(ctx, `SELECT relative_path FROM media_file WHERE storage_location_id = $1 AND available`, storageLocationID)
	if err != nil {
		return c.plan, mount, err
	}
	defer rows.Close()
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return c.plan, mount, err
		}
		for dir := path.Dir(filepath.ToSlash(rel)); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
			c.protected[strings.ToLower(dir)] = true
		}
	}
	if err := rows.Err(); err != nil {
		return c.plan, mount, err
	}

	if _, err := os.Stat(c.mount); err != nil {
		return c.plan, mount, fmt.Errorf("storage location is not readable: %w", err)
	}
	c.walk(ctx, c.mount, "")
	sort.Slice(c.plan.Items, func(i, j int) bool {
		return strings.ToLower(c.plan.Items[i].Path) < strings.ToLower(c.plan.Items[j].Path)
	})
	return c.plan, mount, nil
}

// walk classifies dir. Folders that must stay are returned with keep set;
// removable children of a kept folder are recorded as plan items, while
// removable children of a removable folder are folded into it.
func (c *cleaner) walk(ctx context.Context, abs, rel string) cleanNode {
	node := cleanNode{}
	if ctx.Err() != nil {
		node.keep = true
		return node
	}
	if rel != "" {
		// Torrent data and the downloads in-progress folder are never cleaned;
		// a folder holding them stays, but its other leftovers may go.
		if c.guard.Inside(abs) {
			node.keep = true
			return node
		}
		if blocked, _ := c.guard.Blocked(abs); blocked {
			node.keep = true
		}
		lower := strings.ToLower(rel)
		if c.protected[lower] {
			node.keep = true
		}
		for _, s := range c.skip {
			if strings.EqualFold(rel, s) {
				node.keep = true
				return node
			}
		}
	} else {
		node.keep = true // the storage root itself is never removed
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		node.keep = true
		return node
	}
	type child struct {
		rel  string
		node cleanNode
	}
	var kids []child
	for _, e := range entries {
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		switch {
		case e.Type()&os.ModeSymlink != 0:
			node.keep = true
		case e.IsDir():
			if strings.HasPrefix(name, ".") {
				node.keep = true
				continue
			}
			n := c.walk(ctx, filepath.Join(abs, name), childRel)
			if n.keep {
				node.keep = true
			}
			kids = append(kids, child{childRel, n})
		case e.Type().IsRegular():
			if scanner.IsVideoExt(filepath.Ext(name)) {
				node.keep = true
				continue
			}
			node.files++
			if info, err := e.Info(); err == nil {
				node.bytes += info.Size()
			}
		default:
			node.keep = true // sockets, devices, …
		}
	}
	for _, k := range kids {
		if node.keep {
			if !k.node.keep {
				c.add(CleanupItem{Path: k.rel, Files: k.node.files, Dirs: k.node.dirs, Bytes: k.node.bytes})
			}
			continue
		}
		node.files += k.node.files
		node.dirs += k.node.dirs + 1
		node.bytes += k.node.bytes
	}
	if node.keep && rel != "" {
		// Files inside a kept folder are its companions; they stay.
		node.files, node.dirs, node.bytes = 0, 0, 0
	}
	return node
}

func (c *cleaner) add(it CleanupItem) {
	inside := false
	for _, r := range c.roots {
		if r == "" || it.Path == r || strings.HasPrefix(it.Path, r+"/") {
			inside = true
		}
	}
	if !inside {
		return
	}
	c.plan.Items = append(c.plan.Items, it)
	c.plan.Files += it.Files
	c.plan.Dirs += it.Dirs + 1
	c.plan.Bytes += it.Bytes
}

// RunCleanup recomputes the plan and deletes it. Removal is bottom-up and
// per file, and it re-checks every file: a video that shows up meanwhile
// (or any folder that stops being empty of videos) survives.
func RunCleanup(ctx context.Context, db *pgxpool.Pool, libraryID, storageLocationID int64) (CleanupResult, error) {
	plan, mount, err := BuildCleanup(ctx, db, libraryID, storageLocationID)
	if err != nil {
		return CleanupResult{}, err
	}
	res := CleanupResult{}
	mount = filepath.Clean(mount)
	for _, it := range plan.Items {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		abs := filepath.Join(mount, filepath.FromSlash(it.Path))
		if !strings.HasPrefix(abs, mount+string(filepath.Separator)) {
			continue
		}
		removeLeftovers(abs, &res)
	}
	return res, nil
}

func removeLeftovers(dir string, res *CleanupResult) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
		case e.IsDir():
			removeLeftovers(p, res)
		case e.Type().IsRegular() && !scanner.IsVideoExt(filepath.Ext(e.Name())):
			var size int64
			if info, err := e.Info(); err == nil {
				size = info.Size()
			}
			if err := os.Remove(p); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
			} else {
				res.Files++
				res.Bytes += size
			}
		}
	}
	// os.Remove refuses a folder that still has anything in it.
	if err := os.Remove(dir); err != nil {
		return false
	}
	res.Dirs++
	return true
}

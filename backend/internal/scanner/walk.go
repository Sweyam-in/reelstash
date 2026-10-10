package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// skipDirNames are OS, NAS and trash folders that never hold library videos;
// indexing them turns deleted files and thumbnails into titles.
var skipDirNames = map[string]bool{
	"@eaDir":                    true, // Synology thumbnails
	"#recycle":                  true, // Synology recycle bin
	"#snapshot":                 true,
	"@Recycle":                  true, // QNAP
	"@Recently-Snapshot":        true,
	"$RECYCLE.BIN":              true, // Windows
	"System Volume Information": true,
	"lost+found":                true,
}

// skipDir reports whether a folder is left out of a scan: hidden folders
// (.Trash-1000, .recycle, .snapshots, ...) and the known system ones above.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || skipDirNames[name]
}

// walkParallel lists root and every folder below it, reading up to
// `readers` folders at once — on a NAS or USB disk each ReadDir waits on the
// device, so a single-threaded walk spends most of its time idle. onFile is
// called concurrently for every non-directory entry. onDirErr gets each
// unreadable folder below root; an unreadable root is returned as the error.
// Symlinked folders are not followed (same as filepath.WalkDir). skip, when
// set, leaves out any folder it returns true for (and everything below it).
func walkParallel(ctx context.Context, root string, readers int, skip func(absDir string) bool, onFile func(absPath, name string), onDirErr func(dir string, err error)) error {
	if readers < 1 {
		readers = 1
	}
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	sem := make(chan struct{}, readers)
	var wg sync.WaitGroup
	var visitEntries func(dir string, entries []os.DirEntry)
	visit := func(dir string) {
		defer wg.Done()
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		entries, err := os.ReadDir(dir)
		<-sem
		if err != nil {
			onDirErr(dir, err)
			return
		}
		visitEntries(dir, entries)
	}
	visitEntries = func(dir string, entries []os.DirEntry) {
		for _, e := range entries {
			if ctx.Err() != nil {
				return
			}
			path := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if !skipDir(e.Name()) && (skip == nil || !skip(path)) {
					wg.Add(1)
					go visit(path)
				}
				continue
			}
			onFile(path, e.Name())
		}
	}

	visitEntries(root, rootEntries)
	wg.Wait()
	return ctx.Err()
}

// SkipDir reports whether a folder named name is left out of scans (hidden
// and system folders), for callers that preview what a scan would find.
func SkipDir(name string) bool { return skipDir(name) }

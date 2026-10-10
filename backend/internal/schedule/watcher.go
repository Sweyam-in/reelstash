package schedule

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/scanner"
)

// Status is what the admin UI shows for one watched library.
type Status struct {
	LibraryID   int64      `json:"libraryId"`
	Watching    bool       `json:"watching"`
	Directories int        `json:"directories"`
	LastEventAt *time.Time `json:"lastEventAt,omitempty"`
	LastError   string     `json:"error,omitempty"`
}

// Watcher scans a library shortly after files in its folders change, for the
// libraries with watch_enabled. It relies on inotify (or the platform's
// equivalent), which only reports changes made on this machine: for a
// network share another machine writes to, use a cron schedule instead.
type Watcher struct {
	DB       *pgxpool.Pool
	Jobs     *jobs.Runner
	Alert    Alert
	Debounce time.Duration // quiet time after the last change before scanning
	Retry    time.Duration // wait before retrying when a scan is already running
	Sync     time.Duration // how often the set of watched libraries is refreshed
	Poll     time.Duration // how often network shares are checked; inotify cannot see them

	// enqueue queues a scan of a library; replaced in tests.
	enqueue func(ctx context.Context, libraryID int64) error

	mu      sync.Mutex
	active  map[int64]*locWatch
	failing map[int64]time.Time // library -> next time a failed start may be retried
	errs    map[int64]string    // library -> why its last start failed
	stamps  map[int64]string    // library -> last polled file stamp
}

func NewWatcher(db *pgxpool.Pool, runner *jobs.Runner, alert Alert) *Watcher {
	w := &Watcher{DB: db, Jobs: runner, Alert: alert, Debounce: 45 * time.Second, Retry: time.Minute, Sync: 30 * time.Second, Poll: 2 * time.Minute,
		active: map[int64]*locWatch{}, failing: map[int64]time.Time{}, errs: map[int64]string{}, stamps: map[int64]string{}}
	w.enqueue = func(ctx context.Context, id int64) error {
		_, err := EnqueueLibraryScan(ctx, w.DB, w.Jobs, id)
		return err
	}
	return w
}

// locWatch watches one library's folders.
type locWatch struct {
	id     int64
	roots  []string        // absolute library folders
	skip   map[string]bool // other libraries' folders nested inside them
	key    string          // roots and skip, to notice when they change
	fsw    *fsnotify.Watcher
	cancel context.CancelFunc

	mu        sync.Mutex
	dirs      int
	lastEvent time.Time
}

// Run keeps the set of watchers in step with the watch_enabled flags until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	w.reconcile(ctx)
	if w.Poll <= 0 {
		w.Poll = 2 * time.Minute
	}
	t := time.NewTicker(w.Sync)
	defer t.Stop()
	poll := time.NewTicker(w.Poll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			w.stopAll()
			return
		case <-t.C:
			w.reconcile(ctx)
		case <-poll.C:
			w.pollShares(ctx)
		}
	}
}

// watchTarget is what one library's watch covers.
type watchTarget struct {
	roots []string
	skip  map[string]bool
}

func (t watchTarget) key() string {
	skip := make([]string, 0, len(t.skip))
	for dir := range t.skip {
		skip = append(skip, dir)
	}
	sort.Strings(skip)
	return strings.Join(t.roots, "\x00") + "\x01" + strings.Join(skip, "\x00")
}

// watchTargets loads the folders of every library with watching switched on.
func (w *Watcher) watchTargets(ctx context.Context) (map[int64]watchTarget, error) {
	rows, err := w.DB.Query(ctx, `
		SELECT lr.library_id, l.watch_enabled, sl.mount_path, lr.path
		FROM library_root lr
		JOIN library l ON l.id = lr.library_id
		JOIN storage_location sl ON sl.id = lr.storage_location_id
		ORDER BY lr.library_id, sl.mount_path, lr.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type root struct {
		library int64
		abs     string
	}
	var all []root
	want := map[int64]watchTarget{}
	for rows.Next() {
		var id int64
		var enabled bool
		var mount, rel string
		if err := rows.Scan(&id, &enabled, &mount, &rel); err != nil {
			return nil, err
		}
		abs := filepath.Join(mount, filepath.FromSlash(rel))
		all = append(all, root{id, abs})
		if enabled {
			t := want[id]
			t.roots = append(t.roots, abs)
			want[id] = t
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Locked folders (the downloads in-progress folder) are never watched:
	// half-written files there must not trigger scans.
	locked := pathguard.Load(ctx, w.DB).LockedFolders()
	for id, t := range want {
		t.skip = map[string]bool{}
		for _, l := range locked {
			for _, mine := range t.roots {
				if strings.HasPrefix(l, mine+string(filepath.Separator)) {
					t.skip[l] = true
				}
			}
		}
		for _, other := range all {
			if other.library == id {
				continue
			}
			for _, mine := range t.roots {
				if strings.HasPrefix(other.abs, mine+string(filepath.Separator)) {
					t.skip[other.abs] = true
				}
			}
		}
		want[id] = t
	}
	return want, nil
}

func (w *Watcher) reconcile(ctx context.Context) {
	want, err := w.watchTargets(ctx)
	if err != nil {
		log.Printf("watch: load libraries: %v", err)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for id, lw := range w.active {
		if t, ok := want[id]; !ok || t.key() != lw.key {
			lw.cancel()
			delete(w.active, id)
		}
	}
	for id, target := range want {
		if _, ok := w.active[id]; ok {
			continue
		}
		if next, bad := w.failing[id]; bad && time.Now().Before(next) {
			continue
		}
		lw, err := w.start(ctx, id, target.roots, target.skip)
		if err != nil {
			w.failing[id] = time.Now().Add(5 * time.Minute)
			msg := err.Error()
			if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EMFILE) {
				msg = "the inotify watch limit was reached; raise fs.inotify.max_user_watches on the host"
			}
			log.Printf("watch: library %d: %s", id, msg)
			if w.Alert != nil {
				w.Alert(ctx, "File watching could not start", fmt.Sprintf("library %d: %s", id, msg))
			}
			w.errs[id] = msg
			continue
		}
		delete(w.failing, id)
		delete(w.errs, id)
		w.active[id] = lw
	}
	for id := range w.failing {
		if _, ok := want[id]; !ok {
			delete(w.failing, id)
			delete(w.errs, id)
		}
	}
}

func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, lw := range w.active {
		lw.cancel()
		delete(w.active, id)
	}
}

// Statuses lists every library with file watching switched on.
func (w *Watcher) Statuses() map[int64]Status {
	out := map[int64]Status{}
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, lw := range w.active {
		lw.mu.Lock()
		st := Status{LibraryID: id, Watching: true, Directories: lw.dirs}
		if !lw.lastEvent.IsZero() {
			t := lw.lastEvent
			st.LastEventAt = &t
		}
		lw.mu.Unlock()
		out[id] = st
	}
	for id := range w.failing {
		out[id] = Status{LibraryID: id, LastError: w.errs[id]}
	}
	return out
}

func (w *Watcher) start(ctx context.Context, id int64, roots []string, skip map[string]bool) (*locWatch, error) {
	if len(roots) == 0 {
		return nil, errors.New("the library has no folders")
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: not a directory", root)
		}
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if skip == nil {
		skip = map[string]bool{}
	}
	lw := &locWatch{id: id, roots: roots, skip: skip, fsw: fsw}
	lw.key = watchTarget{roots: roots, skip: skip}.key()
	for _, root := range roots {
		if err := lw.addTree(root); err != nil {
			fsw.Close()
			return nil, err
		}
	}
	wctx, cancel := context.WithCancel(ctx)
	lw.cancel = func() { cancel(); fsw.Close() }
	go w.loop(wctx, lw)
	log.Printf("watch: watching library %d (%s), %d directories", id, strings.Join(roots, ", "), lw.dirs)
	return lw, nil
}

// skipDir names folders that hold no library files and churn constantly.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "@eaDir" || name == "#recycle" || name == "$RECYCLE.BIN" || name == "lost+found"
}

// addTree watches dir and every folder below it. Unreadable sub-folders are
// skipped, but running out of watches fails the whole start.
func (lw *locWatch) addTree(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != dir && (skipDir(d.Name()) || lw.skip[path]) {
			return filepath.SkipDir
		}
		if err := lw.fsw.Add(path); err != nil {
			if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EMFILE) {
				return err
			}
			return nil // vanished or unreadable: not worth failing the rest
		}
		lw.mu.Lock()
		lw.dirs++
		lw.mu.Unlock()
		return nil
	})
}

// relevant filters events down to ones that can change the library: video
// files, subtitle files, and folders coming or going. NFO files are left out
// on purpose: ReelStash writes them itself, which would re-trigger scans.
func relevant(ev fsnotify.Event) bool {
	if ev.Op == fsnotify.Chmod {
		return false
	}
	ext := strings.ToLower(filepath.Ext(ev.Name))
	if scanner.IsVideoExt(ext) {
		return true
	}
	switch ext {
	case ".srt", ".vtt", ".ass", ".ssa":
		return true
	case "":
		return ev.Has(fsnotify.Create) || ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) // possibly a folder
	}
	return false
}

func (w *Watcher) loop(ctx context.Context, lw *locWatch) {
	var timer *time.Timer
	var fire <-chan time.Time
	arm := func(d time.Duration) {
		if timer == nil {
			timer = time.NewTimer(d)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(d)
		}
		fire = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-lw.fsw.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && !skipDir(info.Name()) && !lw.skip[ev.Name] {
					_ = lw.addTree(ev.Name) // files may already be inside a folder dropped in whole
				}
			}
			if !relevant(ev) {
				continue
			}
			lw.mu.Lock()
			lw.lastEvent = time.Now()
			lw.mu.Unlock()
			arm(w.Debounce)
		case err, ok := <-lw.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watch: library %d: %v", lw.id, err)
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				arm(w.Debounce) // events were lost: a scan catches up
			}
		case <-fire:
			fire = nil
			err := w.enqueue(ctx, lw.id)
			switch {
			case err == nil:
			case errors.Is(err, ErrScanRunning), errors.Is(err, ErrOrganizing):
				arm(w.Retry) // changes during a scan are picked up by the next one
			default:
				log.Printf("watch: library %d: queue scan: %v", lw.id, err)
			}
		}
	}
}

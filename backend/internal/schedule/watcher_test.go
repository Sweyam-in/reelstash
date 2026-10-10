package schedule

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newTestWatcher(scans *atomic.Int32) *Watcher {
	w := NewWatcher(nil, nil, nil)
	w.Debounce = 150 * time.Millisecond
	w.Retry = 150 * time.Millisecond
	w.enqueue = func(ctx context.Context, id int64) error { scans.Add(1); return nil }
	return w
}

func startWatching(t *testing.T, w *Watcher, root string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lw, err := w.start(ctx, 1, []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.active[1] = lw
}

func TestWatcherScansAfterANewVideoAppears(t *testing.T) {
	root := t.TempDir()
	var scans atomic.Int32
	w := newTestWatcher(&scans)
	startWatching(t, w, root)

	if err := os.WriteFile(filepath.Join(root, "movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a scan after a new video", func() bool { return scans.Load() >= 1 })
	if st := w.Statuses()[1]; !st.Watching || st.LastEventAt == nil {
		t.Errorf("status = %+v, want watching with a last event", st)
	}
}

func TestWatcherBatchesABurstOfChangesIntoOneScan(t *testing.T) {
	root := t.TempDir()
	var scans atomic.Int32
	w := newTestWatcher(&scans)
	w.Debounce = 400 * time.Millisecond
	startWatching(t, w, root)

	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(root, "part"+string(rune('a'+i))+".mp4"), []byte("x"), 0o644)
		time.Sleep(40 * time.Millisecond)
	}
	waitUntil(t, "the burst to be scanned", func() bool { return scans.Load() >= 1 })
	time.Sleep(600 * time.Millisecond)
	if n := scans.Load(); n != 1 {
		t.Errorf("scans = %d, want 1 for one burst", n)
	}
}

func TestWatcherFollowsNewFolders(t *testing.T) {
	root := t.TempDir()
	var scans atomic.Int32
	w := newTestWatcher(&scans)
	startWatching(t, w, root)

	sub := filepath.Join(root, "Show", "Season 1")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the new folders to be watched", func() bool { return scans.Load() >= 1 })
	before := scans.Load()
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(filepath.Join(sub, "s01e01.mp4"), []byte("x"), 0o644)
	waitUntil(t, "a scan for a file in the new folder", func() bool { return scans.Load() > before })
}

func TestWatcherIgnoresNFOAndOtherFiles(t *testing.T) {
	root := t.TempDir()
	var scans atomic.Int32
	w := newTestWatcher(&scans)
	startWatching(t, w, root)

	os.WriteFile(filepath.Join(root, "movie.nfo"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "cover.jpg"), []byte("x"), 0o644)
	time.Sleep(500 * time.Millisecond)
	if n := scans.Load(); n != 0 {
		t.Errorf("scans = %d, want 0 for .nfo and image files", n)
	}
}

func TestWatcherRetriesWhileAScanIsRunning(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	w := NewWatcher(nil, nil, nil)
	w.Debounce, w.Retry = 100*time.Millisecond, 100*time.Millisecond
	w.enqueue = func(ctx context.Context, id int64) error {
		if calls.Add(1) == 1 {
			return ErrScanRunning
		}
		return nil
	}
	startWatching(t, w, root)
	os.WriteFile(filepath.Join(root, "a.mkv"), []byte("x"), 0o644)
	waitUntil(t, "a retry after the busy answer", func() bool { return calls.Load() >= 2 })
}

func TestWatcherStartFailsForAMissingFolder(t *testing.T) {
	w := NewWatcher(nil, nil, nil)
	if _, err := w.start(context.Background(), 1, []string{filepath.Join(t.TempDir(), "gone")}, nil); err == nil {
		t.Error("starting on a missing folder should fail")
	}
}

func TestWatcherIgnoresAnotherLibrarysNestedFolder(t *testing.T) {
	root := t.TempDir()
	kids := filepath.Join(root, "Kids")
	if err := os.Mkdir(kids, 0o755); err != nil {
		t.Fatal(err)
	}
	var scans atomic.Int32
	w := newTestWatcher(&scans)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lw, err := w.start(ctx, 1, []string{root}, map[string]bool{kids: true})
	if err != nil {
		t.Fatal(err)
	}
	w.active[1] = lw

	os.WriteFile(filepath.Join(kids, "cartoon.mkv"), []byte("x"), 0o644)
	time.Sleep(400 * time.Millisecond)
	if n := scans.Load(); n != 0 {
		t.Fatalf("a change in another library's folder queued %d scans", n)
	}
	os.WriteFile(filepath.Join(root, "movie.mkv"), []byte("x"), 0o644)
	waitUntil(t, "a scan after a change in the library's own folder", func() bool { return scans.Load() >= 1 })
}

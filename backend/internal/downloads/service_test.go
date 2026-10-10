package downloads

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/qbit"
	"reelstash/backend/internal/qbit/qbittest"
	"reelstash/backend/internal/testdb"
)

type fixture struct {
	pool    *pgxpool.Pool
	fake    *qbittest.Server
	svc     *Service
	mount   string
	loc     int64
	library int64
	mu      sync.Mutex
	notes   []string
}

// newFixture sets up a disk at /data with a "Movies" library folder, a
// qBittorrent that saves into /data/downloads, and auto-move switched on.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	f := &fixture{pool: testdb.Isolated(t), fake: qbittest.New(), mount: "/data"}
	t.Cleanup(f.fake.Close)
	f.fake.Prefs["save_path"] = "/data/downloads"
	if err := f.pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ('Disk', 'LOCAL', $1) RETURNING id`, f.mount).Scan(&f.loc); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `
		WITH l AS (INSERT INTO library (name) VALUES ('Movies') RETURNING id)
		INSERT INTO library_root (library_id, storage_location_id, path) SELECT l.id, $1, 'Movies' FROM l RETURNING library_id`, f.loc).Scan(&f.library); err != nil {
		t.Fatal(err)
	}
	runner := jobs.NewRunner(f.pool, 1)
	runner.Register("SCAN", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) { return nil, nil })
	t.Cleanup(func() { runner.Shutdown(time.Second) })
	if err := SaveSettings(ctx, f.pool, Settings{URL: f.fake.URL, AutoMove: true, RulesSince: 100}); err != nil {
		t.Fatal(err)
	}
	f.svc = New(f.pool, runner, qbit.Config{Username: "admin", Password: "adminadmin"}, func(_ context.Context, failed bool, title, _ string) {
		f.mu.Lock()
		defer f.mu.Unlock()
		prefix := "ok: "
		if failed {
			prefix = "failed: "
		}
		f.notes = append(f.notes, prefix+title)
	})
	t.Cleanup(func() { pathguard.SetInUseSource(nil) })
	return f
}

func (f *fixture) rule(t *testing.T, r Rule) Rule {
	t.Helper()
	r.Enabled, r.DestStorageLocationID = true, f.loc
	if r.Name == "" {
		r.Name = "Movies"
	}
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	saved, err := CreateRule(context.Background(), f.pool, r)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func (f *fixture) tick(t *testing.T) {
	t.Helper()
	f.svc.tick(context.Background())
}

func (f *fixture) status(t *testing.T, hash string) (status, message string) {
	t.Helper()
	_ = f.pool.QueryRow(context.Background(), `SELECT status, message FROM download_move WHERE hash = $1`, hash).Scan(&status, &message)
	return
}

func (f *fixture) scanJobs(t *testing.T) int {
	t.Helper()
	var n int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM background_job WHERE kind = 'SCAN' AND (payload->>'libraryId')::bigint = $1`, f.library).Scan(&n)
	return n
}

func TestAutoMoveMovesScansAndRemoves(t *testing.T) {
	f := newFixture(t)
	f.rule(t, Rule{MatchCategory: "movies", DestPath: "Movies", ScanAfter: true, RemoveAfter: RemoveMoved})
	f.fake.AddTorrent("h1", map[string]any{"name": "Film", "category": "movies"}, "Film/film.mkv")

	f.tick(t) // still downloading: nothing happens
	if st, _ := f.status(t, "h1"); st != "" {
		t.Fatalf("acted on an unfinished torrent: %s", st)
	}

	f.fake.Complete("h1", 200)
	f.tick(t)
	if st, msg := f.status(t, "h1"); st != StatusMoving && st != StatusMoved {
		t.Fatalf("status = %s %s", st, msg)
	}
	if calls := f.fake.CallLog(); !contains(calls, "setLocation /data/Movies") {
		t.Fatalf("calls = %v", calls)
	}
	f.tick(t) // sees the new folder: moved, tagged, scan queued
	if st, msg := f.status(t, "h1"); st != StatusMoved && st != StatusRemoved {
		t.Fatalf("status = %s %s", st, msg)
	}
	if !contains(f.fake.CallLog(), "addTags "+DefaultMoveTag) {
		t.Fatalf("not tagged: %v", f.fake.CallLog())
	}
	if f.scanJobs(t) != 1 {
		t.Fatalf("scan jobs = %d", f.scanJobs(t))
	}
	f.tick(t) // scan queued: the rule removes the torrent, keeping its files
	if st, msg := f.status(t, "h1"); st != StatusRemoved || msg != "Removed after moving" {
		t.Fatalf("status = %s %q", st, msg)
	}
	if !contains(f.fake.CallLog(), "delete h1 files=false") {
		t.Fatalf("calls = %v", f.fake.CallLog())
	}
	if len(f.notes) != 1 || f.notes[0] != `ok: Moved "Film"` {
		t.Fatalf("notes = %v", f.notes)
	}
}

func TestAutoMoveLeavesOldAndHandPlacedTorrents(t *testing.T) {
	f := newFixture(t)
	f.rule(t, Rule{DestPath: "Movies"})
	f.fake.AddTorrent("old", map[string]any{"name": "Old"})
	f.fake.Complete("old", 50) // finished before auto-move was switched on
	f.fake.AddTorrent("own", map[string]any{"name": "Own", "save_path": "/data/elsewhere"})
	f.fake.Complete("own", 200)

	f.tick(t)
	if st, _ := f.status(t, "old"); st != "" {
		t.Fatalf("old torrent handled: %s", st)
	}
	if st, msg := f.status(t, "own"); st != StatusSkipped || !strings.Contains(msg, "folder of its own") {
		t.Fatalf("own folder: %s %q", st, msg)
	}
	for _, c := range f.fake.CallLog() {
		if strings.HasPrefix(c, "setLocation") {
			t.Fatalf("moved something: %v", f.fake.CallLog())
		}
	}

	// By hand, the rules apply to both.
	info, err := f.svc.ApplyRules(context.Background(), "own")
	if err != nil || info.Status != StatusMoving {
		t.Fatalf("apply by hand: %+v %v", info, err)
	}
}

func TestNoRuleMatchedIsRecordedOnce(t *testing.T) {
	f := newFixture(t)
	f.rule(t, Rule{MatchCategory: "tv", DestPath: "Movies"})
	f.fake.AddTorrent("h", map[string]any{"name": "Film", "category": "movies"})
	f.fake.Complete("h", 200)
	f.tick(t)
	if st, msg := f.status(t, "h"); st != StatusSkipped || msg != "No rule matched" {
		t.Fatalf("status = %s %q", st, msg)
	}
	// Forgetting the record lets a newly added rule pick it up.
	f.rule(t, Rule{Name: "Everything", DestPath: "Movies"})
	if err := f.svc.Forget(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
	f.tick(t)
	if st, _ := f.status(t, "h"); st != StatusMoving && st != StatusMoved {
		t.Fatalf("after forget: %s", st)
	}
}

func TestSlowAndFailedMoves(t *testing.T) {
	f := newFixture(t)
	f.rule(t, Rule{DestPath: "Movies", Subfolder: "{name}"})
	f.fake.HoldMoves = true
	f.fake.AddTorrent("slow", map[string]any{"name": "Slow"})
	f.fake.Complete("slow", 200)
	f.tick(t)
	f.tick(t)
	if st, _ := f.status(t, "slow"); st != StatusMoving {
		t.Fatalf("held move: %s", st)
	}
	f.fake.FinishMove("slow", "/data/Movies/Slow")
	f.tick(t)
	if st, msg := f.status(t, "slow"); st != StatusMoved {
		t.Fatalf("finished move: %s %q", st, msg)
	}

	f.fake.HoldMoves, f.fake.FailMoves = false, true
	f.fake.AddTorrent("bad", map[string]any{"name": "Bad"})
	f.fake.Complete("bad", 300)
	f.tick(t)
	if st, msg := f.status(t, "bad"); st != StatusFailed || !strings.Contains(msg, "permissions") {
		t.Fatalf("failed move: %s %q", st, msg)
	}
	if !contains(f.notes, `failed: Could not move "Bad"`) {
		t.Fatalf("notes = %v", f.notes)
	}
}

func TestRemoveAfterSeeding(t *testing.T) {
	f := newFixture(t)
	f.rule(t, Rule{DestPath: "Movies", RemoveAfter: RemoveSeeded, RemoveRatio: 2})
	f.fake.AddTorrent("s", map[string]any{"name": "Seed", "ratio": 0.5})
	f.fake.Complete("s", 200)
	f.tick(t)
	f.tick(t)
	f.tick(t)
	if st, _ := f.status(t, "s"); st != StatusMoved {
		t.Fatalf("before ratio: %s", st)
	}
	f.fake.Update("s", map[string]any{"ratio": 2.1})
	f.tick(t)
	if st, msg := f.status(t, "s"); st != StatusRemoved || !strings.Contains(msg, "ratio") {
		t.Fatalf("after ratio: %s %q", st, msg)
	}
}

func TestInUseAndSnapshot(t *testing.T) {
	f := newFixture(t)
	f.fake.AddTorrent("lib", map[string]any{"name": "Film", "save_path": "/data/Movies"})
	f.tick(t)
	if !pathguard.Load(context.Background(), nil).Inside("/data/Movies/Film/film.mkv") {
		t.Fatal("torrent data is not guarded")
	}
	snap, err := f.svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Connected || len(snap.Torrents) != 1 {
		t.Fatalf("snapshot = %+v", snap.Status)
	}
	tor := snap.Torrents[0]
	lib, _ := tor["library"].(map[string]any)
	folder, _ := tor["folder"].(Location)
	if lib["name"] != "Movies" || folder.StorageLocationID != f.loc || folder.Path != "Movies" {
		t.Fatalf("torrent = %v", tor)
	}
	if len(snap.Warnings) == 0 || snap.Warnings[0].Code != "incomplete-missing" {
		t.Fatalf("warnings = %v", snap.Warnings)
	}
}

func TestIncompleteFolderChecksAndProtection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := CheckIncompleteDir(ctx, f.pool, "/data/Movies/incomplete"); err != nil {
		t.Fatalf("refused a folder inside a library: %v", err)
	}
	if err := CheckIncompleteDir(ctx, f.pool, "/data/Movies"); err == nil {
		t.Fatal("accepted the library folder itself")
	}
	if err := CheckIncompleteDir(ctx, f.pool, "/data"); err == nil {
		t.Fatal("accepted a folder holding a library")
	}
	if err := CheckIncompleteDir(ctx, f.pool, "/data/incomplete"); err != nil {
		t.Fatal(err)
	}
	if err := SetProtected(ctx, f.pool, "/data/incomplete"); err != nil {
		t.Fatal(err)
	}
	g := pathguard.Load(ctx, f.pool)
	if !g.Locked(filepath.Join("/data/incomplete", "x.mkv")) || g.Locked("/data/Movies") {
		t.Fatal("protected folder not loaded")
	}
	if err := SetProtected(ctx, f.pool, ""); err != nil {
		t.Fatal(err)
	}
	if pathguard.Load(ctx, f.pool).Locked("/data/incomplete") {
		t.Fatal("protection not removed")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

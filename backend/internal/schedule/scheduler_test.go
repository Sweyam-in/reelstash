package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

// quietRunner is a job runner whose handlers do nothing, so a queued job
// finishes without touching any real storage.
func quietRunner(t *testing.T, pool *pgxpool.Pool) *jobs.Runner {
	t.Helper()
	r := jobs.NewRunner(pool, 1)
	noop := func(ctx context.Context, db *pgxpool.Pool, id int64, payload json.RawMessage) (any, error) {
		return nil, nil
	}
	r.Register("SCAN", noop)
	r.Register("DUPLICATE_SCAN", noop)
	t.Cleanup(func() { r.Shutdown(time.Second) })
	return r
}

// addLocation adds a disk and a library covering all of it, both called name,
// and returns the disk's id.
func addLocation(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, name, "/mnt/"+name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		WITH l AS (INSERT INTO library (name) VALUES ($1) RETURNING id)
		INSERT INTO library_root (library_id, storage_location_id, path) SELECT l.id, $2, '' FROM l`, name, id); err != nil {
		t.Fatal(err)
	}
	return id
}

const libraryOf = `(SELECT library_id FROM library_root WHERE storage_location_id = $1)`

func scanJobLocations(t *testing.T, pool *pgxpool.Pool) map[int64]int {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT (payload->>'storageLocationId')::bigint FROM background_job WHERE kind = 'SCAN'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		out[id]++
	}
	return out
}

func TestSchedulerQueuesDueScansOnceAMinute(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	s := NewScheduler(pool, quietRunner(t, pool), nil)
	a, b := addLocation(t, pool, "a"), addLocation(t, pool, "b")

	pool.Exec(ctx, `INSERT INTO scan_schedule (library_id, cron_expr) VALUES (`+libraryOf+`, '*/5 * * * *')`, a)
	pool.Exec(ctx, `INSERT INTO scan_schedule (library_id, cron_expr, enabled) VALUES (`+libraryOf+`, '* * * * *', FALSE)`, b)

	due := time.Date(2026, 3, 4, 10, 15, 20, 0, time.UTC)
	s.Tick(ctx, due)
	waitUntil(t, "the due scan", func() bool { return scanJobLocations(t, pool)[a] == 1 })
	if n := scanJobLocations(t, pool)[b]; n != 0 {
		t.Errorf("disabled schedule queued %d scans", n)
	}

	// Same minute again: already run.
	s.Tick(ctx, due.Add(10*time.Second))
	// A minute that does not match.
	s.Tick(ctx, time.Date(2026, 3, 4, 10, 16, 0, 0, time.UTC))
	time.Sleep(100 * time.Millisecond)
	if n := scanJobLocations(t, pool)[a]; n != 1 {
		t.Errorf("scans for a = %d, want 1", n)
	}

	// Five minutes on it fires again.
	s.Tick(ctx, time.Date(2026, 3, 4, 10, 20, 0, 0, time.UTC))
	waitUntil(t, "the next due scan", func() bool { return scanJobLocations(t, pool)[a] == 2 })
}

func TestSchedulerWithoutALibraryScansEveryLibrary(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	s := NewScheduler(pool, quietRunner(t, pool), nil)
	a, b := addLocation(t, pool, "a"), addLocation(t, pool, "b")
	pool.Exec(ctx, `INSERT INTO scan_schedule (cron_expr) VALUES ('0 3 * * *')`)

	s.Tick(ctx, time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC))
	waitUntil(t, "both libraries queued", func() bool { got := scanJobLocations(t, pool); return got[a] == 1 && got[b] == 1 })
}

func TestSchedulerSkipsALibraryThatIsAlreadyScanning(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	s := NewScheduler(pool, quietRunner(t, pool), nil)
	a := addLocation(t, pool, "a")
	pool.Exec(ctx, `INSERT INTO scan_schedule (library_id, cron_expr) VALUES (`+libraryOf+`, '* * * * *')`, a)
	pool.Exec(ctx, `INSERT INTO scan_job (storage_location_id) VALUES ($1)`, a) // an unfinished scan

	s.Tick(ctx, time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC))
	time.Sleep(100 * time.Millisecond)
	if n := scanJobLocations(t, pool)[a]; n != 0 {
		t.Errorf("queued %d scans on top of a running one", n)
	}
}

func TestSchedulerIgnoresABadExpressionAndKeepsGoing(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	s := NewScheduler(pool, quietRunner(t, pool), nil)
	a := addLocation(t, pool, "a")
	pool.Exec(ctx, `INSERT INTO scan_schedule (library_id, cron_expr) VALUES (`+libraryOf+`, 'nonsense')`, a)
	pool.Exec(ctx, `INSERT INTO scan_schedule (library_id, cron_expr) VALUES (`+libraryOf+`, '* * * * *')`, a)

	s.Tick(ctx, time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC))
	waitUntil(t, "the valid schedule to run", func() bool { return scanJobLocations(t, pool)[a] == 1 })
}

func TestSchedulerDuplicateScan(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	s := NewScheduler(pool, quietRunner(t, pool), nil)
	pool.Exec(ctx, `INSERT INTO scan_schedule (job_kind, cron_expr) VALUES ('DUPLICATE_SCAN', '* * * * *')`)
	s.Tick(ctx, time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC))
	waitUntil(t, "a duplicate scan", func() bool {
		var n int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM background_job WHERE kind = 'DUPLICATE_SCAN'`).Scan(&n)
		return n == 1
	})
}

func TestSchedulerAlertsWhenAScanCannotBeQueued(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	var mu sync.Mutex
	var alerts []string
	s := NewScheduler(pool, quietRunner(t, pool), func(_ context.Context, title, body string) {
		mu.Lock()
		alerts = append(alerts, fmt.Sprintf("%s: %s", title, body))
		mu.Unlock()
	})
	a := addLocation(t, pool, "a")
	pool.Exec(ctx, `INSERT INTO scan_schedule (library_id, cron_expr) VALUES (`+libraryOf+`, '* * * * *')`, a)
	// Make queueing a scan fail.
	if _, err := pool.Exec(ctx, `ALTER TABLE background_job ADD CONSTRAINT no_scans CHECK (kind <> 'SCAN')`); err != nil {
		t.Fatal(err)
	}

	s.Tick(ctx, time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC))
	mu.Lock()
	defer mu.Unlock()
	if len(alerts) != 1 {
		t.Errorf("alerts = %v, want exactly one", alerts)
	}
}

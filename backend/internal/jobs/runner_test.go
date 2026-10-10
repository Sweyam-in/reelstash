package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

func status(t *testing.T, pool *pgxpool.Pool, id int64) (st, errMsg string, result []byte) {
	t.Helper()
	var e *string
	if err := pool.QueryRow(context.Background(), `SELECT status, error, result FROM background_job WHERE id = $1`, id).Scan(&st, &e, &result); err != nil {
		t.Fatal(err)
	}
	if e != nil {
		errMsg = *e
	}
	return
}

func waitFor(t *testing.T, what string, cond func() bool) {
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

func waitStatus(t *testing.T, pool *pgxpool.Pool, id int64, want string) {
	t.Helper()
	waitFor(t, "job "+want, func() bool { s, _, _ := status(t, pool, id); return s == want })
}

// hooks captures the process-wide finish/change hooks for one test.
func hooks(t *testing.T) (finished func() []jobs.Finished, changes *atomic.Int32) {
	t.Helper()
	var mu sync.Mutex
	var got []jobs.Finished
	changes = new(atomic.Int32)
	jobs.SetFinishHook(func(f jobs.Finished) { mu.Lock(); got = append(got, f); mu.Unlock() })
	jobs.SetChangeHook(func() { changes.Add(1) })
	t.Cleanup(func() { jobs.SetFinishHook(func(jobs.Finished) {}); jobs.SetChangeHook(func() {}) })
	return func() []jobs.Finished { mu.Lock(); defer mu.Unlock(); return append([]jobs.Finished(nil), got...) }, changes
}

func TestJobRunsAndStoresResult(t *testing.T) {
	pool := testdb.Isolated(t)
	finished, changes := hooks(t)
	r := jobs.NewRunner(pool, 2)
	defer r.Shutdown(time.Second)

	var seen json.RawMessage
	r.Register("ECHO", func(ctx context.Context, db *pgxpool.Pool, id int64, payload json.RawMessage) (any, error) {
		seen = payload
		return map[string]int{"total": 3}, nil
	})
	id, err := r.Enqueue(context.Background(), "ECHO", map[string]string{"hello": "world"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, pool, id, "COMPLETED")

	var payload map[string]string
	if err := json.Unmarshal(seen, &payload); err != nil || payload["hello"] != "world" {
		t.Errorf("handler payload = %s", seen)
	}
	_, _, res := status(t, pool, id)
	var result map[string]int
	if err := json.Unmarshal(res, &result); err != nil || result["total"] != 3 {
		t.Errorf("stored result = %s", res)
	}
	var attempts int
	var started, finishedAt *time.Time
	pool.QueryRow(context.Background(), `SELECT attempts, started_at, finished_at FROM background_job WHERE id = $1`, id).Scan(&attempts, &started, &finishedAt)
	if attempts != 1 || started == nil || finishedAt == nil {
		t.Errorf("attempts=%d started=%v finished=%v", attempts, started, finishedAt)
	}
	waitFor(t, "finish hook", func() bool { return len(finished()) == 1 })
	if f := finished()[0]; f.ID != id || f.Kind != "ECHO" || f.Status != "COMPLETED" {
		t.Errorf("finished = %+v", f)
	}
	if changes.Load() < 3 {
		t.Errorf("change hook fired %d times; want enqueue, start and finish", changes.Load())
	}
}

func TestFailingHandlerMarksFailed(t *testing.T) {
	pool := testdb.Isolated(t)
	finished, _ := hooks(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	r.Register("BOOM", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) {
		return nil, errors.New("disk on fire")
	})
	id, _ := r.Enqueue(context.Background(), "BOOM", nil)
	waitStatus(t, pool, id, "FAILED")
	if _, msg, _ := status(t, pool, id); msg != "disk on fire" {
		t.Errorf("error = %q", msg)
	}
	waitFor(t, "finish hook", func() bool { return len(finished()) == 1 })
	if f := finished()[0]; f.Status != "FAILED" || f.Error != "disk on fire" {
		t.Errorf("finished = %+v", f)
	}
}

func TestUnknownKindFails(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	id, _ := r.Enqueue(context.Background(), "NOPE", nil)
	waitStatus(t, pool, id, "FAILED")
	if _, msg, _ := status(t, pool, id); msg != "no handler registered for kind NOPE" {
		t.Errorf("error = %q", msg)
	}
}

func TestEnqueueRejectsUnmarshalablePayload(t *testing.T) {
	pool := testdb.Isolated(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	if _, err := r.Enqueue(context.Background(), "X", make(chan int)); err == nil {
		t.Error("expected a JSON marshal error")
	}
}

func TestCancelRunningJob(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	started := make(chan struct{})
	r.Register("SLOW", func(ctx context.Context, _ *pgxpool.Pool, _ int64, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	id, _ := r.Enqueue(context.Background(), "SLOW", nil)
	<-started

	live, err := r.Cancel(context.Background(), id)
	if err != nil || !live {
		t.Fatalf("Cancel live = %v, %v", live, err)
	}
	waitStatus(t, pool, id, "CANCELLED")
	if _, msg, _ := status(t, pool, id); msg != "cancelled by user" {
		t.Errorf("error = %q", msg)
	}
}

func TestCancelPendingJobIsSkipped(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)

	block := make(chan struct{})
	var ranSecond atomic.Bool
	r.Register("BLOCK", func(ctx context.Context, _ *pgxpool.Pool, _ int64, _ json.RawMessage) (any, error) {
		<-block
		return nil, nil
	})
	r.Register("SECOND", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) {
		ranSecond.Store(true)
		return nil, nil
	})
	first, _ := r.Enqueue(context.Background(), "BLOCK", nil)
	waitStatus(t, pool, first, "RUNNING")
	second, _ := r.Enqueue(context.Background(), "SECOND", nil) // stuck behind the only worker

	live, err := r.Cancel(context.Background(), second)
	if err != nil || live {
		t.Fatalf("Cancel pending: live=%v err=%v", live, err)
	}
	close(block)
	waitStatus(t, pool, first, "COMPLETED")
	time.Sleep(100 * time.Millisecond)
	if ranSecond.Load() {
		t.Error("a cancelled pending job must not run")
	}
	if s, _, _ := status(t, pool, second); s != "CANCELLED" {
		t.Errorf("status = %s", s)
	}
}

func TestCancelFinishedOrUnknownJobErrors(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	r.Register("OK", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) { return nil, nil })
	id, _ := r.Enqueue(context.Background(), "OK", nil)
	waitStatus(t, pool, id, "COMPLETED")
	if _, err := r.Cancel(context.Background(), id); err == nil {
		t.Error("cancelling a completed job should fail")
	}
	if _, err := r.Cancel(context.Background(), 987654321); err == nil {
		t.Error("cancelling an unknown job should fail")
	}
}

// A job interrupted by shutdown is not a failure: it goes back to PENDING so
// the next boot's ResumePending runs it again.
func TestShutdownRevertsRunningJobAndResumePendingRerunsIt(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r1 := jobs.NewRunner(pool, 1)
	started := make(chan struct{})
	r1.Register("LONG", func(ctx context.Context, _ *pgxpool.Pool, _ int64, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	id, _ := r1.Enqueue(context.Background(), "LONG", map[string]int{"n": 1})
	<-started
	r1.Shutdown(2 * time.Second)
	waitStatus(t, pool, id, "PENDING")

	r2 := jobs.NewRunner(pool, 1)
	defer r2.Shutdown(time.Second)
	var got atomic.Value
	r2.Register("LONG", func(_ context.Context, _ *pgxpool.Pool, _ int64, payload json.RawMessage) (any, error) {
		got.Store(string(payload))
		return "done", nil
	})
	if err := r2.ResumePending(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, pool, id, "COMPLETED")
	var resumed map[string]int
	if err := json.Unmarshal([]byte(got.Load().(string)), &resumed); err != nil || resumed["n"] != 1 {
		t.Errorf("resumed payload = %v", got.Load())
	}
	var attempts int
	pool.QueryRow(context.Background(), `SELECT attempts FROM background_job WHERE id = $1`, id).Scan(&attempts)
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestWorkersRunJobsConcurrently(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 3)
	defer r.Shutdown(time.Second)
	var cur, peak atomic.Int32
	release := make(chan struct{})
	r.Register("PAR", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		cur.Add(-1)
		return nil, nil
	})
	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := r.Enqueue(context.Background(), "PAR", nil)
		ids = append(ids, id)
	}
	waitFor(t, "three workers busy", func() bool { return cur.Load() == 3 })
	close(release)
	for _, id := range ids {
		waitStatus(t, pool, id, "COMPLETED")
	}
	if peak.Load() != 3 {
		t.Errorf("peak concurrency = %d, want exactly the 3 workers", peak.Load())
	}
}

func TestZeroWorkersStillGetsOne(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 0)
	defer r.Shutdown(time.Second)
	r.Register("OK", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) { return nil, nil })
	id, _ := r.Enqueue(context.Background(), "OK", nil)
	waitStatus(t, pool, id, "COMPLETED")
}

func TestBurstEnqueueDoesNotDropJobs(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	release := make(chan struct{})
	var ran atomic.Int32
	r.Register("BURST", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) {
		ran.Add(1)
		<-release
		return nil, nil
	})
	var ids []int64
	for i := 0; i < 50; i++ {
		id, err := r.Enqueue(context.Background(), "BURST", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	close(release)
	for _, id := range ids {
		waitStatus(t, pool, id, "COMPLETED")
	}
	if ran.Load() != 50 {
		t.Errorf("ran %d of 50 jobs", ran.Load())
	}
}

func TestAdoptOrphansRestartsKilledJobAndClosesItsScan(t *testing.T) {
	pool := testdb.Isolated(t)
	hooks(t)
	var jobID, locID int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO background_job (kind, status, started_at) VALUES ('SCAN', 'RUNNING', now()) RETURNING id`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ('orphan', 'LOCAL', '/tmp') RETURNING id`).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO scan_job (storage_location_id, background_job_id, started_at) VALUES ($1, $2, now())`, locID, jobID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM scan_job WHERE background_job_id = $1`, jobID)
		pool.Exec(context.Background(), `DELETE FROM background_job WHERE id = $1`, jobID)
		pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, locID)
	})

	r := jobs.NewRunner(pool, 1)
	defer r.Shutdown(time.Second)
	r.Register("SCAN", func(context.Context, *pgxpool.Pool, int64, json.RawMessage) (any, error) { return "again", nil })
	if err := r.AdoptOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, pool, jobID, "COMPLETED")
	var open int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM scan_job WHERE background_job_id = $1 AND completed_at IS NULL`, jobID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Errorf("%d scan rows still open", open)
	}
}

func TestReportProgress(t *testing.T) {
	pool := testdb.Isolated(t)
	_, changes := hooks(t)
	var id int64
	pool.QueryRow(context.Background(), `INSERT INTO background_job (kind, status) VALUES ('X', 'RUNNING') RETURNING id`).Scan(&id)

	before := changes.Load()
	jobs.ReportProgress(pool, id, 4, 10, "halfway-ish")
	var done, total int
	var label string
	pool.QueryRow(context.Background(), `SELECT progress_done, progress_total, progress_label FROM background_job WHERE id = $1`, id).Scan(&done, &total, &label)
	if done != 4 || total != 10 || label != "halfway-ish" {
		t.Errorf("progress = %d/%d %q", done, total, label)
	}
	if changes.Load() != before+1 {
		t.Error("progress should ping the change hook")
	}
	jobs.ReportProgress(pool, 0, 1, 1, "ignored") // id 0 = no job
	if changes.Load() != before+1 {
		t.Error("id 0 must be a no-op")
	}
}

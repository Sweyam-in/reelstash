// Package jobs runs ReelStash's background work (scans, ffprobe, hashing,
// thumbnailing, NFO import, duplicate detection) on a bounded worker pool,
// with job state persisted in PostgreSQL.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Finished describes a job that just reached COMPLETED or FAILED.
type Finished struct {
	ID      int64
	Kind    string
	Status  string
	Error   string
	Payload json.RawMessage
	Result  json.RawMessage
}

var (
	changeHook atomic.Value // func()
	finishHook atomic.Value // func(Finished)
)

// SetChangeHook registers a function called whenever any job is queued,
// starts, reports progress or finishes. Call before starting runners.
func SetChangeHook(fn func()) { changeHook.Store(fn) }

// SetFinishHook registers a function called after a job completes or fails.
func SetFinishHook(fn func(Finished)) { finishHook.Store(fn) }

func changed() {
	if fn, ok := changeHook.Load().(func()); ok && fn != nil {
		fn()
	}
}

func finished(f Finished) {
	if fn, ok := finishHook.Load().(func(Finished)); ok && fn != nil {
		fn(f)
	}
}

type Handler func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (result any, err error)

type Runner struct {
	db       *pgxpool.Pool
	handlers map[string]Handler
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// backlog is every accepted job id not yet handed to a worker. It is
	// unbounded on purpose: a full channel used to leave rows PENDING until
	// the next process start, and ResumePending blocked startup once more
	// than the channel capacity were waiting.
	mu            sync.Mutex
	cond          *sync.Cond
	backlog       []int64
	jobCancels    map[int64]context.CancelFunc
	userCancelled map[int64]bool
}

func NewRunner(db *pgxpool.Pool, workers int) *Runner {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		db:            db,
		handlers:      make(map[string]Handler),
		ctx:           ctx,
		cancel:        cancel,
		jobCancels:    make(map[int64]context.CancelFunc),
		userCancelled: make(map[int64]bool),
	}
	r.cond = sync.NewCond(&r.mu)
	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		go r.worker()
	}
	return r
}

// Cancel requests that job id stop. A RUNNING job's context is cancelled so
// a well-behaved handler (one that checks ctx.Err() or uses ctx-derived
// timeouts/exec calls) unwinds promptly; the job is then marked CANCELLED
// rather than reverted to PENDING like a shutdown-interrupted job, so
// ResumePending never resurrects a job the user deliberately stopped. A job
// that is still PENDING (not yet dequeued) is marked CANCELLED directly;
// run() checks for that status before executing so it's skipped when its
// turn comes.
//
// live reports whether the job was actually tracked in this process (and so
// its own goroutine will finish tidying up any related state once its
// context is cancelled). A RUNNING job this process has no record of — left
// behind by a previous process that was killed before it could revert the
// row to PENDING — is treated the same as PENDING: marked CANCELLED
// directly, since no goroutine is coming to unwind it. The caller is then
// responsible for closing out any state that only that dead goroutine would
// have cleaned up.
func (r *Runner) Cancel(ctx context.Context, id int64) (live bool, err error) {
	r.mu.Lock()
	cancel, running := r.jobCancels[id]
	if running {
		r.userCancelled[id] = true
	}
	r.mu.Unlock()

	if running {
		cancel()
		return true, nil
	}

	tag, err := r.db.Exec(ctx, `
		UPDATE background_job SET status = 'CANCELLED', error = 'cancelled by user', finished_at = now()
		WHERE id = $1 AND status IN ('PENDING', 'RUNNING')
	`, id)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, fmt.Errorf("job %d is not pending or running", id)
	}
	changed()
	return false, nil
}

// Shutdown cancels every in-flight job's context (handlers are expected to
// check ctx.Err() at reasonable checkpoints and return promptly) and waits
// up to timeout for workers to drain. A job that was RUNNING when cancelled
// is reverted to PENDING (see run) so ResumePending picks it back up on the
// next boot instead of leaving a permanently stuck row.
func (r *Runner) Shutdown(timeout time.Duration) {
	r.cancel()
	r.mu.Lock()
	r.cond.Broadcast()
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("jobs: shutdown timed out after %s waiting for in-flight jobs", timeout)
	}
}

func (r *Runner) Register(kind string, handler Handler) {
	r.handlers[kind] = handler
}

// Enqueue persists a PENDING background_job row and schedules it for a
// worker to pick up.
func (r *Runner) Enqueue(ctx context.Context, kind string, payload any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.db.QueryRow(ctx, `
		INSERT INTO background_job (kind, status, payload)
		VALUES ($1, 'PENDING', $2)
		RETURNING id
	`, kind, raw).Scan(&id)
	if err != nil {
		return 0, err
	}
	changed()
	r.push(id)
	return id, nil
}

func (r *Runner) push(id int64) {
	r.mu.Lock()
	r.backlog = append(r.backlog, id)
	r.cond.Signal()
	r.mu.Unlock()
}

func (r *Runner) registeredKinds() []string {
	kinds := make([]string, 0, len(r.handlers))
	for kind := range r.handlers {
		kinds = append(kinds, kind)
	}
	return kinds
}

// AdoptOrphans returns jobs this process did not leave pending on purpose.
// A kill or crash skips Shutdown, so those rows stay RUNNING and an open
// scan_job keeps the next scan from starting. Call once at startup, after
// Register and before serving traffic.
func (r *Runner) AdoptOrphans(ctx context.Context) error {
	kinds := r.registeredKinds()
	if len(kinds) == 0 {
		return nil
	}
	if _, err := r.db.Exec(ctx, `
		UPDATE background_job
		SET status = 'PENDING', started_at = NULL
		WHERE status = 'RUNNING' AND kind = ANY($1)
	`, kinds); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		UPDATE scan_job
		SET completed_at = now()
		WHERE completed_at IS NULL
		  AND background_job_id IN (
			SELECT id FROM background_job WHERE kind = ANY($1) AND status <> 'RUNNING'
		  )
	`, kinds); err != nil {
		return err
	}
	return r.ResumePending(ctx)
}

// ResumePending re-queues PENDING jobs of the kinds this runner handles.
// Jobs for other runners are left alone, so the scan pool and the thumbnail
// pool do not take each other's work.
func (r *Runner) ResumePending(ctx context.Context) error {
	kinds := r.registeredKinds()
	if len(kinds) == 0 {
		return nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT id FROM background_job
		WHERE status = 'PENDING' AND kind = ANY($1)
		ORDER BY created_at
	`, kinds)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		r.push(id)
	}
	return nil
}

func (r *Runner) worker() {
	defer r.wg.Done()
	for {
		r.mu.Lock()
		for len(r.backlog) == 0 && r.ctx.Err() == nil {
			r.cond.Wait()
		}
		if r.ctx.Err() != nil || len(r.backlog) == 0 {
			r.mu.Unlock()
			return
		}
		id := r.backlog[0]
		r.backlog = r.backlog[1:]
		r.mu.Unlock()
		r.run(id)
	}
}

func (r *Runner) run(id int64) {
	var kind, status string
	var payload json.RawMessage
	if err := r.db.QueryRow(r.ctx, `SELECT kind, status, payload FROM background_job WHERE id = $1`, id).Scan(&kind, &status, &payload); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("jobs: load job %d: %v", id, err)
		}
		return
	}
	if status == "CANCELLED" {
		// Cancelled while still sitting PENDING in the queue: skip it.
		return
	}

	handler, ok := r.handlers[kind]
	if !ok {
		r.fail(context.Background(), id, "no handler registered for kind "+kind)
		changed()
		return
	}

	// Give this job its own cancellable context, derived from the runner's,
	// so Cancel(id) can stop just this job without affecting any other
	// in-flight job.
	jobCtx, jobCancel := context.WithCancel(r.ctx)
	r.mu.Lock()
	r.jobCancels[id] = jobCancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.jobCancels, id)
		delete(r.userCancelled, id)
		r.mu.Unlock()
		jobCancel()
	}()

	if _, err := r.db.Exec(jobCtx, `UPDATE background_job SET status = 'RUNNING', started_at = now(), attempts = attempts + 1 WHERE id = $1`, id); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("jobs: mark job %d running: %v", id, err)
		}
		return
	}
	changed()

	result, err := handler(jobCtx, r.db, id, payload)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			r.mu.Lock()
			userCancelled := r.userCancelled[id]
			r.mu.Unlock()
			if userCancelled {
				if _, cancelErr := r.db.Exec(context.Background(), `UPDATE background_job SET status = 'CANCELLED', error = 'cancelled by user', finished_at = now() WHERE id = $1`, id); cancelErr != nil {
					log.Printf("jobs: mark job %d cancelled: %v", id, cancelErr)
				}
				changed()
				return
			}
			// Interrupted by shutdown, not a real failure: revert to PENDING
			// (using a fresh context — jobCtx is already cancelled) so it
			// resumes cleanly via ResumePending on the next boot.
			if _, resetErr := r.db.Exec(context.Background(), `UPDATE background_job SET status = 'PENDING', started_at = NULL WHERE id = $1`, id); resetErr != nil {
				log.Printf("jobs: reset interrupted job %d to pending: %v", id, resetErr)
			}
			return
		}
		r.fail(context.Background(), id, err.Error())
		changed()
		finished(Finished{ID: id, Kind: kind, Status: "FAILED", Error: err.Error(), Payload: payload})
		return
	}

	resultJSON, _ := json.Marshal(result)
	if _, err := r.db.Exec(context.Background(), `UPDATE background_job SET status = 'COMPLETED', result = $2, finished_at = now() WHERE id = $1`, id, resultJSON); err != nil {
		log.Printf("jobs: mark job %d completed: %v", id, err)
	}
	changed()
	finished(Finished{ID: id, Kind: kind, Status: "COMPLETED", Payload: payload, Result: resultJSON})
}

// ReportProgress records how far a running job has got. Best-effort: a
// failed write is ignored because progress is purely informational.
func ReportProgress(db *pgxpool.Pool, id int64, done, total int, label string) {
	if id == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db.Exec(ctx, `UPDATE background_job SET progress_done = $2, progress_total = $3, progress_label = $4 WHERE id = $1`, id, done, total, label)
	changed()
}

func (r *Runner) fail(ctx context.Context, id int64, message string) {
	if _, err := r.db.Exec(ctx, `UPDATE background_job SET status = 'FAILED', error = $2, finished_at = now() WHERE id = $1`, id, message); err != nil {
		log.Printf("jobs: mark job %d failed: %v", id, err)
	}
}

// AwaitQuiet blocks briefly to let already-queued jobs drain; used only by
// tests/manual smoke checks, never by request handlers.
func AwaitQuiet(d time.Duration) { time.Sleep(d) }

package schedule

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
)

// Alert reports something an admin should hear about (a schedule that could
// not queue its job, a watcher that could not start). It is optional.
type Alert func(ctx context.Context, title, body string)

// Scheduler fires the rows of scan_schedule. It checks once a minute; a run
// missed because the server was down is not made up afterwards.
type Scheduler struct {
	DB     *pgxpool.Pool
	Jobs   *jobs.Runner
	Alert  Alert
	Now    func() time.Time // for tests
	ticker time.Duration
}

func NewScheduler(db *pgxpool.Pool, runner *jobs.Runner, alert Alert) *Scheduler {
	return &Scheduler{DB: db, Jobs: runner, Alert: alert, Now: time.Now, ticker: time.Minute}
}

// Run blocks until ctx ends, waking at the start of every minute.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		now := s.Now()
		wait := now.Truncate(time.Minute).Add(s.ticker).Sub(now)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		s.Tick(ctx, s.Now())
	}
}

type dueRow struct {
	id        int64
	kind      string
	libraryID *int64
	expr      string
	lastRun   *time.Time
}

// Tick runs every enabled schedule whose expression matches the minute of now.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) {
	minute := now.Truncate(time.Minute)
	rows, err := s.DB.Query(ctx, `SELECT id, job_kind, library_id, cron_expr, last_run_at FROM scan_schedule WHERE enabled`)
	if err != nil {
		log.Printf("schedule: load schedules: %v", err)
		return
	}
	var all []dueRow
	for rows.Next() {
		var d dueRow
		if err := rows.Scan(&d.id, &d.kind, &d.libraryID, &d.expr, &d.lastRun); err != nil {
			log.Printf("schedule: read schedule: %v", err)
			continue
		}
		all = append(all, d)
	}
	rows.Close()

	for _, d := range all {
		cron, err := ParseCron(d.expr)
		if err != nil {
			log.Printf("schedule %d: invalid cron %q: %v", d.id, d.expr, err)
			continue
		}
		if !cron.Matches(minute) || (d.lastRun != nil && !d.lastRun.Before(minute)) {
			continue
		}
		if _, err := s.DB.Exec(ctx, `UPDATE scan_schedule SET last_run_at = $2 WHERE id = $1`, d.id, now); err != nil {
			log.Printf("schedule %d: record run: %v", d.id, err)
			continue
		}
		s.fire(ctx, d)
	}
}

func (s *Scheduler) fire(ctx context.Context, d dueRow) {
	switch d.kind {
	case "DUPLICATE_SCAN":
		if _, err := EnqueueDuplicateScan(ctx, s.DB, s.Jobs); err != nil && !errors.Is(err, ErrJobRunning) {
			s.alert(ctx, "Scheduled duplicate scan could not start", err.Error())
		}
	case "HOUSEKEEPING":
		if _, err := EnqueueHousekeeping(ctx, s.DB, s.Jobs, nil, false); err != nil && !errors.Is(err, ErrJobRunning) {
			s.alert(ctx, "Scheduled housekeeping could not start", err.Error())
		}
	default:
		ids, err := s.libraryIDs(ctx, d.libraryID)
		if err != nil {
			s.alert(ctx, "Scheduled scan could not start", err.Error())
			return
		}
		for _, id := range ids {
			_, err := EnqueueLibraryScan(ctx, s.DB, s.Jobs, id)
			if err != nil && !errors.Is(err, ErrScanRunning) && !errors.Is(err, ErrOrganizing) && !errors.Is(err, ErrLibraryEmpty) {
				s.alert(ctx, "Scheduled scan could not start", fmt.Sprintf("library %d: %v", id, err))
			}
		}
	}
}

// libraryIDs is the one library a schedule names, or every library.
func (s *Scheduler) libraryIDs(ctx context.Context, only *int64) ([]int64, error) {
	if only != nil {
		return []int64{*only}, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT id FROM library ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func (s *Scheduler) alert(ctx context.Context, title, body string) {
	log.Printf("schedule: %s: %s", title, body)
	if s.Alert != nil {
		s.Alert(ctx, title, body)
	}
}

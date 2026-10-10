package schedule

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
)

var (
	// ErrScanRunning means a scan for the storage location is already queued or running.
	ErrScanRunning = errors.New("a scan is already running for this storage location")
	// ErrOrganizing means the organizer is moving files on the storage location.
	ErrOrganizing = errors.New("the organizer is moving files on this storage location; scan once it finishes")
)

// EnqueueScan queues an async SCAN job for a storage location. It refuses to
// queue a second scan while one is pending or running for the same location
// (a scan stuck on a stalled mount would otherwise pile up), and while the
// organizer is moving files there.
func EnqueueScan(ctx context.Context, db *pgxpool.Pool, runner *jobs.Runner, storageLocationID int64) (int64, error) {
	if err := checkScannable(ctx, db, storageLocationID); err != nil {
		return 0, err
	}
	return runner.Enqueue(ctx, "SCAN", map[string]any{"storageLocationId": storageLocationID})
}

// checkScannable refuses a scan while another scan (whole disk or library) is
// queued or running on the storage location, or the organizer is moving files.
func checkScannable(ctx context.Context, db *pgxpool.Pool, storageLocationID int64) error {
	var existing int64
	err := db.QueryRow(ctx, `
		SELECT id FROM scan_job WHERE storage_location_id = $1 AND completed_at IS NULL
		UNION ALL
		SELECT id FROM background_job WHERE kind = 'SCAN' AND status = 'PENDING' AND payload->>'storageLocationId' = $1::text
		LIMIT 1
	`, storageLocationID).Scan(&existing)
	if err == nil {
		return ErrScanRunning
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var organizing bool
	_ = db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM background_job WHERE kind = 'ORGANIZE' AND status IN ('PENDING','RUNNING')
		               AND payload->>'storageLocationId' = $1::text)
	`, storageLocationID).Scan(&organizing)
	if organizing {
		return ErrOrganizing
	}
	return nil
}

// ErrLibraryEmpty means the library has no folders to scan.
var ErrLibraryEmpty = errors.New("this library has no folders to scan")

// EnqueueLibraryScan queues one SCAN job per disk the library has folders on,
// each walking only the library's folders there. Nothing is queued unless
// every one of those disks is free to scan.
func EnqueueLibraryScan(ctx context.Context, db *pgxpool.Pool, runner *jobs.Runner, libraryID int64) ([]int64, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT storage_location_id FROM library_root WHERE library_id = $1 ORDER BY 1`, libraryID)
	if err != nil {
		return nil, err
	}
	locations, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		return nil, ErrLibraryEmpty
	}
	for _, id := range locations {
		if err := checkScannable(ctx, db, id); err != nil {
			return nil, err
		}
	}
	jobIDs := make([]int64, 0, len(locations))
	for _, id := range locations {
		jobID, err := runner.Enqueue(ctx, "SCAN", map[string]any{"storageLocationId": id, "libraryId": libraryID})
		if err != nil {
			return jobIDs, err
		}
		jobIDs = append(jobIDs, jobID)
	}
	return jobIDs, nil
}

// ErrJobRunning means a job of this kind is already queued or running.
var ErrJobRunning = errors.New("this job is already running")

// EnqueueDuplicateScan queues a DUPLICATE_SCAN unless one is already active.
func EnqueueDuplicateScan(ctx context.Context, db *pgxpool.Pool, runner *jobs.Runner) (int64, error) {
	var existing int64
	err := db.QueryRow(ctx, `SELECT id FROM background_job WHERE kind = 'DUPLICATE_SCAN' AND status IN ('PENDING', 'RUNNING') LIMIT 1`).Scan(&existing)
	if err == nil {
		return 0, ErrJobRunning
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	return runner.Enqueue(ctx, "DUPLICATE_SCAN", map[string]any{})
}

// EnqueueHousekeeping queues a HOUSEKEEPING run unless one is already active.
// No tasks means the default set.
func EnqueueHousekeeping(ctx context.Context, db *pgxpool.Pool, runner *jobs.Runner, tasks []string, dryRun bool) (int64, error) {
	var existing int64
	err := db.QueryRow(ctx, `SELECT id FROM background_job WHERE kind = 'HOUSEKEEPING' AND status IN ('PENDING', 'RUNNING') LIMIT 1`).Scan(&existing)
	if err == nil {
		return 0, ErrJobRunning
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if tasks == nil {
		tasks = []string{}
	}
	return runner.Enqueue(ctx, "HOUSEKEEPING", map[string]any{"tasks": tasks, "dryRun": dryRun})
}

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"

	"reelstash/backend/internal/schedule"
)

// hiddenJobKinds are per-item housekeeping jobs (one per file / performer /
// studio). They would bury the jobs an admin actually started, so the Jobs
// page leaves them out unless asked for by kind.
var hiddenJobKinds = []string{"THUMBNAIL", "NAMER", "PERFORMER_MATCH", "STUDIO_MATCH", "SPRITE", "PREVIEW"}

type jobScanInfo struct {
	FilesSeen      int32    `json:"filesSeen"`
	FilesProcessed int32    `json:"filesProcessed"`
	Discovered     int32    `json:"discovered"`
	Changed        int32    `json:"changed"`
	Missing        int32    `json:"missing"`
	Phase          string   `json:"phase"`
	WalkComplete   bool     `json:"walkComplete"`
	CurrentPath    *string  `json:"currentPath,omitempty"`
	RecentPaths    []string `json:"recentPaths"`
}

type jobView struct {
	ID              int64           `json:"id"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Error           *string         `json:"error,omitempty"`
	Attempts        int32           `json:"attempts"`
	CreatedAt       string          `json:"createdAt"`
	StartedAt       *string         `json:"startedAt,omitempty"`
	FinishedAt      *string         `json:"finishedAt,omitempty"`
	DurationSeconds *float64        `json:"durationSeconds,omitempty"`
	StorageLocation *string         `json:"storageLocationName,omitempty"`
	StorageID       *int64          `json:"storageLocationId,omitempty"`
	LibraryID       *int64          `json:"libraryId,omitempty"`
	LibraryName     *string         `json:"libraryName,omitempty"`
	ProgressDone    int32           `json:"progressDone"`
	ProgressTotal   int32           `json:"progressTotal"`
	ProgressLabel   *string         `json:"progressLabel,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Scan            *jobScanInfo    `json:"scan,omitempty"`
}

const jobSelect = `
	SELECT bj.id, bj.kind, bj.status, bj.error, bj.attempts, bj.created_at::text, bj.started_at::text, bj.finished_at::text,
	       EXTRACT(EPOCH FROM (COALESCE(bj.finished_at, now()) - bj.started_at))::float8,
	       sl.id, sl.name, lib.id, lib.name,
	       bj.progress_done, bj.progress_total, bj.progress_label, bj.payload, bj.result,
	       sj.files_seen, sj.files_processed, sj.discovered_count, sj.changed_count, sj.missing_count,
	       sj.phase, sj.walk_complete, sj.current_path, sj.recent_paths
	FROM background_job bj
	LEFT JOIN storage_location sl ON sl.id = NULLIF(bj.payload->>'storageLocationId', '')::bigint
	LEFT JOIN library lib ON lib.id = NULLIF(bj.payload->>'libraryId', '')::bigint
	LEFT JOIN scan_job sj ON sj.background_job_id = bj.id
`

func scanJobView(row pgx.Row) (jobView, error) {
	var j jobView
	var payload, result, recentRaw []byte
	var filesSeen, filesProcessed, discovered, changed, missing *int32
	var phase *string
	var walkComplete *bool
	var currentPath *string
	if err := row.Scan(&j.ID, &j.Kind, &j.Status, &j.Error, &j.Attempts, &j.CreatedAt, &j.StartedAt, &j.FinishedAt,
		&j.DurationSeconds, &j.StorageID, &j.StorageLocation, &j.LibraryID, &j.LibraryName,
		&j.ProgressDone, &j.ProgressTotal, &j.ProgressLabel, &payload, &result,
		&filesSeen, &filesProcessed, &discovered, &changed, &missing,
		&phase, &walkComplete, &currentPath, &recentRaw); err != nil {
		return j, err
	}
	if len(payload) > 0 {
		j.Payload = payload
	}
	if len(result) > 0 && string(result) != "null" {
		j.Result = result
	}
	if filesSeen != nil {
		info := &jobScanInfo{RecentPaths: []string{}, CurrentPath: currentPath}
		info.FilesSeen, info.FilesProcessed = *filesSeen, *filesProcessed
		info.Discovered, info.Changed, info.Missing = *discovered, *changed, *missing
		if phase != nil {
			info.Phase = *phase
		}
		if walkComplete != nil {
			info.WalkComplete = *walkComplete
		}
		if len(recentRaw) > 0 {
			_ = json.Unmarshal(recentRaw, &info.RecentPaths)
			if info.RecentPaths == nil {
				info.RecentPaths = []string{}
			}
		}
		j.Scan = info
	}
	return j, nil
}

// jobsSnapshot returns the newest jobs (optionally filtered by status and
// kind) plus per-status counts and the most recent run of every kind, which
// the Jobs page uses for its task cards. It backs both the JSON endpoint and
// the live event stream.
func (a *API) jobsSnapshot(ctx context.Context, status, kind string, limit int) (map[string]any, error) {
	return a.jobsPage(ctx, status, kind, limit, 0)
}

// jobsPage is jobsSnapshot from an offset; total is the number of jobs matching the filters.
func (a *API) jobsPage(ctx context.Context, status, kind string, limit, offset int) (map[string]any, error) {
	var total int64
	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*) FROM background_job bj
		WHERE ($1 = '' OR bj.status = $1) AND (($2 <> '' AND bj.kind = $2) OR ($2 = '' AND bj.kind <> ALL($3)))
	`, status, kind, hiddenJobKinds).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, jobSelect+`
		WHERE ($1 = '' OR bj.status = $1)
		  AND (($2 <> '' AND bj.kind = $2) OR ($2 = '' AND bj.kind <> ALL($3)))
		ORDER BY bj.id DESC
		LIMIT $4 OFFSET $5
	`, status, kind, hiddenJobKinds, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []jobView{}
	for rows.Next() {
		j, err := scanJobView(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	counts := map[string]int64{}
	crow, err := a.DB.Query(ctx, `SELECT status, COUNT(*) FROM background_job WHERE kind <> ALL($1) GROUP BY status`, hiddenJobKinds)
	if err == nil {
		for crow.Next() {
			var s string
			var n int64
			if crow.Scan(&s, &n) == nil {
				counts[s] = n
			}
		}
		crow.Close()
	}

	latest := map[string]jobView{}
	lrows, err := a.DB.Query(ctx, jobSelect+`
		WHERE bj.id IN (SELECT MAX(id) FROM background_job GROUP BY kind)
	`)
	if err == nil {
		for lrows.Next() {
			if j, err := scanJobView(lrows); err == nil {
				j.Payload, j.Scan = nil, nil
				latest[j.Kind] = j
			}
		}
		lrows.Close()
	}

	var thumbsPending, previewsPending int64
	_ = a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM background_job WHERE kind = 'THUMBNAIL' AND status IN ('PENDING','RUNNING')`).Scan(&thumbsPending)
	_ = a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM background_job WHERE kind IN ('SPRITE','PREVIEW') AND status IN ('PENDING','RUNNING')`).Scan(&previewsPending)

	return map[string]any{
		"items": items, "total": total, "counts": counts, "latestByKind": latest,
		"thumbnailsPending": thumbsPending, "previewsPending": previewsPending,
	}, nil
}

func (a *API) listBackgroundJobs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	snapshot, err := a.jobsPage(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("kind"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load background jobs")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// runJobRequest starts a job. Library kinds take a library (or, for SCAN,
// ORGANIZE and METADATA_MATCH started the old way, a storage location).
type runJobRequest struct {
	Kind              string `json:"kind"`
	LibraryID         int64  `json:"libraryId"`
	StorageLocationID int64  `json:"storageLocationId"`
}

// libraryJobKinds run on one library, across every disk it has folders on.
var libraryJobKinds = map[string]bool{"SCAN": true, "ORGANIZE": true, "METADATA_MATCH": true}

var locationJobKinds = map[string]bool{"SCAN": true, "ORGANIZE": true, "METADATA_MATCH": true}
var globalJobKinds = map[string]bool{
	"DUPLICATE_SCAN": true, "PERFORMER_SYNC": true, "STUDIO_SYNC": true,
	"SPRITE_SCAN": true, "PREVIEW_SCAN": true, "THUMBNAIL_FIX": true, "REMOVE_MISSING": true, "MP4_REPAIR": true, "TRACK_SCAN": true,
}

// startJob enqueues a job of kind, refusing to stack a second copy of a
// kind (per storage location, where applicable) that is already active.
func (a *API) startJob(ctx context.Context, kind string, storageLocationID int64) (jobID int64, status int, msg string) {
	return a.startJobWith(ctx, kind, storageLocationID, nil)
}

// startJobWith is startJob with extra payload fields (e.g. the library an
// ORGANIZE job works on).
func (a *API) startJobWith(ctx context.Context, kind string, storageLocationID int64, extra map[string]any) (jobID int64, status int, msg string) {
	if a.Jobs == nil {
		return 0, http.StatusServiceUnavailable, "job runner not available"
	}
	if kind == "SCAN" {
		return a.enqueueScan(ctx, storageLocationID)
	}
	var payload map[string]any = map[string]any{}
	for k, v := range extra {
		payload[k] = v
	}
	if locationJobKinds[kind] {
		if storageLocationID == 0 {
			return 0, http.StatusBadRequest, "storageLocationId is required for " + kind
		}
		var exists bool
		if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM storage_location WHERE id = $1)`, storageLocationID).Scan(&exists); err != nil || !exists {
			return 0, http.StatusNotFound, "storage location not found"
		}
		payload["storageLocationId"] = storageLocationID
		if kind == "ORGANIZE" {
			payload["dryRun"] = false
			// A scan walking the disk while files move would see each one
			// vanish and reappear as a new title.
			var scanning bool
			if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM scan_job WHERE storage_location_id = $1 AND completed_at IS NULL)`, storageLocationID).Scan(&scanning); err != nil {
				return 0, http.StatusInternalServerError, "failed to check for an in-progress scan"
			} else if scanning {
				return 0, http.StatusConflict, "a scan is running on this storage location; organize once it finishes"
			}
		}
	} else if !globalJobKinds[kind] {
		return 0, http.StatusBadRequest, "unknown job kind"
	}

	var activeID int64
	err := a.DB.QueryRow(ctx, `
		SELECT id FROM background_job
		WHERE kind = $1 AND status IN ('PENDING','RUNNING')
		  AND ($2::bigint = 0 OR payload->>'storageLocationId' = $2::text)
		LIMIT 1
	`, kind, storageLocationID).Scan(&activeID)
	if err == nil {
		return 0, http.StatusConflict, "this job is already queued or running"
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, http.StatusInternalServerError, "failed to check for a running job"
	}
	jobID, err = a.Jobs.Enqueue(ctx, kind, payload)
	if err != nil {
		return 0, http.StatusInternalServerError, "failed to enqueue job"
	}
	return jobID, 0, ""
}

func (a *API) runBackgroundJob(w http.ResponseWriter, r *http.Request) {
	var req runJobRequest
	if err := decodeJSON(r, &req); err != nil || req.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind is required")
		return
	}
	if libraryJobKinds[req.Kind] && (req.LibraryID > 0 || req.StorageLocationID == 0) {
		jobIDs, status, msg := a.startLibraryJob(r, req.Kind, req.LibraryID)
		if status != 0 {
			writeError(w, status, msg)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobIDs[0], "backgroundJobIds": jobIDs})
		return
	}
	jobID, status, msg := a.startJob(r.Context(), req.Kind, req.StorageLocationID)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

// startLibraryJob starts a library kind on one library: a scan or an
// organize run per disk, or one metadata match for the whole library.
func (a *API) startLibraryJob(r *http.Request, kind string, libraryID int64) (jobIDs []int64, status int, msg string) {
	ctx := r.Context()
	if a.Jobs == nil {
		return nil, http.StatusServiceUnavailable, "job runner not available"
	}
	if libraryID <= 0 {
		return nil, http.StatusBadRequest, "libraryId is required for " + kind
	}
	var exists bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM library WHERE id = $1)`, libraryID).Scan(&exists); err != nil {
		return nil, http.StatusInternalServerError, "failed to load library"
	} else if !exists {
		return nil, http.StatusNotFound, "library not found"
	}
	switch kind {
	case "SCAN":
		ids, err := schedule.EnqueueLibraryScan(ctx, a.DB, a.Jobs, libraryID)
		switch {
		case errors.Is(err, schedule.ErrScanRunning), errors.Is(err, schedule.ErrOrganizing):
			return nil, http.StatusConflict, err.Error()
		case errors.Is(err, schedule.ErrLibraryEmpty):
			return nil, http.StatusBadRequest, err.Error()
		case err != nil:
			return nil, http.StatusInternalServerError, "failed to enqueue scan"
		}
		return ids, 0, ""
	case "ORGANIZE":
		return a.organizeLibrary(r, libraryID)
	default:
		var active bool
		if err := a.DB.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM background_job WHERE kind = $1 AND status IN ('PENDING','RUNNING') AND payload->>'libraryId' = $2::text)
		`, kind, libraryID).Scan(&active); err != nil {
			return nil, http.StatusInternalServerError, "failed to check for a running job"
		} else if active {
			return nil, http.StatusConflict, "this job is already queued or running"
		}
		id, err := a.Jobs.Enqueue(ctx, kind, map[string]any{"libraryId": libraryID})
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to enqueue job"
		}
		return []int64{id}, 0, ""
	}
}

func (a *API) cancelBackgroundJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	var kind, jobStatus string
	var payload []byte
	if err := a.DB.QueryRow(r.Context(), `SELECT kind, status, payload FROM background_job WHERE id = $1`, id).Scan(&kind, &jobStatus, &payload); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	if kind == "SCAN" && jobStatus == "RUNNING" {
		var p struct {
			StorageLocationID int64 `json:"storageLocationId"`
		}
		_ = json.Unmarshal(payload, &p)
		if status, msg := a.cancelScanForLocation(r.Context(), p.StorageLocationID); status != 0 {
			writeError(w, status, msg)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if _, err := a.Jobs.Cancel(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// retryBackgroundJob enqueues a fresh copy of a finished job with the same
// payload.
func (a *API) retryBackgroundJob(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	var kind, status string
	var payload []byte
	if err := a.DB.QueryRow(r.Context(), `SELECT kind, status, payload FROM background_job WHERE id = $1`, id).Scan(&kind, &status, &payload); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}
	if status == "PENDING" || status == "RUNNING" {
		writeError(w, http.StatusConflict, "job is still active")
		return
	}
	var p struct {
		StorageLocationID int64 `json:"storageLocationId"`
		LibraryID         int64 `json:"libraryId"`
	}
	_ = json.Unmarshal(payload, &p)
	var newID int64
	var st int
	var msg string
	switch {
	case kind == "ORGANIZE" && p.LibraryID > 0:
		// One disk of a library: organize that disk again.
		if busy := a.busyLocation(r, p.StorageLocationID); busy != "" {
			st, msg = http.StatusConflict, busy
		} else {
			newID, st, msg = a.startJobWith(r.Context(), kind, p.StorageLocationID, map[string]any{"libraryId": p.LibraryID})
		}
	case kind == FileTransferJobKind:
		// Re-run the same paste; items it already finished are skipped.
		if a.Jobs == nil {
			st, msg = http.StatusServiceUnavailable, "background jobs are not available"
		} else if id, err := a.Jobs.Enqueue(r.Context(), kind, json.RawMessage(payload)); err != nil {
			st, msg = http.StatusInternalServerError, "failed to queue the transfer"
		} else {
			newID = id
		}
	case libraryJobKinds[kind] && p.LibraryID > 0:
		var ids []int64
		if ids, st, msg = a.startLibraryJob(r, kind, p.LibraryID); st == 0 {
			newID = ids[0]
		}
	default:
		newID, st, msg = a.startJob(r.Context(), kind, p.StorageLocationID)
	}
	if st != 0 {
		writeError(w, st, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": newID})
}

// clearFinishedJobs deletes finished job rows (history) but never an active
// job. scan_job rows keep their history; their background_job link is
// nulled by the FK.
func (a *API) clearFinishedJobs(w http.ResponseWriter, r *http.Request) {
	tag, err := a.DB.Exec(r.Context(), `
		DELETE FROM background_job WHERE status IN ('COMPLETED','FAILED','CANCELLED') AND kind <> ALL($1)
	`, hiddenJobKinds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear job history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": tag.RowsAffected()})
}

// organizing reports whether an ORGANIZE job is queued or running for the
// storage location.
func organizing(ctx context.Context, a *API, storageLocationID int64) bool {
	var active bool
	_ = a.DB.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM background_job WHERE kind = 'ORGANIZE' AND status IN ('PENDING','RUNNING')
		               AND payload->>'storageLocationId' = $1::text)
	`, storageLocationID).Scan(&active)
	return active
}

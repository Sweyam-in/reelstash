package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"

	"reelstash/backend/internal/schedule"
)

type storageLocation struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	MountPath         string  `json:"mountPath"`
	TotalCapacity     *int64  `json:"totalCapacity,omitempty"`
	UsedCapacity      *int64  `json:"usedCapacity,omitempty"`
	AvailableCapacity *int64  `json:"availableCapacity,omitempty"`
	Online            bool    `json:"online"`
	LastScanAt        *string `json:"lastScanAt,omitempty"`
}

func (a *API) listStorageLocations(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT id, name, type, mount_path, total_capacity, used_capacity, available_capacity, online, last_scan_at::text
		FROM storage_location
		ORDER BY name
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load storage locations")
		return
	}
	defer rows.Close()

	items := []storageLocation{}
	for rows.Next() {
		var s storageLocation
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.MountPath, &s.TotalCapacity, &s.UsedCapacity, &s.AvailableCapacity, &s.Online, &s.LastScanAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read storage location")
			return
		}
		items = append(items, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createStorageLocationRequest struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	MountPath string `json:"mountPath"`
}

var validStorageTypes = map[string]bool{"LOCAL": true, "EXTERNAL": true, "NAS": true, "NETWORK_SHARE": true, "ARCHIVE": true}

func (a *API) createStorageLocation(w http.ResponseWriter, r *http.Request) {
	var req createStorageLocationRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.MountPath == "" {
		writeError(w, http.StatusBadRequest, "name and mountPath are required")
		return
	}
	if !validStorageTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "type must be one of LOCAL, EXTERNAL, NAS, NETWORK_SHARE, ARCHIVE")
		return
	}

	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO storage_location (name, type, mount_path) VALUES ($1, $2, $3)
		RETURNING id
	`, req.Name, req.Type, req.MountPath).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create storage location (name/mount path may already exist)")
		return
	}
	// Read the new disk's capacity and status now rather than at the next hourly reading.
	if a.Scanner != nil {
		go a.Scanner.SampleCapacity(context.Background())
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

type updateStorageLocationRequest struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

func (a *API) updateStorageLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid storage location id")
		return
	}
	var req updateStorageLocationRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.MountPath == "" {
		writeError(w, http.StatusBadRequest, "name and mountPath are required")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE storage_location SET name = $2, mount_path = $3, updated_at = now() WHERE id = $1
	`, id, req.Name, req.MountPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update storage location")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "storage location not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// deleteStorageLocation refuses to delete a location that still has
// indexed files — the caller must migrate or explicitly clear them first,
// since media_file.storage_location_id is ON DELETE RESTRICT.
func (a *API) deleteStorageLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid storage location id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM storage_location WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusConflict, "cannot delete a storage location that still has indexed files")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "storage location not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type scanJob struct {
	ID                int64    `json:"id"`
	StorageLocationID int64    `json:"storageLocationId"`
	StorageLocation   string   `json:"storageLocationName"`
	LibraryID         *int64   `json:"libraryId,omitempty"`
	LibraryName       *string  `json:"libraryName,omitempty"`
	BackgroundJobID   *int64   `json:"backgroundJobId,omitempty"`
	DiscoveredCount   int32    `json:"discoveredCount"`
	ChangedCount      int32    `json:"changedCount"`
	MissingCount      int32    `json:"missingCount"`
	FilesSeen         int32    `json:"filesSeen"`
	FilesProcessed    int32    `json:"filesProcessed"`
	CurrentPath       *string  `json:"currentPath,omitempty"`
	RecentPaths       []string `json:"recentPaths"`
	Phase             string   `json:"phase"`
	WalkComplete      bool     `json:"walkComplete"`
	StartedAt         *string  `json:"startedAt,omitempty"`
	CompletedAt       *string  `json:"completedAt,omitempty"`
}

func (a *API) listScanJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT sj.id, sj.storage_location_id, COALESCE(sl.name, 'Unknown'), sj.library_id, lib.name, sj.background_job_id,
		       sj.discovered_count, sj.changed_count, sj.missing_count,
		       sj.files_seen, sj.files_processed, sj.current_path, sj.recent_paths, sj.phase, sj.walk_complete,
		       sj.started_at::text, sj.completed_at::text
		FROM scan_job sj
		LEFT JOIN storage_location sl ON sl.id = sj.storage_location_id
		LEFT JOIN library lib ON lib.id = sj.library_id
		ORDER BY sj.id DESC
		LIMIT 50
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load scan jobs")
		return
	}
	defer rows.Close()

	items := []scanJob{}
	for rows.Next() {
		var s scanJob
		var recentRaw []byte
		if err := rows.Scan(&s.ID, &s.StorageLocationID, &s.StorageLocation, &s.LibraryID, &s.LibraryName, &s.BackgroundJobID, &s.DiscoveredCount, &s.ChangedCount, &s.MissingCount, &s.FilesSeen, &s.FilesProcessed, &s.CurrentPath, &recentRaw, &s.Phase, &s.WalkComplete, &s.StartedAt, &s.CompletedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read scan job")
			return
		}
		s.RecentPaths = []string{}
		if len(recentRaw) > 0 {
			_ = json.Unmarshal(recentRaw, &s.RecentPaths)
			if s.RecentPaths == nil {
				s.RecentPaths = []string{}
			}
		}
		items = append(items, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// enqueueScan enqueues an async SCAN background_job for a storage location.
// It never scans inline on the request goroutine. It refuses to enqueue a
// second scan while one is already running/pending for the same location —
// previously every click queued a new one, and a scan stuck on a stalled
// mount would pile up indefinitely instead of surfacing the problem.
// On failure it returns the HTTP status and message to report.
func (a *API) enqueueScan(ctx context.Context, storageLocationID int64) (jobID int64, status int, msg string) {
	if a.Jobs == nil {
		return 0, http.StatusServiceUnavailable, "job runner not available"
	}
	jobID, err := schedule.EnqueueScan(ctx, a.DB, a.Jobs, storageLocationID)
	switch {
	case errors.Is(err, schedule.ErrScanRunning), errors.Is(err, schedule.ErrOrganizing):
		return 0, http.StatusConflict, err.Error()
	case err != nil:
		return 0, http.StatusInternalServerError, "failed to enqueue scan"
	}
	return jobID, 0, ""
}

func (a *API) startScan(w http.ResponseWriter, r *http.Request) {
	storageLocationID, err := pathInt64(r, "storageLocationID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid storage location id")
		return
	}
	jobID, status, msg := a.enqueueScan(r.Context(), storageLocationID)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": jobID})
}

// cancelScanForLocation stops the running/pending scan for a storage
// location, if any. On failure it returns the HTTP status and message.
func (a *API) cancelScanForLocation(ctx context.Context, storageLocationID int64) (status int, msg string) {
	if a.Jobs == nil {
		return http.StatusServiceUnavailable, "job runner not available"
	}

	var backgroundJobID *int64
	err := a.DB.QueryRow(ctx, `
		SELECT background_job_id FROM scan_job
		WHERE storage_location_id = $1 AND completed_at IS NULL
		ORDER BY id DESC LIMIT 1
	`, storageLocationID).Scan(&backgroundJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound, "no scan is running for this storage location"
	} else if err != nil {
		return http.StatusInternalServerError, "failed to look up the running scan"
	}
	closeStale := func() (int, string) {
		if _, err := a.DB.Exec(ctx, `
			UPDATE scan_job SET completed_at = now() WHERE id IN (
				SELECT id FROM scan_job WHERE storage_location_id = $1 AND completed_at IS NULL
			)
		`, storageLocationID); err != nil {
			return http.StatusInternalServerError, "failed to close stale scan"
		}
		return 0, ""
	}
	// A scan_job row with no linked background_job predates per-job
	// cancellation entirely (it was inserted by an older build); there's no
	// job to cancel, so just close the row out directly.
	if backgroundJobID == nil {
		return closeStale()
	}

	live, err := a.Jobs.Cancel(ctx, *backgroundJobID)
	if err != nil {
		return http.StatusInternalServerError, "failed to cancel scan: " + err.Error()
	}
	// A live cancel lets the still-running scan goroutine close out its own
	// scan_job row (with real discovered/changed/missing counts) once it
	// unwinds. A non-live cancel means the background_job was RUNNING in the
	// database but orphaned — left behind by a process that was killed
	// before it could revert the row — so no goroutine is coming; close the
	// scan_job row here instead of leaving it stuck at "Running" forever.
	if !live {
		return closeStale()
	}
	return 0, ""
}

// cancelScan stops the running/pending scan for a storage location, if any.
func (a *API) cancelScan(w http.ResponseWriter, r *http.Request) {
	storageLocationID, err := pathInt64(r, "storageLocationID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid storage location id")
		return
	}
	if status, msg := a.cancelScanForLocation(r.Context(), storageLocationID); status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// startLibraryScan scans one library's folders, one SCAN job per disk it
// spans.
func (a *API) startLibraryScan(w http.ResponseWriter, r *http.Request) {
	libraryID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid library id")
		return
	}
	jobIDs, status, msg := a.startLibraryJob(r, "SCAN", libraryID)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobIds": jobIDs})
}

// cancelLibraryScan stops every queued or running scan of a library.
func (a *API) cancelLibraryScan(w http.ResponseWriter, r *http.Request) {
	libraryID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid library id")
		return
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT id FROM background_job
		WHERE kind = 'SCAN' AND status IN ('PENDING', 'RUNNING') AND payload->>'libraryId' = $1::text
	`, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up the running scan")
		return
	}
	jobIDs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up the running scan")
		return
	}
	if len(jobIDs) == 0 {
		writeError(w, http.StatusNotFound, "no scan is running for this library")
		return
	}
	for _, id := range jobIDs {
		live, err := a.Jobs.Cancel(r.Context(), id)
		if err != nil {
			continue // finished on its own meanwhile
		}
		// No goroutine is coming to close out an orphaned job's scan row
		// (see cancelScanForLocation).
		if !live {
			a.DB.Exec(r.Context(), `UPDATE scan_job SET completed_at = now() WHERE background_job_id = $1 AND completed_at IS NULL`, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type backgroundJob struct {
	ID         int64   `json:"id"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Error      *string `json:"error,omitempty"`
	Attempts   int32   `json:"attempts"`
	CreatedAt  string  `json:"createdAt"`
	StartedAt  *string `json:"startedAt,omitempty"`
	FinishedAt *string `json:"finishedAt,omitempty"`
}

type adminOverviewPayload struct {
	TotalMediaItems     int64 `json:"totalMediaItems"`
	StorageLocations    int64 `json:"storageLocations"`
	OfflineStorage      int64 `json:"offlineStorageLocations"`
	DuplicateGroups     int64 `json:"duplicateGroups"`
	PendingReview       int64 `json:"filesMissingMetadata"`
	FailedBackgroundJob int64 `json:"failedBackgroundJobs"`
	RunningScans        int64 `json:"runningScans"`
	ActiveJobs          int64 `json:"activeJobs"`
}

func (a *API) adminOverview(w http.ResponseWriter, r *http.Request) {
	var p adminOverviewPayload
	ctx := r.Context()

	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM media_item`).Scan(&p.TotalMediaItems); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM storage_location`).Scan(&p.StorageLocations); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM storage_location WHERE NOT online`).Scan(&p.OfflineStorage); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM duplicate_group WHERE NOT dismissed`).Scan(&p.DuplicateGroups); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM media_item mi WHERE mi.metadata_source = 'FILENAME' AND mi.match_status <> 'rejected' AND EXISTS (SELECT 1 FROM media_file mf WHERE mf.media_item_id = mi.id AND mf.available)`).Scan(&p.PendingReview); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM background_job WHERE status = 'FAILED' AND finished_at > now() - interval '7 days' AND kind <> ALL($1)`, hiddenJobKinds).Scan(&p.FailedBackgroundJob); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM scan_job WHERE completed_at IS NULL`).Scan(&p.RunningScans); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}

	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM background_job WHERE status IN ('PENDING','RUNNING') AND kind <> ALL($1)`, hiddenJobKinds).Scan(&p.ActiveJobs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load overview")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

type analyticsDayCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type analyticsNameCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type analyticsStatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type adminAnalyticsPayload struct {
	TotalMediaItems      int64                  `json:"totalMediaItems"`
	TotalPerformers      int64                  `json:"totalPerformers"`
	TotalStudios         int64                  `json:"totalStudios"`
	TotalSeries          int64                  `json:"totalSeries"`
	TotalDurationSeconds float64                `json:"totalDurationSeconds"`
	TotalSizeBytes       int64                  `json:"totalSizeBytes"`
	ItemsAddedByDay      []analyticsDayCount    `json:"itemsAddedByDay"`
	TopStudios           []analyticsNameCount   `json:"topStudios"`
	TopCategories        []analyticsNameCount   `json:"topCategories"`
	JobsByStatus         []analyticsStatusCount `json:"jobsByStatus"`
}

func (a *API) adminAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var p adminAnalyticsPayload

	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM media_item`).Scan(&p.TotalMediaItems); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM performer`).Scan(&p.TotalPerformers); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM studio`).Scan(&p.TotalStudios); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM series`).Scan(&p.TotalSeries); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE(SUM(duration_seconds), 0) FROM media_item`).Scan(&p.TotalDurationSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE(SUM(file_size), 0) FROM media_file WHERE available`).Scan(&p.TotalSizeBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}

	dayRows, err := a.DB.Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COUNT(mi.id)
		FROM generate_series(CURRENT_DATE - INTERVAL '13 days', CURRENT_DATE, INTERVAL '1 day') AS d(day)
		LEFT JOIN media_item mi ON mi.date_added::date = d.day
		GROUP BY d.day ORDER BY d.day
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	p.ItemsAddedByDay = []analyticsDayCount{}
	for dayRows.Next() {
		var dc analyticsDayCount
		if err := dayRows.Scan(&dc.Date, &dc.Count); err != nil {
			dayRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to load analytics")
			return
		}
		p.ItemsAddedByDay = append(p.ItemsAddedByDay, dc)
	}
	dayRows.Close()

	studioRows, err := a.DB.Query(ctx, `
		SELECT s.name, COUNT(mi.id) AS c FROM studio s
		JOIN media_item mi ON mi.studio_id = s.id
		GROUP BY s.name ORDER BY c DESC LIMIT 6
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	p.TopStudios = []analyticsNameCount{}
	for studioRows.Next() {
		var nc analyticsNameCount
		if err := studioRows.Scan(&nc.Name, &nc.Count); err != nil {
			studioRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to load analytics")
			return
		}
		p.TopStudios = append(p.TopStudios, nc)
	}
	studioRows.Close()

	categoryRows, err := a.DB.Query(ctx, `
		SELECT c.name, COUNT(mic.media_item_id) AS n FROM category c
		JOIN media_item_category mic ON mic.category_id = c.id
		GROUP BY c.name ORDER BY n DESC LIMIT 6
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	p.TopCategories = []analyticsNameCount{}
	for categoryRows.Next() {
		var nc analyticsNameCount
		if err := categoryRows.Scan(&nc.Name, &nc.Count); err != nil {
			categoryRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to load analytics")
			return
		}
		p.TopCategories = append(p.TopCategories, nc)
	}
	categoryRows.Close()

	statusRows, err := a.DB.Query(ctx, `SELECT status, COUNT(*) FROM background_job GROUP BY status`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	p.JobsByStatus = []analyticsStatusCount{}
	for statusRows.Next() {
		var sc analyticsStatusCount
		if err := statusRows.Scan(&sc.Status, &sc.Count); err != nil {
			statusRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to load analytics")
			return
		}
		p.JobsByStatus = append(p.JobsByStatus, sc)
	}
	statusRows.Close()

	writeJSON(w, http.StatusOK, p)
}

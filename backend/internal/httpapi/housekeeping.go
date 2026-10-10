package httpapi

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"

	"reelstash/backend/internal/housekeeping"
	"reelstash/backend/internal/schedule"
)

// latestHousekeeping is the newest HOUSEKEEPING job matching where, or nil.
func (a *API) latestHousekeeping(ctx context.Context, where string) (*jobView, error) {
	j, err := scanJobView(a.DB.QueryRow(ctx, jobSelect+` WHERE bj.kind = 'HOUSEKEEPING' AND `+where+` ORDER BY bj.id DESC LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// getHousekeeping returns what the Housekeeping page shows: the tasks, the
// retention settings, the running job, the last analysis and last clean-up,
// and how much space each data folder takes.
func (a *API) getHousekeeping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	active, err := a.latestHousekeeping(ctx, `bj.status IN ('PENDING','RUNNING')`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load housekeeping")
		return
	}
	analysis, err := a.latestHousekeeping(ctx, `bj.status = 'COMPLETED' AND (bj.payload->>'dryRun')::boolean IS TRUE`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load housekeeping")
		return
	}
	cleanup, err := a.latestHousekeeping(ctx, `bj.status IN ('COMPLETED','FAILED','CANCELLED') AND (bj.payload->>'dryRun')::boolean IS NOT TRUE`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load housekeeping")
		return
	}
	// Folder sizes are cached; a clean-up since the last measurement makes them stale.
	var cleaned *time.Time
	_ = a.DB.QueryRow(ctx, `SELECT max(finished_at) FROM background_job WHERE kind = 'HOUSEKEEPING' AND (payload->>'dryRun')::boolean IS NOT TRUE`).Scan(&cleaned)
	if cleaned != nil {
		cacheMu.Lock()
		if !cacheSizing && cacheSizedAt.Before(*cleaned) {
			cacheSizedAt = time.Time{}
		}
		cacheMu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":       housekeeping.Tasks(),
		"settings":    housekeeping.LoadSettings(ctx, a.DB),
		"defaults":    housekeeping.DefaultSettings,
		"active":      active,
		"lastAnalyze": analysis,
		"lastCleanup": cleanup,
		"storage":     a.cacheSizes(),
	})
}

func (a *API) updateHousekeepingSettings(w http.ResponseWriter, r *http.Request) {
	var s housekeeping.Settings
	if err := decodeJSON(r, &s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := housekeeping.SaveSettings(r.Context(), a.DB, s); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save housekeeping settings")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// runHousekeeping queues an analysis (dryRun) or a clean-up of the given
// tasks; no tasks means the default set.
func (a *API) runHousekeeping(w http.ResponseWriter, r *http.Request) {
	var req housekeeping.Payload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := housekeeping.CheckTasks(req.Tasks); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	id, err := schedule.EnqueueHousekeeping(r.Context(), a.DB, a.Jobs, req.Tasks, req.DryRun)
	if errors.Is(err, schedule.ErrJobRunning) {
		writeError(w, http.StatusConflict, "housekeeping is already running")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start housekeeping")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": id})
}

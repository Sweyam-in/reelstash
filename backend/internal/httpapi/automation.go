package httpapi

import (
	"net/http"
	"strings"
	"time"

	"reelstash/backend/internal/schedule"
)

type scheduleView struct {
	ID          int64      `json:"id"`
	JobKind     string     `json:"jobKind"`
	LibraryID   *int64     `json:"libraryId"`
	LibraryName *string    `json:"libraryName,omitempty"`
	Cron        string     `json:"cron"`
	Enabled     bool       `json:"enabled"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt   *time.Time `json:"nextRunAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type watchView struct {
	LibraryID int64         `json:"id"`
	Name      string        `json:"name"`
	Folders   int           `json:"folders"`
	Enabled   bool          `json:"watchEnabled"`
	Watch     *watchDetails `json:"watch,omitempty"`
}

type watchDetails struct {
	Watching    bool       `json:"watching"`
	Directories int        `json:"directories"`
	LastEventAt *time.Time `json:"lastEventAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// getAutomation returns everything the Automation page shows: the recurring
// schedules and the file-watching state of every library.
func (a *API) getAutomation(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT s.id, s.job_kind, s.library_id, l.name, s.cron_expr, s.enabled, s.last_run_at
		FROM scan_schedule s LEFT JOIN library l ON l.id = s.library_id
		ORDER BY s.id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load schedules")
		return
	}
	defer rows.Close()
	schedules := []scheduleView{}
	now := time.Now()
	for rows.Next() {
		var s scheduleView
		if err := rows.Scan(&s.ID, &s.JobKind, &s.LibraryID, &s.LibraryName, &s.Cron, &s.Enabled, &s.LastRunAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read schedule")
			return
		}
		if c, err := schedule.ParseCron(s.Cron); err != nil {
			s.Error = err.Error()
		} else if next := c.Next(now); !next.IsZero() && s.Enabled {
			s.NextRunAt = &next
		}
		schedules = append(schedules, s)
	}
	rows.Close()

	var statuses map[int64]schedule.Status
	if a.Watcher != nil {
		statuses = a.Watcher.Statuses()
	}
	libRows, err := a.DB.Query(r.Context(), `
		SELECT l.id, l.name, (SELECT COUNT(*) FROM library_root lr WHERE lr.library_id = l.id), l.watch_enabled
		FROM library l ORDER BY l.position, lower(l.name)`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load libraries")
		return
	}
	defer libRows.Close()
	libraries := []watchView{}
	for libRows.Next() {
		var l watchView
		if err := libRows.Scan(&l.LibraryID, &l.Name, &l.Folders, &l.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read library")
			return
		}
		if st, ok := statuses[l.LibraryID]; ok && l.Enabled {
			l.Watch = &watchDetails{Watching: st.Watching, Directories: st.Directories, LastEventAt: st.LastEventAt, Error: st.LastError}
		}
		libraries = append(libraries, l)
	}
	zone, _ := now.Zone()
	writeJSON(w, http.StatusOK, map[string]any{
		"schedules": schedules, "libraries": libraries, "timezone": zone, "watcherRunning": a.Watcher != nil,
	})
}

type scheduleRequest struct {
	JobKind   *string `json:"jobKind"`
	LibraryID *int64  `json:"libraryId"`
	Cron      *string `json:"cron"`
	Enabled   *bool   `json:"enabled"`
	// AllLibraries clears the library (scan every library).
	AllLibraries bool `json:"allLibraries"`
}

func (a *API) validateSchedule(w http.ResponseWriter, r *http.Request, req scheduleRequest) bool {
	if req.Cron != nil {
		if _, err := schedule.ParseCron(*req.Cron); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return false
		}
	}
	if req.JobKind != nil && *req.JobKind != "SCAN" && *req.JobKind != "DUPLICATE_SCAN" && *req.JobKind != "HOUSEKEEPING" {
		writeError(w, http.StatusBadRequest, "jobKind must be SCAN, DUPLICATE_SCAN or HOUSEKEEPING")
		return false
	}
	if req.LibraryID != nil {
		var ok bool
		if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM library WHERE id = $1)`, *req.LibraryID).Scan(&ok); err != nil || !ok {
			writeError(w, http.StatusBadRequest, "unknown library")
			return false
		}
	}
	return true
}

func (a *API) createSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := decodeJSON(r, &req); err != nil || req.Cron == nil {
		writeError(w, http.StatusBadRequest, "cron is required")
		return
	}
	kind := "SCAN"
	if req.JobKind != nil {
		kind = *req.JobKind
	}
	if !a.validateSchedule(w, r, req) {
		return
	}
	if kind == "DUPLICATE_SCAN" || kind == "HOUSEKEEPING" {
		req.LibraryID = nil // it always covers everything
	}
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO scan_schedule (job_kind, library_id, cron_expr, enabled) VALUES ($1, $2, $3, COALESCE($4, TRUE)) RETURNING id
	`, kind, req.LibraryID, strings.TrimSpace(*req.Cron), req.Enabled).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create schedule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}
	var req scheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !a.validateSchedule(w, r, req) {
		return
	}
	var cron *string
	if req.Cron != nil {
		trimmed := strings.TrimSpace(*req.Cron)
		cron = &trimmed
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE scan_schedule SET
			cron_expr = COALESCE($2, cron_expr),
			enabled = COALESCE($3, enabled),
			library_id = CASE WHEN $5::boolean OR job_kind <> 'SCAN' THEN NULL WHEN $4::bigint IS NOT NULL THEN $4 ELSE library_id END
		WHERE id = $1
	`, id, cron, req.Enabled, req.LibraryID, req.AllLibraries)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update schedule")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM scan_schedule WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete schedule")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// setLibraryWatch switches file watching on or off for a library. The
// watcher notices the change within a few seconds.
func (a *API) setLibraryWatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid library id")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `UPDATE library SET watch_enabled = $2, updated_at = now() WHERE id = $1`, id, *req.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update library")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

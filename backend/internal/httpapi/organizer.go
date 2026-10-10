package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"reelstash/backend/internal/organizer"
	"reelstash/backend/internal/rename"
)

func (a *API) organizerOptions() organizer.Options {
	return organizer.Options{FallbackTemplate: a.Cfg.RenameTemplate, PosterDir: a.Cfg.PosterDir, ThumbnailDir: a.Cfg.ThumbnailDir}
}

// organizerLibrary reads the {libraryID} URL parameter and checks the
// library exists; it writes the error response itself.
func (a *API) organizerLibrary(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathInt64(r, "libraryID")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid library id")
		return 0, false
	}
	var exists bool
	if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM library WHERE id = $1)`, id).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load library")
		return 0, false
	}
	if !exists {
		writeError(w, http.StatusNotFound, "library not found")
		return 0, false
	}
	return id, true
}

func (a *API) listOrganizerRules(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	rules, err := organizer.ListRules(r.Context(), a.DB, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load organizer rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

type organizerRuleRequest struct {
	Kind       string `json:"kind"`
	EntityID   int64  `json:"entityId"`
	FolderName string `json:"folderName"`
	Enabled    *bool  `json:"enabled"`
}

func (a *API) createOrganizerRule(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	var req organizerRuleRequest
	if err := decodeJSON(r, &req); err != nil || req.EntityID < 1 || (req.Kind != organizer.KindStudio && req.Kind != organizer.KindPerformer) {
		writeError(w, http.StatusBadRequest, "kind (studio or performer) and entityId are required")
		return
	}
	id, err := organizer.CreateRule(r.Context(), a.DB, libraryID, req.Kind, req.EntityID, req.FolderName)
	if err != nil {
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == "23505":
			writeError(w, http.StatusConflict, "a rule for this "+req.Kind+" already exists")
		case errors.As(err, &pgErr) && pgErr.Code == "23503":
			writeError(w, http.StatusNotFound, req.Kind+" not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to create rule")
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) updateOrganizerRule(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}
	var req organizerRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	if err := organizer.UpdateRule(r.Context(), a.DB, libraryID, id, req.FolderName, enabled); err != nil {
		if errors.Is(err, organizer.ErrRuleNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) deleteOrganizerRule(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}
	if err := organizer.DeleteRule(r.Context(), a.DB, libraryID, id); err != nil {
		if errors.Is(err, organizer.ErrRuleNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) reorderOrganizerRules(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ids are required")
		return
	}
	if err := organizer.ReorderRules(r.Context(), a.DB, libraryID, req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type organizerSettingsBody struct {
	organizer.Settings
	Template        string `json:"template"`
	DefaultTemplate string `json:"defaultTemplate,omitempty"`
}

func (a *API) getOrganizerSettings(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, organizerSettingsBody{
		Settings:        organizer.LoadSettings(r.Context(), a.DB, libraryID),
		Template:        rename.Template(r.Context(), a.DB, libraryID, a.Cfg.RenameTemplate),
		DefaultTemplate: a.Cfg.RenameTemplate,
	})
}

// updateOrganizerSettings saves the layout and the naming template. The
// template is the library's own, shared with its Rename Pipeline and namer.
func (a *API) updateOrganizerSettings(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	var req organizerSettingsBody
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Settings.Normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Template = strings.TrimSpace(req.Template)
	if req.Template != "" && !strings.Contains(req.Template, "{title}") {
		writeError(w, http.StatusBadRequest, "the naming template must include {title}")
		return
	}
	if strings.ContainsAny(req.Template, `/\`) {
		writeError(w, http.StatusBadRequest, "the naming template can't contain folder separators")
		return
	}
	ctx := r.Context()
	if err := organizer.SaveSettings(ctx, a.DB, libraryID, req.Settings); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save organizer settings")
		return
	}
	var err error
	if req.Template == "" || req.Template == rename.Template(ctx, a.DB, 0, a.Cfg.RenameTemplate) {
		_, err = a.DB.Exec(ctx, `DELETE FROM app_setting WHERE key = $1`, rename.TemplateKey(libraryID))
	} else {
		_, err = a.DB.Exec(ctx, `
			INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		`, rename.TemplateKey(libraryID), req.Template)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save naming template")
		return
	}
	a.getOrganizerSettings(w, r)
}

// libraryDisk is one disk a library has folders on.
type libraryDisk struct {
	id   int64
	name string
}

// libraryDisks lists the disks the library has folders on.
func (a *API) libraryDisks(ctx context.Context, libraryID int64) ([]libraryDisk, error) {
	rows, err := a.DB.Query(ctx, `
		SELECT DISTINCT sl.id, sl.name FROM library_root lr
		JOIN storage_location sl ON sl.id = lr.storage_location_id
		WHERE lr.library_id = $1 ORDER BY sl.name, sl.id`, libraryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (libraryDisk, error) {
		var d libraryDisk
		err := row.Scan(&d.id, &d.name)
		return d, err
	})
}

// libraryPlan merges the organizer plans of every disk the library is on.
func (a *API) libraryPlan(ctx context.Context, libraryID int64) (organizer.Plan, error) {
	disks, err := a.libraryDisks(ctx, libraryID)
	if err != nil {
		return organizer.Plan{}, err
	}
	merged := organizer.Plan{Settings: organizer.LoadSettings(ctx, a.DB, libraryID), Buckets: []organizer.Bucket{}, Moves: []organizer.Move{}}
	merged.Template = rename.Template(ctx, a.DB, libraryID, a.Cfg.RenameTemplate)
	buckets := map[string]int{} // bucket key -> index in merged.Buckets
	for _, d := range disks {
		plan, err := organizer.BuildPlan(ctx, a.DB, libraryID, d.id, a.organizerOptions())
		if err != nil {
			return merged, err
		}
		merged.Total += plan.Total
		merged.ToMove += plan.ToMove
		merged.Unchanged += plan.Unchanged
		merged.Held += plan.Held
		for _, b := range plan.Buckets {
			key := fmt.Sprintf("%s|%d|%s|%s", b.Kind, b.RuleID, b.Folder, b.Name)
			if i, ok := buckets[key]; ok {
				merged.Buckets[i].Count += b.Count
				continue
			}
			buckets[key] = len(merged.Buckets)
			merged.Buckets = append(merged.Buckets, b)
		}
		for _, m := range plan.Moves {
			m.StorageLocationID, m.StorageLocation = d.id, d.name
			merged.Moves = append(merged.Moves, m)
		}
	}
	return merged, nil
}

// previewOrganizer returns the plan for every disk of a library without
// moving anything: per-folder counts and the videos that would move.
func (a *API) previewOrganizer(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	plan, err := a.libraryPlan(r.Context(), libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build organizer preview")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	ruleFilter := r.URL.Query().Get("rule") // a rule id, "uncategorized", or empty for all
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	moves := []organizer.Move{}
	matching := 0
	for _, m := range plan.Moves {
		if m.Unchanged {
			continue
		}
		if ruleFilter == "uncategorized" && m.RuleID != 0 || ruleFilter != "" && ruleFilter != "uncategorized" && strconv.FormatInt(m.RuleID, 10) != ruleFilter {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(m.From+" "+m.To+" "+m.Title), q) {
			continue
		}
		matching++
		if len(moves) < limit {
			moves = append(moves, m)
		}
	}
	plan.Moves = moves
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "matching": matching})
}

// runOrganizer enqueues one async ORGANIZE background_job per disk of the
// library — it never walks/moves files inline on the request.
func (a *API) runOrganizer(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	jobIDs, status, msg := a.organizeLibrary(r, libraryID)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobIds": jobIDs})
}

// organizeLibrary queues an ORGANIZE job for every disk of the library.
// Nothing is queued unless every disk is free.
func (a *API) organizeLibrary(r *http.Request, libraryID int64) (jobIDs []int64, status int, msg string) {
	disks, err := a.libraryDisks(r.Context(), libraryID)
	if err != nil {
		return nil, http.StatusInternalServerError, "failed to load the library's disks"
	}
	if len(disks) == 0 {
		return nil, http.StatusBadRequest, "this library has no folders to organize"
	}
	for _, d := range disks {
		if msg := a.busyLocation(r, d.id); msg != "" {
			return nil, http.StatusConflict, d.name + ": " + msg
		}
	}
	for _, d := range disks {
		jobID, status, msg := a.startJobWith(r.Context(), "ORGANIZE", d.id, map[string]any{"libraryId": libraryID})
		if status != 0 {
			return jobIDs, status, d.name + ": " + msg
		}
		jobIDs = append(jobIDs, jobID)
	}
	return jobIDs, 0, ""
}

// busyLocation explains why a storage location can't be cleaned or organized
// right now, or returns "" when it is idle.
func (a *API) busyLocation(r *http.Request, storageLocationID int64) string {
	if organizing(r.Context(), a, storageLocationID) {
		return "the organizer is running on this storage location; try again once it finishes"
	}
	var active bool
	_ = a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM scan_job WHERE storage_location_id = $1 AND completed_at IS NULL)`, storageLocationID).Scan(&active)
	if active {
		return "a scan is running on this storage location; try again once it finishes"
	}
	return ""
}

// previewCleanup lists the leftover folders (no videos, outside the
// categorized and uncategorized folders) on every disk of the library,
// without deleting anything.
func (a *API) previewCleanup(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	disks, err := a.libraryDisks(r.Context(), libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look for leftovers")
		return
	}
	merged := organizer.CleanupPlan{Items: []organizer.CleanupItem{}}
	for _, d := range disks {
		plan, _, err := organizer.BuildCleanup(r.Context(), a.DB, libraryID, d.id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to look for leftovers")
			return
		}
		for _, it := range plan.Items {
			it.StorageLocationID, it.StorageLocation = d.id, d.name
			merged.Items = append(merged.Items, it)
		}
		merged.Files += plan.Files
		merged.Dirs += plan.Dirs
		merged.Bytes += plan.Bytes
	}
	writeJSON(w, http.StatusOK, merged)
}

// runCleanup deletes the leftovers on every disk of the library.
func (a *API) runCleanup(w http.ResponseWriter, r *http.Request) {
	libraryID, ok := a.organizerLibrary(w, r)
	if !ok {
		return
	}
	disks, err := a.libraryDisks(r.Context(), libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cleanup failed")
		return
	}
	for _, d := range disks {
		if msg := a.busyLocation(r, d.id); msg != "" {
			writeError(w, http.StatusConflict, d.name+": "+msg)
			return
		}
	}
	total := organizer.CleanupResult{}
	for _, d := range disks {
		res, err := organizer.RunCleanup(r.Context(), a.DB, libraryID, d.id)
		total.Files += res.Files
		total.Dirs += res.Dirs
		total.Bytes += res.Bytes
		total.Errors = append(total.Errors, res.Errors...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cleanup failed on "+d.name)
			return
		}
	}
	writeJSON(w, http.StatusOK, total)
}

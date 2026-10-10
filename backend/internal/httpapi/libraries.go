package httpapi

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"reelstash/backend/internal/pathguard"
)

// Library modes. The mode decides the media app's words and structure
// (movies/shows/cast vs videos/performers/studios) and the providers a new
// library starts with.
const (
	libraryModeEntertainment = "entertainment"
	libraryModePrivate       = "private"
)

// libraryScope is which libraries a request may see. Accessible is every
// library the user was granted (nil with All for admins, who see everything,
// including titles outside every library). Selected is the library the media
// app is browsing, sent as the X-Library header (or ?library= for URLs a
// browser loads directly); listings are narrowed to it.
type libraryScope struct {
	All        bool
	Accessible []int64
	Selected   int64
	// Locked are granted libraries behind a library-scoped PIN lock that is
	// engaged; they are left out of Accessible until the user unlocks.
	Locked []int64
}

const libraryCtxKey ctxKey = "reelstash.library"

func scopeFromContext(ctx context.Context) libraryScope {
	s, _ := ctx.Value(libraryCtxKey).(libraryScope)
	return s
}

// withLibraryScope runs after requireAuth and resolves the request's scope once.
func (a *API) withLibraryScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := UserFromContext(r.Context())
		scope := libraryScope{All: user.Role == "ADMIN"}
		if !scope.All {
			rows, err := a.DB.Query(r.Context(), `SELECT library_id FROM library_access WHERE user_id = $1`, user.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load library access")
				return
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load library access")
				return
			}
			if ids == nil {
				ids = []int64{} // never nil: nil means "no filter" to visibleLibraryIDs
			}
			scope.Accessible = ids
		}
		raw := r.Header.Get("X-Library")
		if raw == "" {
			raw = r.URL.Query().Get("library")
		}
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			scope.Selected = id
		}
		if locked := lockedLibrariesFromContext(r.Context()); len(locked) > 0 {
			if scope.All {
				// Admins then see every library but the locked ones (and no
				// unassigned titles) until they unlock.
				rows, err := a.DB.Query(r.Context(), `SELECT id FROM library`)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "failed to load libraries")
					return
				}
				all, err := pgx.CollectRows(rows, pgx.RowTo[int64])
				if err != nil {
					writeError(w, http.StatusInternalServerError, "failed to load libraries")
					return
				}
				scope.All, scope.Accessible = false, all
			}
			open := []int64{}
			for _, id := range scope.Accessible {
				if slices.Contains(locked, id) {
					scope.Locked = append(scope.Locked, id)
				} else {
					open = append(open, id)
				}
			}
			scope.Accessible = open
			if slices.Contains(scope.Locked, scope.Selected) {
				writeError(w, http.StatusLocked, "library is locked")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), libraryCtxKey, scope)))
	})
}

// canAccess reports whether the scope may open something in library id (NULL:
// outside every library, admins only).
func (s libraryScope) canAccess(id *int64) bool {
	if s.All {
		return true
	}
	if id == nil {
		return false
	}
	for _, a := range s.Accessible {
		if a == *id {
			return true
		}
	}
	return false
}

// libraryFilter returns a SQL condition limiting col (a library_id column) to
// what a listing should show, or "" when nothing needs filtering. A selected
// library the user cannot access yields no rows.
func libraryFilter(r *http.Request, col string, arg func(any) string) string {
	s := scopeFromContext(r.Context())
	if s.Selected > 0 {
		if !s.canAccess(&s.Selected) {
			return "FALSE"
		}
		return col + " = " + arg(s.Selected)
	}
	if s.All {
		return ""
	}
	return col + " = ANY(" + arg(s.Accessible) + ")"
}

// libraryFiltered reports whether listings for this request are narrowed at
// all (a user, or an admin browsing one library).
func libraryFiltered(r *http.Request) bool {
	s := scopeFromContext(r.Context())
	return s.Selected > 0 || !s.All
}

// visibleLibraryIDs is the libraries a listing shows, or nil for no filter.
func visibleLibraryIDs(r *http.Request) []int64 {
	s := scopeFromContext(r.Context())
	switch {
	case s.Selected > 0 && s.canAccess(&s.Selected):
		return []int64{s.Selected}
	case s.Selected > 0:
		return []int64{}
	case s.All:
		return nil
	}
	return s.Accessible
}

// visibleMediaCond is libraryFilter for a media_item alias, always returning a
// usable condition ("TRUE" when unfiltered) so it can be spliced into EXISTS
// subqueries that count an entity's titles.
func visibleMediaCond(r *http.Request, alias string, arg func(any) string) string {
	if f := libraryFilter(r, alias+".library_id", arg); f != "" {
		return f
	}
	return "TRUE"
}

// accessibleMediaCond limits alias to titles the user may open in any of their
// libraries, ignoring the browsed one — for lists that span libraries, such
// as playlists and watch history.
func accessibleMediaCond(r *http.Request, alias string, arg func(any) string) string {
	s := scopeFromContext(r.Context())
	if s.All {
		return "TRUE"
	}
	return alias + ".library_id = ANY(" + arg(s.Accessible) + ")"
}

// requireMediaAccess guards every /media/{id}/... route: a title outside the
// user's libraries answers 404, exactly like one that does not exist.
func (a *API) requireMediaAccess(next http.Handler) http.Handler {
	return a.requireMediaParamAccess("id")(next)
}

// requireMediaParamAccess is requireMediaAccess for a title id in another URL
// parameter (e.g. {mediaItemID}).
func (a *API) requireMediaParamAccess(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return a.mediaAccessGuard(param, next)
	}
}

func (a *API) mediaAccessGuard(param string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := scopeFromContext(r.Context())
		if scope.All {
			next.ServeHTTP(w, r)
			return
		}
		id, err := pathInt64(r, param)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media item id")
			return
		}
		if !a.mediaAccessible(r, id) {
			// A title in a PIN-locked library asks for the PIN rather than
			// pretending not to exist.
			var lib *int64
			if len(scope.Locked) > 0 && a.DB.QueryRow(r.Context(), `SELECT library_id FROM media_item WHERE id = $1`, id).Scan(&lib) == nil &&
				lib != nil && slices.Contains(scope.Locked, *lib) {
				writeError(w, http.StatusLocked, "library is locked")
				return
			}
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) mediaAccessible(r *http.Request, id int64) bool {
	scope := scopeFromContext(r.Context())
	if scope.All {
		return true
	}
	var lib *int64
	if err := a.DB.QueryRow(r.Context(), `SELECT library_id FROM media_item WHERE id = $1`, id).Scan(&lib); err != nil {
		return false
	}
	return scope.canAccess(lib)
}

// entityAccessible reports whether a performer/studio/series/category has at
// least one title the user can see. Admins see every entity, empty or not.
func (a *API) entityAccessible(r *http.Request, existsSQL string, id int64) bool {
	scope := scopeFromContext(r.Context())
	if scope.All {
		return true
	}
	args := []any{id}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	var ok bool
	err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (`+existsSQL+` AND mi.library_id = ANY(`+arg(scope.Accessible)+`))`, args...).Scan(&ok)
	return err == nil && ok
}

// Subqueries for entityAccessible: $1 is the entity id, mi the title alias.
const (
	performerMediaExists = `SELECT 1 FROM (SELECT media_item_id, performer_id FROM media_item_performer UNION ALL SELECT media_item_id, performer_id FROM media_item_crew) mip
		JOIN media_item mi ON mi.id = mip.media_item_id WHERE mip.performer_id = $1`
	studioMediaExists = `SELECT 1 FROM media_item mi WHERE mi.studio_id = $1`
	seriesMediaExists = `SELECT 1 FROM media_item mi WHERE mi.series_id = $1`
)

// ---- User-facing ----

type libraryInfo struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Locked bool   `json:"locked"` // behind an engaged library PIN lock
}

// listMyLibraries is what the media app's library switcher shows.
func (a *API) listMyLibraries(w http.ResponseWriter, r *http.Request) {
	scope := scopeFromContext(r.Context())
	query := `SELECT id, name, mode, COALESCE(id = ANY($1), false) FROM library`
	args := []any{scope.Locked}
	if !scope.All {
		query += ` WHERE id = ANY($2)`
		args = append(args, append(append([]int64{}, scope.Accessible...), scope.Locked...))
	}
	rows, err := a.DB.Query(r.Context(), query+` ORDER BY position, lower(name)`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load libraries")
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (libraryInfo, error) {
		var l libraryInfo
		return l, row.Scan(&l.ID, &l.Name, &l.Mode, &l.Locked)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load libraries")
		return
	}
	user, _ := UserFromContext(r.Context())
	var defaultID *int64
	_ = a.DB.QueryRow(r.Context(), `SELECT default_library_id FROM app_user WHERE id = $1`, user.ID).Scan(&defaultID)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "defaultLibraryId": defaultID})
}

// setDefaultLibrary saves the library the media app opens on (null: the first).
func (a *API) setDefaultLibrary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LibraryID *int64 `json:"libraryId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	scope := scopeFromContext(r.Context())
	if req.LibraryID != nil && !scope.canAccess(req.LibraryID) && !slices.Contains(scope.Locked, *req.LibraryID) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	user, _ := UserFromContext(r.Context())
	if _, err := a.DB.Exec(r.Context(), `UPDATE app_user SET default_library_id = $2, updated_at = now() WHERE id = $1`, user.ID, req.LibraryID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save default library")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"defaultLibraryId": req.LibraryID})
}

// ---- Admin ----

type libraryRoot struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"`
}

type adminLibrary struct {
	ID            int64         `json:"id"`
	Name          string        `json:"name"`
	Mode          string        `json:"mode"`
	DefaultAccess bool          `json:"defaultAccess"`
	Roots         []libraryRoot `json:"roots"`
	Providers     []string      `json:"providers"`
	UserIDs       []int64       `json:"userIds"`
	MediaCount    int64         `json:"mediaCount"`
	TotalBytes    int64         `json:"totalBytes"`
	// LastScanAt is the newest finished scan that covered the library: a
	// scan of the library itself, or a whole-disk scan of every disk it is on.
	LastScanAt *string `json:"lastScanAt,omitempty"`
	Scanning   bool    `json:"scanning"`
}

const adminLibrarySelect = `
	SELECT l.id, l.name, l.mode, l.default_access,
	       COALESCE((SELECT json_agg(json_build_object('storageLocationId', lr.storage_location_id, 'path', lr.path) ORDER BY lr.storage_location_id, lr.path)
	                 FROM library_root lr WHERE lr.library_id = l.id), '[]'::json),
	       COALESCE((SELECT array_agg(lp.provider ORDER BY lp.position, lp.provider) FROM library_provider lp WHERE lp.library_id = l.id), '{}'),
	       COALESCE((SELECT array_agg(la.user_id ORDER BY la.user_id) FROM library_access la WHERE la.library_id = l.id), '{}'),
	       (SELECT COUNT(*) FROM media_item mi WHERE mi.library_id = l.id),
	       COALESCE((SELECT SUM(mf.file_size) FROM media_file mf WHERE mf.library_id = l.id), 0),
	       (SELECT max(sj.completed_at)::text FROM scan_job sj
	        WHERE sj.phase = 'done' AND (sj.library_id = l.id OR (sj.library_id IS NULL
	              AND sj.storage_location_id IN (SELECT storage_location_id FROM library_root WHERE library_id = l.id)))),
	       EXISTS (SELECT 1 FROM background_job bj WHERE bj.kind = 'SCAN' AND bj.status IN ('PENDING', 'RUNNING')
	               AND bj.payload->>'libraryId' = l.id::text)
	FROM library l`

func scanAdminLibrary(row pgx.Row) (adminLibrary, error) {
	var l adminLibrary
	err := row.Scan(&l.ID, &l.Name, &l.Mode, &l.DefaultAccess, &l.Roots, &l.Providers, &l.UserIDs, &l.MediaCount, &l.TotalBytes, &l.LastScanAt, &l.Scanning)
	return l, err
}

func (a *API) listLibraries(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), adminLibrarySelect+` ORDER BY l.position, lower(l.name)`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load libraries")
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (adminLibrary, error) { return scanAdminLibrary(row) })
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load libraries")
		return
	}
	var unassigned int64
	_ = a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM media_item WHERE library_id IS NULL`).Scan(&unassigned)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unassignedCount": unassigned})
}

// libraryRequest creates or replaces a library. Roots, providers and users are
// replaced wholesale when present (nil leaves them as they are on update).
type libraryRequest struct {
	Name          string         `json:"name"`
	Mode          string         `json:"mode"`
	DefaultAccess bool           `json:"defaultAccess"`
	Roots         *[]libraryRoot `json:"roots"`
	Providers     *[]string      `json:"providers"`
	UserIDs       *[]int64       `json:"userIds"`
}

// defaultLibraryProviders is what a new library matches with when the request
// does not say: TMDB. Provider plugins are added per library afterwards.
func defaultLibraryProviders(mode string) []string {
	return []string{"tmdb"}
}

func (a *API) createLibrary(w http.ResponseWriter, r *http.Request) {
	a.saveLibrary(w, r, 0)
}

func (a *API) updateLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid library id")
		return
	}
	a.saveLibrary(w, r, id)
}

func (a *API) saveLibrary(w http.ResponseWriter, r *http.Request, id int64) {
	var req libraryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Mode == "" {
		req.Mode = libraryModeEntertainment
	}
	if req.Mode != libraryModeEntertainment && req.Mode != libraryModePrivate {
		writeError(w, http.StatusBadRequest, `mode must be "entertainment" or "private"`)
		return
	}
	if req.Roots != nil {
		for i, root := range *req.Roots {
			(*req.Roots)[i].Path = strings.Trim(strings.TrimSpace(root.Path), "/")
			if strings.Contains(root.Path, "..") {
				writeError(w, http.StatusBadRequest, "folder paths cannot contain ..")
				return
			}
		}
		// A library folder can't be inside the downloads in-progress folder
		// (scans would skip all of it). Holding it is fine: scans skip just it.
		guard := pathguard.Load(r.Context(), a.DB)
		for _, root := range *req.Roots {
			_, abs, err := a.resolveStorageDir(r, root.StorageLocationID, root.Path)
			if err != nil {
				continue // reported by the insert below
			}
			if guard.Locked(abs) {
				writeError(w, http.StatusBadRequest, "the folder "+root.Path+" is inside the downloads in-progress folder; pick a folder outside it")
				return
			}
		}
	}
	if req.Providers != nil {
		for _, p := range *req.Providers {
			if reg := a.providerRegistry(); reg != nil {
				if _, ok := reg.Get(p); !ok {
					writeError(w, http.StatusBadRequest, "unknown provider "+p)
					return
				}
			}
		}
	}
	if id == 0 && req.Providers == nil {
		p := defaultLibraryProviders(req.Mode)
		req.Providers = &p
	}

	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save library")
		return
	}
	defer tx.Rollback(r.Context())

	if id == 0 {
		err = tx.QueryRow(r.Context(), `
			INSERT INTO library (name, mode, default_access, position)
			VALUES ($1, $2, $3, COALESCE((SELECT MAX(position) + 1 FROM library), 0))
			RETURNING id
		`, req.Name, req.Mode, req.DefaultAccess).Scan(&id)
	} else {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(r.Context(), `
			UPDATE library SET name = $2, mode = $3, default_access = $4, updated_at = now() WHERE id = $1
		`, id, req.Name, req.Mode, req.DefaultAccess)
		if err == nil && tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "library not found")
			return
		}
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a library with that name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save library")
		return
	}

	if req.Roots != nil {
		if _, err := tx.Exec(r.Context(), `DELETE FROM library_root WHERE library_id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save folders")
			return
		}
		for _, root := range *req.Roots {
			if _, err := tx.Exec(r.Context(), `INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $2, $3)`, id, root.StorageLocationID, root.Path); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					writeError(w, http.StatusConflict, "a folder can belong to only one library: "+displayRoot(root))
					return
				}
				writeError(w, http.StatusBadRequest, "invalid folder "+displayRoot(root))
				return
			}
		}
	}
	if req.Providers != nil {
		if _, err := tx.Exec(r.Context(), `DELETE FROM library_provider WHERE library_id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save providers")
			return
		}
		for i, p := range *req.Providers {
			if _, err := tx.Exec(r.Context(), `INSERT INTO library_provider (library_id, provider, position) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, p, i); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save providers")
				return
			}
		}
	}
	if req.UserIDs != nil {
		if _, err := tx.Exec(r.Context(), `DELETE FROM library_access WHERE library_id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save access")
			return
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO library_access (user_id, library_id) SELECT u.id, $1 FROM app_user u WHERE u.id = ANY($2)
		`, id, *req.UserIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save access")
			return
		}
	}
	if req.Roots != nil {
		if _, err := tx.Exec(r.Context(), `SELECT reassign_libraries()`); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign titles to libraries")
			return
		}
	}
	// Titles newly in an entertainment library become movies or episodes.
	if _, err := tx.Exec(r.Context(), `SELECT classify_titles()`); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to classify titles")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save library")
		return
	}
	lib, err := scanAdminLibrary(a.DB.QueryRow(r.Context(), adminLibrarySelect+` WHERE l.id = $1`, id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved but failed to load library")
		return
	}
	writeJSON(w, http.StatusOK, lib)
}

func displayRoot(root libraryRoot) string {
	if root.Path == "" {
		return "the whole disk"
	}
	return root.Path
}

// deleteLibrary removes the library only; its titles stay indexed and become
// unassigned (visible to admins) until another library covers their folders.
func (a *API) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid library id")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete library")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `DELETE FROM library WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete library")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if _, err := tx.Exec(r.Context(), `SELECT reassign_libraries()`); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reassign titles")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete library")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) reorderLibraries(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		UPDATE library l SET position = o.pos - 1
		FROM unnest($1::bigint[]) WITH ORDINALITY AS o(id, pos) WHERE l.id = o.id
	`, req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder libraries")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// setUserLibraries replaces the libraries one user can open.
func (a *API) setUserLibraries(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req struct {
		LibraryIDs []int64 `json:"libraryIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save access")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM library_access WHERE user_id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save access")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO library_access (user_id, library_id) SELECT $1, l.id FROM library l WHERE l.id = ANY($2)
	`, id, req.LibraryIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save access")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save access")
		return
	}
	noteAudit(r, "", map[string]any{"libraryIds": req.LibraryIDs})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "libraryIds": req.LibraryIDs})
}

// ---- File browser ----

// fileRoots returns the library folders on a disk a non-admin may browse
// (relative paths, "" for the whole disk); all is true for admins.
func (a *API) fileRoots(r *http.Request, storageLocationID int64) (roots []string, all bool) {
	scope := scopeFromContext(r.Context())
	if scope.All {
		return nil, true
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT path FROM library_root WHERE storage_location_id = $1 AND library_id = ANY($2)
	`, storageLocationID, scope.Accessible)
	if err != nil {
		return nil, false
	}
	roots, _ = pgx.CollectRows(rows, pgx.RowTo[string])
	return roots, false
}

// fileAccess reports how a non-admin may see rel (clean, no leading slash):
// inside a library folder (full access), an ancestor of one (only the
// directories leading to library folders, returned in via), or not at all.
func fileAccess(roots []string, rel string) (inside bool, via map[string]bool) {
	via = map[string]bool{}
	for _, root := range roots {
		if root == "" || rel == root || strings.HasPrefix(rel, root+"/") {
			return true, nil
		}
		if rel == "" {
			via[strings.SplitN(root, "/", 2)[0]] = true
		} else if strings.HasPrefix(root, rel+"/") {
			via[strings.SplitN(strings.TrimPrefix(root, rel+"/"), "/", 2)[0]] = true
		}
	}
	return false, via
}

func cleanRelPath(p string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean("/"+p)), "/")
}

// fileAllowed is fileAccess for a single file or folder that must lie inside a
// library folder (downloads, NFOs).
func (a *API) fileAllowed(r *http.Request, storageLocationID int64, rel string) bool {
	roots, all := a.fileRoots(r, storageLocationID)
	if all {
		return true
	}
	inside, _ := fileAccess(roots, cleanRelPath(rel))
	return inside
}

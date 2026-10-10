package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type performer struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	DisplayName    *string         `json:"displayName,omitempty"`
	ProfileImageID *int64          `json:"profileImageId,omitempty"`
	Notes          *string         `json:"notes,omitempty"`
	Favorite       bool            `json:"favorite"`
	Aliases        []string        `json:"aliases,omitempty"`
	MediaCount     int64           `json:"mediaCount"`
	MatchStatus    string          `json:"matchStatus,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	Links          json.RawMessage `json:"links,omitempty"`
}

func (a *API) listPerformers(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	var conds []string
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	// role=director|writer lists crew (counting the titles they worked on)
	// instead of cast.
	credits, roleCond := `media_item_performer`, ``
	if role := r.URL.Query().Get("role"); role == "director" || role == "writer" {
		// Whitelisted, so inlined: the COUNT query must not carry unused bind params.
		credits, roleCond = `media_item_crew`, ` AND mip.job = `+sqlLiteral(role)
		conds = append(conds, `EXISTS (SELECT 1 FROM media_item_crew c WHERE c.performer_id = p.id AND c.job = `+sqlLiteral(role)+`)`)
	}
	// Users see performers with a title in their libraries, counted there.
	mediaJoin := `LEFT JOIN ` + credits + ` mip ON mip.performer_id = p.id` + roleCond
	if f := libraryFilter(r, "mi.library_id", arg); f != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM `+credits+` vis JOIN media_item mi ON mi.id = vis.media_item_id WHERE vis.performer_id = p.id AND `+f+`)`)
		mediaJoin = `LEFT JOIN (` + credits + ` mip JOIN media_item mi ON mi.id = mip.media_item_id AND ` + f + `) ON mip.performer_id = p.id` + roleCond
	}
	// Default: most titles first. Searching overrides this with relevance.
	order := `media_count DESC, p.name`
	switch r.URL.Query().Get("sort") {
	case "name":
		order = `p.name`
	case "name_desc":
		order = `p.name DESC`
	}
	if search != "" {
		hay := `(p.name || ' ' || COALESCE(p.display_name, '') || ' ' || COALESCE((SELECT string_agg(pa.alias, ' ') FROM performer_alias pa WHERE pa.performer_id = p.id), ''))`
		conds = append(conds, fuzzyMatch(hay, fuzzyTokens(search), arg))
		// Rank: name starts with the query, then word similarity, then alphabetical.
		// Inlined as quoted literals so the COUNT query doesn't carry unused bind params.
		lower := strings.ToLower(search)
		order = `(LOWER(p.name) LIKE ` + sqlLiteral(likeContains(lower)[1:]) + `) DESC, word_similarity(` + sqlLiteral(lower) + `, ` + hay + `) DESC, p.name`
	}
	user, _ := UserFromContext(r.Context())
	if r.URL.Query().Get("favorite") == "true" {
		conds = append(conds, performerFavorite("p", user.ID))
	}
	if letter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("letter"))); letter == "#" {
		conds = append(conds, `UPPER(LEFT(p.name, 1)) !~ '[A-Z]'`)
	} else if len(letter) == 1 && letter[0] >= 'A' && letter[0] <= 'Z' {
		args = append(args, letter)
		conds = append(conds, `UPPER(LEFT(p.name, 1)) = $`+strconv.Itoa(len(args)))
	}
	if status := r.URL.Query().Get("matchStatus"); status != "" {
		args = append(args, status)
		conds = append(conds, `p.match_status = $`+strconv.Itoa(len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM performer p `+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count performers")
		return
	}
	args = append(args, limit, offset)
	limitArg := len(args) - 1
	rows, err := a.DB.Query(r.Context(), `
		SELECT p.id, p.name, p.display_name, p.profile_image_id, p.notes, `+performerFavorite("p", user.ID)+`, p.match_status,
		       COUNT(mip.media_item_id) AS media_count
		FROM performer p
		`+mediaJoin+`
		`+where+`
		GROUP BY p.id
		ORDER BY `+order+`
		LIMIT $`+strconv.Itoa(limitArg)+` OFFSET $`+strconv.Itoa(limitArg+1)+`
	`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performers")
		return
	}
	defer rows.Close()

	items := []performer{}
	for rows.Next() {
		var p performer
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProfileImageID, &p.Notes, &p.Favorite, &p.MatchStatus, &p.MediaCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read performer")
			return
		}
		items = append(items, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset, "total": total})
}

type createPerformerRequest struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Notes       string   `json:"notes"`
	Aliases     []string `json:"aliases"`
}

func (a *API) createPerformer(w http.ResponseWriter, r *http.Request) {
	var req createPerformerRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)

	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO performer (name, display_name, notes) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
		RETURNING id
	`, req.Name, req.DisplayName, req.Notes).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a performer with that name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create performer")
		return
	}
	for _, alias := range req.Aliases {
		if alias == "" {
			continue
		}
		a.DB.Exec(r.Context(), `INSERT INTO performer_alias (performer_id, alias) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, alias)
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) getPerformer(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}

	if !a.entityAccessible(r, performerMediaExists, id) {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	var p performer
	var metadata []byte
	args := []any{id}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	row := a.DB.QueryRow(r.Context(), `
		SELECT id, name, display_name, profile_image_id, notes, `+performerFavorite("performer", currentUserID(r))+`, match_status, remote_metadata,
		       (SELECT COUNT(DISTINCT mi.id) FROM (SELECT media_item_id, performer_id FROM media_item_performer UNION ALL SELECT media_item_id, performer_id FROM media_item_crew) mip
		        JOIN media_item mi ON mi.id = mip.media_item_id
		        WHERE mip.performer_id = performer.id AND `+visibleMediaCond(r, "mi", arg)+`)
		FROM performer WHERE id = $1
	`, args...)
	if err := row.Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProfileImageID, &p.Notes, &p.Favorite, &p.MatchStatus, &metadata, &p.MediaCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load performer")
		return
	}
	if len(metadata) > 0 && string(metadata) != "null" {
		p.Metadata = metadata
	}
	p.Links = a.loadLinks(r.Context(), "performer", id)

	aliasRows, err := a.DB.Query(r.Context(), `SELECT alias FROM performer_alias WHERE performer_id = $1 ORDER BY alias`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load aliases")
		return
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var alias string
		if err := aliasRows.Scan(&alias); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read alias")
			return
		}
		p.Aliases = append(p.Aliases, alias)
	}

	writeJSON(w, http.StatusOK, p)
}

type updatePerformerRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Notes       string `json:"notes"`
}

func (a *API) updatePerformer(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	var req updatePerformerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var oldName string
	if err := a.DB.QueryRow(r.Context(), `SELECT name FROM performer WHERE id = $1`, id).Scan(&oldName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load performer")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE performer SET name = $2, display_name = NULLIF($3, ''), notes = NULLIF($4, ''), updated_at = now()
		WHERE id = $1
	`, id, req.Name, req.DisplayName, req.Notes)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "another performer already has that name. Merge them instead.")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update performer")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	resp := map[string]any{"id": id}
	if oldName != req.Name {
		// Keep the NFO sidecars in step, otherwise the next scan would bring the old name back.
		a.DB.Exec(r.Context(), `INSERT INTO performer_alias (performer_id, alias) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, oldName)
		var itemIDs []int64
		if rows, err := a.DB.Query(r.Context(), `SELECT media_item_id FROM media_item_performer WHERE performer_id = $1`, id); err == nil {
			for rows.Next() {
				var itemID int64
				if rows.Scan(&itemID) == nil {
					itemIDs = append(itemIDs, itemID)
				}
			}
			rows.Close()
		}
		updated, failed := a.rewritePerformerNFOs(r.Context(), itemIDs, []string{oldName}, req.Name)
		resp["nfoUpdated"] = updated
		resp["nfoFailed"] = failed
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) setPerformerFavorite(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "favorite is required")
		return
	}
	if !a.entityAccessible(r, performerMediaExists, id) {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	query := `DELETE FROM performer_favorite WHERE user_id = $1 AND performer_id = $2`
	if req.Favorite {
		query = `INSERT INTO performer_favorite (user_id, performer_id) SELECT $1, id FROM performer WHERE id = $2 ON CONFLICT DO NOTHING`
	}
	if _, err := a.DB.Exec(r.Context(), query, currentUserID(r), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update favorite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "favorite": req.Favorite})
}

const maxPerformerUploadBytes = 8 << 20

func (a *API) uploadPerformerImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPerformerUploadBytes+(1<<20))
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "choose an image under 8 MB")
		return
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxPerformerUploadBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the image")
		return
	}
	if _, err := a.Namer.AddPerformerPhotoUpload(r.Context(), id, body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var imageID int64
	_ = a.DB.QueryRow(r.Context(), `SELECT profile_image_id FROM performer WHERE id = $1`, id).Scan(&imageID)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "profileImageId": imageID})
}

func (a *API) deletePerformerImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	if err := a.clearPerformerImage(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to remove photo")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearPerformerImage deletes the displayed photo; the next gallery photo, if
// any, takes its place.
func (a *API) clearPerformerImage(ctx context.Context, id int64) error {
	var exists bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM performer WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	} else if !exists {
		return pgx.ErrNoRows
	}
	_, err := a.removePerformerPhotos(ctx, id, `pp.artwork_id = (SELECT profile_image_id FROM performer WHERE id = $1)`)
	return err
}

func (a *API) deletePerformer(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM performer WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete performer")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sqlLiteral quotes s as a standard SQL string literal.
func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// relatedPerformer is a suggested performer plus how many titles they share with the one being viewed.
type relatedPerformer struct {
	performer
	SharedTitles int64 `json:"sharedTitles"`
}

// relatedPerformers suggests other performers to explore from a performer's page. Only performers with a photo are
// returned. Ranking: most titles shared (co-stars) → most titles in the studios this performer works with →
// favourites → most titles overall, so a performer with no shared titles still gets sensible suggestions.
func (a *API) relatedPerformers(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return
	}
	limit := 12
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 40 {
		limit = v
	}
	args := []any{id, limit}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	// Everything is counted over the titles the user can see, and only
	// performers with one of those are suggested.
	visible := `SELECT mip.* FROM media_item_performer mip JOIN media_item mi ON mi.id = mip.media_item_id WHERE ` + visibleMediaCond(r, "mi", arg)
	onlyVisible := ""
	if libraryFiltered(r) {
		onlyVisible = ` AND t.media_count > 0`
	}
	rows, err := a.DB.Query(r.Context(), `
		WITH vis AS (`+visible+`), mine AS (
			SELECT media_item_id FROM vis WHERE performer_id = $1
		), co AS (
			SELECT mip.performer_id, COUNT(*) AS shared
			FROM vis mip JOIN mine ON mine.media_item_id = mip.media_item_id
			WHERE mip.performer_id <> $1 GROUP BY mip.performer_id
		), my_studios AS (
			SELECT DISTINCT mi.studio_id FROM media_item mi JOIN mine ON mine.media_item_id = mi.id WHERE mi.studio_id IS NOT NULL
		), studio_mates AS (
			SELECT mip.performer_id, COUNT(*) AS in_studios
			FROM vis mip JOIN media_item mi ON mi.id = mip.media_item_id
			JOIN my_studios ms ON ms.studio_id = mi.studio_id
			WHERE mip.performer_id <> $1 GROUP BY mip.performer_id
		), totals AS (
			SELECT performer_id, COUNT(*) AS media_count FROM vis GROUP BY performer_id
		)
		SELECT p.id, p.name, p.display_name, p.profile_image_id, `+performerFavorite("p", currentUserID(r))+`,
		       COALESCE(t.media_count, 0), COALESCE(co.shared, 0)
		FROM performer p
		LEFT JOIN co ON co.performer_id = p.id
		LEFT JOIN studio_mates sm ON sm.performer_id = p.id
		LEFT JOIN totals t ON t.performer_id = p.id
		WHERE p.id <> $1 AND p.profile_image_id IS NOT NULL`+onlyVisible+`
		ORDER BY COALESCE(co.shared, 0) DESC, COALESCE(sm.in_studios, 0) DESC, `+performerFavorite("p", currentUserID(r))+` DESC, COALESCE(t.media_count, 0) DESC, p.id
		LIMIT $2
	`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load related performers")
		return
	}
	defer rows.Close()

	items := []relatedPerformer{}
	for rows.Next() {
		var p relatedPerformer
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.ProfileImageID, &p.Favorite, &p.MediaCount, &p.SharedTitles); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read related performer")
			return
		}
		items = append(items, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// emptyPerformers matches performers with no titles, acted in or crewed.
// Anyone's favorites are kept: they were starred on purpose, so they are not
// clutter.
const emptyPerformers = `NOT EXISTS (SELECT 1 FROM performer_favorite pf WHERE pf.performer_id = performer.id)
	AND NOT EXISTS (SELECT 1 FROM media_item_performer mip WHERE mip.performer_id = performer.id)
	AND NOT EXISTS (SELECT 1 FROM media_item_crew mic WHERE mic.performer_id = performer.id)`

// performerFavorite is a SQL boolean: whether the user starred the performer
// aliased as alias. The id is inlined (it is an int64, never user text) so
// queries sharing their bind params with a COUNT query stay valid.
func performerFavorite(alias string, userID int64) string {
	return `EXISTS (SELECT 1 FROM performer_favorite pf WHERE pf.performer_id = ` + alias + `.id AND pf.user_id = ` + strconv.FormatInt(userID, 10) + `)`
}

func currentUserID(r *http.Request) int64 {
	user, _ := UserFromContext(r.Context())
	return user.ID
}

// countEmptyPerformers reports how many performers cleanupPerformers would delete.
func (a *API) countEmptyPerformers(w http.ResponseWriter, r *http.Request) {
	var n int
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM performer WHERE `+emptyPerformers).Scan(&n); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count performers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n})
}

// cleanupPerformers deletes every performer that has no titles (except favorites).
func (a *API) cleanupPerformers(w http.ResponseWriter, r *http.Request) {
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM performer WHERE `+emptyPerformers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up performers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": tag.RowsAffected()})
}

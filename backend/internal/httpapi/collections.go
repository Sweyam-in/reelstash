package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type collection struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	Kind        string            `json:"kind"`
	Rules       json.RawMessage   `json:"rules,omitempty"`
	MediaCount  int64             `json:"mediaCount"`
	Pinned      bool              `json:"pinned"`
	Covers      []collectionCover `json:"covers,omitempty"`
}

// collectionCover is one of the first few titles in a playlist, used to build its cover mosaic.
type collectionCover struct {
	ID            int64  `json:"id"`
	PosterImageID *int64 `json:"posterImageId,omitempty"`
}

// Unassigned playlists remain available to API clients that have not chosen a
// library. The media app always chooses one, so they never appear in its views.
func collectionLibraryCond(r *http.Request, alias string, arg func(any) string) string {
	scope := scopeFromContext(r.Context())
	if scope.Selected > 0 || scope.All {
		return visibleMediaCond(r, alias, arg)
	}
	return "(" + alias + ".library_id IS NULL OR " + visibleMediaCond(r, alias, arg) + ")"
}

func (a *API) listCollections(w http.ResponseWriter, r *http.Request) {
	// A playlist belongs to the library in which it was created.
	args := []any{currentUserID(r)}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	visible := visibleMediaCond(r, "mi", arg)
	collectionScope := collectionLibraryCond(r, "c", arg)
	rows, err := a.DB.Query(r.Context(), `
		SELECT c.id, c.name, c.description, c.kind, c.rules, COUNT(mi.id) AS media_count, c.pinned
		FROM collection c
		LEFT JOIN (collection_media_item cmi JOIN media_item mi ON mi.id = cmi.media_item_id AND `+visible+`) ON cmi.collection_id = c.id
		WHERE c.user_id = $1 AND `+collectionScope+`
		GROUP BY c.id
		ORDER BY c.pinned DESC, c.name
	`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load collections")
		return
	}
	defer rows.Close()

	items := []collection{}
	for rows.Next() {
		var c collection
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Kind, &c.Rules, &c.MediaCount, &c.Pinned); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read collection")
			return
		}
		items = append(items, c)
	}
	rows.Close()

	// Up to four covers per playlist, in playlist order, in a single query.
	coverRows, err := a.DB.Query(r.Context(), `
		SELECT collection_id, media_item_id, poster_image_id FROM (
			SELECT cmi.collection_id, cmi.media_item_id, mi.poster_image_id,
				row_number() OVER (PARTITION BY cmi.collection_id ORDER BY cmi.position, mi.id) AS rn
			FROM collection_media_item cmi JOIN media_item mi ON mi.id = cmi.media_item_id
			JOIN collection c ON c.id = cmi.collection_id
			WHERE c.user_id = $1 AND `+collectionScope+` AND `+visible+`
		) t WHERE rn <= 4 ORDER BY collection_id, rn
	`, args...)
	if err == nil {
		defer coverRows.Close()
		byID := make(map[int64]int, len(items))
		for i, c := range items {
			byID[c.ID] = i
		}
		for coverRows.Next() {
			var cid int64
			var cover collectionCover
			if err := coverRows.Scan(&cid, &cover.ID, &cover.PosterImageID); err != nil {
				break
			}
			if i, ok := byID[cid]; ok {
				items[i].Covers = append(items[i].Covers, cover)
			}
		}
	}

	// Smart playlists hold no rows of their own: count and cover them from their rules.
	for i := range items {
		if items[i].Kind != "SMART" {
			continue
		}
		rules, err := parseSmartRules(items[i].Rules)
		if err != nil {
			continue // a playlist whose rules no longer parse just shows as empty
		}
		page, total, err := a.smartPage(r, rules, 4, 0)
		if err != nil {
			continue
		}
		items[i].MediaCount = int64(total)
		for _, it := range page {
			items[i].Covers = append(items[i].Covers, collectionCover{ID: it.ID, PosterImageID: it.PosterImageID})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// playlistItem is one title row of a playlist.
type playlistItem struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	StudioName    string `json:"studioName"`
	PosterImageID *int64 `json:"posterImageId,omitempty"`
}

// smartPage returns one page of the titles a smart playlist's rules select
// for the current user, and how many titles match in all.
func (a *API) smartPage(r *http.Request, rules smartRules, limit, offset int) ([]playlistItem, int, error) {
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	cte, err := a.smartMatches(r, rules, arg)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), `WITH `+cte+` SELECT COUNT(*) FROM matched`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := a.DB.Query(r.Context(), `WITH `+cte+`
		SELECT mi.id, mi.title, COALESCE(s.name, ''), mi.poster_image_id
		FROM matched m JOIN media_item mi ON mi.id = m.id
		LEFT JOIN studio s ON s.id = mi.studio_id
		ORDER BY m.pos LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []playlistItem{}
	for rows.Next() {
		var it playlistItem
		if err := rows.Scan(&it.ID, &it.Title, &it.StudioName, &it.PosterImageID); err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}
	return items, total, rows.Err()
}

type updateCollectionRequest struct {
	Pinned      *bool            `json:"pinned"`
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Rules       *json.RawMessage `json:"rules"`
}

// updateCollection pins or unpins a playlist, renames it, changes its
// description, and (for smart playlists) replaces its rules. Only the fields
// sent are changed.
func (a *API) updateCollection(w http.ResponseWriter, r *http.Request) {
	id, err := collectionID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	var req updateCollectionRequest
	if err := decodeJSON(r, &req); err != nil || (req.Pinned == nil && req.Name == nil && req.Description == nil && req.Rules == nil) {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || len(trimmed) > 120 {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		req.Name = &trimmed
	}
	var kind string
	if err := a.DB.QueryRow(r.Context(), `SELECT kind FROM collection WHERE id = $1`, id).Scan(&kind); err != nil {
		writeError(w, http.StatusNotFound, "playlist not found")
		return
	}
	var rules any
	if req.Rules != nil {
		if kind != "SMART" {
			writeError(w, http.StatusBadRequest, "only smart playlists have rules")
			return
		}
		parsed, err := parseSmartRules(*req.Rules)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		normalized, _ := json.Marshal(parsed)
		rules = json.RawMessage(normalized)
	}
	_, err = a.DB.Exec(r.Context(), `
		UPDATE collection SET
			pinned = COALESCE($2, pinned),
			name = COALESCE($3, name),
			description = CASE WHEN $4::boolean THEN NULLIF($5, '') ELSE description END,
			rules = CASE WHEN $6::boolean THEN $7::jsonb ELSE rules END,
			updated_at = now()
		WHERE id = $1
	`, id, req.Pinned, req.Name, req.Description != nil, derefString(req.Description), req.Rules != nil, rules)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeError(w, http.StatusConflict, "you already have a playlist with that name")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update playlist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type createCollectionRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Rules       json.RawMessage `json:"rules"`
}

func (a *API) createCollection(w http.ResponseWriter, r *http.Request) {
	var req createCollectionRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Kind != "MANUAL" && req.Kind != "SMART" {
		req.Kind = "MANUAL"
	}
	var rules any
	if req.Kind == "SMART" {
		parsed, err := parseSmartRules(req.Rules)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		normalized, _ := json.Marshal(parsed)
		rules = json.RawMessage(normalized)
	}

	var id int64
	scope := scopeFromContext(r.Context())
	if scope.Selected > 0 && !scope.canAccess(&scope.Selected) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	var libraryID *int64
	if scope.Selected > 0 {
		libraryID = &scope.Selected
	}
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO collection (name, description, kind, rules, user_id, library_id) VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6)
		RETURNING id
	`, req.Name, req.Description, req.Kind, rules, currentUserID(r), libraryID).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeError(w, http.StatusConflict, "you already have a playlist with that name")
		return
	}
	if err != nil {
		log.Printf("create collection: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create collection")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func collectionID(r *http.Request) (int64, error) {
	return pathInt64(r, "id")
}

// requireOwnCollection guards /collections/{id}/...: someone else's playlist
// answers 404, exactly like one that does not exist.
func (a *API) requireOwnCollection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := collectionID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid collection id")
			return
		}
		var own bool
		args := []any{id, currentUserID(r)}
		arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
		visible := collectionLibraryCond(r, "c", arg)
		if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM collection c WHERE c.id = $1 AND c.user_id = $2 AND `+visible+`)`, args...).Scan(&own); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load playlist")
			return
		}
		if !own {
			writeError(w, http.StatusNotFound, "playlist not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) getCollection(w http.ResponseWriter, r *http.Request) {
	id, err := collectionID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	var c collection
	err = a.DB.QueryRow(r.Context(), `SELECT id, name, description, kind, rules, pinned FROM collection WHERE id = $1`, id).Scan(&c.ID, &c.Name, &c.Description, &c.Kind, &c.Rules, &c.Pinned)
	if err != nil {
		writeError(w, http.StatusNotFound, "collection not found")
		return
	}
	limit, offset := parseLimitOffset(r)
	if c.Kind == "SMART" {
		rules, err := parseSmartRules(c.Rules)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "this playlist's rules are no longer valid: "+err.Error())
			return
		}
		items, total, err := a.smartPage(r, rules, limit, offset)
		if err != nil {
			log.Printf("smart playlist %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "failed to load playlist items")
			return
		}
		c.MediaCount = int64(total)
		writeJSON(w, http.StatusOK, map[string]any{"collection": c, "items": items, "limit": limit, "offset": offset, "total": total})
		return
	}
	args := []any{id, limit, offset}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	visible := visibleMediaCond(r, "mi", arg)
	var total int
	if err := a.DB.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM collection_media_item cmi JOIN media_item mi ON mi.id = cmi.media_item_id
		WHERE cmi.collection_id = $1 AND `+visible+` AND $2::int IS NOT NULL AND $3::int IS NOT NULL`, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count playlist items")
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT mi.id, mi.title, COALESCE(s.name, ''), mi.poster_image_id
		FROM collection_media_item cmi
		JOIN media_item mi ON mi.id = cmi.media_item_id
		LEFT JOIN studio s ON s.id = mi.studio_id
		WHERE cmi.collection_id = $1 AND `+visible+`
		ORDER BY cmi.position, cmi.media_item_id
		LIMIT $2 OFFSET $3`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load playlist items")
		return
	}
	defer rows.Close()
	items := []playlistItem{}
	for rows.Next() {
		var media playlistItem
		if err := rows.Scan(&media.ID, &media.Title, &media.StudioName, &media.PosterImageID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read playlist item")
			return
		}
		items = append(items, media)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load playlist items")
		return
	}
	c.MediaCount = int64(total)
	writeJSON(w, http.StatusOK, map[string]any{"collection": c, "items": items, "limit": limit, "offset": offset, "total": total})
}

func (a *API) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, err := collectionID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	result, err := a.DB.Exec(r.Context(), `DELETE FROM collection WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete playlist")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "playlist not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) addCollectionItem(w http.ResponseWriter, r *http.Request) {
	id, err := collectionID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	var req struct {
		MediaItemID int64 `json:"mediaItemId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.MediaItemID < 1 {
		writeError(w, http.StatusBadRequest, "mediaItemId is required")
		return
	}
	if !a.mediaAccessible(r, req.MediaItemID) {
		writeError(w, http.StatusNotFound, "media item not found")
		return
	}
	if selected := scopeFromContext(r.Context()).Selected; selected > 0 {
		var inLibrary bool
		if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM media_item WHERE id = $1 AND library_id = $2)`, req.MediaItemID, selected).Scan(&inLibrary); err != nil || !inLibrary {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
	}
	var kind string
	if err := a.DB.QueryRow(r.Context(), `SELECT kind FROM collection WHERE id = $1`, id).Scan(&kind); err != nil {
		writeError(w, http.StatusNotFound, "playlist not found")
		return
	}
	if kind != "MANUAL" {
		writeError(w, http.StatusBadRequest, "items can only be added to manual playlists")
		return
	}
	_, err = a.DB.Exec(r.Context(), `INSERT INTO collection_media_item (collection_id, media_item_id, position)
		SELECT $1, mi.id, COALESCE((SELECT MAX(position) + 1 FROM collection_media_item WHERE collection_id = $1), 0)
		FROM media_item mi WHERE mi.id = $2 ON CONFLICT DO NOTHING`, id, req.MediaItemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add title to playlist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) removeCollectionItem(w http.ResponseWriter, r *http.Request) {
	id, err := collectionID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	mediaID, err := pathInt64(r, "mediaItemID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	result, err := a.DB.Exec(r.Context(), `DELETE FROM collection_media_item WHERE collection_id = $1 AND media_item_id = $2`, id, mediaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove title from playlist")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "title not found in playlist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// moveCollectionItem moves a title to a 0-based index within a manual playlist and renumbers the rest
// in one statement, so positions stay dense and concurrent moves can't leave duplicates.
func (a *API) moveCollectionItem(w http.ResponseWriter, r *http.Request) {
	id, err := collectionID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id")
		return
	}
	mediaID, err := pathInt64(r, "mediaItemID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req struct {
		Position *int `json:"position"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Position == nil || *req.Position < 0 {
		writeError(w, http.StatusBadRequest, "position is required")
		return
	}
	var kind string
	if err := a.DB.QueryRow(r.Context(), `SELECT kind FROM collection WHERE id = $1`, id).Scan(&kind); err != nil {
		writeError(w, http.StatusNotFound, "playlist not found")
		return
	}
	if kind != "MANUAL" {
		writeError(w, http.StatusBadRequest, "only manual playlists can be reordered")
		return
	}
	result, err := a.DB.Exec(r.Context(), `
		WITH ordered AS (
			SELECT media_item_id, (row_number() OVER (ORDER BY position, media_item_id) - 1)::int AS idx
			FROM collection_media_item WHERE collection_id = $1
		), target AS (
			SELECT idx AS old_idx, LEAST($3::int, (SELECT COUNT(*) FROM ordered)::int - 1) AS new_idx FROM ordered WHERE media_item_id = $2
		), renumbered AS (
			SELECT o.media_item_id, CASE
				WHEN o.media_item_id = $2 THEN t.new_idx
				WHEN o.idx > t.old_idx AND o.idx <= t.new_idx THEN o.idx - 1
				WHEN o.idx < t.old_idx AND o.idx >= t.new_idx THEN o.idx + 1
				ELSE o.idx END AS pos
			FROM ordered o, target t
		)
		UPDATE collection_media_item cmi SET position = r.pos
		FROM renumbered r WHERE cmi.collection_id = $1 AND cmi.media_item_id = r.media_item_id`, id, mediaID, *req.Position)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder playlist")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "title not found in playlist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

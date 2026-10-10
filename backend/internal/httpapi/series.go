package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type series struct {
	ID            int64      `json:"id"`
	StudioID      *int64     `json:"studioId,omitempty"`
	StudioName    *string    `json:"studioName,omitempty"`
	Name          string     `json:"name"`
	Description   *string    `json:"description,omitempty"`
	ReleaseDate   *time.Time `json:"releaseDate,omitempty"`
	PosterImageID *int64     `json:"posterImageId,omitempty"`
	// BackdropImageID is an uploaded backdrop, shown behind the series page header.
	BackdropImageID *int64 `json:"backdropImageId,omitempty"`
	MediaCount      int64  `json:"mediaCount"`
	// FirstMediaItemID is the series' first title, whose artwork stands in
	// when the series has no poster of its own.
	FirstMediaItemID *int64 `json:"firstMediaItemId,omitempty"`
	// PosterCustom is true when the poster was uploaded or picked by hand.
	PosterCustom bool `json:"posterCustom"`
	// Covers are the first few titles, for a thumbnail collage when the
	// series has no poster.
	Covers []collectionCover `json:"covers"`
	// Kind is "show" (a TV show), "collection" (of movies) or empty.
	Kind        *string `json:"kind,omitempty"`
	SeasonCount int64   `json:"seasonCount"`
}

// seriesColumns selects a series; its title count and first title only
// consider titles matching visible (a condition on mi, "TRUE" for all).
func seriesColumns(visible string) string {
	return `
	se.id, se.studio_id, s.name, se.name, se.description, se.release_date, se.poster_image_id, se.backdrop_image_id,
	(SELECT COUNT(*) FROM media_item mi WHERE mi.series_id = se.id AND ` + visible + `),
	(SELECT mi.id FROM media_item mi WHERE mi.series_id = se.id AND ` + visible + ` ORDER BY mi.series_position NULLS LAST, mi.release_date NULLS LAST, mi.id LIMIT 1),
	COALESCE((SELECT ar.custom FROM artwork ar WHERE ar.id = se.poster_image_id), false),
	se.kind,
	(SELECT COUNT(DISTINCT mi.season_number) FROM media_item mi WHERE mi.series_id = se.id AND mi.season_number IS NOT NULL AND ` + visible + `)
	FROM series se LEFT JOIN studio s ON s.id = se.studio_id`
}

func scanSeries(row pgx.Row) (series, error) {
	var s series
	err := row.Scan(&s.ID, &s.StudioID, &s.StudioName, &s.Name, &s.Description, &s.ReleaseDate, &s.PosterImageID, &s.BackdropImageID, &s.MediaCount, &s.FirstMediaItemID, &s.PosterCustom, &s.Kind, &s.SeasonCount)
	s.Covers = []collectionCover{}
	return s, err
}

// loadSeriesCovers fills in up to four covers per series, in series order,
// in a single query.
func (a *API) loadSeriesCovers(ctx context.Context, items []series, libraryIDs []int64) {
	if len(items) == 0 {
		return
	}
	ids := make([]int64, len(items))
	byID := make(map[int64]int, len(items))
	for i, s := range items {
		ids[i] = s.ID
		byID[s.ID] = i
	}
	rows, err := a.DB.Query(ctx, `
		SELECT series_id, id, poster_image_id FROM (
			SELECT mi.series_id, mi.id, mi.poster_image_id,
				row_number() OVER (PARTITION BY mi.series_id ORDER BY mi.series_position NULLS LAST, mi.release_date NULLS LAST, mi.id) AS rn
			FROM media_item mi WHERE mi.series_id = ANY($1) AND ($2::bigint[] IS NULL OR mi.library_id = ANY($2))
		) t WHERE rn <= 4 ORDER BY series_id, rn
	`, ids, libraryIDs)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sid int64
		var cover collectionCover
		if rows.Scan(&sid, &cover.ID, &cover.PosterImageID) != nil {
			return
		}
		if i, ok := byID[sid]; ok {
			items[i].Covers = append(items[i].Covers, cover)
		}
	}
}

func seriesID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return 0, false
	}
	return id, true
}

func (a *API) listSeries(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	var conds []string
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if search != "" {
		conds = append(conds, `se.name ILIKE `+arg(likeContains(search))+` ESCAPE '\'`)
	}
	visible := visibleMediaCond(r, "mi", arg)
	switch r.URL.Query().Get("kind") {
	case "show":
		conds = append(conds, `se.kind = 'show'`)
	case "collection":
		// Everything but TV shows: collections and plain series.
		conds = append(conds, `se.kind IS DISTINCT FROM 'show'`)
	}
	if libraryFiltered(r) {
		conds = append(conds, `EXISTS (SELECT 1 FROM media_item mi WHERE mi.series_id = se.id AND `+visible+`)`)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM series se`+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count series")
		return
	}
	args = append(args, limit, offset)
	limitArg := len(args) - 1
	rows, err := a.DB.Query(r.Context(), `SELECT `+seriesColumns(visible)+where+`
		ORDER BY LOWER(se.name), se.id
		LIMIT $`+strconv.Itoa(limitArg)+` OFFSET $`+strconv.Itoa(limitArg+1), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load series")
		return
	}
	defer rows.Close()

	items := []series{}
	for rows.Next() {
		s, err := scanSeries(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read series")
			return
		}
		items = append(items, s)
	}
	rows.Close()
	a.loadSeriesCovers(r.Context(), items, visibleLibraryIDs(r))

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset, "total": total})
}

func (a *API) getSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	if !a.entityAccessible(r, seriesMediaExists, id) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	args := []any{id}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	s, err := scanSeries(a.DB.QueryRow(r.Context(), `SELECT `+seriesColumns(visibleMediaCond(r, "mi", arg))+` WHERE se.id = $1`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load series")
		return
	}
	one := []series{s}
	a.loadSeriesCovers(r.Context(), one, visibleLibraryIDs(r))
	providerPosters := map[string]string{}
	if a.Namer != nil {
		providerPosters = a.Namer.SeriesProviderPosters(r.Context(), id)
	}
	// A TV show's seasons, with how many episodes of each the user can see.
	type season struct {
		Number         int32 `json:"number"`
		EpisodeCount   int64 `json:"episodeCount"`
		FirstEpisodeID int64 `json:"firstEpisodeId"`
	}
	seasons := []season{}
	seasonArgs := []any{id}
	seasonArg := func(v any) string { seasonArgs = append(seasonArgs, v); return "$" + strconv.Itoa(len(seasonArgs)) }
	if rows, err := a.DB.Query(r.Context(), `
		SELECT mi.season_number, COUNT(*), (array_agg(mi.id ORDER BY mi.episode_number NULLS LAST, mi.id))[1]
		FROM media_item mi WHERE mi.series_id = $1 AND mi.season_number IS NOT NULL AND `+visibleMediaCond(r, "mi", seasonArg)+`
		GROUP BY mi.season_number ORDER BY mi.season_number`, seasonArgs...); err == nil {
		for rows.Next() {
			var se season
			if rows.Scan(&se.Number, &se.EpisodeCount, &se.FirstEpisodeID) == nil {
				seasons = append(seasons, se)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": s.ID, "studioId": s.StudioID, "studioName": s.StudioName, "name": s.Name, "description": s.Description,
		"releaseDate": s.ReleaseDate, "posterImageId": s.PosterImageID, "backdropImageId": s.BackdropImageID, "posterCustom": s.PosterCustom, "mediaCount": s.MediaCount,
		"firstMediaItemId": s.FirstMediaItemID, "covers": one[0].Covers, "providerPosters": providerPosters,
		"links": a.loadLinks(r.Context(), "series", id), "kind": s.Kind, "seasons": seasons,
	})
}

type seriesRequest struct {
	Name        string `json:"name"`
	StudioID    *int64 `json:"studioId"`
	Description string `json:"description"`
}

func seriesWriteError(w http.ResponseWriter, err error, fallback string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeError(w, http.StatusConflict, "a series with that name already exists")
		return
	}
	writeError(w, http.StatusInternalServerError, fallback)
}

func (a *API) createSeries(w http.ResponseWriter, r *http.Request) {
	var req seriesRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO series (name, studio_id, description) VALUES ($1, $2, NULLIF($3, ''))
		RETURNING id
	`, strings.TrimSpace(req.Name), req.StudioID, req.Description).Scan(&id)
	if err != nil {
		seriesWriteError(w, err, "failed to create series")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) updateSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	var req seriesRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE series SET name = $2, studio_id = $3, description = NULLIF($4, ''), updated_at = now()
		WHERE id = $1
	`, id, strings.TrimSpace(req.Name), req.StudioID, req.Description)
	if err != nil {
		seriesWriteError(w, err, "failed to update series")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (a *API) deleteSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	// Titles stay in the library; they only leave the series.
	a.DB.Exec(r.Context(), `UPDATE media_item SET series_position = NULL WHERE series_id = $1`, id)
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM series WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete series")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "series not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) serveSeriesImage(w http.ResponseWriter, r *http.Request) {
	a.serveSeriesArtwork(w, r, "poster_image_id")
}

func (a *API) serveSeriesBackdrop(w http.ResponseWriter, r *http.Request) {
	a.serveSeriesArtwork(w, r, "backdrop_image_id")
}

// serveSeriesArtwork streams a series image; column is a fixed column name, never user input.
func (a *API) serveSeriesArtwork(w http.ResponseWriter, r *http.Request, column string) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	var key, mime string
	err = a.DB.QueryRow(r.Context(), `
		SELECT ar.storage_key, ar.mime_type FROM series se
		JOIN artwork ar ON ar.id = se.`+column+`
		WHERE se.id = $1
	`, id).Scan(&key, &mime)
	if err != nil || key == "" || filepath.Base(key) != key {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(a.Cfg.PosterDir, key))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, key, fileModTime(file), file)
}

// ---- titles in a series ----

// nextSeriesPosition is one past the highest number used in the series.
func (a *API) nextSeriesPosition(ctx context.Context, id int64) int {
	var n int
	a.DB.QueryRow(ctx, `SELECT COALESCE(MAX(series_position), 0) + 1 FROM media_item WHERE series_id = $1`, id).Scan(&n)
	return n
}

// addSeriesMedia adds titles to a series, numbered after the ones already in it.
func (a *API) addSeriesMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	var req struct {
		MediaItemIDs []int64 `json:"mediaItemIds"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.MediaItemIDs) == 0 {
		writeError(w, http.StatusBadRequest, "mediaItemIds is required")
		return
	}
	next := a.nextSeriesPosition(r.Context(), id)
	added := 0
	for _, mid := range req.MediaItemIDs {
		tag, err := a.DB.Exec(r.Context(), `
			UPDATE media_item SET series_id = $2, series_position = $3, updated_at = now()
			WHERE id = $1 AND series_id IS DISTINCT FROM $2
		`, mid, id, next)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add to series")
			return
		}
		if tag.RowsAffected() > 0 {
			added++
			next++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

func (a *API) removeSeriesMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	mid, err := pathInt64(r, "mediaItemId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		UPDATE media_item SET series_id = NULL, series_position = NULL, updated_at = now() WHERE id = $1 AND series_id = $2
	`, mid, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove from series")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// orderSeriesMedia numbers the given titles 1, 2, 3… in the order sent.
func (a *API) orderSeriesMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	var req struct {
		MediaItemIDs []int64 `json:"mediaItemIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder series")
		return
	}
	defer tx.Rollback(r.Context())
	for i, mid := range req.MediaItemIDs {
		if _, err := tx.Exec(r.Context(), `
			UPDATE media_item SET series_position = $3, updated_at = now() WHERE id = $1 AND series_id = $2
		`, mid, id, i+1); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder series")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder series")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ordered": len(req.MediaItemIDs)})
}

// setMediaSeries puts one title in a series (an existing one by id, or a new
// one by name) at a number (0 clears it), or takes it out with seriesId 0 and
// no name.
func (a *API) setMediaSeries(w http.ResponseWriter, r *http.Request) {
	mid, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req struct {
		SeriesID   int64  `json:"seriesId"`
		SeriesName string `json:"seriesName"`
		Position   *int   `json:"position"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Position != nil && *req.Position < 0 {
		writeError(w, http.StatusBadRequest, "position cannot be negative")
		return
	}
	ctx := r.Context()
	name := strings.TrimSpace(req.SeriesName)
	if req.SeriesID == 0 && name != "" {
		err := a.DB.QueryRow(ctx, `SELECT id FROM series WHERE lower(name) = lower($1) ORDER BY id LIMIT 1`, name).Scan(&req.SeriesID)
		if errors.Is(err, pgx.ErrNoRows) {
			// A new series takes the title's studio.
			err = a.DB.QueryRow(ctx, `
				INSERT INTO series (name, studio_id) VALUES ($1, (SELECT studio_id FROM media_item WHERE id = $2)) RETURNING id
			`, name, mid).Scan(&req.SeriesID)
		}
		if err != nil {
			seriesWriteError(w, err, "failed to create series")
			return
		}
	}
	if req.SeriesID == 0 {
		_, err = a.DB.Exec(ctx, `UPDATE media_item SET series_id = NULL, series_position = NULL, updated_at = now() WHERE id = $1`, mid)
	} else {
		var current *int64
		if err := a.DB.QueryRow(ctx, `SELECT series_id FROM media_item WHERE id = $1`, mid).Scan(&current); err != nil {
			writeError(w, http.StatusNotFound, "title not found")
			return
		}
		pos := req.Position
		if pos == nil && (current == nil || *current != req.SeriesID) {
			n := a.nextSeriesPosition(ctx, req.SeriesID)
			pos = &n
		}
		_, err = a.DB.Exec(ctx, `
			UPDATE media_item SET series_id = $2, series_position = NULLIF(COALESCE($3, series_position), 0), updated_at = now() WHERE id = $1
		`, mid, req.SeriesID, pos)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update series")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": mid, "seriesId": req.SeriesID})
}

// ---- provider matching ----

func (a *API) seriesProvidersReady(w http.ResponseWriter) bool {
	if a.Namer == nil || !a.Namer.SeriesEnabled() {
		writeError(w, http.StatusServiceUnavailable, "no metadata provider that supports series is configured (add an API key under Metadata → Providers)")
		return false
	}
	return true
}

func (a *API) searchSeriesProviders(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok || !a.seriesProvidersReady(w) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		a.DB.QueryRow(r.Context(), `SELECT name FROM series WHERE id = $1`, id).Scan(&query)
	}
	items, err := a.Namer.SearchSeries(r.Context(), query, splitCSV(r.URL.Query().Get("providers")))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if items == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "query": query})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "query": query})
}

func (a *API) linkSeriesProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok || !a.seriesProvidersReady(w) {
		return
	}
	var req struct {
		Provider   string `json:"provider"`
		ExternalID string `json:"externalId"`
		Rename     bool   `json:"rename"`
		// Parent links the movie, show or collection a chosen title belongs
		// to instead of the title itself.
		Parent bool `json:"parent"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Provider == "" || !externalIDPattern.MatchString(req.ExternalID) || strings.Contains(req.ExternalID, "..") {
		writeError(w, http.StatusBadRequest, "provider and externalId are required")
		return
	}
	res, err := a.Namer.LinkSeries(r.Context(), id, req.Provider, req.ExternalID, req.Rename, req.Parent)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "title": res.Title, "partCount": len(res.Parts)})
}

func (a *API) unlinkSeriesProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok || a.Namer == nil {
		return
	}
	if err := a.Namer.UnlinkSeries(r.Context(), id, chi.URLParam(r, "provider")); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink series")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listSeriesParts(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	if a.Namer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	views, err := a.Namer.SeriesParts(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load series parts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

// assignSeriesPart pairs a title with a scene/episode of the linked series.
// mode "apply" applies the part's metadata to the title (rename, NFO,
// details); "link" only records the link and the title's number.
func (a *API) assignSeriesPart(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok || !a.namerReady(w) {
		return
	}
	var req struct {
		MediaItemID int64  `json:"mediaItemId"`
		Provider    string `json:"provider"`
		ExternalID  string `json:"externalId"`
		Mode        string `json:"mode"`
	}
	if err := decodeJSON(r, &req); err != nil || req.MediaItemID < 1 || req.Provider == "" || req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "mediaItemId, provider and externalId are required")
		return
	}
	apply := req.Mode != "link"
	var fileID int64
	if apply {
		var found bool
		if fileID, found = a.primaryMediaFileID(r, req.MediaItemID); !found {
			writeError(w, http.StatusConflict, "this title has no available file to match")
			return
		}
	}
	if err := a.Namer.AssignPart(r.Context(), id, req.MediaItemID, req.Provider, req.ExternalID, apply, fileID); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "mediaItemId": req.MediaItemID})
}

// uploadSeriesImage sets an uploaded image as the series poster.
func (a *API) uploadSeriesImage(w http.ResponseWriter, r *http.Request) {
	a.uploadSeriesArtwork(w, r, false)
}

// uploadSeriesBackdrop sets an uploaded image as the series backdrop.
func (a *API) uploadSeriesBackdrop(w http.ResponseWriter, r *http.Request) {
	a.uploadSeriesArtwork(w, r, true)
}

func (a *API) uploadSeriesArtwork(w http.ResponseWriter, r *http.Request, backdrop bool) {
	id, ok := seriesID(w, r)
	if !ok || a.Namer == nil {
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
	save := a.Namer.SetSeriesPosterUpload
	if backdrop {
		save = a.Namer.SetSeriesBackdropUpload
	}
	if err := save(r.Context(), id, body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "series not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if backdrop {
		a.writeSeriesBackdrop(w, r, id)
		return
	}
	a.writeSeriesPoster(w, r, id)
}

func (a *API) writeSeriesBackdrop(w http.ResponseWriter, r *http.Request, id int64) {
	var imageID *int64
	_ = a.DB.QueryRow(r.Context(), `SELECT backdrop_image_id FROM series WHERE id = $1`, id).Scan(&imageID)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "backdropImageId": imageID})
}

// useSeriesProviderImage sets the poster of a linked provider record as the
// series poster.
func (a *API) useSeriesProviderImage(w http.ResponseWriter, r *http.Request) {
	id, ok := seriesID(w, r)
	if !ok || a.Namer == nil {
		return
	}
	var req struct {
		Provider string `json:"provider"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if err := a.Namer.UseSeriesProviderPoster(r.Context(), id, req.Provider); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.writeSeriesPoster(w, r, id)
}

func (a *API) writeSeriesPoster(w http.ResponseWriter, r *http.Request, id int64) {
	var imageID *int64
	_ = a.DB.QueryRow(r.Context(), `SELECT poster_image_id FROM series WHERE id = $1`, id).Scan(&imageID)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "posterImageId": imageID})
}

// deleteSeriesImage removes the series poster; the series then shows a
// collage of its videos' thumbnails.
func (a *API) deleteSeriesImage(w http.ResponseWriter, r *http.Request) {
	a.deleteSeriesArtwork(w, r, "poster_image_id")
}

// deleteSeriesBackdrop removes the series backdrop.
func (a *API) deleteSeriesBackdrop(w http.ResponseWriter, r *http.Request) {
	a.deleteSeriesArtwork(w, r, "backdrop_image_id")
}

// deleteSeriesArtwork clears one image column (a fixed name, never user input) and removes its file.
func (a *API) deleteSeriesArtwork(w http.ResponseWriter, r *http.Request, column string) {
	id, ok := seriesID(w, r)
	if !ok {
		return
	}
	var imageID *int64
	var key string
	if err := a.DB.QueryRow(r.Context(), `
		SELECT se.`+column+`, COALESCE(ar.storage_key, '')
		FROM series se LEFT JOIN artwork ar ON ar.id = se.`+column+` WHERE se.id = $1
	`, id).Scan(&imageID, &key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "series not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load series")
		return
	}
	if imageID != nil {
		if _, err := a.DB.Exec(r.Context(), `UPDATE series SET `+column+` = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove the image")
			return
		}
		a.DB.Exec(r.Context(), `DELETE FROM artwork WHERE id = $1`, *imageID)
		if key != "" && filepath.Base(key) == key {
			os.Remove(filepath.Join(a.Cfg.PosterDir, key))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

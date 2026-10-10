package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type mediaItem struct {
	ID                int64   `json:"id"`
	Title             string  `json:"title"`
	OriginalTitle     *string `json:"originalTitle,omitempty"`
	SortTitle         *string `json:"sortTitle,omitempty"`
	StudioID          *int64  `json:"studioId,omitempty"`
	StudioName        *string `json:"studioName,omitempty"`
	StudioLogoImageID *int64  `json:"studioLogoImageId,omitempty"`
	SeriesID          *int64  `json:"seriesId,omitempty"`
	SeriesName        *string `json:"seriesName,omitempty"`
	SeriesPosition    *int32  `json:"seriesPosition,omitempty"`
	// Kind is "movie" or "episode" in entertainment libraries, else empty.
	Kind            *string    `json:"kind,omitempty"`
	SeasonNumber    *int32     `json:"seasonNumber,omitempty"`
	EpisodeNumber   *int32     `json:"episodeNumber,omitempty"`
	Description     *string    `json:"description,omitempty"`
	ReleaseDate     *time.Time `json:"releaseDate,omitempty"`
	ProductionYear  *int32     `json:"productionYear,omitempty"`
	DurationSeconds *float64   `json:"durationSeconds,omitempty"`
	Favorite        bool       `json:"favorite"` // the current user's own flag, filled by attachPerformers
	// Rating is the shared average across all users (0-10 scale, rounded);
	// MyRating is the current user's own rating, only set by loadMediaItemDetail.
	Rating          *int16           `json:"rating,omitempty"`
	RatingCount     int32            `json:"ratingCount"`
	MyRating        *int16           `json:"myRating,omitempty"`
	Watched         bool             `json:"watched"`
	WatchCount      int32            `json:"watchCount"`
	LastWatchedAt   *time.Time       `json:"lastWatchedAt,omitempty"`
	DateAdded       time.Time        `json:"dateAdded"`
	PosterImageID   *int64           `json:"posterImageId,omitempty"`
	BackdropImageID *int64           `json:"backdropImageId,omitempty"`
	Performers      []string         `json:"performers,omitempty"`
	Cast            []mediaPerformer `json:"cast,omitempty"`
	// Crew is directors and writers; only set by loadMediaItemDetail.
	Crew            []mediaCrew        `json:"crew,omitempty"`
	Categories      []string           `json:"categories,omitempty"`
	Tags            []string           `json:"tags,omitempty"`
	Files           []mediaFileSummary `json:"files,omitempty"`
	PositionSeconds *float64           `json:"positionSeconds,omitempty"`
	Reason          *string            `json:"reason,omitempty"`
	// HasPreview is true when a hover preview clip exists for the title.
	HasPreview bool `json:"hasPreview"`
	// WatchLater is the current user's watch-later flag; only set by getMediaItem.
	WatchLater bool `json:"watchLater"`
	// Reel detail, only set by loadMediaItemDetail: whether it shows under
	// Reels, the admin's override (null: automatic) and what was detected.
	Reel         *bool `json:"reel,omitempty"`
	ReelOverride *bool `json:"reelOverride"`
	ReelDetected *bool `json:"reelDetected,omitempty"`
}

type mediaPerformer struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	ProfileImageID *int64  `json:"profileImageId,omitempty"`
	Character      *string `json:"character,omitempty"`
}

type mediaCrew struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	ProfileImageID *int64 `json:"profileImageId,omitempty"`
	Job            string `json:"job"`
}

type mediaFileSummary struct {
	ID              int64  `json:"id"`
	StorageLocation string `json:"storageLocation"`
	RelativePath    string `json:"relativePath"`
	FileName        string `json:"fileName"`
	// OriginalFileName is the name before the first metadata rename; empty if never renamed.
	OriginalFileName string     `json:"originalFileName,omitempty"`
	Extension        string     `json:"extension"`
	FileSize         int64      `json:"fileSize"`
	Width            *int       `json:"width,omitempty"`
	Height           *int       `json:"height,omitempty"`
	FrameRate        *float64   `json:"frameRate,omitempty"`
	VideoCodec       *string    `json:"videoCodec,omitempty"`
	AudioCodec       *string    `json:"audioCodec,omitempty"`
	Container        *string    `json:"container,omitempty"`
	VideoBitrate     *int64     `json:"videoBitrate,omitempty"`
	AudioBitrate     *int64     `json:"audioBitrate,omitempty"`
	OverallBitrate   *int64     `json:"overallBitrate,omitempty"`
	DurationSeconds  *float64   `json:"durationSeconds,omitempty"`
	ModifiedTime     *time.Time `json:"modifiedTime,omitempty"`
	Available        bool       `json:"available"`
}

const mediaItemSelectColumns = `
	mi.id, mi.title, mi.original_title, mi.sort_title,
	mi.studio_id, s.name, s.logo_image_id, mi.series_id, se.name, mi.series_position,
	mi.kind, mi.season_number, mi.episode_number,
	mi.description, mi.release_date, mi.production_year, mi.duration_seconds,
	FALSE, mi.rating, mi.rating_count, mi.watched, mi.watch_count, mi.last_watched_at,
	mi.date_added, mi.poster_image_id, mi.backdrop_image_id,
	EXISTS (SELECT 1 FROM preview_clip pc JOIN media_file pmf ON pmf.id = pc.media_file_id WHERE pmf.media_item_id = mi.id)
`

const mediaItemSelect = `
	SELECT ` + mediaItemSelectColumns + `
	FROM media_item mi
	LEFT JOIN studio s ON s.id = mi.studio_id
	LEFT JOIN series se ON se.id = mi.series_id
`

// mediaItemScanTargets returns the scan destinations for mediaItemSelectColumns,
// in order, so other queries (e.g. continue-watching) can select those columns
// alongside their own and share the same Scan call.
func mediaItemScanTargets(m *mediaItem) []any {
	return []any{&m.ID, &m.Title, &m.OriginalTitle, &m.SortTitle,
		&m.StudioID, &m.StudioName, &m.StudioLogoImageID, &m.SeriesID, &m.SeriesName, &m.SeriesPosition,
		&m.Kind, &m.SeasonNumber, &m.EpisodeNumber,
		&m.Description, &m.ReleaseDate, &m.ProductionYear, &m.DurationSeconds,
		&m.Favorite, &m.Rating, &m.RatingCount, &m.Watched, &m.WatchCount, &m.LastWatchedAt,
		&m.DateAdded, &m.PosterImageID, &m.BackdropImageID, &m.HasPreview}
}

func scanMediaItem(row pgx.Row) (mediaItem, error) {
	var m mediaItem
	err := row.Scan(mediaItemScanTargets(&m)...)
	return m, err
}

func (a *API) attachPerformers(r *http.Request, items []mediaItem) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	byID := make(map[int64]*mediaItem, len(items))
	for i := range items {
		ids[i] = items[i].ID
		byID[items[i].ID] = &items[i]
	}

	// Favorites are per user; every listing passes through here.
	if user, ok := UserFromContext(r.Context()); ok {
		favRows, err := a.DB.Query(r.Context(), `SELECT media_item_id FROM media_item_favorite WHERE user_id = $1 AND media_item_id = ANY($2)`, user.ID, ids)
		if err != nil {
			return err
		}
		for favRows.Next() {
			var id int64
			if favRows.Scan(&id) == nil {
				if item, ok := byID[id]; ok {
					item.Favorite = true
				}
			}
		}
		favRows.Close()
	}

	rows, err := a.DB.Query(r.Context(), `
		SELECT mip.media_item_id, p.id, p.name, p.profile_image_id, mip.character
		FROM media_item_performer mip
		JOIN performer p ON p.id = mip.performer_id
		WHERE mip.media_item_id = ANY($1)
		ORDER BY mip.media_item_id, mip.position
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var mediaItemID int64
		var person mediaPerformer
		if err := rows.Scan(&mediaItemID, &person.ID, &person.Name, &person.ProfileImageID, &person.Character); err != nil {
			return err
		}
		if item, ok := byID[mediaItemID]; ok {
			item.Performers = append(item.Performers, person.Name)
			item.Cast = append(item.Cast, person)
		}
	}
	return rows.Err()
}

const (
	// defaultPageLimit and maxPageLimit are the list window when a client
	// omits limit or asks for more than the API allows. Offset stays 0 unless
	// the query provides a non-negative integer.
	defaultPageLimit = 30
	maxPageLimit     = 200
	// maxFuzzyTokens caps how many words a search turns into SQL conditions.
	maxFuzzyTokens = 6
)

func parseLimitOffset(r *http.Request) (limit, offset int) {
	limit, offset = defaultPageLimit, 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= maxPageLimit {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	return limit, offset
}

func likeContains(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
	return "%" + escaped + "%"
}

// fuzzyTokens splits a query into lowercase search tokens (max maxFuzzyTokens).
func fuzzyTokens(search string) []string {
	tokens := strings.Fields(strings.ToLower(search))
	if len(tokens) > maxFuzzyTokens {
		tokens = tokens[:maxFuzzyTokens]
	}
	return tokens
}

// fuzzyMatch builds a condition requiring every token to match haystack, either as a
// case-insensitive substring (partial words like "fux") or via pg_trgm word similarity
// (typos). arg registers a bind parameter and returns its placeholder.
// Tokens under 3 chars only match as substrings to avoid noise.
func fuzzyMatch(haystack string, tokens []string, arg func(any) string) string {
	conds := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		c := haystack + ` ILIKE ` + arg(likeContains(tok)) + ` ESCAPE '\'`
		if len([]rune(tok)) >= 3 {
			c += ` OR word_similarity(` + arg(tok) + `, ` + haystack + `) >= 0.5`
		}
		conds = append(conds, `(`+c+`)`)
	}
	return strings.Join(conds, " AND ")
}

func (a *API) listMediaItems(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	q := r.URL.Query()

	query := mediaItemSelect
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }

	if id := q.Get("performerId"); id != "" {
		// A person's titles: those they act in, and those they directed or wrote.
		query += ` JOIN (SELECT media_item_id FROM media_item_performer WHERE performer_id = ` + arg(id) + `
			UNION SELECT media_item_id FROM media_item_crew WHERE performer_id = ` + arg(id) + `) f_mip ON f_mip.media_item_id = mi.id`
	}
	if id := q.Get("categoryId"); id != "" {
		query += ` JOIN media_item_category f_mic ON f_mic.media_item_id = mi.id AND f_mic.category_id = ` + arg(id)
	}
	watchLater := q.Get("watchLater") == "true"
	if watchLater {
		user, _ := UserFromContext(r.Context())
		query += ` JOIN watch_later f_wl ON f_wl.media_item_id = mi.id AND f_wl.user_id = ` + arg(user.ID)
	}
	where := []string{}
	if f := libraryFilter(r, "mi.library_id", arg); f != "" {
		where = append(where, f)
	}
	if id := q.Get("studioId"); id != "" {
		where = append(where, `mi.studio_id = `+arg(id))
	}
	if id := q.Get("seriesId"); id != "" {
		where = append(where, `mi.series_id = `+arg(id))
	}
	if season, err := strconv.Atoi(q.Get("season")); err == nil && season >= 0 {
		where = append(where, `mi.season_number = `+arg(season))
	}
	switch q.Get("kind") {
	case "movie":
		// Everything that is not an episode: unclassified titles show as movies.
		where = append(where, `mi.kind IS DISTINCT FROM 'episode'`)
	case "episode":
		where = append(where, `mi.kind = 'episode'`)
	}
	if q.Get("favorite") == "true" {
		user, _ := UserFromContext(r.Context())
		where = append(where, `EXISTS (SELECT 1 FROM media_item_favorite fav WHERE fav.media_item_id = mi.id AND fav.user_id = `+arg(user.ID)+`)`)
	}
	if v, err := strconv.Atoi(q.Get("minRating")); err == nil && v > 0 {
		where = append(where, `mi.rating >= `+arg(int16(v)))
	}
	if q.Get("reels") == "true" {
		where = append(where, reelCond("mi"))
	}
	if q.Get("hasArtwork") == "true" {
		where = append(where, `(mi.poster_image_id IS NOT NULL OR mi.backdrop_image_id IS NOT NULL)`)
	}
	if name := strings.TrimSpace(q.Get("studio")); name != "" {
		where = append(where, `s.name = `+arg(name))
	}
	if search := strings.TrimSpace(q.Get("q")); search != "" {
		hay := `(mi.title || ' ' || COALESCE(s.name, '') || ' ' || COALESCE(se.name, '') || ' ' || COALESCE((
			SELECT string_agg(pf.name || ' ' || COALESCE(pf.display_name, ''), ' ')
			FROM media_item_performer mip JOIN performer pf ON pf.id = mip.performer_id
			WHERE mip.media_item_id = mi.id), ''))`
		where = append(where, `(`+fuzzyMatch(hay, fuzzyTokens(search), arg)+` OR COALESCE(mi.description, '') ILIKE `+arg(likeContains(search))+` ESCAPE '\')`)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	fromIdx := strings.Index(query, "FROM media_item")
	if fromIdx < 0 {
		writeError(w, http.StatusInternalServerError, "failed to count media items")
		return
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), "SELECT COUNT(*) "+query[fromIdx:], args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count media items")
		return
	}

	order := `mi.date_added DESC, mi.id DESC`
	if watchLater {
		order = `f_wl.added_at DESC, mi.id DESC`
	}
	sortBy := q.Get("sort")
	if sortBy == "" && q.Get("seriesId") != "" {
		sortBy = "series" // a series lists its titles in order by default
	}
	switch sortBy {
	case "series":
		order = `mi.season_number ASC NULLS LAST, mi.episode_number ASC NULLS LAST, mi.series_position ASC NULLS LAST, mi.release_date ASC NULLS LAST, LOWER(mi.title) ASC, mi.id ASC`
	case "oldest":
		order = `mi.date_added ASC, mi.id ASC`
	case "title":
		order = `LOWER(mi.title) ASC, mi.id ASC`
	case "random":
		order = `random()`
	case "shuffle":
		// A random order that holds still while it is paged through: the
		// same seed gives the same order.
		order = `md5(mi.id::text || ` + arg(q.Get("seed")) + `::text), mi.id`
	case "watched":
		order = `mi.watch_count DESC, mi.last_watched_at DESC NULLS LAST, mi.id DESC`
	case "rated":
		order = `mi.rating DESC NULLS LAST, mi.rating_count DESC, mi.id DESC`
	}
	query += ` ORDER BY ` + order + ` LIMIT ` + arg(limit) + ` OFFSET ` + arg(offset)

	rows, err := a.DB.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load media items")
		return
	}
	defer rows.Close()

	items := []mediaItem{}
	for rows.Next() {
		m, err := scanMediaItem(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read media item")
			return
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load media items")
		return
	}

	if err := a.attachPerformers(r, items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performers")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset, "total": total})
}

// loadMediaItemDetail loads a media item plus everything a detail view needs
// (cast, categories, tags, files, and the current user's playback/watch-later
// state) that the bare mediaItemSelect row doesn't carry. Every handler that
// hands a mediaItem back to the frontend must go through this, not just
// scanMediaItem — a partial item overwrites the fuller one the page already
// has (e.g. it looked like the file had gone offline after favoriting).
func (a *API) loadMediaItemDetail(r *http.Request, id int64) (mediaItem, error) {
	row := a.DB.QueryRow(r.Context(), mediaItemSelect+` WHERE mi.id = $1`, id)
	m, err := scanMediaItem(row)
	if err != nil {
		return mediaItem{}, err
	}

	items := []mediaItem{m}
	if err := a.attachPerformers(r, items); err != nil {
		return mediaItem{}, err
	}
	m = items[0]
	var reel, detected bool
	if err := a.DB.QueryRow(r.Context(), `SELECT mi.reel_override, `+reelCond("mi")+`, `+reelDetected("mi")+` FROM media_item mi WHERE mi.id = $1`, id).
		Scan(&m.ReelOverride, &reel, &detected); err == nil {
		m.Reel, m.ReelDetected = &reel, &detected
	}
	if crewRows, err := a.DB.Query(r.Context(), `
		SELECT p.id, p.name, p.profile_image_id, c.job FROM media_item_crew c JOIN performer p ON p.id = c.performer_id
		WHERE c.media_item_id = $1 ORDER BY c.job, c.position`, id); err == nil {
		for crewRows.Next() {
			var c mediaCrew
			if crewRows.Scan(&c.ID, &c.Name, &c.ProfileImageID, &c.Job) == nil {
				m.Crew = append(m.Crew, c)
			}
		}
		crewRows.Close()
	}

	if user, ok := UserFromContext(r.Context()); ok {
		var position float64
		var completed bool
		err := a.DB.QueryRow(r.Context(), `
			SELECT position_seconds, completed FROM playback_state WHERE user_id = $1 AND media_item_id = $2
		`, user.ID, id).Scan(&position, &completed)
		if err == nil && !completed && position > 0 {
			m.PositionSeconds = &position
		}
		_ = a.DB.QueryRow(r.Context(), `SELECT rating FROM media_item_rating WHERE user_id = $1 AND media_item_id = $2`, user.ID, id).Scan(&m.MyRating)
		_ = a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM watch_later WHERE user_id = $1 AND media_item_id = $2)`, user.ID, id).Scan(&m.WatchLater)
	}

	catRows, err := a.DB.Query(r.Context(), `
		SELECT c.name FROM media_item_category mic JOIN category c ON c.id = mic.category_id
		WHERE mic.media_item_id = $1 ORDER BY c.name
	`, id)
	if err == nil {
		defer catRows.Close()
		for catRows.Next() {
			var name string
			if catRows.Scan(&name) == nil {
				m.Categories = append(m.Categories, name)
			}
		}
	}

	tagRows, err := a.DB.Query(r.Context(), `
		SELECT t.name FROM media_item_tag mit JOIN tag t ON t.id = mit.tag_id
		WHERE mit.media_item_id = $1 ORDER BY t.name
	`, id)
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var name string
			if tagRows.Scan(&name) == nil {
				m.Tags = append(m.Tags, name)
			}
		}
	}

	fileRows, err := a.DB.Query(r.Context(), `
		SELECT mf.id, sl.name, mf.relative_path, mf.file_name, COALESCE(mf.original_file_name, ''), mf.extension, mf.file_size,
			mf.width, mf.height, mf.frame_rate, mf.video_codec, mf.audio_codec, mf.container,
			mf.video_bitrate, mf.audio_bitrate, mf.overall_bitrate, mf.duration_seconds,
			mf.modified_time, mf.available
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = $1
		ORDER BY mf.available DESC, mf.height DESC NULLS LAST
	`, id)
	if err == nil {
		defer fileRows.Close()
		for fileRows.Next() {
			var f mediaFileSummary
			if fileRows.Scan(&f.ID, &f.StorageLocation, &f.RelativePath, &f.FileName, &f.OriginalFileName, &f.Extension, &f.FileSize,
				&f.Width, &f.Height, &f.FrameRate, &f.VideoCodec, &f.AudioCodec, &f.Container,
				&f.VideoBitrate, &f.AudioBitrate, &f.OverallBitrate, &f.DurationSeconds,
				&f.ModifiedTime, &f.Available) == nil {
				m.Files = append(m.Files, f)
			}
		}
	}

	return m, nil
}

func (a *API) getMediaItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}

	m, err := a.loadMediaItemDetail(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load media item")
		return
	}

	writeJSON(w, http.StatusOK, m)
}

type createMediaItemRequest struct {
	Title       string `json:"title"`
	StudioID    *int64 `json:"studioId"`
	SeriesID    *int64 `json:"seriesId"`
	Description string `json:"description"`
}

func (a *API) createMediaItem(w http.ResponseWriter, r *http.Request) {
	var req createMediaItemRequest
	if err := decodeJSON(r, &req); err != nil || req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO media_item (title, sort_title, studio_id, series_id, description, metadata_source)
		VALUES ($1, $1, $2, $3, NULLIF($4, ''), 'MANUAL')
		RETURNING id
	`, req.Title, req.StudioID, req.SeriesID, req.Description).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create media item")
		return
	}

	row := a.DB.QueryRow(r.Context(), mediaItemSelect+` WHERE mi.id = $1`, id)
	m, err := scanMediaItem(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "created but failed to load media item")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// updateMediaItemRequest is a partial update. Any field set here marks the
// item MANUAL, so a later NFO import will not silently overwrite it (field-
// level provenance is coarse for now — whole-item, not per-field).
type updateMediaItemRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	StudioID    *int64  `json:"studioId"`
	SeriesID    *int64  `json:"seriesId"`
	Favorite    *bool   `json:"favorite"`
	Rating      *int16  `json:"rating"`
	Watched     *bool   `json:"watched"`
}

func (a *API) updateMediaItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req updateMediaItemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Favorite, rating and watched are anyone's; the title's details are shared
	// metadata only admins edit, and only those mark it MANUAL.
	editsMetadata := req.Title != nil || req.Description != nil || req.StudioID != nil || req.SeriesID != nil
	if user, _ := UserFromContext(r.Context()); editsMetadata && user.Role != "ADMIN" {
		writeError(w, http.StatusForbidden, "admin privileges required")
		return
	}

	tag, err := a.DB.Exec(r.Context(), `
		UPDATE media_item SET
			title = COALESCE($2, title),
			sort_title = COALESCE($2, sort_title),
			description = COALESCE($3, description),
			studio_id = COALESCE($4, studio_id),
			series_id = COALESCE($5, series_id),
			watched = COALESCE($6, watched),
			metadata_source = CASE WHEN $7 THEN 'MANUAL' ELSE metadata_source END,
			updated_at = now()
		WHERE id = $1
	`, id, req.Title, req.Description, req.StudioID, req.SeriesID, req.Watched, editsMetadata)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update media item")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "media item not found")
		return
	}

	if req.Favorite != nil {
		user, _ := UserFromContext(r.Context())
		query := `DELETE FROM media_item_favorite WHERE user_id = $1 AND media_item_id = $2`
		if *req.Favorite {
			query = `INSERT INTO media_item_favorite (user_id, media_item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		}
		if _, err := a.DB.Exec(r.Context(), query, user.ID, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save favorite")
			return
		}
	}

	if req.Rating != nil {
		if user, ok := UserFromContext(r.Context()); ok {
			if err := a.setUserRating(r, user.ID, id, *req.Rating); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save rating")
				return
			}
		}
	}

	m, err := a.loadMediaItemDetail(r, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated but failed to load media item")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// setUserRating stores (or, for 0, clears) one user's rating and refreshes the
// item's cached shared average and count.
func (a *API) setUserRating(r *http.Request, userID, itemID int64, rating int16) error {
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if rating <= 0 {
		_, err = tx.Exec(ctx, `DELETE FROM media_item_rating WHERE user_id = $1 AND media_item_id = $2`, userID, itemID)
	} else {
		if rating > 10 {
			rating = 10
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO media_item_rating (user_id, media_item_id, rating) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, media_item_id) DO UPDATE SET rating = EXCLUDED.rating, updated_at = now()
		`, userID, itemID, rating)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE media_item SET
			rating = (SELECT ROUND(AVG(rating))::smallint FROM media_item_rating WHERE media_item_id = $1),
			rating_count = (SELECT COUNT(*) FROM media_item_rating WHERE media_item_id = $1)
		WHERE id = $1
	`, itemID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// streamMediaFile serves a media item's preferred (or explicitly chosen via
// ?fileId=) physical file with Range/206 support via http.ServeFile, which
// never loads the file into memory. It is only reachable behind
// requireAuth — media is never served from a public static root.
func (a *API) streamMediaFile(w http.ResponseWriter, r *http.Request) {
	mediaItemID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}

	src, status, msg := a.resolveMediaFile(r, mediaItemID)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	fullPath, fileName := src.FullPath, src.FileName

	// Files that are already browser-playable but wrapped in a container the
	// browser won't parse (the common case: h264/aac inside an .mkv) get a
	// one-time zero-recode remux instead of falling through to a full HLS
	// transcode, which is dramatically cheaper — a stream copy runs at disk
	// speed, not encode speed.
	//
	// A non-default audio track (?audio=N) always goes through the remux: it
	// is the only way to hand the browser a file whose sound is that track.
	src.Audio = audioFromRequest(r)
	if src.Audio > 0 {
		if !ffmpegAvailable() {
			writeError(w, http.StatusNotImplemented, "ffmpeg is not installed on the server")
			return
		}
		remuxed, err := a.ensureRemux(src)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "cannot switch audio track without transcoding")
			return
		}
		fullPath = remuxed
		fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".mp4"
	} else if r.URL.Query().Get("raw") != "1" && !src.browserDirectPlayable() && src.remuxable() && ffmpegAvailable() {
		// Apps with their own demuxers (ExoPlayer plays .mkv) ask for ?raw=1
		// and skip the remux.
		if remuxed, err := a.ensureRemux(src); err == nil {
			fullPath = remuxed
			fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".mp4"
		}
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "file missing on disk")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to open file")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stat file")
		return
	}

	w.Header().Set("Content-Disposition", `inline; filename="`+fileName+`"`)
	http.ServeContent(a.countStream(w, r, mediaItemID, "direct"), r, fileName, stat.ModTime(), f)
}

func isWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepathHasPrefix(rel, "..")
}

func filepathHasPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}

// relatedMedia returns items similar to the given one, ranked by shared
// performers (weighted higher) and shared categories.
// relatedLimit is how many titles "More like this" shows.
const relatedLimit = 12

// relatedMedia suggests titles to watch after this one: first those sharing
// people, the studio, the series, categories or tags, then — so a title with no
// metadata (an unmatched video) still gets suggestions — titles from the same
// library the person has not finished, newest first.
func (a *API) relatedMedia(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	args := []any{id, relatedLimit}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	scoped := visibleMediaCond(r, "mi", arg)
	items, err := a.queryMediaItems(r, `
		SELECT `+mediaItemSelectColumns+`
		FROM media_item mi
		LEFT JOIN studio s ON s.id = mi.studio_id
		LEFT JOIN series se ON se.id = mi.series_id
		JOIN (
			SELECT media_item_id, SUM(w) AS score FROM (
				SELECT p2.media_item_id, 3 AS w
				FROM media_item_performer p1
				JOIN media_item_performer p2 ON p2.performer_id = p1.performer_id
				WHERE p1.media_item_id = $1
				UNION ALL
				SELECT o.id, 2 FROM media_item src JOIN media_item o ON o.series_id = src.series_id WHERE src.id = $1
				UNION ALL
				SELECT o.id, 2 FROM media_item src JOIN media_item o ON o.studio_id = src.studio_id WHERE src.id = $1
				UNION ALL
				SELECT c2.media_item_id, 1 AS w
				FROM media_item_category c1
				JOIN media_item_category c2 ON c2.category_id = c1.category_id
				WHERE c1.media_item_id = $1
				UNION ALL
				SELECT t2.media_item_id, 1 AS w
				FROM media_item_tag t1
				JOIN media_item_tag t2 ON t2.tag_id = t1.tag_id
				WHERE t1.media_item_id = $1
			) x GROUP BY media_item_id
		) rel ON rel.media_item_id = mi.id
		WHERE mi.id <> $1 AND `+scoped+`
		ORDER BY rel.score DESC, mi.date_added DESC, mi.id DESC
		LIMIT $2`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load related media")
		return
	}

	if len(items) < relatedLimit {
		user, _ := UserFromContext(r.Context())
		seen := []int64{id}
		for _, m := range items {
			seen = append(seen, m.ID)
		}
		args = []any{id, relatedLimit - len(items), seen, user.ID}
		scoped = visibleMediaCond(r, "mi", arg)
		more, err := a.queryMediaItems(r, `
			SELECT `+mediaItemSelectColumns+`
			FROM media_item mi
			LEFT JOIN studio s ON s.id = mi.studio_id
			LEFT JOIN series se ON se.id = mi.series_id
			JOIN media_item src ON src.id = $1
			LEFT JOIN playback_state ps ON ps.user_id = $4 AND ps.media_item_id = mi.id
			WHERE mi.id <> ALL($3) AND mi.library_id IS NOT DISTINCT FROM src.library_id AND `+scoped+`
			ORDER BY COALESCE(ps.completed, FALSE), mi.date_added DESC, mi.id DESC
			LIMIT $2`, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load related media")
			return
		}
		reason := "More from this library"
		for i := range more {
			more[i].Reason = &reason
		}
		items = append(items, more...)
	}
	if err := a.attachPerformers(r, items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// queryMediaItems runs a query selecting mediaItemSelectColumns.
func (a *API) queryMediaItems(r *http.Request, sql string, args ...any) ([]mediaItem, error) {
	rows, err := a.DB.Query(r.Context(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mediaItem{}
	for rows.Next() {
		m, err := scanMediaItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

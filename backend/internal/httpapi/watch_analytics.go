package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

type activityTotals struct {
	Plays          int64   `json:"plays"`
	Sessions       int64   `json:"sessions"`
	Completions    int64   `json:"completions"`
	WatchSeconds   float64 `json:"watchSeconds"`
	UniqueTitles   int64   `json:"uniqueTitles"`
	LibraryTitles  int64   `json:"libraryTitles"`
	AvgSessionSecs float64 `json:"avgSessionSeconds"`
}

type activityDay struct {
	Date         string  `json:"date"`
	Plays        int64   `json:"plays"`
	WatchSeconds float64 `json:"watchSeconds"`
}

type activityEntity struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	ProfileImageID *int64  `json:"profileImageId,omitempty"`
	Plays          int64   `json:"plays"`
	WatchSeconds   float64 `json:"watchSeconds"`
	Titles         int64   `json:"titles"`
}

type activityMedia struct {
	mediaItem
	Plays        int64      `json:"plays"`
	WatchSeconds float64    `json:"watchSeconds"`
	LastPlayedAt *time.Time `json:"lastPlayedAt,omitempty"`
	Completed    bool       `json:"completed,omitempty"`
}

// watchActivity returns the current user's viewing analytics for the last
// `days` days (default 30): totals, daily/hourly trend, most played titles,
// performers, studios and categories, plus recent history.
func (a *API) watchActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := UserFromContext(r.Context())
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 3650 {
		days = v
	}
	since := time.Now().AddDate(0, 0, -days)
	libraryIDs := visibleLibraryIDs(r)
	const inLibrary = `($3::bigint[] IS NULL OR mi.library_id = ANY($3))`
	fail := func() { writeError(w, http.StatusInternalServerError, "failed to load activity") }

	var t activityTotals
	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE `+countedPlay+`),
			COUNT(*),
			COUNT(*) FILTER (WHERE ws.completed),
			COALESCE(SUM(ws.watched_seconds), 0)::float8,
			COUNT(DISTINCT ws.media_item_id) FILTER (WHERE `+countedPlay+`),
			COALESCE(AVG(ws.watched_seconds) FILTER (WHERE `+countedPlay+`), 0)::float8
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND `+inLibrary+`
	`, user.ID, since, libraryIDs).Scan(&t.Plays, &t.Sessions, &t.Completions, &t.WatchSeconds, &t.UniqueTitles, &t.AvgSessionSecs); err != nil {
		fail()
		return
	}
	if err := a.DB.QueryRow(ctx, `SELECT COUNT(*) FROM media_item mi WHERE ($1::bigint[] IS NULL OR mi.library_id = ANY($1))`, libraryIDs).Scan(&t.LibraryTitles); err != nil {
		fail()
		return
	}

	// Daily trend (bounded to 90 buckets).
	trendDays := min(days, 90)
	byDay := []activityDay{}
	dr, err := a.DB.Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'),
			COUNT(ws.id) FILTER (WHERE `+countedPlay+`),
			COALESCE(SUM(ws.watched_seconds), 0)::float8
		FROM generate_series(CURRENT_DATE - ($2::int - 1), CURRENT_DATE, INTERVAL '1 day') AS d(day)
		LEFT JOIN watch_session ws ON ws.user_id = $1 AND ws.started_at::date = d.day::date
			AND EXISTS (SELECT 1 FROM media_item mi WHERE mi.id = ws.media_item_id AND `+inLibrary+`)
		GROUP BY d.day ORDER BY d.day
	`, user.ID, trendDays, libraryIDs)
	if err != nil {
		fail()
		return
	}
	for dr.Next() {
		var d activityDay
		if err := dr.Scan(&d.Date, &d.Plays, &d.WatchSeconds); err != nil {
			dr.Close()
			fail()
			return
		}
		byDay = append(byDay, d)
	}
	dr.Close()

	byHour := make([]int64, 24)
	hr, err := a.DB.Query(ctx, `
		SELECT EXTRACT(HOUR FROM ws.started_at)::int, COUNT(*)
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND `+countedPlay+` AND `+inLibrary+`
		GROUP BY 1`, user.ID, since, libraryIDs)
	if err != nil {
		fail()
		return
	}
	for hr.Next() {
		var h int
		var c int64
		if hr.Scan(&h, &c) == nil && h >= 0 && h < 24 {
			byHour[h] = c
		}
	}
	hr.Close()

	mostPlayed, err := a.activityMedia(r, `
		GROUP BY mi.id, s.name, s.logo_image_id, se.name
		ORDER BY plays DESC, seconds DESC LIMIT 10`, user.ID, since, false)
	if err != nil {
		fail()
		return
	}
	recent, err := a.activityMedia(r, ``, user.ID, since, true)
	if err != nil {
		fail()
		return
	}

	performers, err := a.activityEntities(r, `
		SELECT p.id, p.name, p.profile_image_id, COUNT(*), COALESCE(SUM(ws.watched_seconds), 0)::float8, COUNT(DISTINCT ws.media_item_id)
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		JOIN media_item_performer mip ON mip.media_item_id = ws.media_item_id
		JOIN performer p ON p.id = mip.performer_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND `+countedPlay+` AND `+inLibrary+`
		GROUP BY p.id ORDER BY 4 DESC, 5 DESC LIMIT 10`, user.ID, since, libraryIDs)
	if err != nil {
		fail()
		return
	}
	studios, err := a.activityEntities(r, `
		SELECT s.id, s.name, NULL::bigint, COUNT(*), COALESCE(SUM(ws.watched_seconds), 0)::float8, COUNT(DISTINCT ws.media_item_id)
		FROM watch_session ws
		JOIN media_item mi ON mi.id = ws.media_item_id
		JOIN studio s ON s.id = mi.studio_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND `+countedPlay+` AND `+inLibrary+`
		GROUP BY s.id ORDER BY 4 DESC, 5 DESC LIMIT 10`, user.ID, since, libraryIDs)
	if err != nil {
		fail()
		return
	}
	categories, err := a.activityEntities(r, `
		SELECT c.id, c.name, NULL::bigint, COUNT(*), COALESCE(SUM(ws.watched_seconds), 0)::float8, COUNT(DISTINCT ws.media_item_id)
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		JOIN media_item_category mic ON mic.media_item_id = ws.media_item_id
		JOIN category c ON c.id = mic.category_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND `+countedPlay+` AND `+inLibrary+`
		GROUP BY c.id ORDER BY 4 DESC, 5 DESC LIMIT 10`, user.ID, since, libraryIDs)
	if err != nil {
		fail()
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"days":       days,
		"totals":     t,
		"byDay":      byDay,
		"byHour":     byHour,
		"mostPlayed": mostPlayed,
		"performers": performers,
		"studios":    studios,
		"categories": categories,
		"recent":     recent,
	})
}

// activityMedia lists titles with play stats. recent=true returns the last
// 20 individual sessions (newest first); otherwise per-title aggregates
// with the caller's GROUP BY/ORDER BY tail.
func (a *API) activityMedia(r *http.Request, tail string, userID int64, since time.Time, recent bool) ([]activityMedia, error) {
	var sql string
	const inLibrary = `($3::bigint[] IS NULL OR mi.library_id = ANY($3))`
	if recent {
		sql = `SELECT ` + mediaItemSelectColumns + `, 1::bigint, ws.watched_seconds::float8, ws.started_at, ws.completed
			FROM watch_session ws
			JOIN media_item mi ON mi.id = ws.media_item_id
			LEFT JOIN studio s ON s.id = mi.studio_id
			LEFT JOIN series se ON se.id = mi.series_id
			WHERE ws.user_id = $1 AND ws.started_at >= $2 AND ` + countedPlay + ` AND ` + inLibrary + `
			ORDER BY ws.started_at DESC LIMIT 10`
	} else {
		sql = `SELECT ` + mediaItemSelectColumns + `, COUNT(*) AS plays, COALESCE(SUM(ws.watched_seconds), 0)::float8 AS seconds, MAX(ws.started_at), bool_or(ws.completed)
			FROM watch_session ws
			JOIN media_item mi ON mi.id = ws.media_item_id
			LEFT JOIN studio s ON s.id = mi.studio_id
			LEFT JOIN series se ON se.id = mi.series_id
			WHERE ws.user_id = $1 AND ws.started_at >= $2 AND ` + countedPlay + ` AND ` + inLibrary + ` ` + tail
	}
	rows, err := a.DB.Query(r.Context(), sql, userID, since, visibleLibraryIDs(r))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []activityMedia{}
	for rows.Next() {
		var am activityMedia
		targets := append(mediaItemScanTargets(&am.mediaItem), &am.Plays, &am.WatchSeconds, &am.LastPlayedAt, &am.Completed)
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		out = append(out, am)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]mediaItem, len(out))
	for i := range out {
		items[i] = out[i].mediaItem
	}
	if err := a.attachPerformers(r, items); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].mediaItem = items[i]
	}
	return out, nil
}

func (a *API) activityEntities(r *http.Request, sql string, args ...any) ([]activityEntity, error) {
	rows, err := a.DB.Query(r.Context(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []activityEntity{}
	for rows.Next() {
		var e activityEntity
		if err := rows.Scan(&e.ID, &e.Name, &e.ProfileImageID, &e.Plays, &e.WatchSeconds, &e.Titles); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

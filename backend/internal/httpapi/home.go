package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

// homeSummary is the "library at a glance" strip on Home.
type homeSummary struct {
	Titles                int64   `json:"titles"`
	TotalSeconds          float64 `json:"totalSeconds"`
	AddedThisWeek         int64   `json:"addedThisWeek"`
	AddedThisWeekUnseen   int64   `json:"addedThisWeekUnseen"`
	NeedsDetails          int64   `json:"needsDetails"`
	PlaysThisMonth        int64   `json:"playsThisMonth"`
	WatchSecondsThisMonth float64 `json:"watchSecondsThisMonth"`
	CompletedThisMonth    int64   `json:"completedThisMonth"`
}

// homeSeries is one series the user has started but not finished, with the part to watch next.
type homeSeries struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Kind          *string    `json:"kind,omitempty"`
	PosterImageID *int64     `json:"posterImageId,omitempty"`
	Total         int64      `json:"total"`
	Watched       int64      `json:"watched"`
	Next          *mediaItem `json:"next,omitempty"`
	NextPosition  int64      `json:"nextPosition"`
}

func (a *API) homeSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := UserFromContext(ctx)
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	visible := visibleMediaCond(r, "mi", arg)
	uid := arg(user.ID)
	weekAgo := arg(time.Now().AddDate(0, 0, -7))
	var s homeSummary
	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(mi.duration_seconds), 0),
			COALESCE(SUM(CASE WHEN mi.date_added >= `+weekAgo+` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mi.date_added >= `+weekAgo+` AND mi.watched = FALSE AND NOT EXISTS (
				SELECT 1 FROM playback_state ps WHERE ps.media_item_id = mi.id AND ps.user_id = `+uid+` AND (ps.completed = TRUE OR ps.position_seconds >= 5)
			) THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM match_link ml WHERE ml.kind = 'media' AND ml.entity_id = mi.id) THEN 1 ELSE 0 END), 0)
		FROM media_item mi WHERE `+visible, args...).Scan(&s.Titles, &s.TotalSeconds, &s.AddedThisWeek, &s.AddedThisWeekUnseen, &s.NeedsDetails); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load library summary")
		return
	}
	since := time.Now().AddDate(0, 0, -30)
	if err := a.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN `+countedPlay+` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(ws.watched_seconds), 0),
			COALESCE(SUM(CASE WHEN ws.completed THEN 1 ELSE 0 END), 0)
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND ($3::bigint[] IS NULL OR mi.library_id = ANY($3))
	`, user.ID, since, visibleLibraryIDs(r)).Scan(&s.PlaysThisMonth, &s.WatchSecondsThisMonth, &s.CompletedThisMonth); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load library summary")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// homeSeriesProgress lists series the user is part-way through, most recently touched first.
func (a *API) homeSeriesProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := UserFromContext(ctx)
	limit, _ := parseLimitOffset(r)
	if limit > 12 {
		limit = 12
	}
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	uid := arg(user.ID)
	visible := visibleMediaCond(r, "mi", arg)
	rows, err := a.DB.Query(ctx, `
		WITH parts AS (
			SELECT mi.series_id AS series_id,
				CASE WHEN mi.watched = TRUE OR EXISTS (SELECT 1 FROM playback_state ps WHERE ps.media_item_id = mi.id AND ps.user_id = `+uid+` AND ps.completed = TRUE) THEN 1 ELSE 0 END AS done,
				(SELECT MAX(ps.updated_at) FROM playback_state ps WHERE ps.media_item_id = mi.id AND ps.user_id = `+uid+`) AS touched
			FROM media_item mi
			WHERE mi.series_id IS NOT NULL AND `+visible+`
		)
		SELECT p.series_id, se.name, se.kind, se.poster_image_id, COUNT(*), SUM(p.done)
		FROM parts p JOIN series se ON se.id = p.series_id
		GROUP BY p.series_id, se.name, se.kind, se.poster_image_id
		HAVING SUM(p.done) > 0 AND SUM(p.done) < COUNT(*) AND MAX(p.touched) IS NOT NULL
		ORDER BY MAX(p.touched) DESC
		LIMIT `+arg(limit), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load series progress")
		return
	}
	list := []homeSeries{}
	for rows.Next() {
		var s homeSeries
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.PosterImageID, &s.Total, &s.Watched); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read series progress")
			return
		}
		list = append(list, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load series progress")
		return
	}

	// The next part of each: the first one in series order that isn't finished.
	nexts := []mediaItem{}
	for i := range list {
		nargs := []any{}
		narg := func(v any) string { nargs = append(nargs, v); return "$" + strconv.Itoa(len(nargs)) }
		nuid := narg(user.ID)
		row := a.DB.QueryRow(ctx, mediaItemSelect+`
			WHERE mi.series_id = `+narg(list[i].ID)+` AND `+visibleMediaCond(r, "mi", narg)+`
			  AND mi.watched = FALSE
			  AND NOT EXISTS (SELECT 1 FROM playback_state ps WHERE ps.media_item_id = mi.id AND ps.user_id = `+nuid+` AND ps.completed = TRUE)
			ORDER BY mi.season_number ASC NULLS LAST, mi.episode_number ASC NULLS LAST, mi.series_position ASC NULLS LAST, mi.release_date ASC NULLS LAST, LOWER(mi.title) ASC, mi.id ASC
			LIMIT 1`, nargs...)
		m, err := scanMediaItem(row)
		if err != nil {
			continue
		}
		nexts = append(nexts, m)
		list[i].NextPosition = list[i].Watched + 1
	}
	if err := a.attachPerformers(r, nexts); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load series progress")
		return
	}
	byID := map[int64]*mediaItem{}
	for i := range nexts {
		byID[nexts[i].ID] = &nexts[i]
	}
	out := []homeSeries{}
	for _, s := range list {
		for i := range nexts {
			if nexts[i].SeriesID != nil && *nexts[i].SeriesID == s.ID {
				s.Next = byID[nexts[i].ID]
				break
			}
		}
		if s.Next != nil {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

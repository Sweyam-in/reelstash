package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Usage analytics for admins. Everything here is an aggregate: counts and
// totals across all users, never one person's watch history.

type usageUsers struct {
	Total    int64 `json:"total"`
	Admins   int64 `json:"admins"`
	Disabled int64 `json:"disabled"`
	Active   int64 `json:"active"`
	Today    int64 `json:"activeToday"`
	New      int64 `json:"new"`
}

type usageWatchDay struct {
	Date     string  `json:"date"`
	Sessions int64   `json:"sessions"`
	Hours    float64 `json:"hours"`
	Viewers  int64   `json:"viewers"`
}

type usageStreamDay struct {
	Date   string `json:"date"`
	Direct int64  `json:"directBytes"`
	HLS    int64  `json:"hlsBytes"`
	Peak   int64  `json:"peakStreams"`
}

type usageLibraryDay struct {
	Date  string `json:"date"`
	Items int64  `json:"items"`
	Bytes int64  `json:"bytes"`
}

type usageTop struct {
	Name  string  `json:"name"`
	Plays int64   `json:"plays"`
	Hours float64 `json:"hours"`
}

type usageNow struct {
	Streams int `json:"streams"`
	Direct  int `json:"direct"`
	HLS     int `json:"hls"`
}

type usagePayload struct {
	Days  int        `json:"days"`
	Users usageUsers `json:"users"`
	Watch struct {
		Sessions  int64           `json:"sessions"`
		Hours     float64         `json:"hours"`
		Completed int64           `json:"completed"`
		Daily     []usageWatchDay `json:"daily"`
		ByHour    []int64         `json:"byHour"`
	} `json:"watch"`
	Streaming struct {
		Direct int64            `json:"directBytes"`
		HLS    int64            `json:"hlsBytes"`
		Peak   int64            `json:"peakStreams"`
		Daily  []usageStreamDay `json:"daily"`
		Now    usageNow         `json:"now"`
	} `json:"streaming"`
	Library struct {
		Items      int64             `json:"items"`
		Bytes      int64             `json:"bytes"`
		Hours      float64           `json:"hours"`
		Performers int64             `json:"performers"`
		Studios    int64             `json:"studios"`
		Series     int64             `json:"series"`
		AddedItems int64             `json:"addedItems"`
		AddedBytes int64             `json:"addedBytes"`
		Daily      []usageLibraryDay `json:"daily"`
	} `json:"library"`
	Top struct {
		Titles     []usageTop `json:"titles"`
		Performers []usageTop `json:"performers"`
		Studios    []usageTop `json:"studios"`
	} `json:"top"`
}

func (a *API) adminUsage(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v >= 1 && v <= 365 {
		days = v
	}
	ctx := r.Context()
	p := usagePayload{Days: days}
	fail := func() { writeError(w, http.StatusInternalServerError, "failed to load analytics") }
	since := `now() - make_interval(days => $1)`

	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE role = 'ADMIN'), COUNT(*) FILTER (WHERE disabled),
		       COUNT(*) FILTER (WHERE created_at >= `+since+`),
		       (SELECT COUNT(DISTINCT user_id) FROM watch_session WHERE started_at >= `+since+`),
		       (SELECT COUNT(DISTINCT user_id) FROM watch_session WHERE started_at >= CURRENT_DATE)
		FROM app_user`, days).
		Scan(&p.Users.Total, &p.Users.Admins, &p.Users.Disabled, &p.Users.New, &p.Users.Active, &p.Users.Today); err != nil {
		fail()
		return
	}

	if err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(watched_seconds), 0) / 3600.0, COUNT(*) FILTER (WHERE completed)
		FROM watch_session WHERE started_at >= `+since, days).
		Scan(&p.Watch.Sessions, &p.Watch.Hours, &p.Watch.Completed); err != nil {
		fail()
		return
	}
	var err error
	if p.Watch.Daily, err = queryRows(ctx, a, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COUNT(ws.id), COALESCE(SUM(ws.watched_seconds), 0) / 3600.0, COUNT(DISTINCT ws.user_id)
		FROM generate_series(CURRENT_DATE - ($1 - 1), CURRENT_DATE, INTERVAL '1 day') AS d(day)
		LEFT JOIN watch_session ws ON ws.started_at >= d.day AND ws.started_at < d.day + INTERVAL '1 day'
		GROUP BY d.day ORDER BY d.day`, []any{days}, func(s rowScanner) (usageWatchDay, error) {
		var d usageWatchDay
		return d, s.Scan(&d.Date, &d.Sessions, &d.Hours, &d.Viewers)
	}); err != nil {
		fail()
		return
	}
	// Hours of the day are in the viewer's time zone when the page sends a valid one.
	tz := "UTC"
	if name := r.URL.Query().Get("tz"); name != "" && name != "Local" {
		if loc, err := time.LoadLocation(name); err == nil {
			tz = loc.String()
		}
	}
	p.Watch.ByHour = make([]int64, 24)
	hours, err := queryRows(ctx, a, `
		SELECT EXTRACT(HOUR FROM started_at AT TIME ZONE $2)::int, COUNT(*) FROM watch_session
		WHERE started_at >= `+since+` GROUP BY 1`, []any{days, tz}, func(s rowScanner) ([2]int64, error) {
		var h [2]int64
		return h, s.Scan(&h[0], &h[1])
	})
	if err != nil {
		fail()
		return
	}
	for _, h := range hours {
		if h[0] >= 0 && h[0] < 24 {
			p.Watch.ByHour[h[0]] = h[1]
		}
	}

	streams.flush(a.DB) // include the last minute
	if p.Streaming.Daily, err = queryRows(ctx, a, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COALESCE(sd.direct_bytes, 0), COALESCE(sd.hls_bytes, 0), COALESCE(sd.peak_streams, 0)
		FROM generate_series(CURRENT_DATE - ($1 - 1), CURRENT_DATE, INTERVAL '1 day') AS d(day)
		LEFT JOIN stream_daily sd ON sd.day = d.day::date
		ORDER BY d.day`, []any{days}, func(s rowScanner) (usageStreamDay, error) {
		var d usageStreamDay
		return d, s.Scan(&d.Date, &d.Direct, &d.HLS, &d.Peak)
	}); err != nil {
		fail()
		return
	}
	for _, d := range p.Streaming.Daily {
		p.Streaming.Direct += d.Direct
		p.Streaming.HLS += d.HLS
		p.Streaming.Peak = max(p.Streaming.Peak, d.Peak)
	}
	p.Streaming.Now.Streams, p.Streaming.Now.Direct, p.Streaming.Now.HLS = streams.Now()

	lib := &p.Library
	if err := a.DB.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM media_item),
		       (SELECT COALESCE(SUM(file_size), 0) FROM media_file WHERE available),
		       (SELECT COALESCE(SUM(duration_seconds), 0) / 3600.0 FROM media_item),
		       (SELECT COUNT(*) FROM performer), (SELECT COUNT(*) FROM studio), (SELECT COUNT(*) FROM series)`).
		Scan(&lib.Items, &lib.Bytes, &lib.Hours, &lib.Performers, &lib.Studios, &lib.Series); err != nil {
		fail()
		return
	}
	if lib.Daily, err = queryRows(ctx, a, `
		SELECT to_char(d.day, 'YYYY-MM-DD'),
		       (SELECT COUNT(*) FROM media_item mi WHERE mi.date_added >= d.day AND mi.date_added < d.day + INTERVAL '1 day'),
		       (SELECT COALESCE(SUM(mf.file_size), 0) FROM media_file mf WHERE mf.discovered_at >= d.day AND mf.discovered_at < d.day + INTERVAL '1 day')
		FROM generate_series(CURRENT_DATE - ($1 - 1), CURRENT_DATE, INTERVAL '1 day') AS d(day)
		ORDER BY d.day`, []any{days}, func(s rowScanner) (usageLibraryDay, error) {
		var d usageLibraryDay
		return d, s.Scan(&d.Date, &d.Items, &d.Bytes)
	}); err != nil {
		fail()
		return
	}
	for _, d := range lib.Daily {
		lib.AddedItems += d.Items
		lib.AddedBytes += d.Bytes
	}

	top := func(nameExpr, join, group string) ([]usageTop, error) {
		return queryRows(ctx, a, `
			SELECT `+nameExpr+`, COUNT(*), COALESCE(SUM(ws.watched_seconds), 0) / 3600.0
			FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id `+join+`
			WHERE ws.started_at >= `+since+`
			GROUP BY `+group+` ORDER BY 3 DESC, 2 DESC LIMIT 8`, []any{days}, func(s rowScanner) (usageTop, error) {
			var t usageTop
			return t, s.Scan(&t.Name, &t.Plays, &t.Hours)
		})
	}
	if p.Top.Titles, err = top(`mi.title`, ``, `mi.id, mi.title`); err != nil {
		fail()
		return
	}
	if p.Top.Performers, err = top(`p.name`, `JOIN media_item_performer mip ON mip.media_item_id = mi.id JOIN performer p ON p.id = mip.performer_id`, `p.id, p.name`); err != nil {
		fail()
		return
	}
	if p.Top.Studios, err = top(`s.name`, `JOIN studio s ON s.id = mi.studio_id`, `s.id, s.name`); err != nil {
		fail()
		return
	}

	writeJSON(w, http.StatusOK, p)
}

type rowScanner interface{ Scan(dest ...any) error }

// queryRows runs a query and maps each row with scan.
func queryRows[T any](ctx context.Context, a *API, sql string, args []any, scan func(rowScanner) (T, error)) ([]T, error) {
	rows, err := a.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

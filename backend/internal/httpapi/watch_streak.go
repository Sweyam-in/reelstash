package httpapi

import (
	"context"
	"time"
)

type activityStreak struct {
	Current int `json:"current"`
	Longest int `json:"longest"`
}

type activityPrevious struct {
	Plays        int64   `json:"plays"`
	WatchSeconds float64 `json:"watchSeconds"`
}

// activityStreakFor counts consecutive days with at least one counted play: the run that reaches
// today (or yesterday, so a streak survives until the day is over) and the longest in the last year.
func (a *API) activityStreakFor(ctx context.Context, userID int64, libraryIDs []int64, now time.Time) (activityStreak, error) {
	rows, err := a.DB.Query(ctx, `
		SELECT DISTINCT to_char(ws.started_at, 'YYYY-MM-DD')
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND `+countedPlay+`
		  AND ($3::bigint[] IS NULL OR mi.library_id = ANY($3))
	`, userID, now.AddDate(-1, 0, -1), libraryIDs)
	if err != nil {
		return activityStreak{}, err
	}
	defer rows.Close()
	played := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return activityStreak{}, err
		}
		played[day] = true
	}
	if err := rows.Err(); err != nil {
		return activityStreak{}, err
	}
	return streakFromDays(played, now), nil
}

// streakFromDays is the pure part of activityStreakFor (days as YYYY-MM-DD).
func streakFromDays(played map[string]bool, now time.Time) activityStreak {
	key := func(t time.Time) string { return t.Format("2006-01-02") }
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var s activityStreak
	start := today
	if !played[key(start)] {
		start = start.AddDate(0, 0, -1)
	}
	for d := start; played[key(d)]; d = d.AddDate(0, 0, -1) {
		s.Current++
	}
	run := 0
	for d := today.AddDate(-1, 0, 0); !d.After(today); d = d.AddDate(0, 0, 1) {
		if played[key(d)] {
			run++
			if run > s.Longest {
				s.Longest = run
			}
		} else {
			run = 0
		}
	}
	return s
}

// activityPreviousFor totals the period just before the one shown, for "vs previous" deltas.
func (a *API) activityPreviousFor(ctx context.Context, userID int64, libraryIDs []int64, since time.Time, days int) (activityPrevious, error) {
	var p activityPrevious
	err := a.DB.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(ws.watched_seconds), 0)
		FROM watch_session ws JOIN media_item mi ON mi.id = ws.media_item_id
		WHERE ws.user_id = $1 AND ws.started_at >= $2 AND ws.started_at < $3 AND `+countedPlay+`
		  AND ($4::bigint[] IS NULL OR mi.library_id = ANY($4))
	`, userID, since.AddDate(0, 0, -days), since, libraryIDs).Scan(&p.Plays, &p.WatchSeconds)
	return p, err
}

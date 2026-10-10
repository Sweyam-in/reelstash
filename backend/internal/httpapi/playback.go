package httpapi

import (
	"net/http"
	"strconv"
)

// listContinueWatching returns the current user's in-progress titles
// (started but not completed), most recently played first, so the
// homepage can offer a "resume" row with real playback position.
func (a *API) listContinueWatching(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	limit, _ := parseLimitOffset(r)

	args := []any{user.ID, limit}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	rows, err := a.DB.Query(r.Context(), `
		SELECT ps.position_seconds, `+mediaItemSelectColumns+`
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.media_item_id
		LEFT JOIN studio s ON s.id = mi.studio_id
		LEFT JOIN series se ON se.id = mi.series_id
		WHERE ps.user_id = $1 AND ps.completed = FALSE AND ps.position_seconds >= 5 AND `+visibleMediaCond(r, "mi", arg)+`
		  AND (mi.duration_seconds IS NULL OR ps.position_seconds < mi.duration_seconds * 0.95)
		ORDER BY ps.updated_at DESC
		LIMIT $2
	`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load continue watching")
		return
	}
	defer rows.Close()

	items := []mediaItem{}
	for rows.Next() {
		var m mediaItem
		var position float64
		targets := append([]any{&position}, mediaItemScanTargets(&m)...)
		if err := rows.Scan(targets...); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read continue watching item")
			return
		}
		m.PositionSeconds = &position
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load continue watching")
		return
	}

	if err := a.attachPerformers(r, items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performers")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type updatePlaybackStateRequest struct {
	PositionSeconds float64 `json:"positionSeconds"`
	Completed       bool    `json:"completed"`
}

func (a *API) updatePlaybackState(w http.ResponseWriter, r *http.Request) {
	mediaItemID, err := pathInt64(r, "mediaItemID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	user, _ := UserFromContext(r.Context())

	var req updatePlaybackStateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	_, err = a.DB.Exec(ctx, `
		INSERT INTO playback_state (user_id, media_item_id, position_seconds, completed, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id, media_item_id) DO UPDATE SET
			position_seconds = EXCLUDED.position_seconds,
			completed = EXCLUDED.completed,
			updated_at = now()
	`, user.ID, mediaItemID, req.PositionSeconds, req.Completed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save playback state")
		return
	}

	// watch_count is incremented once per completed watch session
	// (see updateWatchSession); this endpoint only stores resume position.
	if req.Completed {
		a.DB.Exec(ctx, `UPDATE media_item SET watched = TRUE, last_watched_at = now() WHERE id = $1`, mediaItemID)
		// Finishing a title takes it off this user's Watch later list.
		a.DB.Exec(ctx, `DELETE FROM watch_later WHERE user_id = $1 AND media_item_id = $2`, user.ID, mediaItemID)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// seriesOrder is how the titles of a series run: season and episode, then the
// part number, then release date. The same order the series page uses.
const seriesOrder = `mi.season_number ASC NULLS LAST, mi.episode_number ASC NULLS LAST, mi.series_position ASC NULLS LAST, mi.release_date ASC NULLS LAST, LOWER(mi.title) ASC, mi.id ASC`

// listNextUp returns, for every series the current user has been watching,
// the title that follows the furthest one they finished, most recently
// watched series first. A title already under way is left out: it is on the
// Continue watching row. Series with nothing left to watch do not appear.
func (a *API) listNextUp(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	limit, _ := parseLimitOffset(r)

	args := []any{user.ID, limit}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	rows, err := a.DB.Query(r.Context(), `
		WITH ordered AS (
			SELECT mi.id, mi.series_id, row_number() OVER (PARTITION BY mi.series_id ORDER BY `+seriesOrder+`) AS ord
			FROM media_item mi
			WHERE mi.series_id IS NOT NULL AND `+visibleMediaCond(r, "mi", arg)+`
			  AND EXISTS (SELECT 1 FROM media_file mf WHERE mf.media_item_id = mi.id AND mf.available)
		), last_done AS (
			SELECT o.series_id, MAX(o.ord) AS ord, MAX(ps.updated_at) AS at
			FROM ordered o
			JOIN playback_state ps ON ps.media_item_id = o.id AND ps.user_id = $1 AND ps.completed
			GROUP BY o.series_id
		)
		SELECT `+mediaItemSelectColumns+`
		FROM last_done ld
		JOIN ordered o ON o.series_id = ld.series_id AND o.ord = ld.ord + 1
		JOIN media_item mi ON mi.id = o.id
		LEFT JOIN studio s ON s.id = mi.studio_id
		LEFT JOIN series se ON se.id = mi.series_id
		WHERE NOT EXISTS (
			SELECT 1 FROM playback_state p2
			WHERE p2.user_id = $1 AND p2.media_item_id = mi.id AND p2.position_seconds > 0 AND NOT p2.completed)
		ORDER BY ld.at DESC
		LIMIT $2
	`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load next up")
		return
	}
	defer rows.Close()

	items := []mediaItem{}
	for rows.Next() {
		var m mediaItem
		if err := rows.Scan(mediaItemScanTargets(&m)...); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read next up item")
			return
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load next up")
		return
	}
	if err := a.attachPerformers(r, items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load performers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// nextInSeries answers with the title that follows {id} in its series (the
// next episode, or the next film of a collection), or null at the end of the
// series or for a title that is in none.
func (a *API) nextInSeries(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	args := []any{id}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	var nextID *int64
	err = a.DB.QueryRow(r.Context(), `
		SELECT next_id FROM (
			SELECT mi.id, lead(mi.id) OVER (ORDER BY `+seriesOrder+`) AS next_id
			FROM media_item mi
			WHERE mi.series_id = (SELECT series_id FROM media_item WHERE id = $1) AND `+visibleMediaCond(r, "mi", arg)+`
			  AND (mi.id = $1 OR EXISTS (SELECT 1 FROM media_file mf WHERE mf.media_item_id = mi.id AND mf.available))
		) t WHERE id = $1
	`, args...).Scan(&nextID)
	if err != nil || nextID == nil {
		writeJSON(w, http.StatusOK, map[string]any{"item": nil})
		return
	}
	m, err := scanMediaItem(a.DB.QueryRow(r.Context(), mediaItemSelect+` WHERE mi.id = $1`, *nextID))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"item": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": m})
}

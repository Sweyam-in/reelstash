package httpapi

import (
	"net/http"
)

// A session counts as a real "play" once it has ≥10s watched or completed.
const countedPlay = `(ws.watched_seconds >= 10 OR ws.completed)`

// startWatchSession opens a viewing session when playback begins.
func (a *API) startWatchSession(w http.ResponseWriter, r *http.Request) {
	mediaItemID, err := pathInt64(r, "mediaItemID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	user, _ := UserFromContext(r.Context())

	var id int64
	err = a.DB.QueryRow(r.Context(), `
		INSERT INTO watch_session (user_id, media_item_id, duration_seconds)
		SELECT $1, mi.id, mi.duration_seconds FROM media_item mi WHERE mi.id = $2
		RETURNING id
	`, user.ID, mediaItemID).Scan(&id)
	if err != nil {
		writeError(w, http.StatusNotFound, "media item not found")
		return
	}
	invalidateRecommendations(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": id})
}

type updateWatchSessionRequest struct {
	PositionSeconds float64 `json:"positionSeconds"`
	WatchedSeconds  float64 `json:"watchedSeconds"` // cumulative for this session
	DurationSeconds float64 `json:"durationSeconds"`
	Completed       bool    `json:"completed"`
}

// updateWatchSession records heartbeat/final progress. Watched time only
// ever grows; completion (explicit, or ≥90% reached) bumps the item's
// watch_count exactly once per session.
func (a *API) updateWatchSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := pathInt64(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	user, _ := UserFromContext(r.Context())

	var req updateWatchSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WatchedSeconds < 0 || req.PositionSeconds < 0 {
		writeError(w, http.StatusBadRequest, "invalid progress")
		return
	}

	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save watch session")
		return
	}
	defer tx.Rollback(ctx)

	var mediaItemID int64
	var wasCompleted bool
	var duration *float64
	err = tx.QueryRow(ctx, `
		SELECT media_item_id, completed, duration_seconds FROM watch_session
		WHERE id = $1 AND user_id = $2 FOR UPDATE
	`, sessionID, user.ID).Scan(&mediaItemID, &wasCompleted, &duration)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if req.DurationSeconds > 0 {
		duration = &req.DurationSeconds
	}
	completed := wasCompleted || req.Completed ||
		(duration != nil && *duration > 0 && req.PositionSeconds >= *duration*0.9)

	if _, err = tx.Exec(ctx, `
		UPDATE watch_session SET
			watched_seconds = GREATEST(watched_seconds, $2),
			max_position_seconds = GREATEST(max_position_seconds, $3),
			duration_seconds = COALESCE($4, duration_seconds),
			completed = $5,
			last_activity_at = now()
		WHERE id = $1
	`, sessionID, req.WatchedSeconds, req.PositionSeconds, duration, completed); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save watch session")
		return
	}
	if completed && !wasCompleted {
		if _, err = tx.Exec(ctx, `
			UPDATE media_item SET watched = TRUE, watch_count = watch_count + 1, last_watched_at = now()
			WHERE id = $1
		`, mediaItemID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save watch session")
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save watch session")
		return
	}
	if completed && !wasCompleted {
		invalidateRecommendations(user.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "completed": completed})
}

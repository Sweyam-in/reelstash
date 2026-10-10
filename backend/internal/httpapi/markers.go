package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxMarkerTitleRunes = 120
	maxMarkersPerTitle  = 500
	maxMarkerSeconds    = 1e6
)

// Markers are private to the person who made them, like watch progress.
type marker struct {
	ID        int64     `json:"id"`
	Seconds   float64   `json:"seconds"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// cleanMarkerTitle trims a title, falls back to "Marker", and caps its length.
func cleanMarkerTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Marker"
	}
	if utf8.RuneCountInString(title) > maxMarkerTitleRunes {
		title = string([]rune(title)[:maxMarkerTitleRunes])
	}
	return title
}

func validMarkerSeconds(s float64) bool {
	return !math.IsNaN(s) && !math.IsInf(s, 0) && s >= 0 && s <= maxMarkerSeconds
}

func (a *API) listMarkers(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	user, _ := UserFromContext(r.Context())
	rows, err := a.DB.Query(r.Context(), `
		SELECT id, seconds, title, created_at FROM media_marker
		WHERE media_item_id = $1 AND user_id = $2 ORDER BY seconds, id
	`, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load markers")
		return
	}
	defer rows.Close()
	items := []marker{}
	for rows.Next() {
		var m marker
		if err := rows.Scan(&m.ID, &m.Seconds, &m.Title, &m.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read marker")
			return
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load markers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createMarker(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	user, _ := UserFromContext(r.Context())
	var req struct {
		Seconds float64 `json:"seconds"`
		Title   string  `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil || !validMarkerSeconds(req.Seconds) {
		writeError(w, http.StatusBadRequest, "a valid position in the video is required")
		return
	}
	var count int
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM media_marker WHERE media_item_id = $1 AND user_id = $2`, id, user.ID).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save marker")
		return
	}
	if count >= maxMarkersPerTitle {
		writeError(w, http.StatusConflict, "this title already has the maximum number of markers")
		return
	}
	var m marker
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO media_marker (media_item_id, user_id, seconds, title) VALUES ($1, $2, $3, $4)
		RETURNING id, seconds, title, created_at
	`, id, user.ID, req.Seconds, cleanMarkerTitle(req.Title)).Scan(&m.ID, &m.Seconds, &m.Title, &m.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save marker")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (a *API) updateMarker(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	markerID, err := pathInt64(r, "markerId")
	if err != nil || markerID < 1 {
		writeError(w, http.StatusBadRequest, "invalid marker id")
		return
	}
	user, _ := UserFromContext(r.Context())
	var req struct {
		Seconds *float64 `json:"seconds"`
		Title   *string  `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Seconds == nil && req.Title == nil) {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}
	if req.Seconds != nil && !validMarkerSeconds(*req.Seconds) {
		writeError(w, http.StatusBadRequest, "a valid position in the video is required")
		return
	}
	var title *string
	if req.Title != nil {
		cleaned := cleanMarkerTitle(*req.Title)
		title = &cleaned
	}
	var m marker
	err = a.DB.QueryRow(r.Context(), `
		UPDATE media_marker SET seconds = COALESCE($4, seconds), title = COALESCE($5, title)
		WHERE id = $1 AND media_item_id = $2 AND user_id = $3
		RETURNING id, seconds, title, created_at
	`, markerID, id, user.ID, req.Seconds, title).Scan(&m.ID, &m.Seconds, &m.Title, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "marker not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update marker")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) deleteMarker(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	markerID, err := pathInt64(r, "markerId")
	if err != nil || markerID < 1 {
		writeError(w, http.StatusBadRequest, "invalid marker id")
		return
	}
	user, _ := UserFromContext(r.Context())
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM media_marker WHERE id = $1 AND media_item_id = $2 AND user_id = $3`, markerID, id, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete marker")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "marker not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

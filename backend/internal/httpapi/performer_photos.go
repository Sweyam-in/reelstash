package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"reelstash/backend/internal/namer"
)

func (a *API) servePerformerImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	var key, mime string
	query := `
		SELECT ar.storage_key, ar.mime_type
		FROM performer p
		JOIN artwork ar ON ar.id = p.profile_image_id
		WHERE p.id = $1 AND ar.kind = 'PROFILE'`
	args := []any{id}
	if photoID := chi.URLParam(r, "photoId"); photoID != "" { // one gallery photo
		query = `
			SELECT ar.storage_key, ar.mime_type
			FROM performer_photo pp JOIN artwork ar ON ar.id = pp.artwork_id
			WHERE pp.performer_id = $1 AND pp.artwork_id = $2`
		args = append(args, photoID)
	}
	err = a.DB.QueryRow(r.Context(), query, args...).Scan(&key, &mime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load performer photo", http.StatusInternalServerError)
		return
	}
	if key == "" || filepath.Base(key) != key {
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

type performerPhoto struct {
	ID        int64     `json:"id"`
	Source    string    `json:"source"`
	Primary   bool      `json:"primary"`
	CreatedAt time.Time `json:"createdAt"`
}

// listPerformerPhotos is the performer's gallery, displayed photo first.
func (a *API) listPerformerPhotos(w http.ResponseWriter, r *http.Request) {
	id, ok := performerIDParam(w, r)
	if !ok {
		return
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT pp.artwork_id, pp.source, pp.artwork_id = p.profile_image_id, pp.created_at
		FROM performer_photo pp JOIN performer p ON p.id = pp.performer_id
		WHERE pp.performer_id = $1
		ORDER BY pp.artwork_id = p.profile_image_id DESC, pp.created_at
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load photos")
		return
	}
	defer rows.Close()
	items := []performerPhoto{}
	for rows.Next() {
		var ph performerPhoto
		if err := rows.Scan(&ph.ID, &ph.Source, &ph.Primary, &ph.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load photos")
			return
		}
		items = append(items, ph)
	}
	var chosen bool
	_ = a.DB.QueryRow(r.Context(), `SELECT photo_chosen FROM performer WHERE id = $1`, id).Scan(&chosen)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "chosen": chosen})
}

// choosePerformerPhoto makes a gallery photo the displayed one. Automatic
// matches will not switch away from it.
func (a *API) choosePerformerPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := performerIDParam(w, r)
	if !ok {
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE performer SET profile_image_id = $2, photo_chosen = true, updated_at = now()
		WHERE id = $1 AND EXISTS (SELECT 1 FROM performer_photo WHERE performer_id = $1 AND artwork_id = $2)
	`, id, chi.URLParam(r, "photoId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid photo")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "profileImageId": chi.URLParam(r, "photoId")})
}

func (a *API) deletePerformerPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := performerIDParam(w, r)
	if !ok {
		return
	}
	photoID, err := pathInt64(r, "photoId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid photo id")
		return
	}
	n, err := a.removePerformerPhotos(r.Context(), id, `pp.artwork_id = $2`, photoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove photo")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fetchPerformerPhotos adds each linked provider's photo to the gallery.
func (a *API) fetchPerformerPhotos(w http.ResponseWriter, r *http.Request) {
	id, ok := performerIDParam(w, r)
	if !ok {
		return
	}
	if a.Namer == nil {
		writeError(w, http.StatusServiceUnavailable, "no metadata provider is configured")
		return
	}
	added, err := a.Namer.FetchPerformerPhotos(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

func performerIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid performer id")
		return 0, false
	}
	return id, true
}

// removePerformerPhotos deletes the performer's gallery photos matching where
// (a condition on pp, with extra args from $2), files included. When the
// displayed photo goes, the oldest remaining one takes its place.
func (a *API) removePerformerPhotos(ctx context.Context, performerID int64, where string, args ...any) (int, error) {
	rows, err := a.DB.Query(ctx, `
		SELECT pp.artwork_id, ar.storage_key FROM performer_photo pp JOIN artwork ar ON ar.id = pp.artwork_id
		WHERE pp.performer_id = $1 AND `+where, append([]any{performerID}, args...)...)
	if err != nil {
		return 0, err
	}
	type photo struct {
		id  int64
		key string
	}
	var photos []photo
	for rows.Next() {
		var p photo
		if rows.Scan(&p.id, &p.key) == nil {
			photos = append(photos, p)
		}
	}
	rows.Close()
	if len(photos) == 0 {
		return 0, nil
	}
	ids := make([]int64, len(photos))
	for i, p := range photos {
		ids[i] = p.id
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var wasShown bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(profile_image_id = ANY($2), false) FROM performer WHERE id = $1 FOR UPDATE`, performerID, ids).Scan(&wasShown); err != nil {
		return 0, err
	}
	if wasShown {
		if _, err := tx.Exec(ctx, `
			UPDATE performer SET photo_chosen = false, updated_at = now(), profile_image_id = (
				SELECT artwork_id FROM performer_photo WHERE performer_id = $1 AND NOT (artwork_id = ANY($2))
				ORDER BY created_at LIMIT 1)
			WHERE id = $1
		`, performerID, ids); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM artwork WHERE id = ANY($1)`, ids); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	for _, p := range photos {
		if p.key != "" && filepath.Base(p.key) == p.key {
			if err := os.Remove(filepath.Join(a.Cfg.PosterDir, p.key)); err != nil && !os.IsNotExist(err) {
				log.Printf("remove performer photo file %s: %v", p.key, err)
			}
		}
	}
	return len(photos), nil
}

// performerFieldSources shows each linked provider's value for every detail,
// and which provider the user picked for each.
func (a *API) performerFieldSources(w http.ResponseWriter, r *http.Request) {
	id, ok := performerIDParam(w, r)
	if !ok {
		return
	}
	links, sources, err := namer.PerformerLinks(r.Context(), a.DB, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "performer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider details")
		return
	}
	values := map[string]map[string]string{}
	order := []string{}
	for _, l := range links {
		values[l.Provider] = l.Values
		order = append(order, l.Provider)
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": namer.PerformerFields, "providers": order, "values": values, "sources": sources})
}

func (a *API) setPerformerFieldSources(w http.ResponseWriter, r *http.Request) {
	id, ok := performerIDParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Sources map[string]string `json:"sources"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := namer.SetPerformerFieldSources(r.Context(), a.DB, id, req.Sources); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "performer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	a.performerFieldSources(w, r)
}

package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"reelstash/backend/internal/scanner"
)

type studio struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	LogoImageID *int64          `json:"logoImageId,omitempty"`
	Description *string         `json:"description,omitempty"`
	Website     *string         `json:"website,omitempty"`
	MediaCount  int64           `json:"mediaCount"`
	MatchStatus string          `json:"matchStatus,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	Links       json.RawMessage `json:"links,omitempty"`
}

func (a *API) listStudios(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	var conds []string
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if search != "" {
		conds = append(conds, `s.name ILIKE `+arg(likeContains(search))+` ESCAPE '\'`)
	}
	// Users see studios with a title in their libraries, counted there.
	mediaJoin := `LEFT JOIN media_item mi ON mi.studio_id = s.id`
	if f := libraryFilter(r, "mi.library_id", arg); f != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM media_item mi WHERE mi.studio_id = s.id AND `+f+`)`)
		mediaJoin = `LEFT JOIN media_item mi ON mi.studio_id = s.id AND ` + f
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM studio s `+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count studios")
		return
	}
	args = append(args, limit, offset)
	limitArg := len(args) - 1
	rows, err := a.DB.Query(r.Context(), `
		SELECT s.id, s.name, s.logo_image_id, s.description, s.website, s.match_status,
		       COUNT(mi.id) AS media_count
		FROM studio s
		`+mediaJoin+`
		`+where+`
		GROUP BY s.id
		ORDER BY s.name
		LIMIT $`+strconv.Itoa(limitArg)+` OFFSET $`+strconv.Itoa(limitArg+1)+`
	`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load studios")
		return
	}
	defer rows.Close()

	items := []studio{}
	for rows.Next() {
		var s studio
		if err := rows.Scan(&s.ID, &s.Name, &s.LogoImageID, &s.Description, &s.Website, &s.MatchStatus, &s.MediaCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read studio")
			return
		}
		items = append(items, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset, "total": total})
}

type createStudioRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Website     string `json:"website"`
}

func (a *API) createStudio(w http.ResponseWriter, r *http.Request) {
	var req createStudioRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO studio (name, description, website) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
		RETURNING id
	`, req.Name, req.Description, req.Website).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create studio")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) serveStudioImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	var key, mime string
	err = a.DB.QueryRow(r.Context(), `
		SELECT ar.storage_key, ar.mime_type
		FROM studio s
		JOIN artwork ar ON ar.id = s.logo_image_id
		WHERE s.id = $1 AND ar.kind = 'LOGO'
	`, id).Scan(&key, &mime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load studio image", http.StatusInternalServerError)
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

func (a *API) getStudio(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}

	if !a.entityAccessible(r, studioMediaExists, id) {
		writeError(w, http.StatusNotFound, "studio not found")
		return
	}
	var s studio
	var metadata []byte
	args := []any{id}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	row := a.DB.QueryRow(r.Context(), `
		SELECT s.id, s.name, s.logo_image_id, s.description, s.website, s.match_status, s.remote_metadata,
		       COUNT(mi.id) AS media_count
		FROM studio s
		LEFT JOIN media_item mi ON mi.studio_id = s.id AND `+visibleMediaCond(r, "mi", arg)+`
		WHERE s.id = $1
		GROUP BY s.id
	`, args...)
	if err := row.Scan(&s.ID, &s.Name, &s.LogoImageID, &s.Description, &s.Website, &s.MatchStatus, &metadata, &s.MediaCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "studio not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load studio")
		return
	}
	if len(metadata) > 0 && string(metadata) != "null" {
		s.Metadata = metadata
	}
	s.Links = a.loadLinks(r.Context(), "studio", id)

	writeJSON(w, http.StatusOK, s)
}

type updateStudioRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Website     string `json:"website"`
}

func (a *API) updateStudio(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	var req updateStudioRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	var oldName string
	if err := a.DB.QueryRow(r.Context(), `SELECT name FROM studio WHERE id = $1`, id).Scan(&oldName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "studio not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load studio")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `
		UPDATE studio SET name = $2, description = NULLIF($3, ''), website = NULLIF($4, ''), updated_at = now()
		WHERE id = $1
	`, id, req.Name, req.Description, req.Website); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "another studio already has that name")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update studio")
		return
	}
	resp := map[string]any{"id": id}
	if oldName != req.Name {
		// Keep the NFO sidecars in step, otherwise the next scan would bring the old name back.
		updated, failed := 0, 0
		if rows, err := a.DB.Query(r.Context(), `SELECT id FROM media_item WHERE studio_id = $1`, id); err == nil {
			var itemIDs []int64
			for rows.Next() {
				var itemID int64
				if rows.Scan(&itemID) == nil {
					itemIDs = append(itemIDs, itemID)
				}
			}
			rows.Close()
			for _, itemID := range itemIDs {
				u, f := a.writeItemNFOs(r.Context(), itemID, scanner.NFOFields{Studio: &req.Name}, false)
				updated += u
				failed += f
			}
		}
		resp["nfoUpdated"] = updated
		resp["nfoFailed"] = failed
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) deleteStudio(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM studio WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete studio")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "studio not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) uploadStudioImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid studio id")
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
	if err := a.Namer.SetStudioLogoUpload(r.Context(), id, body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "studio not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var imageID int64
	_ = a.DB.QueryRow(r.Context(), `SELECT logo_image_id FROM studio WHERE id = $1`, id).Scan(&imageID)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "logoImageId": imageID})
}

func (a *API) deleteStudioImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid studio id")
		return
	}
	var imageID *int64
	var key string
	if err := a.DB.QueryRow(r.Context(), `
		SELECT s.logo_image_id, COALESCE(ar.storage_key, '')
		FROM studio s LEFT JOIN artwork ar ON ar.id = s.logo_image_id WHERE s.id = $1
	`, id).Scan(&imageID, &key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "studio not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load studio")
		return
	}
	if imageID != nil {
		if _, err := a.DB.Exec(r.Context(), `UPDATE studio SET logo_image_id = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove logo")
			return
		}
		a.DB.Exec(r.Context(), `DELETE FROM artwork WHERE id = $1`, *imageID)
		if key != "" && filepath.Base(key) == key {
			os.Remove(filepath.Join(a.Cfg.PosterDir, key))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

package httpapi

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
)

// primaryMediaFileID is the file a provider match is applied to: the best
// available one, the same pick the player streams.
func (a *API) primaryMediaFileID(r *http.Request, mediaItemID int64) (int64, bool) {
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		SELECT id FROM media_file WHERE media_item_id = $1 AND available
		ORDER BY height DESC NULLS LAST, file_size DESC LIMIT 1
	`, mediaItemID).Scan(&id)
	return id, err == nil
}

// splitCSV splits a comma-separated query value, dropping blanks.
func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (a *API) namerReady(w http.ResponseWriter) bool {
	if a.Namer == nil || !a.Namer.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "no metadata provider is configured (add an API key under Metadata → Providers)")
		return false
	}
	return true
}

// searchMediaMatches lists provider records that could match this title.
func (a *API) searchMediaMatches(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	if !a.namerReady(w) {
		return
	}
	fileID, ok := a.primaryMediaFileID(r, id)
	if !ok {
		writeError(w, http.StatusConflict, "this title has no available file to match")
		return
	}
	searchType := r.URL.Query().Get("type")
	if searchType == "" {
		searchType = "all"
	}
	if searchType != "all" && searchType != "scenes" && searchType != "movies" && searchType != "shows" && searchType != "jav" {
		writeError(w, http.StatusBadRequest, "invalid search type")
		return
	}
	query, candidates, err := a.Namer.Search(r.Context(), fileID, r.URL.Query().Get("q"), splitCSV(r.URL.Query().Get("providers")), searchType)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": query, "items": candidates})
}

// applyMediaMatch applies a chosen provider scene: the file is renamed, its NFO
// rewritten, and the title's metadata, performers and categories replaced.
func (a *API) applyMediaMatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req struct {
		Provider   string `json:"provider"`
		ExternalID string `json:"externalId"`
		UUID       string `json:"uuid"` // legacy alias for externalId
		// Merge keeps what the title already has and only fills what it lacks.
		Merge bool `json:"merge"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.ExternalID == "" {
		req.ExternalID = req.UUID
	}
	if req.Provider == "" || req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "provider and externalId are required")
		return
	}
	if !a.namerReady(w) {
		return
	}
	fileID, ok := a.primaryMediaFileID(r, id)
	if !ok {
		writeError(w, http.StatusConflict, "this title has no available file to match")
		return
	}
	applied, err := a.Namer.ApplyReplacing(r.Context(), fileID, req.Provider, req.ExternalID, req.Merge)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, applied)
}

// linkMediaProvider links a title to a provider record without changing the
// file, its NFO or the title's details.
func (a *API) linkMediaProvider(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req matchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !a.namerReady(w) {
		return
	}
	providerID, externalID, ok := req.resolve(w, a, provider.KindMedia)
	if !ok {
		return
	}
	var exists bool
	if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM media_item WHERE id = $1)`, id).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "title not found")
		return
	}
	if err := a.Namer.LinkOnly(r.Context(), id, providerID, externalID); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "provider": providerID})
}

type performerRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// updateMediaMetadataRequest is a partial edit: a nil field is left alone.
type updateMediaMetadataRequest struct {
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Studio      *string         `json:"studio"` // "" clears the studio
	Categories  *[]string       `json:"categories"`
	Performers  *[]performerRef `json:"performers"`
}

// updateMediaMetadata edits a title's title, description, studio, categories
// and performers, then writes the same changes into its NFO files.
func (a *API) updateMediaMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	var req updateMediaMetadataRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		req.Title = &trimmed
	}

	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start update")
		return
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM media_item WHERE id = $1 FOR UPDATE`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load media item")
		return
	}

	if _, err := tx.Exec(ctx, `
		UPDATE media_item SET
			title = COALESCE($2, title),
			sort_title = COALESCE($2, sort_title),
			description = CASE WHEN $3::text IS NULL THEN description ELSE NULLIF($3, '') END,
			metadata_source = 'MANUAL',
			updated_at = now()
		WHERE id = $1
	`, id, req.Title, req.Description); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update title")
		return
	}

	var studioName *string
	if req.Studio != nil {
		name := strings.TrimSpace(*req.Studio)
		studioName = &name
		var studioID *int64
		if name != "" {
			var sid int64
			err := tx.QueryRow(ctx, `SELECT id FROM studio WHERE lower(name) = lower($1) LIMIT 1`, name).Scan(&sid)
			if errors.Is(err, pgx.ErrNoRows) {
				err = tx.QueryRow(ctx, `INSERT INTO studio (name) VALUES ($1) RETURNING id`, name).Scan(&sid)
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save studio")
				return
			}
			studioID = &sid
		}
		if _, err := tx.Exec(ctx, `UPDATE media_item SET studio_id = $2 WHERE id = $1`, id, studioID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save studio")
			return
		}
	}

	var categoryNames []string
	if req.Categories != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM media_item_category WHERE media_item_id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save categories")
			return
		}
		seen := map[string]bool{}
		for _, raw := range *req.Categories {
			name := strings.TrimSpace(raw)
			slug := slugifyName(name)
			if slug == "" || seen[slug] {
				continue
			}
			seen[slug] = true
			var cid int64
			var canonical string
			if err := tx.QueryRow(ctx, `
				INSERT INTO category (name, slug) VALUES ($1, $2)
				ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
				RETURNING id, name
			`, name, slug).Scan(&cid, &canonical); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save categories")
				return
			}
			if _, err := tx.Exec(ctx, `INSERT INTO media_item_category (media_item_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, cid); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save categories")
				return
			}
			categoryNames = append(categoryNames, canonical)
		}
	}

	var performerNames []string
	if req.Performers != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM media_item_performer WHERE media_item_id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save performers")
			return
		}
		seen := map[int64]bool{}
		for _, ref := range *req.Performers {
			pid, name, err := resolvePerformer(ctx, tx, ref)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if pid == 0 || seen[pid] {
				continue
			}
			seen[pid] = true
			if _, err := tx.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id, position) VALUES ($1, $2, $3)`, id, pid, len(performerNames)); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save performers")
				return
			}
			performerNames = append(performerNames, name)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save changes")
		return
	}

	fields := scanner.NFOFields{Title: req.Title, Plot: req.Description, Studio: studioName}
	if req.Categories != nil {
		fields.Genres = &categoryNames
	}
	if req.Performers != nil {
		fields.Performers = &performerNames
	}
	updated, failed := a.writeItemNFOs(ctx, id, fields, true)
	writeJSON(w, http.StatusOK, map[string]any{"nfoUpdated": updated, "nfoFailed": failed})
}

// resolvePerformer finds the performer for a ref: by id, else by name or alias,
// else a new performer is created.
func resolvePerformer(ctx context.Context, tx pgx.Tx, ref performerRef) (int64, string, error) {
	if ref.ID > 0 {
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM performer WHERE id = $1`, ref.ID).Scan(&name); err != nil {
			return 0, "", errPerformerNotFound
		}
		return ref.ID, name, nil
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return 0, "", nil
	}
	var id int64
	var canonical string
	err := tx.QueryRow(ctx, `SELECT id, name FROM performer WHERE lower(name) = lower($1) LIMIT 1`, name).Scan(&id, &canonical)
	if err == nil {
		return id, canonical, nil
	}
	err = tx.QueryRow(ctx, `
		SELECT p.id, p.name FROM performer_alias pa JOIN performer p ON p.id = pa.performer_id
		WHERE lower(pa.alias) = lower($1) LIMIT 1
	`, name).Scan(&id, &canonical)
	if err == nil {
		return id, canonical, nil
	}
	if err := tx.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		return 0, "", err
	}
	return id, name, nil
}

type stringError string

func (e stringError) Error() string { return string(e) }

const errPerformerNotFound = stringError("performer not found")

// writeItemNFOs applies fields to every NFO next to the title's files. When
// the title has none and createMissing is set, one is written for its primary file from what the
// database now holds, so the edit is never DB-only.
func (a *API) writeItemNFOs(ctx context.Context, mediaItemID int64, fields scanner.NFOFields, createMissing bool) (updated, failed int) {
	rows, err := a.DB.Query(ctx, `
		SELECT mf.id, mf.storage_location_id, sl.mount_path, mf.relative_path
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id = $1
		ORDER BY mf.available DESC, mf.height DESC NULLS LAST, mf.file_size DESC
	`, mediaItemID)
	if err != nil {
		return 0, 1
	}
	type fileRef struct {
		id, locationID int64
		mount, rel     string
	}
	var files []fileRef
	for rows.Next() {
		var f fileRef
		if rows.Scan(&f.id, &f.locationID, &f.mount, &f.rel) == nil {
			files = append(files, f)
		}
	}
	rows.Close()

	found := false
	for _, f := range files {
		abs := filepath.Join(f.mount, filepath.FromSlash(f.rel))
		nfo, ok := scanner.FindAdjacentNFO(abs)
		if !ok {
			continue
		}
		found = true
		changed, err := scanner.EditNFO(nfo, fields)
		if err != nil {
			failed++
			continue
		}
		if changed {
			updated++
			if err := scanner.RefreshNFORecord(ctx, a.DB, f.id, nfo); err != nil {
				failed++
			}
		}
	}
	if found || len(files) == 0 || !createMissing {
		return updated, failed
	}

	abs := filepath.Join(files[0].mount, filepath.FromSlash(files[0].rel))
	if _, err := os.Stat(abs); err != nil {
		return updated, failed
	}
	if err := a.createItemNFO(ctx, mediaItemID, files[0].id, files[0].locationID, files[0].rel, abs); err != nil {
		return updated, failed + 1
	}
	return updated + 1, failed
}

func (a *API) createItemNFO(ctx context.Context, mediaItemID, mediaFileID, locationID int64, rel, abs string) error {
	nfoPath := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".nfo"
	if err := scanner.WriteItemNFO(ctx, a.DB, mediaItemID, nfoPath); err != nil {
		return err
	}
	return scanner.ImportNFO(ctx, a.DB, mediaFileID, mediaItemID, locationID, rel, nfoPath)
}

// uploadMediaPoster sets a poster the user chose. Nothing else (provider
// matches, rescans) replaces it afterwards.
func (a *API) uploadMediaPoster(w http.ResponseWriter, r *http.Request) {
	a.uploadMediaArtwork(w, r, false)
}

func (a *API) uploadMediaBackdrop(w http.ResponseWriter, r *http.Request) {
	a.uploadMediaArtwork(w, r, true)
}

func (a *API) uploadMediaArtwork(w http.ResponseWriter, r *http.Request, backdrop bool) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	if a.Namer == nil {
		writeError(w, http.StatusServiceUnavailable, "artwork storage is not available")
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
	var saveErr error
	if backdrop {
		saveErr = a.Namer.SetMediaBackdropUpload(r.Context(), id, body)
	} else {
		saveErr = a.Namer.SetMediaPosterUpload(r.Context(), id, body)
	}
	if err := saveErr; err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	column, field := "poster_image_id", "posterImageId"
	if backdrop {
		column, field = "backdrop_image_id", "backdropImageId"
	}
	var imageID int64
	_ = a.DB.QueryRow(r.Context(), `SELECT `+column+` FROM media_item WHERE id = $1`, id).Scan(&imageID)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, field: imageID})
}

// deleteMediaPoster removes the title's poster, custom or not, so the
// generated thumbnail shows again.
func (a *API) deleteMediaPoster(w http.ResponseWriter, r *http.Request) {
	a.deleteMediaArtwork(w, r, false)
}

func (a *API) deleteMediaBackdrop(w http.ResponseWriter, r *http.Request) {
	a.deleteMediaArtwork(w, r, true)
}

func (a *API) deleteMediaArtwork(w http.ResponseWriter, r *http.Request, backdrop bool) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid media item id")
		return
	}
	column := "poster_image_id"
	if backdrop {
		column = "backdrop_image_id"
	}
	var imageID *int64
	var key string
	if err := a.DB.QueryRow(r.Context(), `
		SELECT mi.`+column+`, COALESCE(ar.storage_key, '')
		FROM media_item mi LEFT JOIN artwork ar ON ar.id = mi.`+column+` WHERE mi.id = $1
	`, id).Scan(&imageID, &key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load media item")
		return
	}
	if imageID != nil {
		if _, err := a.DB.Exec(r.Context(), `UPDATE media_item SET `+column+` = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove artwork")
			return
		}
		a.DB.Exec(r.Context(), `DELETE FROM artwork WHERE id = $1`, *imageID)
		if key != "" && filepath.Base(key) == key {
			os.Remove(filepath.Join(a.Cfg.PosterDir, key))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

package namer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"reelstash/backend/internal/provider"
)

const maxPerformerPhotoBytes = 8 << 20

func (e *Engine) saveScenePerformerPhotos(ctx context.Context, scene provider.MediaResult) {
	if e == nil || len(scene.PerformerImages) == 0 {
		return
	}
	source := scene.Ref.Provider
	for name, imageURL := range scene.PerformerImages {
		var id int64
		// One automatic photo per provider: a performer who already has one
		// from this provider does not collect another from every scene.
		err := e.DB.QueryRow(ctx, `
			SELECT p.id FROM performer p
			WHERE p.name = $1
			  AND NOT EXISTS (SELECT 1 FROM performer_photo pp WHERE pp.performer_id = p.id AND pp.source = $2)
		`, name, source).Scan(&id)
		if err != nil {
			continue
		}
		if err := e.AddPerformerPhoto(ctx, id, source, imageURL, false); err != nil {
			fmt.Printf("namer: performer photo for %q: %v\n", name, err)
		}
	}
}

// AddPerformerPhoto downloads a provider photo into the performer's gallery.
// It becomes the displayed photo when the performer has none, or with
// preferred when the user has not picked one themselves. A URL already in the
// gallery is not downloaded again.
func (e *Engine) AddPerformerPhoto(ctx context.Context, performerID int64, source, imageURL string, preferred bool) error {
	if e == nil || imageURL == "" {
		return nil
	}
	var existing int64
	err := e.DB.QueryRow(ctx, `SELECT artwork_id FROM performer_photo WHERE performer_id = $1 AND source_url = $2`, performerID, imageURL).Scan(&existing)
	if err == nil {
		return e.offerPrimaryPhoto(ctx, performerID, existing, preferred)
	}
	if err := e.validateImageURL(imageURL); err != nil {
		return err
	}
	body, mime, err := e.downloadPerformerImage(ctx, imageURL)
	if err != nil {
		return err
	}
	artworkID, err := e.insertPerformerPhoto(ctx, performerID, body, mime, source, imageURL, false)
	if err != nil {
		return err
	}
	return e.offerPrimaryPhoto(ctx, performerID, artworkID, preferred)
}

// AddPerformerPhotoUpload stores an image the user uploaded in the performer's
// gallery and makes it the displayed photo.
func (e *Engine) AddPerformerPhotoUpload(ctx context.Context, performerID int64, body []byte) (int64, error) {
	mime, err := checkUpload(body)
	if err != nil {
		return 0, err
	}
	var exists bool
	if err := e.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM performer WHERE id = $1)`, performerID).Scan(&exists); err != nil {
		return 0, err
	} else if !exists {
		return 0, pgx.ErrNoRows
	}
	artworkID, err := e.insertPerformerPhoto(ctx, performerID, body, mime, "upload", "", true)
	if err != nil {
		return 0, err
	}
	_, err = e.DB.Exec(ctx, `UPDATE performer SET profile_image_id = $2, photo_chosen = true, updated_at = now() WHERE id = $1`, performerID, artworkID)
	return artworkID, err
}

func (e *Engine) insertPerformerPhoto(ctx context.Context, performerID int64, body []byte, mime, source, sourceURL string, custom bool) (int64, error) {
	if err := os.MkdirAll(e.PosterDir, 0o755); err != nil {
		return 0, err
	}
	storageKey := uuid.NewString() + photoExtension(mime)
	outPath := filepath.Join(e.PosterDir, storageKey)
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return 0, err
	}
	tx, err := e.DB.Begin(ctx)
	if err != nil {
		os.Remove(outPath)
		return 0, err
	}
	defer tx.Rollback(ctx)
	var artworkID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO artwork (kind, storage_key, mime_type, custom) VALUES ('PROFILE', $1, $2, $3) RETURNING id
	`, storageKey, mime, custom).Scan(&artworkID); err != nil {
		os.Remove(outPath)
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO performer_photo (performer_id, artwork_id, source, source_url) VALUES ($1, $2, $3, NULLIF($4, ''))
	`, performerID, artworkID, source, sourceURL); err != nil {
		os.Remove(outPath)
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		os.Remove(outPath)
		return 0, err
	}
	return artworkID, nil
}

// offerPrimaryPhoto shows a gallery photo when the performer has none, or, when
// preferred, unless the user picked the displayed photo themselves.
func (e *Engine) offerPrimaryPhoto(ctx context.Context, performerID, artworkID int64, preferred bool) error {
	_, err := e.DB.Exec(ctx, `
		UPDATE performer SET profile_image_id = $2, updated_at = now()
		WHERE id = $1 AND (profile_image_id IS NULL OR ($3 AND NOT photo_chosen))
	`, performerID, artworkID, preferred)
	return err
}

// FetchPerformerPhotos adds each linked provider's current photo to the
// performer's gallery, for performers matched before galleries existed.
func (e *Engine) FetchPerformerPhotos(ctx context.Context, performerID int64) (added int, err error) {
	rows, err := e.DB.Query(ctx, `SELECT provider, external_id FROM match_link WHERE kind = 'performer' AND entity_id = $1 ORDER BY matched_at`, performerID)
	if err != nil {
		return 0, err
	}
	var refs []provider.Ref
	for rows.Next() {
		var ref provider.Ref
		if rows.Scan(&ref.Provider, &ref.ExternalID) == nil {
			refs = append(refs, ref)
		}
	}
	rows.Close()
	if len(refs) == 0 {
		return 0, fmt.Errorf("this performer is not linked to a metadata provider")
	}
	var firstErr error
	for _, ref := range refs {
		p, ok := e.Providers.Get(ref.Provider)
		if !ok || !p.Enabled() {
			continue
		}
		remote, err := p.GetPerformer(ctx, ref.ExternalID)
		if err == nil && remote.ImageURL != "" {
			var before int
			e.DB.QueryRow(ctx, `SELECT COUNT(*) FROM performer_photo WHERE performer_id = $1`, performerID).Scan(&before)
			err = e.AddPerformerPhoto(ctx, performerID, ref.Provider, remote.ImageURL, false)
			var after int
			e.DB.QueryRow(ctx, `SELECT COUNT(*) FROM performer_photo WHERE performer_id = $1`, performerID).Scan(&after)
			added += after - before
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", p.Name(), err)
		}
	}
	if added == 0 && firstErr != nil {
		return 0, firstErr
	}
	return added, nil
}

func checkUpload(body []byte) (string, error) {
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return "", fmt.Errorf("image is empty or larger than %d MB", maxPerformerPhotoBytes>>20)
	}
	mime := http.DetectContentType(body)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return mime, nil
	}
	return "", fmt.Errorf("upload a JPEG, PNG, WebP or GIF image")
}

// ReplaceStudioLogo downloads a studio image and replaces any logo already stored.
func (e *Engine) ReplaceStudioLogo(ctx context.Context, studioID int64, imageURL string) error {
	return e.storeLinkedImage(ctx, "studio", "logo_image_id", "LOGO", studioID, imageURL, true)
}

func (e *Engine) storeLinkedImage(ctx context.Context, table, column, kind string, entityID int64, imageURL string, replace bool) error {
	if e == nil || imageURL == "" {
		return nil
	}
	switch {
	case table == "studio" && column == "logo_image_id" && kind == "LOGO":
	default:
		return fmt.Errorf("unsupported artwork target")
	}
	if err := e.validateImageURL(imageURL); err != nil {
		return err
	}

	var existing *int64
	var custom bool
	err := e.DB.QueryRow(ctx, fmt.Sprintf(`
		SELECT t.%s, COALESCE(ar.custom, false) FROM %s t LEFT JOIN artwork ar ON ar.id = t.%s WHERE t.id = $1
	`, column, table, column), entityID).Scan(&existing, &custom)
	if err != nil {
		return err
	}
	if existing != nil && (!replace || custom) {
		return nil // never replace an image the user set by hand
	}

	body, mime, err := e.downloadPerformerImage(ctx, imageURL)
	if err != nil {
		return err
	}
	return e.storeLinkedImageBytes(ctx, table, column, kind, entityID, body, mime, replace, false)
}

// SetStudioLogoUpload stores an image the user uploaded as the studio's logo,
// replacing any existing one.
func (e *Engine) SetStudioLogoUpload(ctx context.Context, studioID int64, body []byte) error {
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return fmt.Errorf("image is empty or larger than %d MB", maxPerformerPhotoBytes>>20)
	}
	mime := http.DetectContentType(body)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return fmt.Errorf("upload a JPEG, PNG, WebP or GIF image")
	}
	return e.storeLinkedImageBytes(ctx, "studio", "logo_image_id", "LOGO", studioID, body, mime, true, true)
}

// SetMediaPosterUpload stores an image the user uploaded as a title's poster,
// replacing any existing one.
func (e *Engine) SetMediaPosterUpload(ctx context.Context, mediaItemID int64, body []byte) error {
	return e.setMediaArtworkUpload(ctx, mediaItemID, body, "poster_image_id", "POSTER")
}

// SetMediaBackdropUpload stores a user-selected backdrop independently of the poster.
func (e *Engine) SetMediaBackdropUpload(ctx context.Context, mediaItemID int64, body []byte) error {
	return e.setMediaArtworkUpload(ctx, mediaItemID, body, "backdrop_image_id", "BACKDROP")
}

func (e *Engine) setMediaArtworkUpload(ctx context.Context, mediaItemID int64, body []byte, column, kind string) error {
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return fmt.Errorf("image is empty or larger than %d MB", maxPerformerPhotoBytes>>20)
	}
	mime := http.DetectContentType(body)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return fmt.Errorf("upload a JPEG, PNG, WebP or GIF image")
	}
	return e.storeLinkedImageBytes(ctx, "media_item", column, kind, mediaItemID, body, mime, true, true)
}

// storeLinkedImageBytes writes an image and links it. custom marks it as set by
// hand: automatic callers (custom=false) leave an existing custom image alone.
func (e *Engine) storeLinkedImageBytes(ctx context.Context, table, column, kind string, entityID int64, body []byte, mime string, replace, custom bool) error {
	var existing *int64
	var existingCustom bool
	if err := e.DB.QueryRow(ctx, fmt.Sprintf(`
		SELECT t.%s, COALESCE(ar.custom, false) FROM %s t LEFT JOIN artwork ar ON ar.id = t.%s WHERE t.id = $1
	`, column, table, column), entityID).Scan(&existing, &existingCustom); err != nil {
		return err
	}
	if existing != nil && (!replace || (existingCustom && !custom)) {
		return nil
	}
	if err := os.MkdirAll(e.PosterDir, 0o755); err != nil {
		return err
	}
	storageKey := uuid.NewString() + photoExtension(mime)
	outPath := filepath.Join(e.PosterDir, storageKey)
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return err
	}

	var artworkID int64
	if err := e.DB.QueryRow(ctx, `
		INSERT INTO artwork (kind, storage_key, mime_type, custom) VALUES ($1, $2, $3, $4)
		RETURNING id
	`, kind, storageKey, mime, custom).Scan(&artworkID); err != nil {
		os.Remove(outPath)
		return err
	}
	query := fmt.Sprintf(`
		UPDATE %s SET %s = $2, updated_at = now()
		WHERE id = $1 AND %s IS NULL
	`, table, column, column)
	if replace {
		query = fmt.Sprintf(`
			UPDATE %s SET %s = $2, updated_at = now()
			WHERE id = $1
		`, table, column)
	}
	tag, err := e.DB.Exec(ctx, query, entityID, artworkID)
	if err != nil || tag.RowsAffected() == 0 {
		e.DB.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, artworkID)
		os.Remove(outPath)
		return err
	}
	if replace && existing != nil && *existing != artworkID {
		var oldKey string
		if err := e.DB.QueryRow(ctx, `SELECT storage_key FROM artwork WHERE id = $1`, *existing).Scan(&oldKey); err == nil && oldKey != "" && filepath.Base(oldKey) == oldKey {
			os.Remove(filepath.Join(e.PosterDir, oldKey))
		}
		e.DB.Exec(ctx, `DELETE FROM artwork WHERE id = $1`, *existing)
	}
	return nil
}

func (e *Engine) validateImageURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("refusing performer image url")
	}
	host := strings.ToLower(parsed.Hostname())
	if !e.Providers.ImageHostAllowed(host) {
		return fmt.Errorf("refusing performer image host %s", host)
	}
	return nil
}

func (e *Engine) downloadPerformerImage(ctx context.Context, imageURL string) ([]byte, string, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return e.validateImageURL(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "reelstash-namer/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("performer image download returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPerformerPhotoBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return nil, "", fmt.Errorf("performer image is empty or too large")
	}
	mime := resp.Header.Get("Content-Type")
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = mime[:i]
	}
	mime = strings.TrimSpace(strings.ToLower(mime))
	if !strings.HasPrefix(mime, "image/") {
		detected := http.DetectContentType(body)
		if !strings.HasPrefix(detected, "image/") {
			return nil, "", fmt.Errorf("performer image is not an image")
		}
		mime = detected
	}
	return body, mime, nil
}

func photoExtension(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

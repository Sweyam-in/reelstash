package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Artwork stays behind the same session check as the rest of the media API.
// Only database-generated storage keys are used to locate files on disk.
func (a *API) serveMediaArtwork(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil || id < 1 {
		http.Error(w, "invalid media item", http.StatusBadRequest)
		return
	}

	kind := r.URL.Query().Get("kind")
	var steps []artLookup
	switch kind {
	case "backdrop":
		steps = []artLookup{a.lookupBackdrop}
	case "poster":
		// A poster set by hand, then any poster, then the generated thumbnail.
		steps = []artLookup{a.lookupCustomPoster, a.lookupPoster, a.lookupThumbnail}
	default:
		// Thumbnails stay the default, with a hand-set poster ahead of them and
		// any poster only when there is no thumbnail.
		steps = []artLookup{a.lookupCustomPoster, a.lookupThumbnail, a.lookupPoster}
	}
	for _, step := range steps {
		key, mime, dir, findErr := step(r, id)
		if findErr != nil {
			if errors.Is(findErr, pgx.ErrNoRows) {
				continue
			}
			http.Error(w, "failed to load artwork", http.StatusInternalServerError)
			return
		}
		if key == "" || filepath.Base(key) != key {
			continue
		}
		file, openErr := os.Open(filepath.Join(dir, key))
		if openErr != nil {
			continue
		}
		if mime == "" {
			mime = "image/jpeg"
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, key, fileModTime(file), file)
		file.Close()
		return
	}
	http.NotFound(w, r)
}

type artLookup func(r *http.Request, mediaID int64) (key, mime, dir string, err error)

func (a *API) lookupBackdrop(r *http.Request, mediaID int64) (string, string, string, error) {
	var key, mime string
	err := a.DB.QueryRow(r.Context(), `
		SELECT ar.storage_key, ar.mime_type FROM media_item mi
		JOIN artwork ar ON ar.id = mi.backdrop_image_id
		WHERE mi.id = $1 AND ar.kind = 'BACKDROP'
	`, mediaID).Scan(&key, &mime)
	return key, mime, a.Cfg.PosterDir, err
}

func (a *API) lookupCustomPoster(r *http.Request, mediaID int64) (string, string, string, error) {
	var key, mime string
	err := a.DB.QueryRow(r.Context(), `
		SELECT ar.storage_key, ar.mime_type FROM media_item mi
		JOIN artwork ar ON ar.id = mi.poster_image_id
		WHERE mi.id = $1 AND ar.kind = 'POSTER' AND ar.custom
	`, mediaID).Scan(&key, &mime)
	return key, mime, a.Cfg.PosterDir, err
}

func (a *API) lookupPoster(r *http.Request, mediaID int64) (string, string, string, error) {
	var key, mime string
	err := a.DB.QueryRow(r.Context(), `
		SELECT ar.storage_key, ar.mime_type FROM media_item mi
		JOIN artwork ar ON ar.id = mi.poster_image_id
		WHERE mi.id = $1 AND ar.kind = 'POSTER'
	`, mediaID).Scan(&key, &mime)
	return key, mime, a.Cfg.PosterDir, err
}

func (a *API) lookupThumbnail(r *http.Request, mediaID int64) (string, string, string, error) {
	var key string
	err := a.DB.QueryRow(r.Context(), `
		SELECT t.storage_key FROM thumbnail t
		WHERE t.media_item_id = $1
		ORDER BY t.preferred DESC, t.created_at DESC LIMIT 1
	`, mediaID).Scan(&key)
	return key, "", a.Cfg.ThumbnailDir, err
}

func fileModTime(file *os.File) (modTime time.Time) {
	if info, err := file.Stat(); err == nil {
		return info.ModTime()
	}
	return modTime
}

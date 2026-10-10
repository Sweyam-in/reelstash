package httpapi

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// serveStoredFile serves a generated file from dir by its database-issued key.
func serveStoredFile(w http.ResponseWriter, r *http.Request, dir, key, mime string) {
	if key == "" || filepath.Base(key) != key {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(dir, key))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, key, fileModTime(file), file)
}

type spriteRow struct {
	Key       string
	Interval  float64
	TileW     int
	TileH     int
	Columns   int
	Count     int
	CreatedAt int64
}

func (a *API) loadSprite(r *http.Request, fileID int64) (spriteRow, bool, error) {
	var s spriteRow
	err := a.DB.QueryRow(r.Context(), `
		SELECT storage_key, interval_seconds, tile_width, tile_height, columns, tile_count, EXTRACT(EPOCH FROM created_at)::bigint
		FROM scrub_sprite WHERE media_file_id = $1
	`, fileID).Scan(&s.Key, &s.Interval, &s.TileW, &s.TileH, &s.Columns, &s.Count, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, err
}

func (a *API) backgroundJobActive(r *http.Request, kind string, fileID int64) bool {
	var active bool
	err := a.DB.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM background_job
			WHERE kind = $1 AND status IN ('PENDING','RUNNING') AND payload->>'mediaFileId' = $2)
	`, kind, strconv.FormatInt(fileID, 10)).Scan(&active)
	if err != nil {
		log.Printf("sprites: check for a running %s job: %v", kind, err)
	}
	return active
}

func spriteImageURL(mediaID, fileID int64, s spriteRow) string {
	return fmt.Sprintf("/api/media/%d/sprite.jpg?fileId=%d&v=%d", mediaID, fileID, s.CreatedAt)
}

// spriteInfo describes the scrub-preview sheet for the title's video, or says
// it is missing (and whether it is being built) so the player can ask for it.
func (a *API) spriteInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	s, found, err := a.loadSprite(r, src.FileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load preview sprite")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false, "generating": a.backgroundJobActive(r, "SPRITE", src.FileID)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": true, "url": spriteImageURL(id, src.FileID, s),
		"intervalSeconds": s.Interval, "tileWidth": s.TileW, "tileHeight": s.TileH,
		"columns": s.Columns, "count": s.Count,
	})
}

func (a *API) spriteImage(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	s, found, err := a.loadSprite(r, src.FileID)
	if err != nil || !found {
		http.NotFound(w, r)
		return
	}
	serveStoredFile(w, r, a.Cfg.SpriteDir, s.Key, "image/jpeg")
}

func vttTime(seconds float64) string {
	ms := int64(seconds*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// spriteVTT renders the sheet as a WebVTT thumbnail track: one cue per tile,
// pointing at that tile's rectangle within the sheet image.
func spriteVTT(imageURL string, s spriteRow, duration float64) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i := 0; i < s.Count; i++ {
		start := float64(i) * s.Interval
		end := start + s.Interval
		if duration > 0 && end > duration {
			end = duration
		}
		if end <= start {
			break
		}
		x, y := (i%s.Columns)*s.TileW, (i/s.Columns)*s.TileH
		fmt.Fprintf(&b, "%s --> %s\n%s#xywh=%d,%d,%d,%d\n\n", vttTime(start), vttTime(end), imageURL, x, y, s.TileW, s.TileH)
	}
	return b.String()
}

func (a *API) spriteVTT(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	s, found, err := a.loadSprite(r, src.FileID)
	if err != nil || !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(spriteVTT(spriteImageURL(id, src.FileID, s), s, src.Duration)))
}

// requestSprite queues building the scrub-preview sheet if it is missing.
func (a *API) requestSprite(w http.ResponseWriter, r *http.Request) {
	a.requestGenerated(w, r, "SPRITE", func(fileID int64) (bool, error) {
		_, found, err := a.loadSprite(r, fileID)
		return found, err
	})
}

// requestGenerated queues a per-file SPRITE or PREVIEW job unless the result
// already exists or a job for it is already waiting or running.
func (a *API) requestGenerated(w http.ResponseWriter, r *http.Request, kind string, exists func(fileID int64) (bool, error)) {
	id, ok := mediaIDParam(w, r)
	if !ok {
		return
	}
	src, status, msg := a.resolveMediaFile(r, id)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	if found, err := exists(src.FileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check for an existing file")
		return
	} else if found {
		writeJSON(w, http.StatusOK, map[string]any{"ready": true})
		return
	}
	if !ffmpegAvailable() {
		writeError(w, http.StatusNotImplemented, "ffmpeg is not installed on the server")
		return
	}
	if a.Scanner == nil || a.Scanner.ThumbnailJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "job runner not available")
		return
	}
	if !a.backgroundJobActive(r, kind, src.FileID) {
		if _, err := a.Scanner.ThumbnailJobs.Enqueue(r.Context(), kind, map[string]any{"mediaFileId": src.FileID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to queue "+strings.ToLower(kind))
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ready": false, "generating": true, "fileId": strconv.FormatInt(src.FileID, 10)})
}

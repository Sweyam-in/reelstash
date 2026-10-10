package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
)

// Scrub-preview sprite geometry: one JPEG holding a grid of small frames.
const (
	spriteTileW    = 160
	spriteTileH    = 90
	spriteMaxCols  = 10
	spriteMaxTiles = 100
	spriteMinStep  = 4.0 // seconds between frames, at least
	spriteTimeout  = 20 * time.Minute
	previewTimeout = 4 * time.Minute
)

// SpriteJobPayload is the payload of a SPRITE or PREVIEW job.
type SpriteJobPayload struct {
	MediaFileID int64 `json:"mediaFileId"`
}

// spriteLayout works out how often to sample a video of this length and how to
// arrange the frames: at most spriteMaxTiles frames, at least spriteMinStep
// seconds apart, in rows of up to spriteMaxCols.
func spriteLayout(duration float64) (interval float64, count, columns, rows int) {
	if duration <= 0 {
		duration = spriteMinStep
	}
	interval = math.Max(spriteMinStep, math.Ceil(duration/spriteMaxTiles))
	count = int(math.Ceil(duration / interval))
	if count < 1 {
		count = 1
	}
	columns = min(spriteMaxCols, count)
	rows = (count + columns - 1) / columns
	return interval, count, columns, rows
}

// mediaFileSource loads where a media file lives on disk and its stored duration.
func mediaFileSource(ctx context.Context, db *pgxpool.Pool, mediaFileID int64) (path string, duration float64, err error) {
	var mount, rel string
	var dur *float64
	err = db.QueryRow(ctx, `
		SELECT sl.mount_path, mf.relative_path, mf.duration_seconds
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.id = $1 AND mf.available
	`, mediaFileID).Scan(&mount, &rel, &dur)
	if err != nil {
		return "", 0, err
	}
	path = filepath.Join(mount, filepath.FromSlash(rel))
	if dur != nil && *dur > 0 {
		return path, *dur, nil
	}
	pr, perr := Probe(ctx, path)
	if perr != nil || pr.DurationSeconds == nil || *pr.DurationSeconds <= 0 {
		return path, 0, errors.New("cannot determine the video duration")
	}
	return path, *pr.DurationSeconds, nil
}

// spriteWorkers is how many frames are grabbed at once while building a sheet.
const spriteWorkers = 4

// renderSprite writes the tiled sheet for src to out. Instead of decoding the
// whole video it seeks to each sample point and grabs one small frame, so the
// cost is about one short read per tile however long the video is. A tile that
// cannot be read stays black; if most fail the sheet is not worth keeping.
func renderSprite(ctx context.Context, src, out string, interval, duration float64, count, columns, rows int) error {
	ctx, cancel := context.WithTimeout(ctx, spriteTimeout)
	defer cancel()

	sheet := image.NewRGBA(image.Rect(0, 0, columns*spriteTileW, rows*spriteTileH))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)

	var mu sync.Mutex
	failed := 0
	indexes := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < spriteWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				at := math.Min((float64(i)+0.5)*interval, math.Max(0, duration-0.5))
				ok := false
				if data, err := extractFit(ctx, src, at, spriteTileW, spriteTileH); err == nil {
					if tile, err := jpeg.Decode(bytes.NewReader(data)); err == nil {
						x, y := (i%columns)*spriteTileW, (i/columns)*spriteTileH
						mu.Lock()
						draw.Draw(sheet, image.Rect(x, y, x+spriteTileW, y+spriteTileH), tile, tile.Bounds().Min, draw.Src)
						mu.Unlock()
						ok = true
					}
				}
				if !ok {
					mu.Lock()
					failed++
					mu.Unlock()
				}
			}
		}()
	}
feed:
	for i := 0; i < count; i++ {
		select {
		case indexes <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(indexes)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if failed*2 > count {
		return fmt.Errorf("could not read frames for the preview sprite (%d of %d failed)", failed, count)
	}

	file, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(file, sheet, &jpeg.Options{Quality: 72}); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// GenerateSprite builds and records the scrub-preview sheet for a media file,
// replacing any earlier one.
func GenerateSprite(ctx context.Context, db *pgxpool.Pool, spriteDir string, mediaFileID int64) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not available: %w", err)
	}
	src, duration, err := mediaFileSource(ctx, db, mediaFileID)
	if err != nil {
		return err
	}
	interval, count, columns, rows := spriteLayout(duration)
	if err := os.MkdirAll(spriteDir, 0o755); err != nil {
		return fmt.Errorf("create sprite dir: %w", err)
	}
	key := uuid.NewString() + ".jpg"
	final := filepath.Join(spriteDir, key)
	tmp := filepath.Join(spriteDir, key+".part.jpg")
	if err := renderSprite(ctx, src, tmp, interval, duration, count, columns, rows); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return err
	}

	var oldKey string
	_ = db.QueryRow(ctx, `SELECT storage_key FROM scrub_sprite WHERE media_file_id = $1`, mediaFileID).Scan(&oldKey)
	_, err = db.Exec(ctx, `
		INSERT INTO scrub_sprite (media_file_id, storage_key, interval_seconds, tile_width, tile_height, columns, tile_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (media_file_id) DO UPDATE SET
			storage_key = EXCLUDED.storage_key, interval_seconds = EXCLUDED.interval_seconds,
			tile_width = EXCLUDED.tile_width, tile_height = EXCLUDED.tile_height,
			columns = EXCLUDED.columns, tile_count = EXCLUDED.tile_count, created_at = now()
	`, mediaFileID, key, interval, spriteTileW, spriteTileH, columns, count)
	if err != nil {
		os.Remove(final)
		return err
	}
	if oldKey != "" && filepath.Base(oldKey) == oldKey {
		os.Remove(filepath.Join(spriteDir, oldKey))
	}
	return nil
}

// previewSegments picks where the hover preview samples the video and how
// long each piece is. Short videos get fewer, evenly spread pieces.
func previewSegments(duration float64) (starts []float64, length float64) {
	if duration <= 0 {
		return []float64{0}, 3
	}
	if duration < 12 {
		return []float64{0}, math.Min(4, duration)
	}
	length = 1.5
	n := 5
	if duration < 60 {
		n = 3
	}
	for i := 1; i <= n; i++ {
		start := duration * float64(i) / float64(n+1)
		starts = append(starts, math.Min(start, duration-length))
	}
	return starts, length
}

// renderPreview writes a silent, low-resolution mp4 stitched from the given
// pieces of src.
func renderPreview(ctx context.Context, src, out string, starts []float64, length float64) error {
	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()
	args := []string{"-nostdin", "-v", "error", "-y"}
	var chains, labels []string
	for i, start := range starts {
		args = append(args, "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", length), "-i", src)
		chains = append(chains, fmt.Sprintf("[%d:v:0]scale=-2:240,fps=24,setsar=1,format=yuv420p[v%d]", i, i))
		labels = append(labels, fmt.Sprintf("[v%d]", i))
	}
	graph := strings.Join(chains, ";") + ";" + strings.Join(labels, "") + fmt.Sprintf("concat=n=%d:v=1:a=0[v]", len(starts))
	args = append(args, "-filter_complex", graph, "-map", "[v]", "-an",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "30",
		"-movflags", "+faststart", "-f", "mp4", out)
	if b, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg timed out after %s building the preview", previewTimeout)
		}
		return fmt.Errorf("ffmpeg preview failed: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

// GeneratePreview builds and records the hover preview clip for a media file.
func GeneratePreview(ctx context.Context, db *pgxpool.Pool, previewDir string, mediaFileID int64) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not available: %w", err)
	}
	src, duration, err := mediaFileSource(ctx, db, mediaFileID)
	if err != nil {
		return err
	}
	starts, length := previewSegments(duration)
	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		return fmt.Errorf("create preview dir: %w", err)
	}
	key := uuid.NewString() + ".mp4"
	final := filepath.Join(previewDir, key)
	tmp := filepath.Join(previewDir, key+".part.mp4")
	if err := renderPreview(ctx, src, tmp, starts, length); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return err
	}

	var oldKey string
	_ = db.QueryRow(ctx, `SELECT storage_key FROM preview_clip WHERE media_file_id = $1`, mediaFileID).Scan(&oldKey)
	_, err = db.Exec(ctx, `
		INSERT INTO preview_clip (media_file_id, storage_key, duration_seconds) VALUES ($1, $2, $3)
		ON CONFLICT (media_file_id) DO UPDATE SET storage_key = EXCLUDED.storage_key,
			duration_seconds = EXCLUDED.duration_seconds, created_at = now()
	`, mediaFileID, key, length*float64(len(starts)))
	if err != nil {
		os.Remove(final)
		return err
	}
	if oldKey != "" && filepath.Base(oldKey) == oldKey {
		os.Remove(filepath.Join(previewDir, oldKey))
	}
	return nil
}

// primaryFileIDs returns, per title, the file the player streams (best
// resolution, then largest) — the same pick the HTTP layer makes — limited to
// those where missing(file id) is true in SQL.
func (s *Scanner) primaryFileIDs(ctx context.Context, notExistsSQL string) ([]int64, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id FROM (
			SELECT DISTINCT ON (mf.media_item_id) mf.id
			FROM media_file mf WHERE mf.available
			ORDER BY mf.media_item_id, mf.height DESC NULLS LAST, mf.file_size DESC
		) primary_file
		WHERE NOT EXISTS (`+notExistsSQL+`)
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// runBulk works through ids one at a time, reporting progress, so a long
// library-wide job can be watched and stopped. A file that fails is counted and skipped.
func (s *Scanner) runBulk(ctx context.Context, jobID int64, ids []int64, generate func(ctx context.Context, id int64) error) (any, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not available: %w", err)
	}
	generated, failed := 0, 0
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		jobs.ReportProgress(s.DB, jobID, i, len(ids), fmt.Sprintf("file %d", id))
		if err := generate(ctx, id); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failed++
			continue
		}
		generated++
	}
	jobs.ReportProgress(s.DB, jobID, len(ids), len(ids), "")
	return map[string]int{"generated": generated, "failed": failed, "total": len(ids)}, nil
}

func (s *Scanner) handleSpriteJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var p SpriteJobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if err := GenerateSprite(ctx, db, s.SpriteDir, p.MediaFileID); err != nil {
		return nil, err
	}
	return map[string]any{"mediaFileId": p.MediaFileID}, nil
}

func (s *Scanner) handlePreviewJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var p SpriteJobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if err := GeneratePreview(ctx, db, s.PreviewDir, p.MediaFileID); err != nil {
		return nil, err
	}
	return map[string]any{"mediaFileId": p.MediaFileID}, nil
}

func (s *Scanner) handleSpriteScanJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	ids, err := s.primaryFileIDs(ctx, `SELECT 1 FROM scrub_sprite ss WHERE ss.media_file_id = primary_file.id`)
	if err != nil {
		return nil, err
	}
	return s.runBulk(ctx, jobID, ids, func(ctx context.Context, id int64) error {
		return GenerateSprite(ctx, db, s.SpriteDir, id)
	})
}

func (s *Scanner) handlePreviewScanJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	ids, err := s.primaryFileIDs(ctx, `SELECT 1 FROM preview_clip pc WHERE pc.media_file_id = primary_file.id`)
	if err != nil {
		return nil, err
	}
	return s.runBulk(ctx, jobID, ids, func(ctx context.Context, id int64) error {
		return GeneratePreview(ctx, db, s.PreviewDir, id)
	})
}

// handleThumbnailFixJob replaces thumbnails that are black or flat with a
// better frame from the same video.
func (s *Scanner) handleThumbnailFixJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	type thumb struct {
		id, itemID int64
		fileID     *int64
		key        string
	}
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (t.media_item_id) t.id, t.media_item_id, t.media_file_id, t.storage_key
		FROM thumbnail t WHERE t.media_item_id IS NOT NULL
		ORDER BY t.media_item_id, t.preferred DESC, t.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	var thumbs []thumb
	for rows.Next() {
		var t thumb
		if err := rows.Scan(&t.id, &t.itemID, &t.fileID, &t.key); err != nil {
			rows.Close()
			return nil, err
		}
		thumbs = append(thumbs, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	checked, fixed, failed := 0, 0, 0
	for i, t := range thumbs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i%25 == 0 {
			jobs.ReportProgress(db, jobID, i, len(thumbs), "")
		}
		checked++
		if filepath.Base(t.key) != t.key {
			continue
		}
		path := filepath.Join(s.ThumbnailDir, t.key)
		data, readErr := os.ReadFile(path)
		if readErr == nil && UsableFrame(data) {
			continue
		}
		fileID := int64(0)
		if t.fileID != nil {
			fileID = *t.fileID
		} else if err := db.QueryRow(ctx, `
			SELECT id FROM media_file WHERE media_item_id = $1 AND available
			ORDER BY height DESC NULLS LAST, file_size DESC LIMIT 1`, t.itemID).Scan(&fileID); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				failed++
			}
			continue
		}
		if err := GenerateThumbnail(ctx, db, s.ThumbnailDir, fileID); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failed++
			continue
		}
		// The new thumbnail is now the newest preferred one; drop the bad one.
		db.Exec(ctx, `DELETE FROM thumbnail WHERE id = $1`, t.id)
		os.Remove(path)
		fixed++
	}
	jobs.ReportProgress(db, jobID, len(thumbs), len(thumbs), "")
	return map[string]int{"checked": checked, "fixed": fixed, "failed": failed}, nil
}

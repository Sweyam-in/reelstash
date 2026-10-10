package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ThumbnailJobPayload struct {
	MediaFileID int64 `json:"mediaFileId"`
}

// ffmpegTimeout bounds a single ffmpeg invocation so a stalled network
// mount can't wedge a thumbnail job (and the worker slot it occupies)
// forever. Unlike a plain file read, exec.CommandContext actually kills the
// ffmpeg process when its context is done, so this timeout is a real
// cancellation, not just "stop waiting" like FullHash's.
const ffmpegTimeout = 2 * time.Minute

// GenerateThumbnail extracts a preview frame with ffmpeg (preferring ~10% into
// the file, skipping black or flat frames) and records it as the media file's
// thumbnail. Thumbnail generation is always run as an async background_job,
// never inline on a request.
func GenerateThumbnail(ctx context.Context, db *pgxpool.Pool, thumbnailDir string, mediaFileID int64) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not available: %w", err)
	}

	var mediaItemID int64
	var mountPath, relativePath string
	var duration *float64
	err := db.QueryRow(ctx, `
		SELECT mf.media_item_id, sl.mount_path, mf.relative_path, mf.duration_seconds
		FROM media_file mf
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.id = $1
	`, mediaFileID).Scan(&mediaItemID, &mountPath, &relativePath, &duration)
	if err != nil {
		return err
	}

	sourcePath := filepath.Join(mountPath, relativePath)
	if err := os.MkdirAll(thumbnailDir, 0o755); err != nil {
		return fmt.Errorf("create thumbnail dir: %w", err)
	}

	length := 0.0
	if duration != nil {
		length = *duration
	}
	// A fixed early timestamp often lands on a black intro, so sample a few
	// points and keep the first frame with real picture in it.
	frame, timestamp, err := PickBestFrame(ctx, sourcePath, length, 640)
	if err != nil {
		return fmt.Errorf("thumbnail frame failed: %w", err)
	}

	storageKey := uuid.NewString() + ".jpg"
	outPath := filepath.Join(thumbnailDir, storageKey)
	if err := os.WriteFile(outPath, frame, 0o644); err != nil {
		return fmt.Errorf("write thumbnail: %w", err)
	}

	if _, err = db.Exec(ctx, `
		INSERT INTO thumbnail (media_item_id, media_file_id, storage_key, timestamp_seconds, preferred)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (storage_key) DO NOTHING
	`, mediaItemID, mediaFileID, storageKey, timestamp); err != nil {
		os.Remove(outPath)
		return err
	}
	return nil
}

func (s *Scanner) handleThumbnailJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var p ThumbnailJobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if err := GenerateThumbnail(ctx, db, s.ThumbnailDir, p.MediaFileID); err != nil {
		return nil, err
	}
	return map[string]any{"mediaFileId": p.MediaFileID, "generatedAt": time.Now()}, nil
}

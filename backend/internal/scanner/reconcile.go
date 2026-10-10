package scanner

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ReconcileRenamedFile updates an existing media_file after some external
// process (the namer bridge) has renamed the file in place and
// possibly written an adjacent .nfo. It re-probes, re-imports the NFO
// (which is where a provider's title/studio/performers/tags flow into
// media_item), and schedules a fresh thumbnail. It clears content_hash so
// the next duplicate scan recomputes it. It refuses to touch a file
// outside the media file's registered storage_location.
func (s *Scanner) ReconcileRenamedFile(ctx context.Context, mediaFileID int64, newAbsPath, nfoAbsPath string) error {
	var storageLocationID, mediaItemID int64
	var mountPath string
	if err := s.DB.QueryRow(ctx, `
		SELECT mf.storage_location_id, mf.media_item_id, sl.mount_path
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.id = $1
	`, mediaFileID).Scan(&storageLocationID, &mediaItemID, &mountPath); err != nil {
		return fmt.Errorf("load media_file %d: %w", mediaFileID, err)
	}

	cleanMount := filepath.Clean(mountPath)
	cleanNew := filepath.Clean(newAbsPath)
	rel, err := filepath.Rel(cleanMount, cleanNew)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("renamed file %s is outside its storage location's mount %s", newAbsPath, mountPath)
	}
	newRelativePath := filepath.ToSlash(rel)

	stat, err := os.Stat(newAbsPath)
	if err != nil {
		return fmt.Errorf("stat renamed file: %w", err)
	}
	size := stat.Size()

	partialHash, err := PartialHash(newAbsPath, size)
	if err != nil {
		return fmt.Errorf("partial hash: %w", err)
	}
	probe, probeErr := Probe(ctx, newAbsPath)
	if probeErr != nil {
		log.Printf("namer bridge reconcile: ffprobe failed for %s: %v", newAbsPath, probeErr)
	}

	fileName := filepath.Base(newRelativePath)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")

	_, err = s.DB.Exec(ctx, `
		UPDATE media_file SET
			relative_path = $2, file_name = $3, extension = $4, file_size = $5,
			content_hash = NULL, partial_hash = $6, duration_seconds = $7, width = $8, height = $9, frame_rate = $10,
			video_codec = $11, audio_codec = $12, container = $4, video_bitrate = $13, audio_bitrate = $14, overall_bitrate = $15,
			modified_time = $16, mp4_data_boxes = $17, rotation = $18, last_scanned_at = now(), available = TRUE, missing_since = NULL, updated_at = now()
		WHERE id = $1
	`, mediaFileID, newRelativePath, fileName, ext, size, partialHash,
		probe.DurationSeconds, probe.Width, probe.Height, probe.FrameRate,
		probe.VideoCodec, probe.AudioCodec, probe.VideoBitrate, probe.AudioBitrate, probe.OverallBitrate,
		stat.ModTime(), mp4DataBoxes(newAbsPath), probe.RotationOrNil(probeErr))
	if err != nil {
		return fmt.Errorf("update media_file after namer rename: %w", err)
	}

	if err := SaveTracks(ctx, s.DB, mediaFileID, probe.Tracks, probeErr == nil); err != nil {
		log.Printf("namer bridge reconcile: failed to save tracks for %s: %v", newAbsPath, err)
	}
	if err := SaveSidecarCount(ctx, s.DB, mediaFileID, len(FindSidecars(newAbsPath))); err != nil {
		log.Printf("namer bridge reconcile: failed to save subtitle count for %s: %v", newAbsPath, err)
	}

	if nfoAbsPath != "" {
		if _, err := os.Stat(nfoAbsPath); err == nil {
			if err := ImportNFO(ctx, s.DB, mediaFileID, mediaItemID, storageLocationID, newRelativePath, nfoAbsPath); err != nil {
				log.Printf("namer bridge reconcile: nfo import failed for %s: %v", nfoAbsPath, err)
			}
		}
	}

	s.syncNFO(ctx, mediaFileID, mediaItemID, newRelativePath, newAbsPath)

	if _, err := s.ThumbnailJobs.Enqueue(ctx, "THUMBNAIL", ThumbnailJobPayload{MediaFileID: mediaFileID}); err != nil {
		log.Printf("namer bridge reconcile: failed to enqueue thumbnail for media_file %d: %v", mediaFileID, err)
	}

	return nil
}

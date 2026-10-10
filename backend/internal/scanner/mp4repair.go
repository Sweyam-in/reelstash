package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/diskinfo"
	"reelstash/backend/internal/jobs"
)

// MP4RepairResult is what the MP4_REPAIR job reports.
type MP4RepairResult struct {
	Checked          int      `json:"checked"`
	Repaired         int      `json:"repaired"`
	Failed           int      `json:"failed"`
	SkippedLocations []string `json:"skippedLocations"`
}

// mp4RepairTimeout bounds one rewrite. It is a stream copy, so it runs at disk
// speed; this only catches a wedged mount.
const mp4RepairTimeout = 30 * time.Minute

// mp4RepairSuffix marks the rewrite in progress next to the original. It is
// not a video extension, so neither the scanner nor the folder watcher reacts.
const mp4RepairSuffix = ".reelstash-repair"

// enqueueMP4Repair queues an MP4_REPAIR for a storage location that has MP4s
// with a scattered (or not yet checked) box layout, unless one is active.
func (s *Scanner) enqueueMP4Repair(ctx context.Context, storageLocationID int64) {
	var needed bool
	err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM media_file
			WHERE storage_location_id = $1 AND available AND lower(extension) IN ('mp4', 'm4v', 'mov')
			  AND (mp4_data_boxes IS NULL OR mp4_data_boxes > $2)
		) AND NOT EXISTS (
			SELECT 1 FROM background_job WHERE kind = 'MP4_REPAIR' AND status IN ('PENDING', 'RUNNING')
		)`, storageLocationID, ScatteredDataBoxes).Scan(&needed)
	if err != nil || !needed {
		return
	}
	if _, err := s.Jobs.Enqueue(ctx, "MP4_REPAIR", map[string]any{"storageLocationId": storageLocationID}); err != nil {
		log.Printf("scanner: enqueue mp4 repair for storage location %d: %v", storageLocationID, err)
	}
}

// handleMP4RepairJob finds MP4s whose media data is split across many
// top-level boxes and rewrites each in place as a stream copy with a single
// data box (ffmpeg -c copy -movflags +faststart). Nothing is re-encoded: the
// rewritten file holds the same streams, and is only swapped in after it
// probes the same as the original. Files not checked yet (scanned before the
// layout was recorded) are checked first.
//
// A storage location is skipped while a scan or the organizer is working on it.
func (s *Scanner) handleMP4RepairJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var only struct {
		StorageLocationID int64 `json:"storageLocationId"`
	}
	_ = json.Unmarshal(payload, &only)
	res := MP4RepairResult{SkippedLocations: []string{}}

	rows, err := db.Query(ctx, `
		SELECT sl.id, sl.name,
		       EXISTS (SELECT 1 FROM scan_job sj WHERE sj.storage_location_id = sl.id AND sj.completed_at IS NULL)
		    OR EXISTS (SELECT 1 FROM background_job bj WHERE bj.kind = 'ORGANIZE' AND bj.status IN ('PENDING', 'RUNNING')
		               AND bj.payload->>'storageLocationId' = sl.id::text)
		FROM storage_location sl WHERE ($1::bigint = 0 OR sl.id = $1) ORDER BY sl.id`, only.StorageLocationID)
	if err != nil {
		return nil, err
	}
	var locIDs []int64
	for rows.Next() {
		var id int64
		var name string
		var busy bool
		if err := rows.Scan(&id, &name, &busy); err != nil {
			rows.Close()
			return nil, err
		}
		if busy {
			res.SkippedLocations = append(res.SkippedLocations, name)
			continue
		}
		locIDs = append(locIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type candidate struct {
		id    int64
		path  string
		boxes *int
	}
	rows, err = db.Query(ctx, `
		SELECT mf.id, sl.mount_path, mf.relative_path, mf.mp4_data_boxes
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.storage_location_id = ANY($1) AND mf.available AND lower(mf.extension) IN ('mp4', 'm4v', 'mov')
		  AND (mf.mp4_data_boxes IS NULL OR mf.mp4_data_boxes > $2)
		ORDER BY mf.id`, locIDs, ScatteredDataBoxes)
	if err != nil {
		return nil, err
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var mount, rel string
		if err := rows.Scan(&c.id, &mount, &rel, &c.boxes); err != nil {
			rows.Close()
			return nil, err
		}
		c.path = filepath.Join(mount, filepath.FromSlash(rel))
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, c := range cands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		jobs.ReportProgress(db, jobID, i, len(cands), c.path)
		res.Checked++
		boxes := c.boxes
		if boxes == nil {
			if boxes = mp4DataBoxes(c.path); boxes == nil {
				continue // unreadable now; the next run tries again
			}
			if _, err := db.Exec(ctx, `UPDATE media_file SET mp4_data_boxes = $2 WHERE id = $1`, c.id, *boxes); err != nil {
				return nil, err
			}
		}
		if *boxes <= ScatteredDataBoxes {
			continue
		}
		if err := s.repairMP4File(ctx, db, c.id, c.path); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			log.Printf("mp4 repair: %s: %v", c.path, err)
			res.Failed++
			continue
		}
		res.Repaired++
	}
	jobs.ReportProgress(db, jobID, len(cands), len(cands), "")
	return res, nil
}

// repairMP4File rewrites one file with a single data box and records the new
// file on its media_file row, so the next scan sees it as unchanged.
func (s *Scanner) repairMP4File(ctx context.Context, db *pgxpool.Pool, fileID int64, path string) error {
	info, err := RewriteMP4(ctx, path)
	if err != nil {
		return err
	}
	modTime := info.ModTime().Truncate(time.Microsecond)
	partial, err := PartialHash(path, info.Size())
	if err != nil {
		return fmt.Errorf("partial hash: %w", err)
	}
	boxes := mp4DataBoxes(path)
	_, err = db.Exec(ctx, `
		UPDATE media_file SET file_size = $2, partial_hash = $3, content_hash = NULL, modified_time = $4,
			mp4_data_boxes = $5, updated_at = now()
		WHERE id = $1`, fileID, info.Size(), partial, modTime, boxes)
	return err
}

// RewriteMP4 replaces an MP4/MOV with a stream copy of itself whose media
// data is one box after the index, keeping its permissions, owner and
// modification time. The original is only replaced once the copy probes with
// the same duration, video codec and tracks.
func RewriteMP4(ctx context.Context, path string) (os.FileInfo, error) {
	orig, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	// The copy sits next to the original until it replaces it.
	if usage, err := diskinfo.Stat(dir, diskinfo.DefaultTimeout); err == nil && usage.Available > 0 &&
		usage.Available < orig.Size()+512<<20 {
		return nil, errors.New("not enough free space for the rewrite")
	}
	before, err := Probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe original: %w", err)
	}

	tmp := filepath.Join(dir, "."+filepath.Base(path)+mp4RepairSuffix)
	defer os.Remove(tmp) // a no-op once renamed into place
	cctx, cancel := context.WithTimeout(ctx, mp4RepairTimeout)
	defer cancel()
	args := []string{"-nostdin", "-v", "error", "-y", "-i", path,
		"-map", "0", "-dn", "-ignore_unknown", "-c", "copy", "-map_metadata", "0",
		"-movflags", "+faststart", "-f", "mp4", tmp}
	if out, err := exec.CommandContext(cctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}

	after, err := Probe(ctx, tmp)
	if err != nil {
		return nil, fmt.Errorf("probe rewrite: %w", err)
	}
	if err := sameMedia(before, after); err != nil {
		return nil, err
	}
	if n, err := CountMP4DataBoxes(tmp, dataBoxCountLimit); err != nil || n > ScatteredDataBoxes {
		return nil, fmt.Errorf("rewrite still has %d data boxes (%v)", n, err)
	}

	if err := os.Chmod(tmp, orig.Mode().Perm()); err != nil {
		return nil, err
	}
	copyOwner(tmp, orig)
	if err := os.Chtimes(tmp, orig.ModTime(), orig.ModTime()); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return os.Stat(path)
}

// sameMedia checks that a rewrite kept what matters from the original.
func sameMedia(before, after ProbeResult) error {
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	if str(before.VideoCodec) != str(after.VideoCodec) || str(before.AudioCodec) != str(after.AudioCodec) {
		return errors.New("rewrite changed the codecs")
	}
	if len(before.Tracks) != len(after.Tracks) {
		return fmt.Errorf("rewrite has %d tracks, original %d", len(after.Tracks), len(before.Tracks))
	}
	if before.DurationSeconds != nil {
		if after.DurationSeconds == nil || math.Abs(*before.DurationSeconds-*after.DurationSeconds) > 1 {
			return errors.New("rewrite has a different duration")
		}
	}
	return nil
}

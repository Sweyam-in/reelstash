package scanner

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
)

// TrackScanResult is what the TRACK_SCAN job reports.
type TrackScanResult struct {
	Checked       int `json:"checked"`
	Probed        int `json:"probed"`
	Failed        int `json:"failed"`
	WithSubtitles int `json:"withSubtitles"`
}

// trackScanWorkers bounds concurrent ffprobe runs. Reading the stream list
// touches only the start of a file, so this is mostly network latency.
const trackScanWorkers = 4

// enqueueTrackScan queues a TRACK_SCAN for a storage location after a scan,
// unless one is already waiting or running. It reads the tracks of files that
// have none recorded and recounts subtitle files beside every video, which a
// scan does not notice for videos that did not change.
func (s *Scanner) enqueueTrackScan(ctx context.Context, storageLocationID int64) {
	var active bool
	if err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM background_job WHERE kind = 'TRACK_SCAN' AND status IN ('PENDING', 'RUNNING'))`).Scan(&active); err != nil || active {
		return
	}
	if _, err := s.Jobs.Enqueue(ctx, "TRACK_SCAN", map[string]any{"storageLocationId": storageLocationID}); err != nil {
		log.Printf("scanner: enqueue track scan for storage location %d: %v", storageLocationID, err)
	}
}

// BackfillRotation queues one TRACK_SCAN when files were probed before their
// rotation was recorded, so phone videos stored as a rotated landscape frame
// join the reels without waiting for the next library scan.
func (s *Scanner) BackfillRotation(ctx context.Context) {
	var pending bool
	if err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media_file WHERE available AND rotation IS NULL AND tracks_probed_at IS NOT NULL)`).Scan(&pending); err != nil || !pending {
		return
	}
	s.enqueueTrackScan(ctx, 0)
}

// handleTrackScanJob reads the audio and subtitle tracks (and the rotation,
// which decides whether a video is a reel) of every available file that has
// none recorded (files scanned before tracks were stored are
// otherwise only read the first time they play) and recounts the subtitle
// files lying next to each video. An optional storageLocationId narrows it to
// one location.
func (s *Scanner) handleTrackScanJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var only struct {
		StorageLocationID int64 `json:"storageLocationId"`
	}
	_ = json.Unmarshal(payload, &only)

	type candidate struct {
		id     int64
		path   string
		probed bool
	}
	rows, err := db.Query(ctx, `
		SELECT mf.id, sl.mount_path, mf.relative_path, mf.tracks_probed_at IS NOT NULL AND mf.rotation IS NOT NULL
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.available AND ($1::bigint = 0 OR mf.storage_location_id = $1)
		ORDER BY mf.tracks_probed_at IS NOT NULL AND mf.rotation IS NOT NULL, mf.id`, only.StorageLocationID)
	if err != nil {
		return nil, err
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var mount, rel string
		if err := rows.Scan(&c.id, &mount, &rel, &c.probed); err != nil {
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

	var (
		mu   sync.Mutex
		res  TrackScanResult
		done int
	)
	work := make(chan candidate)
	var wg sync.WaitGroup
	for i := 0; i < trackScanWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				probed, failed := false, false
				if !c.probed {
					pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
					pr, perr := Probe(pctx, c.path)
					cancel()
					if perr != nil {
						if ctx.Err() == nil {
							log.Printf("track scan: %s: %v", c.path, perr)
						}
						failed = true
					} else if err := SaveTracks(ctx, db, c.id, pr.Tracks, true); err != nil {
						log.Printf("track scan: save tracks for %s: %v", c.path, err)
						failed = true
					} else {
						probed = true
						if err := SaveRotation(ctx, db, c.id, pr); err != nil {
							log.Printf("track scan: save rotation for %s: %v", c.path, err)
						}
					}
				}
				if err := SaveSidecarCount(ctx, db, c.id, len(FindSidecars(c.path))); err != nil {
					log.Printf("track scan: save subtitle count for %s: %v", c.path, err)
				}
				mu.Lock()
				res.Checked++
				if probed {
					res.Probed++
				}
				if failed {
					res.Failed++
				}
				done++
				n := done
				mu.Unlock()
				if n%10 == 0 || n == len(cands) {
					jobs.ReportProgress(db, jobID, n, len(cands), c.path)
				}
			}
		}()
	}
feed:
	for _, c := range cands {
		select {
		case work <- c:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Files with subtitles they can show: text tracks inside or files beside.
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM media_file mf
		WHERE mf.available AND ($1::bigint = 0 OR mf.storage_location_id = $1)
		  AND (COALESCE(mf.sidecar_subtitles, 0) > 0
		       OR EXISTS (SELECT 1 FROM media_track t WHERE t.media_file_id = mf.id AND t.kind = 'subtitle' AND t.codec = ANY($2)))`,
		only.StorageLocationID, TextSubtitleCodecList()).Scan(&res.WithSubtitles); err != nil {
		return nil, err
	}
	return res, nil
}

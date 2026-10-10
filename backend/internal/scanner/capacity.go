package scanner

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/diskinfo"
)

// capacityRetention is how long hourly capacity readings are kept.
const capacityRetention = 400 * 24 * time.Hour

// recordCapacity stores a disk's current capacity on its row and as a
// history point for the storage charts.
func recordCapacity(ctx context.Context, db *pgxpool.Pool, storageLocationID int64, u diskinfo.Usage) {
	if _, err := db.Exec(ctx, `
		UPDATE storage_location SET total_capacity = $2, used_capacity = $3, available_capacity = $4
		WHERE id = $1
	`, storageLocationID, u.Total, u.Used, u.Available); err != nil {
		log.Printf("scanner: save capacity for storage location %d: %v", storageLocationID, err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO storage_capacity_sample (storage_location_id, total_bytes, used_bytes, available_bytes)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING
	`, storageLocationID, u.Total, u.Used, u.Available); err != nil {
		log.Printf("scanner: record capacity sample for storage location %d: %v", storageLocationID, err)
	}
}

// RunCapacitySampler reads every storage location's capacity now and then
// every interval until ctx ends, keeping online status current between scans.
func (s *Scanner) RunCapacitySampler(ctx context.Context, interval time.Duration) {
	s.SampleCapacity(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SampleCapacity(ctx)
		}
	}
}

// SampleCapacity records one capacity reading for each storage location. A
// location whose mount can't be read, or that is empty while the library
// has files on it (an unmounted disk leaves its mount point behind), is
// marked offline; a readable one is marked online.
func (s *Scanner) SampleCapacity(ctx context.Context) {
	rows, err := s.DB.Query(ctx, `
		SELECT sl.id, sl.mount_path,
		       EXISTS (SELECT 1 FROM media_file mf WHERE mf.storage_location_id = sl.id AND mf.available)
		FROM storage_location sl`)
	if err != nil {
		log.Printf("scanner: capacity sampler: %v", err)
		return
	}
	type location struct {
		id       int64
		path     string
		hasFiles bool
	}
	var locations []location
	for rows.Next() {
		var l location
		if err := rows.Scan(&l.id, &l.path, &l.hasFiles); err != nil {
			rows.Close()
			log.Printf("scanner: capacity sampler: %v", err)
			return
		}
		locations = append(locations, l)
	}
	rows.Close()

	for _, l := range locations {
		if ctx.Err() != nil {
			return
		}
		usage, err := diskinfo.Stat(l.path, fsOpTimeout)
		online := err == nil
		if online && l.hasFiles {
			online, _ = diskinfo.HasEntries(l.path, fsOpTimeout)
		}
		if !online {
			s.DB.Exec(ctx, `UPDATE storage_location SET online = FALSE WHERE id = $1`, l.id)
			continue
		}
		s.DB.Exec(ctx, `UPDATE storage_location SET online = TRUE, last_seen_at = now() WHERE id = $1`, l.id)
		recordCapacity(ctx, s.DB, l.id, usage)
	}
	s.DB.Exec(ctx, `DELETE FROM storage_capacity_sample WHERE sampled_at < $1`, time.Now().Add(-capacityRetention))
}

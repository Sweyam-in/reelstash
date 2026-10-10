package scanner

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
)

// RemoveMissingResult is what the REMOVE_MISSING job reports.
type RemoveMissingResult struct {
	RemovedFiles      int      `json:"removedFiles"`
	RemovedTitles     int      `json:"removedTitles"`
	Kept              int      `json:"kept"`
	SkippedLocations  []string `json:"skippedLocations"`
	SkippedLocationsN int      `json:"skippedLocationCount"`
}

// handleRemoveMissingJob drops index entries for files that a scan flagged
// missing and that are still absent from disk. A title left with no files at
// all is removed with them (its watch history, ratings and playlist entries
// go too); a title that still has an available copy only loses the stale one.
// Nothing on disk is touched.
//
// Unreadable is not the same as deleted, so a storage location is skipped
// while a scan is running on it, when its mount is not a readable directory,
// or when its last scan did not finish walking it.
func (s *Scanner) handleRemoveMissingJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	// An optional storageLocationId limits the cleanup to one location.
	var only struct {
		StorageLocationID int64 `json:"storageLocationId"`
	}
	_ = json.Unmarshal(payload, &only)
	res := RemoveMissingResult{SkippedLocations: []string{}}

	locs, err := db.Query(ctx, `SELECT id, name, mount_path FROM storage_location WHERE ($1::bigint = 0 OR id = $1) ORDER BY id`, only.StorageLocationID)
	if err != nil {
		return nil, err
	}
	type location struct {
		id          int64
		name, mount string
	}
	var all []location
	for locs.Next() {
		var l location
		if err := locs.Scan(&l.id, &l.name, &l.mount); err != nil {
			locs.Close()
			return nil, err
		}
		all = append(all, l)
	}
	locs.Close()
	if err := locs.Err(); err != nil {
		return nil, err
	}

	var safe []location
	for _, l := range all {
		if !s.safeToRemoveMissing(ctx, l.id, l.mount) {
			res.SkippedLocations = append(res.SkippedLocations, l.name)
			continue
		}
		safe = append(safe, l)
	}
	res.SkippedLocationsN = len(res.SkippedLocations)

	type candidate struct {
		fileID, itemID int64
		abs            string
	}
	var cands []candidate
	for _, l := range safe {
		rows, err := db.Query(ctx, `SELECT id, media_item_id, relative_path FROM media_file WHERE storage_location_id = $1 AND NOT available`, l.id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c candidate
			var rel string
			if err := rows.Scan(&c.fileID, &c.itemID, &rel); err != nil {
				rows.Close()
				return nil, err
			}
			c.abs = filepath.Join(l.mount, filepath.FromSlash(rel))
			cands = append(cands, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	items := map[int64]bool{}
	for i, c := range cands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i%25 == 0 {
			jobs.ReportProgress(db, jobID, i, len(cands), "")
		}
		// Re-check the disk: the file may have come back since the scan.
		if _, err := os.Lstat(c.abs); !os.IsNotExist(err) {
			res.Kept++
			continue
		}
		tag, err := db.Exec(ctx, `DELETE FROM media_file WHERE id = $1 AND NOT available`, c.fileID)
		if err != nil {
			return nil, err
		}
		res.RemovedFiles += int(tag.RowsAffected())
		items[c.itemID] = true
	}

	for itemID := range items {
		removed, err := s.removeTitleIfEmpty(ctx, db, itemID)
		if err != nil {
			return nil, err
		}
		if removed {
			res.RemovedTitles++
		}
	}
	jobs.ReportProgress(db, jobID, len(cands), len(cands), "")
	log.Printf("remove missing: %d file(s), %d title(s) removed, %d kept, %d location(s) skipped", res.RemovedFiles, res.RemovedTitles, res.Kept, res.SkippedLocationsN)
	return res, nil
}

// safeToRemoveMissing reports whether a location's "missing" flags can be
// trusted: no scan in flight, the mount is a readable directory, and the
// latest finished scan walked it completely.
func (s *Scanner) safeToRemoveMissing(ctx context.Context, locationID int64, mount string) bool {
	var scanning bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM scan_job WHERE storage_location_id = $1 AND completed_at IS NULL)`, locationID).Scan(&scanning); err != nil || scanning {
		return false
	}
	if st, err := os.Stat(mount); err != nil || !st.IsDir() {
		return false
	}
	if _, err := os.ReadDir(mount); err != nil {
		return false
	}
	var walked bool
	err := s.DB.QueryRow(ctx, `
		SELECT walk_complete FROM scan_job
		WHERE storage_location_id = $1 AND completed_at IS NOT NULL
		ORDER BY id DESC LIMIT 1
	`, locationID).Scan(&walked)
	return err == nil && walked
}

// removeTitleIfEmpty deletes the title (and its uploaded artwork) once it has
// no files left. It reports whether it did.
func (s *Scanner) removeTitleIfEmpty(ctx context.Context, db *pgxpool.Pool, itemID int64) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var remaining int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM media_file WHERE media_item_id = $1`, itemID).Scan(&remaining); err != nil || remaining > 0 {
		return false, err
	}
	var keys []string
	rows, err := tx.Query(ctx, `
		SELECT a.storage_key FROM artwork a
		JOIN media_item mi ON a.id IN (mi.poster_image_id, mi.backdrop_image_id)
		WHERE mi.id = $1`, itemID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			keys = append(keys, k)
		}
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `
		DELETE FROM artwork WHERE id IN (SELECT poster_image_id FROM media_item WHERE id = $1 UNION SELECT backdrop_image_id FROM media_item WHERE id = $1)`, itemID); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM media_item WHERE id = $1`, itemID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	if s.PosterDir != "" {
		for _, k := range keys {
			if filepath.Base(k) == k {
				os.Remove(filepath.Join(s.PosterDir, k))
			}
		}
	}
	return tag.RowsAffected() > 0, nil
}

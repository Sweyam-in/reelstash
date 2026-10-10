package scanner

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DuplicateScanResult is persisted on the DUPLICATE_SCAN background_job
// while it runs and again when it finishes.
type DuplicateScanResult struct {
	Phase       string `json:"phase"`
	Hashed      int    `json:"hashed"`
	Failed      int    `json:"failed"`
	Total       int    `json:"total"`
	CurrentPath string `json:"currentPath,omitempty"`
}

func (s *Scanner) handleDuplicateScanJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	return s.ScanDuplicates(ctx, jobID)
}

// ScanDuplicates fills in content_hash for available files that do not have
// one yet (a library scan never reads a whole file), then rebuilds exact
// and probable duplicate groups. Files that already have a hash are left
// alone, so a second run only reads new or changed files.
func (s *Scanner) ScanDuplicates(ctx context.Context, jobID int64) (DuplicateScanResult, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT mf.id, sl.mount_path, mf.relative_path
		FROM media_file mf
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.available AND mf.content_hash IS NULL
		ORDER BY mf.id
	`)
	if err != nil {
		return DuplicateScanResult{}, err
	}

	type pendingFile struct {
		id   int64
		path string
	}
	var pending []pendingFile
	for rows.Next() {
		var id int64
		var mountPath, relPath string
		if err := rows.Scan(&id, &mountPath, &relPath); err != nil {
			rows.Close()
			return DuplicateScanResult{}, err
		}
		pending = append(pending, pendingFile{
			id:   id,
			path: filepath.Join(mountPath, filepath.FromSlash(relPath)),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return DuplicateScanResult{}, err
	}
	rows.Close()

	progress := DuplicateScanResult{Phase: "hashing", Total: len(pending)}
	var mu sync.Mutex
	flushProgress := func() {
		if jobID == 0 {
			return
		}
		mu.Lock()
		snap := progress
		mu.Unlock()
		raw, err := json.Marshal(snap)
		if err != nil {
			return
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.DB.Exec(writeCtx, `
			UPDATE background_job SET result = $2
			WHERE id = $1 AND status = 'RUNNING'
		`, jobID, raw)
	}
	flushProgress()

	done := make(chan struct{})
	var stopOnce sync.Once
	stopFlush := func() { stopOnce.Do(func() { close(done) }) }
	defer stopFlush()
	if len(pending) > 0 {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					flushProgress()
				}
			}
		}()
	}

	sem := make(chan struct{}, s.FileWorkers)
	var wg sync.WaitGroup
	for _, file := range pending {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(file pendingFile) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			mu.Lock()
			progress.CurrentPath = file.path
			mu.Unlock()

			sum, hashErr := FullHash(file.path)
			if hashErr != nil {
				mu.Lock()
				progress.Failed++
				mu.Unlock()
				log.Printf("scanner: duplicate scan failed to hash %s: %v", file.path, hashErr)
				return
			}
			if _, err := s.DB.Exec(ctx, `UPDATE media_file SET content_hash = $2, updated_at = now() WHERE id = $1`, file.id, sum); err != nil {
				mu.Lock()
				progress.Failed++
				mu.Unlock()
				log.Printf("scanner: duplicate scan failed to store hash for media_file %d: %v", file.id, err)
				return
			}
			mu.Lock()
			progress.Hashed++
			mu.Unlock()
		}(file)
	}
	wg.Wait()
	stopFlush()

	if err := ctx.Err(); err != nil {
		flushProgress()
		return progress, err
	}

	progress.Phase = "grouping"
	progress.CurrentPath = ""
	flushProgress()

	if err := DetectDuplicates(ctx, s.DB); err != nil {
		return progress, err
	}
	progress.Phase = "done"
	flushProgress()
	return progress, nil
}

// DetectDuplicates groups available media_file rows that share a full
// content_hash into EXACT duplicate_group entries, and rows that share a
// partial_hash + file_size but belong to different media_items into
// PROBABLE groups. It never deletes files; it only records groups for the
// Duplicate Center to review. Existing dismissed groups are left alone.
func DetectDuplicates(ctx context.Context, db *pgxpool.Pool) error {
	if err := groupBy(ctx, db, "EXACT", `
		SELECT content_hash AS key, array_agg(id ORDER BY id) AS file_ids
		FROM media_file
		WHERE available AND content_hash IS NOT NULL
		GROUP BY content_hash
		HAVING COUNT(*) > 1
	`); err != nil {
		return err
	}

	return groupBy(ctx, db, "PROBABLE", `
		SELECT partial_hash || ':' || file_size AS key, array_agg(id ORDER BY id) AS file_ids
		FROM media_file
		WHERE available AND partial_hash IS NOT NULL
		GROUP BY partial_hash, file_size
		HAVING COUNT(*) > 1
	`)
}

func groupBy(ctx context.Context, db *pgxpool.Pool, kind, query string) error {
	rows, err := db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	type group struct {
		key     string
		fileIDs []int64
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.key, &g.fileIDs); err != nil {
			return err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, g := range groups {
		var existingCount int
		err := db.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM duplicate_group_file dgf
			JOIN duplicate_group dg ON dg.id = dgf.duplicate_group_id
			WHERE dg.kind = $1 AND NOT dg.dismissed AND dgf.media_file_id = ANY($2)
			GROUP BY dgf.duplicate_group_id
			HAVING COUNT(*) = $3
			LIMIT 1
		`, kind, g.fileIDs, len(g.fileIDs)).Scan(&existingCount)
		if err == nil && existingCount == len(g.fileIDs) {
			continue // an up-to-date group already covers exactly this file set
		}

		var groupID int64
		confidence := 1.0
		if kind == "PROBABLE" {
			confidence = 0.75
		}
		if err := db.QueryRow(ctx, `
			INSERT INTO duplicate_group (kind, confidence) VALUES ($1, $2) RETURNING id
		`, kind, confidence).Scan(&groupID); err != nil {
			return err
		}
		for _, fileID := range g.fileIDs {
			if _, err := db.Exec(ctx, `
				INSERT INTO duplicate_group_file (duplicate_group_id, media_file_id)
				VALUES ($1, $2) ON CONFLICT DO NOTHING
			`, groupID, fileID); err != nil {
				return err
			}
		}
	}
	return nil
}

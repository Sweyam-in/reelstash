package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// quietRoots lists library folders that still have indexed videos but where
// this walk saw none. An empty result means every previously indexed file
// that is still available was seen, or there were none.
func quietRoots(known map[string]knownFile, seen map[string]bool, roots []string, excluder *RuleSet) []string {
	var quiet []string
	for _, root := range roots {
		had := false
		for rel, k := range known {
			if !k.available || (excluder != nil && excluder.Excluded(rel)) || !pathWithin(rel, root) {
				continue
			}
			had = true
			break
		}
		if !had {
			continue
		}
		saw := false
		for rel := range seen {
			if pathWithin(rel, root) {
				saw = true
				break
			}
		}
		if !saw {
			quiet = append(quiet, root)
		}
	}
	return quiet
}

// mountRel is a path relative to the storage mount, using forward slashes.
// A path outside the mount is ignored.
func mountRel(mountPath, path string) string {
	rel, err := filepath.Rel(mountPath, path)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// prepareScan loads the mount, marks it offline when it cannot be read, and
// resolves which folders a library scan is allowed to walk.
func (s *Scanner) prepareScan(ctx context.Context, storageLocationID, libraryID int64) (mountPath string, excluder *RuleSet, scope libraryScope, err error) {
	if err = s.DB.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, storageLocationID).Scan(&mountPath); err != nil {
		return "", nil, libraryScope{}, fmt.Errorf("load storage location: %w", err)
	}
	info, statErr := statWithTimeout(mountPath, fsOpTimeout)
	if statErr != nil || !info.IsDir() {
		s.DB.Exec(ctx, `UPDATE storage_location SET online = FALSE WHERE id = $1`, storageLocationID)
		return "", nil, libraryScope{}, fmt.Errorf("storage location %d is not reachable at %s: %w", storageLocationID, mountPath, statErr)
	}
	excluder, err = LoadRules(ctx, s.DB, storageLocationID)
	if err != nil {
		return "", nil, libraryScope{}, fmt.Errorf("load scan rules: %w", err)
	}
	scope = libraryScope{roots: []string{""}}
	if libraryID > 0 {
		if scope, err = s.loadLibraryScope(ctx, storageLocationID, libraryID); err != nil {
			return "", nil, libraryScope{}, err
		}
		// A library folder that is gone while the disk is mounted is an
		// error, not a reason to mark every file in it missing.
		for _, root := range scope.roots {
			dir := filepath.Join(mountPath, filepath.FromSlash(root))
			if info, err := statWithTimeout(dir, fsOpTimeout); err != nil || !info.IsDir() {
				return "", nil, libraryScope{}, fmt.Errorf("library folder %s is not reachable: %w", dir, err)
			}
		}
	}
	return mountPath, excluder, scope, nil
}

func (s *Scanner) insertScanJob(ctx context.Context, jobID, storageLocationID, libraryID int64) (int64, error) {
	var scanJobID int64
	var backgroundJobID *int64
	if jobID != 0 {
		backgroundJobID = &jobID
	}
	err := s.DB.QueryRow(ctx, `
		INSERT INTO scan_job (storage_location_id, background_job_id, library_id, started_at) VALUES ($1, $2, NULLIF($3, 0), now()) RETURNING id
	`, storageLocationID, backgroundJobID, libraryID).Scan(&scanJobID)
	return scanJobID, err
}

// scanWalk is what the directory walk and file workers produced.
type scanWalk struct {
	known     map[string]knownFile
	knownErr  error
	seen      map[string]bool
	nfos      map[string]bool
	dirErrs   []string
	unchanged []unchangedFile
	walkErr   error
}

// walkAndIndex walks the mount and indexes files until the walk finishes,
// the caller cancels, or no file is touched for the stall timeout.
func (s *Scanner) walkAndIndex(ctx, scanCtx context.Context, cancelScan func(), scanJobID, storageLocationID, libraryID int64, mountPath string, scope libraryScope, excluder *RuleSet, live *liveScan) scanWalk {
	// Known files load while the walk starts; workers wait for them.
	var known map[string]knownFile
	var knownErr error
	knownReady := make(chan struct{})
	go func() {
		defer close(knownReady)
		known, knownErr = s.loadKnownFiles(scanCtx, storageLocationID, libraryID)
		if knownErr != nil {
			cancelScan()
		}
	}()

	var mu sync.Mutex // guards seen, nfos, dirErrs, unchanged
	seen := make(map[string]bool)
	nfos := make(map[string]bool)
	var dirErrs []string
	var unchanged []unchangedFile

	// findOrCreateMediaItem's check-then-insert (match by partial hash+size,
	// else create) is not safe for concurrent callers: two workers hashing
	// distinct copies of the same title in the same scan could both miss
	// each other and create duplicate media_item rows. Everything else
	// per-file (hashing, ffprobe, the media_file upsert) is independent
	// across files, so only that step is serialized.
	var dedupMu sync.Mutex

	tasks := make(chan fileTask, 4*s.FileWorkers)
	progress := make(chan struct{}, 1)
	notifyProgress := func() {
		select {
		case progress <- struct{}{}:
		default:
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < s.FileWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-knownReady:
			case <-scanCtx.Done():
				return
			}
			if knownErr != nil {
				return
			}
			for {
				var task fileTask
				var ok bool
				select {
				case task, ok = <-tasks:
				case <-scanCtx.Done():
					return
				}
				if !ok {
					return
				}
				live.beginFile(task.relPath)
				prev, wasKnown := known[task.relPath]
				same, changed, isNew, err := s.indexFile(scanCtx, storageLocationID, task, prev, wasKnown, &dedupMu)
				if same != nil {
					mu.Lock()
					unchanged = append(unchanged, *same)
					mu.Unlock()
				}
				notifyProgress()
				live.finishFile(task.relPath, isNew, changed && err == nil)
				if err != nil {
					log.Printf("scanner: failed to index %s: %v", task.path, err)
				}
			}
		}()
	}

	walkErrCh := make(chan error, 1)
	go func() {
		skipDir := func(dir string) bool {
			rel := mountRel(mountPath, dir)
			return scope.skip[rel] || excluder.Excluded(rel)
		}
		onFile := func(path, name string) {
			if strings.HasPrefix(name, "._") || name == ".DS_Store" {
				return
			}
			relPath := mountRel(mountPath, path)
			if relPath == "" || excluder.Excluded(relPath) {
				return
			}
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".nfo" {
				mu.Lock()
				nfos[relPath] = true
				mu.Unlock()
				return
			}
			if !videoExtensions[ext] {
				return
			}
			mu.Lock()
			seen[relPath] = true
			mu.Unlock()
			live.noteSeen(relPath)
			notifyProgress()

			select {
			case tasks <- fileTask{path: path, relPath: relPath}:
			case <-scanCtx.Done():
			}
		}
		onDirErr := func(dir string, err error) {
			log.Printf("scanner: cannot read %s: %v", dir, err)
			if rel, relErr := filepath.Rel(mountPath, dir); relErr == nil {
				mu.Lock()
				dirErrs = append(dirErrs, filepath.ToSlash(rel)+"/")
				mu.Unlock()
			}
			notifyProgress()
		}
		for _, root := range scope.roots {
			if err := walkParallel(scanCtx, filepath.Join(mountPath, filepath.FromSlash(root)), max(4, s.FileWorkers), skipDir, onFile, onDirErr); err != nil {
				walkErrCh <- err
				return
			}
		}
		walkErrCh <- nil
	}()

	stallTimer := time.NewTimer(scanStallTimeout)
	defer stallTimer.Stop()

	var walkErr error
	walkFinished := false
waitLoop:
	for {
		select {
		case walkErr = <-walkErrCh:
			walkFinished = true
			break waitLoop
		case <-progress:
			if !stallTimer.Stop() {
				<-stallTimer.C
			}
			stallTimer.Reset(scanStallTimeout)
		case <-stallTimer.C:
			cancelScan()
			walkErr = fmt.Errorf("scan stalled: no file discovered or processed for %s (storage location may be unresponsive)", scanStallTimeout)
			break waitLoop
		case <-ctx.Done():
			cancelScan()
			walkErr = ctx.Err()
			break waitLoop
		}
	}

	if walkFinished {
		// The walk has returned, so nothing sends on tasks any more.
		live.markWalkComplete()
		s.flushScanProgress(scanJobID, live)
		close(tasks)
		wg.Wait()
	} else {
		// We gave up waiting (stall timeout or cancellation) before the walk
		// returned — a folder read may still be blocked inside a syscall on
		// the unresponsive mount. Abandon it rather than block here forever;
		// workers exit on scanCtx. Same documented tradeoff FullHash makes
		// for a single stalled read.
		log.Printf("scanner: abandoning stalled scan of storage location %d: %v", storageLocationID, walkErr)
	}
	<-knownReady
	return scanWalk{known: known, knownErr: knownErr, seen: seen, nfos: nfos, dirErrs: dirErrs, unchanged: unchanged, walkErr: walkErr}
}

// finishScan records missing files, writes the scan_job row, and queues the
// follow-up jobs. done is true only after the scan_job row was completed, so
// the caller's defer does not complete it a second time.
func (s *Scanner) finishScan(ctx context.Context, summary ScanSummary, scanJobID, storageLocationID, libraryID int64, mountPath string, scope libraryScope, excluder *RuleSet, live *liveScan, walk scanWalk) (ScanSummary, bool, error) {
	known, knownErr, seen, nfos, dirErrs, unchanged, walkErr := walk.known, walk.knownErr, walk.seen, walk.nfos, walk.dirErrs, walk.unchanged, walk.walkErr
	snap := live.snapshot()
	summary.Discovered = snap.discovered
	summary.Changed = snap.changed

	if knownErr != nil {
		return summary, false, fmt.Errorf("load indexed files: %w", knownErr)
	}
	if walkErr != nil {
		return summary, false, walkErr
	}

	// An empty folder is not proof the videos were deleted. A mount that
	// came back empty, or a library root that has none of the files it
	// used to, fails the scan instead of wiping those titles.
	quiet := quietRoots(known, seen, scope.roots, excluder)
	if len(quiet) > 0 && len(seen) == 0 {
		s.DB.Exec(ctx, `UPDATE storage_location SET online = FALSE WHERE id = $1`, storageLocationID)
		return summary, false, fmt.Errorf("no video files found under %s, but the library has indexed files there; is the disk mounted?", mountPath)
	}
	for _, root := range quiet {
		log.Printf("scanner: leaving %q alone; it has indexed videos but this scan saw none", root)
		dirErrs = append(dirErrs, root+"/")
	}

	live.setPhase("finishing")
	s.flushScanProgress(scanJobID, live)

	var finishWG sync.WaitGroup
	var touchErr, missingErr error
	finishWG.Add(4)
	go func() {
		defer finishWG.Done()
		touchErr = s.touchUnchanged(ctx, unchanged)
	}()
	go func() {
		defer finishWG.Done()
		s.syncUnchangedNFOs(ctx, unchanged, nfos)
	}()
	go func() {
		defer finishWG.Done()
		if libraryID > 0 {
			// A file indexed here may be the same title at a new path. Drop
			// the old record when that path is gone, on this disk or another,
			// and keep the one this scan just found.
			var replaced int
			replaced, missingErr = s.dropStaleCopies(ctx, storageLocationID, seen)
			if missingErr != nil {
				return
			}
			var flagged, files int
			flagged, files, summary.RemovedTitles, missingErr = s.removeMissing(ctx, known, seen, dirErrs, excluder)
			summary.RemovedFiles = replaced + files
			summary.Missing = flagged + summary.RemovedFiles
		} else {
			summary.Missing, missingErr = s.markMissing(ctx, known, seen, dirErrs, excluder)
		}
	}()
	go func() {
		defer finishWG.Done()
		s.refreshCapacity(ctx, storageLocationID, mountPath)
	}()
	finishWG.Wait()
	if touchErr != nil {
		return summary, false, touchErr
	}
	if missingErr != nil {
		return summary, false, missingErr
	}

	finalSnap := live.snapshot()
	recent, _ := json.Marshal(finalSnap.recent)
	s.DB.Exec(context.Background(), `
		UPDATE scan_job SET
			discovered_count = $2, changed_count = $3, missing_count = $4,
			files_seen = $5, files_processed = $6, current_path = NULL,
			recent_paths = $7, phase = 'done', walk_complete = TRUE, completed_at = now()
		WHERE id = $1
	`, scanJobID, summary.Discovered, summary.Changed, summary.Missing, finalSnap.filesSeen, finalSnap.filesProcessed, recent)
	// Only a whole-disk scan counts as the disk's last scan.
	s.DB.Exec(context.Background(), `
		UPDATE storage_location SET online = TRUE, last_seen_at = now(),
		       last_scan_at = CASE WHEN $2 THEN now() ELSE last_scan_at END
		WHERE id = $1
	`, storageLocationID, libraryID == 0)

	// Metadata lookup is one slow network call per file, so it runs as its
	// own job after the scan has been reported complete instead of holding
	// the scan open for hours on a large library.
	if s.Matcher != nil && s.Matcher.Enabled() {
		if _, err := s.Jobs.Enqueue(context.Background(), "METADATA_MATCH", map[string]any{"storageLocationId": storageLocationID}); err != nil {
			log.Printf("scanner: enqueue metadata match for storage location %d: %v", storageLocationID, err)
		}
	}
	s.enqueueMP4Repair(context.Background(), storageLocationID)
	s.enqueueTrackScan(context.Background(), storageLocationID)

	return summary, true, nil
}

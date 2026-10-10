// Package scanner implements the ReelStash incremental storage scanner:
// discover -> compare DB -> detect new/changed/missing -> ffprobe -> NFO ->
// thumbnail -> partial hash -> match MediaItem -> persist.
// Exact duplicate detection is a separate admin job (DUPLICATE_SCAN) because
// it has to read every byte of each file.
package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/diskinfo"
	"reelstash/backend/internal/jobs"
)

var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".m4v": true, ".wmv": true, ".ts": true, ".webm": true,
}

// IsVideoExt reports whether ext (with its dot, any case) is a file type the
// scanner indexes as a video.
func IsVideoExt(ext string) bool { return videoExtensions[strings.ToLower(ext)] }

// fsOpTimeout bounds a single blocking filesystem call (stat/open) so a
// stalled network mount can't wedge the scan before it even starts.
const fsOpTimeout = 30 * time.Second

// scanStallTimeout aborts a whole scan if no file finishes being discovered
// or processed for this long. Go can't cancel a syscall already in flight —
// same caveat as FullHash's timeout below — so a stall deep inside
// WalkDir's readdir/stat may still leak the walk goroutine, but the scan
// job itself always terminates and reports failure instead of sitting in
// "Running" forever.
const scanStallTimeout = 5 * time.Minute

// fileTask is one discovered file handed from the walk goroutine to a
// worker for hashing/probing/upserting.
type fileTask struct {
	path    string
	relPath string
}

type Scanner struct {
	DB   *pgxpool.Pool
	Jobs *jobs.Runner
	// ThumbnailJobs is a separate worker pool for THUMBNAIL jobs. It is
	// deliberately not the same Runner as Jobs (which runs SCAN): a
	// ScanStorageLocation call occupies its worker for the entire scan, so
	// sharing one pool means a slow/stalled scan starves thumbnail
	// generation (and vice versa) of every available worker.
	ThumbnailJobs *jobs.Runner
	ThumbnailDir  string
	// SpriteDir and PreviewDir hold scrub-preview sheets and hover clips.
	SpriteDir  string
	PreviewDir string
	// PosterDir holds uploaded poster and backdrop images.
	PosterDir string
	// FileWorkers bounds how many files a single scan hashes or probes at
	// once. The directory walk itself stays a single producer goroutine
	// (cheap, and keeps `seen` bookkeeping race-free); only the expensive
	// per-file work (ffprobe, and full SHA-256 during a duplicate scan)
	// fans out.
	FileWorkers int
	// Matcher looks up external metadata after a scan finishes indexing.
	// Nil or disabled means the scan only indexes files.
	Matcher MetadataMatcher
}

// MetadataMatcher fetches metadata for one already-indexed media file.
// status is "matched", "manual_required", or "no_match".
type MetadataMatcher interface {
	Enabled() bool
	Lookup(ctx context.Context, mediaFileID int64) (status string, err error)
}

func New(db *pgxpool.Pool, jobRunner, thumbnailJobRunner *jobs.Runner, thumbnailDir string, fileWorkers int) *Scanner {
	if fileWorkers < 1 {
		fileWorkers = 1
	}
	return &Scanner{DB: db, Jobs: jobRunner, ThumbnailJobs: thumbnailJobRunner, ThumbnailDir: thumbnailDir, FileWorkers: fileWorkers}
}

// RegisterJobs wires the scanner's background_job handlers into their
// runners. Call once at startup, before either Runner's ResumePending.
func (s *Scanner) RegisterJobs() {
	s.Jobs.Register("SCAN", s.handleScanJob)
	s.Jobs.Register("METADATA_MATCH", s.handleMetadataMatchJob)
	s.Jobs.Register("DUPLICATE_SCAN", s.handleDuplicateScanJob)
	s.Jobs.Register("SPRITE_SCAN", s.handleSpriteScanJob)
	s.Jobs.Register("PREVIEW_SCAN", s.handlePreviewScanJob)
	s.Jobs.Register("THUMBNAIL_FIX", s.handleThumbnailFixJob)
	s.Jobs.Register("REMOVE_MISSING", s.handleRemoveMissingJob)
	s.Jobs.Register("MP4_REPAIR", s.handleMP4RepairJob)
	s.Jobs.Register("TRACK_SCAN", s.handleTrackScanJob)
	s.ThumbnailJobs.Register("THUMBNAIL", s.handleThumbnailJob)
	s.ThumbnailJobs.Register("SPRITE", s.handleSpriteJob)
	s.ThumbnailJobs.Register("PREVIEW", s.handlePreviewJob)
}

type ScanJobPayload struct {
	StorageLocationID int64 `json:"storageLocationId"`
	// LibraryID narrows the scan to that library's folders on the disk; 0
	// scans the whole disk.
	LibraryID int64 `json:"libraryId,omitempty"`
}

type ScanSummary struct {
	StorageLocationID int64 `json:"storageLocationId"`
	LibraryID         int64 `json:"libraryId,omitempty"`
	Discovered        int   `json:"discovered"`
	Changed           int   `json:"changed"`
	Missing           int   `json:"missing"`
	// A library scan flags a file the first time it disappears and deletes
	// it only when a later scan still cannot see it. Titles left without
	// any file are deleted with that second pass.
	RemovedFiles  int `json:"removedFiles,omitempty"`
	RemovedTitles int `json:"removedTitles,omitempty"`
}

// liveScan is the in-memory view of a running scan. A flusher copies it
// into scan_job about once a second so the admin UI can show progress
// without waiting for the scan to finish.
type liveScan struct {
	mu             sync.Mutex
	filesSeen      int
	filesProcessed int
	discovered     int
	changed        int
	currentPath    string
	recent         []string
	phase          string
	walkComplete   bool
}

type liveScanSnap struct {
	filesSeen      int
	filesProcessed int
	discovered     int
	changed        int
	currentPath    string
	recent         []string
	phase          string
	walkComplete   bool
}

func (l *liveScan) snapshot() liveScanSnap {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := append([]string(nil), l.recent...)
	if recent == nil {
		recent = []string{}
	}
	return liveScanSnap{
		filesSeen: l.filesSeen, filesProcessed: l.filesProcessed,
		discovered: l.discovered, changed: l.changed,
		currentPath: l.currentPath, recent: recent,
		phase: l.phase, walkComplete: l.walkComplete,
	}
}

func (l *liveScan) noteSeen(rel string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.filesSeen++
	if l.currentPath == "" {
		l.currentPath = rel
	}
}

func (l *liveScan) beginFile(rel string) {
	l.mu.Lock()
	l.currentPath = rel
	l.mu.Unlock()
}

func (l *liveScan) finishFile(rel string, isNew, changed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.filesProcessed++
	if isNew {
		l.discovered++
	} else if changed {
		l.changed++
	}
	l.currentPath = rel
	l.recent = append([]string{rel}, l.recent...)
	if len(l.recent) > 12 {
		l.recent = l.recent[:12]
	}
}

func (l *liveScan) markWalkComplete() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.walkComplete = true
	if l.phase == "walking" {
		l.phase = "indexing"
	}
}

func (l *liveScan) setPhase(phase string) {
	l.mu.Lock()
	l.phase = phase
	l.mu.Unlock()
}

func (l *liveScan) setCurrent(path string) {
	l.mu.Lock()
	l.currentPath = path
	l.mu.Unlock()
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Scanner) startScanProgressFlush(scanJobID int64, live *liveScan) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				s.flushScanProgress(scanJobID, live)
			}
		}
	}()
	return func() { close(done) }
}

func (s *Scanner) flushScanProgress(scanJobID int64, live *liveScan) {
	snap := live.snapshot()
	recent, err := json.Marshal(snap.recent)
	if err != nil {
		recent = []byte("[]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.DB.Exec(ctx, `
		UPDATE scan_job SET
			discovered_count = $2, changed_count = $3,
			files_seen = $4, files_processed = $5, current_path = $6,
			recent_paths = $7, phase = $8, walk_complete = $9
		WHERE id = $1 AND completed_at IS NULL
	`, scanJobID, snap.discovered, snap.changed, snap.filesSeen, snap.filesProcessed, nullIfEmpty(snap.currentPath), recent, snap.phase, snap.walkComplete)
}

func (s *Scanner) handleScanJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var p ScanJobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	return s.scan(ctx, jobID, p.StorageLocationID, p.LibraryID)
}

// knownFile is a media_file row as it stood when a scan started.
type knownFile struct {
	id, itemID int64
	size       int64
	modTime    time.Time
	available  bool
	nfoSynced  bool // its NFO was last confirmed to carry the title's uid
}

// unchangedFile is a file a scan found with the same size+mtime as its row.
type unchangedFile struct {
	knownFile
	relPath, absPath string
}

// loadKnownFiles reads every indexed file of a location in one query, so the
// scan compares against memory instead of querying once per file.
// A library scan (libraryID > 0) only loads the files that belong to that
// library, so files elsewhere on the disk are never marked missing by it.
func (s *Scanner) loadKnownFiles(ctx context.Context, storageLocationID, libraryID int64) (map[string]knownFile, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT mf.id, mf.media_item_id, mf.relative_path, mf.file_size, mf.modified_time,
		       mf.available, mf.nfo_uid IS NOT DISTINCT FROM mi.uid
		FROM media_file mf JOIN media_item mi ON mi.id = mf.media_item_id
		WHERE mf.storage_location_id = $1
		  AND ($2::bigint = 0 OR library_for_file(mf.storage_location_id, mf.relative_path) = $2)
	`, storageLocationID, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]knownFile{}
	for rows.Next() {
		var k knownFile
		var rel string
		var modTime *time.Time
		if err := rows.Scan(&k.id, &k.itemID, &rel, &k.size, &modTime, &k.available, &k.nfoSynced); err != nil {
			return nil, err
		}
		if modTime != nil {
			k.modTime = *modTime
		}
		known[rel] = k
	}
	return known, rows.Err()
}

// ScanStorageLocation walks a registered storage location, incrementally
// updating media_file rows: unchanged files (same size+mtime) are skipped,
// new/changed files are probed and (re)hashed, and previously-seen files no
// longer on disk are marked unavailable rather than deleted.
//
// Every stage runs concurrently: the location's known files load while the
// walk starts, folders are listed in parallel, files are statted, hashed and
// probed on FileWorkers workers, and the closing passes (touching unchanged
// rows, NFO uid sync, missing-file detection, capacity) run side by side.
func (s *Scanner) ScanStorageLocation(ctx context.Context, jobID, storageLocationID int64) (ScanSummary, error) {
	return s.scan(ctx, jobID, storageLocationID, 0)
}

// libraryScope is the part of a disk a library scan walks: the library's
// folders on it, minus subfolders that belong to another library.
type libraryScope struct {
	roots []string        // relative to the mount path; "" is the whole disk
	skip  map[string]bool // other libraries' folders nested inside roots
}

func (s *Scanner) loadLibraryScope(ctx context.Context, storageLocationID, libraryID int64) (libraryScope, error) {
	scope := libraryScope{skip: map[string]bool{}}
	rows, err := s.DB.Query(ctx, `SELECT library_id, path FROM library_root WHERE storage_location_id = $1`, storageLocationID)
	if err != nil {
		return scope, err
	}
	var mine, others []string
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			rows.Close()
			return scope, err
		}
		if id == libraryID {
			mine = append(mine, path)
		} else {
			others = append(others, path)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return scope, err
	}
	if len(mine) == 0 {
		return scope, fmt.Errorf("library %d has no folders on storage location %d", libraryID, storageLocationID)
	}
	// A root inside another of the same library's roots is walked anyway.
	for _, root := range mine {
		nested := false
		for _, other := range mine {
			if other != root && pathWithin(root, other) {
				nested = true
				break
			}
		}
		if !nested {
			scope.roots = append(scope.roots, root)
		}
	}
	for _, other := range others {
		for _, root := range scope.roots {
			if other != root && pathWithin(other, root) {
				scope.skip[other] = true
				break
			}
		}
	}
	return scope, nil
}

// pathWithin reports whether rel is dir or lies under it ("" is the root).
func pathWithin(rel, dir string) bool {
	return dir == "" || rel == dir || strings.HasPrefix(rel, dir+"/")
}

// scan walks a storage location, or only one library's folders on it when
// libraryID > 0.
func (s *Scanner) scan(ctx context.Context, jobID, storageLocationID, libraryID int64) (ScanSummary, error) {
	summary := ScanSummary{StorageLocationID: storageLocationID, LibraryID: libraryID}

	mountPath, excluder, scope, err := s.prepareScan(ctx, storageLocationID, libraryID)
	if err != nil {
		return summary, err
	}
	scanJobID, err := s.insertScanJob(ctx, jobID, storageLocationID, libraryID)
	if err != nil {
		return summary, err
	}

	live := &liveScan{phase: "walking"}
	stopFlush := s.startScanProgressFlush(scanJobID, live)
	defer stopFlush()

	// Whatever happens below — success, error, stall timeout, or the job
	// being cancelled — make sure this scan_job row always gets a
	// completed_at so it never sits showing "Running…" forever in the UI.
	finished := false
	defer func() {
		if finished {
			return
		}
		snap := live.snapshot()
		recent, _ := json.Marshal(snap.recent)
		s.DB.Exec(context.Background(), `
			UPDATE scan_job SET
				discovered_count = $2, changed_count = $3, missing_count = $4,
				files_seen = $5, files_processed = $6, current_path = $7,
				recent_paths = $8, phase = $9, walk_complete = $10, completed_at = now()
			WHERE id = $1
		`, scanJobID, snap.discovered, snap.changed, summary.Missing, snap.filesSeen, snap.filesProcessed, nullIfEmpty(snap.currentPath), recent, snap.phase, snap.walkComplete)
	}()

	// scanCtx lets a stall timeout abort the walk/workers without needing
	// the caller's ctx itself to be cancelled.
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()

	walk := s.walkAndIndex(ctx, scanCtx, cancelScan, scanJobID, storageLocationID, libraryID, mountPath, scope, excluder, live)
	summary, finished, err = s.finishScan(ctx, summary, scanJobID, storageLocationID, libraryID, mountPath, scope, excluder, live, walk)
	return summary, err
}

// touchUnchanged marks every file found unchanged as seen now (and available
// again, if it had gone missing) in one statement.
func (s *Scanner) touchUnchanged(ctx context.Context, files []unchangedFile) error {
	const chunk = 10000
	for start := 0; start < len(files); start += chunk {
		end := min(start+chunk, len(files))
		ids := make([]int64, 0, end-start)
		for _, f := range files[start:end] {
			ids = append(ids, f.id)
		}
		if _, err := s.DB.Exec(ctx, `
			UPDATE media_file SET available = TRUE, missing_since = NULL, last_scanned_at = now()
			WHERE id = ANY($1)
		`, ids); err != nil {
			return fmt.Errorf("mark unchanged files seen: %w", err)
		}
	}
	return nil
}

// syncUnchangedNFOs gives unchanged files whose NFO is missing (per the walk's
// listing, no extra stat) or not yet confirmed to carry the title's uid an NFO
// with it, on FileWorkers workers.
func (s *Scanner) syncUnchangedNFOs(ctx context.Context, files []unchangedFile, nfos map[string]bool) {
	// Older matched NFOs can have provider links without the Jellyfin-style
	// provider id tags. Select only those files for a one-time
	// in-place tag sync; the parsed NFO record marks them complete afterward.
	needsProviderTags := map[int64]bool{}
	needsLegacyIDImport := map[int64]bool{}
	const batch = 10000
	for start := 0; start < len(files); start += batch {
		end := min(start+batch, len(files))
		ids := make([]int64, 0, end-start)
		for _, f := range files[start:end] {
			ids = append(ids, f.id)
		}
		rows, err := s.DB.Query(ctx, `
			SELECT DISTINCT mf.id FROM media_file mf
			JOIN match_link ml ON ml.kind = 'media' AND ml.entity_id = mf.media_item_id
			JOIN nfo_metadata n ON n.media_file_id = mf.id
			WHERE mf.id = ANY($1) AND (
			  (ml.provider = '`+nfoInteropProvider+`' AND COALESCE(n.parsed_metadata->>'PornDBID', '') = '') OR
			  (ml.provider = 'tmdb' AND ml.external_id LIKE 'movie/%' AND COALESCE(n.parsed_metadata->>'TMDBID', '') = '')
			)
		`, ids)
		if err != nil {
			log.Printf("scanner: find NFOs missing provider tags: %v", err)
			break
		}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				needsProviderTags[id] = true
			}
		}
		rows.Close()
		rows, err = s.DB.Query(ctx, `
			SELECT mf.id FROM media_file mf
			JOIN nfo_metadata n ON n.media_file_id = mf.id
			WHERE mf.id = ANY($1) AND NOT (n.parsed_metadata ? 'TPDBID')
		`, ids)
		if err != nil {
			log.Printf("scanner: find NFOs with older provider ID parsing: %v", err)
			break
		}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				needsLegacyIDImport[id] = true
			}
		}
		rows.Close()
	}
	work := make(chan unchangedFile)
	var wg sync.WaitGroup
	for i := 0; i < s.FileWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				if needsLegacyIDImport[f.id] {
					nfoPath, ok := FindAdjacentNFO(f.absPath)
					if ok {
						if err := RefreshNFOLinksFromFile(ctx, s.DB, f.id, f.itemID, nfoPath); err != nil {
							log.Printf("scanner: refresh NFO provider links for %s: %v", nfoPath, err)
						}
					}
				}
				s.syncNFO(ctx, f.id, f.itemID, f.relPath, f.absPath)
			}
		}()
	}
	for _, f := range files {
		nfoRel := strings.TrimSuffix(f.relPath, path.Ext(f.relPath)) + ".nfo"
		if f.nfoSynced && nfos[nfoRel] && !needsProviderTags[f.id] && !needsLegacyIDImport[f.id] {
			continue
		}
		select {
		case work <- f:
		case <-ctx.Done():
		}
	}
	close(work)
	wg.Wait()
}

// MetadataMatchPayload is a METADATA_MATCH job: either every unnamed file on
// one storage location (after a scan), or the titles an admin picked.
type MetadataMatchPayload struct {
	StorageLocationID int64 `json:"storageLocationId"`
	LibraryID         int64 `json:"libraryId,omitempty"` // instead of a storage location

	MediaItemIDs []int64 `json:"mediaItemIds,omitempty"`
}

func (s *Scanner) handleMetadataMatchJob(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
	var p MetadataMatchPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if len(p.MediaItemIDs) > 0 {
		return s.matchSelectedItems(ctx, jobID, p.MediaItemIDs)
	}
	if p.LibraryID > 0 {
		return s.matchUnnamedLibraryFiles(ctx, jobID, p.LibraryID)
	}
	return s.matchUnnamedFiles(ctx, jobID, p.StorageLocationID)
}

// MatchSummary is the result of a METADATA_MATCH job.
type MatchSummary struct {
	Total   int `json:"total"`
	Matched int `json:"matched"`
	Failed  int `json:"failed"`
}

// matchUnnamedFiles asks the namer for metadata on available files that
// still only have a filename title. Confident matches are applied by the
// matcher; anything else is left for a manual match. One lookup failure
// does not fail the job.
func (s *Scanner) matchUnnamedFiles(ctx context.Context, jobID, storageLocationID int64) (MatchSummary, error) {
	var summary MatchSummary
	if s.Matcher == nil || !s.Matcher.Enabled() {
		return summary, errors.New("metadata matching is not configured (add an API key under Metadata → Providers)")
	}
	return s.lookupFiles(ctx, jobID, fmt.Sprintf("storage location %d", storageLocationID), unnamedFilesQuery("mf.storage_location_id = $1"), storageLocationID)
}

// matchUnnamedLibraryFiles is matchUnnamedFiles for every disk of a library.
func (s *Scanner) matchUnnamedLibraryFiles(ctx context.Context, jobID, libraryID int64) (MatchSummary, error) {
	var summary MatchSummary
	if s.Matcher == nil || !s.Matcher.Enabled() {
		return summary, errors.New("metadata matching is not configured (add an API key under Metadata → Providers)")
	}
	return s.lookupFiles(ctx, jobID, fmt.Sprintf("library %d", libraryID), unnamedFilesQuery("mf.library_id = $1"), libraryID)
}

// unnamedFilesQuery selects the available files, narrowed by where, whose
// titles still only have a filename title and no lookup queued.
func unnamedFilesQuery(where string) string {
	return `
		SELECT mf.id, mf.relative_path
		FROM media_file mf
		JOIN media_item mi ON mi.id = mf.media_item_id
		LEFT JOIN nfo_metadata n ON n.media_file_id = mf.id
		WHERE ` + where + `
		  AND mf.available
		  AND mi.metadata_source = 'FILENAME'
		  AND mi.match_status <> 'rejected'
		  AND n.id IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM background_job bj
		    WHERE bj.kind = 'NAMER'
		      AND bj.status IN ('PENDING', 'RUNNING')
		      AND bj.payload->>'mediaFileId' = mf.id::text
		  )
		ORDER BY mf.id
	`
}

// matchSelectedItems looks up metadata for titles an admin picked, whatever
// their current metadata source: one lookup per title, on its best available
// file. The results land in the review queue like any other lookup.
func (s *Scanner) matchSelectedItems(ctx context.Context, jobID int64, mediaItemIDs []int64) (MatchSummary, error) {
	if s.Matcher == nil || !s.Matcher.Enabled() {
		return MatchSummary{}, errors.New("metadata matching is not configured (add an API key under Metadata → Providers)")
	}
	return s.lookupFiles(ctx, jobID, fmt.Sprintf("%d selected title(s)", len(mediaItemIDs)), `
		SELECT f.id, f.relative_path
		FROM media_item mi
		JOIN LATERAL (
			SELECT id, relative_path FROM media_file
			WHERE media_item_id = mi.id AND available
			ORDER BY height DESC NULLS LAST, file_size DESC LIMIT 1
		) f ON true
		WHERE mi.id = ANY($1)
		ORDER BY mi.id
	`, mediaItemIDs)
}

// lookupFiles runs the namer over the (media_file id, relative path) rows the
// query returns, reporting progress on jobID.
func (s *Scanner) lookupFiles(ctx context.Context, jobID int64, what, query string, args ...any) (MatchSummary, error) {
	var summary MatchSummary
	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return summary, err
	}
	type pendingMatch struct {
		id      int64
		relPath string
	}
	var pending []pendingMatch
	for rows.Next() {
		var item pendingMatch
		if err := rows.Scan(&item.id, &item.relPath); err != nil {
			rows.Close()
			return summary, err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return summary, err
	}
	rows.Close()
	summary.Total = len(pending)
	if len(pending) == 0 {
		return summary, nil
	}

	log.Printf("scanner: looking up metadata for %d file(s) on %s", len(pending), what)
	for i, item := range pending {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		jobs.ReportProgress(s.DB, jobID, i, len(pending), item.relPath)
		status, err := s.Matcher.Lookup(ctx, item.id)
		if err != nil {
			summary.Failed++
			log.Printf("scanner: namer lookup failed for media_file %d (%s): %v", item.id, item.relPath, err)
			continue
		}
		summary.Matched++
		log.Printf("scanner: namer lookup for media_file %d (%s): %s", item.id, item.relPath, status)
	}
	jobs.ReportProgress(s.DB, jobID, len(pending), len(pending), "")
	return summary, nil
}

// probeTimeout bounds one ffprobe run: a file that wedges ffprobe must not
// hold a worker (and so the end of the scan) forever.
const probeTimeout = 2 * time.Minute

// indexFile indexes a single file against its row as the scan found it
// (prev, when wasKnown). An unchanged file (same size+mtime) is returned as
// `same` for the closing batch passes and costs no database round trip.
// Otherwise it is hashed and probed (concurrently) and upserted:
// changed=true for a refreshed existing row, isNew=true for a new one.
func (s *Scanner) indexFile(ctx context.Context, storageLocationID int64, task fileTask, prev knownFile, wasKnown bool, dedupMu *sync.Mutex) (same *unchangedFile, changed, isNew bool, err error) {
	relPath, absPath := task.relPath, task.path
	stat, err := statWithTimeout(absPath, fsOpTimeout)
	if err != nil {
		return nil, false, false, err
	}
	size := stat.Size()
	// Postgres keeps microseconds; filesystems report nanoseconds. Without the
	// truncation the stored mtime never equals the disk's, and every scan
	// re-hashes, re-probes and re-thumbnails the whole library.
	modTime := stat.ModTime().Truncate(time.Microsecond)

	if wasKnown && prev.size == size && prev.modTime.Equal(modTime) {
		return &unchangedFile{knownFile: prev, relPath: relPath, absPath: absPath}, false, false, nil
	}
	isNew = !wasKnown

	// ffprobe (a subprocess) runs while this goroutine hashes the file.
	var probe ProbeResult
	var probeErr error
	var dataBoxes *int
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		probe, probeErr = Probe(pctx, absPath)
		dataBoxes = mp4DataBoxes(absPath)
	}()
	partialHash, hashErr := PartialHash(absPath, size)
	nfoPath, hasNFO := FindAdjacentNFO(absPath)
	nfoUID := ""
	if hasNFO {
		nfoUID = NFOUID(nfoPath)
	}
	<-probeDone
	if hashErr != nil {
		return nil, false, false, fmt.Errorf("partial hash: %w", hashErr)
	}
	if probeErr != nil {
		log.Printf("scanner: ffprobe unavailable/failed for %s: %v", absPath, probeErr)
	}

	prevItemID := int64(0)
	if wasKnown {
		prevItemID = prev.itemID
	}
	dedupMu.Lock()
	mediaItemID, err := s.findOrCreateMediaItem(ctx, relPath, nfoUID, prevItemID, partialHash, size, probe.DurationSeconds)
	dedupMu.Unlock()
	if err != nil {
		return nil, false, false, err
	}

	fileName := filepath.Base(relPath)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")

	var mediaFileID int64
	err = s.DB.QueryRow(ctx, `
		INSERT INTO media_file (
			media_item_id, storage_location_id, relative_path, file_name, extension, file_size,
			content_hash, partial_hash, duration_seconds, width, height, frame_rate,
			video_codec, audio_codec, container, video_bitrate, audio_bitrate, overall_bitrate,
			created_time, modified_time, discovered_at, last_scanned_at, available, missing_since, mp4_data_boxes, rotation
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			$19, $20, now(), now(), TRUE, NULL, $21, $22
		)
		ON CONFLICT (storage_location_id, relative_path) DO UPDATE SET
			media_item_id = EXCLUDED.media_item_id,
			file_size = EXCLUDED.file_size,
			content_hash = NULL,
			partial_hash = EXCLUDED.partial_hash,
			duration_seconds = EXCLUDED.duration_seconds,
			width = EXCLUDED.width,
			height = EXCLUDED.height,
			frame_rate = EXCLUDED.frame_rate,
			video_codec = EXCLUDED.video_codec,
			audio_codec = EXCLUDED.audio_codec,
			container = EXCLUDED.container,
			video_bitrate = EXCLUDED.video_bitrate,
			audio_bitrate = EXCLUDED.audio_bitrate,
			overall_bitrate = EXCLUDED.overall_bitrate,
			modified_time = EXCLUDED.modified_time,
			mp4_data_boxes = EXCLUDED.mp4_data_boxes,
			rotation = EXCLUDED.rotation,
			last_scanned_at = now(),
			available = TRUE,
			missing_since = NULL,
			updated_at = now()
		RETURNING id
	`, mediaItemID, storageLocationID, relPath, fileName, ext, size,
		nil, partialHash, probe.DurationSeconds, probe.Width, probe.Height, probe.FrameRate,
		probe.VideoCodec, probe.AudioCodec, ext, probe.VideoBitrate, probe.AudioBitrate, probe.OverallBitrate,
		modTime, modTime, dataBoxes, probe.RotationOrNil(probeErr),
	).Scan(&mediaFileID)
	if err != nil {
		return nil, false, false, fmt.Errorf("upsert media_file: %w", err)
	}

	if err := SaveTracks(ctx, s.DB, mediaFileID, probe.Tracks, probeErr == nil); err != nil {
		log.Printf("scanner: failed to save tracks for %s: %v", absPath, err)
	}
	if err := SaveSidecarCount(ctx, s.DB, mediaFileID, len(FindSidecars(absPath))); err != nil {
		log.Printf("scanner: failed to save subtitle count for %s: %v", absPath, err)
	}
	if hasNFO {
		if err := ImportNFO(ctx, s.DB, mediaFileID, mediaItemID, storageLocationID, relPath, nfoPath); err != nil {
			log.Printf("scanner: nfo import failed for %s: %v", nfoPath, err)
		}
	}
	s.syncNFO(ctx, mediaFileID, mediaItemID, relPath, absPath)

	if _, err := s.ThumbnailJobs.Enqueue(ctx, "THUMBNAIL", ThumbnailJobPayload{MediaFileID: mediaFileID}); err != nil {
		log.Printf("scanner: failed to enqueue thumbnail job for media_file %d: %v", mediaFileID, err)
	}

	return nil, !isNew, isNew, nil
}

// syncNFO gives the file an NFO carrying its title's uid. A failure (e.g. a
// read-only share) is logged, never fatal to the scan.
func (s *Scanner) syncNFO(ctx context.Context, mediaFileID, mediaItemID int64, relPath, absPath string) {
	if err := SyncFileNFO(ctx, s.DB, mediaFileID, mediaItemID, relPath, absPath); err != nil {
		log.Printf("scanner: nfo uid sync failed for %s: %v", absPath, err)
	}
}

// findOrCreateMediaItem picks the logical MediaItem for a new or changed file:
//  1. the ReelStash uid in its NFO (survives renames, moves and remuxes);
//  2. the title its row already belongs to (prevItemID), so a file edited in
//     place — remuxed, re-tagged — keeps its metadata and watch history;
//  3. partial_hash+size+duration (a different copy of something indexed;
//     duration is required so two unrelated files that share a size and a
//     sampled prefix do not become one title);
//  4. otherwise a new MediaItem named after the file, keeping the NFO's uid,
//     if any, so a rebuilt database gets the same identity back.
func (s *Scanner) findOrCreateMediaItem(ctx context.Context, relPath, nfoUID string, prevItemID int64, partialHash string, size int64, duration *float64) (int64, error) {
	var mediaItemID int64
	if nfoUID != "" {
		err := s.DB.QueryRow(ctx, `SELECT id FROM media_item WHERE uid = $1`, nfoUID).Scan(&mediaItemID)
		if err == nil {
			return mediaItemID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	if prevItemID > 0 {
		return prevItemID, nil
	}
	var err error
	switch {
	case partialHash == "":
		err = pgx.ErrNoRows
	case duration != nil && *duration > 0:
		err = s.DB.QueryRow(ctx, `
			SELECT media_item_id FROM media_file
			WHERE partial_hash = $1 AND file_size = $2
			  AND duration_seconds IS NOT NULL
			  AND abs(duration_seconds - $3) < 1
			LIMIT 1
		`, partialHash, size, *duration).Scan(&mediaItemID)
	case size > 0 && size <= int64(2*partialHashSampleBytes):
		// The sample covered the whole file, so the hash is the content.
		err = s.DB.QueryRow(ctx, `
			SELECT media_item_id FROM media_file
			WHERE partial_hash = $1 AND file_size = $2
			LIMIT 1
		`, partialHash, size).Scan(&mediaItemID)
	default:
		err = pgx.ErrNoRows
	}
	if err == nil {
		return mediaItemID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	title := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	err = s.DB.QueryRow(ctx, `
		INSERT INTO media_item (title, sort_title, metadata_source, uid)
		VALUES ($1, $1, 'FILENAME', COALESCE($2::uuid, gen_random_uuid()))
		RETURNING id
	`, title, nullIfEmpty(nfoUID)).Scan(&mediaItemID)
	return mediaItemID, err
}

// removeMissing flags library files the walk didn't find, and deletes a file
// only when an earlier scan had already flagged it and it is still absent.
// A title left with no files is removed with that second pass (its watch
// history, ratings and playlist entries go too). As in markMissing, files
// under a folder the walk couldn't read and files a scan rule excludes are
// left alone. Nothing on disk is touched.
func (s *Scanner) removeMissing(ctx context.Context, known map[string]knownFile, seen map[string]bool, dirErrs []string, excluder *RuleSet) (flagged, files, titles int, err error) {
	var flagIDs, deleteIDs []int64
	items := map[int64]bool{}
	for rel, k := range known {
		if seen[rel] || excluder.Excluded(rel) || underAny(rel, dirErrs) {
			continue
		}
		if k.available {
			flagIDs = append(flagIDs, k.id)
			continue
		}
		deleteIDs = append(deleteIDs, k.id)
		items[k.itemID] = true
	}
	if len(flagIDs) > 0 {
		if _, err = s.DB.Exec(ctx, `
			UPDATE media_file SET available = FALSE, missing_since = now()
			WHERE id = ANY($1)
		`, flagIDs); err != nil {
			return 0, 0, 0, err
		}
		flagged = len(flagIDs)
	}
	if len(deleteIDs) == 0 {
		return flagged, 0, 0, nil
	}
	tag, err := s.DB.Exec(ctx, `DELETE FROM media_file WHERE id = ANY($1) AND NOT available`, deleteIDs)
	if err != nil {
		return flagged, 0, 0, err
	}
	files = int(tag.RowsAffected())
	for itemID := range items {
		removed, err := s.removeTitleIfEmpty(ctx, s.DB, itemID)
		if err != nil {
			return flagged, files, titles, err
		}
		if removed {
			titles++
		}
	}
	log.Printf("scanner: library scan flagged %d missing file(s), removed %d and %d title(s)", flagged, files, titles)
	return flagged, files, titles, nil
}

// underAny reports whether rel lies under one of dirs (each ending in "/").
func underAny(rel string, dirs []string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(rel, dir) {
			return true
		}
	}
	return false
}

// markMissing flags files that were available when the scan started but
// weren't found. Files under a folder the walk couldn't read (dirErrs, each
// ending in "/") are left alone: unreadable is not the same as deleted. So are
// files under a path a scan rule excludes: the scan skips them, it doesn't
// remove them.
func (s *Scanner) markMissing(ctx context.Context, known map[string]knownFile, seen map[string]bool, dirErrs []string, excluder *RuleSet) (int, error) {
	missingIDs := []int64{}
	for rel, k := range known {
		if !k.available || seen[rel] || excluder.Excluded(rel) {
			continue
		}
		unreadable := false
		for _, dir := range dirErrs {
			if strings.HasPrefix(rel, dir) {
				unreadable = true
				break
			}
		}
		if !unreadable {
			missingIDs = append(missingIDs, k.id)
		}
	}
	if len(missingIDs) == 0 {
		return 0, nil
	}

	// A file that vanished from one path but reappeared elsewhere (moved or
	// renamed outside ReelStash, or the disk was migrated with a different
	// layout) already has its watch history and metadata on the shared
	// media_item. Drop the stale row instead of leaving a permanently
	// "missing" duplicate beside the live one. The match needs the same
	// media_item plus either the same partial_hash+size or an NFO confirmed
	// to carry the title's uid (a file remuxed as well as moved), and a row
	// discovered after the old one was last seen, so a pre-existing second
	// copy never absorbs a deletion.
	moved, err := s.DB.Query(ctx, `
		SELECT m.id FROM media_file m
		WHERE m.id = ANY($1)
		  AND EXISTS (
			SELECT 1 FROM media_file n
			JOIN media_item mi ON mi.id = n.media_item_id
			WHERE n.id <> m.id AND n.available = TRUE
			  AND n.media_item_id = m.media_item_id
			  AND ((n.partial_hash = m.partial_hash AND n.file_size = m.file_size) OR n.nfo_uid = mi.uid)
			  AND n.discovered_at >= COALESCE(m.last_scanned_at, m.discovered_at)
			  AND n.id <> ALL($1)
		  )
	`, missingIDs)
	if err != nil {
		return 0, err
	}
	movedIDs := []int64{} // non-nil: a NULL array would make "<> ALL" match nothing
	for moved.Next() {
		var id int64
		if err := moved.Scan(&id); err != nil {
			moved.Close()
			return 0, err
		}
		movedIDs = append(movedIDs, id)
	}
	moved.Close()
	if err := moved.Err(); err != nil {
		return 0, err
	}
	if len(movedIDs) > 0 {
		if _, err := s.DB.Exec(ctx, `DELETE FROM media_file WHERE id = ANY($1)`, movedIDs); err != nil {
			return 0, err
		}
		log.Printf("scanner: %d file(s) detected as moved; dropped their stale index rows", len(movedIDs))
	}

	_, err = s.DB.Exec(ctx, `
		UPDATE media_file SET available = FALSE, missing_since = now()
		WHERE id = ANY($1) AND id <> ALL($2)
	`, missingIDs, movedIDs)
	return len(missingIDs) - len(movedIDs), err
}

// dropStaleCopies deletes index rows for other copies of a title this scan
// just found, when those copies are no longer on disk. The file indexed on
// this scan is kept. A disk that is not mounted (an empty mount point) is
// left alone, so an unplugged drive is not treated as a move.
func (s *Scanner) dropStaleCopies(ctx context.Context, storageLocationID int64, seen map[string]bool) (int, error) {
	if len(seen) == 0 {
		return 0, nil
	}
	rels := make([]string, 0, len(seen))
	for rel := range seen {
		rels = append(rels, rel)
	}
	rows, err := s.DB.Query(ctx, `
		SELECT mf.id, sl.mount_path, mf.relative_path
		FROM media_file mf
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.media_item_id IN (
			SELECT media_item_id FROM media_file
			WHERE storage_location_id = $1 AND relative_path = ANY($2)
		)
		AND mf.storage_location_id <> $1
	`, storageLocationID, rels)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	mounted := map[string]bool{}
	var drop []int64
	for rows.Next() {
		var id int64
		var mount, rel string
		if err := rows.Scan(&id, &mount, &rel); err != nil {
			return 0, err
		}
		present, ok := mounted[mount]
		if !ok {
			present, err = diskinfo.HasEntries(mount, fsOpTimeout)
			if err != nil {
				log.Printf("scanner: skip stale-copy check for %s: %v", mount, err)
				present = false
			}
			mounted[mount] = present
		}
		if !present {
			continue
		}
		abs := filepath.Join(mount, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			log.Printf("scanner: skip stale copy %s: %v", abs, err)
			continue
		}
		drop = append(drop, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(drop) == 0 {
		return 0, nil
	}
	if _, err := s.DB.Exec(ctx, `DELETE FROM media_file WHERE id = ANY($1)`, drop); err != nil {
		return 0, err
	}
	log.Printf("scanner: dropped %d stale file path(s) replaced by a file this scan found", len(drop))
	return len(drop), nil
}

func (s *Scanner) refreshCapacity(ctx context.Context, storageLocationID int64, mountPath string) {
	usage, err := diskinfo.Stat(mountPath, fsOpTimeout)
	if err != nil {
		log.Printf("scanner: statfs failed for %s: %v", mountPath, err)
		return
	}
	recordCapacity(ctx, s.DB, storageLocationID, usage)
}

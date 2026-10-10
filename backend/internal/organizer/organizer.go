// Package organizer lays a storage location out by metadata: every video
// gets a folder of its own, named like the video, holding the video, its
// NFO and its artwork —
//
//	<categorized>/<studio or performer>/<Name>/<Name>.mp4
//	<uncategorized>/<Name>/<Name>.mp4
//
// Which studio/performer folder a video lands in comes from the ordered
// organizer_rule list (first matching rule wins); <Name> comes from the
// shared naming template. Moves stay inside the storage location, update
// media_file in step with the disk, and never overwrite an existing file.
package organizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/rename"
	"reelstash/backend/internal/scanner"
)

// Options carries what the organizer needs from process config.
type Options struct {
	FallbackTemplate string // REELSTASH_RENAME_TEMPLATE; the saved setting wins
	PosterDir        string
	ThumbnailDir     string
}

// Move is one video's place in a plan.
type Move struct {
	MediaFileID int64  `json:"mediaFileId"`
	MediaItemID int64  `json:"mediaItemId"`
	Title       string `json:"title"`
	From        string `json:"from"`
	To          string `json:"to"`
	RuleID      int64  `json:"ruleId,omitempty"`
	RuleKind    string `json:"ruleKind,omitempty"`
	RuleName    string `json:"ruleName,omitempty"`
	Unchanged   bool   `json:"unchanged"`
	// Held moves stay put: the file is in use by a download client (a
	// seeding torrent) or inside a locked folder. HeldReason says which.
	Held       bool   `json:"held,omitempty"`
	HeldReason string `json:"heldReason,omitempty"`
	// Set when a library-wide plan merges the plans of several disks.
	StorageLocationID int64  `json:"storageLocationId,omitempty"`
	StorageLocation   string `json:"storageLocation,omitempty"`
}

// Bucket counts how many videos a plan sends to one top-level folder.
type Bucket struct {
	RuleID int64  `json:"ruleId,omitempty"`
	Kind   string `json:"kind"` // studio | performer | uncategorized
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Count  int    `json:"count"`
}

type Plan struct {
	Settings  Settings `json:"settings"`
	Template  string   `json:"template"`
	Total     int      `json:"total"`
	ToMove    int      `json:"toMove"`
	Unchanged int      `json:"unchanged"`
	Held      int      `json:"held"`
	Buckets   []Bucket `json:"buckets"`
	Moves     []Move   `json:"moves"`

	mountPath string
}

type fileRow struct {
	id, itemID     int64
	relPath        string
	fileName       string
	ext            string
	height         *int
	title          string
	studioID       *int64
	studio         string
	series         string
	releaseDate    *time.Time
	productionYear *int32
}

type castMember struct {
	id   int64
	name string
}

// BuildPlan works out where every available video of the library on the
// storage location belongs, using the library's rules and layout. Folders
// are placed inside the library root the video already sits in, so a move
// never takes a video out of its library. It only reads the database; nothing on disk is touched.
func BuildPlan(ctx context.Context, db *pgxpool.Pool, libraryID, storageLocationID int64, opts Options) (Plan, error) {
	plan := Plan{Settings: LoadSettings(ctx, db, libraryID), Template: rename.Template(ctx, db, libraryID, opts.FallbackTemplate), Moves: []Move{}}
	if err := db.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, storageLocationID).Scan(&plan.mountPath); err != nil {
		return plan, fmt.Errorf("load storage location: %w", err)
	}

	roots, err := libraryRoots(ctx, db, libraryID, storageLocationID)
	if err != nil {
		return plan, fmt.Errorf("load library roots: %w", err)
	}
	rules, err := ListRules(ctx, db, libraryID)
	if err != nil {
		return plan, fmt.Errorf("load organizer rules: %w", err)
	}
	studioRule := map[int64]Rule{}
	performerRule := map[int64]Rule{}
	for _, r := range rules {
		if !r.Enabled || r.Folder() == "" {
			continue
		}
		if r.Kind == KindStudio {
			studioRule[r.EntityID] = r
		} else {
			performerRule[r.EntityID] = r
		}
	}

	rows, err := db.Query(ctx, `
		SELECT mf.id, mf.media_item_id, mf.relative_path, mf.file_name, mf.extension, mf.height,
		       mi.title, mi.studio_id, COALESCE(s.name, ''), COALESCE(se.name, ''), mi.release_date, mi.production_year
		FROM media_file mf
		JOIN media_item mi ON mi.id = mf.media_item_id
		LEFT JOIN studio s ON s.id = mi.studio_id
		LEFT JOIN series se ON se.id = mi.series_id
		WHERE mf.storage_location_id = $1 AND mf.library_id = $2 AND mf.available
		ORDER BY mf.relative_path
	`, storageLocationID, libraryID)
	if err != nil {
		return plan, err
	}
	var files []fileRow
	for rows.Next() {
		var f fileRow
		if err := rows.Scan(&f.id, &f.itemID, &f.relPath, &f.fileName, &f.ext, &f.height,
			&f.title, &f.studioID, &f.studio, &f.series, &f.releaseDate, &f.productionYear); err != nil {
			rows.Close()
			return plan, err
		}
		f.ext = "." + strings.TrimPrefix(f.ext, ".")
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return plan, err
	}

	cast := map[int64][]castMember{}
	crows, err := db.Query(ctx, `
		SELECT mip.media_item_id, p.id, p.name
		FROM media_item_performer mip
		JOIN performer p ON p.id = mip.performer_id
		WHERE mip.media_item_id IN (SELECT media_item_id FROM media_file WHERE storage_location_id = $1 AND library_id = $2 AND available)
		ORDER BY mip.media_item_id, mip.position
	`, storageLocationID, libraryID)
	if err != nil {
		return plan, err
	}
	for crows.Next() {
		var itemID int64
		var c castMember
		if err := crows.Scan(&itemID, &c.id, &c.name); err != nil {
			crows.Close()
			return plan, err
		}
		cast[itemID] = append(cast[itemID], c)
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return plan, err
	}

	type placed struct {
		f         fileRow
		winner    *Rule
		base, ext string
		stem      string
		target    string
		held      string
	}
	candidate := func(p placed, i int) string {
		s := p.stem
		if i > 1 {
			s = fmt.Sprintf("%s (%d)", p.stem, i)
		}
		return path.Join(p.base, s, s+p.ext)
	}

	guard := pathguard.Load(ctx, db)
	buckets := map[string]*Bucket{}
	var bucketOrder []string
	all := make([]placed, 0, len(files))
	for _, f := range files {
		// First matching rule by precedence wins.
		var winner *Rule
		consider := func(r Rule, ok bool) {
			if ok && (winner == nil || r.Position < winner.Position || (r.Position == winner.Position && r.ID < winner.ID)) {
				rr := r
				winner = &rr
			}
		}
		if f.studioID != nil {
			r, ok := studioRule[*f.studioID]
			consider(r, ok)
		}
		names := make([]string, 0, len(cast[f.itemID]))
		for _, c := range cast[f.itemID] {
			r, ok := performerRule[c.id]
			consider(r, ok)
			names = append(names, c.name)
		}

		root := rootFor(roots, f.relPath)
		base := path.Join(root, plan.Settings.UncategorizedDir)
		key := "uncategorized:" + root
		b := Bucket{Kind: "uncategorized", Name: "Uncategorized", Folder: base}
		if winner != nil {
			base = path.Join(root, plan.Settings.CategorizedDir, winner.Folder())
			key = "rule:" + strconv.FormatInt(winner.ID, 10) + ":" + root
			b = Bucket{RuleID: winner.ID, Kind: winner.Kind, Name: winner.Name, Folder: base}
		}
		if buckets[key] == nil {
			buckets[key] = &b
			bucketOrder = append(bucketOrder, key)
		}
		buckets[key].Count++

		stem := strings.TrimSuffix(f.fileName, filepath.Ext(f.fileName))
		ext := filepath.Ext(f.fileName)
		if plan.Settings.RenameFiles {
			ext = strings.ToLower(f.ext)
			stem = strings.TrimSuffix(rename.BuildFileName(plan.Template, f.title, f.studio, f.series, f.releaseDate, f.productionYear, f.height, names, ext), ext)
		}
		stem = trimStem(rename.SanitizeComponent(stem))
		if stem == "" {
			stem = "Untitled " + strconv.FormatInt(f.itemID, 10)
		}
		p := placed{f: f, winner: winner, base: base, ext: ext, stem: stem}
		if blocked, why := guard.Blocked(filepath.Join(plan.mountPath, filepath.FromSlash(f.relPath))); blocked {
			p.held, p.target = why, f.relPath
		}
		all = append(all, p)
	}

	// Two videos never share a folder. Videos already sitting at one of
	// their valid names ("Name" or "Name (n)") keep it, so re-running is a
	// no-op; the rest take the first name nobody holds.
	taken := map[string]bool{} // lower-cased
	for i := range all {
		if all[i].held != "" {
			taken[strings.ToLower(all[i].target)] = true
		}
	}
	for i := range all {
		p := &all[i]
		if p.held != "" || !strings.HasPrefix(p.f.relPath, path.Join(p.base, p.stem)) {
			continue
		}
		for n := 1; n <= 99; n++ {
			if c := candidate(*p, n); c == p.f.relPath && !taken[strings.ToLower(c)] {
				p.target = c
				taken[strings.ToLower(c)] = true
				break
			}
		}
	}
	for i := range all {
		p := &all[i]
		for n := 1; p.target == ""; n++ {
			if c := candidate(*p, n); !taken[strings.ToLower(c)] {
				p.target = c
				taken[strings.ToLower(c)] = true
			}
		}

		m := Move{MediaFileID: p.f.id, MediaItemID: p.f.itemID, Title: p.f.title, From: p.f.relPath, To: p.target, Unchanged: p.target == p.f.relPath,
			Held: p.held != "", HeldReason: p.held}
		if p.winner != nil {
			m.RuleID, m.RuleKind, m.RuleName = p.winner.ID, p.winner.Kind, p.winner.Name
		}
		plan.Total++
		if m.Held {
			plan.Held++
		} else if m.Unchanged {
			plan.Unchanged++
		} else {
			plan.ToMove++
		}
		plan.Moves = append(plan.Moves, m)
	}
	for _, k := range bucketOrder {
		plan.Buckets = append(plan.Buckets, *buckets[k])
	}
	return plan, nil
}

// libraryRoots returns the library's root paths on the storage location
// ("" is the whole disk).
func libraryRoots(ctx context.Context, db *pgxpool.Pool, libraryID, storageLocationID int64) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT path FROM library_root WHERE library_id = $1 AND storage_location_id = $2`, libraryID, storageLocationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roots []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		roots = append(roots, p)
	}
	return roots, rows.Err()
}

// rootFor picks the longest root containing rel ("" when none does).
func rootFor(roots []string, rel string) string {
	best := ""
	for _, r := range roots {
		if r != "" && (rel == r || strings.HasPrefix(rel, r+"/")) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

// trimStem keeps a name comfortably under the 255-byte component limit,
// leaving room for " (n)", "-poster.jpg" and the like.
func trimStem(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && (s[cut]&0xC0) == 0x80 { // don't split a UTF-8 rune
		cut--
	}
	return strings.TrimRight(s[:cut], ". -")
}

// Result is what an ORGANIZE job reports.
type Result struct {
	DryRun    bool     `json:"dryRun,omitempty"`
	Total     int      `json:"total"`
	Moved     int      `json:"moved"`
	Unchanged int      `json:"unchanged"`
	Held      int      `json:"held,omitempty"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

const maxReportedErrors = 50

// Run organizes one library on a storage location. A dry run only builds the plan.
func Run(ctx context.Context, db *pgxpool.Pool, jobID, libraryID, storageLocationID int64, dryRun bool, opts Options) (Result, error) {
	plan, err := BuildPlan(ctx, db, libraryID, storageLocationID, opts)
	if err != nil {
		return Result{}, err
	}
	res := Result{DryRun: dryRun, Total: plan.Total, Unchanged: plan.Unchanged, Held: plan.Held}
	if dryRun {
		res.Moved = plan.ToMove
		return res, nil
	}

	o := &applier{db: db, mount: plan.mountPath, settings: plan.Settings, opts: opts, locationID: storageLocationID}
	for i, m := range plan.Moves {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if i%20 == 0 {
			jobs.ReportProgress(db, jobID, i, len(plan.Moves), m.From)
		}
		finalRel := m.From
		if !m.Unchanged {
			rel, err := o.move(ctx, m)
			if err != nil {
				res.Failed++
				if len(res.Errors) < maxReportedErrors {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", m.From, err))
				}
				continue
			}
			finalRel = rel
			res.Moved++
			log.Printf("organizer: moved %s -> %s", m.From, rel)
		}
		o.writeSidecars(ctx, m, finalRel)
	}
	jobs.ReportProgress(db, jobID, len(plan.Moves), len(plan.Moves), "")
	return res, nil
}

type applier struct {
	db         *pgxpool.Pool
	mount      string
	settings   Settings
	opts       Options
	locationID int64
}

func (o *applier) abs(rel string) string { return filepath.Join(o.mount, filepath.FromSlash(rel)) }

// move relocates one video with its companion files and records the new
// path, returning the relative path it ended up at.
func (o *applier) move(ctx context.Context, m Move) (string, error) {
	oldAbs := o.abs(m.From)
	if _, err := os.Stat(oldAbs); err != nil {
		return "", fmt.Errorf("source not found (rescan the disk): %w", err)
	}

	// The plan only knows the database; something else may already sit at
	// the target on disk (or in a stale media_file row). Step to "Name (n)".
	targetRel := m.To
	ext := path.Ext(targetRel)
	stem := strings.TrimSuffix(path.Base(targetRel), ext)
	base := path.Dir(path.Dir(targetRel))
	for i := 2; ; i++ {
		free, err := o.free(ctx, targetRel, oldAbs, m.MediaFileID)
		if err != nil {
			return "", err
		}
		if free {
			break
		}
		if i > 50 {
			return "", fmt.Errorf("no free name for %s", m.To)
		}
		s := fmt.Sprintf("%s (%d)", stem, i)
		targetRel = path.Join(base, s, s+ext)
	}
	newAbs := o.abs(targetRel)

	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		return "", fmt.Errorf("create folder: %w", err)
	}

	companions := companionFiles(oldAbs, filepath.Dir(oldAbs) != filepath.Clean(o.mount))
	if err := os.Rename(oldAbs, newAbs); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return "", errors.New("the target folder is on a different filesystem than the video")
		}
		return "", fmt.Errorf("move video: %w", err)
	}
	type done struct{ from, to string }
	moved := []done{{oldAbs, newAbs}}
	undo := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Rename(moved[i].to, moved[i].from)
		}
	}

	oldStem := strings.TrimSuffix(filepath.Base(oldAbs), filepath.Ext(oldAbs))
	newStem := strings.TrimSuffix(filepath.Base(newAbs), filepath.Ext(newAbs))
	newDir := filepath.Dir(newAbs)
	for _, c := range companions {
		name := filepath.Base(c)
		dest := filepath.Join(newDir, name) // folder-level artwork keeps its name
		if strings.HasPrefix(name, oldStem) {
			dest = filepath.Join(newDir, newStem+name[len(oldStem):])
		}
		if st, err := os.Lstat(dest); err == nil {
			// On a case-insensitive disk a case-only rename finds c itself.
			if src, serr := os.Lstat(c); serr != nil || !os.SameFile(st, src) {
				log.Printf("organizer: leaving %s behind, %s already exists", c, dest)
				continue
			}
		}
		if err := os.Rename(c, dest); err != nil {
			log.Printf("organizer: could not move %s: %v", c, err)
			continue
		}
		moved = append(moved, done{c, dest})
	}

	tx, err := o.db.Begin(ctx)
	if err != nil {
		undo()
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE media_file SET relative_path = $2, file_name = $3, updated_at = now() WHERE id = $1
	`, m.MediaFileID, targetRel, path.Base(targetRel)); err != nil {
		undo()
		return "", fmt.Errorf("update database: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE nfo_metadata SET relative_path = $2 WHERE media_file_id = $1`, m.MediaFileID, targetRel); err != nil {
		undo()
		return "", fmt.Errorf("update database: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		undo()
		return "", fmt.Errorf("update database: %w", err)
	}

	removeEmptyDirs(filepath.Dir(oldAbs), o.mount)
	return targetRel, nil
}

// free reports whether rel is unused both on disk and by another
// media_file row of this storage location. On a case-insensitive disk a
// case-only rename finds the source itself at rel, which counts as free.
func (o *applier) free(ctx context.Context, rel, source string, mediaFileID int64) (bool, error) {
	if st, err := os.Lstat(o.abs(rel)); err == nil {
		if src, serr := os.Lstat(source); serr != nil || !os.SameFile(st, src) {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	var used bool
	err := o.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media_file WHERE storage_location_id = $1 AND lower(relative_path) = lower($2) AND id <> $3)
	`, o.locationID, rel, mediaFileID).Scan(&used)
	return !used, err
}

// folderArtwork are Kodi/Jellyfin per-folder image names; they belong to a
// video only when it is the sole video in its folder.
var folderArtwork = map[string]bool{
	"poster.jpg": true, "poster.png": true, "folder.jpg": true, "folder.png": true,
	"fanart.jpg": true, "fanart.png": true, "thumb.jpg": true, "landscape.jpg": true,
	"banner.jpg": true, "clearlogo.png": true, "backdrop.jpg": true,
}

// companionFiles lists the files next to video that travel with it: those
// named "<stem>.*" or "<stem>-*" (NFO, subtitles, -poster/-thumb images),
// minus other videos and files that belong to a longer-named sibling video
// (so "Scene.mp4" doesn't take "Scene-2.nfo" from "Scene-2.mp4"). When the
// video is alone in a folder of its own (not the storage root), folder-level
// artwork comes along too.
func companionFiles(video string, ownFolder bool) []string {
	dir := filepath.Dir(video)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	var otherStems []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != filepath.Base(video) && scanner.IsVideoExt(filepath.Ext(e.Name())) {
			otherStems = append(otherStems, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	belongsToSibling := func(name string) bool {
		for _, s := range otherStems {
			if len(s) > len(stem) && strings.HasPrefix(name, s) {
				return true
			}
		}
		return false
	}

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == filepath.Base(video) || scanner.IsVideoExt(filepath.Ext(name)) {
			continue
		}
		own := len(name) > len(stem) && strings.HasPrefix(name, stem) && (name[len(stem)] == '.' || name[len(stem)] == '-')
		if (own && !belongsToSibling(name)) || (ownFolder && len(otherStems) == 0 && folderArtwork[strings.ToLower(name)]) {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

// removeEmptyDirs deletes dir and its parents while they are empty
// (ignoring OS litter such as .DS_Store), stopping at the mount root.
func removeEmptyDirs(dir, mount string) {
	mount = filepath.Clean(mount)
	for {
		dir = filepath.Clean(dir)
		if dir == mount || !strings.HasPrefix(dir, mount+string(filepath.Separator)) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if n := e.Name(); n != ".DS_Store" && n != "Thumbs.db" && !strings.HasPrefix(n, "._") {
				return
			}
		}
		for _, e := range entries {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// writeSidecars gives the video at rel its NFO and artwork files when they
// are missing. Failures are logged; they never fail the move itself.
func (o *applier) writeSidecars(ctx context.Context, m Move, rel string) {
	absVideo := o.abs(rel)
	if o.settings.WriteNFO {
		if err := scanner.SyncFileNFO(ctx, o.db, m.MediaFileID, m.MediaItemID, rel, absVideo); err != nil {
			log.Printf("organizer: nfo for %s: %v", rel, err)
		}
	}
	if !o.settings.WriteImages {
		return
	}
	stemPath := strings.TrimSuffix(absVideo, filepath.Ext(absVideo))

	// -poster: the title's poster (custom or provider).
	var key, mime string
	if err := o.db.QueryRow(ctx, `
		SELECT ar.storage_key, ar.mime_type FROM media_item mi JOIN artwork ar ON ar.id = mi.poster_image_id
		WHERE mi.id = $1
	`, m.MediaItemID).Scan(&key, &mime); err == nil {
		o.exportImage(filepath.Join(o.opts.PosterDir, key), stemPath+"-poster", mime)
	}
	// -thumb: the frame ReelStash generated for the video.
	if err := o.db.QueryRow(ctx, `
		SELECT storage_key FROM thumbnail
		WHERE media_file_id = $1 OR (media_file_id IS NULL AND media_item_id = $2)
		ORDER BY preferred DESC, created_at DESC LIMIT 1
	`, m.MediaFileID, m.MediaItemID).Scan(&key); err == nil {
		o.exportImage(filepath.Join(o.opts.ThumbnailDir, key), stemPath+"-thumb", "image/jpeg")
	}
}

// exportImage copies src to destStem plus an extension matching mime,
// unless an image with that stem is already there.
func (o *applier) exportImage(src, destStem, mime string) {
	if filepath.Base(src) == "." || src == "" {
		return
	}
	for _, e := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if _, err := os.Stat(destStem + e); err == nil {
			return
		}
	}
	ext := ".jpg"
	switch mime {
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	}
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	tmp := destStem + ext + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		log.Printf("organizer: write %s: %v", destStem+ext, err)
		return
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, destStem+ext); err != nil {
		_ = os.Remove(tmp)
	}
}

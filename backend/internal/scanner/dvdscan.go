package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dvdDiscRel is the VIDEO_TS folder of a playable title VOB, relative to the
// mount. Menus (VTS_xx_0.VOB, VIDEO_TS.VOB) and files that are not inside a
// VIDEO_TS folder are not discs.
func dvdDiscRel(rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || !strings.EqualFold(parts[len(parts)-2], "VIDEO_TS") {
		return ""
	}
	match := vtsVOB.FindStringSubmatch(parts[len(parts)-1])
	if match == nil {
		return ""
	}
	piece, _ := strconv.Atoi(match[2])
	if piece == 0 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// syncDVDs records VIDEO_TS folders this scan walked and drops discs that were
// inside the scanned folders but are gone. It never starts a conversion.
// Folders the walk could not read, and paths outside this scan, are left as
// they were.
func (s *Scanner) syncDVDs(ctx context.Context, storageLocationID int64, mount string, roots []string, seen map[string]bool, dirErrs []string, excluder *RuleSet) error {
	if seen == nil {
		seen = map[string]bool{}
	}
	for rel := range seen {
		if !inScanRoots(rel, roots) {
			continue
		}
		abs := filepath.Join(mount, filepath.FromSlash(rel))
		titles, err := DVDTitles(abs)
		if err != nil || len(titles) == 0 {
			continue
		}
		if _, err := s.DB.Exec(ctx, `
			INSERT INTO dvd_disc (storage_location_id, relative_path, title_count, file_size, available, missing_since, last_seen_at)
			VALUES ($1, $2, $3, $4, TRUE, NULL, now())
			ON CONFLICT (storage_location_id, relative_path) DO UPDATE SET
				title_count = EXCLUDED.title_count,
				file_size = EXCLUDED.file_size,
				available = TRUE,
				missing_since = NULL,
				last_seen_at = now(),
				updated_at = now()
		`, storageLocationID, rel, len(titles), discBytes(titles)); err != nil {
			return err
		}
	}

	rows, err := s.DB.Query(ctx, `SELECT id, relative_path FROM dvd_disc WHERE storage_location_id = $1`, storageLocationID)
	if err != nil {
		return err
	}
	type gone struct {
		id  int64
		rel string
	}
	var drop []gone
	for rows.Next() {
		var row gone
		if err := rows.Scan(&row.id, &row.rel); err != nil {
			rows.Close()
			return err
		}
		if !inScanRoots(row.rel, roots) || seen[row.rel] || excluder.Excluded(row.rel) || underAny(row.rel, dirErrs) {
			continue
		}
		drop = append(drop, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, row := range drop {
		if _, err := s.DB.Exec(ctx, `DELETE FROM dvd_disc WHERE id = $1`, row.id); err != nil {
			return err
		}
	}
	return nil
}

func inScanRoots(rel string, roots []string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	for _, root := range roots {
		if pathWithin(rel, strings.Trim(filepath.ToSlash(root), "/")) {
			return true
		}
	}
	return false
}

func discBytes(titles []DVDTitle) int64 {
	var n int64
	for _, title := range titles {
		for _, file := range title.Files {
			info, err := os.Stat(file)
			if err == nil {
				n += info.Size()
			}
		}
	}
	return n
}

// DVDMP4Status counts how many of a disc's MP4s already sit in the movie folder.
// total is 0 when the VIDEO_TS folder cannot be read.
func DVDMP4Status(videoDir, mount string) (ready, total int) {
	titles, err := DVDTitles(videoDir)
	if err != nil || len(titles) == 0 {
		return 0, 0
	}
	outDir := dvdOutputDir(videoDir, filepath.Clean(mount))
	movie := filepath.Base(outDir)
	if movie == "." || movie == string(filepath.Separator) {
		movie = "DVD"
	}
	names, err := DVDOutputNames(movie, titles)
	if err != nil {
		return 0, len(titles)
	}
	ready = 0
	for _, name := range names {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err == nil && !info.IsDir() {
			ready++
		}
	}
	return ready, len(names)
}

// DVDMovieName is the folder a disc is named after: the parent of VIDEO_TS.
func DVDMovieName(rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	if strings.EqualFold(base, "VIDEO_TS") {
		parent := ""
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			parent = rel[:i]
		}
		if j := strings.LastIndex(parent, "/"); j >= 0 {
			parent = parent[j+1:]
		}
		if parent == "" || parent == "." {
			return "DVD"
		}
		return parent
	}
	if base == "" || base == "." {
		return "DVD"
	}
	return base
}

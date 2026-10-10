// Package rename builds and applies ReelStash's file rename pipeline: turn
// a MediaItem's metadata into a filename via a configurable template, then
// (on request) rename the physical file in place and update media_file.
//
// This only ever touches the filename within a media file's existing
// storage_location/directory — it never moves a file to a different
// storage_location, and it never overwrites an existing file at the target
// path.
package rename

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"reelstash/backend/internal/pathguard"
)

const DefaultTemplate = "{studio} - {date} - {title} [{resolution}]"

// Target describes a proposed or applied rename.
type Target struct {
	MediaFileID     int64  `json:"mediaFileId"`
	MediaItemID     int64  `json:"mediaItemId"`
	StorageLocation string `json:"storageLocation"`
	OldRelativePath string `json:"oldRelativePath"`
	NewRelativePath string `json:"newRelativePath"`
	OldFileName     string `json:"oldFileName"`
	NewFileName     string `json:"newFileName"`
	Unchanged       bool   `json:"unchanged"`
}

type fileRow struct {
	mediaItemID     int64
	storageLocation string
	mountPath       string
	relativePath    string
	fileName        string
	extension       string
	height          *int
	title           string
	studioName      *string
	seriesName      *string
	releaseDate     *time.Time
	productionYear  *int32
}

var invalidFileNameChars = regexp.MustCompile(`[\\/:*?"<>|]`)

func sanitizeComponent(s string) string {
	s = invalidFileNameChars.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	return s
}

// SanitizeComponent makes s safe as a single path component: characters
// Windows/SMB shares reject are dropped, and trailing dots/spaces (which
// those shares silently strip) are trimmed.
func SanitizeComponent(s string) string {
	return strings.TrimRight(sanitizeComponent(s), ". ")
}

// TemplateSettingKey is the app_setting row holding the naming template
// shared by the Rename Pipeline, the organizer and the namer. Each library
// has its own override under TemplateSettingKey+":<libraryID>"; this global
// row is the fallback for libraries without one, and the
// REELSTASH_RENAME_TEMPLATE env value is the last resort.
const TemplateSettingKey = "rename_template"

// TemplateKey is the setting row holding a library's own template.
func TemplateKey(libraryID int64) string {
	return TemplateSettingKey + ":" + strconv.FormatInt(libraryID, 10)
}

// Template returns the active naming template for a library: its own saved
// template, else the global saved one, else fallback. libraryID 0 skips the
// library lookup.
func Template(ctx context.Context, db *pgxpool.Pool, libraryID int64, fallback string) string {
	keys := []string{TemplateSettingKey}
	if libraryID > 0 {
		keys = []string{TemplateKey(libraryID), TemplateSettingKey}
	}
	for _, k := range keys {
		var v string
		if err := db.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, k).Scan(&v); err == nil && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if fallback == "" {
		return DefaultTemplate
	}
	return fallback
}

// FileTemplate is Template for the library a media file belongs to.
func FileTemplate(ctx context.Context, db *pgxpool.Pool, mediaFileID int64, fallback string) string {
	var lib *int64
	_ = db.QueryRow(ctx, `SELECT library_id FROM media_file WHERE id = $1`, mediaFileID).Scan(&lib)
	if lib == nil {
		return Template(ctx, db, 0, fallback)
	}
	return Template(ctx, db, *lib, fallback)
}

// BuildFileName expands a rename template against a MediaItem/MediaFile's
// metadata. Supported tokens: {title} {studio} {series} {year} {date}
// {performers} {resolution} {height}.
func BuildFileName(template string, title, studio, series string, releaseDate *time.Time, productionYear *int32, height *int, performers []string, ext string) string {
	year := ""
	if productionYear != nil {
		year = strconv.Itoa(int(*productionYear))
	}
	date := ""
	if releaseDate != nil {
		date = releaseDate.Format("2006-01-02")
	} else if year != "" {
		date = year
	}
	resolution := ""
	if height != nil {
		resolution = fmt.Sprintf("%dp", *height)
	}
	heightStr := ""
	if height != nil {
		heightStr = strconv.Itoa(*height)
	}

	replacer := strings.NewReplacer(
		"{title}", sanitizeComponent(title),
		"{studio}", sanitizeComponent(studio),
		"{series}", sanitizeComponent(series),
		"{year}", year,
		"{date}", date,
		"{performers}", sanitizeComponent(strings.Join(performers, ", ")),
		"{resolution}", resolution,
		"{height}", heightStr,
	)
	name := replacer.Replace(template)

	// Collapse leftover separator debris from empty tokens (e.g. missing
	// studio leaves "- 2024 - Title [ ]").
	name = regexp.MustCompile(`\s*\[\s*\]`).ReplaceAllString(name, "")
	name = regexp.MustCompile(`\s{2,}`).ReplaceAllString(name, " ")
	name = regexp.MustCompile(`(\s*-\s*){2,}`).ReplaceAllString(name, " - ")
	name = strings.Trim(name, " -")
	if name == "" {
		name = sanitizeComponent(title)
	}

	return name + strings.ToLower(ext)
}

func loadFileRow(ctx context.Context, db *pgxpool.Pool, mediaFileID int64) (fileRow, error) {
	var f fileRow
	var ext string
	err := db.QueryRow(ctx, `
		SELECT mf.media_item_id, sl.name, sl.mount_path, mf.relative_path, mf.file_name, mf.extension, mf.height,
		       mi.title, s.name, se.name, mi.release_date, mi.production_year
		FROM media_file mf
		JOIN storage_location sl ON sl.id = mf.storage_location_id
		JOIN media_item mi ON mi.id = mf.media_item_id
		LEFT JOIN studio s ON s.id = mi.studio_id
		LEFT JOIN series se ON se.id = mi.series_id
		WHERE mf.id = $1
	`, mediaFileID).Scan(&f.mediaItemID, &f.storageLocation, &f.mountPath, &f.relativePath, &f.fileName, &ext, &f.height,
		&f.title, &f.studioName, &f.seriesName, &f.releaseDate, &f.productionYear)
	f.extension = "." + strings.TrimPrefix(ext, ".")
	return f, err
}

func loadPerformers(ctx context.Context, db *pgxpool.Pool, mediaItemID int64) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT p.name FROM media_item_performer mip
		JOIN performer p ON p.id = mip.performer_id
		WHERE mip.media_item_id = $1
		ORDER BY mip.position
	`, mediaItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Preview computes the proposed rename for a media file without touching
// disk or the database.
func Preview(ctx context.Context, db *pgxpool.Pool, template string, mediaFileID int64) (Target, error) {
	f, err := loadFileRow(ctx, db, mediaFileID)
	if err != nil {
		return Target{}, err
	}
	performers, err := loadPerformers(ctx, db, f.mediaItemID)
	if err != nil {
		return Target{}, err
	}

	studio := ""
	if f.studioName != nil {
		studio = *f.studioName
	}
	series := ""
	if f.seriesName != nil {
		series = *f.seriesName
	}

	newFileName := BuildFileName(template, f.title, studio, series, f.releaseDate, f.productionYear, f.height, performers, f.extension)
	dir := filepath.Dir(f.relativePath)
	newRelativePath := newFileName
	if dir != "." {
		newRelativePath = filepath.ToSlash(filepath.Join(dir, newFileName))
	}

	return Target{
		MediaFileID:     mediaFileID,
		MediaItemID:     f.mediaItemID,
		StorageLocation: f.storageLocation,
		OldRelativePath: f.relativePath,
		NewRelativePath: newRelativePath,
		OldFileName:     f.fileName,
		NewFileName:     newFileName,
		Unchanged:       newRelativePath == f.relativePath,
	}, nil
}

// Apply renames the physical file and updates media_file to match. It
// refuses to overwrite an existing file at the target path.
func Apply(ctx context.Context, db *pgxpool.Pool, template string, mediaFileID int64) (Target, error) {
	f, err := loadFileRow(ctx, db, mediaFileID)
	if err != nil {
		return Target{}, err
	}
	target, err := Preview(ctx, db, template, mediaFileID)
	if err != nil {
		return Target{}, err
	}
	if target.Unchanged {
		return target, nil
	}

	oldAbs := filepath.Join(f.mountPath, filepath.FromSlash(target.OldRelativePath))
	newAbs := filepath.Join(f.mountPath, filepath.FromSlash(target.NewRelativePath))
	if blocked, why := pathguard.Load(ctx, db).Blocked(oldAbs); blocked {
		return Target{}, errors.New(why)
	}

	if _, err := os.Stat(newAbs); err == nil {
		return Target{}, fmt.Errorf("a file already exists at the target path: %s", target.NewRelativePath)
	} else if !os.IsNotExist(err) {
		return Target{}, err
	}

	if err := os.Rename(oldAbs, newAbs); err != nil {
		return Target{}, fmt.Errorf("rename on disk: %w", err)
	}

	// Keep an adjacent .nfo alongside the file it describes, so the next
	// scan's FindAdjacentNFO still matches it to this media file.
	oldNFO := replaceExt(oldAbs, ".nfo")
	if _, err := os.Stat(oldNFO); err == nil {
		newNFO := replaceExt(newAbs, ".nfo")
		if err := os.Rename(oldNFO, newNFO); err != nil {
			_ = os.Rename(newAbs, oldAbs)
			return Target{}, fmt.Errorf("rename adjacent nfo: %w", err)
		}
	}

	_, err = db.Exec(ctx, `
		UPDATE media_file SET relative_path = $2, file_name = $3, updated_at = now()
		WHERE id = $1
	`, mediaFileID, target.NewRelativePath, target.NewFileName)
	if err != nil {
		// Best-effort: try to move everything back so disk and DB don't diverge.
		_ = os.Rename(newAbs, oldAbs)
		if _, statErr := os.Stat(replaceExt(newAbs, ".nfo")); statErr == nil {
			_ = os.Rename(replaceExt(newAbs, ".nfo"), oldNFO)
		}
		return Target{}, fmt.Errorf("update database after rename: %w", err)
	}

	return target, nil
}

func replaceExt(path, newExt string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + newExt
}

// PendingQueue lists media files whose current filename does not match
// what their library's template would produce — the Rename Pipeline's
// queue. libraryID 0 covers every library, each with its own template.
func PendingQueue(ctx context.Context, db *pgxpool.Pool, libraryID int64, fallback string, limit int) ([]Target, error) {
	// Files are checked in id order, in batches, until `limit` of them need a
	// rename. Checking only the first `limit` files would leave the queue empty
	// whenever those happen to be named correctly already.
	const batch = 500
	type fileLib struct{ id, lib int64 }
	templates := map[int64]string{}
	var pending []Target
	var after int64
	for len(pending) < limit {
		rows, err := db.Query(ctx, `
			SELECT id, COALESCE(library_id, 0) FROM media_file
			WHERE available AND id > $3 AND ($1::bigint = 0 OR library_id = $1)
			ORDER BY id LIMIT $2`, libraryID, batch, after)
		if err != nil {
			return nil, err
		}
		var files []fileLib
		for rows.Next() {
			var f fileLib
			if err := rows.Scan(&f.id, &f.lib); err != nil {
				rows.Close()
				return nil, err
			}
			files = append(files, f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(files) == 0 {
			break
		}
		after = files[len(files)-1].id

		for _, f := range files {
			tpl, ok := templates[f.lib]
			if !ok {
				tpl = Template(ctx, db, f.lib, fallback)
				templates[f.lib] = tpl
			}
			t, err := Preview(ctx, db, tpl, f.id)
			if err != nil {
				continue
			}
			if !t.Unchanged {
				pending = append(pending, t)
				if len(pending) == limit {
					return pending, nil
				}
			}
		}
	}
	return pending, nil
}

var ErrNotFound = pgx.ErrNoRows

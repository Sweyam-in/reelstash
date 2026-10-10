package downloads

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/qbit"
	"reelstash/backend/internal/rename"
	"reelstash/backend/internal/scanner"
)

// Removal policies: keep the torrent, remove it once moved, or remove it once
// it has seeded enough. Removing never deletes the files.
const (
	RemoveNever  = "never"
	RemoveMoved  = "moved"
	RemoveSeeded = "seeded"
)

// Rule moves finished torrents that match every set condition into a folder.
type Rule struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Enabled  bool   `json:"enabled"`

	MatchCategory string `json:"matchCategory"`
	MatchTag      string `json:"matchTag"`
	MatchTracker  string `json:"matchTracker"`
	MatchName     string `json:"matchName"`
	MinSizeMB     int64  `json:"minSizeMb"`
	VideoOnly     bool   `json:"videoOnly"`

	DestStorageLocationID int64  `json:"destStorageLocationId"`
	DestStorageLocation   string `json:"destStorageLocation,omitempty"`
	DestPath              string `json:"destPath"`
	Subfolder             string `json:"subfolder"`

	ScanAfter         bool    `json:"scanAfter"`
	RemoveAfter       string  `json:"removeAfter"`
	RemoveRatio       float64 `json:"removeRatio"`
	RemoveSeedMinutes int64   `json:"removeSeedMinutes"`
}

const ruleColumns = `r.id, r.name, r.position, r.enabled, r.match_category, r.match_tag, r.match_tracker, r.match_name,
	r.min_size_mb, r.video_only, r.dest_storage_location_id, COALESCE(sl.name, ''), r.dest_path, r.subfolder,
	r.scan_after, r.remove_after, r.remove_ratio, r.remove_seed_minutes`

func scanRule(row pgx.CollectableRow) (Rule, error) {
	var r Rule
	err := row.Scan(&r.ID, &r.Name, &r.Position, &r.Enabled, &r.MatchCategory, &r.MatchTag, &r.MatchTracker, &r.MatchName,
		&r.MinSizeMB, &r.VideoOnly, &r.DestStorageLocationID, &r.DestStorageLocation, &r.DestPath, &r.Subfolder,
		&r.ScanAfter, &r.RemoveAfter, &r.RemoveRatio, &r.RemoveSeedMinutes)
	return r, err
}

// ListRules returns the rules in the order they are checked.
func ListRules(ctx context.Context, db *pgxpool.Pool) ([]Rule, error) {
	rows, err := db.Query(ctx, `SELECT `+ruleColumns+` FROM download_rule r
		LEFT JOIN storage_location sl ON sl.id = r.dest_storage_location_id
		ORDER BY r.position, r.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRule)
}

// GetRule returns one rule.
func GetRule(ctx context.Context, db *pgxpool.Pool, id int64) (Rule, error) {
	rows, err := db.Query(ctx, `SELECT `+ruleColumns+` FROM download_rule r
		LEFT JOIN storage_location sl ON sl.id = r.dest_storage_location_id
		WHERE r.id = $1`, id)
	if err != nil {
		return Rule{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanRule)
}

// Normalize trims a rule and checks it can be saved.
func (r *Rule) Normalize() error {
	r.Name = strings.TrimSpace(r.Name)
	r.MatchCategory = strings.TrimSpace(r.MatchCategory)
	r.MatchTag = strings.TrimSpace(r.MatchTag)
	r.MatchTracker = strings.TrimSpace(r.MatchTracker)
	r.MatchName = strings.TrimSpace(r.MatchName)
	r.DestPath = strings.Trim(filepath.ToSlash(strings.TrimSpace(r.DestPath)), "/")
	r.Subfolder = strings.Trim(filepath.ToSlash(strings.TrimSpace(r.Subfolder)), "/")
	if r.Name == "" {
		return errors.New("give the rule a name")
	}
	if r.DestStorageLocationID == 0 {
		return errors.New("choose where matching torrents go")
	}
	for _, part := range strings.Split(r.DestPath+"/"+r.Subfolder, "/") {
		if part == ".." {
			return errors.New("the destination can't climb out of the storage location")
		}
	}
	if r.MinSizeMB < 0 {
		r.MinSizeMB = 0
	}
	switch r.RemoveAfter {
	case "":
		r.RemoveAfter = RemoveNever
	case RemoveNever, RemoveMoved:
	case RemoveSeeded:
		if r.RemoveRatio <= 0 && r.RemoveSeedMinutes <= 0 {
			return errors.New("set a ratio or a seeding time to remove torrents after seeding")
		}
	default:
		return errors.New("unknown removal choice " + r.RemoveAfter)
	}
	if r.RemoveRatio < 0 {
		r.RemoveRatio = 0
	}
	if r.RemoveSeedMinutes < 0 {
		r.RemoveSeedMinutes = 0
	}
	return nil
}

// CreateRule adds a rule at the end of the list.
func CreateRule(ctx context.Context, db *pgxpool.Pool, r Rule) (Rule, error) {
	err := db.QueryRow(ctx, `
		INSERT INTO download_rule (name, position, enabled, match_category, match_tag, match_tracker, match_name, min_size_mb,
			video_only, dest_storage_location_id, dest_path, subfolder, scan_after, remove_after, remove_ratio, remove_seed_minutes)
		VALUES ($1, (SELECT COALESCE(MAX(position), 0) + 1 FROM download_rule), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id`,
		r.Name, r.Enabled, r.MatchCategory, r.MatchTag, r.MatchTracker, r.MatchName, r.MinSizeMB,
		r.VideoOnly, r.DestStorageLocationID, r.DestPath, r.Subfolder, r.ScanAfter, r.RemoveAfter, r.RemoveRatio, r.RemoveSeedMinutes,
	).Scan(&r.ID)
	if err != nil {
		return r, err
	}
	return GetRule(ctx, db, r.ID)
}

// UpdateRule saves a rule's fields (not its position).
func UpdateRule(ctx context.Context, db *pgxpool.Pool, r Rule) (Rule, error) {
	tag, err := db.Exec(ctx, `
		UPDATE download_rule SET name = $2, enabled = $3, match_category = $4, match_tag = $5, match_tracker = $6, match_name = $7,
			min_size_mb = $8, video_only = $9, dest_storage_location_id = $10, dest_path = $11, subfolder = $12, scan_after = $13,
			remove_after = $14, remove_ratio = $15, remove_seed_minutes = $16, updated_at = now()
		WHERE id = $1`,
		r.ID, r.Name, r.Enabled, r.MatchCategory, r.MatchTag, r.MatchTracker, r.MatchName,
		r.MinSizeMB, r.VideoOnly, r.DestStorageLocationID, r.DestPath, r.Subfolder, r.ScanAfter,
		r.RemoveAfter, r.RemoveRatio, r.RemoveSeedMinutes)
	if err != nil {
		return r, err
	}
	if tag.RowsAffected() == 0 {
		return r, pgx.ErrNoRows
	}
	return GetRule(ctx, db, r.ID)
}

// DeleteRule removes a rule; its history rows keep the rule's name.
func DeleteRule(ctx context.Context, db *pgxpool.Pool, id int64) error {
	tag, err := db.Exec(ctx, `DELETE FROM download_rule WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

// ReorderRules sets the check order to ids.
func ReorderRules(ctx context.Context, db *pgxpool.Pool, ids []int64) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE download_rule SET position = $2 WHERE id = $1`, id, i+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// FileLister returns a torrent's file names, fetched only when a rule asks
// about file types.
type FileLister func(hash string) ([]string, error)

// Matches reports whether a finished torrent meets every condition of r.
func (r Rule) Matches(t qbit.Torrent, files FileLister) (bool, error) {
	if !r.Enabled {
		return false, nil
	}
	if r.MatchCategory != "" && !strings.EqualFold(r.MatchCategory, t.Category) {
		return false, nil
	}
	if r.MatchTag != "" {
		found := false
		for _, tag := range t.Tags {
			if strings.EqualFold(tag, r.MatchTag) {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	if r.MatchTracker != "" && !strings.Contains(strings.ToLower(t.Tracker), strings.ToLower(r.MatchTracker)) {
		return false, nil
	}
	if r.MatchName != "" && !NameMatches(r.MatchName, t.Name) {
		return false, nil
	}
	if r.MinSizeMB > 0 && t.Size < r.MinSizeMB<<20 {
		return false, nil
	}
	if r.VideoOnly {
		if files == nil {
			return false, nil
		}
		names, err := files(t.Hash)
		if err != nil {
			return false, err
		}
		video := false
		for _, n := range names {
			if scanner.IsVideoExt(path.Ext(n)) {
				video = true
				break
			}
		}
		if !video {
			return false, nil
		}
	}
	return true, nil
}

// NameMatches checks a torrent name against a pattern: with * or ? it is a
// whole-name wildcard match, otherwise a substring; case never matters.
func NameMatches(pattern, name string) bool {
	pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	if !strings.ContainsAny(pattern, "*?") {
		return strings.Contains(name, pattern)
	}
	var b strings.Builder
	b.WriteString("^")
	for _, c := range pattern {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(name)
}

// ExpandSubfolder fills {name}, {category} and {tag} into a subfolder
// template, each made safe as a single folder name.
func ExpandSubfolder(tmpl string, t qbit.Torrent) string {
	if tmpl == "" {
		return ""
	}
	tag := ""
	if len(t.Tags) > 0 {
		tag = t.Tags[0]
	}
	var parts []string
	for _, part := range strings.Split(tmpl, "/") {
		part = strings.NewReplacer(
			"{name}", safeName(t.Name),
			"{category}", safeName(t.Category),
			"{tag}", safeName(tag),
		).Replace(part)
		part = strings.TrimSpace(part)
		if part != "" && part != "." && part != ".." {
			parts = append(parts, part)
		}
	}
	return path.Join(parts...)
}

func safeName(s string) string {
	return strings.ReplaceAll(rename.SanitizeComponent(s), "/", " ")
}

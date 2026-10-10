package organizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/rename"
)

const (
	KindStudio    = "studio"
	KindPerformer = "performer"
)

// Rule sends every video of one studio or performer to
// <categorized>/<Folder()>/. Position is its precedence: when a video
// matches several rules, the lowest position wins.
type Rule struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	EntityID   int64  `json:"entityId"`
	Name       string `json:"name"`
	ImageID    *int64 `json:"imageId,omitempty"`
	FolderName string `json:"folderName"`
	Enabled    bool   `json:"enabled"`
	Position   int32  `json:"position"`
	MediaCount int64  `json:"mediaCount"`
}

// Folder is the rule's directory name under the categorized root: its
// override when set, otherwise the studio's or performer's name.
func (r Rule) Folder() string {
	if f := rename.SanitizeComponent(r.FolderName); f != "" {
		return f
	}
	return rename.SanitizeComponent(r.Name)
}

var ErrRuleNotFound = errors.New("organizer rule not found")

// ListRules returns every rule of a library in precedence order, with the entity's
// current name, image and how many titles it has.
func ListRules(ctx context.Context, db *pgxpool.Pool, libraryID int64) ([]Rule, error) {
	rows, err := db.Query(ctx, `
		SELECT r.id,
		       CASE WHEN r.studio_id IS NOT NULL THEN 'studio' ELSE 'performer' END,
		       COALESCE(r.studio_id, r.performer_id),
		       COALESCE(s.name, COALESCE(NULLIF(p.display_name, ''), p.name)),
		       COALESCE(s.logo_image_id, p.profile_image_id),
		       COALESCE(r.folder_name, ''), r.enabled, r.position,
		       CASE WHEN r.studio_id IS NOT NULL
		            THEN (SELECT COUNT(*) FROM media_item mi WHERE mi.studio_id = r.studio_id)
		            ELSE (SELECT COUNT(*) FROM media_item_performer mip WHERE mip.performer_id = r.performer_id)
		       END
		FROM organizer_rule r
		LEFT JOIN studio s ON s.id = r.studio_id
		LEFT JOIN performer p ON p.id = r.performer_id
		WHERE r.library_id = $1
		ORDER BY r.position, r.id
	`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Kind, &r.EntityID, &r.Name, &r.ImageID, &r.FolderName, &r.Enabled, &r.Position, &r.MediaCount); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// CreateRule adds a rule for a studio or performer at the end of the
// precedence list (lowest priority).
func CreateRule(ctx context.Context, db *pgxpool.Pool, libraryID int64, kind string, entityID int64, folderName string) (int64, error) {
	var studioID, performerID *int64
	switch kind {
	case KindStudio:
		studioID = &entityID
	case KindPerformer:
		performerID = &entityID
	default:
		return 0, fmt.Errorf("kind must be %q or %q", KindStudio, KindPerformer)
	}
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO organizer_rule (library_id, studio_id, performer_id, folder_name, position)
		VALUES ($4, $1, $2, NULLIF($3, ''), (SELECT COALESCE(MAX(position), -1) + 1 FROM organizer_rule WHERE library_id = $4))
		RETURNING id
	`, studioID, performerID, strings.TrimSpace(folderName), libraryID).Scan(&id)
	return id, err
}

func UpdateRule(ctx context.Context, db *pgxpool.Pool, libraryID, id int64, folderName string, enabled bool) error {
	tag, err := db.Exec(ctx, `
		UPDATE organizer_rule SET folder_name = NULLIF($2, ''), enabled = $3, updated_at = now() WHERE id = $1 AND library_id = $4
	`, id, strings.TrimSpace(folderName), enabled, libraryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

func DeleteRule(ctx context.Context, db *pgxpool.Pool, libraryID, id int64) error {
	tag, err := db.Exec(ctx, `DELETE FROM organizer_rule WHERE id = $1 AND library_id = $2`, id, libraryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// ReorderRules sets precedence from ids (first = highest). Rules missing
// from ids keep their relative order after the listed ones.
func ReorderRules(ctx context.Context, db *pgxpool.Pool, libraryID int64, ids []int64) error {
	_, err := db.Exec(ctx, `
		WITH wanted AS (SELECT id, ord FROM unnest($1::bigint[]) WITH ORDINALITY AS t(id, ord)),
		ranked AS (
			SELECT r.id, ROW_NUMBER() OVER (ORDER BY w.ord NULLS LAST, r.position, r.id) - 1 AS pos
			FROM organizer_rule r LEFT JOIN wanted w ON w.id = r.id
			WHERE r.library_id = $2
		)
		UPDATE organizer_rule r SET position = ranked.pos, updated_at = now()
		FROM ranked WHERE ranked.id = r.id AND r.position <> ranked.pos
	`, ids, libraryID)
	return err
}

// Settings are a library's organizer layout choices, stored as JSON in
// app_setting (one key per library) so they change without a redeploy.
type Settings struct {
	// Both folders are relative to the library root a video sits in.
	CategorizedDir   string `json:"categorizedDir"`
	UncategorizedDir string `json:"uncategorizedDir"`
	// RenameFiles names each video (and its folder) from the naming
	// template; when off, the current filename is kept.
	RenameFiles bool `json:"renameFiles"`
	WriteNFO    bool `json:"writeNfo"`
	WriteImages bool `json:"writeImages"`
}

func settingsKey(libraryID int64) string {
	return "organizer_settings:" + strconv.FormatInt(libraryID, 10)
}

func DefaultSettings() Settings {
	return Settings{CategorizedDir: "categorized", UncategorizedDir: "uncategorized", RenameFiles: true, WriteNFO: true, WriteImages: true}
}

func LoadSettings(ctx context.Context, db *pgxpool.Pool, libraryID int64) Settings {
	s := DefaultSettings()
	var raw string
	if err := db.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, settingsKey(libraryID)).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

// Normalize cleans the two root directories and rejects ones that would
// escape the storage location or collide with each other.
func (s *Settings) Normalize() error {
	clean := func(label, dir string) (string, error) {
		dir = strings.Trim(path.Clean("/"+strings.ReplaceAll(strings.TrimSpace(dir), `\`, "/")), "/")
		if dir == "" || dir == "." {
			return "", fmt.Errorf("%s folder is required", label)
		}
		for _, part := range strings.Split(dir, "/") {
			if part != rename.SanitizeComponent(part) || strings.HasPrefix(part, ".") {
				return "", fmt.Errorf("%s folder %q contains characters that aren't allowed", label, dir)
			}
		}
		return dir, nil
	}
	var err error
	if s.CategorizedDir, err = clean("categorized", s.CategorizedDir); err != nil {
		return err
	}
	if s.UncategorizedDir, err = clean("uncategorized", s.UncategorizedDir); err != nil {
		return err
	}
	a, b := strings.ToLower(s.CategorizedDir)+"/", strings.ToLower(s.UncategorizedDir)+"/"
	if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
		return errors.New("the categorized and uncategorized folders must be separate")
	}
	return nil
}

func SaveSettings(ctx context.Context, db *pgxpool.Pool, libraryID int64, s Settings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, settingsKey(libraryID), string(raw))
	return err
}

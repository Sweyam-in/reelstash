// Package downloads connects ReelStash to qBittorrent: it keeps a live copy
// of the torrent list for the Downloads workspace, protects the folder where
// unfinished downloads live, and moves finished torrents into libraries by
// rule (through qBittorrent, so they keep seeding).
package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/qbit"
)

const settingsKey = "downloads.settings"

// DefaultMoveTag is added to a torrent after a rule moves it.
const DefaultMoveTag = "reelstash"

// Settings are the Downloads settings saved in app_setting. Connection fields
// left empty fall back to the environment (REELSTASH_QBIT_*), which is how
// the Compose stack wires its own qBittorrent in.
type Settings struct {
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// PathFrom/PathTo translate paths when qBittorrent sees the media under
	// another prefix than ReelStash (qBittorrent "/downloads" ⇄ ReelStash
	// "/mnt/disk1/downloads"). Both empty when they share a mount.
	PathFrom string `json:"pathFrom,omitempty"`
	PathTo   string `json:"pathTo,omitempty"`
	// IncompleteDir is the locked in-progress folder, as ReelStash sees it.
	IncompleteDir string `json:"incompleteDir,omitempty"`
	// AutoMove runs the move rules on torrents that finish. RulesSince keeps
	// it from sweeping up torrents that finished before it was switched on.
	AutoMove   bool   `json:"autoMove"`
	RulesSince int64  `json:"rulesSince,omitempty"`
	MoveTag    string `json:"moveTag,omitempty"`
}

// LoadSettings reads the saved settings.
func LoadSettings(ctx context.Context, db *pgxpool.Pool) Settings {
	var s Settings
	var raw string
	if err := db.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

// SaveSettings replaces the saved settings.
func SaveSettings(ctx context.Context, db *pgxpool.Pool, s Settings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, settingsKey, string(raw))
	return err
}

// Connection is the qBittorrent to use: saved fields over the environment.
func (s Settings) Connection(env qbit.Config) qbit.Config {
	c := env
	if s.URL != "" {
		c.URL = s.URL
	}
	if s.Username != "" {
		c.Username = s.Username
	}
	if s.Password != "" {
		c.Password = s.Password
	}
	return c
}

// Tag is the tag added after a move.
func (s Settings) Tag() string {
	if t := strings.TrimSpace(s.MoveTag); t != "" {
		return t
	}
	return DefaultMoveTag
}

// Paths translates between qBittorrent's and ReelStash's view of a path.
type Paths struct{ From, To string }

// Paths returns the translation for these settings.
func (s Settings) Paths() Paths {
	return Paths{From: cleanPrefix(s.PathFrom), To: cleanPrefix(s.PathTo)}
}

func cleanPrefix(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(p))
}

func swapPrefix(p, from, to string) string {
	if p == "" || from == "" || to == "" {
		return p
	}
	clean := filepath.Clean(p)
	if clean == from {
		return to
	}
	if strings.HasPrefix(clean, from+string(filepath.Separator)) {
		return to + clean[len(from):]
	}
	return p
}

// Local is qBittorrent's path p as ReelStash sees it.
func (p Paths) Local(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(swapPrefix(path, p.From, p.To))
}

// Remote is ReelStash's path as qBittorrent sees it.
func (p Paths) Remote(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(swapPrefix(path, p.To, p.From))
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	if dir == "" || p == "" {
		return false
	}
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) || dir == string(filepath.Separator)
}

// Location is a folder on a storage location.
type Location struct {
	StorageLocationID int64  `json:"storageLocationId"`
	StorageLocation   string `json:"storageLocation"`
	Path              string `json:"path"` // relative, slash-separated
}

type storageRow struct {
	id    int64
	name  string
	mount string
}

func loadStorage(ctx context.Context, db *pgxpool.Pool) ([]storageRow, error) {
	rows, err := db.Query(ctx, `SELECT id, name, mount_path FROM storage_location ORDER BY length(mount_path) DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (storageRow, error) {
		var s storageRow
		err := r.Scan(&s.id, &s.name, &s.mount)
		s.mount = filepath.Clean(s.mount)
		return s, err
	})
}

// ErrOutsideStorage means a path is not on any storage location.
var ErrOutsideStorage = errors.New("that folder is not on a storage location")

// ResolveDir turns a storage location and a relative folder into an
// absolute path, refusing paths that climb out of the location.
func ResolveDir(ctx context.Context, db *pgxpool.Pool, storageLocationID int64, rel string) (string, error) {
	var mount string
	if err := db.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, storageLocationID).Scan(&mount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("storage location not found")
		}
		return "", err
	}
	mount = filepath.Clean(mount)
	abs := filepath.Join(mount, filepath.Clean("/"+rel))
	if !within(abs, mount) {
		return "", errors.New("path escapes the storage location")
	}
	return abs, nil
}

// locate finds the storage location holding abs (the innermost one).
func locate(storage []storageRow, abs string) (Location, bool) {
	if abs == "" {
		return Location{}, false
	}
	abs = filepath.Clean(abs)
	for _, s := range storage {
		if within(abs, s.mount) {
			rel, err := filepath.Rel(s.mount, abs)
			if err != nil {
				continue
			}
			if rel == "." {
				rel = ""
			}
			return Location{StorageLocationID: s.id, StorageLocation: s.name, Path: filepath.ToSlash(rel)}, true
		}
	}
	return Location{}, false
}

// Locate finds the storage location holding abs.
func Locate(ctx context.Context, db *pgxpool.Pool, abs string) (Location, error) {
	storage, err := loadStorage(ctx, db)
	if err != nil {
		return Location{}, err
	}
	loc, ok := locate(storage, abs)
	if !ok {
		return Location{}, ErrOutsideStorage
	}
	return loc, nil
}

// libraryRoot is one library folder, absolute.
type libraryRoot struct {
	libraryID int64
	name      string
	abs       string
}

func loadLibraryRoots(ctx context.Context, db *pgxpool.Pool) ([]libraryRoot, error) {
	rows, err := db.Query(ctx, `
		SELECT lr.library_id, l.name, sl.mount_path, lr.path
		FROM library_root lr
		JOIN library l ON l.id = lr.library_id
		JOIN storage_location sl ON sl.id = lr.storage_location_id`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (libraryRoot, error) {
		var lr libraryRoot
		var mount, rel string
		err := r.Scan(&lr.libraryID, &lr.name, &mount, &rel)
		lr.abs = filepath.Join(filepath.Clean(mount), filepath.FromSlash(rel))
		return lr, err
	})
	if err != nil {
		return nil, err
	}
	// Innermost first, so a nested library folder wins over its parent.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len(out[j].abs) > len(out[j-1].abs); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// libraryFor returns the library whose folder holds abs.
func libraryFor(roots []libraryRoot, abs string) (libraryRoot, bool) {
	for _, r := range roots {
		if within(abs, r.abs) {
			return r, true
		}
	}
	return libraryRoot{}, false
}

// SetProtected replaces the downloads-owned locked folder ("" removes it).
func SetProtected(ctx context.Context, db *pgxpool.Pool, abs string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM protected_path WHERE owner = 'downloads'`); err != nil {
		return err
	}
	if abs != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO protected_path (path, owner) VALUES ($1, 'downloads') ON CONFLICT (path) DO UPDATE SET owner = 'downloads'`, filepath.Clean(abs)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CheckIncompleteDir refuses an in-progress folder that is or holds a
// library folder: locking it would hide the whole library from scans. A
// folder inside a library is fine (a library may span a whole disk); being
// locked is what keeps scans out of it.
func CheckIncompleteDir(ctx context.Context, db *pgxpool.Pool, abs string) error {
	roots, err := loadLibraryRoots(ctx, db)
	if err != nil {
		return err
	}
	for _, r := range roots {
		if within(r.abs, abs) {
			return errors.New("the in-progress folder can't be or hold the library folder of \"" + r.name + "\"; pick a folder inside it or elsewhere")
		}
	}
	return nil
}

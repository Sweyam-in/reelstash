// Package housekeeping finds and removes what ReelStash leaves behind over
// time: generated images and clips no row points at any more, rows whose
// files are gone, stale caches, half-written temporary files, old job and
// notification history, and (only when asked) orphaned sidecar files and OS
// clutter in library folders.
//
// Every task runs in two modes: a dry run that only counts what it would
// remove, and a cleanup that removes it. Files younger than an hour are never
// touched, so work in progress (a thumbnail written just before its row) is
// safe.
package housekeeping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/config"
	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/pathguard"
)

// JobKind is the background job that runs housekeeping.
const JobKind = "HOUSEKEEPING"

// grace is how old a file must be before it can count as left over.
const grace = time.Hour

// maxSamples bounds the examples each finding keeps for the report.
const maxSamples = 20

// Dirs are the folders ReelStash writes generated files to.
type Dirs struct {
	Thumbnails string
	Sprites    string
	Previews   string
	Posters    string
	Transcode  string // HLS segment cache
	Remux      string
	Subtitles  string
	Plugins    string
}

// Settings say how long history is kept.
type Settings struct {
	JobDays          int `json:"jobDays"`
	NotificationDays int `json:"notificationDays"`
	CapacityDays     int `json:"capacityDays"`
	CacheDays        int `json:"cacheDays"`
}

// DefaultSettings are used until an admin changes them.
var DefaultSettings = Settings{JobDays: 30, NotificationDays: 90, CapacityDays: 365, CacheDays: 30}

const settingsKey = "housekeeping_settings"

// LoadSettings reads the saved settings, falling back to the defaults for
// anything missing or invalid.
func LoadSettings(ctx context.Context, db *pgxpool.Pool) Settings {
	s := DefaultSettings
	var raw string
	if err := db.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = $1`, settingsKey).Scan(&raw); err == nil {
		var saved Settings
		if json.Unmarshal([]byte(raw), &saved) == nil {
			s = merge(s, saved)
		}
	}
	return s
}

func merge(base, over Settings) Settings {
	pick := func(b, o int) int {
		if o > 0 {
			return o
		}
		return b
	}
	return Settings{
		JobDays:          pick(base.JobDays, over.JobDays),
		NotificationDays: pick(base.NotificationDays, over.NotificationDays),
		CapacityDays:     pick(base.CapacityDays, over.CapacityDays),
		CacheDays:        pick(base.CacheDays, over.CacheDays),
	}
}

// Validate rejects retention periods that make no sense.
func (s Settings) Validate() error {
	for name, v := range map[string]int{"jobDays": s.JobDays, "notificationDays": s.NotificationDays, "capacityDays": s.CapacityDays, "cacheDays": s.CacheDays} {
		if v < 1 || v > 3650 {
			return fmt.Errorf("%s must be between 1 and 3650", name)
		}
	}
	return nil
}

// SaveSettings stores s.
func SaveSettings(ctx context.Context, db *pgxpool.Pool, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	_, err := db.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, settingsKey, string(raw))
	return err
}

// Group sorts tasks on the admin page.
const (
	GroupFiles    = "files"    // ReelStash's own data folders
	GroupDatabase = "database" // rows only
	GroupLibrary  = "library"  // files beside the user's videos
)

// Task is one kind of clean-up.
type Task struct {
	ID          string `json:"id"`
	Group       string `json:"group"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Default tasks run when no list is given (scheduled runs).
	Default bool `json:"default"`
	run     func(r *runner, f *Finding) error
}

// Finding is what one task found (dry run) or removed.
type Finding struct {
	Task    string   `json:"task"`
	Count   int      `json:"count"`
	Bytes   int64    `json:"bytes"`
	Samples []string `json:"samples"`
	Skipped []string `json:"skipped,omitempty"`
	Failed  int      `json:"failed,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func (f *Finding) add(sample string, bytes int64) {
	f.Count++
	f.Bytes += bytes
	if len(f.Samples) < maxSamples {
		f.Samples = append(f.Samples, sample)
	}
}

func (f *Finding) skip(reason string) { f.Skipped = append(f.Skipped, reason) }

// Report is a whole run.
type Report struct {
	DryRun   bool      `json:"dryRun"`
	Settings Settings  `json:"settings"`
	Findings []Finding `json:"findings"`
	Count    int       `json:"count"`
	Bytes    int64     `json:"bytes"`
}

// Service runs housekeeping.
type Service struct {
	DB   *pgxpool.Pool
	Dirs Dirs
	Now  func() time.Time // for tests
}

func New(db *pgxpool.Pool, dirs Dirs) *Service {
	return &Service{DB: db, Dirs: dirs, Now: time.Now}
}

// Tasks lists every task in the order they run.
func Tasks() []Task { return tasks }

// DefaultTaskIDs are the tasks a run without a list performs.
func DefaultTaskIDs() []string {
	var ids []string
	for _, t := range tasks {
		if t.Default {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// Payload is the HOUSEKEEPING job's input. No tasks means the defaults.
type Payload struct {
	Tasks  []string `json:"tasks"`
	DryRun bool     `json:"dryRun"`
}

// Handler runs the HOUSEKEEPING job.
func (s *Service) Handler() jobs.Handler {
	return func(ctx context.Context, db *pgxpool.Pool, jobID int64, payload json.RawMessage) (any, error) {
		var p Payload
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
		}
		return s.Run(ctx, jobID, p.Tasks, p.DryRun)
	}
}

// ErrUnknownTask means a task id is not one of Tasks.
var ErrUnknownTask = errors.New("unknown housekeeping task")

// CheckTasks rejects unknown task ids.
func CheckTasks(ids []string) error {
	for _, id := range ids {
		if taskByID(id) == nil {
			return fmt.Errorf("%w: %s", ErrUnknownTask, id)
		}
	}
	return nil
}

func taskByID(id string) *Task {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i]
		}
	}
	return nil
}

// Run performs the given tasks (the defaults when ids is empty), in the
// order Tasks lists them. jobID, when non-zero, receives progress.
func (s *Service) Run(ctx context.Context, jobID int64, ids []string, dryRun bool) (Report, error) {
	if len(ids) == 0 {
		ids = DefaultTaskIDs()
	}
	if err := CheckTasks(ids); err != nil {
		return Report{}, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	r := &runner{ctx: ctx, s: s, apply: !dryRun, now: s.Now(), settings: LoadSettings(ctx, s.DB)}
	rep := Report{DryRun: dryRun, Settings: r.settings, Findings: []Finding{}}
	done := 0
	for _, t := range tasks {
		if !want[t.ID] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if jobID != 0 {
			jobs.ReportProgress(s.DB, jobID, done, len(want), t.Title)
		}
		f := Finding{Task: t.ID, Samples: []string{}}
		if err := t.run(r, &f); err != nil {
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
			f.Error = err.Error()
		}
		rep.Findings = append(rep.Findings, f)
		rep.Count += f.Count
		rep.Bytes += f.Bytes
		done++
	}
	if jobID != 0 {
		jobs.ReportProgress(s.DB, jobID, done, len(want), "")
	}
	return rep, nil
}

// runner is the state of one run.
type runner struct {
	ctx      context.Context
	s        *Service
	apply    bool
	now      time.Time
	settings Settings

	fileIDs map[int64]bool // media_file ids, loaded on first use
	media   *mediaWalk     // library folder walk, shared by the library tasks
	paths   *pathguard.Guard
}

// guard is what housekeeping must not touch, loaded on first use.
func (r *runner) guard() *pathguard.Guard {
	if r.paths == nil {
		r.paths = pathguard.Load(r.ctx, r.s.DB)
	}
	return r.paths
}

func (r *runner) db() *pgxpool.Pool { return r.s.DB }

func (r *runner) days(n int) time.Time { return r.now.Add(-time.Duration(n) * 24 * time.Hour) }

// settled reports whether a file is old enough to be judged.
func (r *runner) settled(info os.FileInfo) bool { return r.now.Sub(info.ModTime()) >= grace }

// mediaFileIDs is every media_file id, for the per-file caches.
func (r *runner) mediaFileIDs() (map[int64]bool, error) {
	if r.fileIDs != nil {
		return r.fileIDs, nil
	}
	rows, err := r.db().Query(r.ctx, `SELECT id FROM media_file`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	r.fileIDs = make(map[int64]bool, len(ids))
	for _, id := range ids {
		r.fileIDs[id] = true
	}
	return r.fileIDs, nil
}

// remove deletes path (a file or a whole folder) when applying, counting a
// failure instead of stopping the task.
func (r *runner) remove(f *Finding, path string, all bool) {
	if !r.apply {
		return
	}
	if r.guard().Inside(path) {
		return
	}
	var err error
	if all {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil && !os.IsNotExist(err) {
		f.Failed++
	}
}

// managedDirs are the data folders by role, cleaned, without empties.
func (s *Service) managedDirs() map[string]string {
	out := map[string]string{}
	for name, d := range map[string]string{
		"thumbnails": s.Dirs.Thumbnails, "sprites": s.Dirs.Sprites, "previews": s.Dirs.Previews, "posters": s.Dirs.Posters,
		"transcode": s.Dirs.Transcode, "remux": s.Dirs.Remux, "subtitles": s.Dirs.Subtitles, "plugins": s.Dirs.Plugins,
	} {
		if d != "" {
			out[name] = filepath.Clean(d)
		}
	}
	return out
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// readableDir reports whether dir exists and can be listed.
func readableDir(dir string) error {
	if dir == "" {
		return errors.New("not configured")
	}
	if _, err := os.ReadDir(dir); err != nil {
		return err
	}
	return nil
}

// DirsFromConfig is where the server keeps each kind of generated file; it
// mirrors the paths the API serves them from.
func DirsFromConfig(cfg config.Config) Dirs {
	data := filepath.Dir(cfg.ThumbnailDir)
	subtitles := cfg.SubtitleDir
	if subtitles == "" {
		subtitles = filepath.Join(data, "subtitles")
	}
	return Dirs{
		Thumbnails: cfg.ThumbnailDir, Sprites: cfg.SpriteDir, Previews: cfg.PreviewDir, Posters: cfg.PosterDir,
		Transcode: filepath.Join(data, "hls"), Remux: filepath.Join(data, "remux"), Subtitles: subtitles, Plugins: cfg.PluginDir,
	}
}

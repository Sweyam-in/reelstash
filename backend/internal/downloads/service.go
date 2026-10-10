package downloads

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/qbit"
)

// Poll intervals: fast while someone has the Downloads workspace open, slow
// otherwise (still enough to notice finished torrents), and slower again
// while qBittorrent can't be reached.
const (
	pollFast    = 2 * time.Second
	pollSlow    = 20 * time.Second
	pollOffline = 30 * time.Second
	demandTTL   = 20 * time.Second
	// prefsEvery is how often qBittorrent's settings are re-read to notice
	// changes made in its own web UI.
	prefsEvery = 5 * time.Minute
	// moveGrace is how long a move may show no sign of starting before it
	// counts as failed; moveTimeout bounds a move that never finishes.
	moveGrace   = 2 * time.Minute
	moveTimeout = 12 * time.Hour
)

// Notifier tells admins about moves (failed=false) and problems.
type Notifier func(ctx context.Context, failed bool, title, body string)

// Service is the live link to qBittorrent.
type Service struct {
	DB     *pgxpool.Pool
	Runner *jobs.Runner
	// Env is the connection from the environment, used for fields the admin
	// has not saved.
	Env    qbit.Config
	Notify Notifier

	mu         sync.RWMutex
	client     *qbit.Client
	clientCfg  qbit.Config
	state      *qbit.State
	connected  bool
	lastErr    string
	lastSync   time.Time
	version    string
	apiVersion string
	prefs      map[string]any
	prefsAt    time.Time
	settings   Settings
	paths      Paths

	demand atomic.Int64
	kick   chan struct{}
}

// New returns a service; call Run to start polling.
func New(db *pgxpool.Pool, runner *jobs.Runner, env qbit.Config, notify Notifier) *Service {
	s := &Service{DB: db, Runner: runner, Env: env, Notify: notify, state: qbit.NewState(), kick: make(chan struct{}, 1)}
	pathguard.SetInUseSource(s.InUse)
	return s
}

// Touch marks the workspace as open, so the list refreshes quickly.
func (s *Service) Touch() {
	prev := s.demand.Swap(time.Now().UnixNano())
	if time.Since(time.Unix(0, prev)) > demandTTL {
		s.Kick()
	}
}

// Kick asks for a refresh now.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Service) wanted() bool { return time.Since(time.Unix(0, s.demand.Load())) < demandTTL }

// Client returns the client for the current settings, rebuilding it (and
// starting over with a full sync) when the connection settings change.
func (s *Service) Client(ctx context.Context) (*qbit.Client, Settings, error) {
	settings := LoadSettings(ctx, s.DB)
	cfg := settings.Connection(s.Env)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings, s.paths = settings, settings.Paths()
	if !cfg.Enabled() {
		s.client, s.connected = nil, false
		return nil, settings, qbit.ErrNotConfigured
	}
	if s.client == nil || s.clientCfg != cfg {
		s.client, s.clientCfg = qbit.New(cfg), cfg
		s.state = qbit.NewState()
		s.connected, s.prefs, s.version, s.apiVersion = false, nil, "", ""
	}
	return s.client, settings, nil
}

// Paths returns the current path translation.
func (s *Service) Paths() Paths {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paths
}

// Run polls qBittorrent until ctx ends.
func (s *Service) Run(ctx context.Context) {
	for {
		wait := s.tick(ctx)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.kick:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// tick syncs once and runs the move rules; it returns how long to wait.
func (s *Service) tick(ctx context.Context) time.Duration {
	client, settings, err := s.Client(ctx)
	if err != nil {
		return pollOffline
	}
	tctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := s.sync(tctx, client); err != nil {
		return pollOffline
	}
	s.refreshPrefs(tctx, client, false)
	if err := s.process(tctx, client, settings); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("downloads: %v", err)
	}
	if s.wanted() {
		return pollFast
	}
	return pollSlow
}

func (s *Service) sync(ctx context.Context, client *qbit.Client) error {
	s.mu.RLock()
	rid := s.state.RID
	s.mu.RUnlock()
	md, err := client.MainData(ctx, rid)
	if err != nil {
		s.mu.Lock()
		if s.client == client {
			s.connected, s.lastErr = false, err.Error()
		}
		s.mu.Unlock()
		return err
	}
	var version, apiVersion string
	s.mu.RLock()
	needVersion := s.version == ""
	s.mu.RUnlock()
	if needVersion {
		version, _ = client.Version(ctx)
		apiVersion, _ = client.APIVersion(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != client {
		return nil
	}
	s.state.Apply(md)
	s.connected, s.lastErr, s.lastSync = true, "", time.Now()
	if needVersion {
		s.version, s.apiVersion = version, apiVersion
	}
	return nil
}

// refreshPrefs re-reads qBittorrent's settings every few minutes (or now).
func (s *Service) refreshPrefs(ctx context.Context, client *qbit.Client, force bool) map[string]any {
	s.mu.RLock()
	prefs, at := s.prefs, s.prefsAt
	s.mu.RUnlock()
	if !force && prefs != nil && time.Since(at) < prefsEvery {
		return prefs
	}
	fresh, err := client.Preferences(ctx)
	if err != nil {
		return prefs
	}
	s.mu.Lock()
	if s.client == client {
		s.prefs, s.prefsAt = fresh, time.Now()
	}
	s.mu.Unlock()
	return fresh
}

// Prefs returns qBittorrent's settings, re-reading them.
func (s *Service) Prefs(ctx context.Context) (map[string]any, error) {
	client, _, err := s.Client(ctx)
	if err != nil {
		return nil, err
	}
	prefs, err := client.Preferences(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.prefs, s.prefsAt = prefs, time.Now()
	s.mu.Unlock()
	return prefs, nil
}

// SetPrefs changes qBittorrent settings and refreshes the cached copy.
func (s *Service) SetPrefs(ctx context.Context, prefs map[string]any) error {
	client, _, err := s.Client(ctx)
	if err != nil {
		return err
	}
	if err := client.SetPreferences(ctx, prefs); err != nil {
		return err
	}
	s.refreshPrefs(ctx, client, true)
	return nil
}

// torrents copies the current torrent list.
func (s *Service) torrents() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]map[string]any, 0, len(s.state.Torrents))
	for _, t := range s.state.Torrents {
		c := make(map[string]any, len(t))
		for k, v := range t {
			c[k] = v
		}
		out = append(out, c)
	}
	return out
}

// torrent returns one torrent's typed view.
func (s *Service) torrent(hash string) (qbit.Torrent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.state.Torrents[hash]
	if !ok {
		return qbit.Torrent{}, false
	}
	return qbit.View(t), true
}

// InUse reports every torrent's data as in use, so nothing else in ReelStash
// moves or deletes it. The last known list is kept while qBittorrent is
// unreachable: forgetting it would drop the protection.
func (s *Service) InUse() []pathguard.InUse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]pathguard.InUse, 0, len(s.state.Torrents))
	for _, t := range s.state.Torrents {
		v := qbit.View(t)
		p := s.paths.Local(v.ContentPath)
		if p == "" {
			continue
		}
		out = append(out, pathguard.InUse{Path: p, Name: v.Name})
	}
	return out
}

// Status is the connection summary.
type Status struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	Error      string `json:"error,omitempty"`
	Version    string `json:"version,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
	URL        string `json:"url,omitempty"`
	LastSync   string `json:"lastSync,omitempty"`
}

// Status reports whether qBittorrent is reachable.
func (s *Service) Status(ctx context.Context) Status {
	settings := LoadSettings(ctx, s.DB)
	cfg := settings.Connection(s.Env)
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Status{Configured: cfg.Enabled(), URL: cfg.URL}
	if s.client != nil && s.clientCfg == cfg {
		st.Connected, st.Error, st.Version, st.APIVersion = s.connected, s.lastErr, s.version, s.apiVersion
		if !s.lastSync.IsZero() {
			st.LastSync = s.lastSync.UTC().Format(time.RFC3339)
		}
	}
	return st
}

// snapshotKeys are the torrent fields the workspace shows.
var snapshotKeys = []string{
	"hash", "name", "state", "progress", "size", "total_size", "dlspeed", "upspeed", "eta", "ratio",
	"num_seeds", "num_complete", "num_leechs", "num_incomplete", "category", "tags", "save_path", "content_path",
	"added_on", "completion_on", "amount_left", "downloaded", "uploaded", "seeding_time", "tracker", "priority",
	"force_start", "seq_dl", "f_l_piece_prio", "dl_limit", "up_limit", "availability", "time_active", "auto_tmm",
	"ratio_limit", "seeding_time_limit",
}

// MoveInfo is what happened to a finished torrent.
type MoveInfo struct {
	Status   string `json:"status"`
	RuleName string `json:"ruleName,omitempty"`
	Message  string `json:"message,omitempty"`
	Dest     string `json:"dest,omitempty"`
}

// Snapshot is everything the workspace list needs.
type Snapshot struct {
	Status
	Torrents   []map[string]any `json:"torrents"`
	Categories []map[string]any `json:"categories"`
	Tags       []string         `json:"tags"`
	Server     map[string]any   `json:"server"`
	Warnings   []Warning        `json:"warnings"`
	AutoMove   bool             `json:"autoMove"`
	Incomplete *Location        `json:"incomplete,omitempty"`
}

// Warning is something about the setup the admin should fix.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"` // the action the UI offers
}

// Snapshot builds the workspace view: torrents with the library and folder
// they sit in, and what the move rules did with them.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	s.Touch()
	snap := Snapshot{Status: s.Status(ctx), Torrents: []map[string]any{}, Categories: []map[string]any{}, Tags: []string{}, Server: map[string]any{}, Warnings: []Warning{}}
	settings := LoadSettings(ctx, s.DB)
	snap.AutoMove = settings.AutoMove
	storage, _ := loadStorage(ctx, s.DB)
	roots, _ := loadLibraryRoots(ctx, s.DB)
	if settings.IncompleteDir != "" {
		if loc, ok := locate(storage, settings.IncompleteDir); ok {
			snap.Incomplete = &loc
		} else {
			snap.Incomplete = &Location{Path: settings.IncompleteDir}
		}
	}
	paths := settings.Paths()

	raw := s.torrents()
	hashes := make([]string, 0, len(raw))
	for _, t := range raw {
		hashes = append(hashes, qbit.Str(t, "hash"))
	}
	moves := s.moveInfo(ctx, hashes)
	for _, t := range raw {
		out := make(map[string]any, len(snapshotKeys)+4)
		for _, k := range snapshotKeys {
			if v, ok := t[k]; ok {
				out[k] = v
			}
		}
		v := qbit.View(t)
		content := paths.Local(v.ContentPath)
		if lib, ok := libraryFor(roots, content); ok {
			out["library"] = map[string]any{"id": lib.libraryID, "name": lib.name}
		}
		folder := paths.Local(v.SavePath)
		if loc, ok := locate(storage, folder); ok {
			out["folder"] = loc
		}
		if m, ok := moves[v.Hash]; ok {
			out["move"] = m
		}
		snap.Torrents = append(snap.Torrents, out)
	}
	sort.Slice(snap.Torrents, func(i, j int) bool {
		return qbit.Num(snap.Torrents[i], "added_on") > qbit.Num(snap.Torrents[j], "added_on")
	})

	s.mu.RLock()
	for _, name := range qbit.SortedKeys(s.state.Categories) {
		c := s.state.Categories[name]
		entry := map[string]any{"name": name, "savePath": qbit.Str(c, "savePath")}
		if loc, ok := locate(storage, paths.Local(qbit.Str(c, "savePath"))); ok {
			entry["folder"] = loc
		}
		snap.Categories = append(snap.Categories, entry)
	}
	snap.Tags = append(snap.Tags, qbit.SortedKeys(s.state.Tags)...)
	for k, v := range s.state.Server {
		snap.Server[k] = v
	}
	prefs := s.prefs
	s.mu.RUnlock()

	snap.Warnings = setupWarnings(settings, prefs, roots)
	return snap, nil
}

// setupWarnings compares qBittorrent's folders with ReelStash's settings.
func setupWarnings(settings Settings, prefs map[string]any, roots []libraryRoot) []Warning {
	out := []Warning{}
	if prefs == nil {
		return out
	}
	paths := settings.Paths()
	tempOn, _ := prefs["temp_path_enabled"].(bool)
	temp := paths.Local(qbit.Str(prefs, "temp_path"))
	switch {
	case settings.IncompleteDir == "" && tempOn && temp != "":
		out = append(out, Warning{Code: "incomplete-unprotected",
			Message: "qBittorrent keeps unfinished downloads in " + temp + ", but ReelStash does not protect that folder yet.",
			Fix:     "protect-incomplete"})
	case settings.IncompleteDir == "":
		out = append(out, Warning{Code: "incomplete-missing",
			Message: "Unfinished downloads are saved straight into their final folder. Set an in-progress folder so scans never see half-downloaded files."})
	case !tempOn || filepath.Clean(temp) != filepath.Clean(settings.IncompleteDir):
		got := "off"
		if tempOn {
			got = temp
		}
		out = append(out, Warning{Code: "incomplete-drift",
			Message: "qBittorrent's in-progress folder (" + got + ") no longer matches the folder ReelStash protects (" + settings.IncompleteDir + ").",
			Fix:     "apply-incomplete"})
	}
	if save := paths.Local(qbit.Str(prefs, "save_path")); save != "" {
		if lib, ok := libraryFor(roots, save); ok && settings.IncompleteDir == "" {
			out = append(out, Warning{Code: "save-in-library",
				Message: "qBittorrent saves downloads inside the library \"" + lib.name + "\" with no in-progress folder, so scans can pick up unfinished files."})
		}
	}
	return out
}

func (s *Service) moveInfo(ctx context.Context, hashes []string) map[string]MoveInfo {
	out := map[string]MoveInfo{}
	if len(hashes) == 0 {
		return out
	}
	rows, err := s.DB.Query(ctx, `SELECT hash, status, rule_name, message, dest_path FROM download_move WHERE hash = ANY($1)`, hashes)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		var m MoveInfo
		if rows.Scan(&hash, &m.Status, &m.RuleName, &m.Message, &m.Dest) == nil {
			out[hash] = m
		}
	}
	return out
}

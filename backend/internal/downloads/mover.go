package downloads

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/qbit"
	"reelstash/backend/internal/schedule"
)

// Move statuses (download_move.status).
const (
	StatusMoving  = "MOVING"
	StatusMoved   = "MOVED"
	StatusFailed  = "FAILED"
	StatusSkipped = "SKIPPED"
	StatusRemoved = "REMOVED"
)

// process runs one round of the move machinery.
func (s *Service) process(ctx context.Context, client *qbit.Client, settings Settings) error {
	var errs []error
	if settings.AutoMove {
		if err := s.applyRules(ctx, client, settings); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.advanceMoves(ctx, client, settings); err != nil {
		errs = append(errs, err)
	}
	if err := s.queueScans(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.removeSeeded(ctx, client); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Plan is where a rule would send a finished torrent.
type Plan struct {
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	RuleID   int64  `json:"ruleId,omitempty"`
	RuleName string `json:"ruleName,omitempty"`
	From     string `json:"from"`
	Dest     string `json:"dest,omitempty"`
	Skip     string `json:"skip,omitempty"` // why nothing would happen
	rule     *Rule
}

// plan decides what the rules do with one finished torrent.
// force skips the default-folder check, for a run asked for by hand.
func (s *Service) plan(ctx context.Context, client *qbit.Client, settings Settings, rules []Rule, t qbit.Torrent, prefs map[string]any, categories map[string]string, force bool) Plan {
	paths := settings.Paths()
	p := Plan{Hash: t.Hash, Name: t.Name, From: paths.Local(t.SavePath)}
	// A torrent added with a folder of its own was placed on purpose; only
	// torrents in qBittorrent's default (or category) folder are moved.
	if !force && !inDownloadArea(p.From, paths, prefs, categories[t.Category]) {
		p.Skip = "Saved to a folder of its own when added"
		return p
	}
	files := func(hash string) ([]string, error) {
		list, err := client.Files(ctx, hash)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(list))
		for _, f := range list {
			names = append(names, qbit.Str(f, "name"))
		}
		return names, nil
	}
	for i := range rules {
		ok, err := rules[i].Matches(t, files)
		if err != nil {
			p.Skip = "Could not read the torrent's files: " + err.Error()
			return p
		}
		if !ok {
			continue
		}
		r := rules[i]
		dest, err := ResolveDir(ctx, s.DB, r.DestStorageLocationID, r.DestPath)
		if err != nil {
			p.Skip = "Rule \"" + r.Name + "\": " + err.Error()
			return p
		}
		if sub := ExpandSubfolder(r.Subfolder, t); sub != "" {
			dest = filepath.Join(dest, filepath.FromSlash(sub))
		}
		p.RuleID, p.RuleName, p.Dest, p.rule = r.ID, r.Name, dest, &r
		if pathguard.Load(ctx, s.DB).Locked(dest) {
			p.Skip = "The destination is inside the protected in-progress folder"
		}
		return p
	}
	p.Skip = "No rule matched"
	return p
}

// inDownloadArea reports whether a torrent sits in qBittorrent's default
// save folder or its category's folder.
func inDownloadArea(saveDir string, paths Paths, prefs map[string]any, categoryPath string) bool {
	if prefs == nil {
		return true // settings unknown yet: don't skip on a guess
	}
	def := paths.Local(qbit.Str(prefs, "save_path"))
	if def != "" && within(saveDir, def) {
		return true
	}
	if categoryPath != "" {
		cp := categoryPath
		if !filepath.IsAbs(cp) && def != "" {
			cp = filepath.Join(def, cp)
		}
		if within(saveDir, paths.Local(cp)) {
			return true
		}
	}
	return def == ""
}

func (s *Service) categoryPaths() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	for name, c := range s.state.Categories {
		out[name] = qbit.Str(c, "savePath")
	}
	return out
}

func (s *Service) cachedPrefs() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefs
}

// applyRules moves torrents that finished since auto-move was switched on
// and have not been looked at yet.
func (s *Service) applyRules(ctx context.Context, client *qbit.Client, settings Settings) error {
	var fresh []qbit.Torrent
	for _, raw := range s.torrents() {
		t := qbit.View(raw)
		if t.Complete() && t.CompletedOn >= settings.RulesSince {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	hashes := make([]string, len(fresh))
	for i, t := range fresh {
		hashes[i] = t.Hash
	}
	rows, err := s.DB.Query(ctx, `SELECT hash FROM download_move WHERE hash = ANY($1)`, hashes)
	if err != nil {
		return err
	}
	seen, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, h := range seen {
		done[h] = true
	}
	var todo []qbit.Torrent
	for _, t := range fresh {
		if !done[t.Hash] {
			todo = append(todo, t)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	rules, err := ListRules(ctx, s.DB)
	if err != nil {
		return err
	}
	prefs, categories := s.cachedPrefs(), s.categoryPaths()
	for _, t := range todo {
		s.start(ctx, client, settings, s.plan(ctx, client, settings, rules, t, prefs, categories, false), false)
	}
	return nil
}

// start records a plan and, when it moves something, asks qBittorrent to
// move the data. replace overwrites an earlier record for the torrent.
func (s *Service) start(ctx context.Context, client *qbit.Client, settings Settings, p Plan, replace bool) (MoveInfo, error) {
	conflict := `ON CONFLICT (hash) DO NOTHING`
	if replace {
		conflict = `ON CONFLICT (hash) DO UPDATE SET name = EXCLUDED.name, rule_id = EXCLUDED.rule_id, rule_name = EXCLUDED.rule_name,
			status = EXCLUDED.status, from_path = EXCLUDED.from_path, dest_path = EXCLUDED.dest_path, message = EXCLUDED.message,
			scan_pending = false, library_id = EXCLUDED.library_id, remove_after = EXCLUDED.remove_after,
			remove_ratio = EXCLUDED.remove_ratio, remove_seed_minutes = EXCLUDED.remove_seed_minutes,
			created_at = now(), updated_at = now(), moved_at = NULL`
	}
	insert := func(status, message string, libraryID *int64) error {
		var ruleID *int64
		removeAfter, ratio, minutes := RemoveNever, 0.0, int64(0)
		if p.rule != nil {
			if p.rule.ID != 0 {
				ruleID = &p.rule.ID
			}
			removeAfter, ratio, minutes = p.rule.RemoveAfter, p.rule.RemoveRatio, p.rule.RemoveSeedMinutes
		}
		_, err := s.DB.Exec(ctx, `
			INSERT INTO download_move (hash, name, rule_id, rule_name, status, from_path, dest_path, message, library_id,
				remove_after, remove_ratio, remove_seed_minutes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) `+conflict,
			p.Hash, p.Name, ruleID, p.RuleName, status, p.From, p.Dest, message, libraryID, removeAfter, ratio, minutes)
		return err
	}
	if p.Skip != "" {
		return MoveInfo{Status: StatusSkipped, RuleName: p.RuleName, Message: p.Skip}, insert(StatusSkipped, p.Skip, nil)
	}
	var libraryID *int64
	scan := p.rule == nil || p.rule.ScanAfter
	if scan {
		if roots, err := loadLibraryRoots(ctx, s.DB); err == nil {
			if lib, ok := libraryFor(roots, p.Dest); ok {
				libraryID = &lib.libraryID
			}
		}
	}
	if filepath.Clean(p.Dest) == filepath.Clean(p.From) {
		if err := insert(StatusMoved, "Already in place", libraryID); err != nil {
			return MoveInfo{}, err
		}
		_, err := s.DB.Exec(ctx, `UPDATE download_move SET moved_at = now(), scan_pending = $2 WHERE hash = $1`, p.Hash, libraryID != nil)
		return MoveInfo{Status: StatusMoved, RuleName: p.RuleName, Message: "Already in place", Dest: p.Dest}, err
	}
	if err := insert(StatusMoving, "", libraryID); err != nil {
		return MoveInfo{}, err
	}
	if err := client.SetLocation(ctx, []string{p.Hash}, settings.Paths().Remote(p.Dest)); err != nil {
		s.fail(ctx, p.Hash, p.Name, err.Error())
		return MoveInfo{Status: StatusFailed, RuleName: p.RuleName, Message: err.Error(), Dest: p.Dest}, err
	}
	s.Kick()
	return MoveInfo{Status: StatusMoving, RuleName: p.RuleName, Dest: p.Dest}, nil
}

func (s *Service) fail(ctx context.Context, hash, name, message string) {
	_, _ = s.DB.Exec(ctx, `UPDATE download_move SET status = 'FAILED', message = $2, scan_pending = false, updated_at = now() WHERE hash = $1`, hash, message)
	if s.Notify != nil {
		s.Notify(ctx, true, "Could not move \""+name+"\"", message)
	}
}

type moveRow struct {
	hash, name, dest string
	updatedAt        time.Time
	createdAt        time.Time
	scan             bool
}

// advanceMoves follows moves qBittorrent is working on.
func (s *Service) advanceMoves(ctx context.Context, client *qbit.Client, settings Settings) error {
	rows, err := s.DB.Query(ctx, `SELECT hash, name, dest_path, created_at, updated_at, library_id IS NOT NULL FROM download_move WHERE status = 'MOVING'`)
	if err != nil {
		return err
	}
	moving, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (moveRow, error) {
		var m moveRow
		err := r.Scan(&m.hash, &m.name, &m.dest, &m.createdAt, &m.updatedAt, &m.scan)
		return m, err
	})
	if err != nil {
		return err
	}
	paths := settings.Paths()
	for _, m := range moving {
		t, ok := s.torrent(m.hash)
		switch {
		case !ok:
			s.fail(ctx, m.hash, m.name, "The torrent was removed from qBittorrent before the move finished")
		case t.Errored():
			s.fail(ctx, m.hash, m.name, "qBittorrent reports \""+t.State+"\" for this torrent; check its folder permissions")
		case t.Moving():
			if time.Since(m.createdAt) > moveTimeout {
				s.fail(ctx, m.hash, m.name, "The move has not finished after 12 hours")
			}
		case filepath.Clean(paths.Local(t.SavePath)) == filepath.Clean(m.dest):
			if _, err := s.DB.Exec(ctx, `UPDATE download_move SET status = 'MOVED', message = '', moved_at = now(), updated_at = now(),
				scan_pending = $2 WHERE hash = $1`, m.hash, m.scan); err != nil {
				return err
			}
			if tag := settings.Tag(); tag != "" {
				_ = client.AddTags(ctx, []string{m.hash}, tag)
			}
			if s.Notify != nil {
				s.Notify(ctx, false, "Moved \""+m.name+"\"", "Now in "+m.dest)
			}
		case time.Since(m.createdAt) > moveGrace:
			s.fail(ctx, m.hash, m.name, "qBittorrent did not move the torrent (its folder is still "+paths.Local(t.SavePath)+")")
		}
	}
	return nil
}

// queueScans scans the libraries that moved torrents landed in, one scan per
// library, retrying while another scan or the organizer holds the disk.
func (s *Service) queueScans(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT library_id FROM download_move WHERE scan_pending AND status = 'MOVED' AND library_id IS NOT NULL`)
	if err != nil {
		return err
	}
	libraries, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range libraries {
		_, err := schedule.EnqueueLibraryScan(ctx, s.DB, s.Runner, id)
		if errors.Is(err, schedule.ErrScanRunning) || errors.Is(err, schedule.ErrOrganizing) {
			continue // try again next round
		}
		msg := ""
		if err != nil {
			msg = "Moved, but the library scan could not start: " + err.Error()
		}
		if _, err := s.DB.Exec(ctx, `UPDATE download_move SET scan_pending = false, message = CASE WHEN $2 = '' THEN message ELSE $2 END, updated_at = now()
			WHERE scan_pending AND library_id = $1`, id, msg); err != nil {
			return err
		}
	}
	_, err = s.DB.Exec(ctx, `UPDATE download_move SET scan_pending = false WHERE scan_pending AND library_id IS NULL`)
	return err
}

// removeSeeded removes moved torrents from qBittorrent (never their files)
// when their rule says so, and notes torrents removed by hand.
func (s *Service) removeSeeded(ctx context.Context, client *qbit.Client) error {
	rows, err := s.DB.Query(ctx, `SELECT hash, name, remove_after, remove_ratio, remove_seed_minutes, scan_pending FROM download_move WHERE status = 'MOVED'`)
	if err != nil {
		return err
	}
	type row struct {
		hash, name, after string
		ratio             float64
		minutes           int64
		scanPending       bool
	}
	moved, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var m row
		err := r.Scan(&m.hash, &m.name, &m.after, &m.ratio, &m.minutes, &m.scanPending)
		return m, err
	})
	if err != nil {
		return err
	}
	s.mu.RLock()
	synced := s.connected && !s.lastSync.IsZero()
	s.mu.RUnlock()
	if !synced {
		return nil
	}
	for _, m := range moved {
		t, ok := s.torrent(m.hash)
		if !ok {
			_, _ = s.DB.Exec(ctx, `UPDATE download_move SET status = 'REMOVED', message = 'Removed in qBittorrent', updated_at = now() WHERE hash = $1`, m.hash)
			continue
		}
		var why string
		switch m.after {
		case RemoveMoved:
			if !m.scanPending {
				why = "Removed after moving"
			}
		case RemoveSeeded:
			if m.ratio > 0 && t.Ratio >= m.ratio {
				why = fmt.Sprintf("Removed at ratio %.2f", t.Ratio)
			} else if m.minutes > 0 && t.SeedingTime >= m.minutes*60 {
				why = fmt.Sprintf("Removed after seeding %s", (time.Duration(t.SeedingTime) * time.Second).Round(time.Minute))
			}
		}
		if why == "" {
			continue
		}
		if err := client.Delete(ctx, []string{m.hash}, false); err != nil {
			_, _ = s.DB.Exec(ctx, `UPDATE download_move SET message = $2, updated_at = now() WHERE hash = $1`, m.hash, "Could not remove from qBittorrent: "+err.Error())
			continue
		}
		_, _ = s.DB.Exec(ctx, `UPDATE download_move SET status = 'REMOVED', message = $2, updated_at = now() WHERE hash = $1`, m.hash, why)
	}
	return nil
}

// TestRules shows what the rules would do with every finished torrent now.
func (s *Service) TestRules(ctx context.Context) ([]Plan, error) {
	client, settings, err := s.Client(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := ListRules(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	prefs, categories := s.refreshPrefs(ctx, client, false), s.categoryPaths()
	out := []Plan{}
	for _, raw := range s.torrents() {
		t := qbit.View(raw)
		if !t.Complete() {
			continue
		}
		out = append(out, s.plan(ctx, client, settings, rules, t, prefs, categories, false))
	}
	return out, nil
}

// ApplyRules runs the rules on one finished torrent now, even one that
// finished before auto-move was on or was looked at before.
func (s *Service) ApplyRules(ctx context.Context, hash string) (MoveInfo, error) {
	client, settings, err := s.Client(ctx)
	if err != nil {
		return MoveInfo{}, err
	}
	t, ok := s.torrent(hash)
	if !ok {
		return MoveInfo{}, errors.New("torrent not found")
	}
	if !t.Complete() {
		return MoveInfo{}, errors.New("the torrent has not finished downloading")
	}
	if err := s.refuseBusy(ctx, hash); err != nil {
		return MoveInfo{}, err
	}
	rules, err := ListRules(ctx, s.DB)
	if err != nil {
		return MoveInfo{}, err
	}
	p := s.plan(ctx, client, settings, rules, t, nil, nil, true)
	return s.start(ctx, client, settings, p, true)
}

func (s *Service) refuseBusy(ctx context.Context, hash string) error {
	var status string
	err := s.DB.QueryRow(ctx, `SELECT status FROM download_move WHERE hash = $1`, hash).Scan(&status)
	if err == nil && status == StatusMoving {
		return errors.New("this torrent is already being moved")
	}
	return nil
}

// MoveTo moves torrents to a folder by hand, recording it like a rule move
// so the library it lands in is scanned.
func (s *Service) MoveTo(ctx context.Context, hashes []string, dest string, scan bool) error {
	client, settings, err := s.Client(ctx)
	if err != nil {
		return err
	}
	if pathguard.Load(ctx, s.DB).Locked(dest) {
		return errors.New("torrents can't be moved into the protected in-progress folder")
	}
	var errs []string
	for _, hash := range hashes {
		t, ok := s.torrent(hash)
		if !ok {
			errs = append(errs, hash+": not found")
			continue
		}
		if err := s.refuseBusy(ctx, hash); err != nil {
			errs = append(errs, t.Name+": "+err.Error())
			continue
		}
		p := Plan{Hash: hash, Name: t.Name, RuleName: "Moved by hand", From: settings.Paths().Local(t.SavePath), Dest: dest}
		if !scan {
			p.rule = &Rule{RemoveAfter: RemoveNever}
		}
		if _, err := s.start(ctx, client, settings, p, true); err != nil {
			errs = append(errs, t.Name+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// HistoryEntry is one finished torrent's record.
type HistoryEntry struct {
	Hash      string  `json:"hash"`
	Name      string  `json:"name"`
	RuleID    *int64  `json:"ruleId,omitempty"`
	RuleName  string  `json:"ruleName"`
	Status    string  `json:"status"`
	From      string  `json:"from"`
	Dest      string  `json:"dest"`
	Message   string  `json:"message"`
	Library   *string `json:"library,omitempty"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	MovedAt   *string `json:"movedAt,omitempty"`
}

// History lists recent records, newest first.
func History(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.Query(ctx, `
		SELECT m.hash, m.name, m.rule_id, m.rule_name, m.status, m.from_path, m.dest_path, m.message, l.name,
		       to_char(m.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       to_char(m.updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       to_char(m.moved_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM download_move m LEFT JOIN library l ON l.id = m.library_id
		ORDER BY m.updated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryEntry, error) {
		var h HistoryEntry
		err := r.Scan(&h.Hash, &h.Name, &h.RuleID, &h.RuleName, &h.Status, &h.From, &h.Dest, &h.Message, &h.Library, &h.CreatedAt, &h.UpdatedAt, &h.MovedAt)
		return h, err
	})
}

// Forget deletes a torrent's record, so the rules look at it again.
func (s *Service) Forget(ctx context.Context, hash string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM download_move WHERE hash = $1 AND status <> 'MOVING'`, hash)
	s.Kick()
	return err
}

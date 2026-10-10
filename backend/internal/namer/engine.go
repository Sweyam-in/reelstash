// Package namer orchestrates ReelStash's matching engine: parse a
// filename, search the enabled metadata providers, score
// candidates, and — for a confident match or a human-selected candidate —
// rename the file, write its NFO, download a poster, and reconcile all of
// it into ReelStash's Postgres model via internal/scanner. Everything runs
// in-process; there is no external service dependency.
package namer

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/rename"
	"reelstash/backend/internal/scanner"
	"reelstash/backend/internal/titlematch"
)

type Engine struct {
	DB *pgxpool.Pool
	// Providers is the metadata-provider registry used for title matching.
	Providers      *provider.Registry
	Scanner        *scanner.Scanner
	RenameTemplate string
	PosterDir      string
}

func New(db *pgxpool.Pool, providers *provider.Registry, sc *scanner.Scanner, renameTemplate, posterDir string) *Engine {
	return &Engine{DB: db, Providers: providers, Scanner: sc, RenameTemplate: renameTemplate, PosterDir: posterDir}
}

// Enabled reports whether at least one provider can match titles.
func (e *Engine) Enabled() bool {
	return e != nil && e.Providers.AnyEnabled(provider.KindMedia)
}

type CandidateView struct {
	Provider   string   `json:"provider"`
	ExternalID string   `json:"externalId"`
	Kind       string   `json:"kind,omitempty"`
	UUID       string   `json:"uuid"` // same as ExternalID; kept for older UI code
	Title      string   `json:"title"`
	Site       string   `json:"site"`
	Date       string   `json:"date"`
	Performers []string `json:"performers"`
	Poster     string   `json:"poster,omitempty"`
	Score      float64  `json:"score"`
	SiteMatch  bool     `json:"siteMatch"`
	DateMatch  bool     `json:"dateMatch"`
	// Rejected marks a scene the user already flagged as a wrong match for this title.
	Rejected bool `json:"rejected,omitempty"`
}

type AppliedMatch struct {
	MediaFileID int64   `json:"mediaFileId"`
	Provider    string  `json:"provider"`
	NewPath     string  `json:"newPath"`
	Title       string  `json:"title"`
	Site        string  `json:"site"`
	Score       float64 `json:"score"`
}

type MatchResult struct {
	Status     string          `json:"status"` // "matched" | "manual_required" | "no_match"
	Applied    *AppliedMatch   `json:"applied,omitempty"`
	Candidates []CandidateView `json:"candidates,omitempty"`
}

func toCandidateView(c provider.MediaCandidate) CandidateView {
	return CandidateView{
		Provider:   c.Result.Ref.Provider,
		ExternalID: c.Result.Ref.ExternalID,
		Kind:       c.Result.Kind,
		UUID:       c.Result.Ref.ExternalID,
		Title:      c.Result.Title,
		Site:       c.Result.Studio,
		Date:       c.Result.Date,
		Performers: c.Result.Performers,
		Poster:     c.Result.PosterURL,
		Score:      c.Score,
		SiteMatch:  c.StudioHit,
		DateMatch:  c.DateHit,
	}
}

type fileLocation struct {
	absPath     string
	mountPath   string
	mediaItemID int64
}

func (e *Engine) loadFileLocation(ctx context.Context, mediaFileID int64) (fileLocation, error) {
	var loc fileLocation
	var relativePath string
	err := e.DB.QueryRow(ctx, `
		SELECT sl.mount_path, mf.relative_path, mf.media_item_id
		FROM media_file mf JOIN storage_location sl ON sl.id = mf.storage_location_id
		WHERE mf.id = $1
	`, mediaFileID).Scan(&loc.mountPath, &relativePath, &loc.mediaItemID)
	if err != nil {
		return loc, err
	}
	loc.absPath = filepath.Join(loc.mountPath, filepath.FromSlash(relativePath))
	return loc, nil
}

// autoApply reports whether confident matches of this kind ("titles",
// "performers", "studios") are applied on their own; when off, providers only
// suggest and the admin confirms each match in the review queue.
func (e *Engine) autoApply(ctx context.Context, kind string) bool {
	return provider.AutoMatchEnabled(ctx, e.DB, kind)
}

// matchMode is "priority" (default): providers are tried in priority order and
// the first confident match wins. "best": every provider is searched and the
// highest-scoring confident match wins.
func (e *Engine) matchMode(ctx context.Context) string {
	var v string
	if err := e.DB.QueryRow(ctx, `SELECT value FROM app_setting WHERE key = 'match_mode'`).Scan(&v); err == nil && v == "best" {
		return "best"
	}
	return "priority"
}

// searchProvider searches one provider and drops candidates the user already
// rejected for this title.
func (e *Engine) searchProvider(ctx context.Context, p provider.Provider, q provider.MediaQuery, rejected map[string]bool) ([]provider.MediaCandidate, error) {
	cands, err := p.SearchMedia(ctx, q)
	if err != nil {
		return nil, err
	}
	kept := cands[:0]
	for _, c := range cands {
		if !rejected[rejectKey(p.ID(), c.Result.Ref.ExternalID)] {
			kept = append(kept, c)
		}
	}
	return kept, nil
}

// MatchFile parses mediaFileID's filename, searches the enabled providers, and
// either applies a confident match immediately or returns candidates for
// manual review.
func (e *Engine) MatchFile(ctx context.Context, mediaFileID int64) (MatchResult, error) {
	loc, err := e.loadFileLocation(ctx, mediaFileID)
	if err != nil {
		return MatchResult{}, fmt.Errorf("load media file: %w", err)
	}
	providers := e.mediaProviders(ctx, loc.mediaItemID, provider.KindMedia)
	if len(providers) == 0 {
		return MatchResult{}, fmt.Errorf("no metadata provider is configured for this library")
	}
	q := provider.MediaQuery{FileName: filepath.Base(loc.absPath)}
	rejected := e.RejectedMatches(ctx, loc.mediaItemID)

	var all []provider.MediaCandidate
	var firstErr error
	failed := 0
	var best *provider.MediaCandidate
	auto := e.autoApply(ctx, "titles")
	consider := func(cands []provider.MediaCandidate) {
		for i := range cands {
			c := cands[i]
			// With automatic title matching off, every title goes to the review
			// queue whatever the provider's confidence.
			if auto && c.AutoMatch && (best == nil || c.Score > best.Score) {
				best = &c
			}
		}
	}

	if e.matchMode(ctx) == "best" {
		type outcome struct {
			cands []provider.MediaCandidate
			err   error
		}
		results := make([]outcome, len(providers))
		var wg sync.WaitGroup
		for i, p := range providers {
			wg.Add(1)
			go func(i int, p provider.Provider) {
				defer wg.Done()
				c, err := e.searchProvider(ctx, p, q, rejected)
				results[i] = outcome{c, err}
			}(i, p)
		}
		wg.Wait()
		for _, r := range results { // priority order breaks score ties (first wins)
			if r.err != nil {
				failed++
				if firstErr == nil {
					firstErr = r.err
				}
				continue
			}
			all = append(all, r.cands...)
			consider(r.cands)
		}
	} else {
		for _, p := range providers {
			cands, err := e.searchProvider(ctx, p, q, rejected)
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			all = append(all, cands...)
			consider(cands)
			if best != nil {
				break // a higher-priority provider already found a confident match
			}
		}
	}

	if best != nil {
		applied, err := e.apply(ctx, mediaFileID, loc, best.Result, false)
		if err != nil {
			return MatchResult{}, err
		}
		applied.Score = best.Score
		return MatchResult{Status: "matched", Applied: &applied}, nil
	}
	if len(all) == 0 {
		if failed == len(providers) && firstErr != nil {
			return MatchResult{}, firstErr
		}
		e.clearSuggestions(ctx, loc.mediaItemID)
		return MatchResult{Status: "no_match"}, nil
	}
	views := topViews(all, 10)
	e.saveSuggestions(ctx, loc.mediaItemID, views)
	return MatchResult{Status: "manual_required", Candidates: views}, nil
}

// topViews orders candidates best-first (stable, so provider priority breaks
// ties) and converts the first limit to views.
func topViews(cands []provider.MediaCandidate, limit int) []CandidateView {
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	views := make([]CandidateView, 0, len(cands))
	for _, c := range cands {
		views = append(views, toCandidateView(c))
	}
	return views
}

func (e *Engine) fetchMedia(ctx context.Context, providerID, externalID string) (provider.MediaResult, error) {
	p, ok := e.Providers.Get(providerID)
	if !ok || !p.Enabled() {
		return provider.MediaResult{}, fmt.Errorf("provider %q is not configured", providerID)
	}
	res, err := p.GetMedia(ctx, externalID)
	if err != nil {
		return provider.MediaResult{}, fmt.Errorf("fetch from %s: %w", p.Name(), err)
	}
	return res, nil
}

// SelectCandidate finalizes a human-chosen candidate from a prior
// MatchFile's manual_required response.
func (e *Engine) SelectCandidate(ctx context.Context, mediaFileID int64, providerID, externalID string) (MatchResult, error) {
	loc, err := e.loadFileLocation(ctx, mediaFileID)
	if err != nil {
		return MatchResult{}, fmt.Errorf("load media file: %w", err)
	}
	res, err := e.fetchMedia(ctx, providerID, externalID)
	if err != nil {
		return MatchResult{}, err
	}
	applied, err := e.apply(ctx, mediaFileID, loc, res, false)
	if err != nil {
		return MatchResult{}, err
	}
	return MatchResult{Status: "matched", Applied: &applied}, nil
}

// apply renames the file after the chosen record, writes its NFO, downloads the
// poster and reconciles everything into the library. With merge, the title's
// existing details win and the record only fills gaps (performers and
// categories are combined), so a second provider adds to a title instead of
// overwriting what the first one supplied.
func (e *Engine) apply(ctx context.Context, mediaFileID int64, loc fileLocation, scene provider.MediaResult, merge bool) (AppliedMatch, error) {
	scene = e.resolveCast(ctx, scene)
	if merge {
		cur, err := e.currentMedia(ctx, loc.mediaItemID)
		if err != nil {
			return AppliedMatch{}, err
		}
		scene = mergeMedia(cur, scene)
	}
	probe, probeErr := scanner.Probe(ctx, loc.absPath)
	var height *int32
	if probeErr == nil && probe.Height != nil {
		h := int32(*probe.Height)
		height = &h
	}

	var releaseDate *time.Time
	var year *int32
	if scene.Date != "" {
		if t, err := time.Parse("2006-01-02", scene.Date); err == nil {
			releaseDate = &t
			y := int32(t.Year())
			year = &y
		}
	}

	heightInt := (*int)(nil)
	if height != nil {
		h := int(*height)
		heightInt = &h
	}

	ext := filepath.Ext(loc.absPath)
	var seriesName string
	e.DB.QueryRow(ctx, `SELECT COALESCE(se.name, '') FROM media_item mi LEFT JOIN series se ON se.id = mi.series_id WHERE mi.id = $1`, loc.mediaItemID).Scan(&seriesName)
	newFileName := rename.BuildFileName(rename.FileTemplate(ctx, e.DB, mediaFileID, e.RenameTemplate), scene.Title, scene.Studio, seriesName, releaseDate, year, heightInt, scene.Performers, ext)
	dir := filepath.Dir(loc.absPath)
	newAbsPath := filepath.Join(dir, newFileName)
	// A file a torrent is seeding keeps its name; renaming it would break the torrent.
	if pathguard.Load(ctx, e.DB).Inside(loc.absPath) {
		newAbsPath = loc.absPath
	}

	if newAbsPath != loc.absPath {
		// Remember the name the file came with; only the first rename records it.
		if _, err := e.DB.Exec(ctx, `UPDATE media_file SET original_file_name = COALESCE(original_file_name, file_name) WHERE id = $1`, mediaFileID); err != nil {
			return AppliedMatch{}, fmt.Errorf("record original file name: %w", err)
		}
		if _, err := os.Stat(newAbsPath); err == nil {
			return AppliedMatch{}, fmt.Errorf("a file already exists at the target path: %s", newFileName)
		} else if !os.IsNotExist(err) {
			return AppliedMatch{}, err
		}
		if err := os.Rename(loc.absPath, newAbsPath); err != nil {
			return AppliedMatch{}, fmt.Errorf("rename on disk: %w", err)
		}
	}

	nfoAbsPath := strings.TrimSuffix(newAbsPath, filepath.Ext(newAbsPath)) + ".nfo"
	yearInt := 0
	if year != nil {
		yearInt = int(*year)
	}
	if err := scanner.WriteNFO(nfoAbsPath, scene.Title, scene.Description, scene.Studio, scene.Date, yearInt, scene.Tags, scene.Performers, e.nfoIDs(ctx, loc.mediaItemID, scene.Ref)); err != nil {
		return AppliedMatch{}, fmt.Errorf("write nfo: %w", err)
	}
	// The NFO under the old name is superseded by the one just written (which
	// carries the same ReelStash uid); leaving it would orphan it.
	if oldNFO := strings.TrimSuffix(loc.absPath, filepath.Ext(loc.absPath)) + ".nfo"; oldNFO != nfoAbsPath {
		if err := os.Remove(oldNFO); err != nil && !os.IsNotExist(err) {
			log.Printf("namer: remove superseded nfo %s: %v", oldNFO, err)
		}
	}

	if err := e.Scanner.ReconcileRenamedFile(ctx, mediaFileID, newAbsPath, nfoAbsPath); err != nil {
		return AppliedMatch{}, fmt.Errorf("reconcile after match: %w", err)
	}

	if scene.PosterURL != "" { // empty when a merge keeps the existing poster
		if err := e.downloadPoster(ctx, loc.mediaItemID, scene.PosterURL); err != nil {
			// Non-fatal: the match itself (rename + NFO + DB) already succeeded.
			fmt.Printf("namer: poster download failed for media_item %d: %v\n", loc.mediaItemID, err)
		}
	}
	if scene.BackgroundURL != "" {
		if err := e.downloadBackdrop(ctx, loc.mediaItemID, scene.BackgroundURL, !merge); err != nil {
			// Artwork is optional; a failed download must not undo a successful match.
			fmt.Printf("namer: backdrop download failed for media_item %d: %v\n", loc.mediaItemID, err)
		}
	}
	e.saveScenePerformerPhotos(ctx, scene)
	e.linkCast(ctx, loc.mediaItemID, scene)
	e.applyEntertainment(ctx, loc.mediaItemID, scene)
	e.recordMediaLink(ctx, loc.mediaItemID, scene.Ref)

	return AppliedMatch{
		MediaFileID: mediaFileID,
		Provider:    scene.Ref.Provider,
		NewPath:     newAbsPath,
		Title:       scene.Title,
		Site:        scene.Studio,
		Score:       0,
	}, nil
}

func (e *Engine) downloadPoster(ctx context.Context, mediaItemID int64, posterURL string) error {
	var custom bool
	if err := e.DB.QueryRow(ctx, `
		SELECT COALESCE(ar.custom, false) FROM media_item mi LEFT JOIN artwork ar ON ar.id = mi.poster_image_id WHERE mi.id = $1
	`, mediaItemID).Scan(&custom); err == nil && custom {
		return nil // never replace a poster the user set by hand
	}
	artworkID, err := e.saveArtwork(ctx, posterURL, "POSTER")
	if err != nil {
		return err
	}
	_, err = e.DB.Exec(ctx, `UPDATE media_item SET poster_image_id = $2, updated_at = now() WHERE id = $1`, mediaItemID, artworkID)
	return err
}

// downloadBackdrop stores a provider backdrop. A fresh match replaces the
// current one unless the user set it by hand; a merge keeps what is there.
func (e *Engine) downloadBackdrop(ctx context.Context, mediaItemID int64, backdropURL string, replace bool) error {
	var hasBackdrop, custom bool
	if err := e.DB.QueryRow(ctx, `
		SELECT mi.backdrop_image_id IS NOT NULL, COALESCE(ar.custom, false)
		FROM media_item mi LEFT JOIN artwork ar ON ar.id = mi.backdrop_image_id WHERE mi.id = $1
	`, mediaItemID).Scan(&hasBackdrop, &custom); err != nil {
		return err
	}
	if hasBackdrop && (!replace || custom) {
		return nil
	}
	artworkID, err := e.saveArtwork(ctx, backdropURL, "BACKDROP")
	if err != nil {
		return err
	}
	_, err = e.DB.Exec(ctx, `UPDATE media_item SET backdrop_image_id = $2, updated_at = now() WHERE id = $1`, mediaItemID, artworkID)
	return err
}

// Search looks up candidates for a media file across the given providers (all
// enabled ones when providerIDs is empty). An empty query searches by the
// parsed filename; otherwise it is a free-text search. Providers that fail are
// skipped and reported in the returned error map only if none succeeded. It
// returns the query that was actually used.
func (e *Engine) Search(ctx context.Context, mediaFileID int64, query string, providerIDs []string, searchType string) (string, []CandidateView, error) {
	loc, err := e.loadFileLocation(ctx, mediaFileID)
	if err != nil {
		return "", nil, fmt.Errorf("load media file: %w", err)
	}
	var providers []provider.Provider
	if len(providerIDs) == 0 {
		providers = e.mediaProviders(ctx, loc.mediaItemID, provider.KindMedia)
	} else {
		for _, id := range providerIDs {
			if p, ok := e.Providers.Get(id); ok && p.Enabled() && p.Capabilities().Media {
				providers = append(providers, p)
			}
		}
	}
	if len(providers) == 0 {
		return "", nil, fmt.Errorf("no matching provider is configured")
	}

	text := strings.TrimSpace(query)
	q := provider.MediaQuery{FileName: filepath.Base(loc.absPath), Text: text, SearchType: searchType}
	shown := text
	if shown == "" {
		// Only used to prefill the search box; the providers parse the file name themselves.
		shown = titlematch.SearchText(q.FileName)
	}

	rejected := e.RejectedMatches(ctx, loc.mediaItemID)
	type outcome struct {
		cands []provider.MediaCandidate
		err   error
	}
	results := make([]outcome, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func(i int, p provider.Provider) {
			defer wg.Done()
			cands, err := p.SearchMedia(ctx, q)
			results[i] = outcome{cands, err}
		}(i, p)
	}
	wg.Wait()

	var all []provider.MediaCandidate
	var firstErr error
	failed := 0
	for _, r := range results {
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		all = append(all, r.cands...)
	}
	if failed == len(providers) && firstErr != nil {
		return "", nil, firstErr
	}
	views := topViews(all, len(all))
	for i := range views {
		views[i].Rejected = rejected[rejectKey(views[i].Provider, views[i].ExternalID)]
	}
	return shown, views, nil
}

// ApplyReplacing applies a chosen provider record to a media file like
// SelectCandidate, but as an explicit user re-match: the title's manual-edit
// protection is lifted and its performers and categories are replaced by the
// record's instead of merged. They are restored if the match fails. With merge
// nothing is replaced: the record only fills what the title lacks (see apply).
func (e *Engine) ApplyReplacing(ctx context.Context, mediaFileID int64, providerID, externalID string, merge bool) (AppliedMatch, error) {
	loc, err := e.loadFileLocation(ctx, mediaFileID)
	if err != nil {
		return AppliedMatch{}, fmt.Errorf("load media file: %w", err)
	}
	scene, err := e.fetchMedia(ctx, providerID, externalID)
	if err != nil {
		return AppliedMatch{}, err
	}

	var source string
	if err := e.DB.QueryRow(ctx, `SELECT metadata_source FROM media_item WHERE id = $1`, loc.mediaItemID).Scan(&source); err != nil {
		return AppliedMatch{}, err
	}
	restore := func() {
		bg := context.WithoutCancel(ctx)
		e.DB.Exec(bg, `UPDATE media_item SET metadata_source = $2 WHERE id = $1`, loc.mediaItemID, source)
	}
	links, err := e.snapshotLinks(ctx, loc.mediaItemID)
	if err != nil {
		return AppliedMatch{}, err
	}
	clear := `
		WITH p AS (DELETE FROM media_item_performer WHERE media_item_id = $1),
		     c AS (DELETE FROM media_item_category WHERE media_item_id = $1)
		UPDATE media_item SET metadata_source = 'NFO' WHERE id = $1`
	if merge { // keep performers and categories; only lift the manual-edit protection
		clear = `UPDATE media_item SET metadata_source = 'NFO' WHERE id = $1 AND metadata_source = 'MANUAL'`
	}
	if _, err := e.DB.Exec(ctx, clear, loc.mediaItemID); err != nil {
		return AppliedMatch{}, err
	}

	applied, err := e.apply(ctx, mediaFileID, loc, scene, merge)
	if err != nil {
		restore()
		e.restoreLinks(context.WithoutCancel(ctx), loc.mediaItemID, links)
		return AppliedMatch{}, err
	}
	return applied, nil
}

type itemLinks struct {
	performers []int64
	categories []int64
}

func (e *Engine) snapshotLinks(ctx context.Context, mediaItemID int64) (itemLinks, error) {
	var l itemLinks
	if err := e.DB.QueryRow(ctx, `
		SELECT COALESCE(array_agg(performer_id ORDER BY position), '{}') FROM media_item_performer WHERE media_item_id = $1
	`, mediaItemID).Scan(&l.performers); err != nil {
		return l, err
	}
	err := e.DB.QueryRow(ctx, `
		SELECT COALESCE(array_agg(category_id), '{}') FROM media_item_category WHERE media_item_id = $1
	`, mediaItemID).Scan(&l.categories)
	return l, err
}

func (e *Engine) restoreLinks(ctx context.Context, mediaItemID int64, l itemLinks) {
	for i, id := range l.performers {
		e.DB.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id, position) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, mediaItemID, id, i)
	}
	for _, id := range l.categories {
		e.DB.Exec(ctx, `INSERT INTO media_item_category (media_item_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, mediaItemID, id)
	}
}

// saveArtwork stores provider artwork in the shared artwork directory.
func (e *Engine) saveArtwork(ctx context.Context, imageURL, kind string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("artwork download returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20+1))
	if err != nil {
		return 0, err
	}
	if len(body) > 20<<20 {
		return 0, fmt.Errorf("artwork exceeds 20 MB")
	}
	mime := http.DetectContentType(body)
	extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}[mime]
	if extension == "" {
		return 0, fmt.Errorf("unsupported artwork type %q", mime)
	}

	if err := os.MkdirAll(e.PosterDir, 0o755); err != nil {
		return 0, err
	}
	storageKey := uuid.NewString() + extension
	outPath := filepath.Join(e.PosterDir, storageKey)
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return 0, err
	}

	var artworkID int64
	if err := e.DB.QueryRow(ctx, `
		INSERT INTO artwork (kind, storage_key, mime_type) VALUES ($1, $2, $3)
		RETURNING id
	`, kind, storageKey, mime).Scan(&artworkID); err != nil {
		os.Remove(outPath)
		return 0, err
	}
	return artworkID, nil
}

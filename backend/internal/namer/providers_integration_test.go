package namer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
	"reelstash/backend/internal/testdb"
)

// stubProvider is a canned provider for engine tests.
type stubProvider struct {
	id         string
	media      map[string]provider.MediaResult
	candidates []provider.MediaCandidate
	performers map[string]provider.PerformerResult // by name
	studios    map[string]provider.StudioResult    // by name
}

func (s *stubProvider) ID() string   { return s.id }
func (s *stubProvider) Name() string { return strings.ToUpper(s.id) }
func (s *stubProvider) Capabilities() provider.Caps {
	return provider.Caps{Media: true, Performer: true, Studio: true}
}
func (s *stubProvider) Enabled() bool                     { return true }
func (s *stubProvider) ConfigFields() []provider.Field    { return nil }
func (s *stubProvider) Configure(map[string]string) error { return nil }
func (s *stubProvider) Test(context.Context) error        { return nil }
func (s *stubProvider) ImageHosts() []string              { return nil }
func (s *stubProvider) SearchMedia(context.Context, provider.MediaQuery) ([]provider.MediaCandidate, error) {
	return s.candidates, nil
}
func (s *stubProvider) GetMedia(_ context.Context, id string) (provider.MediaResult, error) {
	return s.media[id], nil
}
func (s *stubProvider) SearchPerformers(_ context.Context, name string) ([]provider.PerformerResult, error) {
	if p, ok := s.performers[name]; ok {
		return []provider.PerformerResult{p}, nil
	}
	return nil, nil
}
func (s *stubProvider) GetPerformer(_ context.Context, id string) (provider.PerformerResult, error) {
	for _, p := range s.performers {
		if p.Ref.ExternalID == id {
			return p, nil
		}
	}
	return provider.PerformerResult{}, os.ErrNotExist
}
func (s *stubProvider) SearchStudios(_ context.Context, name string) ([]provider.StudioResult, error) {
	if p, ok := s.studios[name]; ok {
		return []provider.StudioResult{p}, nil
	}
	return nil, nil
}
func (s *stubProvider) GetStudio(_ context.Context, id string) (provider.StudioResult, error) {
	for _, p := range s.studios {
		if p.Ref.ExternalID == id {
			return p, nil
		}
	}
	return provider.StudioResult{}, os.ErrNotExist
}

func integrationEngine(t *testing.T, providers ...provider.Provider) (*Engine, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := testdb.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reg := provider.NewRegistry()
	for _, p := range providers {
		reg.Register(p)
	}
	jobRunner := jobs.NewRunner(pool, 1)
	sc := scanner.New(pool, jobRunner, jobs.NewRunner(pool, 1), t.TempDir(), 1)
	return New(pool, reg, sc, "{studio} - {date} - {title}", t.TempDir()), pool
}

// newTitle inserts a title with one file on disk and returns its ids and path.
func newTitle(t *testing.T, pool *pgxpool.Pool, fileName string) (itemID, fileID int64, mount string) {
	t.Helper()
	ctx := context.Background()
	mount = t.TempDir()
	if err := os.WriteFile(filepath.Join(mount, fileName), []byte("fake video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	var locID int64
	if err := pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"t-"+uuid.NewString(), mount).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO media_item (title, sort_title, metadata_source) VALUES ('placeholder', 'placeholder', 'FILENAME') RETURNING id`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Ext(fileName)
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
		VALUES ($1, $2, $3, $3, $4, 16) RETURNING id`, itemID, locID, fileName, strings.TrimPrefix(ext, ".")).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	return itemID, fileID, mount
}

// resetAutoMatch drops saved automatic-matching settings now and after the
// test: integrationEngine shares one database, so settings would leak.
func resetAutoMatch(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	clear := func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM app_setting WHERE key LIKE 'auto\_match.%'`); err != nil {
			t.Fatal(err)
		}
	}
	clear()
	t.Cleanup(clear)
}

func linkCount(t *testing.T, pool *pgxpool.Pool, kind string, id int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM match_link WHERE kind = $1 AND entity_id = $2`, kind, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// By default a performer sync links an exact, unambiguous name match.
func TestPerformerSyncAutoLinksByDefault(t *testing.T) {
	name := "Jane " + uuid.NewString()[:8]
	a := &stubProvider{id: "x-alpha", performers: map[string]provider.PerformerResult{name: {
		Ref: provider.Ref{Provider: "x-alpha", ExternalID: "pd-" + name}, Name: name,
	}}}
	e, pool := integrationEngine(t, a)
	resetAutoMatch(t, pool)
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SyncPerformers(ctx, 0, false); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "performer", id); n != 1 {
		t.Fatalf("default sync should link the exact match, got %d links", n)
	}
}

// With automatic title matching on, a confident candidate is applied without review.
func TestMatchFileAppliesWhenTitleAutoMatchOn(t *testing.T) {
	scene := provider.MediaResult{
		Ref: provider.Ref{Provider: "x-alpha", ExternalID: "scenes/auto-" + uuid.NewString()[:8]}, Title: "Auto Applied Scene",
		Date: "2022-01-03", Studio: "Evil Angel", Kind: "scene",
	}
	stub := &stubProvider{id: "x-alpha", media: map[string]provider.MediaResult{scene.Ref.ExternalID: scene},
		candidates: []provider.MediaCandidate{{Result: scene, Score: 100, AutoMatch: true, StudioHit: true, DateHit: true}}}
	e, pool := integrationEngine(t, stub)
	resetAutoMatch(t, pool)
	ctx := context.Background()
	if err := provider.SaveAutoMatch(ctx, pool, "titles", true); err != nil {
		t.Fatal(err)
	}
	itemID, fileID, _ := newTitle(t, pool, "EvilAngel.22.01.03.Auto.Applied.Scene.mp4")

	result, err := e.MatchFile(ctx, fileID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "matched" || result.Applied == nil {
		t.Fatalf("expected the confident match to be applied, got %+v", result)
	}
	var title string
	if err := pool.QueryRow(ctx, `SELECT title FROM media_item WHERE id = $1`, itemID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Auto Applied Scene" {
		t.Errorf("title = %q", title)
	}
}

func TestPerformerLinksAcrossProviders(t *testing.T) {
	name := "Jane " + uuid.NewString()[:8]
	a := &stubProvider{id: "x-alpha", performers: map[string]provider.PerformerResult{name: {
		Ref: provider.Ref{Provider: "x-alpha", ExternalID: "pd-" + name}, Name: name, Bio: "Provider bio", Aliases: []string{"J. A"},
	}}}
	b := &stubProvider{id: "tmdb", performers: map[string]provider.PerformerResult{name: {
		Ref: provider.Ref{Provider: "tmdb", ExternalID: "77" + name[5:]}, Name: name, Bio: "TMDB bio", Birthplace: "Paris", Aliases: []string{"J. B"},
	}}}
	e, pool := integrationEngine(t, a, b)
	ctx := context.Background()

	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}

	// With automatic matching off, a sync never links, even on an exact match.
	resetAutoMatch(t, pool)
	if err := provider.SaveAutoMatch(ctx, pool, "performers", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SyncPerformers(ctx, 0, false); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "performer", id); n != 0 {
		t.Fatalf("sync must not auto-link, got %d links", n)
	}

	// Manual matches add providers one by one; metadata is merged, not replaced.
	if _, err := e.MatchPerformer(ctx, id, "x-alpha", a.performers[name].Ref.ExternalID); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "performer", id); n != 1 {
		t.Fatalf("expected 1 link, got %d", n)
	}
	if _, err := e.MatchPerformer(ctx, id, "tmdb", b.performers[name].Ref.ExternalID); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "performer", id); n != 2 {
		t.Fatalf("expected 2 links, got %d", n)
	}
	var raw string
	if err := pool.QueryRow(ctx, `SELECT remote_metadata::text FROM performer WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"provider": "tmdb"`, "TMDB bio", "Paris", "J. A", "J. B"} {
		if !strings.Contains(raw, want) {
			t.Errorf("merged metadata lacks %q: %s", want, raw)
		}
	}

	// A provider record can only belong to one performer.
	var other int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, name+" II").Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := e.MatchPerformer(ctx, other, "tmdb", b.performers[name].Ref.ExternalID); err == nil ||
		!strings.Contains(err.Error(), "already matched") {
		t.Errorf("expected an already-matched error, got %v", err)
	}
}

func TestMediaMergeNFOIDsAndRecovery(t *testing.T) {
	title := "Alpha " + uuid.NewString()[:8]
	a := &stubProvider{id: "x-alpha", media: map[string]provider.MediaResult{"scenes/1": {
		Ref: provider.Ref{Provider: "x-alpha", ExternalID: "scenes/1"}, Title: title, Studio: "Evil Angel", Date: "2022-01-03",
		Performers: []string{"Ann"}, Tags: []string{"Drama"},
	}}}
	b := &stubProvider{id: "tmdb", media: map[string]provider.MediaResult{"movie/9": {
		Ref: provider.Ref{Provider: "tmdb", ExternalID: "movie/9"}, Title: "Other", Description: "From TMDB",
		Studio: "Warner", Date: "2020-05-05", Performers: []string{"Bob"}, Tags: []string{"Action"},
	}}}
	e, pool := integrationEngine(t, a, b)
	ctx := context.Background()
	itemID, fileID, mount := newTitle(t, pool, "original.mp4")

	if _, err := e.SelectCandidate(ctx, fileID, "x-alpha", "scenes/1"); err != nil {
		t.Fatal(err)
	}
	// Merge in the second provider: the title keeps its details and gains the gaps.
	if _, err := e.ApplyReplacing(ctx, fileID, "tmdb", "movie/9", true); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "media", itemID); n != 2 {
		t.Fatalf("expected 2 links, got %d", n)
	}
	var gotTitle, desc, studio, date string
	if err := pool.QueryRow(ctx, `
		SELECT mi.title, COALESCE(mi.description, ''), COALESCE(s.name, ''), COALESCE(mi.release_date::text, '')
		FROM media_item mi LEFT JOIN studio s ON s.id = mi.studio_id WHERE mi.id = $1`, itemID).Scan(&gotTitle, &desc, &studio, &date); err != nil {
		t.Fatal(err)
	}
	if gotTitle != title || studio != "Evil Angel" || date != "2022-01-03" || desc != "From TMDB" {
		t.Errorf("merge result = %q / %q / %q / %q", gotTitle, studio, date, desc)
	}
	var performers int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_item_performer WHERE media_item_id = $1`, itemID).Scan(&performers)
	if performers != 2 {
		t.Errorf("performers should be combined, got %d", performers)
	}

	// The NFO carries both ids, so a rescan restores the links.
	matches, _ := filepath.Glob(filepath.Join(mount, "*.nfo"))
	if len(matches) != 1 {
		t.Fatalf("expected one NFO, got %v", matches)
	}
	nfo, _ := os.ReadFile(matches[0])
	for _, want := range []string{`type="x-alpha"`, "scenes/1", `type="tmdb"`, "movie/9"} {
		if !strings.Contains(string(nfo), want) {
			t.Errorf("NFO lacks %q:\n%s", want, nfo)
		}
	}
	pool.Exec(ctx, `DELETE FROM match_link WHERE kind = 'media' AND entity_id = $1`, itemID)
	var locID int64
	var rel string
	pool.QueryRow(ctx, `SELECT storage_location_id, relative_path FROM media_file WHERE id = $1`, fileID).Scan(&locID, &rel)
	if err := scanner.ImportNFO(ctx, pool, fileID, itemID, locID, rel, matches[0]); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "media", itemID); n != 2 {
		t.Errorf("NFO import should restore both links, got %d", n)
	}

	// Recovery: a matched title with no links is re-identified, link-only.
	pool.Exec(ctx, `DELETE FROM match_link WHERE kind = 'media' AND entity_id = $1`, itemID)
	a.candidates = []provider.MediaCandidate{{Result: a.media["scenes/1"]}}
	if _, err := e.RecoverLinks(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "media", itemID); n != 1 {
		t.Errorf("recovery should link the exact record once, got %d", n)
	}

	// Link-only adds another provider without touching the title.
	if err := e.LinkOnly(ctx, itemID, "tmdb", "movie/9"); err != nil {
		t.Fatal(err)
	}
	if n := linkCount(t, pool, "media", itemID); n != 2 {
		t.Errorf("link-only should add a link, got %d", n)
	}
}

// Applying records from two providers that spell a person differently lands on
// one performer: the first apply links them by id, the second finds them by it.
func TestCastResolvesByLinkAcrossProviders(t *testing.T) {
	suffix := uuid.NewString()[:8]
	local := "Riley Mitchel " + suffix
	a := &stubProvider{id: "x-alpha", media: map[string]provider.MediaResult{"scenes/c1": {
		Ref: provider.Ref{Provider: "x-alpha", ExternalID: "scenes/c1"}, Title: "Cast A " + suffix, Date: "2022-01-03",
		Performers:    []string{"RILEY-MITCHEL " + suffix},
		PerformerRefs: map[string]provider.Ref{"RILEY-MITCHEL " + suffix: {Provider: "x-alpha", ExternalID: "pd-riley-" + suffix}},
	}}}
	b := &stubProvider{id: "tmdb", media: map[string]provider.MediaResult{"movie/c2": {
		Ref: provider.Ref{Provider: "tmdb", ExternalID: "movie/c2"}, Title: "Cast B " + suffix, Date: "2023-02-04",
		// A spelling no name comparison would accept, reached through the TMDB link.
		Performers:    []string{"Riley Mitchell " + suffix},
		PerformerRefs: map[string]provider.Ref{"Riley Mitchell " + suffix: {Provider: "tmdb", ExternalID: "tm-riley-" + suffix}},
	}}}
	e, pool := integrationEngine(t, a, b)
	ctx := context.Background()

	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, local).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// The TMDB person is already linked (e.g. from a performer match).
	if _, err := pool.Exec(ctx, `INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('performer', $1, 'tmdb', $2)`, id, "tm-riley-"+suffix); err != nil {
		t.Fatal(err)
	}

	_, fileA, _ := newTitle(t, pool, "a-"+suffix+".mp4")
	if _, err := e.SelectCandidate(ctx, fileA, "x-alpha", "scenes/c1"); err != nil {
		t.Fatal(err)
	}
	_, fileB, _ := newTitle(t, pool, "b-"+suffix+".mp4")
	if _, err := e.SelectCandidate(ctx, fileB, "tmdb", "movie/c2"); err != nil {
		t.Fatal(err)
	}

	var count int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM performer WHERE name ILIKE '%' || $1`, suffix).Scan(&count)
	if count != 1 {
		t.Errorf("expected one performer, got %d", count)
	}
	var ext string
	if err := pool.QueryRow(ctx, `SELECT external_id FROM match_link WHERE kind = 'performer' AND entity_id = $1 AND provider = 'x-alpha'`, id).Scan(&ext); err != nil || ext != "pd-riley-"+suffix {
		t.Errorf("x-alpha link = %q, %v", ext, err)
	}
	var titles int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_item_performer WHERE performer_id = $1`, id).Scan(&titles)
	if titles != 2 {
		t.Errorf("performer should be on both titles, got %d", titles)
	}
}

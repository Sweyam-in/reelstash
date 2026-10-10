package namer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/scanner"
	"reelstash/backend/internal/testdb"
)

// TestMatchFileAppliesConfidentMatch is a full integration test of the
// native namer engine: parse a real filename, "search" a fixture Provider
// server, auto-apply the confident match (rename + write NFO + reconcile),
// and confirm the result landed correctly in Postgres. Requires
// REELSTASH_TEST_DATABASE_URL; skipped otherwise.
func TestMatchFileAppliesConfidentMatch(t *testing.T) {
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := testdb.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mountDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mountDir, "EvilAngel.22.01.03.Carmela.Clutch.Fabulous.Anal.3-Way.XXX.2160p.mp4"),
		[]byte("fake video bytes for namer engine test"), 0o644); err != nil {
		t.Fatal(err)
	}

	var storageLocationID, mediaItemID, mediaFileID int64
	if err := pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"NamerEngineTestDisk-"+uuid.NewString(), mountDir).Scan(&storageLocationID); err != nil {
		t.Fatalf("insert storage_location: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO media_item (title, sort_title, metadata_source) VALUES ('placeholder', 'placeholder', 'FILENAME') RETURNING id`).
		Scan(&mediaItemID); err != nil {
		t.Fatalf("insert media_item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
		VALUES ($1, $2, 'EvilAngel.22.01.03.Carmela.Clutch.Fabulous.Anal.3-Way.XXX.2160p.mp4', 'EvilAngel.22.01.03.Carmela.Clutch.Fabulous.Anal.3-Way.XXX.2160p.mp4', 'mp4', 39)
		RETURNING id
	`, mediaItemID, storageLocationID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert media_file: %v", err)
	}

	jobRunner := jobs.NewRunner(pool, 1)
	thumbnailJobRunner := jobs.NewRunner(pool, 1)
	sc := scanner.New(pool, jobRunner, thumbnailJobRunner, t.TempDir(), 1)
	scene := provider.MediaResult{
		Ref: provider.Ref{Provider: "x-alpha", ExternalID: "scenes/abc123"}, Title: "Carmela Clutch Fabulous Anal 3-Way",
		Description: "A great scene", Date: "2022-01-03", SourceURL: "https://alpha.example/scenes/abc123", Studio: "Evil Angel",
		Performers: []string{"Carmela Clutch"}, Tags: []string{"Anal"}, Kind: "scene",
	}
	providers := provider.NewRegistry()
	providers.Register(&stubProvider{id: "x-alpha", media: map[string]provider.MediaResult{"scenes/abc123": scene},
		candidates: []provider.MediaCandidate{{Result: scene, Score: 100, AutoMatch: true, StudioHit: true, DateHit: true}}})
	engine := New(pool, providers, sc, "{studio} - {date} - {title} [{resolution}]", t.TempDir())

	// Automatic title matching is off by default: even a 100% candidate is only suggested.
	if _, err := pool.Exec(ctx, `DELETE FROM app_setting WHERE key = 'auto_match.titles'`); err != nil {
		t.Fatal(err)
	}
	suggested, err := engine.MatchFile(ctx, mediaFileID)
	if err != nil {
		t.Fatalf("MatchFile: %v", err)
	}
	if suggested.Status != "manual_required" || suggested.Applied != nil || len(suggested.Candidates) != 1 {
		t.Fatalf("expected one suggestion and nothing applied, got %+v", suggested)
	}
	if _, err := os.Stat(filepath.Join(mountDir, "EvilAngel.22.01.03.Carmela.Clutch.Fabulous.Anal.3-Way.XXX.2160p.mp4")); err != nil {
		t.Fatalf("file must not be renamed before the user confirms: %v", err)
	}

	// Confirming the suggestion renames, writes the NFO and reconciles.
	result, err := engine.SelectCandidate(ctx, mediaFileID, "x-alpha", "scenes/abc123")
	if err != nil {
		t.Fatalf("SelectCandidate: %v", err)
	}
	if result.Status != "matched" {
		t.Fatalf("expected status matched, got %s (candidates: %v)", result.Status, result.Candidates)
	}
	if result.Applied == nil {
		t.Fatal("expected an Applied result")
	}

	expectedName := "Evil Angel - 2022-01-03 - Carmela Clutch Fabulous Anal 3-Way.mp4"
	if filepath.Base(result.Applied.NewPath) != expectedName {
		t.Errorf("new path = %q, want basename %q", result.Applied.NewPath, expectedName)
	}
	if _, err := os.Stat(result.Applied.NewPath); err != nil {
		t.Errorf("expected renamed file to exist on disk: %v", err)
	}
	nfoPath := filepath.Join(mountDir, "Evil Angel - 2022-01-03 - Carmela Clutch Fabulous Anal 3-Way.nfo")
	if _, err := os.Stat(nfoPath); err != nil {
		t.Errorf("expected nfo file to exist on disk: %v", err)
	}

	var title, metadataSource string
	if err := pool.QueryRow(ctx, `SELECT title, metadata_source FROM media_item WHERE id = $1`, mediaItemID).
		Scan(&title, &metadataSource); err != nil {
		t.Fatalf("read back media_item: %v", err)
	}
	if title != "Carmela Clutch Fabulous Anal 3-Way" {
		t.Errorf("title = %q", title)
	}
	if metadataSource != "NFO" {
		t.Errorf("metadata_source = %q, want NFO", metadataSource)
	}
}

func TestMergeMediaKeepsExistingAndFillsGaps(t *testing.T) {
	cur := currentMediaState{
		Title: "My Title", Description: "", Studio: "Evil Angel", Date: "",
		HasPoster: true, Performers: []string{"Ann"}, Categories: []string{"Drama"},
	}
	in := provider.MediaResult{
		Ref: provider.Ref{Provider: "tmdb", ExternalID: "movie/1"}, Title: "Other Title",
		Description: "From TMDB", Studio: "Warner", Date: "2020-01-02", PosterURL: "https://image.tmdb.org/p.jpg",
		Performers: []string{"ann", "Bob"}, Tags: []string{"drama", "Action"},
	}
	got := mergeMedia(cur, in)
	if got.Title != "My Title" || got.Studio != "Evil Angel" {
		t.Errorf("existing title/studio must win: %+v", got)
	}
	if got.Description != "From TMDB" || got.Date != "2020-01-02" {
		t.Errorf("missing description/date must be filled: %+v", got)
	}
	if got.PosterURL != "" {
		t.Errorf("an existing poster must be kept")
	}
	if len(got.Performers) != 2 || got.Performers[0] != "Ann" || got.Performers[1] != "Bob" {
		t.Errorf("performers = %v", got.Performers)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "Drama" || got.Tags[1] != "Action" {
		t.Errorf("tags = %v", got.Tags)
	}
	if got.Ref.Provider != "tmdb" {
		t.Errorf("the applied record's ref must be kept")
	}

	// A title that is only its file name takes the record's title.
	cur.FromFile = true
	if got := mergeMedia(cur, in); got.Title != "Other Title" {
		t.Errorf("filename-only title should be replaced, got %q", got.Title)
	}
}

func TestSameRecord(t *testing.T) {
	item := recoverRow{title: "Carmela Clutch: Fabulous Anal 3-Way", date: "2022-01-03", studio: "Evil Angel"}
	rec := provider.MediaResult{Title: "carmela clutch fabulous anal 3 way", Date: "2022-01-03", Studio: "Other"}
	if !sameRecord(item, rec) {
		t.Error("same title and date should match")
	}
	rec.Date = "2022-01-04"
	if sameRecord(item, rec) {
		t.Error("a different date must not match")
	}
	item.date, rec.Date = "", ""
	rec.Studio = "EvilAngel"
	if !sameRecord(item, rec) {
		t.Error("no dates: same studio should match")
	}
	rec.Studio = "Nope"
	if sameRecord(item, rec) {
		t.Error("no dates and a different studio must not match")
	}
	if sameRecord(recoverRow{title: "", date: "2022-01-03"}, provider.MediaResult{Title: "", Date: "2022-01-03"}) {
		t.Error("an empty title must never match")
	}
}

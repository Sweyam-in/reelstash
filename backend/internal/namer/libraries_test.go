package namer

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"reelstash/backend/internal/provider"
)

func TestMediaProvidersFollowLibrary(t *testing.T) {
	e, pool := integrationEngine(t, &stubProvider{id: "alpha"}, &stubProvider{id: "beta"})
	ctx := context.Background()
	itemID, fileID, _ := newTitle(t, pool, "Some Movie (2020).mp4")

	ids := func() []string {
		var out []string
		for _, p := range e.mediaProviders(ctx, itemID, "media") {
			out = append(out, p.ID())
		}
		return out
	}
	// Outside every library: every enabled provider.
	if got := ids(); len(got) != 2 {
		t.Fatalf("unassigned title providers = %v, want both", got)
	}

	var locID, libID int64
	if err := pool.QueryRow(ctx, `SELECT storage_location_id FROM media_file WHERE id = $1`, fileID).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO library (name) VALUES ($1) RETURNING id`, "lib-"+uuid.NewString()).Scan(&libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM library WHERE id = $1`, libID) })
	if _, err := pool.Exec(ctx, `INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $2, '')`, libID, locID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SELECT reassign_libraries()`); err != nil {
		t.Fatal(err)
	}
	// In a library with no providers ticked: matching is off.
	if got := ids(); len(got) != 0 {
		t.Fatalf("library without providers = %v, want none", got)
	}
	// The library's own order wins over registration order.
	if _, err := pool.Exec(ctx, `INSERT INTO library_provider (library_id, provider, position) VALUES ($1, 'beta', 0), ($1, 'alpha', 1)`, libID); err != nil {
		t.Fatal(err)
	}
	if got := ids(); len(got) != 2 || got[0] != "beta" || got[1] != "alpha" {
		t.Fatalf("library providers = %v, want [beta alpha]", got)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM library_provider WHERE library_id = $1 AND provider = 'beta'`, libID); err != nil {
		t.Fatal(err)
	}
	if got := ids(); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("after removing beta = %v, want [alpha]", got)
	}
}

func TestApplyEntertainmentEpisode(t *testing.T) {
	e, pool := integrationEngine(t)
	ctx := context.Background()
	itemID, _, _ := newTitle(t, pool, "Severance.S01E02.mkv")
	tag := uuid.NewString()[:8]
	actor, director, show := "Adam Scott "+tag, "Ben Stiller "+tag, "Severance "+tag
	var actorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, actor).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id) VALUES ($1, $2)`, itemID, actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM performer WHERE name = ANY($1)`, []string{actor, director})
		pool.Exec(ctx, `DELETE FROM series WHERE name = $1`, show)
	})

	scene := provider.MediaResult{
		Kind: "episode", Show: show, ShowRef: provider.Ref{Provider: "tmdb", ExternalID: "tv/" + tag}, Season: 1, Episode: 2,
		Characters: map[string]string{actor: "Mark S."},
		Crew:       []provider.CrewCredit{{Name: director, Job: "director", Ref: provider.Ref{Provider: "tmdb", ExternalID: "p-" + tag}}},
	}
	e.applyEntertainment(ctx, itemID, scene)
	// A second episode of the same show joins the same series.
	other, _, _ := newTitle(t, pool, "Severance.S01E03.mkv")
	scene.Episode = 3
	e.applyEntertainment(ctx, other, scene)

	var kind string
	var season, episode int
	var seriesID, otherSeries int64
	if err := pool.QueryRow(ctx, `SELECT kind, season_number, episode_number, series_id FROM media_item WHERE id = $1`, itemID).Scan(&kind, &season, &episode, &seriesID); err != nil {
		t.Fatal(err)
	}
	if kind != "episode" || season != 1 || episode != 2 {
		t.Fatalf("episode fields = %s S%dE%d", kind, season, episode)
	}
	if err := pool.QueryRow(ctx, `SELECT series_id FROM media_item WHERE id = $1`, other).Scan(&otherSeries); err != nil || otherSeries != seriesID {
		t.Fatalf("second episode series = %d, want %d (%v)", otherSeries, seriesID, err)
	}
	var seriesKind string
	if err := pool.QueryRow(ctx, `SELECT kind FROM series WHERE id = $1`, seriesID).Scan(&seriesKind); err != nil || seriesKind != "show" {
		t.Fatalf("series kind = %q (%v)", seriesKind, err)
	}
	var character string
	if err := pool.QueryRow(ctx, `SELECT character FROM media_item_performer WHERE media_item_id = $1 AND performer_id = $2`, itemID, actorID).Scan(&character); err != nil || character != "Mark S." {
		t.Fatalf("character = %q (%v)", character, err)
	}
	var job string
	if err := pool.QueryRow(ctx, `SELECT c.job FROM media_item_crew c JOIN performer p ON p.id = c.performer_id WHERE c.media_item_id = $1 AND p.name = $2`, itemID, director).Scan(&job); err != nil || job != "director" {
		t.Fatalf("crew job = %q (%v)", job, err)
	}
}

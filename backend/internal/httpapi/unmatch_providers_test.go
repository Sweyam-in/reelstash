package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/config"
	"reelstash/backend/internal/testdb"
)

func unmatchAPI(t *testing.T) (*API, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := testdb.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return &API{DB: pool, Cfg: config.Config{PosterDir: t.TempDir()}}, pool
}

func callUnmatch(t *testing.T, h http.HandlerFunc, id int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", strconv.FormatInt(id, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUnmatchPerformerOneProvider(t *testing.T) {
	a, pool := unmatchAPI(t)
	ctx := context.Background()
	name := "P " + uuid.NewString()[:8]
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO performer (name, match_status, remote_metadata) VALUES ($1, 'matched', '{"provider":"tmdb","aliases":["A-tmdb","A-both"]}')
		RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pool.Exec(ctx, `INSERT INTO match_link (kind, entity_id, provider, external_id, remote_metadata) VALUES ('performer', $1, 'x-alpha', $2, '{"aliases":["A-pd","A-both"]}')`, id, "pd-"+name)
	pool.Exec(ctx, `INSERT INTO match_link (kind, entity_id, provider, external_id, remote_metadata) VALUES ('performer', $1, 'tmdb', $2, '{"aliases":["A-tmdb","A-both"]}')`, id, "tm-"+name)
	for _, alias := range []string{"A-pd", "A-tmdb", "A-both"} {
		pool.Exec(ctx, `INSERT INTO performer_alias (performer_id, alias) VALUES ($1, $2)`, id, alias)
	}

	rec := callUnmatch(t, a.unmatchPerformer, id, `{"provider":"tmdb","markWrong":true}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"linksRemaining":1`) {
		t.Fatalf("unlink tmdb: %d %s", rec.Code, rec.Body)
	}
	if n := linkCount(t, pool, "performer", id); n != 1 {
		t.Errorf("one link should remain, got %d", n)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM performer_rejected_match WHERE performer_id = $1 AND provider = 'tmdb'`, id); n != 1 {
		t.Errorf("tmdb rejection = %d", n)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM performer_rejected_match WHERE performer_id = $1 AND provider = 'x-alpha'`, id); n != 0 {
		t.Errorf("the other provider must not be rejected, got %d", n)
	}
	var status string
	pool.QueryRow(ctx, `SELECT match_status FROM performer WHERE id = $1`, id).Scan(&status)
	if status != "matched" {
		t.Errorf("performer should stay matched via the other provider: %s", status)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM performer_alias WHERE performer_id = $1 AND alias = 'A-tmdb'`, id); n != 0 {
		t.Errorf("alias unique to the removed provider should go")
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM performer_alias WHERE performer_id = $1 AND alias IN ('A-pd', 'A-both')`, id); n != 2 {
		t.Errorf("aliases the remaining provider also supplies must stay")
	}

	if rec := callUnmatch(t, a.unmatchPerformer, id, `{"provider":"tmdb"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unlinking a provider that isn't linked should 404, got %d", rec.Code)
	}

	// Removing the last link resets the performer.
	rec = callUnmatch(t, a.unmatchPerformer, id, `{"provider":"x-alpha"}`)
	if rec.Code != 200 {
		t.Fatalf("unlink last: %d %s", rec.Code, rec.Body)
	}
	pool.QueryRow(ctx, `SELECT match_status FROM performer WHERE id = $1`, id).Scan(&status)
	if status != "unmatched" || linkCount(t, pool, "performer", id) != 0 {
		t.Errorf("last unlink should reset: %s", status)
	}
}

func TestUnmatchMediaOneProvider(t *testing.T) {
	a, pool := unmatchAPI(t)
	ctx := context.Background()
	var itemID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_item (title, sort_title, metadata_source, match_status, description)
		VALUES ('Real Title', 'Real Title', 'NFO', 'matched', 'kept?') RETURNING id`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var locID int64
	pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, "u-"+uuid.NewString(), t.TempDir()).Scan(&locID)
	pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size) VALUES ($1, $2, 'x.mp4', 'x.mp4', 'mp4', 1)`, itemID, locID)
	pool.Exec(ctx, `INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('media', $1, 'x-alpha', 'scenes/1'), ('media', $1, 'tmdb', 'movie/9')`, itemID)

	rec := callUnmatch(t, a.unmatchMedia, itemID, `{"provider":"tmdb","markWrong":true}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"linksRemaining":1`) {
		t.Fatalf("unlink tmdb: %d %s", rec.Code, rec.Body)
	}
	var title, status string
	pool.QueryRow(ctx, `SELECT title, match_status FROM media_item WHERE id = $1`, itemID).Scan(&title, &status)
	if title != "Real Title" || status != "matched" {
		t.Errorf("title must keep its details while another provider is linked: %q %q", title, status)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM media_rejected_match WHERE media_item_id = $1 AND provider = 'tmdb' AND scene_uuid = 'movie/9'`, itemID); n != 1 {
		t.Errorf("tmdb rejection = %d", n)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM media_rejected_match WHERE media_item_id = $1 AND provider = 'x-alpha'`, itemID); n != 0 {
		t.Errorf("the other provider must not be rejected")
	}

	rec = callUnmatch(t, a.unmatchMedia, itemID, `{"provider":"x-alpha"}`)
	if rec.Code != 200 {
		t.Fatalf("unlink last: %d %s", rec.Code, rec.Body)
	}
	var desc *string
	pool.QueryRow(ctx, `SELECT title, match_status, description FROM media_item WHERE id = $1`, itemID).Scan(&title, &status, &desc)
	if title != "x" || status != "unmatched" || desc != nil {
		t.Errorf("last unlink should reset the title: %q %q %v", title, status, desc)
	}
}

func linkCount(t *testing.T, pool *pgxpool.Pool, kind string, id int64) int {
	return count(t, pool, `SELECT COUNT(*) FROM match_link WHERE kind = $1 AND entity_id = $2`, kind, id)
}

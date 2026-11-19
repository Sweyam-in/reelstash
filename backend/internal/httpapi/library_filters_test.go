package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// The Library page's filters: runtime, file quality, this user's progress, date added and
// several studios at once, each narrowing the selected library's titles.
func TestLibraryFilters(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	var adminID int64
	if err := f.pool.QueryRow(ctx, `SELECT user_id FROM web_session WHERE id = $1`, f.adminCookie).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	studio := func(name string) int64 {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(ctx, `INSERT INTO studio (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM studio WHERE id = $1`, id) })
		return id
	}
	halcyon, lumen, other := uniq("Halcyon"), uniq("Lumen"), uniq("Other")
	studioIDs := map[string]int64{halcyon: studio(halcyon), lumen: studio(lumen), other: studio(other)}
	created := []int64{}
	video := func(title, rel string, seconds, width, height int, studioName string, added time.Time) int64 {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(ctx, `INSERT INTO media_item (title, duration_seconds, studio_id, date_added) VALUES ($1, $2, $3, $4) RETURNING id`,
			uniq(title), seconds, studioIDs[studioName], added).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size, width, height)
			VALUES ($1, $2, $3, 'x.mp4', 'mp4', 1, $4, $5)`, id, f.disk, rel, width, height); err != nil {
			t.Fatal(err)
		}
		created = append(created, id)
		return id
	}
	now := time.Now()
	short := video("Short", "movies/filters-short.mp4", 300, 3840, 2160, halcyon, now)
	long := video("Long", "movies/filters-long.mp4", 5400, 1920, 1080, lumen, now)
	mid := video("Mid", "movies/filters-mid.mp4", 1200, 1280, 720, other, now)
	old := video("Old", "movies/filters-old.mp4", 2400, 640, 480, other, now.AddDate(0, 0, -60))
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM playback_state WHERE media_item_id = ANY($1)`, created)
		f.pool.Exec(context.Background(), `DELETE FROM media_file WHERE media_item_id = ANY($1)`, created)
		f.pool.Exec(context.Background(), `DELETE FROM media_item WHERE id = ANY($1)`, created)
	})
	if _, err := f.pool.Exec(ctx, `INSERT INTO playback_state (user_id, media_item_id, position_seconds, completed) VALUES ($1, $2, 0, TRUE), ($1, $3, 100, FALSE)`, adminID, long, mid); err != nil {
		t.Fatal(err)
	}

	header := strconv.FormatInt(f.movies, 10)
	list := func(query string) map[int64]bool {
		t.Helper()
		return itemIDs(t, f.get(t, f.adminCookie, "/api/media?limit=200&"+query, "X-Library", header))
	}
	expect := func(query string, want ...int64) {
		t.Helper()
		got := list(query)
		wanted := map[int64]bool{}
		for _, id := range want {
			wanted[id] = true
			if !got[id] {
				t.Fatalf("%s: missing %d (got %v)", query, id, got)
			}
		}
		for _, id := range []int64{short, long, mid, old} {
			if got[id] && !wanted[id] {
				t.Fatalf("%s: unexpected %d", query, id)
			}
		}
	}
	expect("maxDuration=900", short)
	expect("minDuration=3600", long)
	expect("minDuration=1000&maxDuration=3000", mid, old)
	expect("quality=4k", short)
	expect("quality=1080p", long)
	expect("quality=720p", mid)
	expect("quality=sd", old)
	expect("addedWithin=7", short, long, mid)
	expect("studios="+url.QueryEscape(halcyon+","+lumen), short, long)
	expect("watched=watched", long)
	expect("watched=inProgress", mid)
	expect("watched=unwatched", short, old)
	expect("watched=unwatched&quality=4k", short)

	// Filters change the total the page shows.
	rec := f.get(t, f.adminCookie, "/api/media?limit=1&maxDuration=900", "X-Library", header)
	var body struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("total: %d %s", rec.Code, rec.Body.String())
	}
	if body.Total < 1 {
		t.Fatalf("total = %d", body.Total)
	}
}

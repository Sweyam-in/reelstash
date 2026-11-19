package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// Home's "library at a glance" counts and "Up next in your series".
func TestHomeSummaryAndSeriesProgress(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	var adminID, seriesID int64
	if err := f.pool.QueryRow(ctx, `SELECT user_id FROM web_session WHERE id = $1`, f.adminCookie).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO series (name) VALUES ($1) RETURNING id`, uniq("Long Coast")).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	parts := []int64{}
	for i := 1; i <= 3; i++ {
		var id int64
		if err := f.pool.QueryRow(ctx, `INSERT INTO media_item (title, series_id, series_position, duration_seconds, date_added) VALUES ($1, $2, $3, 1800, $4) RETURNING id`,
			uniq("Chapter "+strconv.Itoa(i)), seriesID, i, time.Now()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size) VALUES ($1, $2, $3, 'x.mp4', 'mp4', 1)`,
			id, f.disk, "movies/coast-"+strconv.Itoa(i)+"-"+strconv.FormatInt(seriesID, 10)+".mp4"); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, id)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, `DELETE FROM playback_state WHERE media_item_id = ANY($1)`, parts)
		f.pool.Exec(bg, `DELETE FROM media_file WHERE media_item_id = ANY($1)`, parts)
		f.pool.Exec(bg, `DELETE FROM media_item WHERE id = ANY($1)`, parts)
		f.pool.Exec(bg, `DELETE FROM series WHERE id = $1`, seriesID)
	})
	header := strconv.FormatInt(f.movies, 10)

	// Not started yet: not listed.
	seriesList := func() []homeSeries {
		t.Helper()
		rec := f.get(t, f.adminCookie, "/api/home/series", "X-Library", header)
		if rec.Code != http.StatusOK {
			t.Fatalf("series: %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Items []homeSeries `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Items
	}
	find := func(list []homeSeries) *homeSeries {
		for i := range list {
			if list[i].ID == seriesID {
				return &list[i]
			}
		}
		return nil
	}
	if find(seriesList()) != nil {
		t.Fatal("an unstarted series is listed")
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO playback_state (user_id, media_item_id, position_seconds, completed) VALUES ($1, $2, 0, TRUE)`, adminID, parts[0]); err != nil {
		t.Fatal(err)
	}
	got := find(seriesList())
	if got == nil || got.Total != 3 || got.Watched != 1 || got.Next == nil || got.Next.ID != parts[1] || got.NextPosition != 2 {
		t.Fatalf("series progress = %+v", got)
	}

	rec := f.get(t, f.adminCookie, "/api/home/summary", "X-Library", header)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", rec.Code, rec.Body.String())
	}
	var s homeSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Titles < 4 || s.AddedThisWeek < 3 || s.AddedThisWeekUnseen < 2 || s.TotalSeconds < 5400 || s.NeedsDetails < 3 {
		t.Fatalf("summary = %+v", s)
	}
	// A user without the library sees none of it.
	if rec := f.get(t, f.userCookie, "/api/home/summary", "X-Library", strconv.FormatInt(f.private, 10)); rec.Code == http.StatusOK {
		var other homeSummary
		json.Unmarshal(rec.Body.Bytes(), &other)
		if other.Titles != 0 {
			t.Fatalf("leaked titles from a library the user cannot open: %+v", other)
		}
	}
}

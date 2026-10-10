package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestPlaylistsAndActivityFollowSelectedLibrary(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	var userID int64
	if err := f.pool.QueryRow(ctx, `SELECT user_id FROM web_session WHERE id = $1`, f.adminCookie).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for _, mediaID := range []int64{f.movieItem, f.privItem} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO watch_session (user_id, media_item_id, watched_seconds, completed) VALUES ($1, $2, 30, true)`, userID, mediaID); err != nil {
			t.Fatal(err)
		}
	}
	makePlaylist := func(lib, media int64) int64 {
		t.Helper()
		header := strconv.FormatInt(lib, 10)
		rec := f.do(t, f.adminCookie, http.MethodPost, "/api/collections/", `{"name":"`+uniq("Library playlist")+`"}`, "X-Library", header)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create playlist: %d %s", rec.Code, rec.Body.String())
		}
		var result struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rec := f.do(t, f.adminCookie, http.MethodPost, "/api/collections/"+strconv.FormatInt(result.ID, 10)+"/items", `{"mediaItemId":`+strconv.FormatInt(media, 10)+`}`, "X-Library", header); rec.Code != http.StatusOK {
			t.Fatalf("add playlist item: %d %s", rec.Code, rec.Body.String())
		}
		return result.ID
	}
	movieList := makePlaylist(f.movies, f.movieItem)
	privateList := makePlaylist(f.private, f.privItem)
	for _, tc := range []struct{ library, visible, hidden, media int64 }{
		{f.movies, movieList, privateList, f.movieItem},
		{f.private, privateList, movieList, f.privItem},
	} {
		header := strconv.FormatInt(tc.library, 10)
		if ids := itemIDs(t, f.get(t, f.adminCookie, "/api/collections/", "X-Library", header)); !ids[tc.visible] || ids[tc.hidden] {
			t.Fatalf("library %d playlists: %v", tc.library, ids)
		}
		if rec := f.get(t, f.adminCookie, "/api/collections/"+strconv.FormatInt(tc.hidden, 10), "X-Library", header); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign playlist detail: %d", rec.Code)
		}
		if rec := f.do(t, f.adminCookie, http.MethodPost, "/api/collections/"+strconv.FormatInt(tc.visible, 10)+"/items", `{"mediaItemId":`+strconv.FormatInt(f.movieItem+f.privItem-tc.media, 10)+`}`, "X-Library", header); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign playlist title: %d", rec.Code)
		}
		rec := f.get(t, f.adminCookie, "/api/activity", "X-Library", header)
		if rec.Code != http.StatusOK {
			t.Fatalf("activity: %d %s", rec.Code, rec.Body.String())
		}
		var activity struct {
			Totals struct {
				Plays        int64 `json:"plays"`
				UniqueTitles int64 `json:"uniqueTitles"`
			} `json:"totals"`
			Recent []struct {
				ID int64 `json:"id"`
			} `json:"recent"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &activity); err != nil {
			t.Fatal(err)
		}
		if activity.Totals.Plays != 1 || activity.Totals.UniqueTitles != 1 || len(activity.Recent) != 1 || activity.Recent[0].ID != tc.media {
			t.Fatalf("library %d activity: %s", tc.library, rec.Body.String())
		}
	}
}

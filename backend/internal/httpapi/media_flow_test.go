package httpapi

import (
	"context"
	"net/http"
	"testing"

	"reelstash/backend/internal/namer"
)

func TestPosterAndBackdropRemainIndependent(t *testing.T) {
	a, pool := unmatchAPI(t)
	a.Namer = namer.New(pool, nil, nil, "", a.Cfg.PosterDir)
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO media_item (title) VALUES ($1) RETURNING id`, uniq("Artwork")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM media_item WHERE id = $1`, id) })
	image := []byte{0xff, 0xd8, 0xff, 0xd9}
	if err := a.Namer.SetMediaPosterUpload(ctx, id, image); err != nil {
		t.Fatal(err)
	}
	if err := a.Namer.SetMediaBackdropUpload(ctx, id, image); err != nil {
		t.Fatal(err)
	}
	var posterID, backdropID int64
	if err := pool.QueryRow(ctx, `SELECT poster_image_id, backdrop_image_id FROM media_item WHERE id = $1`, id).Scan(&posterID, &backdropID); err != nil {
		t.Fatal(err)
	}
	if posterID == backdropID {
		t.Fatal("poster and backdrop should have separate artwork records")
	}
	rec := callUnmatch(t, a.deleteMediaBackdrop, id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete backdrop: %d %s", rec.Code, rec.Body.String())
	}
	var keptPoster int64
	var removedBackdrop *int64
	if err := pool.QueryRow(ctx, `SELECT poster_image_id, backdrop_image_id FROM media_item WHERE id = $1`, id).Scan(&keptPoster, &removedBackdrop); err != nil {
		t.Fatal(err)
	}
	if keptPoster != posterID || removedBackdrop != nil {
		t.Errorf("after removing backdrop: poster=%d, backdrop=%v", keptPoster, removedBackdrop)
	}
}

func TestCreateMediaItemAndFilters(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(&h.admin, "POST", "/api/media", map[string]string{"title": ""}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank title: %d", rec.Code)
	}
	word := "zz" + uniq("m")[2:]
	out := h.jsonBody(h.do(&h.admin, "POST", "/api/media", map[string]string{"title": "Alpha " + word}), http.StatusCreated)
	id := int64(out["id"].(float64))
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM media_item WHERE id = $1`, id) })
	out2 := h.jsonBody(h.do(&h.admin, "POST", "/api/media", map[string]string{"title": "Beta " + word}), http.StatusCreated)
	id2 := int64(out2["id"].(float64))
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM media_item WHERE id = $1`, id2) })

	var src string
	h.pool.QueryRow(context.Background(), `SELECT metadata_source FROM media_item WHERE id = $1`, id).Scan(&src)
	if src != "MANUAL" {
		t.Errorf("metadata_source = %q, want MANUAL", src)
	}

	found := h.items(h.do(&h.admin, "GET", "/api/media?q="+word, nil))
	if !hasID(found, id) || !hasID(found, id2) || len(found) != 2 {
		t.Errorf("search by q found %d items, want the two created", len(found))
	}
	paged := h.jsonBody(h.do(&h.admin, "GET", "/api/media?q="+word+"&limit=1&offset=1", nil), 200)
	if n := len(paged["items"].([]any)); n != 1 {
		t.Errorf("limit=1 returned %d items", n)
	}
	if paged["total"] != nil && paged["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", paged["total"])
	}
	// Out-of-range paging falls back to defaults instead of erroring.
	h.expect(h.do(&h.admin, "GET", "/api/media?limit=99999&offset=-4", nil), 200)
	// An unknown filter value yields an empty page, not an error.
	if got := h.items(h.do(&h.admin, "GET", "/api/media?q="+word+"&favorite=true", nil)); len(got) != 0 {
		t.Errorf("no favorites yet, got %d", len(got))
	}
	// A user with no library access sees none of them.
	if got := h.items(h.do(&h.user, "GET", "/api/media?q="+word, nil)); len(got) != 0 {
		t.Errorf("user without library access saw %d items", len(got))
	}
}

func TestPlaybackStateAndContinueWatching(t *testing.T) {
	h := newHarness(t)
	title := h.newTitle(uniq("Playing"))
	put := "/api/playback/" + itoa(title)

	h.expect(h.do(&h.admin, "PUT", put, map[string]any{"positionSeconds": 42.5}), 200)
	if !hasID(h.items(h.do(&h.admin, "GET", "/api/playback/continue", nil)), title) {
		t.Error("in-progress title should be in continue watching")
	}
	// It is this user's alone.
	if hasID(h.items(h.do(&h.other, "GET", "/api/playback/continue", nil)), title) {
		t.Error("continue watching leaked to another user")
	}

	// Saving again updates rather than duplicating.
	h.expect(h.do(&h.admin, "PUT", put, map[string]any{"positionSeconds": 90}), 200)
	var n int
	var pos float64
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*), MAX(position_seconds) FROM playback_state WHERE user_id = $1 AND media_item_id = $2`, h.admin.id, title).Scan(&n, &pos)
	if n != 1 || pos != 90 {
		t.Errorf("playback rows=%d pos=%v, want 1 row at 90", n, pos)
	}

	// Finishing marks the title watched, drops it from continue watching and
	// from watch later.
	h.expect(h.do(&h.admin, "PUT", "/api/watch-later/"+itoa(title), nil), 200)
	h.expect(h.do(&h.admin, "PUT", put, map[string]any{"positionSeconds": 100, "completed": true}), 200)
	if hasID(h.items(h.do(&h.admin, "GET", "/api/playback/continue", nil)), title) {
		t.Error("completed title must leave continue watching")
	}
	var watched bool
	h.pool.QueryRow(context.Background(), `SELECT watched FROM media_item WHERE id = $1`, title).Scan(&watched)
	if !watched {
		t.Error("completing should mark the title watched")
	}
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM watch_later WHERE user_id = $1 AND media_item_id = $2`, h.admin.id, title).Scan(&n)
	if n != 0 {
		t.Error("completing should clear watch later")
	}

	if rec := h.do(&h.admin, "PUT", put, "{bad"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/playback/nope", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", rec.Code)
	}
}

func TestMarkersArePerUserAndValidated(t *testing.T) {
	h := newHarness(t)
	second := h.newActor("ADMIN")
	title := h.newTitle(uniq("Marked"))
	base := "/api/media/" + itoa(title) + "/markers"

	for _, bad := range []any{map[string]any{"seconds": -1}, map[string]any{"seconds": 1e12}, "{oops"} {
		if rec := h.do(&h.admin, "POST", base, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("marker %v: %d, want 400", bad, rec.Code)
		}
	}
	late := h.jsonBody(h.do(&h.admin, "POST", base, map[string]any{"seconds": 120, "title": "  Late  "}), http.StatusCreated)
	early := h.jsonBody(h.do(&h.admin, "POST", base, map[string]any{"seconds": 10}), http.StatusCreated)
	if late["title"] != "Late" {
		t.Errorf("title should be trimmed, got %q", late["title"])
	}

	items := h.items(h.do(&h.admin, "GET", base, nil))
	if len(items) != 2 || items[0]["seconds"].(float64) != 10 {
		t.Errorf("markers should be ordered by position: %v", items)
	}
	if got := h.items(h.do(&second, "GET", base, nil)); len(got) != 0 {
		t.Errorf("markers leaked to another user: %v", got)
	}

	lateID := itoa(int64(late["id"].(float64)))
	earlyID := itoa(int64(early["id"].(float64)))
	// Another user can neither edit nor delete it.
	if rec := h.do(&second, "PATCH", base+"/"+lateID, map[string]any{"title": "mine"}); rec.Code != http.StatusNotFound {
		t.Errorf("foreign edit: %d, want 404", rec.Code)
	}
	if rec := h.do(&second, "DELETE", base+"/"+lateID, nil); rec.Code != http.StatusNotFound {
		t.Errorf("foreign delete: %d, want 404", rec.Code)
	}

	upd := h.jsonBody(h.do(&h.admin, "PATCH", base+"/"+lateID, map[string]any{"seconds": 5}), 200)
	if upd["seconds"].(float64) != 5 {
		t.Errorf("update = %v", upd)
	}
	if rec := h.do(&h.admin, "PATCH", base+"/"+lateID, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty patch: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PATCH", base+"/"+lateID, map[string]any{"seconds": -3}); rec.Code != http.StatusBadRequest {
		t.Errorf("negative patch: %d", rec.Code)
	}
	h.expect(h.do(&h.admin, "DELETE", base+"/"+earlyID, nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "DELETE", base+"/"+earlyID, nil), http.StatusNotFound)
}

func TestMarkerLimitPerTitle(t *testing.T) {
	h := newHarness(t)
	title := h.newTitle(uniq("Many markers"))
	if _, err := h.pool.Exec(context.Background(), `
		INSERT INTO media_marker (media_item_id, user_id, seconds, title)
		SELECT $1, $2, g, '' FROM generate_series(1, $3) g`, title, h.admin.id, maxMarkersPerTitle); err != nil {
		t.Fatal(err)
	}
	rec := h.do(&h.admin, "POST", "/api/media/"+itoa(title)+"/markers", map[string]any{"seconds": 9999})
	if rec.Code != http.StatusConflict {
		t.Errorf("over the limit: %d, want 409", rec.Code)
	}
}

// Favorites, ratings and watched are anyone's; details are admin-only.
func TestMediaPatchPermissionsAndPerUserState(t *testing.T) {
	f := newLibraryFixture(t)
	path := "/api/media/" + itoa(f.movieItem)

	if rec := f.do(t, f.userCookie, "PATCH", path, `{"title":"Hijacked"}`); rec.Code != http.StatusForbidden {
		t.Errorf("user editing title: %d, want 403", rec.Code)
	}
	if rec := f.do(t, f.userCookie, "PATCH", path, "{bad"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", rec.Code)
	}

	rec := f.do(t, f.userCookie, "PATCH", path, `{"favorite":true,"rating":8,"watched":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("user personal state: %d %s", rec.Code, rec.Body.String())
	}
	// The admin has not favorited or rated it.
	if n := count(t, f.pool, `SELECT COUNT(*) FROM media_item_favorite WHERE media_item_id = $1`, f.movieItem); n != 1 {
		t.Errorf("favorites = %d, want 1 (the user's)", n)
	}
	if n := count(t, f.pool, `SELECT COUNT(*) FROM media_item_rating WHERE media_item_id = $1`, f.movieItem); n != 1 {
		t.Errorf("ratings = %d, want 1", n)
	}
	// Ratings above 10 clamp; 0 clears.
	f.do(t, f.userCookie, "PATCH", path, `{"rating":99}`)
	if n := count(t, f.pool, `SELECT rating FROM media_item_rating WHERE media_item_id = $1`, f.movieItem); n != 10 {
		t.Errorf("clamped rating = %d, want 10", n)
	}
	f.do(t, f.userCookie, "PATCH", path, `{"rating":0,"favorite":false}`)
	if n := count(t, f.pool, `SELECT COUNT(*) FROM media_item_rating WHERE media_item_id = $1`, f.movieItem); n != 0 {
		t.Error("rating 0 should clear it")
	}
	if n := count(t, f.pool, `SELECT COUNT(*) FROM media_item_favorite WHERE media_item_id = $1`, f.movieItem); n != 0 {
		t.Error("favorite:false should remove it")
	}

	// The admin may edit details, which marks the title MANUAL.
	rec = f.do(t, f.adminCookie, "PATCH", path, `{"title":"Edited by admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin edit: %d %s", rec.Code, rec.Body.String())
	}
	var title, src string
	f.pool.QueryRow(context.Background(), `SELECT title, metadata_source FROM media_item WHERE id = $1`, f.movieItem).Scan(&title, &src)
	if title != "Edited by admin" || src != "MANUAL" {
		t.Errorf("title=%q source=%q", title, src)
	}
	if rec := f.do(t, f.adminCookie, "PATCH", "/api/media/999999999", `{"watched":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("patch missing: %d", rec.Code)
	}
}

func TestUserCannotReachTitleOutsideLibrary(t *testing.T) {
	f := newLibraryFixture(t)
	priv := "/api/media/" + itoa(f.privItem)
	for _, p := range []string{priv, priv + "/markers", priv + "/related", priv + "/playback-info"} {
		if rec := f.get(t, f.userCookie, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s as scoped user: %d, want 404", p, rec.Code)
		}
	}
	if rec := f.do(t, f.userCookie, "PUT", "/api/playback/"+itoa(f.privItem), `{"positionSeconds":5}`); rec.Code != http.StatusNotFound {
		t.Errorf("playback on foreign title: %d, want 404", rec.Code)
	}
	if rec := f.do(t, f.userCookie, "PUT", "/api/watch-later/"+itoa(f.privItem), ""); rec.Code != http.StatusNotFound {
		t.Errorf("watch-later on foreign title: %d, want 404", rec.Code)
	}
	if rec := f.get(t, f.adminCookie, priv); rec.Code != http.StatusOK {
		t.Errorf("admin: %d", rec.Code)
	}
}

func TestAdminReadEndpointsRespond(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{
		"/api/admin/overview",
		"/api/admin/analytics",
		"/api/admin/analytics/usage",
		"/api/admin/health",
		"/api/admin/providers",
		"/api/admin/libraries",
		"/api/admin/scans",
		"/api/admin/webhooks",
		"/api/admin/videos",
		"/api/admin/review",
		"/api/admin/review/matches",
		"/api/admin/rename",
		"/api/admin/duplicates/scan",
		"/api/admin/performers/empty",
		"/api/admin/performers/sync",
		"/api/admin/studios/sync",
		"/api/duplicates",
		"/api/performers",
		"/api/performers/duplicates",
		"/api/studios",
		"/api/series",
		"/api/recommendations/home",
		"/api/activity",
		"/api/libraries",
		"/api/providers",
		"/api/applock",
	} {
		rec := h.do(&h.admin, "GET", p, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
}

func TestUserReadEndpointsRespond(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{
		"/api/media", "/api/categories", "/api/tags", "/api/collections", "/api/notifications",
		"/api/playback/continue", "/api/recommendations/home", "/api/activity", "/api/libraries",
		"/api/providers", "/api/applock", "/api/performers", "/api/studios", "/api/series", "/api/search?q=a",
	} {
		if rec := h.do(&h.user, "GET", p, nil); rec.Code != http.StatusOK {
			t.Errorf("GET %s as user: %d %s", p, rec.Code, rec.Body.String())
		}
	}
}

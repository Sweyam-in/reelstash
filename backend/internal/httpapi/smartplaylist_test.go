package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func (h *harness) cond(field, op string, value any) map[string]any {
	c := map[string]any{"field": field}
	if op != "" {
		c["op"] = op
	}
	if value != nil {
		c["value"] = value
	}
	return c
}

func (h *harness) smartPlaylist(who *actor, name string, rules map[string]any) int64 {
	h.t.Helper()
	id := h.createdID(who, "/api/collections", map[string]any{"name": name, "kind": "SMART", "rules": rules})
	h.t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, id) })
	return id
}

func (h *harness) playlistItems(who *actor, id int64) []map[string]any {
	h.t.Helper()
	out := h.jsonBody(h.do(who, "GET", "/api/collections/"+itoa(id), nil), http.StatusOK)
	raw, _ := out["items"].([]any)
	items := []map[string]any{}
	for _, v := range raw {
		items = append(items, v.(map[string]any))
	}
	return items
}

func TestSmartPlaylistMatchesByTextAndTagAndFollowsLibraryChanges(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	word := "zq" + uniq("smart")[6:]
	a := h.newTitle(word + " alpha")
	b := h.newTitle(word + " beta")
	other := h.newTitle("unrelated " + word[:4])

	tag := uniq("tag")
	var tagID int64
	if err := h.pool.QueryRow(ctx, `INSERT INTO tag (name) VALUES ($1) RETURNING id`, tag).Scan(&tagID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(ctx, `DELETE FROM tag WHERE id = $1`, tagID) })
	h.pool.Exec(ctx, `INSERT INTO media_item_tag (media_item_id, tag_id) VALUES ($1, $2)`, b, tagID)

	id := h.smartPlaylist(&h.admin, uniq("Text"), map[string]any{"match": "all", "conditions": []any{h.cond("text", "", word)}})
	items := h.playlistItems(&h.admin, id)
	if !hasID(items, a) || !hasID(items, b) || hasID(items, other) {
		t.Errorf("text rule matched %v", items)
	}

	both := h.smartPlaylist(&h.admin, uniq("Both"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("tag", "is", tag)}})
	if items := h.playlistItems(&h.admin, both); len(items) != 1 || !hasID(items, b) {
		t.Errorf("text AND tag should match only b, got %v", items)
	}

	either := h.smartPlaylist(&h.admin, uniq("Either"), map[string]any{"match": "any", "conditions": []any{h.cond("tag", "is", tag), h.cond("text", "", "alpha "+word)}})
	if items := h.playlistItems(&h.admin, either); !hasID(items, a) || !hasID(items, b) {
		t.Errorf("text OR tag should match a and b, got %v", items)
	}

	notTag := h.smartPlaylist(&h.admin, uniq("NotTag"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("tag", "isNot", tag)}})
	if items := h.playlistItems(&h.admin, notTag); len(items) != 1 || !hasID(items, a) {
		t.Errorf("tag isNot should leave only a, got %v", items)
	}

	// A title added later joins on its own.
	c := h.newTitle(word + " gamma")
	if items := h.playlistItems(&h.admin, id); !hasID(items, c) {
		t.Errorf("new title did not join the smart playlist: %v", items)
	}
}

func TestSmartPlaylistRatingWatchedFavoriteAreThePlaylistOwners(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	word := "zq" + uniq("mine")[5:]
	good := h.newTitle(word + " good")
	meh := h.newTitle(word + " meh")
	seen := h.newTitle(word + " seen")
	h.pool.Exec(ctx, `INSERT INTO media_item_rating (user_id, media_item_id, rating) VALUES ($1, $2, 10), ($1, $3, 4)`, h.admin.id, good, meh)
	h.pool.Exec(ctx, `INSERT INTO media_item_rating (user_id, media_item_id, rating) VALUES ($1, $2, 2)`, h.other.id, good) // someone else's opinion
	h.pool.Exec(ctx, `INSERT INTO playback_state (user_id, media_item_id, completed) VALUES ($1, $2, TRUE)`, h.admin.id, seen)
	h.pool.Exec(ctx, `INSERT INTO media_item_favorite (user_id, media_item_id) VALUES ($1, $2)`, h.admin.id, meh)

	rated := h.smartPlaylist(&h.admin, uniq("Rated"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("rating", "gte", 4)}})
	if items := h.playlistItems(&h.admin, rated); len(items) != 1 || !hasID(items, good) {
		t.Errorf("my rating >= 4 stars should be only 'good', got %v", items)
	}
	unwatched := h.smartPlaylist(&h.admin, uniq("Unwatched"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("watched", "", false)}})
	if items := h.playlistItems(&h.admin, unwatched); hasID(items, seen) || !hasID(items, good) || !hasID(items, meh) {
		t.Errorf("unwatched should skip 'seen', got %v", items)
	}
	fav := h.smartPlaylist(&h.admin, uniq("Fav"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("favorite", "", true)}})
	if items := h.playlistItems(&h.admin, fav); len(items) != 1 || !hasID(items, meh) {
		t.Errorf("favorites should be only 'meh', got %v", items)
	}
}

func TestSmartPlaylistDatesSortAndLimit(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	word := "zq" + uniq("dates")[6:]
	old := h.newTitle(word + " old")
	mid := h.newTitle(word + " mid")
	recent := h.newTitle(word + " recent")
	h.pool.Exec(ctx, `UPDATE media_item SET date_added = '2024-03-10', production_year = 1999 WHERE id = $1`, old)
	h.pool.Exec(ctx, `UPDATE media_item SET date_added = '2024-11-20', production_year = 2005 WHERE id = $1`, mid)
	h.pool.Exec(ctx, `UPDATE media_item SET date_added = now(), production_year = 2020 WHERE id = $1`, recent)

	from2024 := h.smartPlaylist(&h.admin, uniq("2024"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("addedYear", "", 2024)}, "sort": "oldest"})
	items := h.playlistItems(&h.admin, from2024)
	if len(items) != 2 || int64(items[0]["id"].(float64)) != old || int64(items[1]["id"].(float64)) != mid {
		t.Errorf("saved in 2024, oldest first, should be old then mid, got %v", items)
	}
	lastMonth := h.smartPlaylist(&h.admin, uniq("30d"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("addedWithin", "", 30)}})
	if items := h.playlistItems(&h.admin, lastMonth); len(items) != 1 || !hasID(items, recent) {
		t.Errorf("added within 30 days should be only 'recent', got %v", items)
	}
	range90s := h.smartPlaylist(&h.admin, uniq("years"), map[string]any{"conditions": []any{h.cond("text", "", word), h.cond("year", "lte", 2005), h.cond("addedAfter", "", "2024-06-01")}})
	if items := h.playlistItems(&h.admin, range90s); len(items) != 1 || !hasID(items, mid) {
		t.Errorf("year <= 2005 and added after June 2024 should be only 'mid', got %v", items)
	}
	capped := h.smartPlaylist(&h.admin, uniq("cap"), map[string]any{"conditions": []any{h.cond("text", "", word)}, "sort": "title", "limit": 2})
	out := h.jsonBody(h.do(&h.admin, "GET", "/api/collections/"+itoa(capped), nil), http.StatusOK)
	if out["total"].(float64) != 2 || len(out["items"].([]any)) != 2 {
		t.Errorf("limit 2 should cap the playlist at 2 titles, got %v", out)
	}
}

func TestSmartPlaylistListingCountsAndCovers(t *testing.T) {
	h := newHarness(t)
	word := "zq" + uniq("list")[5:]
	a := h.newTitle(word + " a")
	h.newTitle(word + " b")
	id := h.smartPlaylist(&h.admin, uniq("Listed"), map[string]any{"conditions": []any{h.cond("text", "", word)}})

	var found map[string]any
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/collections", nil)) {
		if int64(it["id"].(float64)) == id {
			found = it
		}
	}
	if found == nil {
		t.Fatal("smart playlist missing from the list")
	}
	if found["kind"] != "SMART" || found["mediaCount"].(float64) != 2 {
		t.Errorf("listing = %v, want kind SMART with 2 titles", found)
	}
	if covers, _ := found["covers"].([]any); len(covers) != 2 {
		t.Errorf("covers = %v, want 2", found["covers"])
	}
	_ = a
}

func TestSmartPlaylistRulesAreValidated(t *testing.T) {
	h := newHarness(t)
	bad := []map[string]any{
		{"conditions": []any{}},
		{"conditions": []any{h.cond("nonsense", "", "x")}},
		{"conditions": []any{h.cond("rating", "gte", 9)}},
		{"conditions": []any{h.cond("rating", "contains", 3)}},
		{"conditions": []any{h.cond("tag", "is", "")}},
		{"conditions": []any{h.cond("addedAfter", "", "yesterday")}},
		{"conditions": []any{h.cond("kind", "", "documentary")}},
		{"match": "sometimes", "conditions": []any{h.cond("favorite", "", true)}},
		{"sort": "sideways", "conditions": []any{h.cond("favorite", "", true)}},
		{"limit": -1, "conditions": []any{h.cond("favorite", "", true)}},
	}
	for i, rules := range bad {
		rec := h.do(&h.admin, "POST", "/api/collections", map[string]any{"name": uniq("bad"), "kind": "SMART", "rules": rules})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: status = %d, want 400 (%s)", i, rec.Code, rec.Body.String())
		}
	}
	if rec := h.do(&h.admin, "POST", "/api/collections", map[string]any{"name": uniq("norules"), "kind": "SMART"}); rec.Code != http.StatusBadRequest {
		t.Errorf("smart playlist without rules: status = %d, want 400", rec.Code)
	}
}

func TestSmartPlaylistCanBeEditedAndDeleted(t *testing.T) {
	h := newHarness(t)
	word := "zq" + uniq("edit")[5:]
	a := h.newTitle(word + " a")
	id := h.smartPlaylist(&h.admin, uniq("Editable"), map[string]any{"conditions": []any{h.cond("text", "", word)}})

	name := uniq("Renamed")
	h.expect(h.do(&h.admin, "PATCH", "/api/collections/"+itoa(id), map[string]any{
		"name": name, "description": "now with a description",
		"rules": map[string]any{"conditions": []any{h.cond("text", "", "no-such-title-"+word)}},
	}), http.StatusOK)
	out := h.jsonBody(h.do(&h.admin, "GET", "/api/collections/"+itoa(id), nil), http.StatusOK)
	c := out["collection"].(map[string]any)
	if c["name"] != name || c["description"] != "now with a description" {
		t.Errorf("collection = %v", c)
	}
	if out["total"].(float64) != 0 || hasID(h.playlistItems(&h.admin, id), a) {
		t.Errorf("new rules should match nothing, got %v", out)
	}
	var rules map[string]any
	raw, _ := json.Marshal(c["rules"])
	json.Unmarshal(raw, &rules)
	if rules["match"] != "all" || rules["sort"] != "added" {
		t.Errorf("rules were not normalised: %v", rules)
	}

	// Bad edits are refused and pinning still works on its own.
	h.expect(h.do(&h.admin, "PATCH", "/api/collections/"+itoa(id), map[string]any{"rules": map[string]any{"conditions": []any{}}}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PATCH", "/api/collections/"+itoa(id), map[string]any{"name": "  "}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PATCH", "/api/collections/"+itoa(id), map[string]any{"pinned": true}), http.StatusOK)

	// Rules only make sense on smart playlists.
	manual := h.createdID(&h.admin, "/api/collections", map[string]any{"name": uniq("Manual"), "kind": "MANUAL"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, manual) })
	h.expect(h.do(&h.admin, "PATCH", "/api/collections/"+itoa(manual), map[string]any{"rules": map[string]any{"conditions": []any{h.cond("favorite", "", true)}}}), http.StatusBadRequest)
	// Titles cannot be added to, or moved in, a smart playlist.
	h.expect(h.do(&h.admin, "POST", "/api/collections/"+itoa(id)+"/items", map[string]any{"mediaItemId": a}), http.StatusBadRequest)

	h.expect(h.do(&h.admin, "DELETE", "/api/collections/"+itoa(id), nil), http.StatusOK)
	h.expect(h.do(&h.admin, "GET", "/api/collections/"+itoa(id), nil), http.StatusNotFound)
}

func TestSmartPlaylistsArePrivate(t *testing.T) {
	h := newHarness(t)
	word := "zq" + uniq("priv")[5:]
	h.newTitle(word + " a")
	id := h.smartPlaylist(&h.admin, uniq("Private"), map[string]any{"conditions": []any{h.cond("text", "", word)}})
	h.expect(h.do(&h.other, "GET", "/api/collections/"+itoa(id), nil), http.StatusNotFound)
	h.expect(h.do(&h.other, "PATCH", "/api/collections/"+itoa(id), map[string]any{"name": "mine now"}), http.StatusNotFound)
	h.expect(h.do(&h.other, "DELETE", "/api/collections/"+itoa(id), nil), http.StatusNotFound)
}

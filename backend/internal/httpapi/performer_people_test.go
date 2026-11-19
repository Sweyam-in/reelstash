package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// The People page's extras: per-person unwatched counts, the "needs a photo" and
// "recently watched" lists, cast title counts and "notify me about new titles".
func TestPerformerDirectoryExtras(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	lib := h.library(nil)
	name := uniq("Dir Person")
	person := h.createdID(&h.admin, "/api/performers", map[string]string{"name": name})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM performer WHERE id = $1`, person) })
	watched, fresh := h.newTitle(uniq("Seen")), h.newTitle(uniq("Unseen"))
	for _, id := range []int64{watched, fresh} {
		h.pool.Exec(ctx, `UPDATE media_item SET library_id = $2 WHERE id = $1`, id, lib)
		if _, err := h.pool.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id) VALUES ($1, $2)`, id, person); err != nil {
			t.Fatal(err)
		}
	}
	h.finish(&h.admin, watched)

	find := func(path string) map[string]any {
		for _, it := range h.items(h.do(&h.admin, "GET", path, nil)) {
			if int64(it["id"].(float64)) == person {
				return it
			}
		}
		return nil
	}
	row := find("/api/performers?q=" + "Dir+Person")
	if row == nil {
		t.Fatal("person missing from the list")
	}
	if row["mediaCount"].(float64) != 2 || row["unwatchedCount"].(float64) != 1 || row["notify"].(bool) {
		t.Fatalf("row = %v, want 2 titles, 1 unwatched, no notify", row)
	}

	// No picture yet, so the person is in "needs a photo"; played a title, so also in "recent".
	if find("/api/performers?needsPhoto=true&limit=200") == nil {
		t.Fatal("expected the person under needsPhoto")
	}
	if find("/api/performers?recent=true&limit=200") == nil {
		t.Fatal("expected the person under recent")
	}

	// Notify on, then filtered lists and the media filter pick the person up.
	h.expect(h.do(&h.admin, "PUT", "/api/performers/"+itoa(person)+"/notify", map[string]bool{"notify": true}), http.StatusOK)
	if r := find("/api/performers?notify=true&limit=200"); r == nil || !r["notify"].(bool) {
		t.Fatalf("notify list = %v", r)
	}
	if !hasID(h.items(h.do(&h.admin, "GET", "/api/media?notifyOnly=true&limit=200", nil)), fresh) {
		t.Fatal("notifyOnly should include titles with a followed person")
	}
	got := h.jsonBody(h.do(&h.admin, "GET", "/api/performers/"+itoa(person), nil), http.StatusOK)
	if !got["notify"].(bool) {
		t.Fatalf("detail = %v, want notify", got)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/performers/"+itoa(person)+"/notify", map[string]bool{"notify": false}), http.StatusOK)
	if find("/api/performers?notify=true&limit=200") != nil {
		t.Fatal("person should leave the notify list")
	}

	// A title's cast says how many titles each person is in.
	detail := h.jsonBody(h.do(&h.admin, "GET", "/api/media/"+itoa(fresh), nil), http.StatusOK)
	cast, _ := detail["cast"].([]any)
	if len(cast) != 1 || cast[0].(map[string]any)["mediaCount"].(float64) != 2 {
		t.Fatalf("cast = %v, want one person in 2 titles", cast)
	}
}

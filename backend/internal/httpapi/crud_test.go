package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCategoriesCRUD(t *testing.T) {
	h := newHarness(t)
	name := uniq("Cat")

	if rec := h.do(&h.admin, "POST", "/api/categories", map[string]string{"name": ""}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty name: %d, want 400", rec.Code)
	}
	id := h.createdID(&h.admin, "/api/categories", map[string]string{"name": name})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM category WHERE id = $1`, id) })

	// Duplicate name/slug is rejected.
	if rec := h.do(&h.admin, "POST", "/api/categories", map[string]string{"name": name}); rec.Code == http.StatusCreated {
		t.Error("duplicate category must not be created")
	}

	var slug string
	if err := h.pool.QueryRow(context.Background(), `SELECT slug FROM category WHERE id = $1`, id).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if slug != slugifyName(name) {
		t.Errorf("slug = %q, want %q", slug, slugifyName(name))
	}

	// Anyone signed in can list; the admin sees the new row.
	if !hasID(h.items(h.do(&h.admin, "GET", "/api/categories", nil)), id) {
		t.Error("admin list should include the category")
	}
	h.items(h.do(&h.user, "GET", "/api/categories", nil))

	renamed := uniq("Renamed")
	h.expect(h.do(&h.admin, "PUT", "/api/categories/"+itoa(id), map[string]string{"name": renamed}), 200)
	if rec := h.do(&h.admin, "PUT", "/api/categories/999999999", map[string]string{"name": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("update missing: %d, want 404", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/categories/abc", map[string]string{"name": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("update bad id: %d, want 400", rec.Code)
	}

	h.expect(h.do(&h.admin, "DELETE", "/api/categories/"+itoa(id), nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "DELETE", "/api/categories/"+itoa(id), nil), http.StatusNotFound)
}

func TestCategoryCountsFollowTitles(t *testing.T) {
	h := newHarness(t)
	id := h.createdID(&h.admin, "/api/categories", map[string]string{"name": uniq("Counted")})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM category WHERE id = $1`, id) })
	title := h.newTitle(uniq("Counted title"))
	if _, err := h.pool.Exec(context.Background(), `INSERT INTO media_item_category (media_item_id, category_id) VALUES ($1, $2)`, title, id); err != nil {
		t.Fatal(err)
	}
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/categories", nil)) {
		if int64(it["id"].(float64)) == id && it["mediaCount"].(float64) != 1 {
			t.Errorf("mediaCount = %v, want 1", it["mediaCount"])
		}
	}
	// A non-empty category is never swept by cleanup.
	h.expect(h.do(&h.admin, "POST", "/api/admin/categories/cleanup", nil), 200)
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM category WHERE id = $1`, id).Scan(&n)
	if n != 1 {
		t.Error("cleanup deleted a category that still has a title")
	}
}

func TestEmptyCategoryCleanup(t *testing.T) {
	h := newHarness(t)
	id := h.createdID(&h.admin, "/api/categories", map[string]string{"name": uniq("Empty")})
	out := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/categories/empty", nil), 200)
	if out["count"].(float64) < 1 {
		t.Errorf("empty count = %v, want >= 1", out["count"])
	}
	h.jsonBody(h.do(&h.admin, "POST", "/api/admin/categories/cleanup", nil), 200)
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM category WHERE id = $1`, id).Scan(&n)
	if n != 0 {
		t.Error("cleanup should delete empty categories")
	}
}

func TestTagsCRUD(t *testing.T) {
	h := newHarness(t)
	name := uniq("Tag")
	if rec := h.do(&h.admin, "POST", "/api/tags", map[string]string{}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty tag name: %d, want 400", rec.Code)
	}
	id := h.createdID(&h.admin, "/api/tags", map[string]string{"name": name, "optionalColor": "#ff0000"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM tag WHERE id = $1`, id) })

	found := false
	for _, it := range h.items(h.do(&h.user, "GET", "/api/tags", nil)) {
		if int64(it["id"].(float64)) == id {
			found = true
			if it["optionalColor"] != "#ff0000" {
				t.Errorf("color = %v", it["optionalColor"])
			}
		}
	}
	if !found {
		t.Error("tag missing from list")
	}

	other := h.createdID(&h.admin, "/api/tags", map[string]string{"name": uniq("Other tag")})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM tag WHERE id = $1`, other) })

	renamed := uniq("Renamed tag")
	h.expect(h.do(&h.admin, "PUT", "/api/admin/tags/"+itoa(id), map[string]string{"name": "  " + renamed + "  "}), 200)
	var got string
	h.pool.QueryRow(context.Background(), `SELECT name FROM tag WHERE id = $1`, id).Scan(&got)
	if got != renamed {
		t.Errorf("rename should trim: got %q", got)
	}
	// Renaming onto an existing tag conflicts.
	if rec := h.do(&h.admin, "PUT", "/api/admin/tags/"+itoa(other), map[string]string{"name": renamed}); rec.Code != http.StatusConflict {
		t.Errorf("rename collision: %d, want 409", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/tags/"+itoa(id), map[string]string{"name": "   "}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank rename: %d, want 400", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/tags/999999999", map[string]string{"name": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("rename missing: %d, want 404", rec.Code)
	}
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/tags/"+itoa(id), nil), 200)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/tags/"+itoa(id), nil), http.StatusNotFound)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/tags/zzz", nil), http.StatusBadRequest)
}

func TestPlaylistsArePrivateAndOrdered(t *testing.T) {
	h := newHarness(t)
	a, b, c := h.newTitle(uniq("A")), h.newTitle(uniq("B")), h.newTitle(uniq("C"))

	name := uniq("List")
	if rec := h.do(&h.admin, "POST", "/api/collections", map[string]string{"name": ""}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty name: %d", rec.Code)
	}
	id := h.createdID(&h.admin, "/api/collections", map[string]string{"name": name})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, id) })
	if rec := h.do(&h.admin, "POST", "/api/collections", map[string]string{"name": name}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name for the same owner: %d, want 409", rec.Code)
	}
	base := "/api/collections/" + itoa(id)

	// Another user cannot see, read or modify it.
	if hasID(h.items(h.do(&h.other, "GET", "/api/collections", nil)), id) {
		t.Error("playlist leaked into another user's list")
	}
	for _, c := range []struct{ m, p string }{
		{"GET", base}, {"DELETE", base}, {"PATCH", base}, {"POST", base + "/items"},
	} {
		if rec := h.do(&h.other, c.m, c.p, map[string]any{"pinned": true, "mediaItemId": a}); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as other user: %d, want 404", c.m, c.p, rec.Code)
		}
	}
	// ...but the same name is free for them.
	otherID := h.createdID(&h.other, "/api/collections", map[string]string{"name": name})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, otherID) })

	// Add titles (admin sees every title). Adding twice is idempotent.
	for _, m := range []int64{a, b, c, a} {
		h.expect(h.do(&h.admin, "POST", base+"/items", map[string]int64{"mediaItemId": m}), 200)
	}
	if rec := h.do(&h.admin, "POST", base+"/items", map[string]int64{}); rec.Code != http.StatusBadRequest {
		t.Errorf("missing mediaItemId: %d", rec.Code)
	}
	// Admins pass the access check for any id, so an unknown title is a
	// silent no-op (200) rather than a 404; nothing must be inserted.
	if rec := h.do(&h.admin, "POST", base+"/items", map[string]int64{"mediaItemId": 999999999}); rec.Code >= 500 {
		t.Errorf("unknown title: %d", rec.Code)
	}
	// A non-admin without access to a title is refused.
	if rec := h.do(&h.user, "POST", "/api/collections", map[string]string{"name": uniq("U")}); rec.Code == http.StatusCreated {
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		uid := int64(m["id"].(float64))
		t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, uid) })
		if r := h.do(&h.user, "POST", "/api/collections/"+itoa(uid)+"/items", map[string]int64{"mediaItemId": a}); r.Code != http.StatusNotFound {
			t.Errorf("user adding a title outside their libraries: %d, want 404", r.Code)
		}
	}

	order := func() []int64 {
		out := h.jsonBody(h.do(&h.admin, "GET", base, nil), 200)
		var ids []int64
		for _, v := range out["items"].([]any) {
			ids = append(ids, int64(v.(map[string]any)["id"].(float64)))
		}
		if int(out["total"].(float64)) != len(ids) {
			t.Errorf("total %v != %d items", out["total"], len(ids))
		}
		return ids
	}
	eq := func(got []int64, want ...int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want %v", got, want)
			}
		}
	}
	eq(order(), a, b, c)

	h.expect(h.do(&h.admin, "POST", base+"/items/"+itoa(c)+"/move", map[string]int{"position": 0}), 200)
	eq(order(), c, a, b)
	h.expect(h.do(&h.admin, "POST", base+"/items/"+itoa(c)+"/move", map[string]int{"position": 99}), 200) // clamps
	eq(order(), a, b, c)
	if rec := h.do(&h.admin, "POST", base+"/items/"+itoa(c)+"/move", map[string]int{"position": -1}); rec.Code != http.StatusBadRequest {
		t.Errorf("negative position: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", base+"/items/999999999/move", map[string]int{"position": 0}); rec.Code != http.StatusNotFound {
		t.Errorf("move missing: %d", rec.Code)
	}

	// Pinning sorts first and is reflected in the list.
	h.expect(h.do(&h.admin, "PATCH", base, map[string]bool{"pinned": true}), 200)
	if rec := h.do(&h.admin, "PATCH", base, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("patch without pinned: %d", rec.Code)
	}
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/collections", nil)) {
		if int64(it["id"].(float64)) == id {
			if it["pinned"] != true || it["mediaCount"].(float64) != 3 {
				t.Errorf("list entry = %v", it)
			}
		}
	}

	h.expect(h.do(&h.admin, "DELETE", base+"/items/"+itoa(b), nil), 200)
	eq(order(), a, c)
	h.expect(h.do(&h.admin, "DELETE", base+"/items/"+itoa(b), nil), http.StatusNotFound)

	h.expect(h.do(&h.admin, "DELETE", base, nil), 200)
	h.expect(h.do(&h.admin, "GET", base, nil), http.StatusNotFound)
}

func TestSmartPlaylistsHaveNoManualItems(t *testing.T) {
	h := newHarness(t)
	title := h.newTitle(uniq("T"))
	id := h.createdID(&h.admin, "/api/collections", map[string]any{"name": uniq("Smart"), "kind": "SMART", "rules": map[string]any{"conditions": []any{map[string]any{"field": "favorite"}}}})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, id) })
	base := "/api/collections/" + itoa(id)
	if rec := h.do(&h.admin, "POST", base+"/items", map[string]int64{"mediaItemId": title}); rec.Code != http.StatusBadRequest {
		t.Errorf("add to smart: %d, want 400", rec.Code)
	}
	if rec := h.do(&h.admin, "POST", base+"/items/"+itoa(title)+"/move", map[string]int{"position": 0}); rec.Code != http.StatusBadRequest {
		t.Errorf("reorder smart: %d, want 400", rec.Code)
	}
	if rec := h.do(&h.admin, "DELETE", base, nil); rec.Code != http.StatusOK {
		t.Errorf("delete smart: %d, want 200", rec.Code)
	}
}

func TestUnknownPlaylistKindDefaultsToManual(t *testing.T) {
	h := newHarness(t)
	id := h.createdID(&h.admin, "/api/collections", map[string]any{"name": uniq("Weird"), "kind": "BOGUS"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, id) })
	var kind string
	h.pool.QueryRow(context.Background(), `SELECT kind FROM collection WHERE id = $1`, id).Scan(&kind)
	if kind != "MANUAL" {
		t.Errorf("kind = %q, want MANUAL", kind)
	}
}

func TestWatchLater(t *testing.T) {
	h := newHarness(t)
	title := h.newTitle(uniq("Later"))
	path := "/api/watch-later/" + itoa(title)

	out := h.jsonBody(h.do(&h.admin, "PUT", path, nil), 200)
	if out["watchLater"] != true {
		t.Errorf("add = %v", out)
	}
	h.expect(h.do(&h.admin, "PUT", path, nil), 200) // idempotent
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM watch_later WHERE user_id = $1 AND media_item_id = $2`, h.admin.id, title).Scan(&n)
	if n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
	if rec := h.do(&h.admin, "PUT", "/api/watch-later/999999999", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown title: %d, want 404", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/watch-later/0", nil); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Errorf("zero id: %d", rec.Code)
	}
	out = h.jsonBody(h.do(&h.admin, "DELETE", path, nil), 200)
	if out["watchLater"] != false {
		t.Errorf("remove = %v", out)
	}
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM watch_later WHERE user_id = $1 AND media_item_id = $2`, h.admin.id, title).Scan(&n)
	if n != 0 {
		t.Errorf("rows after remove = %d", n)
	}
}

func TestPerformerLifecycle(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(&h.admin, "POST", "/api/performers", map[string]string{"name": "   "}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rec.Code)
	}
	name := uniq("Performer")
	id := h.createdID(&h.admin, "/api/performers", map[string]any{"name": "  " + name + "  ", "aliases": []string{"Al " + name, ""}})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM performer WHERE id = $1`, id) })
	if rec := h.do(&h.admin, "POST", "/api/performers", map[string]string{"name": name}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate: %d, want 409", rec.Code)
	}
	var stored string
	var aliases int
	h.pool.QueryRow(context.Background(), `SELECT name FROM performer WHERE id = $1`, id).Scan(&stored)
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM performer_alias WHERE performer_id = $1`, id).Scan(&aliases)
	if stored != name || aliases != 1 {
		t.Errorf("stored %q with %d aliases; want trimmed name and 1 alias (blank skipped)", stored, aliases)
	}

	// An admin can open a performer with no titles; a user cannot.
	h.expect(h.do(&h.admin, "GET", "/api/performers/"+itoa(id), nil), 200)
	h.expect(h.do(&h.user, "GET", "/api/performers/"+itoa(id), nil), http.StatusNotFound)
	h.expect(h.do(&h.admin, "GET", "/api/performers/abc", nil), http.StatusBadRequest)

	// Favorites are per user.
	h.expect(h.do(&h.admin, "PUT", "/api/performers/"+itoa(id)+"/favorite", map[string]bool{"favorite": true}), 200)

	h.expect(h.do(&h.admin, "DELETE", "/api/performers/"+itoa(id), nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "GET", "/api/performers/"+itoa(id), nil), http.StatusNotFound)
}

func TestStudioLifecycle(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(&h.admin, "POST", "/api/studios", map[string]string{}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rec.Code)
	}
	name := uniq("Studio")
	id := h.createdID(&h.admin, "/api/studios", map[string]string{"name": name, "website": "https://example.test"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM studio WHERE id = $1`, id) })

	out := h.jsonBody(h.do(&h.admin, "GET", "/api/studios/"+itoa(id), nil), 200)
	if s, _ := out["name"].(string); s != name {
		t.Errorf("studio = %v", out)
	}
	h.expect(h.do(&h.user, "GET", "/api/studios/"+itoa(id), nil), http.StatusNotFound)
	if !hasID(h.items(h.do(&h.admin, "GET", "/api/studios", nil)), id) {
		t.Error("admin list should include the studio")
	}
	h.expect(h.do(&h.admin, "DELETE", "/api/studios/"+itoa(id), nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "GET", "/api/studios/"+itoa(id), nil), http.StatusNotFound)
}

func TestSeriesLifecycleAndOrdering(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(&h.admin, "POST", "/api/series", map[string]string{"name": " "}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rec.Code)
	}
	name := uniq("Series")
	id := h.createdID(&h.admin, "/api/series", map[string]string{"name": name, "description": "d"})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM series WHERE id = $1`, id) })
	base := "/api/series/" + itoa(id)

	renamed := uniq("Series renamed")
	h.expect(h.do(&h.admin, "PUT", base, map[string]string{"name": renamed}), 200)
	if rec := h.do(&h.admin, "PUT", "/api/series/999999999", map[string]string{"name": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("update missing: %d", rec.Code)
	}

	t1, t2 := h.newTitle(uniq("E1")), h.newTitle(uniq("E2"))
	out := h.jsonBody(h.do(&h.admin, "POST", base+"/media", map[string]any{"mediaItemIds": []int64{t1, t2}}), 200)
	if out["added"].(float64) != 2 {
		t.Errorf("added = %v, want 2", out["added"])
	}
	out = h.jsonBody(h.do(&h.admin, "POST", base+"/media", map[string]any{"mediaItemIds": []int64{t1}}), 200)
	if out["added"].(float64) != 0 {
		t.Errorf("re-adding a member should add 0, got %v", out["added"])
	}
	if rec := h.do(&h.admin, "POST", base+"/media", map[string]any{"mediaItemIds": []int64{}}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty add: %d", rec.Code)
	}
	position := func(id int64) (sid *int64, pos *int32) {
		h.pool.QueryRow(context.Background(), `SELECT series_id, series_position FROM media_item WHERE id = $1`, id).Scan(&sid, &pos)
		return
	}
	if sid, pos := position(t1); sid == nil || *sid != id || pos == nil || *pos != 1 {
		t.Errorf("first member position = %v/%v, want series %d at 1", sid, pos, id)
	}
	if _, pos := position(t2); pos == nil || *pos != 2 {
		t.Errorf("second member position = %v, want 2", pos)
	}

	h.expect(h.do(&h.admin, "PUT", base+"/media/order", map[string]any{"mediaItemIds": []int64{t2, t1}}), 200)
	if _, pos := position(t2); pos == nil || *pos != 1 {
		t.Errorf("after reorder t2 position = %v, want 1", pos)
	}

	h.expect(h.do(&h.admin, "DELETE", base+"/media/"+itoa(t1), nil), http.StatusNoContent)
	if sid, _ := position(t1); sid != nil {
		t.Error("removed episode should leave the series")
	}

	h.expect(h.do(&h.admin, "DELETE", base, nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "GET", base, nil), http.StatusNotFound)
}

func TestStorageLocationLifecycle(t *testing.T) {
	h := newHarness(t)
	mount := t.TempDir()
	name := uniq("Disk")

	for _, bad := range []map[string]string{
		{"name": "", "type": "LOCAL", "mountPath": mount},
		{"name": name, "type": "LOCAL", "mountPath": ""},
		{"name": name, "type": "FLOPPY", "mountPath": mount},
	} {
		if rec := h.do(&h.admin, "POST", "/api/admin/storage", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d, want 400", bad, rec.Code)
		}
	}
	id := h.createdID(&h.admin, "/api/admin/storage", map[string]string{"name": name, "type": "LOCAL", "mountPath": mount})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, id) })
	if rec := h.do(&h.admin, "POST", "/api/admin/storage", map[string]string{"name": name, "type": "LOCAL", "mountPath": mount}); rec.Code == http.StatusCreated {
		t.Error("duplicate storage name/mount must be rejected")
	}
	if !hasID(h.items(h.do(&h.admin, "GET", "/api/admin/storage", nil)), id) {
		t.Error("storage list should include the new location")
	}

	renamed := uniq("Disk renamed")
	h.expect(h.do(&h.admin, "PUT", "/api/admin/storage/"+itoa(id), map[string]string{"name": renamed, "mountPath": mount}), 200)
	if rec := h.do(&h.admin, "PUT", "/api/admin/storage/"+itoa(id), map[string]string{"name": ""}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank update: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/storage/999999999", map[string]string{"name": "x", "mountPath": "/x"}); rec.Code != http.StatusNotFound {
		t.Errorf("update missing: %d", rec.Code)
	}

	// A location with indexed files can't be deleted.
	item := h.newTitle(uniq("Indexed"))
	if _, err := h.pool.Exec(context.Background(), `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size) VALUES ($1, $2, 'a.mp4', 'a.mp4', 'mp4', 1)`, item, id); err != nil {
		t.Fatal(err)
	}
	if rec := h.do(&h.admin, "DELETE", "/api/admin/storage/"+itoa(id), nil); rec.Code != http.StatusConflict {
		t.Errorf("delete with files: %d, want 409", rec.Code)
	}
	h.pool.Exec(context.Background(), `DELETE FROM media_file WHERE storage_location_id = $1`, id)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/storage/"+itoa(id), nil), 200)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/storage/"+itoa(id), nil), http.StatusNotFound)
}

func TestScanRules(t *testing.T) {
	h := newHarness(t)
	pattern := "skip-" + uniq("dir")
	for _, bad := range []string{"", "  ", "/"} {
		if rec := h.do(&h.admin, "POST", "/api/admin/scan-rules", map[string]string{"pattern": bad}); rec.Code != http.StatusBadRequest {
			t.Errorf("pattern %q: %d, want 400", bad, rec.Code)
		}
	}
	id := h.createdID(&h.admin, "/api/admin/scan-rules", map[string]string{"pattern": pattern})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM scan_rule WHERE id = $1`, id) })
	if rec := h.do(&h.admin, "POST", "/api/admin/scan-rules", map[string]string{"pattern": pattern}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate rule: %d, want 409", rec.Code)
	}
	h.expect(h.do(&h.admin, "POST", "/api/admin/scan-rules/test", map[string]string{"pattern": pattern}), 200)
	if rec := h.do(&h.admin, "POST", "/api/admin/scan-rules/test", map[string]string{"pattern": ""}); rec.Code != http.StatusBadRequest {
		t.Errorf("test empty: %d", rec.Code)
	}
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/scan-rules/"+itoa(id), nil), 200)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/scan-rules/"+itoa(id), nil), http.StatusNotFound)
	h.expect(h.do(&h.admin, "DELETE", "/api/admin/scan-rules/nope", nil), http.StatusBadRequest)
	if rec := h.do(&h.admin, "POST", "/api/admin/scan-rules", map[string]any{"pattern": "x", "libraryId": 999999999}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown library: %d, want 400", rec.Code)
	}
}

// A path rule is relative to the library folder, not the disk root.
func TestScanRulesResolveAgainstLibraryFolders(t *testing.T) {
	h := newHarness(t)
	disk := h.storage(t.TempDir())
	lib := h.library(map[int64]string{disk: "Movies"})
	h.addFile(h.newTitle(uniq("Old")), disk, "Movies/Old/a.mkv")
	matches := func(body map[string]any) float64 {
		t.Helper()
		return h.jsonBody(h.do(&h.admin, "POST", "/api/admin/scan-rules/test", body), 200)["matches"].(float64)
	}
	if n := matches(map[string]any{"pattern": "Old/a.mkv", "libraryId": lib}); n != 1 {
		t.Errorf("library-relative path matched %v, want 1", n)
	}
	if n := matches(map[string]any{"pattern": "Movies/Old", "libraryId": lib}); n != 0 {
		t.Errorf("disk-relative path matched %v, want 0", n)
	}
	id := h.createdID(&h.admin, "/api/admin/scan-rules", map[string]any{"pattern": "Old", "libraryId": lib})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM scan_rule WHERE id = $1`, id) })
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/admin/scan-rules", nil)) {
		if int64(it["id"].(float64)) == id && (it["libraryName"] == nil || int64(it["libraryId"].(float64)) != lib) {
			t.Errorf("listed rule = %v", it)
		}
	}
}

func TestWebhooksCRUD(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []map[string]any{
		{"url": "ftp://x.test/hook"},
		{"url": "not a url"},
		{"url": "https://hooks.test/x", "events": []string{"made.up"}},
	} {
		if rec := h.do(&h.admin, "POST", "/api/admin/webhooks", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d, want 400", bad, rec.Code)
		}
	}
	created := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/webhooks", map[string]any{
		"url": "https://hooks.test/reelstash", "events": []string{"scan.completed", "scan.completed", "job.failed"},
	}), http.StatusCreated)
	id := int64(created["id"].(float64))
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM webhook WHERE id = $1`, id) })
	if created["name"] != "hooks.test" {
		t.Errorf("default name = %v, want the host", created["name"])
	}
	if ev, _ := created["events"].([]any); len(ev) != 2 {
		t.Errorf("events should be de-duplicated, got %v", created["events"])
	}
	if _, leaked := created["secret"]; leaked && created["secret"] != "" && created["secret"] != nil {
		t.Errorf("secret must not be echoed back: %v", created["secret"])
	}

	list := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/webhooks", nil), 200)
	if k, _ := list["kinds"].([]any); len(k) == 0 {
		t.Error("list should describe the subscribable event kinds")
	}

	base := "/api/admin/webhooks/" + itoa(id)
	h.expect(h.do(&h.admin, "PUT", base, map[string]any{"enabled": false, "name": "Renamed"}), 200)
	var enabled bool
	var name string
	h.pool.QueryRow(context.Background(), `SELECT enabled, name FROM webhook WHERE id = $1`, id).Scan(&enabled, &name)
	if enabled || name != "Renamed" {
		t.Errorf("update not applied: enabled=%v name=%q", enabled, name)
	}
	if rec := h.do(&h.admin, "PUT", base, map[string]any{"url": "gopher://x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad url update: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/webhooks/0", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("zero id: %d", rec.Code)
	}
	h.expect(h.do(&h.admin, "DELETE", base, nil), http.StatusNoContent)
	h.expect(h.do(&h.admin, "DELETE", base, nil), http.StatusNotFound)
}

func TestUserManagement(t *testing.T) {
	h := newHarness(t)
	list := h.items(h.do(&h.admin, "GET", "/api/admin/users", nil))
	if !hasID(list, h.user.id) || !hasID(list, h.admin.id) {
		t.Error("user list should include the test accounts")
	}

	if rec := h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.user.id), map[string]any{"role": "SUPERUSER"}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad role: %d", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.admin.id), map[string]any{"role": "USER"}); rec.Code != http.StatusBadRequest {
		t.Errorf("self demotion: %d, want 400", rec.Code)
	}
	if rec := h.do(&h.admin, "PUT", "/api/admin/users/999999999", map[string]any{"role": "USER"}); rec.Code != http.StatusNotFound {
		t.Errorf("missing user: %d", rec.Code)
	}

	// Disabling a user locks them out immediately.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.user.id), map[string]any{"role": "USER", "disabled": true}), 200)
	h.expect(h.do(&h.user, "GET", "/api/me", nil), http.StatusForbidden)
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.user.id), map[string]any{"role": "USER", "disabled": false}), 200)
	h.expect(h.do(&h.user, "GET", "/api/me", nil), 200)

	// Promotion takes effect on the next request.
	h.expect(h.do(&h.admin, "PUT", "/api/admin/users/"+itoa(h.other.id), map[string]any{"role": "ADMIN"}), 200)
	h.expect(h.do(&h.other, "GET", "/api/admin/users", nil), 200)
}

func TestNotificationsAdminOnlyVisibilityAndReadState(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	pub, priv := uniq("public"), uniq("admin-only")
	var pubID, privID int64
	if err := h.pool.QueryRow(ctx, `INSERT INTO notification (kind, title, admin_only) VALUES ('job.completed', $1, false) RETURNING id`, pub).Scan(&pubID); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(ctx, `INSERT INTO notification (kind, title, admin_only) VALUES ('job.failed', $1, true) RETURNING id`, priv).Scan(&privID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(ctx, `DELETE FROM notification WHERE id = ANY($1)`, []int64{pubID, privID}) })

	titles := func(who *actor) map[string]bool {
		out := map[string]bool{}
		for _, it := range h.items(h.do(who, "GET", "/api/notifications?limit=200", nil)) {
			out[it["title"].(string)] = true
		}
		return out
	}
	if u := titles(&h.user); !u[pub] || u[priv] {
		t.Errorf("user sees %v; want the public one only", u)
	}
	if a := titles(&h.admin); !a[pub] || !a[priv] {
		t.Errorf("admin should see both, got %v", a)
	}

	unread := func(who *actor) float64 {
		return h.jsonBody(h.do(who, "GET", "/api/notifications", nil), 200)["unread"].(float64)
	}
	if unread(&h.user) < 1 {
		t.Error("expected unread notifications")
	}
	h.expect(h.do(&h.user, "POST", "/api/notifications/read", nil), 200)
	if n := unread(&h.user); n != 0 {
		t.Errorf("unread after mark-read = %v", n)
	}
	// Read state is per user.
	if unread(&h.other) < 1 {
		t.Error("marking one user's notifications read must not affect another's")
	}
}

func TestSearchFindsEntitiesAndScopesForUsers(t *testing.T) {
	h := newHarness(t)
	word := "zq" + uniq("srch")[5:]
	title := h.newTitle("Findable " + word)
	studioID := h.createdID(&h.admin, "/api/studios", map[string]string{"name": "Studio " + word})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM studio WHERE id = $1`, studioID) })

	if out := h.items(h.do(&h.admin, "GET", "/api/search", nil)); len(out) != 0 {
		t.Errorf("empty query should return nothing, got %v", out)
	}

	types := map[string]bool{}
	for _, it := range h.items(h.do(&h.admin, "GET", "/api/search?q="+word, nil)) {
		types[it["type"].(string)] = true
	}
	if len(types) < 2 {
		t.Errorf("admin search for %q found types %v; want the title and the studio", word, types)
	}
	// Users only find what sits in their libraries: this title and studio
	// belong to none.
	if got := h.items(h.do(&h.user, "GET", "/api/search?q="+word, nil)); hasType(got, "studio") {
		t.Errorf("user must not find a studio with no visible titles: %v", got)
	}
	_ = title
}

func TestSearchRanksAndToleratesTypos(t *testing.T) {
	h := newHarness(t)
	word := "zq" + uniq("rank")[5:]
	exact := h.newTitle(word)
	other := h.newTitle(word + " extended cut")
	studioID := h.createdID(&h.admin, "/api/studios", map[string]string{"name": "Studio " + word})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM studio WHERE id = $1`, studioID) })

	items := h.items(h.do(&h.admin, "GET", "/api/search?q="+word, nil))
	if len(items) == 0 || int64(items[0]["id"].(float64)) != exact || items[0]["type"] != "media_item" {
		t.Errorf("exact title should rank first, got %v", items)
	}
	// Type-ahead: a prefix finds the title and the studio.
	prefix := word[:len(word)-2]
	got := h.items(h.do(&h.admin, "GET", "/api/search?q="+prefix, nil))
	if !hasID(got, other) || !hasType(got, "studio") {
		t.Errorf("prefix %q should find the title and studio, got %v", prefix, got)
	}
	// A dropped letter still matches through trigram similarity.
	typo := word[:3] + word[4:]
	if got := h.items(h.do(&h.admin, "GET", "/api/search?q="+typo, nil)); !hasID(got, exact) {
		t.Errorf("typo %q should still find the title, got %v", typo, got)
	}
}

func hasType(items []map[string]any, typ string) bool {
	for _, it := range items {
		if it["type"] == typ {
			return true
		}
	}
	return false
}

func TestSeriesBackdrop(t *testing.T) {
	h := newHarness(t)
	id := h.createdID(&h.admin, "/api/series", map[string]string{"name": uniq("Series")})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM series WHERE id = $1`, id) })
	base := "/api/series/" + itoa(id)

	if rec := h.do(&h.admin, "GET", base+"/backdrop", nil); rec.Code != http.StatusNotFound {
		t.Errorf("backdrop before upload: %d, want 404", rec.Code)
	}
	if got := h.jsonBody(h.do(&h.admin, "GET", base, nil), 200)["backdropImageId"]; got != nil {
		t.Errorf("backdropImageId = %v, want none", got)
	}

	var artworkID int64
	if err := h.pool.QueryRow(context.Background(), `INSERT INTO artwork (kind, storage_key, mime_type, custom) VALUES ('BACKDROP', 'missing.jpg', 'image/jpeg', true) RETURNING id`).Scan(&artworkID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM artwork WHERE id = $1`, artworkID) })
	h.pool.Exec(context.Background(), `UPDATE series SET backdrop_image_id = $2 WHERE id = $1`, id, artworkID)
	if got := h.jsonBody(h.do(&h.admin, "GET", base, nil), 200)["backdropImageId"]; got != float64(artworkID) {
		t.Errorf("backdropImageId = %v, want %d", got, artworkID)
	}

	if rec := h.do(&h.user, "DELETE", base+"/backdrop", nil); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin delete: %d, want 403", rec.Code)
	}
	h.expect(h.do(&h.admin, "DELETE", base+"/backdrop", nil), 204)
	var left *int64
	h.pool.QueryRow(context.Background(), `SELECT backdrop_image_id FROM series WHERE id = $1`, id).Scan(&left)
	if left != nil {
		t.Errorf("backdrop_image_id = %v after delete, want NULL", *left)
	}
	var n int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM artwork WHERE id = $1`, artworkID).Scan(&n)
	if n != 0 {
		t.Errorf("artwork row survived delete")
	}
}

package httpapi

import (
	"context"
	"net/http"
	"sort"
	"testing"
)

func TestUpdatePerformerAliases(t *testing.T) {
	h := newHarness(t)
	name := uniq("Alias Person")
	id := h.createdID(&h.admin, "/api/performers", map[string]string{"name": name})
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM performer WHERE id = $1`, id) })
	path := "/api/performers/" + itoa(id)

	aliases := func() []string {
		rows, err := h.pool.Query(context.Background(), `SELECT alias FROM performer_alias WHERE performer_id = $1`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var a string
			rows.Scan(&a)
			out = append(out, a)
		}
		sort.Strings(out)
		return out
	}

	// Blanks, repeats and the person's own name are dropped.
	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"name": name, "aliases": []string{" Ann B ", "ann b", "", name, "Zed"}}), http.StatusOK)
	if got := aliases(); len(got) != 2 || got[0] != "Ann B" || got[1] != "Zed" {
		t.Fatalf("aliases = %v", got)
	}
	// Leaving aliases out keeps them.
	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"name": name, "notes": "n"}), http.StatusOK)
	if got := aliases(); len(got) != 2 {
		t.Fatalf("aliases after edit without them = %v", got)
	}
	// An empty list clears them.
	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"name": name, "aliases": []string{}}), http.StatusOK)
	if got := aliases(); len(got) != 0 {
		t.Fatalf("aliases after clearing = %v", got)
	}
}

func TestUpdateMediaReleaseDate(t *testing.T) {
	h := newHarness(t)
	id := h.newTitle(uniq("Dated"))
	path := "/api/media/" + itoa(id) + "/metadata"
	read := func() (date *string, year *int) {
		if err := h.pool.QueryRow(context.Background(), `SELECT to_char(release_date, 'YYYY-MM-DD'), production_year FROM media_item WHERE id = $1`, id).Scan(&date, &year); err != nil {
			t.Fatal(err)
		}
		return
	}

	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"releaseDate": "14/09/2023"}), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"releaseDate": "2023-09-14"}), http.StatusOK)
	if date, year := read(); date == nil || *date != "2023-09-14" || year == nil || *year != 2023 {
		t.Fatalf("date=%v year=%v", date, year)
	}
	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"title": "Still dated"}), http.StatusOK)
	if date, _ := read(); date == nil {
		t.Fatal("an edit without releaseDate cleared it")
	}
	h.expect(h.do(&h.admin, "PUT", path, map[string]any{"releaseDate": ""}), http.StatusOK)
	if date, year := read(); date != nil || year != nil {
		t.Fatalf("after clearing: date=%v year=%v", date, year)
	}
}

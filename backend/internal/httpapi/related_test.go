package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// An unmatched title (no people, studio or tags) still gets suggestions: titles
// sharing metadata first, then the rest of its own library, unfinished first.
func TestRelatedFillsFromTheSameLibrary(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	libA, libB := h.library(nil), h.library(nil)
	var studio int64
	if err := h.pool.QueryRow(ctx, `INSERT INTO studio (name) VALUES ($1) RETURNING id`, uniq("Studio")).Scan(&studio); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM studio WHERE id = $1`, studio) })
	in := func(lib int64, title string) int64 {
		id := h.newTitle(uniq(title))
		h.pool.Exec(ctx, `UPDATE media_item SET library_id = $2 WHERE id = $1`, id, lib)
		return id
	}
	src := in(libA, "Unmatched")
	finished := in(libA, "Finished")
	fresh := in(libA, "Fresh")
	sameStudio := in(libA, "Same studio")
	other := in(libB, "Other library")
	h.pool.Exec(ctx, `UPDATE media_item SET studio_id = $2 WHERE id = ANY($1)`, []int64{src, sameStudio}, studio)
	h.finish(&h.admin, finished)

	var out struct {
		Items []struct {
			ID     int64   `json:"id"`
			Reason *string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal(h.expect(h.do(&h.admin, "GET", "/api/media/"+itoa(src)+"/related", nil), http.StatusOK), &out); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	if len(ids) != 3 || ids[0] != sameStudio || ids[1] != fresh || ids[2] != finished {
		t.Fatalf("related = %v, want [same studio %d, fresh %d, finished %d]", ids, sameStudio, fresh, finished)
	}
	if out.Items[0].Reason != nil || out.Items[1].Reason == nil {
		t.Errorf("only filled-in titles carry a reason: %+v", out.Items)
	}
	for _, id := range ids {
		if id == src || id == other {
			t.Errorf("related must not include the title itself or another library's titles: %v", ids)
		}
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func callPhoto(h http.HandlerFunc, method string, id, photoID int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", strconv.FormatInt(id, 10))
	if photoID != 0 {
		rc.URLParams.Add("photoId", strconv.FormatInt(photoID, 10))
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestPerformerPhotoGallery(t *testing.T) {
	a, pool := unmatchAPI(t)
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, "Photo "+uuid.NewString()[:8]).Scan(&id); err != nil {
		t.Fatal(err)
	}
	add := func(source string) (int64, string) {
		key := uuid.NewString() + ".jpg"
		if err := os.WriteFile(filepath.Join(a.Cfg.PosterDir, key), []byte("img"), 0o644); err != nil {
			t.Fatal(err)
		}
		var art int64
		if err := pool.QueryRow(ctx, `INSERT INTO artwork (kind, storage_key, mime_type) VALUES ('PROFILE', $1, 'image/jpeg') RETURNING id`, key).Scan(&art); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO performer_photo (performer_id, artwork_id, source) VALUES ($1, $2, $3)`, id, art, source); err != nil {
			t.Fatal(err)
		}
		return art, key
	}
	pd, _ := add("x-alpha")
	tm, tmKey := add("tmdb")
	pool.Exec(ctx, `UPDATE performer SET profile_image_id = $2 WHERE id = $1`, id, pd)

	// Choosing a photo shows it and marks it as picked by hand.
	if rec := callPhoto(a.choosePerformerPhoto, http.MethodPut, id, tm); rec.Code != 200 {
		t.Fatalf("choose: %d %s", rec.Code, rec.Body)
	}
	var shown int64
	var chosen bool
	pool.QueryRow(ctx, `SELECT profile_image_id, photo_chosen FROM performer WHERE id = $1`, id).Scan(&shown, &chosen)
	if shown != tm || !chosen {
		t.Errorf("after choose: shown %d chosen %v", shown, chosen)
	}

	// The list puts the displayed photo first.
	rec := callPhoto(a.listPerformerPhotos, http.MethodGet, id, 0)
	var list struct{ Items []performerPhoto }
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Items) != 2 || list.Items[0].ID != tm || !list.Items[0].Primary || list.Items[1].Source != "x-alpha" {
		t.Errorf("list = %+v", list.Items)
	}

	// Another performer's photo cannot be chosen.
	if rec := callPhoto(a.choosePerformerPhoto, http.MethodPut, id+1_000_000, tm); rec.Code != http.StatusNotFound {
		t.Errorf("foreign choose = %d", rec.Code)
	}

	// Deleting the displayed photo removes its file and falls back to the next one.
	if rec := callPhoto(a.deletePerformerPhoto, http.MethodDelete, id, tm); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	pool.QueryRow(ctx, `SELECT profile_image_id, photo_chosen FROM performer WHERE id = $1`, id).Scan(&shown, &chosen)
	if shown != pd || chosen {
		t.Errorf("after delete: shown %d chosen %v", shown, chosen)
	}
	if _, err := os.Stat(filepath.Join(a.Cfg.PosterDir, tmKey)); !os.IsNotExist(err) {
		t.Error("deleted photo's file should be gone")
	}

	// Removing the last photo leaves the performer without one.
	if rec := callPhoto(a.deletePerformerImage, http.MethodDelete, id, 0); rec.Code != http.StatusNoContent {
		t.Fatalf("delete displayed: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM performer WHERE id = $1 AND profile_image_id IS NULL`, id); n != 1 {
		t.Error("performer should have no photo left")
	}
}

func TestMergeKeepsBothGalleries(t *testing.T) {
	a, pool := unmatchAPI(t)
	ctx := context.Background()
	newPerformer := func() int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, "M "+uuid.NewString()[:8]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	add := func(performer int64, source, url string, custom bool) int64 {
		var art int64
		pool.QueryRow(ctx, `INSERT INTO artwork (kind, storage_key, mime_type, custom) VALUES ('PROFILE', $1, 'image/jpeg', $2) RETURNING id`, uuid.NewString()+".jpg", custom).Scan(&art)
		if _, err := pool.Exec(ctx, `INSERT INTO performer_photo (performer_id, artwork_id, source, source_url) VALUES ($1, $2, $3, NULLIF($4, ''))`, performer, art, source, url); err != nil {
			t.Fatal(err)
		}
		return art
	}
	target, source := newPerformer(), newPerformer()
	targetPD := add(target, "x-alpha", "https://x/same.jpg", false)
	add(source, "x-alpha", "https://x/same.jpg", false) // duplicate of the target's
	sourceUpload := add(source, "upload", "", true)
	pool.Exec(ctx, `UPDATE performer SET profile_image_id = $2 WHERE id = $1`, target, targetPD)
	pool.Exec(ctx, `UPDATE performer SET profile_image_id = $2, photo_chosen = true WHERE id = $1`, source, sourceUpload)

	req := httptest.NewRequest(http.MethodPost, "/", stringsReader(`{"sourceId":`+strconv.FormatInt(source, 10)+`}`))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", strconv.FormatInt(target, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	a.mergePerformer(rec, req)
	if rec.Code != 200 {
		t.Fatalf("merge: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM performer_photo WHERE performer_id = $1`, target); n != 2 {
		t.Errorf("gallery should hold the target's photo and the upload, got %d", n)
	}
	var shown int64
	var chosen bool
	pool.QueryRow(ctx, `SELECT profile_image_id, photo_chosen FROM performer WHERE id = $1`, target).Scan(&shown, &chosen)
	if shown != sourceUpload || !chosen {
		t.Errorf("the hand-picked upload should show: shown %d chosen %v", shown, chosen)
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }

func TestPerformerFieldSources(t *testing.T) {
	a, pool := unmatchAPI(t)
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO performer (name, match_status) VALUES ($1, 'matched') RETURNING id`, "F "+uuid.NewString()[:8]).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pool.Exec(ctx, `INSERT INTO match_link (kind, entity_id, provider, external_id, remote_metadata, matched_at)
		VALUES ('performer', $1, 'x-alpha', $2, '{"provider":"x-alpha","bio":"provider bio","height":"173cm"}', now() - interval '1 day')`, id, "pd-"+uuid.NewString())
	pool.Exec(ctx, `INSERT INTO match_link (kind, entity_id, provider, external_id, remote_metadata)
		VALUES ('performer', $1, 'tmdb', $2, '{"provider":"tmdb","bio":"TMDB bio","height":"175cm","birthplace":"Caracas"}')`, id, "tm-"+uuid.NewString())

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", strconv.FormatInt(id, 10))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		a.setPerformerFieldSources(rec, req)
		return rec
	}
	meta := func() map[string]string {
		var raw []byte
		pool.QueryRow(ctx, `SELECT remote_metadata FROM performer WHERE id = $1`, id).Scan(&raw)
		out := map[string]string{}
		var any map[string]any
		json.Unmarshal(raw, &any)
		for k, v := range any {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	}

	// Bio from provider; the rest from the newest match (TMDB).
	if rec := put(`{"sources":{"bio":"x-alpha","nope":"tmdb"}}`); rec.Code != 200 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	m := meta()
	if m["bio"] != "provider bio" || m["height"] != "175cm" || m["birthplace"] != "Caracas" {
		t.Errorf("metadata = %v", m)
	}
	// Clearing the choice goes back to the newest match.
	put(`{"sources":{}}`)
	if m := meta(); m["bio"] != "TMDB bio" {
		t.Errorf("after clear bio = %q", m["bio"])
	}
	// The GET view lists both providers' values.
	rec := callPhoto(a.performerFieldSources, http.MethodGet, id, 0)
	var view struct {
		Providers []string
		Values    map[string]map[string]string
	}
	json.Unmarshal(rec.Body.Bytes(), &view)
	if len(view.Providers) != 2 || view.Values["x-alpha"]["height"] != "173cm" {
		t.Errorf("view = %s", rec.Body)
	}
}

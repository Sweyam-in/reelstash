package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// Reels are the selected library's portrait videos: taller than wide, or a
// landscape frame a phone flagged as rotated a quarter turn.
func TestReelsListPortraitVideosOfSelectedLibrary(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	video := func(title, rel string, width, height int, rotation *int) int64 {
		t.Helper()
		var id int64
		if err := f.pool.QueryRow(ctx, `INSERT INTO media_item (title) VALUES ($1) RETURNING id`, uniq(title)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size, width, height, rotation)
			VALUES ($1, $2, $3, 'x.mp4', 'mp4', 1, $4, $5, $6)`, id, f.disk, rel, width, height, rotation); err != nil {
			t.Fatal(err)
		}
		return id
	}
	quarter, none := 90, 0
	vertical := video("Vertical", "movies/vertical.mp4", 1080, 1920, nil)
	rotated := video("Phone", "movies/phone.mov", 1920, 1080, &quarter)
	wide := video("Wide", "movies/wide.mp4", 1920, 1080, &none)
	square := video("Square", "movies/square.mp4", 1080, 1080, nil)
	otherLib := video("Other vertical", "private/vertical.mp4", 720, 1280, nil)
	// A title whose best file is landscape plays landscape, so it is no reel.
	mixed := video("Mixed", "movies/mixed-small.mp4", 480, 640, nil)
	if _, err := f.pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size, width, height)
		VALUES ($1, $2, 'movies/mixed-big.mp4', 'y.mp4', 'mp4', 1, 3840, 2160)`, mixed, f.disk); err != nil {
		t.Fatal(err)
	}

	header := strconv.FormatInt(f.movies, 10)
	ids := itemIDs(t, f.get(t, f.adminCookie, "/api/media?reels=true&sort=shuffle&seed=abc&limit=100", "X-Library", header))
	if !ids[vertical] || !ids[rotated] {
		t.Fatalf("portrait videos missing from reels: %v", ids)
	}
	for name, id := range map[string]int64{"wide": wide, "square": square, "other library": otherLib, "landscape best file": mixed} {
		if ids[id] {
			t.Fatalf("%s video listed as a reel", name)
		}
	}
	// Reels stay in the library as well.
	if all := itemIDs(t, f.get(t, f.adminCookie, "/api/media?limit=200", "X-Library", header)); !all[vertical] || !all[wide] {
		t.Fatalf("library listing lost titles: %v", all)
	}

	// An admin's override wins over the detection, both ways; null goes back to automatic.
	setReel := func(id int64, override string) {
		t.Helper()
		rec := f.do(t, f.adminCookie, http.MethodPut, "/api/media/"+strconv.FormatInt(id, 10)+"/reel", `{"override":`+override+`}`, "X-Library", header)
		if rec.Code != http.StatusOK {
			t.Fatalf("set reel %s: %d %s", override, rec.Code, rec.Body.String())
		}
		var body struct {
			Reel         *bool `json:"reel"`
			ReelOverride *bool `json:"reelOverride"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Reel == nil {
			t.Fatalf("set reel response: %s", rec.Body.String())
		}
		if (override == "null") != (body.ReelOverride == nil) {
			t.Fatalf("override %s echoed as %v", override, body.ReelOverride)
		}
	}
	setReel(wide, "true")
	setReel(vertical, "false")
	ids = itemIDs(t, f.get(t, f.adminCookie, "/api/media?reels=true&limit=100", "X-Library", header))
	if !ids[wide] || ids[vertical] {
		t.Fatalf("overrides ignored: %v", ids)
	}
	setReel(vertical, "null")
	if !itemIDs(t, f.get(t, f.adminCookie, "/api/media?reels=true&limit=100", "X-Library", header))[vertical] {
		t.Fatal("clearing the override did not restore detection")
	}
	if rec := f.do(t, f.userCookie, http.MethodPut, "/api/media/"+strconv.FormatInt(wide, 10)+"/reel", `{"override":null}`, "X-Library", header); rec.Code != http.StatusForbidden {
		t.Fatalf("user set a reel override: %d", rec.Code)
	}

	if !itemIDs(t, f.get(t, f.adminCookie, "/api/media?reels=true", "X-Library", strconv.FormatInt(f.private, 10)))[otherLib] {
		t.Fatal("private library reel missing")
	}
	if rec := f.get(t, f.userCookie, "/api/media?reels=true", "X-Library", strconv.FormatInt(f.private, 10)); rec.Code == http.StatusOK && itemIDs(t, rec)[otherLib] {
		t.Fatal("user without access sees another library's reels")
	}
}

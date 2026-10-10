package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/config"
	"reelstash/backend/internal/downloads"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/qbit"
	"reelstash/backend/internal/qbit/qbittest"
	"reelstash/backend/internal/testdb"
)

type downloadsRig struct {
	*harness
	fake  *qbittest.Server
	svc   *downloads.Service
	mount string
	loc   int64
}

// newDownloadsRig runs the API with a Downloads service talking to a fake
// qBittorrent, over a disk with a "Movies" library folder.
func newDownloadsRig(t *testing.T) downloadsRig {
	t.Helper()
	pool := testdb.Isolated(t)
	h := newHarnessOn(t, pool, config.Config{}, nil)
	fake := qbittest.New()
	t.Cleanup(fake.Close)
	mount := t.TempDir()
	fake.Prefs["save_path"] = filepath.Join(mount, "downloads")
	for _, rel := range []string{"Movies/a.mp4", "downloads/incomplete/half.mkv", "downloads/done.mkv"} {
		p := filepath.Join(mount, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var loc int64
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ('Disk', 'LOCAL', $1) RETURNING id`, mount).Scan(&loc); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		WITH l AS (INSERT INTO library (name) VALUES ('Movies') RETURNING id)
		INSERT INTO library_root (library_id, storage_location_id, path) SELECT l.id, $1, 'Movies' FROM l`, loc); err != nil {
		t.Fatal(err)
	}
	svc := downloads.New(pool, nil, qbit.Config{URL: fake.URL, Username: "admin", Password: "adminadmin"}, nil)
	t.Cleanup(func() { pathguard.SetInUseSource(nil) })
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Run(runCtx)
	h.handler = NewRouter(pool, h.cfg, nil, nil, nil, nil, nil, WithPlugins(nil, h.authPlugins), WithDownloads(svc))
	return downloadsRig{harness: h, fake: fake, svc: svc, mount: mount, loc: loc}
}

// connected waits for the first sync.
func (d downloadsRig) connected(t *testing.T) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap := d.jsonBody(d.do(&d.admin, "GET", "/api/downloads", nil), 200)
		if snap["connected"] == true {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("never connected: %v", snap)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDownloadsWorkspaceAPI(t *testing.T) {
	d := newDownloadsRig(t)
	d.fake.AddTorrent("h1", map[string]any{"name": "Film", "save_path": filepath.Join(d.mount, "Movies")})

	snap := d.connected(t)
	torrents, _ := snap["torrents"].([]any)
	if len(torrents) != 1 {
		t.Fatalf("torrents = %v", snap["torrents"])
	}
	first := torrents[0].(map[string]any)
	if lib, _ := first["library"].(map[string]any); lib["name"] != "Movies" {
		t.Fatalf("torrent library = %v", first["library"])
	}
	if snap["version"] != "v5.0.2" {
		t.Fatalf("version = %v", snap["version"])
	}

	// Actions reach qBittorrent.
	d.expect(d.do(&d.admin, "POST", "/api/downloads/torrents/action", map[string]any{"hashes": []string{"h1"}, "action": "stop"}), 200)
	d.expect(d.do(&d.admin, "POST", "/api/downloads/torrents/action", map[string]any{"hashes": []string{"h1"}, "action": "explode"}), 400)
	d.expect(d.do(&d.admin, "POST", "/api/downloads/torrents/action", map[string]any{"hashes": []string{}, "action": "stop"}), 400)
	if calls := d.fake.CallLog(); len(calls) == 0 || calls[0] != "torrents/stop h1" {
		t.Fatalf("calls = %v", calls)
	}

	// Only whitelisted qBittorrent settings can be changed.
	d.expect(d.do(&d.admin, "PUT", "/api/downloads/preferences", map[string]any{"autorun_enabled": true}), 400)
	d.expect(d.do(&d.admin, "PUT", "/api/downloads/preferences", map[string]any{"dl_limit": "fast"}), 400)
	prefs := d.jsonBody(d.do(&d.admin, "PUT", "/api/downloads/preferences", map[string]any{"dl_limit": 1024}), 200)
	if v := prefs["values"].(map[string]any)["dl_limit"]; v != float64(1024) {
		t.Fatalf("dl_limit = %v", v)
	}

	// Adding a magnet link with a save folder.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("urls", "magnet:?xt=urn:btih:abc")
	mw.WriteField("storageLocationId", itoa(d.loc))
	mw.WriteField("path", "downloads")
	mw.WriteField("category", "movies")
	mw.Close()
	req := httptest.NewRequest("POST", "/api/downloads/add", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: d.admin.cookie})
	rec := httptest.NewRecorder()
	d.handler.ServeHTTP(rec, req)
	d.expect(rec, 200)
	if len(d.fake.Added) != 1 || d.fake.Added[0]["savepath"] != filepath.Join(d.mount, "downloads") || d.fake.Added[0]["category"] != "movies" {
		t.Fatalf("added = %v", d.fake.Added)
	}

	// Non-admins never reach qBittorrent.
	d.expect(d.do(&d.user, "GET", "/api/downloads", nil), 403)
}

func TestDownloadsFoldersAndRules(t *testing.T) {
	d := newDownloadsRig(t)
	d.connected(t)

	// The in-progress folder can't be a library folder.
	d.expect(d.do(&d.admin, "PUT", "/api/downloads/folders", map[string]any{"incomplete": map[string]any{"storageLocationId": d.loc, "path": "Movies"}}), 400)
	out := d.jsonBody(d.do(&d.admin, "PUT", "/api/downloads/folders", map[string]any{
		"incomplete": map[string]any{"storageLocationId": d.loc, "path": "downloads/incomplete"},
		"savePath":   map[string]any{"storageLocationId": d.loc, "path": "downloads"},
	}), 200)
	if out["warning"] != "" {
		t.Fatalf("warning = %v", out["warning"])
	}
	inc := filepath.Join(d.mount, "downloads", "incomplete")
	if d.fake.Prefs["temp_path"] != inc || d.fake.Prefs["temp_path_enabled"] != true || d.fake.Prefs["save_path"] != filepath.Join(d.mount, "downloads") {
		t.Fatalf("qBittorrent prefs = %v", d.fake.Prefs)
	}
	if !pathguard.Load(context.Background(), d.pool).Locked(filepath.Join(inc, "half.mkv")) {
		t.Fatal("in-progress folder is not locked")
	}

	// A library folder can't be placed inside it; holding it is fine.
	var libID int64
	d.pool.QueryRow(context.Background(), `SELECT id FROM library WHERE name = 'Movies'`).Scan(&libID)
	d.expect(d.do(&d.admin, "PUT", "/api/admin/libraries/"+itoa(libID), map[string]any{
		"name": "Movies", "mode": "entertainment", "roots": []map[string]any{{"storageLocationId": d.loc, "path": "downloads/incomplete/x"}},
	}), 400)
	d.expect(d.do(&d.admin, "PUT", "/api/admin/libraries/"+itoa(libID), map[string]any{
		"name": "Movies", "mode": "entertainment", "roots": []map[string]any{{"storageLocationId": d.loc, "path": "Movies"}, {"storageLocationId": d.loc, "path": "downloads"}},
	}), 200)

	// Rules: validation, and a destination inside the locked folder is refused.
	d.expect(d.do(&d.admin, "POST", "/api/downloads/rules", map[string]any{"name": "", "destStorageLocationId": d.loc}), 400)
	d.expect(d.do(&d.admin, "POST", "/api/downloads/rules", map[string]any{"name": "Bad", "destStorageLocationId": d.loc, "destPath": "downloads/incomplete/x"}), 400)
	id := d.createdID(&d.admin, "/api/downloads/rules", map[string]any{"name": "Movies", "enabled": true, "matchCategory": "movies", "destStorageLocationId": d.loc, "destPath": "Movies", "scanAfter": true})
	d.createdID(&d.admin, "/api/downloads/rules", map[string]any{"name": "Other", "enabled": true, "destStorageLocationId": d.loc, "destPath": "Other"})
	rules := d.items(d.do(&d.admin, "GET", "/api/downloads/rules", nil))
	if len(rules) != 2 || rules[0]["name"] != "Movies" || rules[0]["destStorageLocation"] != "Disk" {
		t.Fatalf("rules = %v", rules)
	}

	// Dry run over finished torrents.
	d.fake.AddTorrent("done", map[string]any{"name": "Film", "category": "movies"})
	d.fake.Complete("done", time.Now().Unix())
	d.svc.Kick()
	deadline := time.Now().Add(3 * time.Second)
	var plans []map[string]any
	for time.Now().Before(deadline) {
		plans = d.items(d.do(&d.admin, "GET", "/api/downloads/rules/test", nil))
		if len(plans) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(plans) != 1 || plans[0]["ruleId"] != float64(id) || plans[0]["dest"] != filepath.Join(d.mount, "Movies") {
		t.Fatalf("plans = %v", plans)
	}
	// Auto-move is off, so nothing moved.
	for _, c := range d.fake.CallLog() {
		if strings.HasPrefix(c, "setLocation") {
			t.Fatalf("moved with auto-move off: %v", d.fake.CallLog())
		}
	}

	d.expect(d.do(&d.admin, "PUT", "/api/downloads/rules/order", map[string]any{"ids": []int64{id + 1, id}}), 200)
	d.expect(d.do(&d.admin, "DELETE", "/api/downloads/rules/"+itoa(id), nil), 204)
	d.expect(d.do(&d.admin, "DELETE", "/api/downloads/rules/"+itoa(id), nil), 404)
}

func TestDownloadsSettings(t *testing.T) {
	d := newDownloadsRig(t)
	view := d.jsonBody(d.do(&d.admin, "GET", "/api/downloads/settings", nil), 200)
	if view["envUrl"] != d.fake.URL || view["passwordSet"] != false || view["autoMove"] != false {
		t.Fatalf("settings = %v", view)
	}
	d.expect(d.do(&d.admin, "PUT", "/api/downloads/settings", map[string]any{"url": "ftp://x"}), 400)
	d.expect(d.do(&d.admin, "PUT", "/api/downloads/settings", map[string]any{"pathFrom": "/downloads"}), 400)

	bad := d.jsonBody(d.do(&d.admin, "POST", "/api/downloads/settings/test", map[string]any{"password": "wrong"}), 200)
	if bad["ok"] != false || !strings.Contains(bad["error"].(string), "refused") {
		t.Fatalf("test with a wrong password = %v", bad)
	}
	good := d.jsonBody(d.do(&d.admin, "POST", "/api/downloads/settings/test", map[string]any{}), 200)
	if good["ok"] != true || good["version"] != "v5.0.2" {
		t.Fatalf("test = %v", good)
	}

	view = d.jsonBody(d.do(&d.admin, "PUT", "/api/downloads/settings", map[string]any{"autoMove": true, "password": "secret"}), 200)
	if view["autoMove"] != true || view["passwordSet"] != true {
		t.Fatalf("saved = %v", view)
	}
	if s := downloads.LoadSettings(context.Background(), d.pool); s.RulesSince == 0 || s.Password != "secret" {
		t.Fatalf("stored = %+v", s)
	}
	if strings.Contains(d.do(&d.admin, "GET", "/api/downloads/settings", nil).Body.String(), "secret") {
		t.Fatal("password leaked")
	}
}

func TestFilesRefuseProtectedPaths(t *testing.T) {
	f := newFileRig(t)
	ctx := context.Background()
	locked := filepath.Join(f.mount, "docs")
	if _, err := f.pool.Exec(ctx, `INSERT INTO protected_path (path) VALUES ($1)`, locked); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM protected_path WHERE path = $1`, locked) })
	pathguard.SetInUseSource(func() []pathguard.InUse {
		return []pathguard.InUse{{Path: filepath.Join(f.mount, "movies", "a.mp4"), Name: "Film"}}
	})
	t.Cleanup(func() { pathguard.SetInUseSource(nil) })

	// Locked folder: nothing changes or is added inside it.
	f.expect(f.do(&f.admin, "PUT", "/api/admin/files", map[string]any{"storageLocationId": f.loc, "path": "docs/note.txt", "newName": "x.txt"}), 409)
	f.expect(f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("docs", "recursive", "true"), nil), 409)
	f.expect(f.do(&f.admin, "POST", "/api/admin/files/dir", map[string]any{"storageLocationId": f.loc, "path": "docs", "name": "new"}), 409)
	f.expect(f.do(&f.admin, "POST", "/api/admin/files/copy", map[string]any{"storageLocationId": f.loc, "path": "root.txt", "destPath": "docs"}), 409)
	// Torrent data: no rename, move or delete, but copying it elsewhere is fine.
	f.expect(f.do(&f.admin, "PUT", "/api/admin/files", map[string]any{"storageLocationId": f.loc, "path": "movies/a.mp4", "newName": "b.mp4"}), 409)
	f.expect(f.do(&f.admin, "POST", "/api/admin/files/move", map[string]any{"storageLocationId": f.loc, "path": "movies", "destPath": ""}), 409)
	f.expect(f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("movies/a.mp4"), nil), 409)
	if !f.exists("docs/note.txt") || !f.exists("movies/a.mp4") {
		t.Fatal("a protected file changed")
	}
	// The listing says why.
	list := f.jsonBody(f.do(&f.admin, "GET", "/api/files?"+f.q(""), nil), 200)
	marks := map[string]any{}
	for _, it := range list["items"].([]any) {
		m := it.(map[string]any)
		marks[m["name"].(string)] = m["protected"]
	}
	if marks["docs"] != "locked" || marks["movies"] != nil || marks["root.txt"] != nil {
		t.Fatalf("marks = %v", marks)
	}
	movies := f.jsonBody(f.do(&f.admin, "GET", "/api/files?"+f.q("movies"), nil), 200)
	for _, it := range movies["items"].([]any) {
		if m := it.(map[string]any); m["name"] == "a.mp4" && m["protected"] != "torrent" {
			t.Fatalf("a.mp4 = %v", m)
		}
	}
}

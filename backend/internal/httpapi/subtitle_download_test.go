package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeOpenSubtitles answers the few OpenSubtitles calls ReelStash makes.
type fakeOpenSubtitles struct {
	mu        sync.Mutex
	logins    int
	downloads int
	lastQuery string
}

func (f *fakeOpenSubtitles) server(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/file/22" { // the temporary link needs no key
			w.Write([]byte("1\n00:00:00,500 --> 00:00:01,500\nDownloaded line\n"))
			return
		}
		if r.Header.Get("Api-Key") != "key-123" || r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/login":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "me" || body["password"] != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"message":"wrong credentials"}`))
				return
			}
			f.logins++
			w.Write([]byte(`{"token":"tok","base_url":"api.opensubtitles.com"}`))
		case r.URL.Path == "/infos/formats":
			w.Write([]byte(`{"data":{"output_formats":["srt"]}}`))
		case r.URL.Path == "/infos/user":
			w.Write([]byte(`{"data":{"level":"Sub leecher","allowed_downloads":20,"remaining_downloads":19}}`))
		case r.URL.Path == "/subtitles":
			f.lastQuery = r.URL.RawQuery
			w.Write([]byte(`{"data":[
				{"attributes":{"language":"en","download_count":5,"release":"Other.Release","files":[{"file_id":11,"file_name":"other.srt"}]}},
				{"attributes":{"language":"en","download_count":2,"release":"Movie.Exact","moviehash_match":true,"hearing_impaired":true,"files":[{"file_id":22,"file_name":"exact.srt"}]}},
				{"attributes":{"language":"pt-BR","download_count":9,"release":"Two.CDs","files":[{"file_id":33},{"file_id":34}]}}
			]}`))
		case r.Method == "POST" && r.URL.Path == "/download":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f.downloads++
			w.Write([]byte(`{"link":"` + srv.URL + `/file/22","file_name":"exact.srt","remaining":18}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenSubtitlesSearchDownloadAndRemove(t *testing.T) {
	fake := &fakeOpenSubtitles{}
	srv := fake.server(t)
	subtitlesBaseURL = srv.URL
	t.Cleanup(func() { subtitlesBaseURL = "" })

	h := newIsolatedHarness(t, configWithData(t), nil)
	dir := t.TempDir()
	video := filepath.Join(dir, "Movie.mp4")
	os.WriteFile(video, make([]byte, 200*1024), 0o644)
	item := h.newTitle(uniq("Movie"))
	h.addFile(item, h.storage(dir), "Movie.mp4")
	base := "/api/media/" + itoa(item)

	// Not set up yet.
	h.expect(h.do(&h.admin, "GET", base+"/subtitles/search", nil), http.StatusConflict)

	// Settings: secrets are masked, a plain user cannot read or change them.
	h.expect(h.do(&h.user, "GET", "/api/admin/subtitle-provider", nil), http.StatusForbidden)
	cfg := h.jsonBody(h.do(&h.admin, "PUT", "/api/admin/subtitle-provider", map[string]string{"apiKey": "key-123", "languages": "EN, fr fr"}), http.StatusOK)
	if cfg["enabled"] != true || cfg["canDownload"] != false {
		t.Fatalf("after saving a key: %v", cfg)
	}
	if langs := cfg["languages"].([]any); len(langs) != 2 || langs[0] != "en" || langs[1] != "fr" {
		t.Errorf("languages = %v, want [en fr]", langs)
	}
	for _, f := range cfg["fields"].([]any) {
		if m := f.(map[string]any); m["key"] == "apiKey" && m["value"] == "key-123" {
			t.Errorf("api key is not masked: %v", m)
		}
	}
	if out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/subtitle-provider/test", nil), http.StatusOK); out["ok"] != true {
		t.Errorf("key-only test = %v", out)
	}

	// Searching works with the key alone; the file hash and languages go along.
	// A plain user cannot see this title at all (it is in no library of theirs).
	h.expect(h.do(&h.user, "GET", base+"/subtitles/search", nil), http.StatusNotFound)
	res := h.jsonBody(h.do(&h.admin, "GET", base+"/subtitles/search?q=movie", nil), http.StatusOK)
	if !strings.Contains(fake.lastQuery, "moviehash=") || !strings.Contains(fake.lastQuery, "languages=en%2Cfr") {
		t.Errorf("search query = %s", fake.lastQuery)
	}
	items := res["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("got %d results, want 2 (a two-CD subtitle cannot fit one video): %v", len(items), items)
	}
	if first := items[0].(map[string]any); first["fileId"] != float64(22) || first["hashMatch"] != true {
		t.Errorf("the hash match should come first: %v", first)
	}
	// Downloading needs the account.
	h.expect(h.do(&h.admin, "POST", base+"/subtitles", map[string]any{"fileId": 22, "language": "en"}), http.StatusConflict)

	h.expect(h.do(&h.admin, "PUT", "/api/admin/subtitle-provider", map[string]string{"username": "me", "password": "wrong"}), http.StatusOK)
	if out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/subtitle-provider/test", nil), http.StatusOK); out["ok"] != false {
		t.Errorf("wrong password test = %v", out)
	}
	h.expect(h.do(&h.admin, "PUT", "/api/admin/subtitle-provider", map[string]string{"password": "secret"}), http.StatusOK)
	if out := h.jsonBody(h.do(&h.admin, "POST", "/api/admin/subtitle-provider/test", nil), http.StatusOK); out["ok"] != true || out["account"].(map[string]any)["remainingDownloads"] != float64(19) {
		t.Errorf("account test = %v", out)
	}

	// The subtitle lands beside the video and shows up as a track.
	h.expect(h.do(&h.user, "POST", base+"/subtitles", map[string]any{"fileId": 22, "language": "en"}), http.StatusNotFound)
	dl := h.jsonBody(h.do(&h.admin, "POST", base+"/subtitles", map[string]any{"fileId": 22, "language": "eng", "hearingImpaired": true}), http.StatusCreated)
	if dl["fileName"] != "Movie.en.sdh.srt" || dl["remaining"] != float64(18) {
		t.Errorf("download = %v", dl)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Movie.en.sdh.srt")); !strings.Contains(string(b), "Downloaded line") {
		t.Errorf("subtitle file = %q", b)
	}
	// A second copy never overwrites the first.
	dl2 := h.jsonBody(h.do(&h.admin, "POST", base+"/subtitles", map[string]any{"fileId": 22, "language": "en", "hearingImpaired": true}), http.StatusCreated)
	if dl2["fileName"] != "Movie.en.sdh.2.srt" {
		t.Errorf("second download = %v", dl2)
	}
	if fake.logins != 1 { // the account test logged in; both downloads reuse its token
		t.Errorf("logged in %d times, want 1", fake.logins)
	}

	tracks := h.jsonBody(h.do(&h.admin, "GET", base+"/tracks", nil), http.StatusOK)
	subs := tracks["subtitles"].([]any)
	if len(subs) != 2 {
		t.Fatalf("tracks list %d subtitles, want 2: %v", len(subs), subs)
	}
	if s := subs[0].(map[string]any); s["language"] != "en" || s["fileName"] == nil || s["source"] != "external" {
		t.Errorf("subtitle track = %v", s)
	}
	if code := h.do(&h.admin, "GET", base+"/subtitles/"+dl["trackId"].(string)+".vtt", nil).Code; code != http.StatusOK {
		t.Errorf("serving the downloaded subtitle: %d", code)
	}

	// The admin Videos list counts it and filters on it.
	videos := h.jsonBody(h.do(&h.admin, "GET", "/api/admin/videos?tracks=subtitles&q="+strings.ReplaceAll(titleOf(t, h, item), " ", "+"), nil), http.StatusOK)
	vs := videos["items"].([]any)
	if len(vs) != 1 {
		t.Fatalf("subtitles filter returned %d titles, want 1", len(vs))
	}
	file := vs[0].(map[string]any)["file"].(map[string]any)
	if tr, ok := file["tracks"].(map[string]any); !ok || tr["subtitles"] != float64(2) {
		t.Errorf("file tracks = %v", file["tracks"])
	}

	// Removing: only files beside the video, never other files.
	h.expect(h.do(&h.user, "DELETE", base+"/subtitles/"+dl2["trackId"].(string), nil), http.StatusNotFound)
	h.expect(h.do(&h.admin, "DELETE", base+"/subtitles/e0", nil), http.StatusBadRequest)
	h.expect(h.do(&h.admin, "DELETE", base+"/subtitles/x00000000", nil), http.StatusNotFound)
	h.expect(h.do(&h.admin, "DELETE", base+"/subtitles/"+dl2["trackId"].(string), nil), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(dir, "Movie.en.sdh.2.srt")); !os.IsNotExist(err) {
		t.Errorf("subtitle file still there: %v", err)
	}
	if _, err := os.Stat(video); err != nil {
		t.Errorf("the video must stay: %v", err)
	}

	h.expect(h.do(&h.admin, "DELETE", "/api/admin/subtitle-provider", nil), http.StatusOK)
	h.expect(h.do(&h.admin, "GET", base+"/subtitles/search", nil), http.StatusConflict)
}

func titleOf(t *testing.T, h *harness, item int64) string {
	t.Helper()
	var title string
	if err := h.pool.QueryRow(t.Context(), `SELECT title FROM media_item WHERE id = $1`, item).Scan(&title); err != nil {
		t.Fatal(err)
	}
	return title
}

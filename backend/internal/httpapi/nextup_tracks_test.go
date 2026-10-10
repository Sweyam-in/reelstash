package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reelstash/backend/internal/config"
)

// storage registers a storage location rooted at mount, removed after the test.
func (h *harness) storage(mount string) int64 {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(context.Background(),
		`INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, uniq("Disk"), mount).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, id) })
	return id
}

// library adds a library with the given folders (disk id -> path), removed
// after the test.
func (h *harness) library(roots map[int64]string) int64 {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(context.Background(), `INSERT INTO library (name) VALUES ($1) RETURNING id`, uniq("Library")).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM library WHERE id = $1`, id) })
	for disk, path := range roots {
		if _, err := h.pool.Exec(context.Background(), `INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $2, $3)`, id, disk, path); err != nil {
			h.t.Fatal(err)
		}
	}
	return id
}

// addFile gives a title an available file row, removed before the title.
func (h *harness) addFile(item, storage int64, rel string) int64 {
	h.t.Helper()
	var id int64
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
		VALUES ($1, $2, $3, $4, 'mkv', 1) RETURNING id`, item, storage, rel, filepath.Base(rel)).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM media_file WHERE id = $1`, id) })
	return id
}

// episodes creates a show of n available episodes (season 1) and returns their ids in order.
func (h *harness) episodes(n int) (seriesID int64, ids []int64) {
	h.t.Helper()
	ctx := context.Background()
	if err := h.pool.QueryRow(ctx, `INSERT INTO series (name) VALUES ($1) RETURNING id`, uniq("Show")).Scan(&seriesID); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { h.pool.Exec(context.Background(), `DELETE FROM series WHERE id = $1`, seriesID) })
	disk := h.storage(h.t.TempDir())
	for i := 1; i <= n; i++ {
		id := h.newTitle(uniq("Episode"))
		h.pool.Exec(ctx, `UPDATE media_item SET series_id = $2, kind = 'episode', season_number = 1, episode_number = $3 WHERE id = $1`, id, seriesID, i)
		h.addFile(id, disk, "s01e0"+itoa(int64(i))+".mkv")
		ids = append(ids, id)
	}
	return seriesID, ids
}

func (h *harness) finish(who *actor, id int64) {
	h.t.Helper()
	h.expect(h.do(who, "PUT", "/api/playback/"+itoa(id), map[string]any{"positionSeconds": 0, "completed": true}), http.StatusOK)
}

func (h *harness) nextUp(who *actor) []map[string]any {
	h.t.Helper()
	return h.items(h.do(who, "GET", "/api/playback/next-up", nil))
}

func TestNextUpFollowsTheFurthestFinishedEpisode(t *testing.T) {
	h := newHarness(t)
	_, eps := h.episodes(3)

	if hasID(h.nextUp(&h.admin), eps[0]) {
		t.Error("a show that was never started must not be in Next up")
	}
	h.finish(&h.admin, eps[0])
	if got := h.nextUp(&h.admin); !hasID(got, eps[1]) || hasID(got, eps[2]) || hasID(got, eps[0]) {
		t.Errorf("after episode 1 the next up is episode 2 only, got %v", got)
	}
	// Finishing out of order moves on from the furthest one.
	h.finish(&h.admin, eps[2])
	if got := h.nextUp(&h.admin); hasID(got, eps[0]) || hasID(got, eps[1]) || hasID(got, eps[2]) {
		t.Errorf("a finished series has no next episode, got %v", got)
	}
}

func TestNextUpIsPerUserAndLeavesOutTitlesInProgress(t *testing.T) {
	h := newHarness(t)
	_, eps := h.episodes(3)
	h.finish(&h.admin, eps[0])

	if hasID(h.nextUp(&h.other), eps[1]) {
		t.Error("someone else's progress leaked into Next up")
	}
	// Under way: it belongs on Continue watching instead.
	h.pool.Exec(context.Background(), `UPDATE media_item SET duration_seconds = 1000 WHERE id = $1`, eps[1])
	h.expect(h.do(&h.admin, "PUT", "/api/playback/"+itoa(eps[1]), map[string]any{"positionSeconds": 300, "completed": false}), http.StatusOK)
	if hasID(h.nextUp(&h.admin), eps[1]) {
		t.Error("an episode already under way is in Continue watching, not Next up")
	}
	if !hasID(h.items(h.do(&h.admin, "GET", "/api/playback/continue", nil)), eps[1]) {
		t.Error("the episode under way should be in Continue watching")
	}
}

func TestNextUpSkipsEpisodesWithoutAFile(t *testing.T) {
	h := newHarness(t)
	_, eps := h.episodes(3)
	h.finish(&h.admin, eps[0])
	h.pool.Exec(context.Background(), `UPDATE media_file SET available = FALSE WHERE media_item_id = $1`, eps[1])
	if got := h.nextUp(&h.admin); hasID(got, eps[1]) || !hasID(got, eps[2]) {
		t.Errorf("an offline episode is skipped, so episode 3 is next, got %v", got)
	}
}

func TestContinueWatchingDropsNearlyFinishedTitles(t *testing.T) {
	h := newHarness(t)
	almost := h.newTitle(uniq("Almost"))
	half := h.newTitle(uniq("Half"))
	h.pool.Exec(context.Background(), `UPDATE media_item SET duration_seconds = 1000 WHERE id = ANY($1)`, []int64{almost, half})
	h.expect(h.do(&h.admin, "PUT", "/api/playback/"+itoa(almost), map[string]any{"positionSeconds": 980}), http.StatusOK)
	h.expect(h.do(&h.admin, "PUT", "/api/playback/"+itoa(half), map[string]any{"positionSeconds": 500}), http.StatusOK)
	got := h.items(h.do(&h.admin, "GET", "/api/playback/continue", nil))
	if hasID(got, almost) || !hasID(got, half) {
		t.Errorf("continue watching = %v, want only the half-watched title", got)
	}
}

func TestNextInSeries(t *testing.T) {
	h := newHarness(t)
	_, eps := h.episodes(3)
	next := func(id int64) any {
		out := h.jsonBody(h.do(&h.admin, "GET", "/api/media/"+itoa(id)+"/next", nil), http.StatusOK)
		if item, ok := out["item"].(map[string]any); ok {
			return int64(item["id"].(float64))
		}
		return nil
	}
	if next(eps[0]) != eps[1] || next(eps[1]) != eps[2] {
		t.Errorf("next of ep1 = %v, of ep2 = %v", next(eps[0]), next(eps[1]))
	}
	if next(eps[2]) != nil {
		t.Errorf("the last episode has no next, got %v", next(eps[2]))
	}
	alone := h.newTitle(uniq("Standalone"))
	if next(alone) != nil {
		t.Errorf("a title outside any series has no next, got %v", next(alone))
	}
}

func TestPreferences(t *testing.T) {
	h := newHarness(t)
	out := h.jsonBody(h.do(&h.user, "GET", "/api/preferences", nil), http.StatusOK)
	if out["subtitleMode"] != "off" || out["audioLanguage"] != nil {
		t.Errorf("defaults = %v", out)
	}
	if langs, _ := out["languages"].([]any); len(langs) < 10 {
		t.Errorf("languages = %v", out["languages"])
	}

	h.expect(h.do(&h.user, "PUT", "/api/preferences", map[string]any{"audioLanguage": "jpn", "subtitleLanguage": "English", "subtitleMode": "foreign"}), http.StatusOK)
	out = h.jsonBody(h.do(&h.user, "GET", "/api/preferences", nil), http.StatusOK)
	if out["audioLanguage"] != "ja" || out["subtitleLanguage"] != "en" || out["subtitleMode"] != "foreign" {
		t.Errorf("saved = %v, want languages normalised to ja/en", out)
	}
	if o := h.jsonBody(h.do(&h.other, "GET", "/api/preferences", nil), http.StatusOK); o["audioLanguage"] != nil {
		t.Errorf("another user's preferences changed: %v", o)
	}

	for _, bad := range []map[string]any{
		{"audioLanguage": "not a language at all", "subtitleMode": "off"},
		{"subtitleMode": "sometimes"},
		{"subtitleMode": "on"}, // needs a language
	} {
		if rec := h.do(&h.user, "PUT", "/api/preferences", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status = %d, want 400", bad, rec.Code)
		}
	}
	// Clearing works.
	h.expect(h.do(&h.user, "PUT", "/api/preferences", map[string]any{"audioLanguage": nil, "subtitleLanguage": "", "subtitleMode": "off"}), http.StatusOK)
	if o := h.jsonBody(h.do(&h.user, "GET", "/api/preferences", nil), http.StatusOK); o["audioLanguage"] != nil || o["subtitleLanguage"] != nil {
		t.Errorf("cleared = %v", o)
	}
}

// movieWithTracks writes a short MKV with two audio tracks (Japanese, English),
// an embedded English subtitle, and sidecar subtitles next to it.
func movieWithTracks(t *testing.T) (dir string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir = t.TempDir()
	srt := filepath.Join(dir, "embedded.srt")
	os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:01,500\nHello from inside\n\n"), 0o644)
	out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=24:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=22050:duration=3",
		"-i", srt,
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s",
		"-c:v", "libx264", "-c:a", "aac", "-c:s", "srt",
		"-metadata:s:a:0", "language=jpn", "-metadata:s:a:1", "language=eng", "-metadata:s:s:0", "language=eng",
		filepath.Join(dir, "Movie.mkv")).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg setup: %v: %s", err, out)
	}
	os.Remove(srt)
	os.WriteFile(filepath.Join(dir, "Movie.es.srt"), []byte("1\r\n00:00:01,000 --> 00:00:02,250\r\nHola, mundo\r\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Movie.fr.forced.vtt"), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nBonjour\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Other.en.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nnot mine\n"), 0o644)
	return dir
}

func TestMediaTracksListsEmbeddedAndSidecarTracks(t *testing.T) {
	dir := movieWithTracks(t)
	h := newHarnessCfg(t, configWithData(t))
	item := h.newTitle(uniq("Movie"))
	file := h.addFile(item, h.storage(dir), "Movie.mkv")

	out := h.jsonBody(h.do(&h.admin, "GET", "/api/media/"+itoa(item)+"/tracks", nil), http.StatusOK)
	audio := out["audio"].([]any)
	if len(audio) != 2 || audio[0].(map[string]any)["language"] != "ja" || audio[1].(map[string]any)["language"] != "en" {
		t.Fatalf("audio = %v, want Japanese then English", audio)
	}
	if !strings.HasPrefix(audio[1].(map[string]any)["label"].(string), "English") {
		t.Errorf("audio label = %v", audio[1])
	}
	subs := out["subtitles"].([]any)
	byLang := map[string]map[string]any{}
	for _, s := range subs {
		m := s.(map[string]any)
		byLang[m["language"].(string)+"/"+m["source"].(string)] = m
	}
	for _, want := range []string{"en/embedded", "es/external", "fr/external"} {
		if byLang[want] == nil {
			t.Errorf("missing subtitle %s in %v", want, subs)
		}
	}
	if len(subs) != 3 {
		t.Errorf("got %d subtitles, want 3 (another movie's .srt must not match): %v", len(subs), subs)
	}
	if byLang["fr/external"]["forced"] != true {
		t.Errorf("forced flag was not read from the file name: %v", byLang["fr/external"])
	}

	// The probe result is stored, so the next call does not need ffprobe.
	var probed bool
	h.pool.QueryRow(context.Background(), `SELECT tracks_probed_at IS NOT NULL FROM media_file WHERE id = $1`, file).Scan(&probed)
	var stored int
	h.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM media_track WHERE media_file_id = $1`, file).Scan(&stored)
	if !probed || stored != 3 {
		t.Errorf("probed = %v, stored tracks = %d, want true and 3", probed, stored)
	}

	get := func(id string) (int, string, string) {
		rec := h.do(&h.admin, "GET", "/api/media/"+itoa(item)+"/subtitles/"+id+".vtt", nil)
		return rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()
	}
	code, ctype, body := get(byLang["es/external"]["id"].(string))
	if code != 200 || !strings.HasPrefix(ctype, "text/vtt") || !strings.HasPrefix(body, "WEBVTT") || !strings.Contains(body, "00:00:01.000 --> 00:00:02.250") || !strings.Contains(body, "Hola, mundo") {
		t.Errorf("srt sidecar: %d %q\n%s", code, ctype, body)
	}
	if code, _, body := get(byLang["fr/external"]["id"].(string)); code != 200 || !strings.Contains(body, "Bonjour") {
		t.Errorf("vtt sidecar: %d\n%s", code, body)
	}
	if code, _, body := get(byLang["en/embedded"]["id"].(string)); code != 200 || !strings.Contains(body, "Hello from inside") || !strings.HasPrefix(body, "WEBVTT") {
		t.Errorf("embedded subtitle: %d\n%s", code, body)
	}
	if code, _, _ := get("x00000000"); code != http.StatusNotFound {
		t.Errorf("unknown sidecar: %d, want 404", code)
	}
	if code, _, _ := get("e9"); code != http.StatusUnprocessableEntity {
		t.Errorf("missing embedded stream: %d, want 422", code)
	}
	if code, _, _ := get("nonsense"); code != http.StatusBadRequest {
		t.Errorf("bad track id: %d, want 400", code)
	}
}

func TestHLSAndStreamSupportAudioTrackSelection(t *testing.T) {
	dir := movieWithTracks(t)
	h := newHarnessCfg(t, configWithData(t))
	item := h.newTitle(uniq("Dual audio"))
	h.addFile(item, h.storage(dir), "Movie.mkv")
	h.pool.Exec(context.Background(), `UPDATE media_file SET duration_seconds = 3, video_codec = 'h264', audio_codec = 'aac', container = 'matroska' WHERE media_item_id = $1`, item)

	base := "/api/media/" + itoa(item)
	def := h.do(&h.admin, "GET", base+"/hls/index.m3u8", nil).Body.String()
	alt := h.do(&h.admin, "GET", base+"/hls/index.m3u8?audio=1", nil).Body.String()
	if strings.Contains(def, "audio=") || !strings.Contains(alt, "audio=1") {
		t.Errorf("playlist segment urls:\ndefault: %s\nalt: %s", def, alt)
	}

	// Each track's sound really is that track: the second one is 22.05 kHz.
	rateOf := func(url string) string {
		rec := h.do(&h.admin, "GET", url, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", url, rec.Code, rec.Body.String())
		}
		f := filepath.Join(t.TempDir(), "seg.ts")
		os.WriteFile(f, rec.Body.Bytes(), 0o644)
		return audioSampleRate(t, f)
	}
	if got := rateOf(base + "/hls/seg/0.ts"); got != "44100" {
		t.Errorf("default segment sample rate = %s, want 44100 (first track)", got)
	}
	if got := rateOf(base + "/hls/seg/0.ts?audio=1"); got != "22050" {
		t.Errorf("audio=1 segment sample rate = %s, want 22050 (second track)", got)
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(h.cfg.ThumbnailDir), "hls"))
	if len(entries) != 2 {
		t.Errorf("each audio track needs its own segment cache, got %d directories", len(entries))
	}

	// Direct play with another track goes through a remux of that track.
	rec := h.do(&h.admin, "GET", base+"/stream?audio=1", nil)
	if rec.Code != http.StatusOK || len(rec.Body.Bytes()) < 1000 {
		t.Fatalf("stream with audio=1: %d (%d bytes)", rec.Code, rec.Body.Len())
	}
	remuxed := filepath.Join(t.TempDir(), "r.mp4")
	os.WriteFile(remuxed, rec.Body.Bytes(), 0o644)
	if got := audioSampleRate(t, remuxed); got != "22050" {
		t.Errorf("remuxed sample rate = %s, want 22050 (second track), and only one audio track", got)
	}
	h.expect(h.do(&h.admin, "GET", base+"/hls/index.m3u8?audio=abc", nil), http.StatusOK) // junk falls back to the first track
}

func configWithData(t *testing.T) config.Config {
	return config.Config{ThumbnailDir: filepath.Join(t.TempDir(), "thumbnails")}
}

// audioSampleRate reads the sample rate of a file's only audio stream; it fails
// the test when there is more than one.
func audioSampleRate(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=sample_rate", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var parsed struct {
		Streams []struct {
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil || len(parsed.Streams) != 1 {
		t.Fatalf("%s: want exactly one audio stream, got %s", path, out)
	}
	return parsed.Streams[0].SampleRate
}

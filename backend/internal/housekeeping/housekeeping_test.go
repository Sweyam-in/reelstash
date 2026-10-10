package housekeeping

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"reelstash/backend/internal/testdb"
)

func TestOwnedBy(t *testing.T) {
	stems := []string{"Film", "Film (2020)"}
	for name, want := range map[string]bool{
		"Film.nfo": true, "Film.en.srt": true, "Film-poster.jpg": true, "Film (2020)-fanart.jpg": true,
		"Filmography.nfo": false, "Other.nfo": false, "Film": false,
	} {
		if got := ownedBy(name, stems); got != want {
			t.Errorf("ownedBy(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsSidecar(t *testing.T) {
	for name, want := range map[string]bool{
		"a.nfo": true, "a.EN.SRT": true, "a-poster.jpg": true, "a-THUMB.png": true,
		"holiday.jpg": false, "a.mp4": false, "notes.txt": false,
	} {
		if got := isSidecar(name); got != want {
			t.Errorf("isSidecar(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestRun builds a data folder and a library folder full of leftovers and
// checks that a dry run only reports them and a clean-up removes exactly
// those, leaving everything still in use alone.
func TestRun(t *testing.T) {
	ctx := context.Background()
	db := testdb.Isolated(t)
	data := t.TempDir()
	dirs := Dirs{
		Thumbnails: filepath.Join(data, "thumbnails"), Sprites: filepath.Join(data, "sprites"), Previews: filepath.Join(data, "previews"),
		Posters: filepath.Join(data, "posters"), Transcode: filepath.Join(data, "hls"), Remux: filepath.Join(data, "remux"),
		Subtitles: filepath.Join(data, "subtitles"), Plugins: filepath.Join(data, "plugins"),
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	recent := time.Now().Add(-2 * time.Hour)
	write := func(path string, mtime time.Time) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return path
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	id := func(q string, args ...any) int64 {
		t.Helper()
		var v int64
		if err := db.QueryRow(ctx, q, args...).Scan(&v); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return v
	}

	// Library: one title with one file, on a disk with a library folder.
	media := t.TempDir()
	lib := id(`INSERT INTO library (name) VALUES ('Films') RETURNING id`)
	loc := id(`INSERT INTO storage_location (name, type, mount_path) VALUES ('Disk', 'LOCAL', $1) RETURNING id`, media)
	exec(`INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $2, 'films')`, lib, loc)
	poster := id(`INSERT INTO artwork (kind, storage_key, mime_type) VALUES ('POSTER', 'used.jpg', 'image/jpeg') RETURNING id`)
	item := id(`INSERT INTO media_item (title, poster_image_id) VALUES ('Film', $1) RETURNING id`, poster)
	file := id(`INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
		VALUES ($1, $2, 'films/Film.mp4', 'Film.mp4', 'mp4', 4) RETURNING id`, item, loc)
	fid := strconv.FormatInt(file, 10)

	// Generated files.
	keep := map[string]string{
		"thumbnail":      write(filepath.Join(dirs.Thumbnails, "keep.jpg"), old),
		"fresh orphan":   write(filepath.Join(dirs.Thumbnails, "fresh.jpg"), time.Now()),
		"poster":         write(filepath.Join(dirs.Posters, "used.jpg"), old),
		"remux":          write(filepath.Join(dirs.Remux, fid+".mp4"), recent),
		"subtitle":       write(filepath.Join(dirs.Subtitles, fid+"-e0-200.vtt"), recent),
		"hls":            write(filepath.Join(dirs.Transcode, fid, "0.ts"), recent),
		"plugin":         write(filepath.Join(dirs.Plugins, "demo", "app", "manifest.json"), old),
		"video":          write(filepath.Join(media, "films", "Film.mp4"), old),
		"nfo":            write(filepath.Join(media, "films", "Film.nfo"), old),
		"subs folder":    write(filepath.Join(media, "films", "Subs", "Film.en.srt"), old),
		"folder nfo":     write(filepath.Join(media, "films", "movie.nfo"), old),
		"photo":          write(filepath.Join(media, "films", "holiday.jpg"), old),
		"outside folder": write(filepath.Join(media, "other", "Gone.nfo"), old),
	}
	gone := map[string]string{
		"orphan thumb":   write(filepath.Join(dirs.Thumbnails, "orphan.jpg"), old),
		"part":           write(filepath.Join(dirs.Thumbnails, "x.jpg.part"), old),
		"unused art":     write(filepath.Join(dirs.Posters, "unused.jpg"), old),
		"orphan remux":   write(filepath.Join(dirs.Remux, "999999.mp4"), recent),
		"stale remux":    write(filepath.Join(dirs.Remux, fid+"-a1.mp4"), old),
		"old subtitle":   write(filepath.Join(dirs.Subtitles, fid+"-e0-100.vtt"), recent),
		"orphan hls":     write(filepath.Join(dirs.Transcode, "999999-high", "0.ts"), recent),
		"plugin staging": write(filepath.Join(dirs.Plugins, "demo", "app.old", "manifest.json"), old),
		"orphan nfo":     write(filepath.Join(media, "films", "Gone.nfo"), old),
		"orphan poster":  write(filepath.Join(media, "films", "Gone-poster.jpg"), old),
		"junk":           write(filepath.Join(media, "films", ".DS_Store"), old),
	}
	for _, d := range []string{filepath.Join(dirs.Plugins, "demo", "app.old"), filepath.Join(dirs.Transcode, "999999-high")} {
		os.Chtimes(d, old, old)
	}
	exec(`INSERT INTO thumbnail (media_item_id, storage_key) VALUES ($1, 'keep.jpg'), ($1, 'lost.jpg')`, item)
	exec(`INSERT INTO artwork (kind, storage_key, mime_type, created_at) VALUES ('POSTER', 'unused.jpg', 'image/jpeg', now() - interval '2 days')`)

	// Rows.
	user := id(`INSERT INTO app_user (auth_subject, role) VALUES ('hk', 'USER') RETURNING id`)
	exec(`INSERT INTO web_session (id, user_id, expires_at) VALUES ('old', $1, now() - interval '1 day'), ('live', $1, now() + interval '1 day')`, user)
	exec(`INSERT INTO background_job (kind, status, created_at, finished_at) VALUES
		('SCAN', 'COMPLETED', now() - interval '60 days', now() - interval '60 days'),
		('SCAN', 'COMPLETED', now(), now()),
		('SCAN', 'PENDING', now() - interval '60 days', NULL)`)
	exec(`INSERT INTO notification (kind, title, created_at) VALUES ('x', 'old', now() - interval '200 days'), ('x', 'new', now())`)
	exec(`INSERT INTO match_link (kind, entity_id, provider, external_id) VALUES ('media', $1, 'p', '1'), ('media', 999999, 'p', '2')`, item)
	exec(`INSERT INTO media_item (title, created_at) VALUES ('Empty', now() - interval '2 days')`)

	s := New(db, dirs)
	all := []string{}
	for _, task := range Tasks() {
		all = append(all, task.ID)
	}

	count := func(q string) int {
		t.Helper()
		return int(id(q))
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	// Dry run: everything is reported, nothing changes.
	rep, err := s.Run(ctx, 0, all, true)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Finding{}
	for _, f := range rep.Findings {
		if f.Error != "" {
			t.Errorf("%s: %s", f.Task, f.Error)
		}
		found[f.Task] = f
	}
	want := map[string]int{
		"orphan_files": 1, "remux_cache": 2, "subtitle_cache": 1, "transcode_cache": 1, "temp_files": 1, "plugin_files": 1,
		"missing_artifacts": 1, "unused_artwork": 1, "finished_jobs": 1, "old_notifications": 1, "expired_sessions": 1,
		"orphan_links": 1, "empty_titles": 1, "orphan_sidecars": 2, "os_junk": 1, "capacity_history": 0, "plugin_settings": 0,
	}
	for task, n := range want {
		if found[task].Count != n {
			t.Errorf("dry run %s: count %d, want %d (%v, skipped %v)", task, found[task].Count, n, found[task].Samples, found[task].Skipped)
		}
	}
	for name, p := range gone {
		if !exists(p) {
			t.Errorf("dry run removed %s", name)
		}
	}
	if n := count(`SELECT COUNT(*) FROM web_session`); n != 2 {
		t.Errorf("dry run removed sessions: %d left", n)
	}

	// Clean-up.
	if _, err := s.Run(ctx, 0, all, false); err != nil {
		t.Fatal(err)
	}
	for name, p := range gone {
		if exists(p) {
			t.Errorf("%s was not removed", name)
		}
	}
	for name, p := range keep {
		if !exists(p) {
			t.Errorf("%s was removed", name)
		}
	}
	for q, n := range map[string]int{
		`SELECT COUNT(*) FROM web_session`:                                  1,
		`SELECT COUNT(*) FROM background_job`:                               2,
		`SELECT COUNT(*) FROM notification`:                                 1,
		`SELECT COUNT(*) FROM match_link`:                                   1,
		`SELECT COUNT(*) FROM media_item`:                                   1,
		`SELECT COUNT(*) FROM thumbnail`:                                    1,
		`SELECT COUNT(*) FROM artwork`:                                      1,
		`SELECT COUNT(*) FROM media_item WHERE poster_image_id IS NOT NULL`: 1,
	} {
		if got := count(q); got != n {
			t.Errorf("%s = %d, want %d", q, got, n)
		}
	}

	// A second run finds nothing left.
	rep, err = s.Run(ctx, 0, all, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		if f.Count != 0 {
			t.Errorf("after clean-up %s still finds %v", f.Task, f.Samples)
		}
	}
}

// TestMissingFolderIsNotCleared makes sure an unmounted data folder does not
// read as "every file is missing".
func TestMissingFolderIsNotCleared(t *testing.T) {
	ctx := context.Background()
	db := testdb.Isolated(t)
	item := int64(0)
	if err := db.QueryRow(ctx, `INSERT INTO media_item (title) VALUES ('x') RETURNING id`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := db.Exec(ctx, `INSERT INTO thumbnail (media_item_id, storage_key) VALUES ($1, $2)`, item, strconv.Itoa(i)+".jpg"); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	s := New(db, Dirs{Thumbnails: dir})
	rep, err := s.Run(ctx, 0, []string{"missing_artifacts"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if f := rep.Findings[0]; f.Count != 0 || len(f.Skipped) == 0 {
		t.Fatalf("finding = %+v, want skipped", f)
	}
	var n int
	db.QueryRow(ctx, `SELECT COUNT(*) FROM thumbnail`).Scan(&n)
	if n != 30 {
		t.Fatalf("%d thumbnails left, want 30", n)
	}
}

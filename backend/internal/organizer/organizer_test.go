package organizer

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"reelstash/backend/internal/testdb"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	sort.Strings(out)
	return out
}

func TestCompanionFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Scene.mp4", "Scene.nfo", "Scene-poster.jpg", "Scene.en.srt", "Scene-2.mp4", "Scene-2.nfo", "Scenery.txt", "poster.jpg"} {
		touch(t, filepath.Join(dir, n))
	}
	got := names(companionFiles(filepath.Join(dir, "Scene.mp4"), true))
	want := []string{"Scene-poster.jpg", "Scene.en.srt", "Scene.nfo"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("companions = %v, want %v (sibling video's files and shared folder art stay)", got, want)
	}

	solo := t.TempDir()
	for _, n := range []string{"Clip.mkv", "Clip.nfo", "fanart.jpg"} {
		touch(t, filepath.Join(solo, n))
	}
	if got := names(companionFiles(filepath.Join(solo, "Clip.mkv"), true)); strings.Join(got, ",") != "Clip.nfo,fanart.jpg" {
		t.Fatalf("solo companions = %v, want folder artwork included", got)
	}
	if got := names(companionFiles(filepath.Join(solo, "Clip.mkv"), false)); strings.Join(got, ",") != "Clip.nfo" {
		t.Fatalf("root companions = %v, want folder artwork left at the storage root", got)
	}
}

func TestRemoveEmptyDirs(t *testing.T) {
	mount := t.TempDir()
	touch(t, filepath.Join(mount, "a", "keep.txt"))
	touch(t, filepath.Join(mount, "a", "b", "c", ".DS_Store"))
	removeEmptyDirs(filepath.Join(mount, "a", "b", "c"), mount)
	if _, err := os.Stat(filepath.Join(mount, "a", "b")); !os.IsNotExist(err) {
		t.Fatalf("a/b should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(mount, "a", "keep.txt")); err != nil {
		t.Fatalf("a/ holds a file and must stay: %v", err)
	}
	removeEmptyDirs(mount, mount)
	if _, err := os.Stat(mount); err != nil {
		t.Fatal("the mount root must never be removed")
	}
}

func TestSettingsNormalize(t *testing.T) {
	s := Settings{CategorizedDir: " /Library/categorized/ ", UncategorizedDir: `uncategorized\`}
	if err := s.Normalize(); err != nil {
		t.Fatal(err)
	}
	if s.CategorizedDir != "Library/categorized" || s.UncategorizedDir != "uncategorized" {
		t.Fatalf("normalized to %q, %q", s.CategorizedDir, s.UncategorizedDir)
	}
	for _, bad := range []Settings{
		{CategorizedDir: "lib", UncategorizedDir: "lib/unsorted"},
		{CategorizedDir: "a:b", UncategorizedDir: "u"},
		{CategorizedDir: "", UncategorizedDir: "u"},
	} {
		b := bad
		if err := b.Normalize(); err == nil {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}
	escape := Settings{CategorizedDir: "../../etc", UncategorizedDir: "u"}
	if err := escape.Normalize(); err != nil || escape.CategorizedDir != "etc" {
		t.Fatalf("parent references must be cleaned inside the location, got %q, %v", escape.CategorizedDir, err)
	}
}

// TestOrganizeRun covers precedence, folder-per-video layout, companion
// files, name collisions and the database update. Requires Postgres at
// REELSTASH_TEST_DATABASE_URL; skipped otherwise.
func TestOrganizeRun(t *testing.T) {
	ctx := context.Background()
	// Isolated schema: this test rewrites global settings and rules
	// (rename_template, organizer_rule) that other packages' tests read.
	db := testdb.Isolated(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	id := func(sql string, args ...any) int64 {
		t.Helper()
		var v int64
		if err := db.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return v
	}
	exec(`DELETE FROM organizer_rule`)
	exec(`DELETE FROM app_setting WHERE key IN ('rename_template')`)

	mount := t.TempDir()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	loc := id(`INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, "OrgTest "+mount, mount)
	lib := id(`INSERT INTO library (name, mode) VALUES ($1, 'private') RETURNING id`, "OrgLib "+suffix)
	exec(`INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $2, '')`, lib, loc)
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM library WHERE id = $1`, lib)
		db.Exec(ctx, `DELETE FROM organizer_rule`)
		db.Exec(ctx, `DELETE FROM media_file WHERE storage_location_id = $1`, loc)
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, loc)
		db.Exec(ctx, `DELETE FROM studio WHERE name LIKE '%' || $1`, suffix)
		db.Exec(ctx, `DELETE FROM performer WHERE name LIKE '%' || $1`, suffix)
	})
	studio := id(`INSERT INTO studio (name) VALUES ($1) RETURNING id`, "Studio X "+suffix)
	perf := id(`INSERT INTO performer (name) VALUES ($1) RETURNING id`, "Jane Doe "+suffix)

	addFile := func(rel, title string, studioID *int64, performer *int64) int64 {
		touch(t, filepath.Join(mount, rel))
		item := id(`INSERT INTO media_item (title, studio_id, metadata_source) VALUES ($1, $2, 'MANUAL') RETURNING id`, title, studioID)
		if performer != nil {
			exec(`INSERT INTO media_item_performer (media_item_id, performer_id) VALUES ($1, $2)`, item, *performer)
		}
		return id(`INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size)
			VALUES ($1, $2, $3, $4, 'mp4', 1) RETURNING id`, item, loc, rel, filepath.Base(rel))
	}
	both := addFile("inbox/both.mp4", "Both", &studio, &perf)       // studio + performer
	studioOnly := addFile("inbox/studio.mp4", "Solo", &studio, nil) // studio only
	loose := addFile("loose.MP4", "Loose", nil, nil)                // no rule
	dupe := addFile("other/loose.mp4", "Loose", nil, nil)           // same name as loose
	touch(t, filepath.Join(mount, "inbox", "both.nfo"))
	touch(t, filepath.Join(mount, "inbox", "both-poster.jpg"))

	if err := SaveSettings(ctx, db, lib, Settings{CategorizedDir: "categorized", UncategorizedDir: "uncategorized", RenameFiles: true}); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO app_setting (key, value) VALUES ('rename_template', '{title}')`)

	// Studio first: "Both" files under the studio.
	if _, err := CreateRule(ctx, db, lib, KindStudio, studio, ""); err != nil {
		t.Fatal(err)
	}
	perfRule, err := CreateRule(ctx, db, lib, KindPerformer, perf, "Jane")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(ctx, db, lib, loc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range plan.Moves {
		if m.MediaFileID == both && m.To != "categorized/Studio X "+suffix+"/Both/Both.mp4" {
			t.Fatalf("studio-first plan put Both at %s", m.To)
		}
	}

	// Now give the performer precedence.
	var studioRuleID int64
	rules, _ := ListRules(ctx, db, lib)
	for _, r := range rules {
		if r.Kind == KindStudio {
			studioRuleID = r.ID
		}
	}
	if err := ReorderRules(ctx, db, lib, []int64{perfRule, studioRuleID}); err != nil {
		t.Fatal(err)
	}

	res, err := Run(ctx, db, 0, lib, loc, false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 4 || res.Failed != 0 {
		t.Fatalf("result = %+v", res)
	}

	want := map[int64]string{
		both:       "categorized/Jane/Both/Both.mp4",
		studioOnly: "categorized/Studio X " + suffix + "/Solo/Solo.mp4",
	}
	for fileID, rel := range want {
		var got string
		if err := db.QueryRow(ctx, `SELECT relative_path FROM media_file WHERE id = $1`, fileID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != rel {
			t.Errorf("media_file %d at %s, want %s", fileID, got, rel)
		}
		if _, err := os.Stat(filepath.Join(mount, rel)); err != nil {
			t.Errorf("video not on disk at %s: %v", rel, err)
		}
	}
	var a, b string
	db.QueryRow(ctx, `SELECT relative_path FROM media_file WHERE id = $1`, loose).Scan(&a)
	db.QueryRow(ctx, `SELECT relative_path FROM media_file WHERE id = $1`, dupe).Scan(&b)
	got := []string{a, b}
	sort.Strings(got)
	if got[0] != "uncategorized/Loose (2)/Loose (2).mp4" || got[1] != "uncategorized/Loose/Loose.mp4" {
		t.Errorf("same-named videos got %v; each needs its own folder", got)
	}
	for _, f := range []string{"Both.nfo", "Both-poster.jpg"} {
		if _, err := os.Stat(filepath.Join(mount, "categorized", "Jane", "Both", f)); err != nil {
			t.Errorf("companion %s did not follow the video: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(mount, "inbox")); !os.IsNotExist(err) {
		t.Errorf("emptied inbox/ should be removed")
	}

	// Organizing again is a no-op.
	res, err = Run(ctx, db, 0, lib, loc, false, Options{})
	if err != nil || res.Moved != 0 || res.Unchanged != 4 {
		t.Fatalf("second run = %+v, %v", res, err)
	}
	exec(`DELETE FROM app_setting WHERE key IN ('rename_template')`)
}

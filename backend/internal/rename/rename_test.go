package rename_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/rename"
	"reelstash/backend/internal/testdb"
)

func ptr[T any](v T) *T { return &v }

func TestSanitizeComponent(t *testing.T) {
	cases := map[string]string{
		`A/B\C:D*E?F"G<H>I|J`: "ABCDEFGHIJ",
		"  padded  ":          "padded",
		"trailing dots...":    "trailing dots",
		"trailing. . ":        "trailing",
		"":                    "",
		"ok name":             "ok name",
	}
	for in, want := range cases {
		if got := rename.SanitizeComponent(in); got != want {
			t.Errorf("SanitizeComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildFileName(t *testing.T) {
	d := time.Date(2022, 1, 3, 0, 0, 0, 0, time.UTC)
	tpl := "{studio} - {date} - {title} [{resolution}]"
	cases := []struct {
		name                  string
		tpl                   string
		title, studio, series string
		date                  *time.Time
		year                  *int32
		height                *int
		performers            []string
		ext, want             string
	}{
		{"full", tpl, "Scene", "Evil Angel", "", &d, nil, ptr(2160), nil, ".MP4", "Evil Angel - 2022-01-03 - Scene [2160p].mp4"},
		{"year replaces missing date", tpl, "Scene", "Studio", "", nil, ptr(int32(2019)), ptr(1080), nil, ".mkv", "Studio - 2019 - Scene [1080p].mkv"},
		{"missing studio and height collapse", tpl, "Scene", "", "", &d, nil, nil, nil, ".mp4", "2022-01-03 - Scene.mp4"},
		{"nothing but title", tpl, "Only Title", "", "", nil, nil, nil, nil, ".mp4", "Only Title.mp4"},
		{"illegal chars stripped from tokens", tpl, `What/Is:This?`, "A|B", "", &d, nil, nil, nil, ".mp4", "AB - 2022-01-03 - WhatIsThis.mp4"},
		{"performers joined", "{title} ({performers})", "T", "", "", nil, nil, nil, []string{"Ann", "Bob"}, ".mp4", "T (Ann, Bob).mp4"},
		{"series, year and height tokens", "{series} {year} {height}", "T", "", "Saga", nil, ptr(int32(2020)), ptr(720), nil, ".mp4", "Saga 2020 720.mp4"},
		{"empty template falls back to title", "{studio}", "Fallback", "", "", nil, nil, nil, nil, ".avi", "Fallback.avi"},
		{"literal text survives", "Prefix - {title}", "T", "", "", nil, nil, nil, nil, ".mp4", "Prefix - T.mp4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rename.BuildFileName(c.tpl, c.title, c.studio, c.series, c.date, c.year, c.height, c.performers, c.ext)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTemplateSettingOverridesFallback(t *testing.T) {
	pool := testdb.Isolated(t)
	ctx := context.Background()
	if got := rename.Template(ctx, pool, 0, ""); got != rename.DefaultTemplate {
		t.Errorf("default = %q", got)
	}
	if got := rename.Template(ctx, pool, 0, "{title}"); got != "{title}" {
		t.Errorf("fallback = %q", got)
	}
	pool.Exec(ctx, `INSERT INTO app_setting (key, value) VALUES ($1, $2)`, rename.TemplateSettingKey, "{date} {title}")
	if got := rename.Template(ctx, pool, 0, "{title}"); got != "{date} {title}" {
		t.Errorf("saved setting should win, got %q", got)
	}
	pool.Exec(ctx, `UPDATE app_setting SET value = '   ' WHERE key = $1`, rename.TemplateSettingKey)
	if got := rename.Template(ctx, pool, 0, "{title}"); got != "{title}" {
		t.Errorf("a blank setting is ignored, got %q", got)
	}
}

type fixture struct {
	pool   *pgxpool.Pool
	mount  string
	fileID int64
}

// newFile lays out mount/Sub/<name> on disk and in the database, with a studio,
// a release date, a height and two performers.
func newFile(t *testing.T, name string) fixture {
	t.Helper()
	pool := testdb.Isolated(t)
	ctx := context.Background()
	mount := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mount, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "Sub", name), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var loc, studio, item, file int64
	must(pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, "d-"+uuid.NewString(), mount).Scan(&loc))
	must(pool.QueryRow(ctx, `INSERT INTO studio (name) VALUES ('Acme') RETURNING id`).Scan(&studio))
	must(pool.QueryRow(ctx, `INSERT INTO media_item (title, studio_id, release_date) VALUES ('Great Scene', $1, '2024-05-06') RETURNING id`, studio).Scan(&item))
	must(pool.QueryRow(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size, height)
		VALUES ($1, $2, $3, $4, 'mp4', 5, 1080) RETURNING id`, item, loc, "Sub/"+name, name).Scan(&file))
	for i, p := range []string{"Zed", "Amy"} {
		var pid int64
		must(pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, p).Scan(&pid))
		_, err := pool.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id, position) VALUES ($1, $2, $3)`, item, pid, i)
		must(err)
	}
	return fixture{pool, mount, file}
}

const tpl = "{studio} - {date} - {title} [{resolution}]"

func TestPreviewDoesNotTouchDisk(t *testing.T) {
	f := newFile(t, "random.mp4")
	got, err := rename.Preview(context.Background(), f.pool, tpl, f.fileID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NewRelativePath != "Sub/Acme - 2024-05-06 - Great Scene [1080p].mp4" || got.OldRelativePath != "Sub/random.mp4" || got.Unchanged {
		t.Errorf("preview = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "Sub", "random.mp4")); err != nil {
		t.Error("Preview must not rename anything")
	}
	// Performers keep their stored order.
	p2, _ := rename.Preview(context.Background(), f.pool, "{performers}", f.fileID)
	if p2.NewFileName != "Zed, Amy.mp4" {
		t.Errorf("performers = %q", p2.NewFileName)
	}
}

func TestApplyRenamesFileNFOAndRow(t *testing.T) {
	f := newFile(t, "random.mp4")
	nfo := filepath.Join(f.mount, "Sub", "random.nfo")
	os.WriteFile(nfo, []byte("<movie/>"), 0o644)

	got, err := rename.Apply(context.Background(), f.pool, tpl, f.fileID)
	if err != nil {
		t.Fatal(err)
	}
	newVideo := filepath.Join(f.mount, "Sub", "Acme - 2024-05-06 - Great Scene [1080p].mp4")
	if _, err := os.Stat(newVideo); err != nil {
		t.Errorf("renamed video missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "Sub", "random.mp4")); !os.IsNotExist(err) {
		t.Error("old video should be gone")
	}
	if _, err := os.Stat(filepath.Join(f.mount, "Sub", "Acme - 2024-05-06 - Great Scene [1080p].nfo")); err != nil {
		t.Errorf("the adjacent .nfo must follow the video: %v", err)
	}
	var rel, name string
	f.pool.QueryRow(context.Background(), `SELECT relative_path, file_name FROM media_file WHERE id = $1`, f.fileID).Scan(&rel, &name)
	if rel != got.NewRelativePath || name != got.NewFileName {
		t.Errorf("row = %q/%q, target = %+v", rel, name, got)
	}

	// Applying again is a no-op.
	again, err := rename.Apply(context.Background(), f.pool, tpl, f.fileID)
	if err != nil || !again.Unchanged {
		t.Errorf("second apply = %+v, %v", again, err)
	}
}

func TestApplyRefusesToOverwrite(t *testing.T) {
	f := newFile(t, "random.mp4")
	blocker := filepath.Join(f.mount, "Sub", "Acme - 2024-05-06 - Great Scene [1080p].mp4")
	os.WriteFile(blocker, []byte("someone else's file"), 0o644)
	if _, err := rename.Apply(context.Background(), f.pool, tpl, f.fileID); err == nil {
		t.Fatal("expected an error when the target exists")
	}
	if b, _ := os.ReadFile(blocker); string(b) != "someone else's file" {
		t.Error("the existing file was clobbered")
	}
	if _, err := os.Stat(filepath.Join(f.mount, "Sub", "random.mp4")); err != nil {
		t.Error("the source must stay put after a refused rename")
	}
	var rel string
	f.pool.QueryRow(context.Background(), `SELECT relative_path FROM media_file WHERE id = $1`, f.fileID).Scan(&rel)
	if rel != "Sub/random.mp4" {
		t.Errorf("row changed despite refusal: %q", rel)
	}
}

func TestApplyFailsWhenSourceMissing(t *testing.T) {
	f := newFile(t, "random.mp4")
	os.Remove(filepath.Join(f.mount, "Sub", "random.mp4"))
	if _, err := rename.Apply(context.Background(), f.pool, tpl, f.fileID); err == nil {
		t.Error("renaming a file that vanished must error")
	}
}

func TestUnknownFile(t *testing.T) {
	f := newFile(t, "random.mp4")
	if _, err := rename.Preview(context.Background(), f.pool, tpl, 999999); err == nil {
		t.Error("expected not found")
	}
	if _, err := rename.Apply(context.Background(), f.pool, tpl, 999999); err == nil {
		t.Error("expected not found")
	}
}

func TestPendingQueueListsOnlyMismatches(t *testing.T) {
	f := newFile(t, "random.mp4")
	q, err := rename.PendingQueue(context.Background(), f.pool, 0, tpl, 10)
	if err != nil || len(q) != 1 || q[0].MediaFileID != f.fileID {
		t.Fatalf("queue = %+v, %v", q, err)
	}
	if _, err := rename.Apply(context.Background(), f.pool, tpl, f.fileID); err != nil {
		t.Fatal(err)
	}
	q, _ = rename.PendingQueue(context.Background(), f.pool, 0, tpl, 10)
	if len(q) != 0 {
		t.Errorf("queue after applying = %+v", q)
	}
	// Files that went missing are not queued.
	f.pool.Exec(context.Background(), `UPDATE media_file SET available = false`)
	f.pool.Exec(context.Background(), `UPDATE media_file SET file_name = 'x.mp4', relative_path = 'Sub/x.mp4'`)
	if q, _ = rename.PendingQueue(context.Background(), f.pool, 0, tpl, 10); len(q) != 0 {
		t.Errorf("unavailable file queued: %+v", q)
	}
}

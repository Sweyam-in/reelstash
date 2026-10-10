package scanner

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

func box(kind string, payload int) []byte {
	b := make([]byte, 8+payload)
	binary.BigEndian.PutUint32(b, uint32(8+payload))
	copy(b[4:], kind)
	return b
}

func TestCountMP4DataBoxes(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, parts ...[]byte) string {
		var all []byte
		for _, p := range parts {
			all = append(all, p...)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, all, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// 64-bit size box: size field 1, then the real size after the type.
	large := make([]byte, 16+4)
	binary.BigEndian.PutUint32(large, 1)
	copy(large[4:], "mdat")
	binary.BigEndian.PutUint64(large[8:], uint64(len(large)))

	normal := write("normal.mp4", box("ftyp", 16), box("moov", 100), box("mdat", 1000))
	scattered := [][]byte{box("ftyp", 16), box("moov", 100)}
	for range 20 {
		scattered = append(scattered, box("mdat", 50), box("mdat", 5))
	}
	many := write("many.mp4", scattered...)
	wide := write("wide.mp4", box("ftyp", 16), large, box("mdat", 10))

	for _, tc := range []struct {
		path  string
		limit int
		want  int
	}{{normal, 64, 1}, {many, 64, 40}, {many, 9, 9}, {wide, 64, 2}} {
		got, err := CountMP4DataBoxes(tc.path, tc.limit)
		if err != nil || got != tc.want {
			t.Errorf("%s (limit %d) = %d, %v; want %d", filepath.Base(tc.path), tc.limit, got, err, tc.want)
		}
	}
	if mp4DataBoxes(filepath.Join(dir, "clip.mkv")) != nil {
		t.Error("non-mp4 files should not be checked")
	}
}

// fragmentedTestVideo builds a short MP4 whose media data is split across a
// box per keyframe, like the files the repair job targets.
func fragmentedTestVideo(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=12",
		"-c:v", "libx264", "-g", "10", "-c:a", "aac", "-shortest",
		"-movflags", "frag_keyframe+empty_moov", path).CombinedOutput()
	if err != nil {
		t.Skipf("cannot build test video: %v: %s", err, out)
	}
}

func TestRewriteMP4(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	fragmentedTestVideo(t, path)
	if n, _ := CountMP4DataBoxes(path, 64); n <= ScatteredDataBoxes {
		t.Fatalf("test video has %d data boxes, want a scattered one", n)
	}
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chmod(path, 0o640)
	os.Chtimes(path, old, old)

	info, err := RewriteMP4(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := CountMP4DataBoxes(path, 64); n != 1 {
		t.Errorf("rewritten file has %d data boxes, want 1", n)
	}
	if !info.ModTime().Equal(old) || info.Mode().Perm() != 0o640 {
		t.Errorf("mtime %v mode %v not kept", info.ModTime(), info.Mode().Perm())
	}
	if pr, err := Probe(context.Background(), path); err != nil || len(pr.Tracks) != 1 || pr.DurationSeconds == nil || *pr.DurationSeconds < 11 {
		t.Errorf("rewritten file probes as %+v, %v", pr, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*"+mp4RepairSuffix)); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// TestMP4RepairJob checks that a scan records the box layout and the job
// rewrites only the scattered file, updating its row to match the disk.
func TestMP4RepairJob(t *testing.T) {
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	if err := testdb.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	lib := t.TempDir()
	fragmentedTestVideo(t, filepath.Join(lib, "scattered.mp4"))
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y", "-i", filepath.Join(lib, "scattered.mp4"),
		"-c", "copy", "-movflags", "+faststart", filepath.Join(lib, "normal.mp4")).CombinedOutput(); err != nil {
		t.Fatalf("normal copy: %v: %s", err, out)
	}

	var locID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"MP4Repair "+lib, lib).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM background_job WHERE kind = 'MP4_REPAIR' AND payload->>'storageLocationId' = $1::text`, locID)
		db.Exec(ctx, `DELETE FROM media_file WHERE storage_location_id = $1`, locID)
		db.Exec(ctx, `DELETE FROM scan_job WHERE storage_location_id = $1`, locID)
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, locID)
	})
	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 2)
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}
	boxes := func(rel string) (n *int, size int64, mtime time.Time) {
		t.Helper()
		if err := db.QueryRow(ctx, `SELECT mp4_data_boxes, file_size, modified_time FROM media_file WHERE storage_location_id = $1 AND relative_path = $2`,
			locID, rel).Scan(&n, &size, &mtime); err != nil {
			t.Fatal(err)
		}
		return
	}
	if n, _, _ := boxes("scattered.mp4"); n == nil || *n <= ScatteredDataBoxes {
		t.Fatalf("scan recorded %v data boxes for the scattered file", n)
	}
	if n, _, _ := boxes("normal.mp4"); n == nil || *n != 1 {
		t.Fatalf("scan recorded %v data boxes for the normal file", n)
	}
	// A file scanned before the layout was recorded gets checked by the job.
	db.Exec(ctx, `UPDATE media_file SET mp4_data_boxes = NULL WHERE storage_location_id = $1 AND relative_path = 'normal.mp4'`, locID)
	normalBefore, _ := os.Stat(filepath.Join(lib, "normal.mp4"))

	out, err := s.handleMP4RepairJob(ctx, db, 0, []byte(`{"storageLocationId":`+strconv.FormatInt(locID, 10)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if res := out.(MP4RepairResult); res.Checked != 2 || res.Repaired != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want 2 checked, 1 repaired", res)
	}
	n, size, mtime := boxes("scattered.mp4")
	disk, _ := os.Stat(filepath.Join(lib, "scattered.mp4"))
	if n == nil || *n != 1 || size != disk.Size() || !mtime.Equal(disk.ModTime().Truncate(time.Microsecond)) {
		t.Errorf("row after repair: boxes %v size %d mtime %v; disk size %d mtime %v", n, size, mtime, disk.Size(), disk.ModTime())
	}
	if n, _, _ := boxes("normal.mp4"); n == nil || *n != 1 {
		t.Errorf("normal file not checked: %v", n)
	}
	if after, _ := os.Stat(filepath.Join(lib, "normal.mp4")); !after.ModTime().Equal(normalBefore.ModTime()) || after.Size() != normalBefore.Size() {
		t.Error("normal file was rewritten")
	}
	// The next scan sees the repaired file as unchanged.
	if sum, err := s.ScanStorageLocation(ctx, 0, locID); err != nil || sum.Changed != 0 {
		t.Errorf("rescan after repair: %+v, %v (want nothing changed)", sum, err)
	}
}

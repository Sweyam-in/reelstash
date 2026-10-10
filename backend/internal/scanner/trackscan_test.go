package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
	"reelstash/backend/internal/testdb"
)

func TestTrackScanJobReadsTracksAndCountsSubtitleFiles(t *testing.T) {
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
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
	srt := filepath.Join(t.TempDir(), "in.srt")
	os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:01,000\nInside\n\n"), 0o644)
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=10:duration=1", "-f", "lavfi", "-i", "sine=duration=1", "-i", srt,
		"-map", "0:v", "-map", "1:a", "-map", "2:s", "-c:v", "libx264", "-c:a", "aac", "-c:s", "srt",
		filepath.Join(lib, "inside.mkv")).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=10:duration=1", "-c:v", "libx264", filepath.Join(lib, "beside.mp4")).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}

	var locID int64
	if err := db.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`,
		"TrackScan "+lib, lib).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM background_job WHERE kind IN ('TRACK_SCAN', 'MP4_REPAIR') AND payload->>'storageLocationId' = $1::text`, locID)
		db.Exec(ctx, `DELETE FROM media_file WHERE storage_location_id = $1`, locID)
		db.Exec(ctx, `DELETE FROM scan_job WHERE storage_location_id = $1`, locID)
		db.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, locID)
	})
	s := New(db, jobs.NewRunner(db, 1), jobs.NewRunner(db, 1), t.TempDir(), 2)
	if _, err := s.ScanStorageLocation(ctx, 0, locID); err != nil {
		t.Fatal(err)
	}
	// Pretend both were scanned before tracks were recorded, and drop a
	// subtitle file beside one afterwards (a scan does not notice that).
	db.Exec(ctx, `DELETE FROM media_track WHERE media_file_id IN (SELECT id FROM media_file WHERE storage_location_id = $1)`, locID)
	db.Exec(ctx, `UPDATE media_file SET tracks_probed_at = NULL, sidecar_subtitles = NULL WHERE storage_location_id = $1`, locID)
	os.WriteFile(filepath.Join(lib, "beside.en.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nHi\n"), 0o644)

	out, err := s.handleTrackScanJob(ctx, db, 0, []byte(`{"storageLocationId":`+strconv.FormatInt(locID, 10)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if res := out.(TrackScanResult); res.Checked != 2 || res.Probed != 2 || res.Failed != 0 || res.WithSubtitles != 2 {
		t.Fatalf("result = %+v, want 2 checked, 2 probed, 2 with subtitles", res)
	}
	row := func(rel string) (probed bool, sidecars *int, subs int) {
		t.Helper()
		if err := db.QueryRow(ctx, `
			SELECT mf.tracks_probed_at IS NOT NULL, mf.sidecar_subtitles,
			       (SELECT COUNT(*) FROM media_track t WHERE t.media_file_id = mf.id AND t.kind = 'subtitle')
			FROM media_file mf WHERE storage_location_id = $1 AND relative_path = $2`, locID, rel).Scan(&probed, &sidecars, &subs); err != nil {
			t.Fatal(err)
		}
		return
	}
	if probed, sidecars, subs := row("inside.mkv"); !probed || sidecars == nil || *sidecars != 0 || subs != 1 {
		t.Errorf("inside.mkv: probed %v, sidecars %v, embedded subtitles %d", probed, sidecars, subs)
	}
	if probed, sidecars, subs := row("beside.mp4"); !probed || sidecars == nil || *sidecars != 1 || subs != 0 {
		t.Errorf("beside.mp4: probed %v, sidecars %v, embedded subtitles %d", probed, sidecars, subs)
	}
}

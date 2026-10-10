package httpapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reelstash/backend/internal/config"
)

// makeDamagedVideo encodes a 30s clip and overwrites a stretch in the middle
// with zeros, like a partial download or a bad sector would.
func makeDamagedVideo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "damaged.mp4")
	out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=24:duration=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30",
		"-c:v", "libx264", "-g", "24", "-c:a", "aac", "-movflags", "+faststart", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg setup: %v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mid := len(data) / 2
	for i := mid; i < mid+len(data)/5; i++ {
		data[i] = 0
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func segmentDuration(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe %s: %v: %s", path, err, out)
	}
	return string(out)
}

func TestEnsureSegmentSurvivesDamagedSource(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := makeDamagedVideo(t)
	a := &API{Cfg: config.Config{ThumbnailDir: filepath.Join(t.TempDir(), "thumbnails")}}
	ms := mediaSource{FileID: 1, FullPath: src, Duration: 30}

	// Every segment across the damaged range must come back non-empty and
	// playable, never an error and never a 0-byte file.
	for n := 0; n*a.segmentSeconds() < 30; n++ {
		out, err := a.ensureSegment(context.Background(), ms, n)
		if err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
		info, err := os.Stat(out)
		if err != nil || info.Size() == 0 {
			t.Fatalf("segment %d is empty (%v)", n, err)
		}
		segmentDuration(t, out)
	}
}

func TestEnsureSegmentIgnoresEmptyCachedSegment(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := makeDamagedVideo(t)
	a := &API{Cfg: config.Config{ThumbnailDir: filepath.Join(t.TempDir(), "thumbnails")}}
	ms := mediaSource{FileID: 2, FullPath: src, Duration: 30}
	dir := a.hlsCacheDir(ms.FileID, hlsQualities["balanced"])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "0.ts")
	if err := os.WriteFile(stale, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := a.ensureSegment(context.Background(), ms, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(out); info == nil || info.Size() == 0 {
		t.Fatal("a stale 0-byte cached segment was served instead of rebuilt")
	}
}

func TestQualityTiersUseSeparateCachesAndRateControl(t *testing.T) {
	a := &API{Cfg: config.Config{ThumbnailDir: filepath.Join(t.TempDir(), "thumbnails")}}
	bal, high, low := hlsQualities["balanced"], hlsQualities["high"], hlsQualities["low"]
	if a.hlsCacheDir(9, bal) == a.hlsCacheDir(9, high) || a.hlsCacheDir(9, high) == a.hlsCacheDir(9, low) {
		t.Fatal("each quality tier needs its own segment cache")
	}
	if filepath.Base(a.hlsCacheDir(9, bal)) != "9" {
		t.Fatalf("balanced must keep the original cache directory, got %s", a.hlsCacheDir(9, bal))
	}
	if got := encoderArgsFor(softwareProfile, high); !contains(got, "-crf", "18") {
		t.Fatalf("high should use crf 18, got %v", got)
	}
	if got := encoderArgsFor(softwareProfile, low); !contains(got, "-crf", "27") {
		t.Fatalf("low should use crf 27, got %v", got)
	}
	nv := hwProfile{encoderArgs: []string{"-c:v", "h264_nvenc", "-b:v", "6M"}}
	if got := encoderArgsFor(nv, low); !contains(got, "-b:v", "2500k") || nv.encoderArgs[3] != "6M" {
		t.Fatalf("low should use 2500k without mutating the profile, got %v / %v", got, nv.encoderArgs)
	}
}

func contains(args []string, key, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == val {
			return true
		}
	}
	return false
}

func TestLowQualityIsScaledTo720(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "hd.mp4")
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=1920x1080:rate=24:duration=8",
		"-f", "lavfi", "-i", "sine=duration=8", "-c:v", "libx264", "-c:a", "aac", srcPath).CombinedOutput(); err != nil {
		t.Fatalf("setup: %v: %s", err, out)
	}
	a := &API{Cfg: config.Config{ThumbnailDir: filepath.Join(dir, "thumbnails")}}
	out, err := a.ensureSegment(context.Background(), mediaSource{FileID: 3, FullPath: srcPath, Duration: 8, Quality: hlsQualities["low"]}, 0)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=height", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := strings.Cut(strings.TrimSpace(string(probe)), "\n"); got != "720" {
		t.Fatalf("low tier height = %q, want 720", got)
	}
}

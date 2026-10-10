package httpapi

import (
	"math"
	"strings"
	"testing"
)

func TestClampSeconds(t *testing.T) {
	cases := []struct{ t, dur, want float64 }{
		{-5, 100, 0},
		{math.NaN(), 100, 0},
		{math.Inf(1), 100, 0},
		{50, 100, 50},
		{100, 100, 99.9},
		{500, 100, 99.9},
		{500, 0, 500}, // unknown duration: no upper bound
	}
	for _, c := range cases {
		if got := clampSeconds(c.t, c.dur); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("clampSeconds(%v, %v) = %v, want %v", c.t, c.dur, got, c.want)
		}
	}
}

func TestCleanMarkerTitle(t *testing.T) {
	if got := cleanMarkerTitle("   "); got != "Marker" {
		t.Errorf("blank title = %q, want Marker", got)
	}
	if got := cleanMarkerTitle("  Best part  "); got != "Best part" {
		t.Errorf("trimmed title = %q", got)
	}
	long := strings.Repeat("é", 300)
	if got := []rune(cleanMarkerTitle(long)); len(got) != maxMarkerTitleRunes {
		t.Errorf("long title kept %d runes, want %d", len(got), maxMarkerTitleRunes)
	}
}

func TestValidMarkerSeconds(t *testing.T) {
	for _, ok := range []float64{0, 12.5, 7200} {
		if !validMarkerSeconds(ok) {
			t.Errorf("%v should be valid", ok)
		}
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), 1e9} {
		if validMarkerSeconds(bad) {
			t.Errorf("%v should be invalid", bad)
		}
	}
}

func TestCleanEvents(t *testing.T) {
	got, ok := cleanEvents([]string{"scan.failed", " scan.failed ", "job.failed"})
	if !ok || len(got) != 2 || got[0] != "scan.failed" || got[1] != "job.failed" {
		t.Fatalf("cleanEvents = %v, %v", got, ok)
	}
	if _, ok := cleanEvents([]string{"scan.failed", "bogus"}); ok {
		t.Fatal("unknown event must be rejected")
	}
	if got, ok := cleanEvents(nil); !ok || got == nil || len(got) != 0 {
		t.Fatalf("empty list should mean all events, got %v %v", got, ok)
	}
}

func TestSpriteVTT(t *testing.T) {
	s := spriteRow{Interval: 10, TileW: 160, TileH: 90, Columns: 2, Count: 3}
	vtt := spriteVTT("/api/media/1/sprite.jpg", s, 25)
	want := "WEBVTT\n\n" +
		"00:00:00.000 --> 00:00:10.000\n/api/media/1/sprite.jpg#xywh=0,0,160,90\n\n" +
		"00:00:10.000 --> 00:00:20.000\n/api/media/1/sprite.jpg#xywh=160,0,160,90\n\n" +
		"00:00:20.000 --> 00:00:25.000\n/api/media/1/sprite.jpg#xywh=0,90,160,90\n\n"
	if vtt != want {
		t.Fatalf("unexpected VTT:\n%s\nwant:\n%s", vtt, want)
	}
}

func TestVTTTime(t *testing.T) {
	if got := vttTime(3661.5); got != "01:01:01.500" {
		t.Fatalf("vttTime = %s", got)
	}
}

func TestJobsActive(t *testing.T) {
	idle := map[string]any{"counts": map[string]int64{"COMPLETED": 4}, "thumbnailsPending": int64(0), "previewsPending": int64(0)}
	if jobsActive(idle) {
		t.Error("nothing running should be idle")
	}
	running := map[string]any{"counts": map[string]int64{"RUNNING": 1}}
	if !jobsActive(running) {
		t.Error("a running job should be active")
	}
	thumbs := map[string]any{"counts": map[string]int64{}, "thumbnailsPending": int64(3)}
	if !jobsActive(thumbs) {
		t.Error("pending thumbnails should count as active")
	}
}

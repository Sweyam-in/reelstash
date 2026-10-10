package scanner

import "testing"

func TestSpriteLayout(t *testing.T) {
	cases := []struct {
		duration             float64
		interval             float64
		count, columns, rows int
	}{
		{20, 4, 5, 5, 1},
		{120, 4, 30, 10, 3},
		{400, 4, 100, 10, 10},
		{7200, 72, 100, 10, 10},
		{0, 4, 1, 1, 1},
	}
	for _, c := range cases {
		interval, count, columns, rows := spriteLayout(c.duration)
		if interval != c.interval || count != c.count || columns != c.columns || rows != c.rows {
			t.Errorf("spriteLayout(%v) = %v,%d,%d,%d want %v,%d,%d,%d", c.duration, interval, count, columns, rows, c.interval, c.count, c.columns, c.rows)
		}
		if columns*rows < count {
			t.Errorf("spriteLayout(%v): %dx%d grid cannot hold %d tiles", c.duration, columns, rows, count)
		}
		if count > spriteMaxTiles {
			t.Errorf("spriteLayout(%v): %d tiles exceeds the cap", c.duration, count)
		}
	}
}

func TestPreviewSegmentsStayInsideVideo(t *testing.T) {
	for _, d := range []float64{3, 11, 12, 45, 59, 60, 600, 7200} {
		starts, length := previewSegments(d)
		if len(starts) == 0 || length <= 0 {
			t.Fatalf("duration %v: no segments", d)
		}
		for _, s := range starts {
			if s < 0 || s+length > d+1e-9 {
				t.Errorf("duration %v: segment %v+%v runs past the end", d, s, length)
			}
		}
	}
	if starts, _ := previewSegments(600); len(starts) != 5 {
		t.Errorf("long video should sample 5 pieces, got %d", len(starts))
	}
	if starts, _ := previewSegments(30); len(starts) != 3 {
		t.Errorf("short video should sample 3 pieces, got %d", len(starts))
	}
}

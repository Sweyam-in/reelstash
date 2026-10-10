package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCountingWriterMatchesServeContent(t *testing.T) {
	body := bytes.Repeat([]byte("reelstash"), 5000)
	for _, rangeHeader := range []string{"", "bytes=100-1099"} {
		var counted int64
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/stream", nil)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		w := &countingWriter{ResponseWriter: rec, count: func(n int64) { counted += n }}
		http.ServeContent(w, req, "a.mp4", time.Now(), bytes.NewReader(body))
		if counted != int64(rec.Body.Len()) || counted == 0 {
			t.Errorf("range %q: counted %d bytes, sent %d", rangeHeader, counted, rec.Body.Len())
		}
	}
}

func TestStreamStatsActiveAndPeak(t *testing.T) {
	s := &streamStats{active: map[streamKey]activeStream{}}
	s.touch(nil, streamKey{1, 10}, "direct")
	s.touch(nil, streamKey{2, 11}, "hls")
	s.touch(nil, streamKey{1, 10}, "direct") // same stream again
	total, direct, hls := s.Now()
	if total != 2 || direct != 1 || hls != 1 || s.peak != 2 {
		t.Fatalf("got total=%d direct=%d hls=%d peak=%d", total, direct, hls, s.peak)
	}
	s.active[streamKey{2, 11}] = activeStream{kind: "hls", lastSeen: time.Now().Add(-2 * streamActiveWindow)}
	if total, _, _ := s.Now(); total != 1 {
		t.Fatalf("stale stream still counted: %d", total)
	}
	s.add(nil, "hls", 500)
	s.add(nil, "direct", 200)
	if s.hls != 500 || s.direct != 200 {
		t.Fatalf("bytes hls=%d direct=%d", s.hls, s.direct)
	}
}

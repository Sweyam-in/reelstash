package httpapi

import (
	"context"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// streamActiveWindow is how long after its last request a stream still
// counts as playing. HLS players fetch a segment every few seconds; a
// direct-play range request can cover a minute or more of video.
const streamActiveWindow = 90 * time.Second

type streamKey struct {
	userID      int64
	mediaItemID int64
}

type activeStream struct {
	kind     string // "direct" or "hls"
	lastSeen time.Time
}

// streamStats counts bytes served per day and which streams are playing,
// in memory, flushing the day totals to stream_daily every minute. Only
// totals are kept: nothing here records who watched what.
type streamStats struct {
	mu     sync.Mutex
	day    string // local date the pending counters belong to
	direct int64
	hls    int64
	peak   int
	active map[streamKey]activeStream
}

var streams = &streamStats{active: map[streamKey]activeStream{}}

func today() string { return time.Now().Format("2006-01-02") }

func (s *streamStats) rollDay(db *pgxpool.Pool) {
	if d := today(); d != s.day {
		if s.day != "" {
			s.flushLocked(db)
		}
		s.day = d
		s.peak = 0
	}
}

// touch notes a request for a stream; it returns the number playing now.
func (s *streamStats) touch(db *pgxpool.Pool, key streamKey, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollDay(db)
	now := time.Now()
	s.active[key] = activeStream{kind: kind, lastSeen: now}
	if n := s.countLocked(now); n > s.peak {
		s.peak = n
	}
}

func (s *streamStats) add(db *pgxpool.Pool, kind string, n int64) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollDay(db)
	if kind == "hls" {
		s.hls += n
	} else {
		s.direct += n
	}
}

func (s *streamStats) countLocked(now time.Time) int {
	n := 0
	for key, st := range s.active {
		if now.Sub(st.lastSeen) > streamActiveWindow {
			delete(s.active, key)
			continue
		}
		n++
	}
	return n
}

// Now reports how many streams are playing, split by delivery.
func (s *streamStats) Now() (total, direct, hls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.countLocked(now)
	for _, st := range s.active {
		if st.kind == "hls" {
			hls++
		} else {
			direct++
		}
	}
	return direct + hls, direct, hls
}

func (s *streamStats) flushLocked(db *pgxpool.Pool) {
	if s.day == "" || (s.direct == 0 && s.hls == 0 && s.peak == 0) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `
		INSERT INTO stream_daily (day, direct_bytes, hls_bytes, peak_streams) VALUES ($1, $2, $3, $4)
		ON CONFLICT (day) DO UPDATE SET
			direct_bytes = stream_daily.direct_bytes + EXCLUDED.direct_bytes,
			hls_bytes = stream_daily.hls_bytes + EXCLUDED.hls_bytes,
			peak_streams = GREATEST(stream_daily.peak_streams, EXCLUDED.peak_streams)
	`, s.day, s.direct, s.hls, s.peak); err != nil {
		log.Printf("stream stats: flush: %v", err)
		return // keep the counters; the next flush retries
	}
	s.direct, s.hls = 0, 0
}

func (s *streamStats) flush(db *pgxpool.Pool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollDay(db)
	s.flushLocked(db)
}

// runStreamStatsFlush saves the counters every minute. Up to a minute of
// totals is lost if the server stops abruptly.
func (a *API) runStreamStatsFlush(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			streams.flush(a.DB)
			return
		case <-ticker.C:
			streams.flush(a.DB)
		}
	}
}

// countStream wraps w so the bytes written are added to kind's daily total,
// and notes the stream as playing.
func (a *API) countStream(w http.ResponseWriter, r *http.Request, mediaItemID int64, kind string) http.ResponseWriter {
	var userID int64
	if user, ok := UserFromContext(r.Context()); ok {
		userID = user.ID
	}
	streams.touch(a.DB, streamKey{userID: userID, mediaItemID: mediaItemID}, kind)
	return &countingWriter{ResponseWriter: w, count: func(n int64) { streams.add(a.DB, kind, n) }}
}

// countingWriter counts response bytes. It keeps io.ReaderFrom so
// http.ServeContent can still use sendfile for direct play.
type countingWriter struct {
	http.ResponseWriter
	count func(int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.count(int64(n))
	return n, err
}

func (c *countingWriter) ReadFrom(r io.Reader) (int64, error) {
	var n int64
	var err error
	if rf, ok := c.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(struct{ io.Writer }{c.ResponseWriter}, r)
	}
	c.count(n)
	return n, err
}

func (c *countingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *countingWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

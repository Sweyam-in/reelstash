package plugins

import (
	"bufio"
	"io"
	"strings"
	"sync"
	"time"
)

// lineRing keeps the last lines a service printed.
type lineRing struct {
	mu   sync.Mutex
	max  int
	data []string
}

func newLineRing(max int) *lineRing { return &lineRing{max: max} }

func (r *lineRing) add(line string) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) > 2000 {
		line = line[:2000] + "…"
	}
	line = time.Now().Format("15:04:05 ") + line
	r.mu.Lock()
	r.data = append(r.data, line)
	if len(r.data) > r.max {
		r.data = r.data[len(r.data)-r.max:]
	}
	r.mu.Unlock()
}

// scan reads rd to its end, adding each line.
func (r *lineRing) scan(rd interface{ Read([]byte) (int, error) }) {
	sc := bufio.NewScanner(io.Reader(rd))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		r.add(sc.Text())
	}
}

func (r *lineRing) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.data...)
}

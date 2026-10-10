package extprovider

import (
	"strings"
	"sync"
	"time"
)

// maxCalls is how many recent requests are kept per plugin.
const maxCalls = 100

// Call is one request ReelStash made to a plugin, for the admin's request log.
type Call struct {
	At         time.Time `json:"at"`
	Method     string    `json:"method"`
	Path       string    `json:"path"` // with the query string, shortened
	Status     int       `json:"status"`
	DurationMs int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
}

var callLog = struct {
	mu    sync.Mutex
	byID  map[string][]Call // oldest first
	cap   int
	nowFn func() time.Time
}{byID: map[string][]Call{}, cap: maxCalls, nowFn: time.Now}

func recordCall(id string, c Call) {
	if id == "" || id == probeID {
		return
	}
	if len(c.Path) > 200 {
		c.Path = c.Path[:200] + "…"
	}
	if len(c.Error) > 300 {
		c.Error = c.Error[:300] + "…"
	}
	callLog.mu.Lock()
	defer callLog.mu.Unlock()
	calls := append(callLog.byID[id], c)
	if len(calls) > callLog.cap {
		calls = calls[len(calls)-callLog.cap:]
	}
	callLog.byID[id] = calls
}

// RecentCalls returns a plugin's latest requests, newest first. The log lives
// in memory and survives re-registering the plugin, not a restart.
func RecentCalls(id string) []Call {
	callLog.mu.Lock()
	defer callLog.mu.Unlock()
	src := callLog.byID[id]
	out := make([]Call, 0, len(src))
	for i := len(src) - 1; i >= 0; i-- {
		out = append(out, src[i])
	}
	return out
}

// ClearCalls forgets a plugin's request log.
func ClearCalls(id string) {
	callLog.mu.Lock()
	defer callLog.mu.Unlock()
	delete(callLog.byID, id)
}

// pathWithQuery shows a request the way an admin would read it.
func pathWithQuery(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}
	return path + "?" + strings.TrimPrefix(rawQuery, "?")
}

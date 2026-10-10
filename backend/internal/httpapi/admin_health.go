package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

var processStart = time.Now()

type healthCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"` // ok, warning, error
	Detail string `json:"detail"`
}

type healthDisk struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Online     bool    `json:"online"`
	Total      *int64  `json:"total,omitempty"`
	Available  *int64  `json:"available,omitempty"`
	LastSeenAt *string `json:"lastSeenAt,omitempty"`
}

type healthJobFailure struct {
	ID         int64   `json:"id"`
	Kind       string  `json:"kind"`
	Error      *string `json:"error,omitempty"`
	FinishedAt *string `json:"finishedAt,omitempty"`
}

type healthTool struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

type healthCache struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int64  `json:"files"`
	Error string `json:"error,omitempty"`
}

type healthPayload struct {
	Status   string        `json:"status"`
	Checks   []healthCheck `json:"checks"`
	Database struct {
		LatencyMs float64 `json:"latencyMs"`
		SizeBytes int64   `json:"sizeBytes"`
		Version   string  `json:"version"`
		PoolTotal int32   `json:"poolTotal"`
		PoolIdle  int32   `json:"poolIdle"`
		PoolInUse int32   `json:"poolInUse"`
		PoolMax   int32   `json:"poolMax"`
	} `json:"database"`
	Jobs struct {
		Pending        int64              `json:"pending"`
		Running        int64              `json:"running"`
		Failed24h      int64              `json:"failed24h"`
		OldestPending  *string            `json:"oldestPendingAt,omitempty"`
		RecentFailures []healthJobFailure `json:"recentFailures"`
	} `json:"jobs"`
	Disks     []healthDisk `json:"disks"`
	Providers []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"providers"`
	Tools   []healthTool  `json:"tools"`
	Caches  []healthCache `json:"caches"`
	Streams usageNow      `json:"streams"`
	Runtime struct {
		StartedAt     time.Time `json:"startedAt"`
		UptimeSeconds float64   `json:"uptimeSeconds"`
		Version       string    `json:"version"`
		GoVersion     string    `json:"goVersion"`
		Revision      string    `json:"revision,omitempty"`
		Goroutines    int       `json:"goroutines"`
		HeapBytes     uint64    `json:"heapBytes"`
		SysBytes      uint64    `json:"sysBytes"`
		CPUs          int       `json:"cpus"`
	} `json:"runtime"`
}

// lowSpaceFraction flags a disk with less than this share free.
const lowSpaceFraction = 0.1

func (a *API) adminHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var p healthPayload
	add := func(id, label, status, detail string) {
		p.Checks = append(p.Checks, healthCheck{ID: id, Label: label, Status: status, Detail: detail})
	}

	// Database
	start := time.Now()
	dbErr := a.DB.Ping(ctx)
	p.Database.LatencyMs = float64(time.Since(start).Microseconds()) / 1000
	if dbErr != nil {
		add("database", "Database", "error", dbErr.Error())
	} else {
		_ = a.DB.QueryRow(ctx, `SELECT pg_database_size(current_database()), current_setting('server_version')`).Scan(&p.Database.SizeBytes, &p.Database.Version)
		status := "ok"
		if p.Database.LatencyMs > 250 {
			status = "warning"
		}
		add("database", "Database", status, "Responding in "+formatMs(p.Database.LatencyMs))
	}
	stat := a.DB.Stat()
	p.Database.PoolTotal, p.Database.PoolIdle, p.Database.PoolInUse, p.Database.PoolMax = stat.TotalConns(), stat.IdleConns(), stat.AcquiredConns(), stat.MaxConns()

	// Jobs
	_ = a.DB.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status = 'PENDING'), COUNT(*) FILTER (WHERE status = 'RUNNING'),
		       COUNT(*) FILTER (WHERE status = 'FAILED' AND finished_at >= now() - INTERVAL '24 hours'),
		       (MIN(created_at) FILTER (WHERE status = 'PENDING'))::text
		FROM background_job`).Scan(&p.Jobs.Pending, &p.Jobs.Running, &p.Jobs.Failed24h, &p.Jobs.OldestPending)
	p.Jobs.RecentFailures, _ = queryRows(ctx, a, `
		SELECT id, kind, error, finished_at::text FROM background_job
		WHERE status = 'FAILED' ORDER BY finished_at DESC NULLS LAST LIMIT 5`, nil, func(s rowScanner) (healthJobFailure, error) {
		var f healthJobFailure
		return f, s.Scan(&f.ID, &f.Kind, &f.Error, &f.FinishedAt)
	})
	if p.Jobs.RecentFailures == nil {
		p.Jobs.RecentFailures = []healthJobFailure{}
	}
	switch {
	case p.Jobs.Failed24h > 0:
		add("jobs", "Background jobs", "warning", pluralize(p.Jobs.Failed24h, "job")+" failed in the last 24 hours")
	default:
		add("jobs", "Background jobs", "ok", pluralize(p.Jobs.Running, "job")+" running, "+pluralize(p.Jobs.Pending, "job")+" queued")
	}

	// Disks
	p.Disks, _ = queryRows(ctx, a, `
		SELECT id, name, online, total_capacity, available_capacity, last_seen_at::text FROM storage_location ORDER BY name`, nil,
		func(s rowScanner) (healthDisk, error) {
			var d healthDisk
			return d, s.Scan(&d.ID, &d.Name, &d.Online, &d.Total, &d.Available, &d.LastSeenAt)
		})
	if p.Disks == nil {
		p.Disks = []healthDisk{}
	}
	var offline, low []string
	for _, d := range p.Disks {
		if !d.Online {
			offline = append(offline, d.Name)
		} else if d.Total != nil && *d.Total > 0 && d.Available != nil && float64(*d.Available)/float64(*d.Total) < lowSpaceFraction {
			low = append(low, d.Name)
		}
	}
	switch {
	case len(p.Disks) == 0:
		add("disks", "Disks", "warning", "No storage locations are set up")
	case len(offline) > 0:
		add("disks", "Disks", "error", "Offline: "+strings.Join(offline, ", "))
	case len(low) > 0:
		add("disks", "Disks", "warning", "Low on space: "+strings.Join(low, ", "))
	default:
		add("disks", "Disks", "ok", pluralize(int64(len(p.Disks)), "disk")+" online")
	}

	// Metadata providers
	enabled := 0
	if reg := a.providerRegistry(); reg != nil {
		for _, pr := range reg.All() {
			on := pr.Enabled()
			if on {
				enabled++
			}
			p.Providers = append(p.Providers, struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			}{pr.ID(), pr.Name(), on})
		}
	}
	if enabled == 0 {
		add("providers", "Metadata providers", "warning", "None configured, so titles can't be matched")
	} else {
		add("providers", "Metadata providers", "ok", pluralize(int64(enabled), "provider")+" configured")
	}

	// Tools
	p.Tools = mediaTools()
	missing := []string{}
	for _, t := range p.Tools {
		if !t.Found {
			missing = append(missing, t.Name)
		}
	}
	if len(missing) > 0 {
		add("tools", "Media tools", "error", "Not found: "+strings.Join(missing, ", ")+". Thumbnails, previews and transcoding need them.")
	} else {
		add("tools", "Media tools", "ok", "ffmpeg and ffprobe available")
	}

	// Caches (sized in the background, at most every 10 minutes)
	p.Caches = a.cacheSizes()

	p.Streams.Streams, p.Streams.Direct, p.Streams.HLS = streams.Now()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	rt := &p.Runtime
	rt.StartedAt, rt.UptimeSeconds = processStart, time.Since(processStart).Seconds()
	rt.Version = Version
	rt.GoVersion, rt.Goroutines, rt.HeapBytes, rt.SysBytes, rt.CPUs = runtime.Version(), runtime.NumGoroutine(), mem.HeapAlloc, mem.Sys, runtime.NumCPU()
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				rt.Revision = s.Value[:7]
			}
		}
	}

	p.Status = "ok"
	for _, c := range p.Checks {
		if c.Status == "error" {
			p.Status = "error"
			break
		}
		if c.Status == "warning" {
			p.Status = "warning"
		}
	}
	writeJSON(w, http.StatusOK, p)
}

var (
	toolsOnce  sync.Once
	toolsFound []healthTool
)

// mediaTools finds ffmpeg and ffprobe once per process; they don't change while it runs.
func mediaTools() []healthTool {
	toolsOnce.Do(func() {
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			t := healthTool{Name: name}
			if path, err := exec.LookPath(name); err == nil {
				t.Found, t.Path = true, path
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if out, err := exec.CommandContext(ctx, path, "-version").Output(); err == nil {
					first := strings.SplitN(string(out), "\n", 2)[0]
					if fields := strings.Fields(first); len(fields) >= 3 {
						t.Version = fields[2]
					}
				}
				cancel()
			}
			toolsFound = append(toolsFound, t)
		}
	})
	return toolsFound
}

var (
	cacheMu      sync.Mutex
	cacheResult  []healthCache
	cacheSizedAt time.Time
	cacheSizing  bool
)

// cacheSizes reports how much space ReelStash's generated files take. Walking
// the folders can be slow, so it happens in the background; callers get the
// last result (empty until the first walk finishes).
func (a *API) cacheSizes() []healthCache {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if !cacheSizing && time.Since(cacheSizedAt) > 10*time.Minute {
		cacheSizing = true
		dataDir := filepath.Dir(a.Cfg.ThumbnailDir)
		dirs := [][2]string{
			{"Thumbnails", a.Cfg.ThumbnailDir},
			{"Scrub previews", a.Cfg.SpriteDir},
			{"Hover previews", a.Cfg.PreviewDir},
			{"Posters", a.Cfg.PosterDir},
			{"Transcode cache", filepath.Join(dataDir, "hls")},
			{"Remux cache", a.remuxCacheDir()},
			{"Subtitle cache", a.subtitleCacheDir()},
		}
		go func() {
			out := make([]healthCache, 0, len(dirs))
			for _, d := range dirs {
				c := healthCache{Name: d[0], Path: d[1]}
				size, err := dirSize(d[1])
				c.Bytes, c.Files = size[0], size[1]
				if err != nil {
					c.Error = err.Error()
				}
				out = append(out, c)
			}
			cacheMu.Lock()
			cacheResult, cacheSizedAt, cacheSizing = out, time.Now(), false
			cacheMu.Unlock()
		}()
	}
	if cacheResult == nil {
		return []healthCache{}
	}
	return cacheResult
}

// dirSize totals the bytes and file count under dir, giving up after two minutes.
func dirSize(dir string) ([2]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var total [2]int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if path == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total[0] += info.Size()
				total[1]++
			}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		err = nil // not created yet: nothing cached
	}
	return total, err
}

func pluralize(n int64, word string) string {
	s := strconv.FormatInt(n, 10) + " " + word
	if n != 1 {
		s += "s"
	}
	return s
}

func formatMs(ms float64) string {
	if ms < 1 {
		return "under 1 ms"
	}
	return strconv.FormatInt(int64(ms+0.5), 10) + " ms"
}

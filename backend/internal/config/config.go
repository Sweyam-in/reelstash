// Package config loads ReelStash server configuration from the environment.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the process configuration loaded from the environment by Load.
// Zero values for the numeric overrides mean "use the built-in default".
type Config struct {
	Addr         string
	DatabaseURL  string
	PublicOrigin string

	// ServerName is how apps label this server in their server list.
	ServerName string

	// MediaURLs and AdminURLs pair each origin of the media app with the
	// admin (+ files) app served beside it, index for index — e.g.
	// https://reelstash.example ↔ https://admin.reelstash.example and
	// http://media.local:8000 ↔ http://media.local:8001. The UI
	// asks /auth/config which pair its own host belongs to so it can link
	// across to the other app. Empty means both apps share one origin (dev).
	MediaURLs []string
	AdminURLs []string

	EmergencyLocalAuth     bool
	EmergencyLocalPassword string

	// AdminUsername and AdminPassword create a native admin account at start
	// when that username does not exist yet. Optional: without them the first
	// visitor creates the admin on the sign-in screen.
	AdminUsername string
	AdminPassword string

	// PluginDir is where plugin bundles are installed. PluginBundles switches
	// installing and running bundles on (the default) or off.
	PluginDir     string
	PluginBundles bool

	// ScanWorkers bounds how many background_job rows (SCAN, THUMBNAIL, ...)
	// run concurrently across the whole process.
	ScanWorkers int
	// ScanFileWorkers bounds how many files a single storage-location scan
	// hashes/probes concurrently (the expensive per-file work: full SHA-256,
	// ffprobe). Independent of ScanWorkers, which is about job-level, not
	// file-level, concurrency.
	ScanFileWorkers  int
	ThumbnailWorkers int
	ThumbnailDir     string
	// SpriteDir and PreviewDir hold scrub-preview sheets and hover clips.
	// They default to siblings of ThumbnailDir so one data volume covers all.
	SpriteDir  string
	PreviewDir string
	// SubtitleDir caches subtitles converted to WebVTT. It needs a volume of
	// its own in Docker, or every deploy throws the cache away.
	SubtitleDir string

	RenameTemplate string

	// HLSSegmentSeconds sets the HLS transcode segment length. Shorter
	// segments start faster but re-encode more often on seeks; longer
	// segments are more CPU-efficient but slower to start. 0 uses the
	// built-in default (6s).
	HLSSegmentSeconds int
	// HLSWorkers bounds concurrent ffmpeg transcode processes for HLS
	// playback. 0 uses the built-in default (max(2, NumCPU/2)).
	HLSWorkers int
	// PgMaxConns overrides the Postgres pool size. pgx otherwise defaults to
	// max(4, NumCPU), which can be too small once ffmpeg-heavy scanning and
	// concurrent streaming compete for the same small box. 0 keeps the pgx
	// default.
	PgMaxConns int

	PosterDir string

	// QbitURL, QbitUsername and QbitPassword connect the Downloads workspace
	// to qBittorrent (such as the one in the Compose stack). Settings saved
	// in the app override them.
	QbitURL      string
	QbitUsername string
	QbitPassword string
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return fallback
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func Load() Config {
	thumbnailDir := getenv("REELSTASH_THUMBNAIL_DIR", "/app-data/thumbnails")
	dataDir := filepath.Dir(thumbnailDir)
	return Config{
		Addr:         getenv("REELSTASH_ADDR", ":8902"),
		DatabaseURL:  getenv("DATABASE_URL", "postgres://reelstash:reelstash@localhost:5432/reelstash?sslmode=disable"),
		PublicOrigin: getenv("REELSTASH_PUBLIC_ORIGIN", "http://localhost:8902"),
		ServerName:   getenv("REELSTASH_SERVER_NAME", "ReelStash"),

		MediaURLs: splitList(os.Getenv("REELSTASH_MEDIA_URLS")),
		AdminURLs: splitList(os.Getenv("REELSTASH_ADMIN_URLS")),

		EmergencyLocalAuth:     getenv("EMERGENCY_LOCAL_AUTH", "false") == "true",
		EmergencyLocalPassword: getenv("EMERGENCY_LOCAL_PASSWORD", "changeme"),

		AdminUsername: os.Getenv("REELSTASH_ADMIN_USERNAME"),
		AdminPassword: os.Getenv("REELSTASH_ADMIN_PASSWORD"),

		PluginDir:     getenv("REELSTASH_PLUGIN_DIR", filepath.Join(dataDir, "plugins")),
		PluginBundles: getenv("REELSTASH_PLUGIN_BUNDLES", "true") != "false",

		ScanWorkers:      getenvInt("REELSTASH_SCAN_WORKERS", 4),
		ScanFileWorkers:  getenvInt("REELSTASH_SCAN_FILE_WORKERS", 4),
		ThumbnailWorkers: getenvInt("REELSTASH_THUMBNAIL_WORKERS", 2),
		ThumbnailDir:     thumbnailDir,
		SpriteDir:        getenv("REELSTASH_SPRITE_DIR", filepath.Join(dataDir, "sprites")),
		PreviewDir:       getenv("REELSTASH_PREVIEW_DIR", filepath.Join(dataDir, "previews")),
		SubtitleDir:      getenv("REELSTASH_SUBTITLE_DIR", filepath.Join(dataDir, "subtitles")),

		RenameTemplate: getenv("REELSTASH_RENAME_TEMPLATE", "{studio} - {date} - {title} [{resolution}]"),

		HLSSegmentSeconds: getenvInt("REELSTASH_HLS_SEGMENT_SECONDS", 0),
		HLSWorkers:        getenvInt("REELSTASH_HLS_WORKERS", 0),
		PgMaxConns:        getenvInt("REELSTASH_DB_MAX_CONNS", 0),

		PosterDir: getenv("REELSTASH_POSTER_DIR", "/app-data/posters"),

		QbitURL:      os.Getenv("REELSTASH_QBIT_URL"),
		QbitUsername: os.Getenv("REELSTASH_QBIT_USERNAME"),
		QbitPassword: os.Getenv("REELSTASH_QBIT_PASSWORD"),
	}
}

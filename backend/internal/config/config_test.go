package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitList(t *testing.T) {
	cases := map[string][]string{
		"":                 nil,
		"  ,, ":            nil,
		"a":                {"a"},
		" a , b,,c ":       {"a", "b", "c"},
		"http://x:1, y.z/": {"http://x:1", "y.z/"},
	}
	for in, want := range cases {
		if got := splitList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestGetenvHelpers(t *testing.T) {
	t.Setenv("RS_TEST_STR", "value")
	if getenv("RS_TEST_STR", "fb") != "value" || getenv("RS_TEST_UNSET_XYZ", "fb") != "fb" {
		t.Error("getenv fallback")
	}
	t.Setenv("RS_TEST_EMPTY", "")
	if getenv("RS_TEST_EMPTY", "fb") != "fb" {
		t.Error("an empty variable counts as unset")
	}
	t.Setenv("RS_TEST_INT", "7")
	t.Setenv("RS_TEST_BADINT", "seven")
	if getenvInt("RS_TEST_INT", 1) != 7 || getenvInt("RS_TEST_BADINT", 1) != 1 || getenvInt("RS_TEST_UNSET_XYZ", 9) != 9 {
		t.Error("getenvInt")
	}
	t.Setenv("RS_TEST_ZERO", "0")
	if getenvInt("RS_TEST_ZERO", 5) != 0 {
		t.Error("an explicit 0 must not fall back")
	}
}

func clearEnv(t *testing.T) {
	for _, k := range []string{
		"REELSTASH_ADDR", "DATABASE_URL", "REELSTASH_PUBLIC_ORIGIN", "REELSTASH_MEDIA_URLS",
		"REELSTASH_ADMIN_URLS", "EMERGENCY_LOCAL_AUTH", "EMERGENCY_LOCAL_PASSWORD", "REELSTASH_SCAN_WORKERS",
		"REELSTASH_SCAN_FILE_WORKERS", "REELSTASH_THUMBNAIL_WORKERS", "REELSTASH_THUMBNAIL_DIR", "REELSTASH_SPRITE_DIR",
		"REELSTASH_PREVIEW_DIR", "REELSTASH_RENAME_TEMPLATE", "REELSTASH_HLS_SEGMENT_SECONDS", "REELSTASH_HLS_WORKERS",
		"REELSTASH_DB_MAX_CONNS", "REELSTASH_POSTER_DIR",
		"REELSTASH_ADMIN_USERNAME", "REELSTASH_ADMIN_PASSWORD", "REELSTASH_PLUGIN_DIR", "REELSTASH_PLUGIN_BUNDLES",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	if c.Addr != ":8902" || c.PublicOrigin != "http://localhost:8902" {
		t.Errorf("addr/origin = %q %q", c.Addr, c.PublicOrigin)
	}
	if c.EmergencyLocalAuth {
		t.Error("emergency local auth must be off by default")
	}
	if c.ScanWorkers != 4 || c.ScanFileWorkers != 4 || c.ThumbnailWorkers != 2 {
		t.Errorf("workers = %d %d %d", c.ScanWorkers, c.ScanFileWorkers, c.ThumbnailWorkers)
	}
	if c.ThumbnailDir != "/app-data/thumbnails" || c.SpriteDir != filepath.Join("/app-data", "sprites") || c.PreviewDir != filepath.Join("/app-data", "previews") {
		t.Errorf("dirs = %q %q %q", c.ThumbnailDir, c.SpriteDir, c.PreviewDir)
	}
	if c.RenameTemplate != "{studio} - {date} - {title} [{resolution}]" || c.PosterDir != "/app-data/posters" {
		t.Errorf("misc defaults: %q %q", c.RenameTemplate, c.PosterDir)
	}
	if c.HLSSegmentSeconds != 0 || c.HLSWorkers != 0 || c.PgMaxConns != 0 {
		t.Error("HLS/pg overrides default to 0 (use built-ins)")
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("REELSTASH_ADDR", ":9000")
	t.Setenv("EMERGENCY_LOCAL_AUTH", "true")
	t.Setenv("EMERGENCY_LOCAL_PASSWORD", "pw")
	t.Setenv("REELSTASH_MEDIA_URLS", "https://m1,https://m2")
	t.Setenv("REELSTASH_ADMIN_URLS", "https://a1,https://a2")
	t.Setenv("REELSTASH_THUMBNAIL_DIR", "/data/x/thumbs")
	t.Setenv("REELSTASH_SCAN_WORKERS", "9")
	t.Setenv("REELSTASH_HLS_WORKERS", "3")
	t.Setenv("REELSTASH_DB_MAX_CONNS", "20")
	c := Load()
	if c.Addr != ":9000" || !c.EmergencyLocalAuth || c.EmergencyLocalPassword != "pw" {
		t.Errorf("basic overrides: %+v", c)
	}
	if !reflect.DeepEqual(c.MediaURLs, []string{"https://m1", "https://m2"}) || !reflect.DeepEqual(c.AdminURLs, []string{"https://a1", "https://a2"}) {
		t.Errorf("url pairs = %v %v", c.MediaURLs, c.AdminURLs)
	}
	// Sprites and previews default to siblings of the thumbnail dir.
	if c.SpriteDir != "/data/x/sprites" || c.PreviewDir != "/data/x/previews" {
		t.Errorf("sibling dirs = %q %q", c.SpriteDir, c.PreviewDir)
	}
	if c.ScanWorkers != 9 || c.HLSWorkers != 3 || c.PgMaxConns != 20 {
		t.Errorf("ints = %d %d %d", c.ScanWorkers, c.HLSWorkers, c.PgMaxConns)
	}
}

func TestEmergencyAuthNeedsExactTrue(t *testing.T) {
	clearEnv(t)
	for _, v := range []string{"1", "TRUE", "yes", "True"} {
		t.Setenv("EMERGENCY_LOCAL_AUTH", v)
		if Load().EmergencyLocalAuth {
			t.Errorf("EMERGENCY_LOCAL_AUTH=%q must not enable the backdoor login", v)
		}
	}
}

func TestPluginAndAdminSettings(t *testing.T) {
	clearEnv(t)
	c := Load()
	if !c.PluginBundles || c.PluginDir != filepath.Join("/app-data", "plugins") || c.AdminUsername != "" || c.AdminPassword != "" {
		t.Errorf("defaults: bundles=%v dir=%q admin=%q", c.PluginBundles, c.PluginDir, c.AdminUsername)
	}
	t.Setenv("REELSTASH_THUMBNAIL_DIR", "/data/x/thumbs")
	if c := Load(); c.PluginDir != "/data/x/plugins" {
		t.Errorf("the plugin dir should sit beside the other asset dirs, got %q", c.PluginDir)
	}
	t.Setenv("REELSTASH_PLUGIN_DIR", "/elsewhere")
	t.Setenv("REELSTASH_PLUGIN_BUNDLES", "false")
	t.Setenv("REELSTASH_ADMIN_USERNAME", "root")
	t.Setenv("REELSTASH_ADMIN_PASSWORD", "pw-123456")
	c = Load()
	if c.PluginBundles || c.PluginDir != "/elsewhere" || c.AdminUsername != "root" || c.AdminPassword != "pw-123456" {
		t.Errorf("overrides: %+v", c)
	}
}

package downloads

import (
	"testing"

	"reelstash/backend/internal/qbit"
)

func TestRuleMatches(t *testing.T) {
	tor := qbit.Torrent{Hash: "h", Name: "Some.Movie.2020.1080p", Category: "Movies", Tags: []string{"hd", "x"}, Tracker: "https://tracker.example.org/announce", Size: 2 << 30}
	videoFiles := func(string) ([]string, error) { return []string{"Some.Movie/movie.MKV", "readme.txt"}, nil }
	noVideo := func(string) ([]string, error) { return []string{"a.flac"}, nil }

	for _, c := range []struct {
		name  string
		rule  Rule
		files FileLister
		want  bool
	}{
		{"empty rule matches", Rule{Enabled: true}, nil, true},
		{"disabled", Rule{}, nil, false},
		{"category case-insensitive", Rule{Enabled: true, MatchCategory: "movies"}, nil, true},
		{"category mismatch", Rule{Enabled: true, MatchCategory: "tv"}, nil, false},
		{"tag", Rule{Enabled: true, MatchTag: "HD"}, nil, true},
		{"missing tag", Rule{Enabled: true, MatchTag: "sd"}, nil, false},
		{"tracker substring", Rule{Enabled: true, MatchTracker: "example.org"}, nil, true},
		{"name substring", Rule{Enabled: true, MatchName: "movie.2020"}, nil, true},
		{"name wildcard", Rule{Enabled: true, MatchName: "*.1080p"}, nil, true},
		{"name wildcard is whole-name", Rule{Enabled: true, MatchName: "Some*2019*"}, nil, false},
		{"min size", Rule{Enabled: true, MinSizeMB: 4096}, nil, false},
		{"video only", Rule{Enabled: true, VideoOnly: true}, videoFiles, true},
		{"video only without video", Rule{Enabled: true, VideoOnly: true}, noVideo, false},
	} {
		got, err := c.rule.Matches(tor, c.files)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v", c.name, got, err)
		}
	}
}

func TestExpandSubfolder(t *testing.T) {
	tor := qbit.Torrent{Name: "A: B/C?", Category: "Movies", Tags: []string{"hd"}}
	if got := ExpandSubfolder("{category}/{name} [{tag}]", tor); got != "Movies/A BC [hd]" {
		t.Fatalf("got %q", got)
	}
	if got := ExpandSubfolder("{category}/../{name}", qbit.Torrent{Name: "x"}); got != "x" {
		t.Fatalf("climbing template: %q", got)
	}
}

func TestRuleNormalize(t *testing.T) {
	r := Rule{Name: " Movies ", DestStorageLocationID: 1, DestPath: "/Movies/", RemoveAfter: ""}
	if err := r.Normalize(); err != nil || r.Name != "Movies" || r.DestPath != "Movies" || r.RemoveAfter != RemoveNever {
		t.Fatalf("normalize: %+v %v", r, err)
	}
	bad := []Rule{
		{DestStorageLocationID: 1},
		{Name: "x"},
		{Name: "x", DestStorageLocationID: 1, DestPath: "../etc"},
		{Name: "x", DestStorageLocationID: 1, RemoveAfter: RemoveSeeded},
		{Name: "x", DestStorageLocationID: 1, RemoveAfter: "sometimes"},
	}
	for i, b := range bad {
		if err := b.Normalize(); err == nil {
			t.Errorf("bad rule %d accepted", i)
		}
	}
}

func TestPathsTranslate(t *testing.T) {
	p := Settings{PathFrom: "/downloads", PathTo: "/mnt/disk1/dl"}.Paths()
	if got := p.Local("/downloads/a/b"); got != "/mnt/disk1/dl/a/b" {
		t.Fatalf("Local = %s", got)
	}
	if got := p.Remote("/mnt/disk1/dl/a"); got != "/downloads/a" {
		t.Fatalf("Remote = %s", got)
	}
	if got := p.Local("/downloadsX/a"); got != "/downloadsX/a" {
		t.Fatalf("prefix lookalike translated: %s", got)
	}
	if got := (Paths{}).Local("/mnt/x/"); got != "/mnt/x" {
		t.Fatalf("no mapping: %s", got)
	}
}

func TestInDownloadArea(t *testing.T) {
	prefs := map[string]any{"save_path": "/mnt/downloads"}
	if !inDownloadArea("/mnt/downloads/movies", Paths{}, prefs, "") {
		t.Error("default folder not in area")
	}
	if inDownloadArea("/mnt/Movies", Paths{}, prefs, "") {
		t.Error("own folder counted as download area")
	}
	if !inDownloadArea("/mnt/cat", Paths{}, prefs, "/mnt/cat") {
		t.Error("category folder not in area")
	}
	if !inDownloadArea("/anything", Paths{}, nil, "") {
		t.Error("unknown prefs should not skip")
	}
}

func TestSetupWarnings(t *testing.T) {
	roots := []libraryRoot{{libraryID: 1, name: "Movies", abs: "/mnt/Movies"}}
	codes := func(w []Warning) []string {
		out := []string{}
		for _, x := range w {
			out = append(out, x.Code)
		}
		return out
	}
	if got := codes(setupWarnings(Settings{}, map[string]any{"temp_path_enabled": true, "temp_path": "/mnt/inc"}, roots)); len(got) != 1 || got[0] != "incomplete-unprotected" {
		t.Errorf("unprotected: %v", got)
	}
	if got := codes(setupWarnings(Settings{IncompleteDir: "/mnt/inc"}, map[string]any{"temp_path_enabled": false}, roots)); len(got) != 1 || got[0] != "incomplete-drift" {
		t.Errorf("drift: %v", got)
	}
	if got := codes(setupWarnings(Settings{IncompleteDir: "/mnt/inc"}, map[string]any{"temp_path_enabled": true, "temp_path": "/mnt/inc"}, roots)); len(got) != 0 {
		t.Errorf("in sync: %v", got)
	}
	if got := codes(setupWarnings(Settings{}, map[string]any{"save_path": "/mnt/Movies/dl"}, roots)); len(got) != 2 {
		t.Errorf("save in library: %v", got)
	}
}

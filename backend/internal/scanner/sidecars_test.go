package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindSidecarsMatchesOnlyTheVideosOwnFiles(t *testing.T) {
	dir := t.TempDir()
	touch := func(rel string) {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	for _, f := range []string{
		"Show S01E01.mkv", "Show S01E01.srt", "Show S01E01.en.srt", "Show S01E01.eng.sdh.srt", "Show S01E01.spanish.forced.ass",
		"Show S01E01.nb.vtt", "Show S01E01.txt", "Show S01E02.en.srt", "Show S01E010.en.srt", "Subs/Show S01E01.de.srt", "Subs/Show S01E02.de.srt",
	} {
		touch(f)
	}
	found := map[string]Sidecar{}
	for _, sc := range FindSidecars(filepath.Join(dir, "Show S01E01.mkv")) {
		found[strings.TrimPrefix(sc.Path, dir+"/")] = sc
	}
	want := []string{"Show S01E01.srt", "Show S01E01.en.srt", "Show S01E01.eng.sdh.srt", "Show S01E01.spanish.forced.ass", "Show S01E01.nb.vtt", "Subs/Show S01E01.de.srt"}
	if len(found) != len(want) {
		t.Errorf("found %d sidecars %v, want %d", len(found), found, len(want))
	}
	for _, w := range want {
		if _, ok := found[w]; !ok {
			t.Errorf("missing %s", w)
		}
	}
	if sc := found["Show S01E01.srt"]; sc.Language != "" {
		t.Errorf("a bare .srt has no language, got %q", sc.Language)
	}
	if sc := found["Show S01E01.eng.sdh.srt"]; sc.Language != "en" || sc.Title != "SDH" {
		t.Errorf("eng.sdh = %+v", sc)
	}
	if sc := found["Show S01E01.spanish.forced.ass"]; sc.Language != "es" || !sc.Forced {
		t.Errorf("spanish.forced = %+v", sc)
	}
	if sc := found["Subs/Show S01E01.de.srt"]; sc.Language != "de" {
		t.Errorf("Subs folder file = %+v", sc)
	}
	ids := map[string]bool{}
	for _, sc := range found {
		if ids[sc.ID] {
			t.Errorf("duplicate id %s", sc.ID)
		}
		ids[sc.ID] = true
	}
}

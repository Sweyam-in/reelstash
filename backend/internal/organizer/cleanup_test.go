package organizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanerWalk(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("categorized/A/a.mp4")
	write("uncategorized/b/b.nfo")  // skipped folder: untouched even without videos
	write("junk/readme.txt")        // removable
	write("junk/deep/x.nfo")        // folded into junk
	write("empty/.DS_Store")        // removable
	write("mixed/keep.mp4")         // has a video: stays
	write("mixed/old/leftover.jpg") // removable inside a kept folder
	write("mixed/notes.txt")        // companion of kept folder: stays
	write("known/x.txt")            // protected by a library row
	write("rootfile.txt")           // loose root file: untouched
	write(".hidden/x.txt")          // hidden: untouched

	c := &cleaner{mount: root, roots: []string{""}, skip: []string{"categorized", "uncategorized"}, protected: map[string]bool{"known": true}, plan: CleanupPlan{Items: []CleanupItem{}}}
	c.walk(context.Background(), root, "")

	got := map[string]CleanupItem{}
	for _, it := range c.plan.Items {
		got[it.Path] = it
	}
	if len(got) != 3 || got["junk"].Files != 2 || got["junk"].Dirs != 1 || got["empty"].Files != 1 || got["mixed/old"].Files != 1 {
		t.Fatalf("unexpected plan: %+v", c.plan.Items)
	}

	var res CleanupResult
	for _, it := range c.plan.Items {
		removeLeftovers(filepath.Join(root, filepath.FromSlash(it.Path)), &res)
	}
	if res.Files != 4 || res.Dirs != 4 {
		t.Fatalf("unexpected result: %+v", res)
	}
	for _, keep := range []string{"categorized/A/a.mp4", "uncategorized/b/b.nfo", "mixed/keep.mp4", "mixed/notes.txt", "known/x.txt", "rootfile.txt", ".hidden/x.txt"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Errorf("%s should survive: %v", keep, err)
		}
	}
}

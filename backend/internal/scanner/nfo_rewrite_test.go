package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteNFOPerformer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	src := `<?xml version="1.0"?>
<movie>
  <title>Keep &amp; This</title>
  <actor><name>Buck Richard</name></actor>
  <actor><name>Jane Roe</name></actor>
  <actor><name>buck richards</name></actor>
  <thumb>x</thumb>
</movie>`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := RewriteNFOPerformer(path, []string{"Buck Richard"}, "Buck Richards")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	out, _ := os.ReadFile(path)
	got := string(out)
	if strings.Count(got, "<actor>") != 2 || !strings.Contains(got, "<name>Buck Richards</name>") || strings.Contains(got, "Buck Richard<") {
		t.Fatalf("unexpected rewrite:\n%s", got)
	}
	if !strings.Contains(got, "Keep &amp; This") || !strings.Contains(got, "<thumb>x</thumb>") {
		t.Fatalf("other content changed:\n%s", got)
	}
	changed, err = RewriteNFOPerformer(path, []string{"Buck Richard"}, "Buck Richards")
	if err != nil || changed {
		t.Fatalf("second run changed=%v err=%v", changed, err)
	}
}

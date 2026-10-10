package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testUID  = "0b3c6c1e-5d0a-4f43-9d8e-2f5b6a7c8d90"
	otherUID = "11111111-2222-4333-8444-555555555555"
)

func TestSetNFOUIDAddsAndPreserves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	original := `<?xml version="1.0"?>
<movie>
  <title>Keep Me</title>
  <uniqueid type="tmdb" default="true">movie/603</uniqueid>
  <custom>untouched</custom>
</movie>
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := SetNFOUID(path, testUID)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	data, _ := os.ReadFile(path)
	doc := string(data)
	for _, want := range []string{"<title>Keep Me</title>", `<uniqueid type="tmdb" default="true">movie/603</uniqueid>`, "<custom>untouched</custom>"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("lost %q:\n%s", want, doc)
		}
	}
	if got := NFOUID(path); got != testUID {
		t.Fatalf("uid = %q", got)
	}

	// Idempotent.
	if changed, err := SetNFOUID(path, testUID); err != nil || changed {
		t.Fatalf("second call changed=%v err=%v", changed, err)
	}
}

func TestSetNFOUIDReplacesAndDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	original := `<movie>
  <uniqueid type='ReelStash'>` + otherUID + `</uniqueid>
  <title>T</title>
  <uniqueid type="reelstash">` + otherUID + `</uniqueid>
</movie>
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetNFOUID(path, testUID); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(strings.ToLower(string(data)), "reelstash"); n != 1 {
		t.Fatalf("want one reelstash id, got %d:\n%s", n, data)
	}
	if got := NFOUID(path); got != testUID {
		t.Fatalf("uid = %q", got)
	}
}

func TestSetNFOUIDRejectsNonMovie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	if err := os.WriteFile(path, []byte("https://example.com/title/1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetNFOUID(path, testUID); err != errNotMovieNFO {
		t.Fatalf("err = %v", err)
	}
}

func TestStubNFOIsIdentityOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	if err := writeStubNFO(path, testUID); err != nil {
		t.Fatal(err)
	}
	movie, err := parseNFO(path)
	if err != nil {
		t.Fatal(err)
	}
	if movie.ReelStashUID() != testUID || movie.hasMetadata() || len(movie.LinkHints()) != 0 {
		t.Fatalf("unexpected stub: %+v", movie)
	}
}

func TestWriteNFONeverDefaultsReelStashID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	ids := []NFOID{{Provider: ReelStashIDType, ExternalID: testUID}, {Provider: "tmdb", ExternalID: "movie/603"}}
	if err := WriteNFO(path, "T", "", "", "", 0, nil, nil, ids); err != nil {
		t.Fatal(err)
	}
	movie, err := parseNFO(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range movie.UniqueIDs {
		if (u.Default == "true") != (u.Type == "tmdb") {
			t.Fatalf("wrong default on %+v", u)
		}
	}
	if movie.ReelStashUID() != testUID || !movie.hasMetadata() {
		t.Fatalf("unexpected movie: %+v", movie)
	}
}

func TestJellyfinProviderTagsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	ids := []NFOID{
		{Provider: "x-porndb", ExternalID: "movies/abc123"},
		{Provider: "tmdb", ExternalID: "movie/603"},
		{Provider: ReelStashIDType, ExternalID: testUID},
	}
	if err := WriteNFO(path, "Film", "", "", "", 0, nil, nil, ids); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"<theporndbid>movies/abc123</theporndbid>", "<tmdbid>603</tmdbid>"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q from %s", want, data)
		}
	}
	movie, err := parseNFO(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := movie.LinkHints(); len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatalf("link hints = %+v", got)
	}
}

func TestAddNFOProviderTagsPreservesExistingMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	before := `<movie>
  <title>Keep this title</title>
  <custom>untouched</custom>
</movie>
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	ids := []NFOID{{Provider: "x-porndb", ExternalID: "scenes/abc123"}, {Provider: "tmdb", ExternalID: "movie/603"}}
	changed, err := AddNFOProviderTags(path, ids)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"<title>Keep this title</title>", "<custom>untouched</custom>", "<theporndbid>scenes/abc123</theporndbid>", "<tmdbid>603</tmdbid>"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q from %s", want, data)
		}
	}
	if changed, err := AddNFOProviderTags(path, ids); err != nil || changed {
		t.Fatalf("second call changed=%v err=%v", changed, err)
	}
}

func TestJellyfinOnlyProviderTagsImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	if err := os.WriteFile(path, []byte(`<movie><theporndbid>movies/abc123</theporndbid><tmdbid>603</tmdbid></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	movie, err := parseNFO(path)
	if err != nil {
		t.Fatal(err)
	}
	got := movie.LinkHints()
	if len(got) != 2 || got[0] != (NFOID{Provider: "tmdb", ExternalID: "movie/603"}) || got[1] != (NFOID{Provider: "x-porndb", ExternalID: "movies/abc123"}) {
		t.Fatalf("link hints = %+v", got)
	}
}

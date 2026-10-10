package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditNFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nfo")
	original := `<?xml version="1.0"?>
<movie>
  <title>Old</title>
  <genre>A</genre>
  <tag>B</tag>
  <actor><name>Ann</name><role>x</role></actor>
  <actor><name>Bob</name></actor>
</movie>
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	title, studio := "New & Better", "Studio X"
	genres := []string{"C", "D"}
	cast := []string{"Cy", "Ann"}
	changed, err := EditNFO(path, NFOFields{Title: &title, Studio: &studio, Genres: &genres, Performers: &cast})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	movie, err := parseNFO(path)
	if err != nil {
		t.Fatal(err)
	}
	if movie.Title != title || movie.Studio != studio || len(movie.Genres) != 2 || len(movie.Tags) != 0 {
		t.Fatalf("unexpected movie: %+v", movie)
	}
	if len(movie.Actors) != 2 || movie.Actors[0].Name != "Cy" || movie.Actors[1].Name != "Ann" {
		t.Fatalf("unexpected cast: %+v", movie.Actors)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "<role>x</role>") {
		t.Fatal("kept actor lost its extra fields")
	}
}

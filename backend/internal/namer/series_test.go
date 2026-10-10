package namer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/tmdb"
)

func TestPartHint(t *testing.T) {
	cases := []struct {
		in     string
		label  string
		number int
	}{
		{"Big Movie - Scene 3.mp4", "", 3},
		{"big.movie.sc02.1080p.mp4", "", 2},
		{"Show.S01E04.mkv", "S01E04", 0},
		{"Part #5", "", 5},
		{"Just a title.mp4", "", 0},
	}
	for _, c := range cases {
		label, number := partHint(c.in)
		if label != c.label || number != c.number {
			t.Errorf("partHint(%q) = %q, %d; want %q, %d", c.in, label, number, c.label, c.number)
		}
	}
}

func TestSuggestPart(t *testing.T) {
	parts := []SeriesPartView{
		{SeriesPart: provider.SeriesPart{Number: 1, Label: "Scene 1", Title: "Opening Night"}},
		{SeriesPart: provider.SeriesPart{Number: 2, Label: "Scene 2", Title: "The Pool Party"}},
		{SeriesPart: provider.SeriesPart{Number: 3, Label: "Scene 3", Title: "Finale"}},
	}
	if got := suggestPart(seriesVideo{title: "Movie", fileName: "movie.scene.3.mp4"}, parts, map[int]bool{}); got != 2 {
		t.Errorf("by number = %d, want 2", got)
	}
	if got := suggestPart(seriesVideo{title: "The Pool Party"}, parts, map[int]bool{}); got != 1 {
		t.Errorf("by title = %d, want 1", got)
	}
	if got := suggestPart(seriesVideo{title: "The Pool Party"}, parts, map[int]bool{1: true}); got != -1 {
		t.Errorf("taken part = %d, want -1", got)
	}
	pos := int32(1)
	if got := suggestPart(seriesVideo{title: "unrelated", position: &pos}, parts, map[int]bool{}); got != 0 {
		t.Errorf("by position = %d, want 0", got)
	}
}

func TestMediaKind(t *testing.T) {
	cases := map[string]string{
		"scenes/abc": "scene", "movies/abc": "movie", "movie/12": "movie",
		"tv/7": "show", "tv/7/1/2": "episode", "other": "title",
	}
	for id, want := range cases {
		if got := mediaKind(provider.Ref{ExternalID: id}); got != want {
			t.Errorf("mediaKind(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestSearchSeriesIncludesTitles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/tv":
			w.Write([]byte(`{"results":[{"id":7,"name":"Saga"}]}`))
		case "/search/collection":
			w.Write([]byte(`{"results":[]}`))
		case "/search/movie":
			w.Write([]byte(`{"results":[{"id":1,"title":"Saga Part One"}]}`))
		default:
			w.Write([]byte(`{"results":[]}`))
		}
	}))
	defer srv.Close()
	reg := provider.NewRegistry()
	reg.Register(tmdb.NewProvider(tmdb.New(srv.URL, "key")))
	e := &Engine{Providers: reg}

	got, err := e.SearchSeries(context.Background(), "Saga", nil)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, c := range got {
		kinds = append(kinds, c.ExternalID+"="+c.Kind)
	}
	if len(got) != 2 || got[0].ExternalID != "tv/7" || !got[0].IsSeries || got[1].ExternalID != "movie/1" || got[1].Kind != "movie" || got[1].IsSeries {
		t.Errorf("results = %v, want the show first, then the movie title", kinds)
	}
}

func TestIsSeriesID(t *testing.T) {
	for id, want := range map[string]bool{
		"movies/a": true, "collection/9": true, "tv/7": true,
		"tv/7/1/2": false, "movie/1": false, "scenes/a": false,
	} {
		if got := isSeriesID(id); got != want {
			t.Errorf("isSeriesID(%q) = %v, want %v", id, got, want)
		}
	}
}

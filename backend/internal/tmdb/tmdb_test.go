package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reelstash/backend/internal/provider"
)

func TestParseFileName(t *testing.T) {
	cases := []struct {
		in   string
		want FileInfo
	}{
		{"The.Matrix.1999.1080p.BluRay.x264.mkv", FileInfo{Title: "The Matrix", Year: 1999}},
		{"Blade Runner 2049 (2017) [2160p].mp4", FileInfo{Title: "Blade Runner 2049", Year: 2017}},
		{"Breaking.Bad.S02E05.720p.HDTV.mkv", FileInfo{Title: "Breaking Bad", Season: 2, Episode: 5}},
		{"Some Show - 3x07 - Title.mkv", FileInfo{Title: "Some Show", Season: 3, Episode: 7}},
		{"Show.Name.2019.S01E02.WEB-DL.mkv", FileInfo{Title: "Show Name", Year: 2019, Season: 1, Episode: 2}},
		{"2012.2009.1080p.mkv", FileInfo{Title: "2012", Year: 2009}},
		{"1917.mkv", FileInfo{Title: "1917"}},
		{"Just A Title.mp4", FileInfo{Title: "Just A Title"}},
	}
	for _, c := range cases {
		if got := ParseFileName(c.in); got != c.want {
			t.Errorf("ParseFileName(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	if s := similarity("The Matrix", "the matrix"); s != 100 {
		t.Errorf("identical = %v", s)
	}
	if s := similarity("Matrix The", "The Matrix"); s != 100 {
		t.Errorf("word order = %v", s)
	}
	if s := similarity("The Matrix", "Finding Nemo"); s > 40 {
		t.Errorf("unrelated = %v", s)
	}
}

func fakeTMDB(t *testing.T) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "key123" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"status_message":"Invalid API key"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/search/movie"):
			if r.URL.Query().Get("query") != "The Matrix" {
				t.Errorf("query = %q", r.URL.Query().Get("query"))
			}
			w.Write([]byte(`{"results":[
				{"id":603,"title":"The Matrix","release_date":"1999-03-30","poster_path":"/m.jpg"},
				{"id":604,"title":"The Matrix Reloaded","release_date":"2003-05-15"}]}`))
		case strings.HasSuffix(r.URL.Path, "/movie/603"):
			w.Write([]byte(`{"id":603,"title":"The Matrix","overview":"A hacker.","release_date":"1999-03-30",
				"poster_path":"/m.jpg","backdrop_path":"/b.jpg","genres":[{"name":"Action"}],
				"production_companies":[{"name":"Warner Bros."}],
				"credits":{"cast":[{"name":"Keanu Reeves","profile_path":"/k.jpg"},{"name":"Carrie-Anne Moss"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/tv/1396"):
			w.Write([]byte(`{"id":1396,"name":"Breaking Bad","networks":[{"name":"AMC"}],"genres":[{"name":"Drama"}],
				"credits":{"cast":[{"name":"Bryan Cranston"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/tv/1396/season/2/episode/5"):
			w.Write([]byte(`{"name":"Breakage","overview":"Ep.","air_date":"2009-04-05","still_path":"/s.jpg",
				"guest_stars":[{"name":"Guest One"}]}`))
		case strings.HasSuffix(r.URL.Path, "/person/1"):
			w.Write([]byte(`{"id":1,"name":"Keanu Reeves","biography":"Actor.","birthday":"1964-09-02",
				"place_of_birth":"Beirut","also_known_as":["Keanu"],"gender":2,"profile_path":"/p.jpg"}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return NewProvider(New(srv.URL, "key123"))
}

func TestSearchMovieAutoMatch(t *testing.T) {
	p := fakeTMDB(t)
	cands, err := p.SearchMedia(context.Background(), provider.MediaQuery{FileName: "The.Matrix.1999.1080p.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].Result.Ref.ExternalID != "movie/603" {
		t.Fatalf("candidates = %+v", cands)
	}
	if !cands[0].AutoMatch || !cands[0].DateHit {
		t.Errorf("expected a confident match with year hit: %+v", cands[0])
	}
	if cands[1].AutoMatch {
		t.Errorf("a different film must not auto-match")
	}
}

func TestSearchWithoutYearNeedsHumanWhenAmbiguous(t *testing.T) {
	p := fakeTMDB(t)
	cands, err := p.SearchMedia(context.Background(), provider.MediaQuery{Text: "The Matrix"})
	if err != nil {
		t.Fatal(err)
	}
	// "The Matrix" vs "The Matrix Reloaded" are far apart, so the exact hit still auto-matches.
	if !cands[0].AutoMatch {
		t.Errorf("exact unambiguous title should match: %+v", cands[0])
	}
}

func TestManualSearchTypeFiltersCatalogs(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/movie":
			w.Write([]byte(`{"results":[{"id":603,"title":"The Matrix"}]}`))
		case "/search/tv":
			w.Write([]byte(`{"results":[{"id":1396,"name":"The Matrix"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "test-key"))
	for _, tc := range []struct {
		typeName  string
		wantPaths []string
		wantKinds []string
	}{
		{"all", []string{"/search/movie", "/search/tv"}, []string{"movie", "show"}},
		{"movies", []string{"/search/movie"}, []string{"movie"}},
		{"shows", []string{"/search/tv"}, []string{"show"}},
		{"scenes", nil, nil},
		{"jav", nil, nil},
	} {
		paths = nil
		got, err := p.SearchMedia(context.Background(), provider.MediaQuery{Text: "The Matrix", SearchType: tc.typeName})
		if err != nil {
			t.Fatalf("%s: %v", tc.typeName, err)
		}
		if strings.Join(paths, ",") != strings.Join(tc.wantPaths, ",") || len(got) != len(tc.wantKinds) {
			t.Fatalf("%s paths=%v candidates=%+v", tc.typeName, paths, got)
		}
		for i, want := range tc.wantKinds {
			if got[i].Result.Kind != want {
				t.Fatalf("%s kind[%d] = %q, want %q", tc.typeName, i, got[i].Result.Kind, want)
			}
		}
	}
}

func TestGetMediaMovieAndEpisode(t *testing.T) {
	p := fakeTMDB(t)
	m, err := p.GetMedia(context.Background(), "movie/603")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "The Matrix" || m.Studio != "Warner Bros." || len(m.Performers) != 2 ||
		m.PerformerImages["Keanu Reeves"] != "https://image.tmdb.org/t/p/w185/k.jpg" ||
		m.PosterURL != "https://image.tmdb.org/t/p/w500/m.jpg" || m.Tags[0] != "Action" {
		t.Errorf("movie = %+v", m)
	}
	ep, err := p.GetMedia(context.Background(), "tv/1396/2/5")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Title != "Breaking Bad - S02E05 - Breakage" || ep.Studio != "AMC" || ep.Date != "2009-04-05" ||
		ep.Performers[0] != "Guest One" || ep.Ref.ExternalID != "tv/1396/2/5" {
		t.Errorf("episode = %+v", ep)
	}
	if _, err := p.GetMedia(context.Background(), "movie/../etc"); err == nil {
		t.Error("expected an error for a malformed id")
	}
}

func TestGetPerformerAndAuthError(t *testing.T) {
	p := fakeTMDB(t)
	got, err := p.GetPerformer(context.Background(), "1")
	if err != nil || got.Gender != "male" || got.Birthdate != "1964-09-02" || got.Aliases[0] != "Keanu" {
		t.Fatalf("performer = %+v, err = %v", got, err)
	}
	bad := NewProvider(New(strings.Replace(p.Client.baseURL, "", "", 0), "wrong"))
	if err := bad.Test(context.Background()); err == nil || strings.Contains(err.Error(), "wrong") {
		t.Errorf("Test with a bad key should fail without leaking it, got %v", err)
	}
}

func TestIncludeAdultSetting(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("include_adult"))
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "k"))
	search := func() {
		if _, err := p.SearchPerformers(context.Background(), "x"); err != nil {
			t.Fatal(err)
		}
	}
	search() // default: included
	if err := p.Configure(map[string]string{"includeAdult": "true"}); err != nil {
		t.Fatal(err)
	}
	search()
	p.Configure(map[string]string{"includeAdult": "false"})
	search()
	if strings.Join(got, ",") != "true,true,false" {
		t.Errorf("include_adult sent = %v, want true,true,false", got)
	}
	var found bool
	for _, f := range p.ConfigFields() {
		found = found || f.Key == "includeAdult" && f.Type == "bool"
	}
	if !found {
		t.Error("includeAdult must be advertised as a bool setting")
	}
}

func TestCastNamesKeepsPersonRefs(t *testing.T) {
	names, _, refs := castNames([]castMember{{ID: 42, Name: "Drake North"}, {Name: "No Id"}})
	if len(names) != 2 || refs["Drake North"].ExternalID != "42" || refs["Drake North"].Provider != ProviderID {
		t.Errorf("names = %v, refs = %v", names, refs)
	}
	if _, ok := refs["No Id"]; ok {
		t.Error("a cast member without an id has no ref")
	}
}

package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSeriesShowAndCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/7":
			w.Write([]byte(`{"id":7,"name":"Show","first_air_date":"2020-01-01","seasons":[{"season_number":0,"episode_count":3},{"season_number":1,"episode_count":2},{"season_number":2,"episode_count":1}]}`))
		case "/tv/7/season/1":
			w.Write([]byte(`{"episodes":[{"name":"Pilot","episode_number":1,"air_date":"2020-01-01"},{"name":"Two","episode_number":2}]}`))
		case "/tv/7/season/2":
			w.Write([]byte(`{"episodes":[{"name":"Three","episode_number":1}]}`))
		case "/movie/1":
			w.Write([]byte(`{"id":1,"title":"First","belongs_to_collection":{"id":9}}`))
		case "/movie/5":
			w.Write([]byte(`{"id":5,"title":"Standalone"}`))
		case "/collection/9":
			w.Write([]byte(`{"id":9,"name":"Saga","parts":[{"id":2,"title":"Second","release_date":"2003-01-01"},{"id":3,"title":"Undated"},{"id":1,"title":"First","release_date":"2001-01-01"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "key123"))

	show, err := p.GetSeries(context.Background(), "tv/7")
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Parts) != 3 {
		t.Fatalf("parts = %d, want 3 (specials skipped)", len(show.Parts))
	}
	if got := show.Parts[2]; got.Number != 3 || got.Label != "S02E01" || got.Ref.ExternalID != "tv/7/2/1" {
		t.Errorf("third part = %+v", got)
	}

	coll, err := p.GetSeries(context.Background(), "collection/9")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, part := range coll.Parts {
		order = append(order, part.Title)
	}
	if len(order) != 3 || order[0] != "First" || order[1] != "Second" || order[2] != "Undated" {
		t.Errorf("collection order = %v", order)
	}
	if coll.Parts[0].Ref.ExternalID != "movie/1" || coll.Date != "2001-01-01" {
		t.Errorf("first part = %+v, date %q", coll.Parts[0], coll.Date)
	}

	if fromEpisode, err := p.GetSeries(context.Background(), "tv/7/2/1"); err != nil || fromEpisode.Ref.ExternalID != "tv/7" {
		t.Errorf("episode -> show = %+v, %v", fromEpisode.Ref, err)
	}
	if fromMovie, err := p.GetSeries(context.Background(), "movie/1"); err != nil || fromMovie.Ref.ExternalID != "collection/9" {
		t.Errorf("movie -> collection = %+v, %v", fromMovie.Ref, err)
	}
	if _, err := p.GetSeries(context.Background(), "movie/5"); err == nil {
		t.Error("a movie outside any collection is not a series")
	}
}

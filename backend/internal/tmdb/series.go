package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"reelstash/backend/internal/provider"
)

var _ provider.SeriesProvider = (*Provider)(nil)

// TMDB series are TV shows ("tv/<id>", parts are episodes) and movie
// collections ("collection/<id>", parts are the films).

const maxSeasons = 40

func (p *Provider) SearchSeries(ctx context.Context, query string) ([]provider.SeriesResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	var (
		wg               sync.WaitGroup
		shows            []searchTV
		colls            []searchCollection
		showErr, collErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		var resp struct {
			Results []searchTV `json:"results"`
		}
		showErr = p.Client.get(ctx, "search/tv", url.Values{"query": {query}, "include_adult": {p.Client.adultParam()}}, &resp)
		shows = resp.Results
	}()
	go func() {
		defer wg.Done()
		var resp struct {
			Results []searchCollection `json:"results"`
		}
		collErr = p.Client.get(ctx, "search/collection", url.Values{"query": {query}, "include_adult": {p.Client.adultParam()}}, &resp)
		colls = resp.Results
	}()
	wg.Wait()
	if showErr != nil && collErr != nil {
		return nil, showErr
	}

	type scored struct {
		res   provider.SeriesResult
		score float64
	}
	var all []scored
	for _, r := range shows {
		all = append(all, scored{provider.SeriesResult{
			Ref: tvRef(r.ID, 0, 0), Title: r.Name, Description: r.Overview, Date: r.FirstAirDate,
			Kind: "show", PosterURL: imageURL("w342", r.PosterPath),
		}, max(similarity(query, r.Name), similarity(query, r.OriginalName))})
	}
	for _, r := range colls {
		all = append(all, scored{provider.SeriesResult{
			Ref:   provider.Ref{Provider: ProviderID, ExternalID: "collection/" + strconv.FormatInt(r.ID, 10)},
			Title: r.Name, Description: r.Overview, Kind: "collection", PosterURL: imageURL("w342", r.PosterPath),
		}, similarity(query, r.Name)})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]provider.SeriesResult, 0, len(all))
	for i, s := range all {
		if i >= 15 {
			break
		}
		out = append(out, s.res)
	}
	return out, nil
}

// errNoSeries means a title record belongs to no series.
var errNoSeries = fmt.Errorf("tmdb: this title is not part of a series")

// GetSeries loads a show ("tv/<id>") or collection ("collection/<id>"). An
// episode id loads its show, and a movie id loads the collection the movie
// belongs to.
func (p *Provider) GetSeries(ctx context.Context, externalID string) (provider.SeriesResult, error) {
	parts := strings.Split(externalID, "/")
	if len(parts) == 4 && parts[0] == "tv" { // an episode: its show
		parts = parts[:2]
	}
	if len(parts) == 2 && parts[0] == "movie" {
		if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
			return provider.SeriesResult{}, fmt.Errorf("invalid tmdb id %q", externalID)
		}
		var d movieDetail
		if err := p.Client.get(ctx, "movie/"+parts[1], nil, &d); err != nil {
			return provider.SeriesResult{}, err
		}
		if d.BelongsToCollection == nil || d.BelongsToCollection.ID < 1 {
			return provider.SeriesResult{}, errNoSeries
		}
		return p.getCollection(ctx, strconv.FormatInt(d.BelongsToCollection.ID, 10))
	}
	if len(parts) != 2 {
		return provider.SeriesResult{}, fmt.Errorf("invalid tmdb series id %q", externalID)
	}
	if id, err := strconv.ParseInt(parts[1], 10, 64); err != nil || id < 1 {
		return provider.SeriesResult{}, fmt.Errorf("invalid tmdb series id %q", externalID)
	}
	switch parts[0] {
	case "tv":
		return p.getShow(ctx, parts[1])
	case "collection":
		return p.getCollection(ctx, parts[1])
	}
	return provider.SeriesResult{}, fmt.Errorf("invalid tmdb series id %q", externalID)
}

func (p *Provider) getShow(ctx context.Context, id string) (provider.SeriesResult, error) {
	var d tvDetail
	if err := p.Client.get(ctx, "tv/"+id, nil, &d); err != nil {
		return provider.SeriesResult{}, err
	}
	res := provider.SeriesResult{
		Ref: tvRef(d.ID, 0, 0), Title: d.Name, Description: d.Overview, Date: d.FirstAirDate,
		Studio: firstName(d.Networks, d.ProductionCompanies), Kind: "show",
		PosterURL: imageURL("w500", d.PosterPath), BackdropURL: imageURL("w1280", d.BackdropPath), SourceURL: "https://www.themoviedb.org/tv/" + id,
	}
	n := 0
	seasons := 0
	for _, s := range d.Seasons {
		if s.SeasonNumber < 1 || s.EpisodeCount == 0 { // skip specials
			continue
		}
		if seasons++; seasons > maxSeasons {
			break
		}
		var sd seasonDetail
		if err := p.Client.get(ctx, fmt.Sprintf("tv/%s/season/%d", id, s.SeasonNumber), nil, &sd); err != nil {
			return provider.SeriesResult{}, err
		}
		for _, ep := range sd.Episodes {
			if ep.EpisodeNum < 1 {
				continue
			}
			n++
			res.Parts = append(res.Parts, provider.SeriesPart{
				Ref:       tvRef(d.ID, s.SeasonNumber, ep.EpisodeNum),
				Number:    n,
				Label:     fmt.Sprintf("S%02dE%02d", s.SeasonNumber, ep.EpisodeNum),
				Title:     ep.Name,
				Date:      ep.AirDate,
				PosterURL: imageURL("w300", ep.StillPath),
			})
		}
	}
	res.PartCount = len(res.Parts)
	return res, nil
}

func (p *Provider) getCollection(ctx context.Context, id string) (provider.SeriesResult, error) {
	var d collectionDetail
	if err := p.Client.get(ctx, "collection/"+id, nil, &d); err != nil {
		return provider.SeriesResult{}, err
	}
	films := append([]searchMovie(nil), d.Parts...)
	sort.SliceStable(films, func(i, j int) bool {
		a, b := films[i].ReleaseDate, films[j].ReleaseDate
		if a == "" || b == "" {
			return b == "" && a != "" // undated films go last
		}
		return a < b
	})
	res := provider.SeriesResult{
		Ref:   provider.Ref{Provider: ProviderID, ExternalID: "collection/" + id},
		Title: d.Name, Description: d.Overview, Kind: "collection",
		PosterURL: imageURL("w500", d.PosterPath), BackdropURL: imageURL("w1280", d.BackdropPath), SourceURL: "https://www.themoviedb.org/collection/" + id,
	}
	for i, f := range films {
		res.Parts = append(res.Parts, provider.SeriesPart{
			Ref: movieRef(f.ID), Number: i + 1, Label: fmt.Sprintf("Part %d", i+1),
			Title: f.Title, Date: f.ReleaseDate, PosterURL: imageURL("w300", f.PosterPath),
		})
	}
	if len(films) > 0 {
		res.Date = films[0].ReleaseDate
	}
	res.PartCount = len(res.Parts)
	return res, nil
}

package namer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reelstash/backend/internal/provider"
	"reelstash/backend/internal/titlematch"
)

// SeriesCandidate is one provider search hit for a series.
type SeriesCandidate struct {
	provider.SeriesResult
	Provider   string `json:"provider"`
	ExternalID string `json:"externalId"`
	// IsSeries is false for a single title (a scene, episode or film). It can
	// still be used: as the series' own metadata, or for its parent series.
	IsSeries bool `json:"isSeries"`
}

// seriesProviders is every provider that can match series or titles (a title
// can stand for a series: its parent movie, show or collection, or itself),
// limited to ids when given.
func (e *Engine) seriesProviders(ids []string) []provider.Provider {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []provider.Provider
	for _, p := range e.Providers.All() {
		_, series := p.(provider.SeriesProvider)
		if !p.Enabled() || !series && !p.Capabilities().Media || len(ids) > 0 && !want[p.ID()] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// SeriesEnabled reports whether any configured provider can match series.
func (e *Engine) SeriesEnabled() bool {
	return e != nil && len(e.seriesProviders(nil)) > 0
}

// mediaKind names what a title record is, from its provider id.
func mediaKind(ref provider.Ref) string {
	id := ref.ExternalID
	switch {
	case strings.HasPrefix(id, "scenes/"):
		return "scene"
	case strings.HasPrefix(id, "movies/"), strings.HasPrefix(id, "movie/"):
		return "movie"
	case strings.HasPrefix(id, "tv/") && strings.Count(id, "/") >= 3:
		return "episode"
	case strings.HasPrefix(id, "tv/"):
		return "show"
	}
	return "title"
}

// isSeriesID reports whether a provider id is itself a series with parts: a
// provider movie, a TMDB show or a TMDB collection.
func isSeriesID(id string) bool {
	switch {
	case strings.HasPrefix(id, "movies/"), strings.HasPrefix(id, "collection/"):
		return true
	case strings.HasPrefix(id, "tv/"):
		return strings.Count(id, "/") == 1
	}
	return false
}

// SearchSeries searches the given providers (every capable one when empty) in
// parallel, for series records and for titles alike, so a series can also be
// matched from one of its scenes, episodes or films. Series hits come first
// per provider; a title that is also a series hit is listed once. Failing
// searches are skipped unless all of them fail.
func (e *Engine) SearchSeries(ctx context.Context, query string, providerIDs []string) ([]SeriesCandidate, error) {
	providers := e.seriesProviders(providerIDs)
	if len(providers) == 0 {
		return nil, errors.New("no metadata provider is configured")
	}
	type outcome struct {
		series    []provider.SeriesResult
		media     []provider.MediaCandidate
		seriesErr error
		mediaErr  error
		tried     int
	}
	results := make([]outcome, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		if sp, ok := p.(provider.SeriesProvider); ok {
			results[i].tried++
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i].series, results[i].seriesErr = sp.SearchSeries(ctx, query)
			}(i)
		}
		if p.Capabilities().Media {
			results[i].tried++
			wg.Add(1)
			go func(i int, p provider.Provider) {
				defer wg.Done()
				results[i].media, results[i].mediaErr = p.SearchMedia(ctx, provider.MediaQuery{Text: query})
			}(i, p)
		}
	}
	wg.Wait()

	var out []SeriesCandidate
	seen := map[string]bool{}
	add := func(r provider.SeriesResult, isSeries bool) {
		key := r.Ref.Provider + "\x00" + r.Ref.ExternalID
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, SeriesCandidate{SeriesResult: r, Provider: r.Ref.Provider, ExternalID: r.Ref.ExternalID, IsSeries: isSeries})
	}
	var firstErr error
	tried, failed := 0, 0
	for _, r := range results {
		tried += r.tried
		for _, err := range []error{r.seriesErr, r.mediaErr} {
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		for _, s := range r.series {
			add(s, true)
		}
		media := append([]provider.MediaCandidate(nil), r.media...)
		sort.SliceStable(media, func(i, j int) bool { return media[i].Score > media[j].Score })
		for _, c := range media {
			add(provider.SeriesResult{
				Ref: c.Result.Ref, Title: c.Result.Title, Description: c.Result.Description, Date: c.Result.Date,
				Studio: c.Result.Studio, Kind: mediaKind(c.Result.Ref), PosterURL: c.Result.PosterURL, SourceURL: c.Result.SourceURL,
			}, isSeriesID(c.Result.Ref.ExternalID))
		}
	}
	if failed == tried && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// getSeries loads a series record with its parts. A series record (movie,
// show, collection) loads as itself. A single title is used as it is — its
// metadata becomes the series' and it is the one part — unless parent asks for
// the movie, show or collection it belongs to (see the providers' GetSeries).
func (e *Engine) getSeries(ctx context.Context, providerID, externalID string, parent bool) (provider.SeriesResult, error) {
	p, ok := e.Providers.Get(providerID)
	if !ok || !p.Enabled() {
		return provider.SeriesResult{}, fmt.Errorf("provider %q is not configured", providerID)
	}
	if sp, ok := p.(provider.SeriesProvider); ok && (parent || isSeriesID(externalID)) {
		res, err := sp.GetSeries(ctx, externalID)
		if err != nil {
			return provider.SeriesResult{}, fmt.Errorf("fetch from %s: %w", p.Name(), err)
		}
		return res, nil
	}
	if parent {
		return provider.SeriesResult{}, fmt.Errorf("%s cannot look up the series of a title", p.Name())
	}
	if !p.Capabilities().Media {
		return provider.SeriesResult{}, fmt.Errorf("%s does not support titles", p.Name())
	}
	m, err := p.GetMedia(ctx, externalID)
	if err != nil {
		return provider.SeriesResult{}, fmt.Errorf("fetch from %s: %w", p.Name(), err)
	}
	kind := mediaKind(m.Ref)
	return provider.SeriesResult{
		Ref: m.Ref, Title: m.Title, Description: m.Description, Date: m.Date, Studio: m.Studio,
		Kind: kind, PosterURL: m.PosterURL, BackdropURL: m.BackgroundURL, SourceURL: m.SourceURL, PartCount: 1,
		Parts: []provider.SeriesPart{{
			Ref: m.Ref, Number: 1, Label: strings.ToUpper(kind[:1]) + kind[1:], Title: m.Title,
			Date: m.Date, PosterURL: m.PosterURL, Performers: m.Performers,
		}},
	}, nil
}

// LinkSeries links a series to a provider record (a series, a single title,
// or with parent the series that title belongs to) and stores its parts on the
// link. The series' description, release date, studio (an existing one with
// the same name), poster and backdrop are taken from the record when it has them; with rename the
// series also takes the record's title.
func (e *Engine) LinkSeries(ctx context.Context, seriesID int64, providerID, externalID string, rename, parent bool) (provider.SeriesResult, error) {
	res, err := e.getSeries(ctx, providerID, externalID, parent)
	if err != nil {
		return res, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return res, err
	}
	if err := upsertLink(ctx, e.DB, "series", seriesID, res.Ref, raw); err != nil {
		return res, err
	}
	var date *time.Time
	if t, err := time.Parse("2006-01-02", res.Date); err == nil {
		date = &t
	}
	name := ""
	if rename {
		name = strings.TrimSpace(res.Title)
	}
	_, err = e.DB.Exec(ctx, `
		UPDATE series SET
			name = COALESCE(NULLIF($2, ''), name),
			description = COALESCE(NULLIF($3, ''), description),
			release_date = COALESCE($4, release_date),
			studio_id = COALESCE((SELECT id FROM studio WHERE lower(name) = lower($5) AND $5 <> '' LIMIT 1), studio_id),
			updated_at = now()
		WHERE id = $1
	`, seriesID, name, res.Description, date, res.Studio)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return res, fmt.Errorf("another series is already named %q", name)
		}
		return res, err
	}
	if res.PosterURL != "" {
		if err := e.downloadSeriesPoster(ctx, seriesID, res.PosterURL, false); err != nil {
			fmt.Printf("namer: series poster download failed for series %d: %v\n", seriesID, err)
		}
	}
	if res.BackdropURL != "" {
		if err := e.downloadSeriesBackdrop(ctx, seriesID, res.BackdropURL); err != nil {
			fmt.Printf("namer: series backdrop download failed for series %d: %v\n", seriesID, err)
		}
	}
	return res, nil
}

// downloadSeriesPoster stores a provider poster as the series poster. With
// custom it counts as the user's choice (later matches keep it); otherwise it
// never replaces a poster the user chose.
func (e *Engine) downloadSeriesPoster(ctx context.Context, seriesID int64, posterURL string, custom bool) error {
	body, mime, err := downloadPoster(ctx, posterURL)
	if err != nil {
		return err
	}
	return e.storeLinkedImageBytes(ctx, "series", "poster_image_id", "POSTER", seriesID, body, mime, true, custom)
}

// downloadSeriesBackdrop stores a provider backdrop as the series backdrop,
// never replacing one the user chose.
func (e *Engine) downloadSeriesBackdrop(ctx context.Context, seriesID int64, backdropURL string) error {
	body, mime, err := downloadPoster(ctx, backdropURL)
	if err != nil {
		return err
	}
	return e.storeLinkedImageBytes(ctx, "series", "backdrop_image_id", "BACKDROP", seriesID, body, mime, true, false)
}

// downloadPoster fetches a provider's poster image. Like title posters, any
// https host the provider's record points at is accepted (covers are often on
// a CDN of their own); the body must be an image within the size limit.
func downloadPoster(ctx context.Context, posterURL string) ([]byte, string, error) {
	parsed, err := url.Parse(posterURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, "", errors.New("the provider's poster address is not a secure image link")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, posterURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "reelstash-namer/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("poster download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("poster download returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPerformerPhotoBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return nil, "", errors.New("the poster is empty or too large")
	}
	mime := http.DetectContentType(body)
	if !strings.HasPrefix(mime, "image/") {
		return nil, "", errors.New("the poster is not an image")
	}
	return body, mime, nil
}

// SetSeriesPosterUpload stores an uploaded image as the series poster.
func (e *Engine) SetSeriesPosterUpload(ctx context.Context, seriesID int64, body []byte) error {
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return fmt.Errorf("image is empty or larger than %d MB", maxPerformerPhotoBytes>>20)
	}
	mime := http.DetectContentType(body)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return fmt.Errorf("upload a JPEG, PNG, WebP or GIF image")
	}
	return e.storeLinkedImageBytes(ctx, "series", "poster_image_id", "POSTER", seriesID, body, mime, true, true)
}

// SetSeriesBackdropUpload stores an uploaded image as the series backdrop.
func (e *Engine) SetSeriesBackdropUpload(ctx context.Context, seriesID int64, body []byte) error {
	if len(body) == 0 || len(body) > maxPerformerPhotoBytes {
		return fmt.Errorf("image is empty or larger than %d MB", maxPerformerPhotoBytes>>20)
	}
	mime := http.DetectContentType(body)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return fmt.Errorf("upload a JPEG, PNG, WebP or GIF image")
	}
	return e.storeLinkedImageBytes(ctx, "series", "backdrop_image_id", "BACKDROP", seriesID, body, mime, true, true)
}

// SeriesProviderPosters lists the poster of each provider record the series
// is linked to: provider id -> image URL.
func (e *Engine) SeriesProviderPosters(ctx context.Context, seriesID int64) map[string]string {
	out := map[string]string{}
	rows, err := e.DB.Query(ctx, `SELECT provider, remote_metadata FROM match_link WHERE kind = 'series' AND entity_id = $1`, seriesID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var providerID string
		var raw []byte
		if rows.Scan(&providerID, &raw) != nil {
			continue
		}
		var res provider.SeriesResult
		if json.Unmarshal(raw, &res) == nil && res.PosterURL != "" {
			out[providerID] = res.PosterURL
		}
	}
	return out
}

// UseSeriesProviderPoster makes the linked provider record's poster the
// series poster.
func (e *Engine) UseSeriesProviderPoster(ctx context.Context, seriesID int64, providerID string) error {
	posterURL := e.SeriesProviderPosters(ctx, seriesID)[providerID]
	if posterURL == "" {
		return errors.New("that provider has no poster for this series")
	}
	return e.downloadSeriesPoster(ctx, seriesID, posterURL, true)
}

// SeriesPartView is one provider part and the video paired with it, if any.
type SeriesPartView struct {
	provider.SeriesPart
	Provider    string `json:"provider"`
	ExternalID  string `json:"externalId"`
	MediaItemID *int64 `json:"mediaItemId,omitempty"`
	// SuggestedMediaItemID is an unpaired video of the series that looks like this part.
	SuggestedMediaItemID *int64 `json:"suggestedMediaItemId,omitempty"`
}

// SeriesLinkView is a series' link to one provider record with its parts.
type SeriesLinkView struct {
	Provider   string           `json:"provider"`
	ExternalID string           `json:"externalId"`
	Title      string           `json:"title"`
	Kind       string           `json:"kind,omitempty"`
	SourceURL  string           `json:"sourceUrl,omitempty"`
	Parts      []SeriesPartView `json:"parts"`
}

type seriesVideo struct {
	id       int64
	title    string
	fileName string
	position *int32
	links    map[string]string // provider -> external id
}

func (e *Engine) seriesVideos(ctx context.Context, seriesID int64) ([]seriesVideo, error) {
	rows, err := e.DB.Query(ctx, `
		SELECT mi.id, mi.title, mi.series_position,
		       COALESCE((SELECT mf.file_name FROM media_file mf WHERE mf.media_item_id = mi.id ORDER BY mf.available DESC, mf.id LIMIT 1), ''),
		       COALESCE((SELECT jsonb_object_agg(ml.provider, ml.external_id) FROM match_link ml WHERE ml.kind = 'media' AND ml.entity_id = mi.id), '{}'::jsonb)
		FROM media_item mi WHERE mi.series_id = $1
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []seriesVideo
	for rows.Next() {
		var v seriesVideo
		var links []byte
		if err := rows.Scan(&v.id, &v.title, &v.position, &v.fileName, &links); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(links, &v.links)
		out = append(out, v)
	}
	return out, rows.Err()
}

var (
	episodeMarker = regexp.MustCompile(`(?i)\bs(\d{1,2})[ ._-]?e(\d{1,3})\b`)
	numberMarker  = regexp.MustCompile(`(?i)\b(?:scene|sc|part|pt|episode|ep|e)[ ._#-]*(\d{1,3})\b`)
)

// partHint reads an "S01E02" label or a "scene 3" / "part 3" number from a
// video's title or file name.
func partHint(texts ...string) (label string, number int) {
	for _, t := range texts {
		t = strings.TrimSuffix(t, filepath.Ext(t))
		if m := episodeMarker.FindStringSubmatch(t); m != nil {
			s, _ := strconv.Atoi(m[1])
			e, _ := strconv.Atoi(m[2])
			return fmt.Sprintf("S%02dE%02d", s, e), 0
		}
		if m := numberMarker.FindStringSubmatch(t); m != nil {
			n, _ := strconv.Atoi(m[1])
			return "", n
		}
	}
	return "", 0
}

// suggestPart picks the part a video most likely is, among parts still free.
func suggestPart(v seriesVideo, parts []SeriesPartView, taken map[int]bool) int {
	label, number := partHint(v.title, v.fileName)
	for i, p := range parts {
		if taken[i] {
			continue
		}
		if label != "" && strings.EqualFold(p.Label, label) || label == "" && number > 0 && p.Number == number {
			return i
		}
	}
	best, bestScore := -1, 80.0
	for i, p := range parts {
		if taken[i] || p.Title == "" {
			continue
		}
		s := max(titlematch.Similarity(v.title, p.Title), titlematch.Similarity(v.fileName, p.Title))
		if s >= bestScore {
			best, bestScore = i, s
		}
	}
	if best < 0 && label == "" && number == 0 && v.position != nil {
		for i, p := range parts {
			if !taken[i] && p.Number == int(*v.position) {
				return i
			}
		}
	}
	return best
}

// SeriesParts lists each provider link of a series with its parts, the video
// paired with each part, and a suggested video for parts not paired yet.
func (e *Engine) SeriesParts(ctx context.Context, seriesID int64) ([]SeriesLinkView, error) {
	rows, err := e.DB.Query(ctx, `
		SELECT provider, external_id, remote_metadata FROM match_link
		WHERE kind = 'series' AND entity_id = $1 ORDER BY matched_at
	`, seriesID)
	if err != nil {
		return nil, err
	}
	var views []SeriesLinkView
	for rows.Next() {
		var v SeriesLinkView
		var raw []byte
		if err := rows.Scan(&v.Provider, &v.ExternalID, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var res provider.SeriesResult
		_ = json.Unmarshal(raw, &res)
		v.Title, v.Kind, v.SourceURL = res.Title, res.Kind, res.SourceURL
		for _, p := range res.Parts {
			v.Parts = append(v.Parts, SeriesPartView{SeriesPart: p, Provider: p.Ref.Provider, ExternalID: p.Ref.ExternalID})
		}
		if v.Parts == nil {
			v.Parts = []SeriesPartView{}
		}
		views = append(views, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return []SeriesLinkView{}, nil
	}

	videos, err := e.seriesVideos(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(videos, func(i, j int) bool { return videos[i].id < videos[j].id })
	for vi := range views {
		view := &views[vi]
		byRef := map[string]int{}
		for i, p := range view.Parts {
			byRef[p.ExternalID] = i
		}
		taken := map[int]bool{}
		var unpaired []seriesVideo
		for _, v := range videos {
			id := v.id
			if i, ok := byRef[v.links[view.Provider]]; ok && view.Parts[i].MediaItemID == nil {
				view.Parts[i].MediaItemID = &id
				taken[i] = true
				continue
			}
			unpaired = append(unpaired, v)
		}
		for _, v := range unpaired {
			id := v.id
			if i := suggestPart(v, view.Parts, taken); i >= 0 {
				view.Parts[i].SuggestedMediaItemID = &id
				taken[i] = true
			}
		}
	}
	return views, nil
}

// UnlinkSeries removes a series' link to one provider. Its videos keep their
// own matches.
func (e *Engine) UnlinkSeries(ctx context.Context, seriesID int64, providerID string) error {
	_, err := e.DB.Exec(ctx, `DELETE FROM match_link WHERE kind = 'series' AND entity_id = $1 AND provider = $2`, seriesID, providerID)
	return err
}

// seriesPart finds one part of a linked series record.
func (e *Engine) seriesPart(ctx context.Context, seriesID int64, providerID, externalID string) (provider.SeriesPart, error) {
	var raw []byte
	if err := e.DB.QueryRow(ctx, `
		SELECT remote_metadata FROM match_link WHERE kind = 'series' AND entity_id = $1 AND provider = $2
	`, seriesID, providerID).Scan(&raw); err != nil {
		return provider.SeriesPart{}, errors.New("the series is not linked to that provider")
	}
	var res provider.SeriesResult
	_ = json.Unmarshal(raw, &res)
	for _, p := range res.Parts {
		if p.Ref.ExternalID == externalID {
			return p, nil
		}
	}
	return provider.SeriesPart{}, errors.New("that part is not in the linked series")
}

// AssignPart pairs a video with one part of a linked series: the video joins
// the series at the part's number and is matched to the part's record. With
// apply the match is applied like a manual match (rename, NFO, details);
// otherwise the record is only linked.
func (e *Engine) AssignPart(ctx context.Context, seriesID, mediaItemID int64, providerID, externalID string, apply bool, mediaFileID int64) error {
	part, err := e.seriesPart(ctx, seriesID, providerID, externalID)
	if err != nil {
		return err
	}
	if apply {
		if _, err := e.ApplyReplacing(ctx, mediaFileID, providerID, externalID, false); err != nil {
			return err
		}
	} else if err := e.LinkOnly(ctx, mediaItemID, providerID, externalID); err != nil {
		return err
	}
	_, err = e.DB.Exec(ctx, `
		UPDATE media_item SET series_id = $2, series_position = $3, updated_at = now() WHERE id = $1
	`, mediaItemID, seriesID, part.Number)
	return err
}

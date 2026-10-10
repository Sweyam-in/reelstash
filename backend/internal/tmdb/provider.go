package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"reelstash/backend/internal/provider"
)

// ProviderID is TMDB's stable provider id (stored in match_link.provider).
const ProviderID = "tmdb"

const maxCandidates = 8

// Provider adapts Client to the provider.Provider plugin interface.
type Provider struct {
	*Client
}

var (
	_ provider.Provider = (*Provider)(nil)
	_ provider.Noticer  = (*Provider)(nil)
)

func NewProvider(c *Client) *Provider { return &Provider{Client: c} }

func (p *Provider) ID() string   { return ProviderID }
func (p *Provider) Name() string { return "TMDB" }
func (p *Provider) Capabilities() provider.Caps {
	return provider.Caps{Media: true, Performer: true, Studio: true}
}
func (p *Provider) Enabled() bool        { return p.Client.Enabled() }
func (p *Provider) ImageHosts() []string { return []string{"image.tmdb.org"} }

func (p *Provider) Notice() string {
	return "This product uses the TMDB API but is not endorsed or certified by TMDB."
}

func (p *Provider) ConfigFields() []provider.Field {
	return []provider.Field{
		{Key: "token", Label: "API key or read access token", Secret: true},
		{Key: "language", Label: "Language", Placeholder: "en-US"},
		{Key: "includeAdult", Label: "Include adult titles and people in search results", Type: "bool", Default: "true"},
		{Key: "baseUrl", Label: "API base URL", Placeholder: DefaultBaseURL},
	}
}

// Configure accepts "token", "language", "baseUrl" and "includeAdult" ("true"/"false").
func (p *Provider) Configure(cfg map[string]string) error {
	base, token, language := p.Client.creds()
	if v, ok := cfg["baseUrl"]; ok {
		if v != "" {
			if _, err := url.ParseRequestURI(v); err != nil {
				return fmt.Errorf("invalid base url: %w", err)
			}
		}
		base = v
	}
	if v, ok := cfg["token"]; ok {
		token = v
	}
	if v, ok := cfg["language"]; ok {
		language = v
		if v == "" {
			language = "en-US"
		}
	}
	p.Client.set(base, token, language)
	if v, ok := cfg["includeAdult"]; ok {
		p.Client.SetIncludeAdult(v != "false") // on unless explicitly turned off
	}
	return nil
}

func (p *Provider) Test(ctx context.Context) error {
	if !p.Enabled() {
		return fmt.Errorf("no API key configured")
	}
	var out struct{}
	return p.Client.get(ctx, "configuration", nil, &out)
}

// ---- titles ----

func year(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

func movieRef(id int64) provider.Ref {
	return provider.Ref{Provider: ProviderID, ExternalID: "movie/" + strconv.FormatInt(id, 10)}
}

func tvRef(id int64, season, episode int) provider.Ref {
	ext := "tv/" + strconv.FormatInt(id, 10)
	if season > 0 && episode > 0 {
		ext += "/" + strconv.Itoa(season) + "/" + strconv.Itoa(episode)
	}
	return provider.Ref{Provider: ProviderID, ExternalID: ext}
}

func (p *Provider) SearchMedia(ctx context.Context, q provider.MediaQuery) ([]provider.MediaCandidate, error) {
	info := ParseFileName(q.FileName)
	if text := strings.TrimSpace(q.Text); text != "" {
		info = ParseName(text)
	}
	if info.Title == "" {
		return nil, nil
	}

	var out []provider.MediaCandidate
	switch {
	case q.SearchType == "scenes" || q.SearchType == "jav":
		return nil, nil
	case q.SearchType == "movies":
		movies, err := p.searchMovies(ctx, info)
		if err != nil {
			return nil, err
		}
		out = movies
	case q.SearchType == "shows":
		shows, err := p.searchTV(ctx, info)
		if err != nil {
			return nil, err
		}
		out = shows
	case q.SearchType == "all":
		movies, movieErr := p.searchMovies(ctx, info)
		shows, showErr := p.searchTV(ctx, info)
		if movieErr != nil && showErr != nil {
			return nil, fmt.Errorf("search TMDB movies: %v; TV shows: %w", movieErr, showErr)
		}
		out = append(movies, shows...)
		if len(out) == 0 && movieErr != nil {
			return nil, fmt.Errorf("search TMDB movies: %w", movieErr)
		}
		if len(out) == 0 && showErr != nil {
			return nil, fmt.Errorf("search TMDB TV shows: %w", showErr)
		}
	case info.IsEpisode():
		shows, err := p.searchTV(ctx, info)
		if err != nil {
			return nil, err
		}
		out = shows
	default:
		movies, err := p.searchMovies(ctx, info)
		if err != nil {
			return nil, err
		}
		out = movies
		if len(out) == 0 { // series-level fallback for names with no episode marker
			shows, err := p.searchTV(ctx, info)
			if err != nil {
				return nil, err
			}
			out = shows
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	// Without a year to tell remakes apart, a close runner-up makes the top
	// hit ambiguous, so leave it for a human.
	if len(out) > 1 && info.Year == 0 && out[1].Score >= out[0].Score-3 {
		out[0].AutoMatch = false
	}
	return out, nil
}

// score compares the parsed title with a result's titles and applies the year.
func score(info FileInfo, resultYear int, titles ...string) (s float64, yearHit bool) {
	for _, t := range titles {
		if v := similarity(info.Title, t); v > s {
			s = v
		}
	}
	if info.Year != 0 && resultYear != 0 {
		if info.Year == resultYear {
			yearHit = true
		} else {
			s -= 15
			if s < 0 {
				s = 0
			}
		}
	}
	return s, yearHit
}

func autoMatch(info FileInfo, s float64, yearHit bool) bool {
	return s >= 95 && (info.Year == 0 || yearHit)
}

func (p *Provider) searchMovies(ctx context.Context, info FileInfo) ([]provider.MediaCandidate, error) {
	run := func(withYear bool) ([]searchMovie, error) {
		v := url.Values{"query": {info.Title}, "include_adult": {p.Client.adultParam()}}
		if withYear && info.Year != 0 {
			v.Set("year", strconv.Itoa(info.Year))
		}
		var resp struct {
			Results []searchMovie `json:"results"`
		}
		return resp.Results, p.Client.get(ctx, "search/movie", v, &resp)
	}
	// Release years are sometimes a year off, so a year-filtered miss retries without it.
	results, err := run(true)
	if err == nil && len(results) == 0 && info.Year != 0 {
		results, err = run(false)
	}
	if err != nil {
		return nil, err
	}
	out := make([]provider.MediaCandidate, 0, len(results))
	for _, r := range results {
		s, yearHit := score(info, year(r.ReleaseDate), r.Title, r.OriginalTitle)
		out = append(out, provider.MediaCandidate{
			Result: provider.MediaResult{
				Ref: movieRef(r.ID), Title: r.Title, Description: r.Overview, Date: r.ReleaseDate,
				PosterURL: imageURL("w342", r.PosterPath),
				Kind:      "movie",
			},
			Score: s, AutoMatch: autoMatch(info, s, yearHit), DateHit: yearHit,
		})
	}
	return out, nil
}

func (p *Provider) searchTV(ctx context.Context, info FileInfo) ([]provider.MediaCandidate, error) {
	v := url.Values{"query": {info.Title}, "include_adult": {p.Client.adultParam()}}
	if info.Year != 0 {
		v.Set("first_air_date_year", strconv.Itoa(info.Year))
	}
	var resp struct {
		Results []searchTV `json:"results"`
	}
	if err := p.Client.get(ctx, "search/tv", v, &resp); err != nil {
		return nil, err
	}
	out := make([]provider.MediaCandidate, 0, len(resp.Results))
	for _, r := range resp.Results {
		s, yearHit := score(info, year(r.FirstAirDate), r.Name, r.OriginalName)
		title := r.Name
		if info.IsEpisode() {
			title = fmt.Sprintf("%s - S%02dE%02d", r.Name, info.Season, info.Episode)
		}
		out = append(out, provider.MediaCandidate{
			Result: provider.MediaResult{
				Ref: tvRef(r.ID, info.Season, info.Episode), Title: title, Description: r.Overview, Date: r.FirstAirDate,
				PosterURL: imageURL("w342", r.PosterPath),
				Kind:      "show",
			},
			Score: s, AutoMatch: autoMatch(info, s, yearHit), DateHit: yearHit,
		})
	}
	return out, nil
}

const maxCast = 10

func castNames(lists ...[]castMember) (names []string, images map[string]string, refs map[string]provider.Ref) {
	images = map[string]string{}
	refs = map[string]provider.Ref{}
	seen := map[string]bool{}
	for _, list := range lists {
		for _, c := range list {
			key := strings.ToLower(c.Name)
			if c.Name == "" || seen[key] || len(names) >= maxCast {
				continue
			}
			seen[key] = true
			names = append(names, c.Name)
			if c.ID > 0 {
				refs[c.Name] = provider.Ref{Provider: ProviderID, ExternalID: strconv.FormatInt(c.ID, 10)}
			}
			if u := imageURL("w185", c.ProfilePath); u != "" {
				images[c.Name] = u
			}
		}
	}
	return names, images, refs
}

// characters maps each cast member's name to the role they play.
func characters(lists ...[]castMember) map[string]string {
	out := map[string]string{}
	for _, list := range lists {
		for _, c := range list {
			if c.Name != "" && c.Character != "" {
				if _, seen := out[c.Name]; !seen {
					out[c.Name] = c.Character
				}
			}
		}
	}
	return out
}

// maxCrew caps the directors and writers kept per title.
const maxCrew = 6

// crewCredits keeps the directors, then the writers, of a title.
func crewCredits(lists ...[]crewMember) []provider.CrewCredit {
	jobs := map[string]string{"Director": "director", "Screenplay": "writer", "Writer": "writer", "Teleplay": "writer"}
	var out []provider.CrewCredit
	seen := map[string]bool{}
	for _, want := range []string{"director", "writer"} {
		for _, list := range lists {
			for _, c := range list {
				job := jobs[c.Job]
				key := job + "\x00" + strings.ToLower(c.Name)
				if job != want || c.Name == "" || seen[key] || len(out) >= maxCrew {
					continue
				}
				seen[key] = true
				credit := provider.CrewCredit{Name: c.Name, Job: job, ImageURL: imageURL("w185", c.ProfilePath)}
				if c.ID > 0 {
					credit.Ref = provider.Ref{Provider: ProviderID, ExternalID: strconv.FormatInt(c.ID, 10)}
				}
				out = append(out, credit)
			}
		}
	}
	return out
}

func genreNames(g []name) []string {
	out := make([]string, 0, len(g))
	for _, x := range g {
		if x.Name != "" {
			out = append(out, x.Name)
		}
	}
	return out
}

func firstName(lists ...[]name) string {
	for _, l := range lists {
		for _, n := range l {
			if n.Name != "" {
				return n.Name
			}
		}
	}
	return ""
}

// GetMedia loads full metadata. External ids are "movie/<id>", "tv/<id>" (the
// series) or "tv/<id>/<season>/<episode>".
func (p *Provider) GetMedia(ctx context.Context, externalID string) (provider.MediaResult, error) {
	parts := strings.Split(externalID, "/")
	if len(parts) < 2 {
		return provider.MediaResult{}, fmt.Errorf("invalid tmdb id %q", externalID)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 1 {
		return provider.MediaResult{}, fmt.Errorf("invalid tmdb id %q", externalID)
	}
	credits := url.Values{"append_to_response": {"credits"}}

	switch parts[0] {
	case "movie":
		var d movieDetail
		if err := p.Client.get(ctx, "movie/"+parts[1], credits, &d); err != nil {
			return provider.MediaResult{}, err
		}
		names, images, refs := castNames(d.Credits.Cast)
		return provider.MediaResult{
			Ref: movieRef(d.ID), Title: d.Title, Description: d.Overview, Date: d.ReleaseDate,
			Studio: firstName(d.ProductionCompanies), Performers: names, PerformerImages: images, PerformerRefs: refs,
			PosterURL: imageURL("w500", d.PosterPath), BackgroundURL: imageURL("w1280", d.BackdropPath),
			Tags: genreNames(d.Genres), SourceURL: "https://www.themoviedb.org/movie/" + parts[1],
			Kind: "movie", Characters: characters(d.Credits.Cast), Crew: crewCredits(d.Credits.Crew),
		}, nil

	case "tv":
		var d tvDetail
		if err := p.Client.get(ctx, "tv/"+parts[1], credits, &d); err != nil {
			return provider.MediaResult{}, err
		}
		res := provider.MediaResult{
			Ref: tvRef(d.ID, 0, 0), Title: d.Name, Description: d.Overview, Date: d.FirstAirDate,
			Studio:    firstName(d.Networks, d.ProductionCompanies),
			PosterURL: imageURL("w500", d.PosterPath), BackgroundURL: imageURL("w1280", d.BackdropPath),
			Tags: genreNames(d.Genres), SourceURL: "https://www.themoviedb.org/tv/" + parts[1],
		}
		res.Performers, res.PerformerImages, res.PerformerRefs = castNames(d.Credits.Cast)
		res.Characters = characters(d.Credits.Cast)
		res.Crew = crewCredits(d.Credits.Crew)
		if len(parts) < 4 {
			return res, nil
		}
		season, e1 := strconv.Atoi(parts[2])
		episode, e2 := strconv.Atoi(parts[3])
		if e1 != nil || e2 != nil || season < 1 || episode < 1 {
			return provider.MediaResult{}, fmt.Errorf("invalid tmdb id %q", externalID)
		}
		var ep episodeDetail
		if err := p.Client.get(ctx, fmt.Sprintf("tv/%s/season/%d/episode/%d", parts[1], season, episode), nil, &ep); err != nil {
			return provider.MediaResult{}, err
		}
		res.Ref = tvRef(d.ID, season, episode)
		res.Title = fmt.Sprintf("%s - S%02dE%02d - %s", d.Name, season, episode, ep.Name)
		if ep.Name == "" {
			res.Title = fmt.Sprintf("%s - S%02dE%02d", d.Name, season, episode)
		}
		if ep.Overview != "" {
			res.Description = ep.Overview
		}
		if ep.AirDate != "" {
			res.Date = ep.AirDate
		}
		if still := imageURL("w780", ep.StillPath); still != "" {
			res.BackgroundURL = still
		}
		res.Performers, res.PerformerImages, res.PerformerRefs = castNames(ep.GuestStars, d.Credits.Cast)
		res.Characters = characters(ep.GuestStars, d.Credits.Cast)
		res.Crew = crewCredits(ep.Crew, d.Credits.Crew)
		res.Kind, res.Show, res.ShowRef, res.Season, res.Episode = "episode", d.Name, tvRef(d.ID, 0, 0), season, episode
		res.SourceURL = fmt.Sprintf("https://www.themoviedb.org/tv/%s/season/%d/episode/%d", parts[1], season, episode)
		return res, nil
	}
	return provider.MediaResult{}, fmt.Errorf("invalid tmdb id %q", externalID)
}

// ---- performers ----

func genderName(g int) string {
	switch g {
	case 1:
		return "female"
	case 2:
		return "male"
	case 3:
		return "non-binary"
	}
	return ""
}

func (p *Provider) SearchPerformers(ctx context.Context, query string) ([]provider.PerformerResult, error) {
	var resp struct {
		Results []personHit `json:"results"`
	}
	v := url.Values{"query": {query}, "include_adult": {p.Client.adultParam()}}
	if err := p.Client.get(ctx, "search/person", v, &resp); err != nil {
		return nil, err
	}
	out := make([]provider.PerformerResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		hint := r.KnownForDepartment
		if len(r.KnownFor) > 0 {
			work := r.KnownFor[0].Title
			if work == "" {
				work = r.KnownFor[0].Name
			}
			if work != "" {
				hint = strings.TrimSpace(hint + " · " + work)
			}
		}
		out = append(out, provider.PerformerResult{
			Ref:  provider.Ref{Provider: ProviderID, ExternalID: strconv.FormatInt(r.ID, 10)},
			Name: r.Name, Disambiguation: hint, ImageURL: imageURL("w185", r.ProfilePath),
		})
	}
	return out, nil
}

func (p *Provider) GetPerformer(ctx context.Context, externalID string) (provider.PerformerResult, error) {
	if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
		return provider.PerformerResult{}, fmt.Errorf("invalid tmdb person id")
	}
	var d personDetail
	if err := p.Client.get(ctx, "person/"+externalID, nil, &d); err != nil {
		return provider.PerformerResult{}, err
	}
	return provider.PerformerResult{
		Ref:  provider.Ref{Provider: ProviderID, ExternalID: externalID},
		Name: d.Name, Bio: d.Biography, Aliases: d.AlsoKnownAs, ImageURL: imageURL("h632", d.ProfilePath),
		Gender: genderName(d.Gender), Birthdate: d.Birthday, Birthplace: d.PlaceOfBirth,
	}, nil
}

// ---- studios (TMDB production companies) ----

func (p *Provider) SearchStudios(ctx context.Context, query string) ([]provider.StudioResult, error) {
	var resp struct {
		Results []companyHit `json:"results"`
	}
	if err := p.Client.get(ctx, "search/company", url.Values{"query": {query}}, &resp); err != nil {
		return nil, err
	}
	out := make([]provider.StudioResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, provider.StudioResult{
			Ref:  provider.Ref{Provider: ProviderID, ExternalID: strconv.FormatInt(r.ID, 10)},
			Name: r.Name, Network: r.OriginCountry, ImageURL: imageURL("w300", r.LogoPath),
		})
	}
	return out, nil
}

func (p *Provider) GetStudio(ctx context.Context, externalID string) (provider.StudioResult, error) {
	if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
		return provider.StudioResult{}, fmt.Errorf("invalid tmdb company id")
	}
	var d companyDetail
	if err := p.Client.get(ctx, "company/"+externalID, nil, &d); err != nil {
		return provider.StudioResult{}, err
	}
	res := provider.StudioResult{
		Ref:  provider.Ref{Provider: ProviderID, ExternalID: externalID},
		Name: d.Name, Description: d.Description, URL: d.Homepage, ImageURL: imageURL("w300", d.LogoPath),
	}
	if d.ParentCompany != nil {
		res.Network = d.ParentCompany.Name
	}
	return res, nil
}

// ---- name similarity ----

func normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// similarity is a 0..100 score: the better of a plain edit-distance ratio and
// one that ignores word order, on punctuation- and case-folded text.
func similarity(a, b string) float64 {
	na, nb := normalize(a), normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	best := ratio(na, nb)
	if r := ratio(sortWords(na), sortWords(nb)); r > best {
		best = r
	}
	return best
}

func sortWords(s string) string {
	w := strings.Fields(s)
	sort.Strings(w)
	return strings.Join(w, " ")
}

func ratio(a, b string) float64 {
	if a == b {
		return 100
	}
	ra, rb := []rune(a), []rune(b)
	longest := len(ra)
	if len(rb) > longest {
		longest = len(rb)
	}
	return 100 * (1 - float64(levenshtein(ra, rb))/float64(longest))
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

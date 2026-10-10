// Package extprovider lets a metadata provider live outside ReelStash: any
// HTTP service that speaks the small JSON contract below can be added in
// Admin → Metadata → Providers and is then used for matching exactly like the
// built-in TMDB provider. See docs/provider-plugins.md.
package extprovider

import "reelstash/backend/internal/provider"

// Manifest is what GET {base}/manifest returns.
type Manifest struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Capabilities Capabilities `json:"capabilities"`
	ImageHosts   []string     `json:"imageHosts"`
	Notice       string       `json:"notice,omitempty"`
	// Test says the service answers POST /test, which checks its own settings
	// (an API token, say) with a real call; "Test connection" then uses it.
	Test bool `json:"test,omitempty"`
}

type Capabilities struct {
	Media     bool `json:"media"`
	Performer bool `json:"performer"`
	Studio    bool `json:"studio"`
	Series    bool `json:"series"`
}

type wireCrew struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Job      string `json:"job"`
	ImageURL string `json:"imageUrl"`
}

type wireMedia struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Date            string            `json:"date"`
	Studio          string            `json:"studio"`
	Performers      []string          `json:"performers"`
	PerformerImages map[string]string `json:"performerImages"`
	PerformerIDs    map[string]string `json:"performerIds"`
	Poster          string            `json:"poster"`
	Background      string            `json:"background"`
	Tags            []string          `json:"tags"`
	SourceURL       string            `json:"sourceUrl"`
	Kind            string            `json:"kind"`
	Show            string            `json:"show"`
	ShowID          string            `json:"showId"`
	Season          int               `json:"season"`
	Episode         int               `json:"episode"`
	Characters      map[string]string `json:"characters"`
	Crew            []wireCrew        `json:"crew"`
}

type wireCandidate struct {
	Score     float64   `json:"score"`
	AutoMatch bool      `json:"autoMatch"`
	StudioHit bool      `json:"studioHit"`
	DateHit   bool      `json:"dateHit"`
	Result    wireMedia `json:"result"`
}

type wirePerformer struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Disambiguation string   `json:"disambiguation"`
	Bio            string   `json:"bio"`
	Aliases        []string `json:"aliases"`
	Image          string   `json:"image"`
	Gender         string   `json:"gender"`
	Birthdate      string   `json:"birthdate"`
	Birthplace     string   `json:"birthplace"`
	Ethnicity      string   `json:"ethnicity"`
	Nationality    string   `json:"nationality"`
	HairColor      string   `json:"hairColor"`
	EyeColor       string   `json:"eyeColor"`
	Height         string   `json:"height"`
	Measurements   string   `json:"measurements"`
}

type wireStudio struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ShortName   string   `json:"shortName"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Network     string   `json:"network"`
	Image       string   `json:"image"`
	Aliases     []string `json:"aliases"`
}

type wirePart struct {
	ID         string   `json:"id"`
	Number     int      `json:"number"`
	Label      string   `json:"label"`
	Title      string   `json:"title"`
	Date       string   `json:"date"`
	Poster     string   `json:"poster"`
	Performers []string `json:"performers"`
}

type wireSeries struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Date        string     `json:"date"`
	Studio      string     `json:"studio"`
	Kind        string     `json:"kind"`
	Poster      string     `json:"poster"`
	Background  string     `json:"background"`
	SourceURL   string     `json:"sourceUrl"`
	PartCount   int        `json:"partCount"`
	Parts       []wirePart `json:"parts"`
}

func (m wireMedia) toResult(providerID string) provider.MediaResult {
	ref := func(id string) provider.Ref { return provider.Ref{Provider: providerID, ExternalID: id} }
	r := provider.MediaResult{
		Ref: ref(m.ID), Title: m.Title, Description: m.Description, Date: m.Date, Studio: m.Studio,
		Performers: m.Performers, PerformerImages: m.PerformerImages, PosterURL: m.Poster, BackgroundURL: m.Background,
		Tags: m.Tags, SourceURL: m.SourceURL, Kind: m.Kind, Show: m.Show, Season: m.Season, Episode: m.Episode, Characters: m.Characters,
	}
	if m.ShowID != "" {
		r.ShowRef = ref(m.ShowID)
	}
	if len(m.PerformerIDs) > 0 {
		r.PerformerRefs = map[string]provider.Ref{}
		for name, id := range m.PerformerIDs {
			r.PerformerRefs[name] = ref(id)
		}
	}
	for _, c := range m.Crew {
		r.Crew = append(r.Crew, provider.CrewCredit{Name: c.Name, Job: c.Job, Ref: ref(c.ID), ImageURL: c.ImageURL})
	}
	return r
}

func (p wirePerformer) toResult(providerID string) provider.PerformerResult {
	return provider.PerformerResult{
		Ref: provider.Ref{Provider: providerID, ExternalID: p.ID}, Name: p.Name, Disambiguation: p.Disambiguation, Bio: p.Bio,
		Aliases: p.Aliases, ImageURL: p.Image, Gender: p.Gender, Birthdate: p.Birthdate, Birthplace: p.Birthplace,
		Ethnicity: p.Ethnicity, Nationality: p.Nationality, HairColor: p.HairColor, EyeColor: p.EyeColor, Height: p.Height, Measurements: p.Measurements,
	}
}

func (s wireStudio) toResult(providerID string) provider.StudioResult {
	return provider.StudioResult{
		Ref: provider.Ref{Provider: providerID, ExternalID: s.ID}, Name: s.Name, ShortName: s.ShortName, Description: s.Description,
		URL: s.URL, Network: s.Network, ImageURL: s.Image, Aliases: s.Aliases,
	}
}

func (s wireSeries) toResult(providerID string) provider.SeriesResult {
	out := provider.SeriesResult{
		Ref: provider.Ref{Provider: providerID, ExternalID: s.ID}, Title: s.Title, Description: s.Description, Date: s.Date,
		Studio: s.Studio, Kind: s.Kind, PosterURL: s.Poster, BackdropURL: s.Background, SourceURL: s.SourceURL, PartCount: s.PartCount,
	}
	for _, p := range s.Parts {
		out.Parts = append(out.Parts, provider.SeriesPart{
			Ref: provider.Ref{Provider: providerID, ExternalID: p.ID}, Number: p.Number, Label: p.Label, Title: p.Title, Date: p.Date, PosterURL: p.Poster, Performers: p.Performers,
		})
	}
	return out
}

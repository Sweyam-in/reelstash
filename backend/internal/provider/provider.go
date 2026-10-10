// Package provider defines the metadata-provider plugin surface. A Provider
// (TMDB, plugins, ...) searches and fetches metadata in a normalized shape;
// internal/namer orchestrates matching across whichever providers are
// registered and enabled, without knowing any provider's wire format.
package provider

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ErrUnsupported is returned by a provider for a kind it does not implement.
var ErrUnsupported = errors.New("provider does not support this operation")

// Kind is the type of record a provider can match.
type Kind string

const (
	KindMedia     Kind = "media"
	KindPerformer Kind = "performer"
	KindStudio    Kind = "studio"
)

// Ref identifies one record at one provider. IDs are only unique per provider.
type Ref struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"externalId"`
}

// MediaQuery is what a provider searches for. FileName is the raw file name
// (each provider parses out what it understands: a provider may use site/date, TMDB
// uses year and season/episode). Text, when set, is a free-text override typed
// by the user in a manual search.
type MediaQuery struct {
	FileName string
	Text     string
	// SearchType limits a manual search to "scenes", "movies", or "shows".
	// Empty keeps each provider's filename matching behavior; "all" searches
	// every media catalog supported by that provider.
	SearchType string
}

// MediaResult is the normalized metadata for one title.
type MediaResult struct {
	Ref             Ref
	Title           string
	Description     string
	Date            string // YYYY-MM-DD
	Studio          string
	Performers      []string
	PerformerImages map[string]string // performer name -> photo URL
	// PerformerRefs is each performer's own record at this provider, keyed by
	// name, so a performer already linked to it is found whatever the spelling.
	PerformerRefs map[string]Ref
	PosterURL     string
	BackgroundURL string
	Tags          []string
	SourceURL     string

	// Movie/TV providers only (all empty for scene providers):
	// Kind is "movie" or "episode" ("" when the provider doesn't say).
	Kind string
	// Show is the TV show an episode belongs to, with its record at this
	// provider, so every episode of it lands in one series.
	Show    string
	ShowRef Ref
	Season  int
	Episode int
	// Characters maps a cast member's name to the role they play.
	Characters map[string]string
	// Crew are directors and writers, in billing order.
	Crew []CrewCredit
}

// CrewCredit is one person's non-acting job on a title.
type CrewCredit struct {
	Name     string
	Job      string // "director" or "writer"
	Ref      Ref
	ImageURL string
}

// MediaCandidate is a scored search hit. Score is 0..100, comparable only
// within one provider; AutoMatch is the provider's own "confident enough to
// apply without review" verdict.
type MediaCandidate struct {
	Result    MediaResult
	Score     float64
	AutoMatch bool
	StudioHit bool // the file's studio/site agrees with the result
	DateHit   bool // the file's date agrees with the result
}

// PerformerResult is the normalized metadata for one performer/person.
type PerformerResult struct {
	Ref            Ref
	Name           string
	Disambiguation string
	Bio            string
	Aliases        []string
	ImageURL       string
	Gender         string
	Birthdate      string
	Birthplace     string
	Ethnicity      string
	Nationality    string
	HairColor      string
	EyeColor       string
	Height         string
	Measurements   string
}

// StudioResult is the normalized metadata for one studio/company.
type StudioResult struct {
	Ref         Ref
	Name        string
	ShortName   string
	Description string
	URL         string
	Network     string
	ImageURL    string
	Aliases     []string
}

// Caps declares which kinds a provider supports.
type Caps struct {
	Media     bool
	Performer bool
	Studio    bool
}

func (c Caps) Supports(k Kind) bool {
	switch k {
	case KindMedia:
		return c.Media
	case KindPerformer:
		return c.Performer
	case KindStudio:
		return c.Studio
	}
	return false
}

// Field describes one setting a provider needs, so the Settings UI can render
// the form without knowing the provider.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret"`
	Placeholder string `json:"placeholder,omitempty"`
	// Type is "" (text) or "bool" (a checkbox whose value is "true"/"false").
	Type string `json:"type,omitempty"`
	// Default is the value shown (and used) while nothing has been saved.
	Default string `json:"default,omitempty"`
}

// Provider is one pluggable metadata source. Methods for a kind the provider
// does not support return ErrUnsupported.
type Provider interface {
	ID() string
	Name() string
	Capabilities() Caps
	// Enabled reports whether the provider has the credentials it needs.
	Enabled() bool
	// ConfigFields lists the settings Configure accepts.
	ConfigFields() []Field
	// Configure hot-swaps credentials/settings (no restart). Keys are
	// provider-defined, e.g. "token", "baseUrl".
	Configure(cfg map[string]string) error
	// Test makes a cheap authenticated call to validate the configuration.
	Test(ctx context.Context) error
	// ImageHosts lists hostnames (and their subdomains) artwork may be
	// downloaded from; anything else is refused.
	ImageHosts() []string

	SearchMedia(ctx context.Context, q MediaQuery) ([]MediaCandidate, error)
	GetMedia(ctx context.Context, externalID string) (MediaResult, error)

	SearchPerformers(ctx context.Context, name string) ([]PerformerResult, error)
	GetPerformer(ctx context.Context, externalID string) (PerformerResult, error)
	SearchStudios(ctx context.Context, name string) ([]StudioResult, error)
	GetStudio(ctx context.Context, externalID string) (StudioResult, error)
}

// SeriesPart is one title within a series: a scene of a movie, an episode of
// a show, or a film of a collection. Ref is fetched with GetMedia to match a
// video to it.
type SeriesPart struct {
	Ref        Ref      `json:"ref"`
	Number     int      `json:"number"`          // 1-based order within the series
	Label      string   `json:"label,omitempty"` // e.g. "S01E02" or "Scene 3"
	Title      string   `json:"title"`
	Date       string   `json:"date,omitempty"`
	PosterURL  string   `json:"poster,omitempty"`
	Performers []string `json:"performers,omitempty"`
}

// SeriesResult is the normalized metadata for one series. Search results may
// leave Parts empty; GetSeries fills them.
type SeriesResult struct {
	Ref         Ref          `json:"ref"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Date        string       `json:"date,omitempty"`
	Studio      string       `json:"studio,omitempty"`
	Kind        string       `json:"kind,omitempty"` // provider's own word: "movie", "show", "collection"
	PosterURL   string       `json:"poster,omitempty"`
	BackdropURL string       `json:"backdrop,omitempty"`
	SourceURL   string       `json:"sourceUrl,omitempty"`
	PartCount   int          `json:"partCount,omitempty"`
	Parts       []SeriesPart `json:"parts,omitempty"`
}

// SeriesProvider is implemented by providers that can match a whole series
// and list its parts.
type SeriesProvider interface {
	SearchSeries(ctx context.Context, query string) ([]SeriesResult, error)
	GetSeries(ctx context.Context, externalID string) (SeriesResult, error)
}

// EnabledSeries returns the configured providers that can match series, in
// priority order.
func (r *Registry) EnabledSeries() []Provider {
	var out []Provider
	for _, p := range r.All() {
		if _, ok := p.(SeriesProvider); ok && p.Enabled() {
			out = append(out, p)
		}
	}
	return out
}

// Noticer is implemented by providers that must show a legal notice (for
// example TMDB's attribution) next to their settings.
type Noticer interface {
	Notice() string
}

// Registry holds the registered providers in priority order.
type Registry struct {
	mu        sync.RWMutex
	providers []Provider
	order     []string // explicit priority; providers not listed follow in registration order
}

func NewRegistry() *Registry { return &Registry{} }

// Register adds a provider. A later registration with the same ID replaces it.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.providers {
		if existing.ID() == p.ID() {
			r.providers[i] = p
			return
		}
	}
	r.providers = append(r.providers, p)
}

// SetOrder sets the provider priority by ID. Unknown IDs are ignored.
func (r *Registry) SetOrder(ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append([]string(nil), ids...)
}

// Unregister removes a provider. It reports whether one was registered.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.providers {
		if p.ID() == id {
			r.providers = append(r.providers[:i], r.providers[i+1:]...)
			return true
		}
	}
	return false
}

// Get returns a provider by ID.
func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.providers {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// All returns every registered provider in priority order.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]Provider(nil), r.providers...)
	rank := map[string]int{}
	for i, id := range r.order {
		rank[id] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i].ID()]
		rj, jok := rank[out[j].ID()]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		}
		return false
	})
	return out
}

// Enabled returns the configured providers that support kind, in priority order.
func (r *Registry) Enabled(kind Kind) []Provider {
	var out []Provider
	for _, p := range r.All() {
		if p.Enabled() && p.Capabilities().Supports(kind) {
			out = append(out, p)
		}
	}
	return out
}

// AnyEnabled reports whether at least one provider can match kind.
func (r *Registry) AnyEnabled(kind Kind) bool {
	return r != nil && len(r.Enabled(kind)) > 0
}

// ImageHostAllowed reports whether any registered provider permits downloading
// artwork from host.
func (r *Registry) ImageHostAllowed(host string) bool {
	if r == nil {
		return false
	}
	for _, p := range r.All() {
		for _, allowed := range p.ImageHosts() {
			if host == allowed || len(host) > len(allowed) && host[len(host)-len(allowed)-1:] == "."+allowed {
				return true
			}
		}
	}
	return false
}

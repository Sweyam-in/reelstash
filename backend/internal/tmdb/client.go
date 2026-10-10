// Package tmdb is the metadata provider for The Movie Database
// (themoviedb.org): movies, TV episodes, people and companies.
package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://api.themoviedb.org/3"
	imageBase      = "https://image.tmdb.org/t/p/"
)

// Client is a small TMDB v3 client. It accepts either a v3 API key (sent as
// the api_key query parameter) or a v4 "read access token" (a JWT, sent as a
// bearer token); the two are told apart by their shape.
type Client struct {
	HTTPClient *http.Client

	mu           sync.RWMutex
	token        string
	baseURL      string
	language     string
	includeAdult bool
}

// SetIncludeAdult controls whether searches return adult-flagged titles and
// people (TMDB hides them unless include_adult=true is sent).
func (c *Client) SetIncludeAdult(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.includeAdult = on
}

// adultParam is the include_adult query value for searches.
func (c *Client) adultParam() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.includeAdult {
		return "true"
	}
	return "false"
}

func New(baseURL, token string) *Client {
	c := &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}, language: "en-US", includeAdult: true}
	c.set(baseURL, token, "")
	return c
}

func (c *Client) set(baseURL, token, language string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c.baseURL = strings.TrimRight(baseURL, "/")
	c.token = strings.TrimSpace(token)
	if language != "" {
		c.language = language
	}
}

func (c *Client) creds() (baseURL, token, language string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.token, c.language
}

func (c *Client) Enabled() bool {
	_, token, _ := c.creds()
	return c != nil && token != ""
}

func isBearer(token string) bool { return strings.HasPrefix(token, "eyJ") }

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	base, token, language := c.creds()
	if token == "" {
		return fmt.Errorf("tmdb: no API key configured")
	}
	if query == nil {
		query = url.Values{}
	}
	if query.Get("language") == "" && language != "" {
		query.Set("language", language)
	}
	if !isBearer(token) {
		query.Set("api_key", token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+strings.TrimLeft(path, "/")+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	if isBearer(token) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "reelstash/1")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("tmdb request failed: %w", redactErr(err, token))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tmdb returned %d: %s", resp.StatusCode, apiMessage(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode tmdb response: %w", err)
	}
	return nil
}

// redactErr keeps the API key out of error text (url.Error embeds the URL).
func redactErr(err error, token string) error {
	if token == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), token, "REDACTED"))
}

func apiMessage(body []byte) string {
	var e struct {
		StatusMessage string `json:"status_message"`
	}
	if json.Unmarshal(body, &e) == nil && e.StatusMessage != "" {
		return e.StatusMessage
	}
	if len(body) > 200 {
		body = body[:200]
	}
	return string(body)
}

func imageURL(size, path string) string {
	if path == "" {
		return ""
	}
	return imageBase + size + path
}

// ---- response shapes ----

type searchMovie struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	Popularity    float64 `json:"popularity"`
}

type searchTV struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	FirstAirDate string  `json:"first_air_date"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	Popularity   float64 `json:"popularity"`
}

type name struct {
	Name string `json:"name"`
}

type castMember struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
	Character   string `json:"character"`
}

type crewMember struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	Job         string `json:"job"`
}

type credits struct {
	Cast []castMember `json:"cast"`
	Crew []crewMember `json:"crew"`
}

type movieDetail struct {
	ID                  int64 `json:"id"`
	BelongsToCollection *struct {
		ID int64 `json:"id"`
	} `json:"belongs_to_collection"`
	Title               string  `json:"title"`
	Overview            string  `json:"overview"`
	ReleaseDate         string  `json:"release_date"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	Genres              []name  `json:"genres"`
	ProductionCompanies []name  `json:"production_companies"`
	Credits             credits `json:"credits"`
}

type tvDetail struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	Overview            string  `json:"overview"`
	FirstAirDate        string  `json:"first_air_date"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	Genres              []name  `json:"genres"`
	Networks            []name  `json:"networks"`
	ProductionCompanies []name  `json:"production_companies"`
	Credits             credits `json:"credits"`
	Seasons             []struct {
		SeasonNumber int `json:"season_number"`
		EpisodeCount int `json:"episode_count"`
	} `json:"seasons"`
}

type seasonDetail struct {
	Episodes []episodeDetail `json:"episodes"`
}

type searchCollection struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	PosterPath string `json:"poster_path"`
}

type collectionDetail struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	Overview     string        `json:"overview"`
	PosterPath   string        `json:"poster_path"`
	BackdropPath string        `json:"backdrop_path"`
	Parts        []searchMovie `json:"parts"`
}

type episodeDetail struct {
	Name         string       `json:"name"`
	Overview     string       `json:"overview"`
	AirDate      string       `json:"air_date"`
	StillPath    string       `json:"still_path"`
	SeasonNumber int          `json:"season_number"`
	EpisodeNum   int          `json:"episode_number"`
	GuestStars   []castMember `json:"guest_stars"`
	Crew         []crewMember `json:"crew"`
}

type personHit struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	ProfilePath        string `json:"profile_path"`
	KnownForDepartment string `json:"known_for_department"`
	KnownFor           []struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	} `json:"known_for"`
}

type personDetail struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Biography    string   `json:"biography"`
	Birthday     string   `json:"birthday"`
	PlaceOfBirth string   `json:"place_of_birth"`
	AlsoKnownAs  []string `json:"also_known_as"`
	Gender       int      `json:"gender"`
	ProfilePath  string   `json:"profile_path"`
}

type companyHit struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	LogoPath      string `json:"logo_path"`
	OriginCountry string `json:"origin_country"`
}

type companyDetail struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	LogoPath      string `json:"logo_path"`
	Headquarters  string `json:"headquarters"`
	OriginCountry string `json:"origin_country"`
	ParentCompany *name  `json:"parent_company"`
}

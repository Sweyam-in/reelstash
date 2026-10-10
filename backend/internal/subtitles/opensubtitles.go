// Package subtitles finds and downloads subtitles from OpenSubtitles.com
// (REST API v1). Searching needs an API key; downloading also needs the
// account's username and password, which are traded for a token that is
// kept for a day.
package subtitles

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the OpenSubtitles REST API. A login may move later calls
// to another host (the VIP server), which the client follows.
const DefaultBaseURL = "https://api.opensubtitles.com/api/v1"

// userAgent identifies ReelStash to OpenSubtitles, which requires one.
const userAgent = "ReelStash v1"

// maxSubtitleBytes bounds a downloaded subtitle file.
const maxSubtitleBytes = 8 << 20

// tokenLifetime is how long a login token is reused. OpenSubtitles tokens
// last 24 hours; logging in is rate limited, so it is done sparingly.
const tokenLifetime = 23 * time.Hour

var (
	// ErrNotConfigured means no API key was saved.
	ErrNotConfigured = errors.New("OpenSubtitles is not set up: add an API key under Providers")
	// ErrNoAccount means downloading was tried without a username and password.
	ErrNoAccount = errors.New("downloading from OpenSubtitles needs an account: add your username and password under Providers")
)

// Config is what the admin saves under Providers.
type Config struct {
	APIKey   string
	Username string
	Password string
	// BaseURL overrides DefaultBaseURL (tests).
	BaseURL string
}

// Enabled reports whether searching is possible.
func (c Config) Enabled() bool { return strings.TrimSpace(c.APIKey) != "" }

// CanDownload reports whether downloading is possible.
func (c Config) CanDownload() bool {
	return c.Enabled() && strings.TrimSpace(c.Username) != "" && c.Password != ""
}

// Client talks to OpenSubtitles with one saved configuration.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client for cfg.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

type session struct {
	token   string
	baseURL string
	expires time.Time
}

// sessions caches login tokens per API key and username, so every request
// does not log in again (OpenSubtitles allows a few logins an hour).
var (
	sessionsMu sync.Mutex
	sessions   = map[string]session{}
)

func (c *Client) sessionKey() string {
	return c.cfg.BaseURL + "\x00" + c.cfg.APIKey + "\x00" + c.cfg.Username
}

// ForgetSessions drops cached logins, after the settings change.
func ForgetSessions() {
	sessionsMu.Lock()
	sessions = map[string]session{}
	sessionsMu.Unlock()
}

// APIError is an error answer from OpenSubtitles.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("OpenSubtitles: %s (HTTP %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("OpenSubtitles answered HTTP %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, base, path string, query url.Values, body any, token string, out any) error {
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", c.cfg.APIKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach OpenSubtitles: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Message
		if msg == "" && len(e.Errors) > 0 {
			msg = strings.Join(e.Errors, "; ")
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("OpenSubtitles sent an answer ReelStash could not read: %w", err)
	}
	return nil
}

// Account is what OpenSubtitles reports about the signed-in user.
type Account struct {
	Level              string `json:"level"`
	AllowedDownloads   int    `json:"allowedDownloads"`
	RemainingDownloads int    `json:"remainingDownloads"`
	VIP                bool   `json:"vip"`
}

// login returns a token for the saved account, reusing a cached one.
func (c *Client) login(ctx context.Context, fresh bool) (session, error) {
	if !c.cfg.CanDownload() {
		return session{}, ErrNoAccount
	}
	key := c.sessionKey()
	sessionsMu.Lock()
	s, ok := sessions[key]
	sessionsMu.Unlock()
	if ok && !fresh && time.Now().Before(s.expires) {
		return s, nil
	}
	var out struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := c.do(ctx, http.MethodPost, c.cfg.BaseURL, "/login", nil,
		map[string]string{"username": c.cfg.Username, "password": c.cfg.Password}, "", &out); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			return session{}, errors.New("OpenSubtitles did not accept the username and password")
		}
		return session{}, err
	}
	if out.Token == "" {
		return session{}, errors.New("OpenSubtitles did not return a login token")
	}
	s = session{token: out.Token, baseURL: c.cfg.BaseURL, expires: time.Now().Add(tokenLifetime)}
	// The answer names a host ("vip-api.opensubtitles.com") to use from now on.
	if host := strings.TrimSpace(out.BaseURL); host != "" && c.cfg.BaseURL == DefaultBaseURL {
		if !strings.Contains(host, "://") {
			host = "https://" + host
		}
		if pu, err := url.Parse(host); err == nil && strings.HasSuffix(pu.Hostname(), "opensubtitles.com") {
			s.baseURL = "https://" + pu.Host + "/api/v1"
		}
	}
	sessionsMu.Lock()
	sessions[key] = s
	sessionsMu.Unlock()
	return s, nil
}

// Test checks the saved settings: the API key, and the account when one is
// saved. It reports the account's download allowance.
func (c *Client) Test(ctx context.Context) (*Account, error) {
	if !c.cfg.Enabled() {
		return nil, ErrNotConfigured
	}
	if !c.cfg.CanDownload() {
		// No account: check the key with a cheap call.
		if err := c.do(ctx, http.MethodGet, c.cfg.BaseURL, "/infos/formats", nil, nil, "", nil); err != nil {
			return nil, err
		}
		return nil, nil
	}
	s, err := c.login(ctx, true)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Level              string `json:"level"`
			AllowedDownloads   int    `json:"allowed_downloads"`
			RemainingDownloads int    `json:"remaining_downloads"`
			VIP                bool   `json:"vip"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, s.baseURL, "/infos/user", nil, nil, s.token, &out); err != nil {
		return nil, err
	}
	return &Account{Level: out.Data.Level, AllowedDownloads: out.Data.AllowedDownloads, RemainingDownloads: out.Data.RemainingDownloads, VIP: out.Data.VIP}, nil
}

// Query is a subtitle search.
type Query struct {
	// Text is a title or file name.
	Text string
	// MovieHash is the OpenSubtitles hash of the video file (see FileHash).
	MovieHash string
	// Languages are 2-letter codes; empty means every language.
	Languages []string
	IMDbID    string
	TMDbID    string
}

// Result is one subtitle offered by OpenSubtitles.
type Result struct {
	FileID          int64   `json:"fileId"`
	FileName        string  `json:"fileName"`
	Language        string  `json:"language"`
	Release         string  `json:"release"`
	Title           string  `json:"title,omitempty"`
	Year            int     `json:"year,omitempty"`
	Downloads       int     `json:"downloads"`
	Rating          float64 `json:"rating,omitempty"`
	HearingImpaired bool    `json:"hearingImpaired"`
	ForeignOnly     bool    `json:"foreignPartsOnly"`
	MachineMade     bool    `json:"machineTranslated"`
	AIMade          bool    `json:"aiTranslated"`
	Trusted         bool    `json:"trusted"`
	HashMatch       bool    `json:"hashMatch"`
	FPS             float64 `json:"fps,omitempty"`
	Uploaded        string  `json:"uploaded,omitempty"`
	URL             string  `json:"url,omitempty"`
}

// Search looks for subtitles. Results that match the file's hash come first,
// then the most downloaded.
func (c *Client) Search(ctx context.Context, q Query) ([]Result, error) {
	if !c.cfg.Enabled() {
		return nil, ErrNotConfigured
	}
	v := url.Values{}
	if q.MovieHash != "" {
		v.Set("moviehash", q.MovieHash)
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		v.Set("query", strings.ToLower(t))
	}
	if q.IMDbID != "" {
		v.Set("imdb_id", strings.TrimPrefix(strings.ToLower(q.IMDbID), "tt"))
	}
	if q.TMDbID != "" {
		v.Set("tmdb_id", q.TMDbID)
	}
	if len(q.Languages) > 0 {
		langs := append([]string(nil), q.Languages...)
		for i := range langs {
			langs[i] = strings.ToLower(strings.TrimSpace(langs[i]))
		}
		sort.Strings(langs) // the API asks for them in alphabetical order
		v.Set("languages", strings.Join(langs, ","))
	}
	if len(v) == 0 {
		return nil, errors.New("type something to search for")
	}
	base := c.cfg.BaseURL
	token := ""
	if c.cfg.CanDownload() {
		// The VIP host wants the token on every call; the regular one ignores it.
		if s, err := c.login(ctx, false); err == nil {
			base, token = s.baseURL, s.token
		}
	}
	var out struct {
		Data []struct {
			Attributes struct {
				Language         string  `json:"language"`
				DownloadCount    int     `json:"download_count"`
				HearingImpaired  bool    `json:"hearing_impaired"`
				ForeignPartsOnly bool    `json:"foreign_parts_only"`
				MachineTrans     bool    `json:"machine_translated"`
				AITrans          bool    `json:"ai_translated"`
				FromTrusted      bool    `json:"from_trusted"`
				FPS              float64 `json:"fps"`
				Ratings          float64 `json:"ratings"`
				Release          string  `json:"release"`
				UploadDate       string  `json:"upload_date"`
				MoviehashMatch   bool    `json:"moviehash_match"`
				URL              string  `json:"url"`
				FeatureDetails   struct {
					Title     string `json:"title"`
					MovieName string `json:"movie_name"`
					Year      int    `json:"year"`
				} `json:"feature_details"`
				Files []struct {
					FileID   int64  `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, base, "/subtitles", v, nil, token, &out); err != nil {
		return nil, err
	}
	results := []Result{}
	for _, d := range out.Data {
		a := d.Attributes
		title := a.FeatureDetails.MovieName
		if title == "" {
			title = a.FeatureDetails.Title
		}
		// A subtitle split over several CDs has a file per part; only
		// single-file subtitles fit a single video.
		if len(a.Files) != 1 {
			continue
		}
		f := a.Files[0]
		results = append(results, Result{
			FileID: f.FileID, FileName: f.FileName, Language: strings.ToLower(a.Language), Release: a.Release,
			Title: title, Year: a.FeatureDetails.Year, Downloads: a.DownloadCount, Rating: a.Ratings,
			HearingImpaired: a.HearingImpaired, ForeignOnly: a.ForeignPartsOnly, MachineMade: a.MachineTrans, AIMade: a.AITrans,
			Trusted: a.FromTrusted, HashMatch: a.MoviehashMatch, FPS: a.FPS, Uploaded: a.UploadDate, URL: a.URL,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].HashMatch != results[j].HashMatch {
			return results[i].HashMatch
		}
		return results[i].Downloads > results[j].Downloads
	})
	return results, nil
}

// Download fetches one subtitle file (always SRT, UTF-8) and reports how many
// downloads the account has left today.
func (c *Client) Download(ctx context.Context, fileID int64) (data []byte, fileName string, remaining int, err error) {
	if !c.cfg.Enabled() {
		return nil, "", 0, ErrNotConfigured
	}
	s, err := c.login(ctx, false)
	if err != nil {
		return nil, "", 0, err
	}
	var out struct {
		Link      string `json:"link"`
		FileName  string `json:"file_name"`
		Remaining int    `json:"remaining"`
		Message   string `json:"message"`
		ResetTime string `json:"reset_time"`
	}
	req := map[string]any{"file_id": fileID, "sub_format": "srt"}
	err = c.do(ctx, http.MethodPost, s.baseURL, "/download", nil, req, s.token, &out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		// The cached token expired early: log in again once.
		if s, err = c.login(ctx, true); err != nil {
			return nil, "", 0, err
		}
		err = c.do(ctx, http.MethodPost, s.baseURL, "/download", nil, req, s.token, &out)
	}
	if err != nil {
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotAcceptable || apiErr.Status == http.StatusTooManyRequests) {
			return nil, "", 0, fmt.Errorf("the OpenSubtitles download limit for today is used up: %s", apiErr.Message)
		}
		return nil, "", 0, err
	}
	if out.Link == "" {
		msg := out.Message
		if msg == "" {
			msg = "no download link was returned"
		}
		return nil, "", out.Remaining, errors.New("OpenSubtitles: " + msg)
	}
	lreq, err := http.NewRequestWithContext(ctx, http.MethodGet, out.Link, nil)
	if err != nil {
		return nil, "", 0, err
	}
	lreq.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(lreq)
	if err != nil {
		return nil, "", 0, fmt.Errorf("could not fetch the subtitle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", 0, fmt.Errorf("could not fetch the subtitle: HTTP %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxSubtitleBytes+1))
	if err != nil {
		return nil, "", 0, err
	}
	if len(data) > maxSubtitleBytes {
		return nil, "", 0, errors.New("the subtitle file is too large")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, "", 0, errors.New("the subtitle file is empty")
	}
	return data, out.FileName, out.Remaining, nil
}

// hashChunk is how much of each end of a file the OpenSubtitles hash reads.
const hashChunk = 64 * 1024

// FileHash computes the OpenSubtitles "moviehash" of a video: its size plus
// the sum of the first and last 64 KiB read as little-endian 64-bit words.
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size < hashChunk {
		return "", errors.New("file is too small to hash")
	}
	sum := uint64(size)
	buf := make([]byte, hashChunk)
	for _, off := range []int64{0, size - hashChunk} {
		if _, err := f.ReadAt(buf, off); err != nil {
			return "", err
		}
		for i := 0; i < hashChunk; i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", sum), nil
}

// ParseLanguages splits "en, fr" into codes.
func ParseLanguages(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

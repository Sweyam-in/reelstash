package extprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"reelstash/backend/internal/provider"
)

const (
	// IDPrefix marks plugin providers, keeping their ids apart from built-in ones.
	IDPrefix     = "x-"
	maxBody      = 4 << 20
	callTimeout  = 25 * time.Second
	maxCandidate = 100
	// probeID names the throwaway provider Discover uses; its calls are not logged.
	probeID = "probe"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}$`)

// Spec is what ReelStash remembers about a plugin between restarts.
type Spec struct {
	ID         string       `json:"id"` // prefixed: "x-<manifest id>"
	Name       string       `json:"name"`
	BaseURL    string       `json:"baseUrl"`
	Caps       Capabilities `json:"capabilities"`
	ImageHosts []string     `json:"imageHosts"`
	Notice     string       `json:"notice,omitempty"`
	// Disabled keeps the plugin connected but out of matching.
	Disabled bool `json:"disabled,omitempty"`
	// Bundle names the installed plugin bundle that runs this service. Bundle
	// providers are registered by the bundle manager (their address changes on
	// every start), so they are never saved with the connected plugins.
	Bundle string `json:"bundle,omitempty"`
	// CanTest is set when the service offers POST /test (see Manifest.Test).
	CanTest bool `json:"canTest,omitempty"`
}

// Provider is a provider.Provider backed by a remote HTTP service.
type Provider struct {
	client *http.Client

	mu    sync.RWMutex
	spec  Spec
	token string
}

var (
	_ provider.Provider = (*Provider)(nil)
	_ provider.Noticer  = (*Provider)(nil)
)

func New(spec Spec, token string) *Provider {
	return &Provider{spec: spec, token: token, client: &http.Client{Timeout: callTimeout}}
}

// NewProvider returns the provider for spec, which also implements
// provider.SeriesProvider when the plugin declared series support.
func NewProvider(spec Spec, token string) provider.Provider {
	p := New(spec, token)
	if spec.Caps.Series {
		return &seriesProvider{p}
	}
	return p
}

// Spec returns a copy of the current plugin description.
func (p *Provider) Spec() Spec {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.spec
}

func (p *Provider) BaseURL() string { return p.Spec().BaseURL }

func (p *Provider) ID() string   { return p.Spec().ID }
func (p *Provider) Name() string { return p.Spec().Name }
func (p *Provider) Capabilities() provider.Caps {
	c := p.Spec().Caps
	return provider.Caps{Media: c.Media, Performer: c.Performer, Studio: c.Studio}
}
func (p *Provider) Enabled() bool {
	s := p.Spec()
	return s.BaseURL != "" && !s.Disabled
}

// SetDisabled switches the plugin off or on without disconnecting it.
func (p *Provider) SetDisabled(disabled bool) {
	p.mu.Lock()
	p.spec.Disabled = disabled
	p.mu.Unlock()
}

// Bundle is the installed bundle that runs this plugin, or "".
func (p *Provider) Bundle() string { return p.Spec().Bundle }

func (p *Provider) ImageHosts() []string { return p.Spec().ImageHosts }
func (p *Provider) Notice() string       { return p.Spec().Notice }

func (p *Provider) ConfigFields() []provider.Field {
	if p.Spec().Bundle != "" {
		return nil // a bundle's settings live on its Plugins entry; its token is generated
	}
	return []provider.Field{{Key: "token", Label: "Access token sent as a Bearer header (optional)", Secret: true}}
}

// Configure accepts "token".
func (p *Provider) Configure(cfg map[string]string) error {
	if v, ok := cfg["token"]; ok {
		p.mu.Lock()
		p.token = v
		p.mu.Unlock()
	}
	return nil
}

func (p *Provider) Test(ctx context.Context) error {
	m, err := p.fetchManifest(ctx)
	if err != nil {
		return err
	}
	if IDPrefix+m.ID != p.ID() {
		return fmt.Errorf("the service now answers as %q, not %q", m.ID, strings.TrimPrefix(p.ID(), IDPrefix))
	}
	// A plugin may offer POST /test to check its own settings (for example an
	// API token) with a real call; one that does not is simply reachable.
	if p.Spec().CanTest {
		return p.do(ctx, http.MethodPost, "/test", nil, map[string]any{}, nil)
	}
	return nil
}

// ErrNotFound is returned for an unknown external id.
var ErrNotFound = errors.New("not found at the provider")

func (p *Provider) do(ctx context.Context, method, path string, query url.Values, body any, out any) (err error) {
	spec := p.Spec()
	u := strings.TrimRight(spec.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	p.mu.RLock()
	token := p.token
	p.mu.RUnlock()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	started := time.Now()
	call := Call{At: started, Method: method, Path: pathWithQuery(path, req.URL.RawQuery)}
	defer func() {
		call.DurationMs = time.Since(started).Milliseconds()
		if err != nil {
			call.Error = err.Error()
		}
		recordCall(spec.ID, call)
	}()
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.Name, err)
	}
	defer resp.Body.Close()
	call.Status = resp.StatusCode
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(raw) > maxBody {
		return fmt.Errorf("%s: response is larger than %d MB", spec.Name, maxBody>>20)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusNotImplemented:
		return provider.ErrUnsupported
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s refused the access token (HTTP %d)", spec.Name, resp.StatusCode)
	case resp.StatusCode >= 300:
		return fmt.Errorf("%s answered HTTP %d: %s", spec.Name, resp.StatusCode, errorText(raw))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s sent invalid JSON: %w", spec.Name, err)
	}
	return nil
}

// errorText is what a plugin said went wrong: its {"error": "..."} message when
// it sent one, else the start of the body.
func errorText(raw []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && strings.TrimSpace(body.Error) != "" {
		return snippet([]byte(body.Error))
	}
	return snippet(raw)
}

func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func (p *Provider) fetchManifest(ctx context.Context) (Manifest, error) {
	var m Manifest
	err := p.do(ctx, http.MethodGet, "/manifest", nil, nil, &m)
	return m, err
}

// Discover asks a service for its manifest and validates it, returning the
// Spec to store. Nothing is registered yet.
func Discover(ctx context.Context, baseURL, token string) (Spec, error) {
	baseURL = strings.TrimSpace(baseURL)
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Spec{}, errors.New("the address must be a full http:// or https:// URL")
	}
	baseURL = strings.TrimRight(u.String(), "/")
	probe := New(Spec{ID: probeID, Name: u.Host, BaseURL: baseURL}, token)
	m, err := probe.fetchManifest(ctx)
	if err != nil {
		return Spec{}, err
	}
	if !idPattern.MatchString(m.ID) {
		return Spec{}, errors.New("the manifest id must be 2-31 lowercase letters, digits or dashes")
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.ID
	}
	c := m.Capabilities
	if !c.Media && !c.Performer && !c.Studio {
		return Spec{}, errors.New("the manifest declares no capabilities (media, performer or studio)")
	}
	hosts := []string{}
	for _, h := range m.ImageHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" && !strings.ContainsAny(h, "/: ") {
			hosts = append(hosts, h)
		}
	}
	return Spec{ID: IDPrefix + m.ID, Name: strings.TrimSpace(m.Name), BaseURL: baseURL, Caps: c, ImageHosts: hosts, Notice: strings.TrimSpace(m.Notice), CanTest: m.Test}, nil
}

// --- media ---

func (p *Provider) SearchMedia(ctx context.Context, q provider.MediaQuery) ([]provider.MediaCandidate, error) {
	if !p.Spec().Caps.Media {
		return nil, provider.ErrUnsupported
	}
	var resp struct {
		Candidates []wireCandidate `json:"candidates"`
	}
	body := map[string]string{"fileName": q.FileName, "text": q.Text, "searchType": q.SearchType}
	if err := p.do(ctx, http.MethodPost, "/media/search", nil, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Candidates) > maxCandidate {
		resp.Candidates = resp.Candidates[:maxCandidate]
	}
	id := p.ID()
	out := make([]provider.MediaCandidate, 0, len(resp.Candidates))
	for _, c := range resp.Candidates {
		if c.Result.ID == "" {
			continue
		}
		out = append(out, provider.MediaCandidate{Result: c.Result.toResult(id), Score: clamp(c.Score), AutoMatch: c.AutoMatch, StudioHit: c.StudioHit, DateHit: c.DateHit})
	}
	return out, nil
}

func clamp(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func (p *Provider) GetMedia(ctx context.Context, externalID string) (provider.MediaResult, error) {
	if !p.Spec().Caps.Media {
		return provider.MediaResult{}, provider.ErrUnsupported
	}
	var m wireMedia
	if err := p.do(ctx, http.MethodGet, "/media/"+url.PathEscape(externalID), nil, nil, &m); err != nil {
		return provider.MediaResult{}, err
	}
	if m.ID == "" {
		m.ID = externalID
	}
	return m.toResult(p.ID()), nil
}

// --- performers ---

func (p *Provider) SearchPerformers(ctx context.Context, name string) ([]provider.PerformerResult, error) {
	if !p.Spec().Caps.Performer {
		return nil, provider.ErrUnsupported
	}
	var resp struct {
		Results []wirePerformer `json:"results"`
	}
	if err := p.do(ctx, http.MethodGet, "/performers/search", url.Values{"q": {name}}, nil, &resp); err != nil {
		return nil, err
	}
	out := []provider.PerformerResult{}
	for _, r := range resp.Results {
		if r.ID != "" && len(out) < maxCandidate {
			out = append(out, r.toResult(p.ID()))
		}
	}
	return out, nil
}

func (p *Provider) GetPerformer(ctx context.Context, externalID string) (provider.PerformerResult, error) {
	if !p.Spec().Caps.Performer {
		return provider.PerformerResult{}, provider.ErrUnsupported
	}
	var r wirePerformer
	if err := p.do(ctx, http.MethodGet, "/performers/"+url.PathEscape(externalID), nil, nil, &r); err != nil {
		return provider.PerformerResult{}, err
	}
	if r.ID == "" {
		r.ID = externalID
	}
	return r.toResult(p.ID()), nil
}

// --- studios ---

func (p *Provider) SearchStudios(ctx context.Context, name string) ([]provider.StudioResult, error) {
	if !p.Spec().Caps.Studio {
		return nil, provider.ErrUnsupported
	}
	var resp struct {
		Results []wireStudio `json:"results"`
	}
	if err := p.do(ctx, http.MethodGet, "/studios/search", url.Values{"q": {name}}, nil, &resp); err != nil {
		return nil, err
	}
	out := []provider.StudioResult{}
	for _, r := range resp.Results {
		if r.ID != "" && len(out) < maxCandidate {
			out = append(out, r.toResult(p.ID()))
		}
	}
	return out, nil
}

func (p *Provider) GetStudio(ctx context.Context, externalID string) (provider.StudioResult, error) {
	if !p.Spec().Caps.Studio {
		return provider.StudioResult{}, provider.ErrUnsupported
	}
	var r wireStudio
	if err := p.do(ctx, http.MethodGet, "/studios/"+url.PathEscape(externalID), nil, nil, &r); err != nil {
		return provider.StudioResult{}, err
	}
	if r.ID == "" {
		r.ID = externalID
	}
	return r.toResult(p.ID()), nil
}

// --- series (optional) ---

type seriesProvider struct{ *Provider }

var _ provider.SeriesProvider = (*seriesProvider)(nil)

func (s *seriesProvider) SearchSeries(ctx context.Context, query string) ([]provider.SeriesResult, error) {
	var resp struct {
		Results []wireSeries `json:"results"`
	}
	if err := s.do(ctx, http.MethodGet, "/series/search", url.Values{"q": {query}}, nil, &resp); err != nil {
		return nil, err
	}
	out := []provider.SeriesResult{}
	for _, r := range resp.Results {
		if r.ID != "" && len(out) < maxCandidate {
			out = append(out, r.toResult(s.ID()))
		}
	}
	return out, nil
}

func (s *seriesProvider) GetSeries(ctx context.Context, externalID string) (provider.SeriesResult, error) {
	var r wireSeries
	if err := s.do(ctx, http.MethodGet, "/series/"+url.PathEscape(externalID), nil, nil, &r); err != nil {
		return provider.SeriesResult{}, err
	}
	if r.ID == "" {
		r.ID = externalID
	}
	return r.toResult(s.ID()), nil
}

// Package authplugin lets sign-in live outside ReelStash. An auth plugin is a
// small HTTP service (usually run from an installed plugin bundle) that proves
// who a visitor is; ReelStash keeps the user record, the role and the session.
// See docs/auth-plugins.md for the contract.
package authplugin

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
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	LoginRedirect = "redirect" // the browser is sent to the plugin's identity provider
	LoginPassword = "password" // ReelStash shows a username/password form and asks the plugin

	maxBody     = 1 << 20
	callTimeout = 20 * time.Second
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}$`)

// Manifest is what GET {base}/manifest returns.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Login       Login  `json:"login"`
	// SubjectPrefix is put before the plugin's subjects to form the stored
	// identity key. Absent means "<id>:", which keeps two plugins' users apart.
	// An empty string stores subjects as they are, which lets a plugin take over
	// users an earlier sign-in method created.
	SubjectPrefix *string `json:"subjectPrefix,omitempty"`
	// CallbackPath is the address (under /auth/) the provider sends the browser
	// back to, when it must not be the default /auth/plugin/<id>/callback: a
	// provider's client may already be registered with an older address.
	CallbackPath string `json:"callbackPath,omitempty"`
	// LogoutURL is a page to open after sign-out, to end the provider's session
	// too. It is only offered on the host it is on.
	LogoutURL string `json:"logoutUrl,omitempty"`
}

// reservedPaths are /auth addresses ReelStash serves itself.
var reservedPaths = map[string]bool{
	"/auth/config": true, "/auth/login": true, "/auth/logout": true, "/auth/setup": true,
	"/auth/native-login": true, "/auth/local-login": true,
}

// Login says how people sign in with the plugin.
type Login struct {
	Type  string `json:"type"`
	Label string `json:"label,omitempty"` // button text, for example "Continue with SSO"
}

// Identity is a verified person, as reported by the plugin.
type Identity struct {
	Subject     string `json:"subject"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	// Role is "ADMIN" or "USER" (the default). A user who is already an admin
	// in ReelStash is never demoted by a plugin.
	Role string `json:"role,omitempty"`
}

// Result is the outcome of finishing a sign-in.
type Result struct {
	Identity   *Identity `json:"identity,omitempty"`
	RedirectTo string    `json:"redirectTo,omitempty"`
	// Error is one of: cancelled, not_authorized, account_disabled,
	// login_failed. Set instead of Identity when sign-in did not succeed.
	Error string `json:"error,omitempty"`
}

// Plugin is a connected auth plugin.
type Plugin struct {
	manifest Manifest
	baseURL  string
	token    string
	client   *http.Client
}

// ErrUnsupported is returned for a flow the plugin does not offer.
var ErrUnsupported = errors.New("the plugin does not support this sign-in method")

// New returns a client for the service at baseURL. The token, if any, is sent
// as a Bearer header.
func New(m Manifest, baseURL, token string) *Plugin {
	return &Plugin{manifest: m, baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: callTimeout}}
}

// Discover reads and validates a service's manifest.
func Discover(ctx context.Context, baseURL, token string) (*Plugin, error) {
	p := New(Manifest{}, baseURL, token)
	var m Manifest
	if err := p.do(ctx, http.MethodGet, "/manifest", nil, &m); err != nil {
		return nil, err
	}
	if !idPattern.MatchString(m.ID) {
		return nil, errors.New("the manifest id must be 2-31 lowercase letters, digits or dashes")
	}
	if m.Login.Type != LoginRedirect && m.Login.Type != LoginPassword {
		return nil, fmt.Errorf("the manifest login type must be %q or %q", LoginRedirect, LoginPassword)
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.ID
	}
	if cp := m.CallbackPath; cp != "" {
		if !strings.HasPrefix(cp, "/auth/") || strings.ContainsAny(cp, "?#") || strings.Contains(cp, "..") || strings.HasPrefix(cp, "/auth/plugin/") || reservedPaths[cp] {
			return nil, errors.New("callbackPath must be an unused address under /auth/, such as /auth/callback")
		}
	}
	if lu := m.LogoutURL; lu != "" {
		if u, err := url.Parse(lu); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("logoutUrl must be a full http:// or https:// address")
		}
	}
	p.manifest = m
	return p, nil
}

func (p *Plugin) Manifest() Manifest { return p.manifest }
func (p *Plugin) ID() string         { return p.manifest.ID }
func (p *Plugin) Name() string       { return p.manifest.Name }

// Label is the sign-in button text.
func (p *Plugin) Label() string {
	if l := strings.TrimSpace(p.manifest.Login.Label); l != "" {
		return l
	}
	return "Continue with " + p.manifest.Name
}

// SubjectKey turns a plugin's subject into the key stored for the user.
func (p *Plugin) SubjectKey(subject string) string {
	prefix := p.manifest.ID + ":"
	if p.manifest.SubjectPrefix != nil {
		prefix = *p.manifest.SubjectPrefix
	}
	return prefix + subject
}

// Start begins a redirect sign-in and returns where to send the browser.
// callbackURL is ReelStash's own callback for this plugin; redirectTo is the
// page to return to afterwards, and comes back from Finish.
func (p *Plugin) Start(ctx context.Context, callbackURL, redirectTo string) (string, error) {
	if p.manifest.Login.Type != LoginRedirect {
		return "", ErrUnsupported
	}
	var out struct {
		URL string `json:"url"`
	}
	body := map[string]string{"callbackUrl": callbackURL, "redirectTo": redirectTo}
	if err := p.do(ctx, http.MethodPost, "/auth/start", body, &out); err != nil {
		return "", err
	}
	u, err := url.Parse(out.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("the plugin did not return a valid sign-in address")
	}
	return out.URL, nil
}

// Finish completes a redirect sign-in with the query the browser came back
// with (state, code, error, ...).
func (p *Plugin) Finish(ctx context.Context, callbackURL string, query url.Values) (Result, error) {
	if p.manifest.Login.Type != LoginRedirect {
		return Result{}, ErrUnsupported
	}
	flat := map[string]string{}
	for k := range query {
		flat[k] = query.Get(k)
	}
	var res Result
	err := p.do(ctx, http.MethodPost, "/auth/finish", map[string]any{"callbackUrl": callbackURL, "query": flat}, &res)
	return p.checked(res, err)
}

// Password checks a username and password with the plugin.
func (p *Plugin) Password(ctx context.Context, username, password string) (Result, error) {
	if p.manifest.Login.Type != LoginPassword {
		return Result{}, ErrUnsupported
	}
	var res Result
	err := p.do(ctx, http.MethodPost, "/auth/login", map[string]string{"username": username, "password": password}, &res)
	return p.checked(res, err)
}

func (p *Plugin) checked(res Result, err error) (Result, error) {
	if err != nil {
		return Result{}, err
	}
	if res.Error != "" {
		return res, nil
	}
	if res.Identity == nil || strings.TrimSpace(res.Identity.Subject) == "" {
		return Result{}, errors.New("the plugin answered without a subject")
	}
	if res.Identity.Role != "ADMIN" {
		res.Identity.Role = "USER"
	}
	return res, nil
}

func (p *Plugin) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("auth plugin: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(raw) > maxBody {
		return errors.New("auth plugin: response is too large")
	}
	switch {
	case resp.StatusCode == http.StatusNotImplemented:
		return ErrUnsupported
	case resp.StatusCode >= 300:
		return fmt.Errorf("auth plugin answered HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("auth plugin sent invalid JSON: %w", err)
	}
	return nil
}

// Registry holds the auth plugins that are running now.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin
}

func NewRegistry() *Registry { return &Registry{plugins: map[string]*Plugin{}} }

func (r *Registry) Register(p *Plugin) {
	r.mu.Lock()
	r.plugins[p.ID()] = p
	r.mu.Unlock()
}

func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	delete(r.plugins, id)
	r.mu.Unlock()
}

func (r *Registry) Get(id string) (*Plugin, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	return p, ok
}

// List returns the running plugins, ordered by name.
func (r *Registry) List() []*Plugin {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

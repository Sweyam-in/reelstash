// Package qbit is a small client for the qBittorrent WebUI API (v2). It signs
// in with a username and password, keeps the session cookie, and signs in
// again when the session expires.
package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotConfigured means no qBittorrent address is set.
	ErrNotConfigured = errors.New("qBittorrent is not set up (add its address under Downloads → Settings)")
	// ErrLogin means qBittorrent refused the username or password.
	ErrLogin = errors.New("qBittorrent refused the username or password")
	// ErrBanned means qBittorrent temporarily banned this address after
	// too many failed sign-ins.
	ErrBanned = errors.New("qBittorrent has temporarily banned ReelStash after failed sign-ins; check the password and try again later")
)

// Config is how to reach one qBittorrent.
type Config struct {
	URL      string
	Username string
	Password string
}

// Enabled reports whether an address is set.
func (c Config) Enabled() bool { return strings.TrimSpace(c.URL) != "" }

// Client talks to one qBittorrent WebUI. It is safe for concurrent use.
type Client struct {
	cfg  Config
	base string
	http *http.Client

	mu  sync.Mutex
	sid string
	// loginFailedAt backs off sign-in after a refusal, so a wrong password
	// does not get ReelStash banned by qBittorrent's brute-force guard.
	loginFailedAt time.Time
	loginErr      error

	verMu      sync.Mutex
	apiVersion string
}

// New returns a client for cfg.
func New(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		base: strings.TrimRight(strings.TrimSpace(cfg.URL), "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Config returns the settings the client was built from.
func (c *Client) Config() Config { return c.cfg }

// loginBackoff is how long a refused sign-in blocks the next attempt.
const loginBackoff = 30 * time.Second

func (c *Client) login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loginErr != nil && time.Since(c.loginFailedAt) < loginBackoff {
		return c.loginErr
	}
	form := url.Values{"username": {c.cfg.Username}, "password": {c.cfg.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// qBittorrent compares Referer/Origin with its own address when present.
	req.Header.Set("Referer", c.base)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach qBittorrent at %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusForbidden:
		c.loginErr, c.loginFailedAt = ErrBanned, time.Now()
		return ErrBanned
	case resp.StatusCode == http.StatusUnauthorized:
		// qBittorrent 5.2+ refuses with 401 instead of "Fails.".
		c.loginErr, c.loginFailedAt = ErrLogin, time.Now()
		return ErrLogin
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		// 200 "Ok." before 5.2, 204 No Content since.
		return fmt.Errorf("qBittorrent sign-in failed: %s", resp.Status)
	}
	sid := ""
	for _, ck := range resp.Cookies() {
		// "SID" before qBittorrent 5.2, "QBT_SID_<port>" since.
		if ck.Name == "SID" || strings.Contains(ck.Name, "SID") {
			sid = ck.Name + "=" + ck.Value
		}
	}
	if sid == "" {
		// No cookie: either refused ("Fails.") or authentication is
		// bypassed for this address, in which case requests work without one.
		if strings.TrimSpace(string(body)) == "Fails." {
			c.loginErr, c.loginFailedAt = ErrLogin, time.Now()
			return ErrLogin
		}
		sid = "-"
	}
	c.sid, c.loginErr = sid, nil
	return nil
}

func (c *Client) session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sid
}

func (c *Client) dropSession(stale string) {
	c.mu.Lock()
	if c.sid == stale {
		c.sid = ""
	}
	c.mu.Unlock()
}

// HTTPError is a non-2xx answer from qBittorrent.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("qBittorrent: %s (%d)", e.Body, e.Status)
	}
	return fmt.Sprintf("qBittorrent answered %d %s", e.Status, http.StatusText(e.Status))
}

// IsStatus reports whether err is an HTTPError with the status.
func IsStatus(err error, status int) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == status
}

// do sends one request, signing in first when needed and once more when the
// session turns out to have expired. build makes a fresh request each try.
func (c *Client) do(ctx context.Context, build func() (*http.Request, error)) ([]byte, error) {
	if c.base == "" {
		return nil, ErrNotConfigured
	}
	for attempt := 0; ; attempt++ {
		sid := c.session()
		if sid == "" {
			if err := c.login(ctx); err != nil {
				return nil, err
			}
			sid = c.session()
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		if sid != "-" {
			req.Header.Set("Cookie", sid)
		}
		req.Header.Set("Referer", c.base)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("could not reach qBittorrent at %s: %w", c.base, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusForbidden && attempt == 0 {
			c.dropSession(sid)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			msg := strings.TrimSpace(string(body))
			if len(msg) > 300 || strings.HasPrefix(msg, "<") {
				msg = ""
			}
			return nil, &HTTPError{Status: resp.StatusCode, Body: msg}
		}
		return body, nil
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.do(ctx, func() (*http.Request, error) {
		u := c.base + "/api/v2/" + path
		if len(query) > 0 {
			u += "?" + query.Encode()
		}
		return http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	})
}

func (c *Client) post(ctx context.Context, path string, form url.Values) ([]byte, error) {
	return c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/"+path, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return req, err
	})
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	body, err := c.get(ctx, path, query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("qBittorrent sent an unexpected answer to %s: %w", path, err)
	}
	return nil
}

// Version is qBittorrent's own version, such as "v5.0.2".
func (c *Client) Version(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "app/version", nil)
	return strings.TrimSpace(string(body)), err
}

// APIVersion is the WebUI API version, such as "2.11.2". It is cached.
func (c *Client) APIVersion(ctx context.Context) (string, error) {
	c.verMu.Lock()
	defer c.verMu.Unlock()
	if c.apiVersion != "" {
		return c.apiVersion, nil
	}
	body, err := c.get(ctx, "app/webapiVersion", nil)
	if err != nil {
		return "", err
	}
	c.apiVersion = strings.TrimSpace(string(body))
	return c.apiVersion, nil
}

// VersionAtLeast compares dotted versions ("2.11.0" >= "2.9").
func VersionAtLeast(have, want string) bool {
	h, w := strings.Split(strings.TrimPrefix(have, "v"), "."), strings.Split(strings.TrimPrefix(want, "v"), ".")
	for i := 0; i < len(h) || i < len(w); i++ {
		var a, b int
		if i < len(h) {
			a, _ = strconv.Atoi(h[i])
		}
		if i < len(w) {
			b, _ = strconv.Atoi(w[i])
		}
		if a != b {
			return a > b
		}
	}
	return true
}

// stopStartNames returns the action names for pausing and resuming:
// qBittorrent 5 (API 2.11) renamed pause/resume to stop/start.
func (c *Client) stopStartNames(ctx context.Context) (stop, start string) {
	if v, err := c.APIVersion(ctx); err == nil && !VersionAtLeast(v, "2.11.0") {
		return "pause", "resume"
	}
	return "stop", "start"
}

// MainData is one answer of sync/maindata. Torrents hold only the fields that
// changed since rid, unless FullUpdate is set.
type MainData struct {
	RID               int64                     `json:"rid"`
	FullUpdate        bool                      `json:"full_update"`
	Torrents          map[string]map[string]any `json:"torrents"`
	TorrentsRemoved   []string                  `json:"torrents_removed"`
	Categories        map[string]map[string]any `json:"categories"`
	CategoriesRemoved []string                  `json:"categories_removed"`
	Tags              []string                  `json:"tags"`
	TagsRemoved       []string                  `json:"tags_removed"`
	ServerState       map[string]any            `json:"server_state"`
}

// MainData fetches what changed since rid (0 = everything).
func (c *Client) MainData(ctx context.Context, rid int64) (MainData, error) {
	var md MainData
	err := c.getJSON(ctx, "sync/maindata", url.Values{"rid": {strconv.FormatInt(rid, 10)}}, &md)
	return md, err
}

// Files lists a torrent's files.
func (c *Client) Files(ctx context.Context, hash string) ([]map[string]any, error) {
	out := []map[string]any{}
	err := c.getJSON(ctx, "torrents/files", url.Values{"hash": {hash}}, &out)
	return out, err
}

// Trackers lists a torrent's trackers.
func (c *Client) Trackers(ctx context.Context, hash string) ([]map[string]any, error) {
	out := []map[string]any{}
	err := c.getJSON(ctx, "torrents/trackers", url.Values{"hash": {hash}}, &out)
	return out, err
}

// Properties returns a torrent's general properties.
func (c *Client) Properties(ctx context.Context, hash string) (map[string]any, error) {
	out := map[string]any{}
	err := c.getJSON(ctx, "torrents/properties", url.Values{"hash": {hash}}, &out)
	return out, err
}

// Peers returns a torrent's connected peers.
func (c *Client) Peers(ctx context.Context, hash string) (map[string]any, error) {
	out := struct {
		Peers map[string]any `json:"peers"`
	}{}
	err := c.getJSON(ctx, "sync/torrentPeers", url.Values{"hash": {hash}, "rid": {"0"}}, &out)
	if out.Peers == nil {
		out.Peers = map[string]any{}
	}
	return out.Peers, err
}

func joinHashes(hashes []string) string { return strings.Join(hashes, "|") }

// Stop pauses torrents.
func (c *Client) Stop(ctx context.Context, hashes []string) error {
	stop, _ := c.stopStartNames(ctx)
	_, err := c.post(ctx, "torrents/"+stop, url.Values{"hashes": {joinHashes(hashes)}})
	return err
}

// Start resumes torrents.
func (c *Client) Start(ctx context.Context, hashes []string) error {
	_, start := c.stopStartNames(ctx)
	_, err := c.post(ctx, "torrents/"+start, url.Values{"hashes": {joinHashes(hashes)}})
	return err
}

// Delete removes torrents, and their downloaded data when deleteFiles.
func (c *Client) Delete(ctx context.Context, hashes []string, deleteFiles bool) error {
	_, err := c.post(ctx, "torrents/delete", url.Values{"hashes": {joinHashes(hashes)}, "deleteFiles": {strconv.FormatBool(deleteFiles)}})
	return err
}

// Action runs a hashes-only torrent action: recheck, reannounce, topPrio,
// bottomPrio, increasePrio, decreasePrio, toggleSequentialDownload,
// toggleFirstLastPiecePrio.
func (c *Client) Action(ctx context.Context, action string, hashes []string) error {
	_, err := c.post(ctx, "torrents/"+action, url.Values{"hashes": {joinHashes(hashes)}})
	return err
}

// SetForceStart turns force start on or off.
func (c *Client) SetForceStart(ctx context.Context, hashes []string, on bool) error {
	_, err := c.post(ctx, "torrents/setForceStart", url.Values{"hashes": {joinHashes(hashes)}, "value": {strconv.FormatBool(on)}})
	return err
}

// SetLocation moves torrents' data to a folder. qBittorrent creates it and
// keeps seeding from the new place.
func (c *Client) SetLocation(ctx context.Context, hashes []string, location string) error {
	_, err := c.post(ctx, "torrents/setLocation", url.Values{"hashes": {joinHashes(hashes)}, "location": {location}})
	switch {
	case IsStatus(err, http.StatusForbidden):
		return fmt.Errorf("qBittorrent can't write to %s (check folder permissions for its user)", location)
	case IsStatus(err, http.StatusConflict):
		return fmt.Errorf("qBittorrent could not create %s (check folder permissions for its user)", location)
	}
	return err
}

// SetCategory puts torrents in a category ("" = none).
func (c *Client) SetCategory(ctx context.Context, hashes []string, category string) error {
	_, err := c.post(ctx, "torrents/setCategory", url.Values{"hashes": {joinHashes(hashes)}, "category": {category}})
	return err
}

// AddTags tags torrents (tags are comma-separated).
func (c *Client) AddTags(ctx context.Context, hashes []string, tags string) error {
	_, err := c.post(ctx, "torrents/addTags", url.Values{"hashes": {joinHashes(hashes)}, "tags": {tags}})
	return err
}

// RemoveTags untags torrents.
func (c *Client) RemoveTags(ctx context.Context, hashes []string, tags string) error {
	_, err := c.post(ctx, "torrents/removeTags", url.Values{"hashes": {joinHashes(hashes)}, "tags": {tags}})
	return err
}

// Rename changes a torrent's display name.
func (c *Client) Rename(ctx context.Context, hash, name string) error {
	_, err := c.post(ctx, "torrents/rename", url.Values{"hash": {hash}, "name": {name}})
	return err
}

// FilePriority sets the priority (0 skip, 1 normal, 6 high, 7 maximum) of
// files by index.
func (c *Client) FilePriority(ctx context.Context, hash string, ids []int, priority int) error {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	_, err := c.post(ctx, "torrents/filePrio", url.Values{"hash": {hash}, "id": {strings.Join(parts, "|")}, "priority": {strconv.Itoa(priority)}})
	return err
}

// SetLimits sets per-torrent speed limits in bytes/s (0 = none, -1 = leave).
func (c *Client) SetLimits(ctx context.Context, hashes []string, download, upload int64) error {
	if download >= 0 {
		if _, err := c.post(ctx, "torrents/setDownloadLimit", url.Values{"hashes": {joinHashes(hashes)}, "limit": {strconv.FormatInt(download, 10)}}); err != nil {
			return err
		}
	}
	if upload >= 0 {
		if _, err := c.post(ctx, "torrents/setUploadLimit", url.Values{"hashes": {joinHashes(hashes)}, "limit": {strconv.FormatInt(upload, 10)}}); err != nil {
			return err
		}
	}
	return nil
}

// AddOptions are the choices offered when adding torrents.
type AddOptions struct {
	SavePath           string
	Category           string
	Tags               string
	Stopped            bool
	SkipChecking       bool
	Sequential         bool
	FirstLastPiecePrio bool
	Rename             string
}

// TorrentFile is an uploaded .torrent.
type TorrentFile struct {
	Name string
	Data []byte
}

// Add adds magnet links / URLs and .torrent files.
func (c *Client) Add(ctx context.Context, urls []string, files []TorrentFile, opts AddOptions) error {
	stopField := "stopped"
	if v, err := c.APIVersion(ctx); err == nil && !VersionAtLeast(v, "2.11.0") {
		stopField = "paused"
	}
	body, err := c.do(ctx, func() (*http.Request, error) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		field := func(k, v string) {
			if v != "" {
				_ = mw.WriteField(k, v)
			}
		}
		field("urls", strings.Join(urls, "\n"))
		field("savepath", opts.SavePath)
		field("category", opts.Category)
		field("tags", opts.Tags)
		field("rename", opts.Rename)
		if opts.SavePath != "" {
			field("autoTMM", "false")
		}
		if opts.Stopped {
			field(stopField, "true")
		}
		if opts.SkipChecking {
			field("skip_checking", "true")
		}
		if opts.Sequential {
			field("sequentialDownload", "true")
		}
		if opts.FirstLastPiecePrio {
			field("firstLastPiecePrio", "true")
		}
		for _, f := range files {
			part, err := mw.CreateFormFile("torrents", f.Name)
			if err != nil {
				return nil, err
			}
			if _, err := part.Write(f.Data); err != nil {
				return nil, err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/torrents/add", &buf)
		if err == nil {
			req.Header.Set("Content-Type", mw.FormDataContentType())
		}
		return req, err
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(body)) == "Fails." {
		return errors.New("qBittorrent could not add the torrent (an invalid link or file, or it is already added)")
	}
	return nil
}

// CreateCategory adds a category with an optional save folder.
func (c *Client) CreateCategory(ctx context.Context, name, savePath string) error {
	_, err := c.post(ctx, "torrents/createCategory", url.Values{"category": {name}, "savePath": {savePath}})
	return err
}

// EditCategory changes a category's save folder.
func (c *Client) EditCategory(ctx context.Context, name, savePath string) error {
	_, err := c.post(ctx, "torrents/editCategory", url.Values{"category": {name}, "savePath": {savePath}})
	return err
}

// RemoveCategories deletes categories.
func (c *Client) RemoveCategories(ctx context.Context, names []string) error {
	_, err := c.post(ctx, "torrents/removeCategories", url.Values{"categories": {strings.Join(names, "\n")}})
	return err
}

// CreateTags adds tags (comma-separated).
func (c *Client) CreateTags(ctx context.Context, tags string) error {
	_, err := c.post(ctx, "torrents/createTags", url.Values{"tags": {tags}})
	return err
}

// DeleteTags removes tags (comma-separated) everywhere.
func (c *Client) DeleteTags(ctx context.Context, tags string) error {
	_, err := c.post(ctx, "torrents/deleteTags", url.Values{"tags": {tags}})
	return err
}

// ToggleAltSpeed switches the alternative speed limits on or off.
func (c *Client) ToggleAltSpeed(ctx context.Context) error {
	_, err := c.post(ctx, "transfer/toggleSpeedLimitsMode", nil)
	return err
}

// Preferences returns qBittorrent's settings.
func (c *Client) Preferences(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := c.getJSON(ctx, "app/preferences", nil, &out)
	return out, err
}

// SetPreferences changes some of qBittorrent's settings.
func (c *Client) SetPreferences(ctx context.Context, prefs map[string]any) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	_, err = c.post(ctx, "app/setPreferences", url.Values{"json": {string(raw)}})
	return err
}

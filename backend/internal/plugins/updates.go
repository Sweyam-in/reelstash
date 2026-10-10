package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const downloadTimeout = 60 * time.Second

// Release is one published version of a plugin, as listed in an update index.
type Release struct {
	Version string `json:"version"`
	File    string `json:"file"` // relative to the index, or a full address
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// IndexEntry is one plugin in an update index.
type IndexEntry struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     Kind      `json:"kind"`
	Latest   string    `json:"latest"`
	Versions []Release `json:"versions"`
}

type index struct {
	Plugins []IndexEntry `json:"plugins"`
}

// ErrBadAddress and ErrTooLarge are mistakes in what was asked for, as opposed
// to a download that failed.
var (
	ErrBadAddress = errors.New("the address must be a full http:// or https:// URL")
	ErrTooLarge   = fmt.Errorf("the download is larger than %d MB", MaxArchive>>20)
)

// Download fetches a bundle from an http(s) address, refusing anything larger
// than MaxArchive.
func Download(ctx context.Context, address string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ErrBadAddress
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "reelstash-plugin-manager/1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxArchive+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxArchive {
		return nil, ErrTooLarge
	}
	return data, nil
}

// semver parses "1.2.3".
func semver(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer reports whether version a is later than b. Versions that are not
// major.minor.patch count as newer when they merely differ.
func newer(a, b string) bool {
	x, okA := semver(a)
	y, okB := semver(b)
	if !okA || !okB {
		return a != b
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// UpdateInfo is what a check for updates found.
type UpdateInfo struct {
	Source    string `json:"source"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
}

func fetchIndex(ctx context.Context, address string) (index, *url.URL, error) {
	raw, err := Download(ctx, address)
	if err != nil {
		return index{}, nil, err
	}
	var idx index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return index{}, nil, fmt.Errorf("the update source is not a plugin index: %w", err)
	}
	base, _ := url.Parse(address)
	return idx, base, nil
}

func (idx index) entry(id string) (IndexEntry, bool) {
	for _, e := range idx.Plugins {
		if e.ID == id {
			return e, true
		}
	}
	return IndexEntry{}, false
}

// UpdateSource is where a bundle looks for newer versions: the address saved
// for it, else the one its plugin.json names, else "".
func (m *Manager) UpdateSource(ctx context.Context, id string) string {
	if st, ok := m.loadStates(ctx)[id]; ok && st.UpdateURL != "" {
		return st.UpdateURL
	}
	if b, err := m.get(id); err == nil {
		return b.man.UpdateURL
	}
	return ""
}

// SetUpdateSource saves where to look for newer versions ("" goes back to the
// address in plugin.json).
func (m *Manager) SetUpdateSource(ctx context.Context, id, address string) error {
	if _, err := m.get(id); err != nil {
		return err
	}
	address = strings.TrimSpace(address)
	if address != "" {
		if u, err := url.Parse(address); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("the update source must be a full http:// or https:// address of an index.json")
		}
	}
	states := m.loadStates(ctx)
	st := states[id]
	st.UpdateURL = address
	states[id] = st
	return m.saveStates(ctx, states)
}

// CheckUpdate asks the update source whether a newer version is published.
func (m *Manager) CheckUpdate(ctx context.Context, id string) (UpdateInfo, error) {
	b, err := m.get(id)
	if err != nil {
		return UpdateInfo{}, err
	}
	source := m.UpdateSource(ctx, id)
	if source == "" {
		return UpdateInfo{}, errors.New("this plugin has no update source; add the address of its index.json")
	}
	idx, _, err := fetchIndex(ctx, source)
	if err != nil {
		return UpdateInfo{}, err
	}
	entry, ok := idx.entry(id)
	if !ok || len(entry.Versions) == 0 {
		return UpdateInfo{}, fmt.Errorf("the update source does not list %q", id)
	}
	latest := entry.Latest
	if latest == "" {
		latest = entry.Versions[len(entry.Versions)-1].Version
	}
	b.mu.Lock()
	current := b.man.Version
	b.mu.Unlock()
	return UpdateInfo{Source: source, Current: current, Latest: latest, Available: newer(latest, current)}, nil
}

// Update installs the latest published version over the installed one. The
// download must match the sha256 the index lists.
func (m *Manager) Update(ctx context.Context, id string) (Info, error) {
	info, err := m.CheckUpdate(ctx, id)
	if err != nil {
		return Info{}, err
	}
	if !info.Available {
		return Info{}, fmt.Errorf("already on the latest version (%s)", info.Current)
	}
	idx, base, err := fetchIndex(ctx, info.Source)
	if err != nil {
		return Info{}, err
	}
	entry, _ := idx.entry(id)
	var rel *Release
	for i := range entry.Versions {
		if entry.Versions[i].Version == info.Latest {
			rel = &entry.Versions[i]
		}
	}
	if rel == nil || rel.File == "" {
		return Info{}, fmt.Errorf("the update source lists %s %s without a file", id, info.Latest)
	}
	ref, err := url.Parse(rel.File)
	if err != nil {
		return Info{}, errors.New("the update source lists an invalid file address")
	}
	data, err := Download(ctx, base.ResolveReference(ref).String())
	if err != nil {
		return Info{}, err
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(rel.SHA256)) {
		return Info{}, errors.New("the download does not match the checksum in the update source; nothing was installed")
	}
	got, err := m.InstallZip(ctx, data)
	if err != nil {
		return Info{}, err
	}
	if got.ID != id {
		return Info{}, fmt.Errorf("the download is plugin %q, not %q", got.ID, id)
	}
	return got, nil
}

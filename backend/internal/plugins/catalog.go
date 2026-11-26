package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// A catalog is a plugin index (the index.json the reelstash_plugins build
// writes) that admins browse to install plugins from, instead of pasting a
// .zip address. The addresses are kept in the settings store.
const catalogKey = "plugins.catalogs"

// MaxCatalogs bounds how many catalog addresses an admin can save.
const MaxCatalogs = 10

// CatalogItem is one plugin a catalog offers, with what is installed here.
type CatalogItem struct {
	Source      string `json:"source"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	Description string `json:"description,omitempty"`
	Latest      string `json:"latest"`
	Installed   string `json:"installed,omitempty"` // installed version, if any
	Update      bool   `json:"update,omitempty"`    // a newer version is listed
}

// CatalogSource is one catalog and what reading it found.
type CatalogSource struct {
	Address string        `json:"address"`
	Error   string        `json:"error,omitempty"`
	Items   []CatalogItem `json:"items"`
}

// Catalogs returns the saved catalog addresses.
func (m *Manager) Catalogs(ctx context.Context) []string {
	var out []string
	if raw, ok := m.opts.Store.Get(ctx, catalogKey); ok {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// SetCatalogs saves the catalog addresses, which must be http(s) URLs.
func (m *Manager) SetCatalogs(ctx context.Context, addresses []string) ([]string, error) {
	seen := map[string]bool{}
	clean := []string{}
	for _, a := range addresses {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		u, err := url.Parse(a)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("%q: %w", a, ErrBadAddress)
		}
		seen[a] = true
		clean = append(clean, a)
	}
	if len(clean) > MaxCatalogs {
		return nil, fmt.Errorf("at most %d catalogs", MaxCatalogs)
	}
	raw, _ := json.Marshal(clean)
	if err := m.opts.Store.Put(ctx, catalogKey, string(raw)); err != nil {
		return nil, err
	}
	return clean, nil
}

// BrowseCatalogs reads every saved catalog. A catalog that cannot be read is
// reported with its error; the others still list.
func (m *Manager) BrowseCatalogs(ctx context.Context) []CatalogSource {
	installed := map[string]string{}
	for _, info := range m.List(ctx) {
		installed[info.ID] = info.Version
	}
	out := []CatalogSource{}
	for _, address := range m.Catalogs(ctx) {
		src := CatalogSource{Address: address, Items: []CatalogItem{}}
		idx, _, err := fetchIndex(ctx, address)
		if err != nil {
			src.Error = err.Error()
			out = append(out, src)
			continue
		}
		for _, e := range idx.Plugins {
			if e.ID == "" || e.Latest == "" {
				continue
			}
			item := CatalogItem{Source: address, ID: e.ID, Name: e.Name, Kind: e.Kind, Description: e.Description, Latest: e.Latest}
			if v, ok := installed[e.ID]; ok {
				item.Installed = v
				item.Update = newer(e.Latest, v)
			}
			if item.Name == "" {
				item.Name = e.ID
			}
			src.Items = append(src.Items, item)
		}
		out = append(out, src)
	}
	return out
}

// InstallFromCatalog installs the latest version of plugin id from a saved
// catalog, after checking its sha256, and keeps the catalog as the plugin's
// update source so later versions show up under Updates.
func (m *Manager) InstallFromCatalog(ctx context.Context, address, id string) (Info, error) {
	if !m.opts.Enabled {
		return Info{}, ErrDisabled
	}
	known := false
	for _, a := range m.Catalogs(ctx) {
		known = known || a == address
	}
	if !known {
		return Info{}, errors.New("that catalog is not in the saved list")
	}
	idx, base, err := fetchIndex(ctx, address)
	if err != nil {
		return Info{}, err
	}
	entry, ok := idx.entry(id)
	if !ok {
		return Info{}, fmt.Errorf("the catalog does not list %q", id)
	}
	data, err := downloadRelease(ctx, base, entry, entry.Latest)
	if err != nil {
		return Info{}, err
	}
	info, err := m.InstallZip(ctx, data)
	if err != nil {
		return Info{}, err
	}
	if info.ID != id {
		return info, fmt.Errorf("the download is plugin %q, not %q", info.ID, id)
	}
	if err := m.SetUpdateSource(ctx, id, address); err != nil {
		return info, err
	}
	return info, nil
}

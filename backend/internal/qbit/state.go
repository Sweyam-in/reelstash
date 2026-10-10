package qbit

import (
	"sort"
	"strings"
)

// State is qBittorrent's torrents, categories, tags and transfer totals, kept
// current by applying sync/maindata answers in order.
type State struct {
	RID        int64
	Torrents   map[string]map[string]any
	Categories map[string]map[string]any
	Tags       map[string]bool
	Server     map[string]any
}

// NewState is an empty state that asks for a full update first.
func NewState() *State {
	return &State{Torrents: map[string]map[string]any{}, Categories: map[string]map[string]any{}, Tags: map[string]bool{}, Server: map[string]any{}}
}

// Apply merges one maindata answer: a full update replaces everything, a
// partial one changes only the fields it carries.
func (s *State) Apply(md MainData) {
	if md.FullUpdate {
		s.Torrents = map[string]map[string]any{}
		s.Categories = map[string]map[string]any{}
		s.Tags = map[string]bool{}
		s.Server = map[string]any{}
	}
	for hash, fields := range md.Torrents {
		t := s.Torrents[hash]
		if t == nil {
			t = map[string]any{}
			s.Torrents[hash] = t
		}
		for k, v := range fields {
			t[k] = v
		}
		t["hash"] = hash
	}
	for _, hash := range md.TorrentsRemoved {
		delete(s.Torrents, hash)
	}
	for name, fields := range md.Categories {
		c := s.Categories[name]
		if c == nil {
			c = map[string]any{}
			s.Categories[name] = c
		}
		for k, v := range fields {
			c[k] = v
		}
	}
	for _, name := range md.CategoriesRemoved {
		delete(s.Categories, name)
	}
	for _, tag := range md.Tags {
		s.Tags[tag] = true
	}
	for _, tag := range md.TagsRemoved {
		delete(s.Tags, tag)
	}
	for k, v := range md.ServerState {
		s.Server[k] = v
	}
	s.RID = md.RID
}

// Torrent is a typed view of the torrent fields ReelStash acts on.
type Torrent struct {
	Hash        string
	Name        string
	State       string
	Progress    float64
	Size        int64
	Category    string
	Tags        []string
	SavePath    string
	ContentPath string
	Tracker     string
	CompletedOn int64
	Ratio       float64
	SeedingTime int64
}

// View reads a torrent's fields.
func View(t map[string]any) Torrent {
	return Torrent{
		Hash:        Str(t, "hash"),
		Name:        Str(t, "name"),
		State:       Str(t, "state"),
		Progress:    Num(t, "progress"),
		Size:        int64(Num(t, "size")),
		Category:    Str(t, "category"),
		Tags:        SplitTags(Str(t, "tags")),
		SavePath:    Str(t, "save_path"),
		ContentPath: Str(t, "content_path"),
		Tracker:     Str(t, "tracker"),
		CompletedOn: int64(Num(t, "completion_on")),
		Ratio:       Num(t, "ratio"),
		SeedingTime: int64(Num(t, "seeding_time")),
	}
}

// Complete reports whether every wanted piece is downloaded.
func (t Torrent) Complete() bool {
	return t.Progress >= 1 && t.CompletedOn > 0 && !strings.HasPrefix(t.State, "checking") && t.State != "moving"
}

// Moving reports whether qBittorrent is moving the torrent's data.
func (t Torrent) Moving() bool { return t.State == "moving" }

// Errored reports whether the torrent is in an error state.
func (t Torrent) Errored() bool { return t.State == "error" || t.State == "missingFiles" }

// Str reads a string field.
func Str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// Num reads a numeric field.
func Num(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

// SplitTags splits qBittorrent's comma-separated tag list.
func SplitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// SortedKeys returns a map's keys in order.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

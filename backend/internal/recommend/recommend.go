// Package recommend scores library items against a user's taste profile.
//
// A profile is a weighted bag of features (performers, studios, series,
// categories, tags) built from three signals: watch history (completion,
// rewatches, recency, abandonment), favorites and playlist membership.
// Candidates are scored by the IDF-weighted overlap with the profile.
package recommend

import (
	"hash/fnv"
	"math"
	"sort"
	"time"
)

type Kind byte

const (
	Performer Kind = 'p'
	Studio    Kind = 's'
	Series    Kind = 'e'
	Category  Kind = 'c'
	Tag       Kind = 't'
)

type Feature struct {
	Kind Kind
	ID   int64
}

var kindWeight = map[Kind]float64{
	Performer: 3, Series: 2, Studio: 1.5, Category: 1, Tag: 0.7,
}

type Item struct {
	ID        int64
	Features  []Feature
	Favorite  bool
	Rating    *int16 // 0-10
	AddedAt   time.Time
	Available bool
}

// Engagement is one user's aggregated viewing of one item.
type Engagement struct {
	Plays          int
	WatchedSeconds float64
	Completion     float64 // best fraction watched, 0..1
	Completed      bool
	LastPlayed     time.Time
}

type Profile map[Feature]float64

type Profiles struct {
	History, Favorites, Playlist Profile
}

type Model struct {
	Items map[int64]*Item
	Eng   map[int64]Engagement
	idf   map[Feature]float64
	Prof  Profiles
	Now   time.Time
}

// Seen reports whether the user has meaningfully watched the item.
func (m *Model) Seen(id int64) bool {
	e, ok := m.Eng[id]
	return ok && (e.Completed || e.Plays > 0)
}

func Build(items []*Item, eng map[int64]Engagement, playlistCount map[int64]int, now time.Time) *Model {
	m := &Model{Items: make(map[int64]*Item, len(items)), Eng: eng, idf: map[Feature]float64{}, Now: now}
	df := map[Feature]int{}
	for _, it := range items {
		m.Items[it.ID] = it
		for _, f := range it.Features {
			df[f]++
		}
	}
	n := float64(len(items))
	for f, c := range df {
		m.idf[f] = math.Log(1 + n/float64(1+c))
	}

	m.Prof = Profiles{History: Profile{}, Favorites: Profile{}, Playlist: Profile{}}
	for _, it := range items {
		if w := historyWeight(it, eng[it.ID], now); w != 0 {
			m.Prof.History.add(it, w)
		}
		if it.Favorite {
			m.Prof.Favorites.add(it, 3)
		}
		if c := playlistCount[it.ID]; c > 0 {
			m.Prof.Playlist.add(it, 2+math.Min(float64(c-1)*0.5, 1))
		}
	}
	return m
}

// historyWeight: completion and rewatches raise it, quick abandonment
// lowers it, and old history decays (floored so it never vanishes).
func historyWeight(it *Item, e Engagement, now time.Time) float64 {
	var w float64
	switch {
	case e.Plays > 0 && e.Completion < 0.1 && e.WatchedSeconds < 45 && !e.Completed:
		w = -0.4
	case e.Plays > 0 || e.Completed:
		plays := math.Max(float64(e.Plays), 1)
		w = e.Completion*1.5 + 0.7*math.Log(plays)
		if e.Completed {
			w += 0.5
		}
		days := now.Sub(e.LastPlayed).Hours() / 24
		w *= math.Max(math.Pow(0.5, days/45), 0.25)
	}
	if it.Rating != nil && (e.Plays > 0 || e.Completed) {
		w += (float64(*it.Rating) - 5) / 5
	}
	return w
}

func (p Profile) add(it *Item, w float64) {
	perKind := map[Kind]int{}
	for _, f := range it.Features {
		perKind[f.Kind]++
	}
	for _, f := range it.Features {
		p[f] += w * kindWeight[f.Kind] / math.Sqrt(float64(perKind[f.Kind]))
	}
}

// Match is the raw affinity of an item for a profile plus its single
// strongest contributing feature (used for "because you like…" reasons).
type Match struct {
	Score  float64
	Reason Feature
	Has    bool
}

func (m *Model) Match(it *Item, p Profile) Match {
	perKind := map[Kind]int{}
	for _, f := range it.Features {
		perKind[f.Kind]++
	}
	var out Match
	best := 0.0
	for _, f := range it.Features {
		a, ok := p[f]
		if !ok {
			continue
		}
		c := a * m.idf[f] / math.Sqrt(float64(perKind[f.Kind]))
		out.Score += c
		if c > best {
			best, out.Reason, out.Has = c, f, true
		}
	}
	return out
}

// Combined profile: history-led, favorites and playlists as boosts.
func (m *Model) Combined(it *Item) Match {
	h := m.Match(it, m.Prof.History)
	f := m.Match(it, m.Prof.Favorites)
	pl := m.Match(it, m.Prof.Playlist)
	out := Match{Score: h.Score + 0.8*f.Score + 0.8*pl.Score}
	out.Reason, out.Has = h.Reason, h.Has
	top := h.Score
	if 0.8*f.Score > top {
		out.Reason, out.Has, top = f.Reason, f.Has, 0.8*f.Score
	}
	if 0.8*pl.Score > top {
		out.Reason, out.Has = pl.Reason, pl.Has
	}
	return out
}

type Scored struct {
	ID     int64
	Score  float64
	Reason Feature
	Has    bool
}

// Rank orders candidates by normalized affinity blended with freshness,
// rating and a small per-day deterministic jitter so the shelf rotates.
func (m *Model) Rank(cands []*Item, match func(*Item) Match, seed string, jitter float64) []Scored {
	ms := make([]Match, len(cands))
	maxS := 0.0
	for i, it := range cands {
		ms[i] = match(it)
		if ms[i].Score > maxS {
			maxS = ms[i].Score
		}
	}
	out := make([]Scored, len(cands))
	for i, it := range cands {
		p := 0.0
		if maxS > 0 {
			p = math.Max(ms[i].Score, 0) / maxS
		}
		age := m.Now.Sub(it.AddedAt).Hours() / 24
		fresh := math.Exp(-math.Max(age, 0) / 90)
		rate := 0.0
		if it.Rating != nil {
			rate = float64(*it.Rating) / 10
		}
		s := 0.78*p + 0.12*fresh + 0.06*rate + jitter*hash01(seed, it.ID)
		out[i] = Scored{ID: it.ID, Score: s, Reason: ms[i].Reason, Has: ms[i].Has && ms[i].Score > 0}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Diversify keeps at most maxPer items per group (e.g. studio) while
// filling n slots; leftover slots are back-filled in rank order.
func Diversify(ranked []Scored, n, maxPer int, group func(id int64) int64) []Scored {
	var out, spill []Scored
	counts := map[int64]int{}
	for _, s := range ranked {
		if len(out) >= n {
			break
		}
		g := group(s.ID)
		if g != 0 && counts[g] >= maxPer {
			spill = append(spill, s)
			continue
		}
		counts[g]++
		out = append(out, s)
	}
	for _, s := range spill {
		if len(out) >= n {
			break
		}
		out = append(out, s)
	}
	return out
}

func hash01(seed string, id int64) float64 {
	h := fnv.New32a()
	h.Write([]byte(seed))
	var b [8]byte
	for i := range b {
		b[i] = byte(id >> (8 * i))
	}
	h.Write(b[:])
	return float64(h.Sum32()%10000) / 10000
}

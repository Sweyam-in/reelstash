package recommend

import (
	"testing"
	"time"
)

func TestRankPrefersLikedPerformerAndSkipsAbandoned(t *testing.T) {
	now := time.Now()
	liked := Feature{Performer, 1}
	other := Feature{Performer, 2}
	items := []*Item{
		{ID: 1, Features: []Feature{liked}, AddedAt: now.AddDate(0, -6, 0), Available: true},
		{ID: 2, Features: []Feature{liked}, AddedAt: now.AddDate(0, -6, 0), Available: true},
		{ID: 3, Features: []Feature{other}, AddedAt: now.AddDate(0, -6, 0), Available: true},
		{ID: 4, Features: []Feature{other}, AddedAt: now.AddDate(0, -6, 0), Available: true},
	}
	eng := map[int64]Engagement{
		1: {Plays: 3, WatchedSeconds: 900, Completion: 1, Completed: true, LastPlayed: now},
		3: {Plays: 1, WatchedSeconds: 5, Completion: 0.01, LastPlayed: now},
	}
	m := Build(items, eng, nil, now)
	ranked := m.Rank([]*Item{items[1], items[3]}, m.Combined, "s", 0)
	if ranked[0].ID != 2 {
		t.Fatalf("expected item 2 first, got %d", ranked[0].ID)
	}
	if !ranked[0].Has || ranked[0].Reason != liked {
		t.Fatalf("expected reason %v, got %v", liked, ranked[0].Reason)
	}
}

func TestDiversifyCapsGroup(t *testing.T) {
	r := []Scored{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	g := func(id int64) int64 {
		if id <= 3 {
			return 7
		}
		return 8
	}
	out := Diversify(r, 3, 2, g)
	if out[0].ID != 1 || out[1].ID != 2 || out[2].ID != 4 {
		t.Fatalf("unexpected order %+v", out)
	}
}

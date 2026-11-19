package httpapi

import (
	"testing"
	"time"
)

func TestStreakFromDays(t *testing.T) {
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	days := func(list ...string) map[string]bool {
		out := map[string]bool{}
		for _, d := range list {
			out[d] = true
		}
		return out
	}
	cases := []struct {
		name             string
		played           map[string]bool
		current, longest int
	}{
		{"nothing", days(), 0, 0},
		{"today only", days("2026-11-19"), 1, 1},
		{"through yesterday still counts", days("2026-11-18", "2026-11-17"), 2, 2},
		{"broken two days ago", days("2026-11-16", "2026-11-15"), 0, 2},
		{"longest earlier", days("2026-11-19", "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"), 1, 4},
	}
	for _, c := range cases {
		got := streakFromDays(c.played, now)
		if got.Current != c.current || got.Longest != c.longest {
			t.Fatalf("%s: got %+v, want current %d longest %d", c.name, got, c.current, c.longest)
		}
	}
}

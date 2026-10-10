package schedule

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCronMatches(t *testing.T) {
	cases := []struct {
		expr string
		when string
		want bool
	}{
		{"* * * * *", "2026-03-04 05:06", true},
		{"*/15 * * * *", "2026-03-04 05:30", true},
		{"*/15 * * * *", "2026-03-04 05:31", false},
		{"0 3 * * *", "2026-03-04 03:00", true},
		{"0 3 * * *", "2026-03-04 04:00", false},
		{"30 2 * * 1-5", "2026-03-04 02:30", true},  // Wednesday
		{"30 2 * * 1-5", "2026-03-08 02:30", false}, // Sunday
		{"0 0 * * 7", "2026-03-08 00:00", true},     // 7 is Sunday
		{"0 12 1,15 * *", "2026-03-15 12:00", true},
		{"0 12 1,15 * *", "2026-03-16 12:00", false},
		{"10-50/20 * * * *", "2026-03-04 05:30", true},
		{"10-50/20 * * * *", "2026-03-04 05:20", false},
		{"@hourly", "2026-03-04 09:00", true},
		{"@daily", "2026-03-04 00:00", true},
		{"@daily", "2026-03-04 00:01", false},
		// Both day fields restricted: either may match.
		{"0 0 13 * 5", "2026-03-13 00:00", true}, // Friday the 13th
		{"0 0 13 * 5", "2026-03-06 00:00", true}, // a Friday, not the 13th
		{"0 0 13 * 5", "2026-03-07 00:00", false},
	}
	for _, tc := range cases {
		c, err := ParseCron(tc.expr)
		if err != nil {
			t.Fatalf("%q: %v", tc.expr, err)
		}
		if got := c.Matches(at(tc.when)); got != tc.want {
			t.Errorf("%q at %s = %v, want %v", tc.expr, tc.when, got, tc.want)
		}
	}
}

func TestCronRejectsBadExpressions(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "61 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "a * * * *", "*/0 * * * *", "5-1 * * * *", "* * * * * *"} {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("%q should not parse", expr)
		}
	}
}

func TestCronNext(t *testing.T) {
	c, _ := ParseCron("0 3 * * *")
	if got, want := c.Next(at("2026-03-04 03:00")), at("2026-03-05 03:00"); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
	if got, want := c.Next(at("2026-03-04 01:10")), at("2026-03-04 03:00"); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
	never, _ := ParseCron("0 0 31 2 *")
	if !never.Next(at("2026-01-01 00:00")).IsZero() {
		t.Error("Feb 31 should never fire")
	}
}

package titlematch

import "testing"

func TestSimilarity(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want float64
	}{
		{"Evil Angel", "evil-angel", 100},
		{"Angel Evil", "Evil Angel", 100}, // word order does not matter
		{"", "x", 0},
		{"abc", "abd", 100 * (1 - 1.0/3)},
	} {
		if got := Similarity(tc.a, tc.b); got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("Similarity(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

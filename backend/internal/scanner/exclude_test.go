package scanner

import "testing"

func TestExcluder(t *testing.T) {
	e, err := NewExcluder([]string{"*.part", "Samples", "/Movies/Old", "Clips/**/tmp", "  ", "trailer?.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"a/b/video.part", true},
		{"video.mp4", false},
		{"Show/Samples/clip.mp4", true},
		{"Show/SamplesX/clip.mp4", false},
		{"Movies/Old/a.mp4", true},
		{"Movies/Old", true},
		{"Other/Movies/Old/a.mp4", false},
		{"Movies/Older/a.mp4", false},
		{"Clips/tmp/a.mp4", true},
		{"Clips/x/y/tmp/a.mp4", true},
		{"Clips/x/tmpx/a.mp4", false},
		{"x/trailer1.mp4", true},
		{"x/trailer12.mp4", false},
	}
	for _, c := range cases {
		if got := e.Excluded(c.path); got != c.want {
			t.Errorf("Excluded(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestExcluderEmpty(t *testing.T) {
	var e *Excluder
	if e.Excluded("anything") {
		t.Fatal("nil excluder excluded a path")
	}
	if err := ValidatePattern(" / "); err == nil {
		t.Fatal("blank pattern validated")
	}
}

func TestRuleSetResolvesRulesPerLibraryFolder(t *testing.T) {
	rs, err := NewRuleSet(
		[]string{"Samples"},
		map[int64][]string{1: {"Old/Stuff"}, 2: {"*.part"}},
		map[string]int64{"Movies": 1, "Movies/Kids": 2, "": 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"Movies":                  false, // a library folder itself
		"Movies/Old/Stuff/a.mp4":  true,  // library 1 rule, relative to its folder
		"Old/Stuff/a.mp4":         false, // library 3 (whole disk) has no such rule
		"Movies/Kids/Old/Stuff/x": false, // nested library 2 doesn't inherit library 1's rules
		"Movies/Kids/a.part":      true,
		"Movies/a.part":           false,
		"Movies/Samples/a.mp4":    true, // rules for every library apply everywhere
		"Other/Samples/a.mp4":     true,
	}
	for path, want := range cases {
		if got := rs.Excluded(path); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", path, got, want)
		}
	}
	var none *RuleSet
	if none.Excluded("a/b") {
		t.Error("nil rule set excluded a path")
	}
}

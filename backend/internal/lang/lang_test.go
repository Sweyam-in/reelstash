package lang

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"eng": "en", "EN": "en", "en-US": "en", "English": "en", "fre": "fr", "fra": "fr", "jpn": "ja",
		"": "", "und": "", "xyz": "xyz", "a-very-long-tag": "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLabel(t *testing.T) {
	if Label("ja") != "Japanese" || Label("xyz") != "XYZ" {
		t.Errorf("unexpected labels: %q %q", Label("ja"), Label("xyz"))
	}
}

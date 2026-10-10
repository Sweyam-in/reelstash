package names

import "testing"

func TestKey(t *testing.T) {
	same := [][2]string{
		{"Riley Mitchel", "riley mitchel"},
		{"Buck  Richards", "Richards, Buck"},
		{"José Ruiz", "Jose-Ruiz"},
	}
	for _, p := range same {
		if Key(p[0]) != Key(p[1]) {
			t.Errorf("Key(%q) != Key(%q)", p[0], p[1])
		}
	}
	if Key("Riley Mitchel") == Key("Riley Mitchell") {
		t.Error("different spellings must not collide")
	}
	if Key("!!") != "" {
		t.Error("a name with no letters has no key")
	}
}

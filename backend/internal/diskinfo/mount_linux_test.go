package diskinfo

import "testing"

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/mnt/My\040Disk`); got != "/mnt/My Disk" {
		t.Fatalf("got %q", got)
	}
}

func TestWithin(t *testing.T) {
	cases := []struct {
		path, point string
		want        bool
	}{
		{"/mnt/media/a", "/mnt/media", true},
		{"/mnt/media", "/mnt/media", true},
		{"/mnt/media2", "/mnt/media", false},
		{"/anything", "/", true},
	}
	for _, c := range cases {
		if got := within(c.path, c.point); got != c.want {
			t.Errorf("within(%q, %q) = %v", c.path, c.point, got)
		}
	}
}

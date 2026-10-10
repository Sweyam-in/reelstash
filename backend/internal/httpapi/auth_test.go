package httpapi

import "testing"

func TestAppURLsFor(t *testing.T) {
	media := []string{"https://reelstash.example", "http://media.local:8000/"}
	admin := []string{"https://admin.reelstash.example", "http://media.local:8001"}
	cases := []struct{ host, media, admin string }{
		{"reelstash.example", "https://reelstash.example", "https://admin.reelstash.example"},
		{"ADMIN.reelstash.example", "https://reelstash.example", "https://admin.reelstash.example"},
		{"media.local:8001", "http://media.local:8000", "http://media.local:8001"},
		{"media.local:8000", "http://media.local:8000", "http://media.local:8001"},
		{"localhost:3000", "", ""},
	}
	for _, c := range cases {
		m, a := appURLsFor(media, admin, c.host)
		if m != c.media || a != c.admin {
			t.Errorf("appURLsFor(%q) = %q, %q; want %q, %q", c.host, m, a, c.media, c.admin)
		}
	}
	if m, a := appURLsFor(media, nil, "reelstash.example"); m != "" || a != "" {
		t.Errorf("unpaired lists should yield no URLs, got %q, %q", m, a)
	}
}

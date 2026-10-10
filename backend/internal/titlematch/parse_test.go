package titlematch

import "testing"

func TestParseFileName(t *testing.T) {
	cases := []struct {
		filename string
		site     string
		date     string
		name     string
		ext      string
	}{
		{
			filename: "EvilAngel.22.01.03.Carmela.Clutch.Fabulous.Anal.3-Way.XXX.2160p.mp4",
			site:     "EvilAngel",
			date:     "2022-01-03",
			name:     "Carmela Clutch Fabulous Anal 3-Way",
			ext:      "mp4",
		},
		{
			filename: "DorcelClub.20.12.15.Aya.Benetti.Megane.Lopez.And.Bella.Tina.2160p.mp4",
			site:     "DorcelClub",
			date:     "2020-12-15",
			name:     "Aya Benetti Megane Lopez And Bella Tina",
			ext:      "mp4",
		},
		{
			filename: "random-file-name-2334.mp4",
			site:     "random-file-name-2334",
			date:     "",
			ext:      "mp4",
		},
	}

	for _, c := range cases {
		info := ParseFileName(c.filename)
		if info.Date != c.date {
			t.Errorf("%s: date = %q, want %q", c.filename, info.Date, c.date)
		}
		if c.name != "" && info.Name != c.name {
			t.Errorf("%s: name = %q, want %q", c.filename, info.Name, c.name)
		}
		if info.Extension != c.ext {
			t.Errorf("%s: extension = %q, want %q", c.filename, info.Extension, c.ext)
		}
		if c.site != "" && c.date != "" && info.Site != c.site {
			t.Errorf("%s: site = %q, want %q", c.filename, info.Site, c.site)
		}
	}
}

func TestSearchTextKeepsFirstWord(t *testing.T) {
	for in, want := range map[string]string{
		"Stud Loads - Breeding Wall Stud Loads Vol 4.mp4":           "Stud Loads Breeding Wall Stud Loads Vol 4",
		"Stud Loads - Breeding Wall: Stud Loads Vol. 4 [1080p].mp4": "Stud Loads Breeding Wall Stud Loads Vol 4",
		"Don't_Stop__Now_-_Vol.2.mkv":                               "Dont Stop Now Vol 2",
		"EvilAngel.22.01.03.Carmela.Clutch.2160p.mp4":               "EvilAngel 22 01 03 Carmela Clutch",
	} {
		if got := SearchText(in); got != want {
			t.Errorf("SearchText(%q) = %q, want %q", in, got, want)
		}
	}
}

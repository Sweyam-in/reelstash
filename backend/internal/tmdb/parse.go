package tmdb

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FileInfo is what a release-style file name tells us about a title.
type FileInfo struct {
	Title   string
	Year    int
	Season  int // 0 when the name has no season/episode marker
	Episode int
}

func (f FileInfo) IsEpisode() bool { return f.Season > 0 && f.Episode > 0 }

var (
	episodeRe = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:s(\d{1,2})[\s._\-]*e(\d{1,3})|(\d{1,2})x(\d{2,3}))(?:$|[\s._\-\])e])`)
	yearRe    = regexp.MustCompile(`(?:19|20)\d{2}`)
	junkRe    = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:\d{3,4}p|\d{3,4}x\d{3,4}|[248]k|uhd|hdr\d*|dv|web[\s._\-]?dl|web[\s._\-]?rip|webrip|blu[\s._\-]?ray|bdrip|brrip|dvdrip|hdtv|remux|x26[45]|h[\s._]?26[45]|hevc|avc|xvid|aac\d*|ac3|dts\S*|atmos|ddp?\d*|proper|repack|extended|unrated|imax|remastered|multi|internal)(?:$|[\s._\-\])])`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// ParseFileName extracts title, year and season/episode from a file name such
// as "The.Matrix.1999.1080p.BluRay.x264.mkv" or "Show.Name.S02E05.720p.mkv".
func ParseFileName(filename string) FileInfo {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return ParseName(base)
}

// ParseName is ParseFileName for text that has no extension (a typed search).
func ParseName(name string) FileInfo {
	var info FileInfo
	cut := len(name)
	mark := func(idx int) {
		if idx >= 0 && idx < cut {
			cut = idx
		}
	}

	if m := episodeRe.FindStringSubmatchIndex(name); m != nil {
		season, episode := group(name, m, 2), group(name, m, 4)
		if season == "" {
			season, episode = group(name, m, 6), group(name, m, 8)
		}
		info.Season, _ = strconv.Atoi(season)
		info.Episode, _ = strconv.Atoi(episode)
		mark(m[0])
	}
	if y, at := findYear(name); y != 0 {
		info.Year = y
		mark(at)
	}
	if m := junkRe.FindStringIndex(name); m != nil {
		mark(m[0])
	}

	info.Title = cleanTitle(name[:cut])
	if info.Title == "" { // the whole name was a year or junk token
		info.Title = cleanTitle(name)
		info.Year = 0
	}
	return info
}

func group(s string, m []int, i int) string {
	if i+1 >= len(m) || m[i] < 0 {
		return ""
	}
	return s[m[i]:m[i+1]]
}

func cleanTitle(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = strings.Trim(spaceRe.ReplaceAllString(s, " "), " -[]()")
	return s
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// findYear picks the release year in a name and returns where the title text
// ends. A year in brackets wins ("Blade Runner 2049 (2017)"); otherwise the
// last year that follows some title text does ("2012.2009.1080p", "2001 A
// Space Odyssey 1968"). A year at the very start is part of the title.
func findYear(name string) (year, cutAt int) {
	bestWrapped, bestPlain := -1, -1
	for _, m := range yearRe.FindAllStringIndex(name, -1) {
		start, end := m[0], m[1]
		if start == 0 || isAlnum(name[start-1]) || end < len(name) && isAlnum(name[end]) {
			continue
		}
		if name[start-1] == '(' || name[start-1] == '[' {
			bestWrapped = start
		} else {
			bestPlain = start
		}
	}
	at := bestWrapped
	if at < 0 {
		at = bestPlain
	}
	if at < 0 {
		return 0, 0
	}
	year, _ = strconv.Atoi(name[at : at+4])
	if name[at-1] == '(' || name[at-1] == '[' || strings.ContainsRune(" ._-", rune(name[at-1])) {
		at--
	}
	return year, at
}

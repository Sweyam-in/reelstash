// Package titlematch holds the file-name parsing and fuzzy title similarity
// ReelStash itself needs: the name shown for a file being matched, and how
// close a provider's title is to a video's. Provider-specific matching (what to
// search, how to score candidates) lives in each provider plugin.
package titlematch

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"
)

// FileInfo is the parsed form of a filename like
// "EvilAngel.22.01.03.Carmela.Clutch.Fabulous.Anal.3-Way.XXX.2160p.mp4".
type FileInfo struct {
	Site      string
	Date      string // YYYY-MM-DD
	Name      string
	Extension string
}

// nameParserRegex mirrors namer's original token order
// '{_site}{_sep}{_optional_date}{_ts}{_name}{_dot}{_ext}' (see
// backend/app/models/fileinfo.py's DEFAULT_REGEX_TOKENS and
// parser_config_to_regex — the token sub-patterns are ported verbatim; this
// package assembles them in the order that actually parses
// "Site.YY.MM.DD.Name.ext" without a double-separator gap). Go's standard
// regexp (RE2) can't express the negative lookahead the _name group needs,
// so this uses regexp2 (.NET-flavored regex engine) instead.
var nameParserRegex = regexp2.MustCompile(
	`(?<site>.*?)`+ // _site
		`[\.\- ]+`+ // _sep
		`(?:(?<year>[0-9]{2}(?:[0-9]{2})?)[\.\- ]+(?<month>[0-9]{2})[\.\- ]+(?<day>[0-9]{2})[\.\- ]+)?`+ // _optional_date
		`((?<trans>[Tt][Ss])[\.\- ]+){0,1}`+ // _ts
		`(?<name>(?:.(?![0-9]{2,4}[\.\- ][0-9]{2}[\.\- ][0-9]{2}))*)`+ // _name (negative lookahead)
		`\.`+ // _dot
		`(?<ext>[a-zA-Z0-9]{3,4})$`, // _ext
	regexp2.None,
)

// reCleanup mirrors backend/app/models/database.py's default re_cleanup
// list: resolution/codec/frame-rate/group-tag noise stripped out of the
// "name" segment before it's used for matching.
var reCleanup = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b[0-9]{3,4}x[0-9]{3,4}\b`),
	regexp.MustCompile(`(?i)\b[0-9]{2}fps\b`),
	regexp.MustCompile(`(?i)\b[0-9]{3,4}p\b`),
	regexp.MustCompile(`(?i)\b[0-9]k\b`),
	regexp.MustCompile(`(?i)\bXXX.*\b`),
	regexp.MustCompile(`(?i)\b\[WEBDL-.*\b`),
}

var reBrackets = regexp.MustCompile(`\[[^\]]*\]`)

// reSpecial matches everything that is not a letter, digit or space (dots,
// underscores, dashes, colons, commas...).
var reSpecial = regexp.MustCompile(`[^\p{L}\p{N}\s]+`)

var whitespaceRe = regexp.MustCompile(`\s+`)

func cleanName(name string) string {
	for _, re := range reCleanup {
		name = re.ReplaceAllString(name, "")
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = whitespaceRe.ReplaceAllString(name, " ")
	return strings.Trim(strings.TrimSpace(name), "-")
}

// SearchText is the text prefilled in the match search box for a file: the whole
// file name minus its extension, resolution/codec noise and punctuation. Unlike ParseFileName
// it never splits off a leading "site", because names are not always scene-style
// (ReelStash's own renames look like "{studio} - {title} [1080p]") and dropping
// the first word would silently change what gets searched.
func SearchText(filename string) string {
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	stem = reBrackets.ReplaceAllString(stem, " ")
	for _, re := range reCleanup {
		stem = re.ReplaceAllString(stem, "")
	}
	stem = strings.NewReplacer("'", "", "’", "").Replace(stem)
	stem = reSpecial.ReplaceAllString(stem, " ")
	return whitespaceRe.ReplaceAllString(strings.TrimSpace(stem), " ")
}

// ParseFileName extracts site/date/name/extension from a media filename,
// e.g. "DorcelClub.20.12.15.Aya.Benetti.2160p.mp4" ->
// {Site: "DorcelClub", Date: "2020-12-15", Name: "Aya Benetti", Extension: "mp4"}.
func ParseFileName(filename string) FileInfo {
	info := FileInfo{Extension: strings.TrimPrefix(filepath.Ext(filename), ".")}

	match, err := nameParserRegex.FindStringMatch(filename)
	if err != nil || match == nil {
		return info
	}

	if g := match.GroupByName("year"); g != nil && g.String() != "" {
		year := g.String()
		if len(year) == 2 {
			year = "20" + year
		}
		month := groupString(match, "month")
		day := groupString(match, "day")
		info.Date = year + "-" + month + "-" + day
	}
	if g := match.GroupByName("site"); g != nil {
		info.Site = g.String()
	}
	if g := match.GroupByName("name"); g != nil {
		info.Name = cleanName(g.String())
	}
	if g := match.GroupByName("ext"); g != nil && g.String() != "" {
		info.Extension = g.String()
	}

	return info
}

func groupString(match *regexp2.Match, name string) string {
	if g := match.GroupByName(name); g != nil {
		return g.String()
	}
	return ""
}

// Package lang normalises the language tags found in media files (ISO 639-2
// codes from ffprobe, 2-letter codes from sidecar file names) so audio and
// subtitle tracks can be matched against a user's language preference.
package lang

import "strings"

type entry struct {
	code    string
	label   string
	aliases []string
}

// Canonical codes are the 2-letter ISO 639-1 code where one exists.
var table = []entry{
	{"en", "English", []string{"eng", "english"}},
	{"ja", "Japanese", []string{"jpn", "japanese"}},
	{"es", "Spanish", []string{"spa", "spanish", "espanol", "español"}},
	{"fr", "French", []string{"fre", "fra", "french", "francais", "français"}},
	{"de", "German", []string{"ger", "deu", "german", "deutsch"}},
	{"it", "Italian", []string{"ita", "italian", "italiano"}},
	{"pt", "Portuguese", []string{"por", "portuguese", "portugues", "português"}},
	{"ru", "Russian", []string{"rus", "russian"}},
	{"zh", "Chinese", []string{"chi", "zho", "chinese", "chs", "cht", "cmn", "yue"}},
	{"ko", "Korean", []string{"kor", "korean"}},
	{"hi", "Hindi", []string{"hin", "hindi"}},
	{"ml", "Malayalam", []string{"mal", "malayalam"}},
	{"ta", "Tamil", []string{"tam", "tamil"}},
	{"te", "Telugu", []string{"tel", "telugu"}},
	{"kn", "Kannada", []string{"kan", "kannada"}},
	{"bn", "Bengali", []string{"ben", "bengali"}},
	{"mr", "Marathi", []string{"mar", "marathi"}},
	{"ar", "Arabic", []string{"ara", "arabic"}},
	{"tr", "Turkish", []string{"tur", "turkish"}},
	{"pl", "Polish", []string{"pol", "polish"}},
	{"nl", "Dutch", []string{"dut", "nld", "dutch"}},
	{"sv", "Swedish", []string{"swe", "swedish"}},
	{"no", "Norwegian", []string{"nor", "nob", "nno", "norwegian"}},
	{"da", "Danish", []string{"dan", "danish"}},
	{"fi", "Finnish", []string{"fin", "finnish"}},
	{"cs", "Czech", []string{"cze", "ces", "czech"}},
	{"el", "Greek", []string{"gre", "ell", "greek"}},
	{"he", "Hebrew", []string{"heb", "hebrew"}},
	{"hu", "Hungarian", []string{"hun", "hungarian"}},
	{"ro", "Romanian", []string{"rum", "ron", "romanian"}},
	{"uk", "Ukrainian", []string{"ukr", "ukrainian"}},
	{"th", "Thai", []string{"tha", "thai"}},
	{"vi", "Vietnamese", []string{"vie", "vietnamese"}},
	{"id", "Indonesian", []string{"ind", "indonesian"}},
	{"ms", "Malay", []string{"may", "msa", "malay"}},
	{"fa", "Persian", []string{"per", "fas", "persian", "farsi"}},
	{"ur", "Urdu", []string{"urd", "urdu"}},
	{"bg", "Bulgarian", []string{"bul", "bulgarian"}},
	{"hr", "Croatian", []string{"hrv", "croatian"}},
	{"sr", "Serbian", []string{"srp", "serbian"}},
	{"sk", "Slovak", []string{"slo", "slk", "slovak"}},
	{"sl", "Slovenian", []string{"slv", "slovenian"}},
	{"ca", "Catalan", []string{"cat", "catalan"}},
	{"la", "Latin", []string{"lat", "latin"}},
}

var (
	byAlias = map[string]*entry{}
	byCode  = map[string]*entry{}
)

func init() {
	for i := range table {
		e := &table[i]
		byCode[e.code] = e
		byAlias[e.code] = e
		for _, a := range e.aliases {
			byAlias[a] = e
		}
	}
}

// Normalize returns the canonical code for a language tag ("eng", "en-US",
// "English" all give "en"). Unknown tags that look like a code come back
// lowercased; anything else, including "und" (undetermined), gives "".
func Normalize(tag string) string {
	t := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(t, "-_"); i > 0 {
		t = t[:i]
	}
	if t == "" || t == "und" || t == "unk" || t == "zxx" || t == "mis" || t == "mul" {
		return ""
	}
	if e, ok := byAlias[t]; ok {
		return e.code
	}
	if len(t) == 2 || len(t) == 3 {
		return t
	}
	return ""
}

// Label is a human-readable name for a canonical code (the code itself when unknown).
func Label(code string) string {
	if e, ok := byCode[code]; ok {
		return e.label
	}
	return strings.ToUpper(code)
}

// Option is one selectable language in a preference menu.
type Option struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Options lists every language the preference menus offer.
func Options() []Option {
	out := make([]Option, len(table))
	for i, e := range table {
		out[i] = Option{e.code, e.label}
	}
	return out
}

package scanner

import (
	"encoding/xml"
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// NFOFields is a partial edit of a title's NFO. A nil field is left alone; an
// empty string or slice clears it.
type NFOFields struct {
	Title      *string
	Plot       *string
	Studio     *string
	Genres     *[]string // replaces every <genre> and <tag>, written back as <genre>
	Performers *[]string // replaces the cast, in this order; kept actors retain their extra fields
	Season     *int
	Episode    *int
}

var (
	nfoGenreBlock = regexp.MustCompile(`(?s)[ \t]*(?:<(genre|tag)>.*?</(?:genre|tag)>|<(?:genre|tag)\s*/>)[ \t]*\r?\n?`)
)

func nfoEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

// setNFOElement replaces the first <tag> element, or inserts one before </movie>.
func setNFOElement(doc, tag, value string) string {
	re := regexp.MustCompile(`(?s)<` + tag + `>.*?</` + tag + `>|<` + tag + `\s*/>`)
	element := "<" + tag + ">" + nfoEscape(value) + "</" + tag + ">"
	if re.MatchString(doc) {
		done := false
		return re.ReplaceAllStringFunc(doc, func(old string) string {
			if done {
				return old
			}
			done = true
			return element
		})
	}
	return insertBeforeMovieEnd(doc, "  "+element+"\n")
}

func insertBeforeMovieEnd(doc, text string) string {
	end := strings.LastIndex(doc, "</movie>")
	if end < 0 {
		return doc
	}
	return doc[:end] + text + doc[end:]
}

// EditNFO applies the fields to an existing NFO, leaving every other byte of
// the file alone. It reports whether the file changed.
func EditNFO(path string, f NFOFields) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := string(data)

	if f.Title != nil {
		doc = setNFOElement(doc, "title", *f.Title)
	}
	if f.Plot != nil {
		doc = setNFOElement(doc, "plot", *f.Plot)
	}
	if f.Studio != nil {
		doc = setNFOElement(doc, "studio", *f.Studio)
	}
	if f.Season != nil {
		doc = setNFOElement(doc, "season", strconv.Itoa(*f.Season))
	}
	if f.Episode != nil {
		doc = setNFOElement(doc, "episode", strconv.Itoa(*f.Episode))
	}
	if f.Genres != nil {
		doc = nfoGenreBlock.ReplaceAllString(doc, "")
		var b strings.Builder
		for _, genre := range *f.Genres {
			b.WriteString("  <genre>" + nfoEscape(genre) + "</genre>\n")
		}
		doc = insertBeforeMovieEnd(doc, b.String())
	}
	if f.Performers != nil {
		existing := map[string]string{}
		for _, block := range nfoActorBlock.FindAllString(doc, -1) {
			if m := nfoNameTag.FindStringSubmatch(block); m != nil {
				key := nfoNameKey(html.UnescapeString(m[1]))
				if _, ok := existing[key]; !ok {
					existing[key] = block
				}
			}
		}
		doc = nfoActorBlock.ReplaceAllString(doc, "")
		var b strings.Builder
		written := map[string]bool{}
		for _, name := range *f.Performers {
			key := nfoNameKey(name)
			if key == "" || written[key] {
				continue
			}
			written[key] = true
			if block, ok := existing[key]; ok {
				b.WriteString(strings.TrimRight(block, "\r\n") + "\n")
			} else {
				b.WriteString("  <actor><name>" + nfoEscape(name) + "</name></actor>\n")
			}
		}
		doc = insertBeforeMovieEnd(doc, b.String())
	}
	return writeIfChanged(path, string(data), doc)
}

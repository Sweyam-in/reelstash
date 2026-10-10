// Package names normalizes people's names so spelling variants such as
// "Buck  Richards", "Richards, Buck" and "búck-richards" compare equal.
package names

import (
	"sort"
	"strings"
	"unicode"
)

var accentFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "ā", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e", "ē", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ī", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o", "ō", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ū", "u",
	"ý", "y", "ÿ", "y", "ñ", "n", "ç", "c", "ß", "ss", "š", "s", "ž", "z", "č", "c", "ł", "l",
)

// Normalize lower-cases a name, folds common accents and reduces
// everything that is not a letter or digit to single spaces.
func Normalize(name string) string {
	name = accentFold.Replace(strings.ToLower(name))
	var b strings.Builder
	space := true
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// SortedTokens sorts the words of a normalized name, so word order is ignored.
func SortedTokens(normalized string) string {
	tokens := strings.Fields(normalized)
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

// Key is the order-independent comparison key for a name ("" when it has no letters or digits).
func Key(name string) string { return SortedTokens(Normalize(name)) }

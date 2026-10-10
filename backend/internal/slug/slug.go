// Package slug builds stable URL slugs from display names.
//
// Category rows and NFO-imported genres both store a slug derived the same
// way, so a name written by the scanner matches a category an admin created.
package slug

import "strings"

// Name lower-cases name and turns every run of non-alphanumeric characters
// into a single hyphen. Leading and trailing hyphens are dropped.
// "Sci-Fi & Fantasy" and "sci fi fantasy" both become "sci-fi-fantasy".
func Name(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

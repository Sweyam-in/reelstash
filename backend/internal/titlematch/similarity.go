package titlematch

import (
	"regexp"
	"sort"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9 ]`)

// normalize lowercases, strips punctuation and collapses whitespace.
func normalize(s string) string {
	return strings.Join(strings.Fields(nonAlnum.ReplaceAllString(strings.ToLower(s), " ")), " ")
}

// Similarity is a 0-100 score for how alike two titles are: the better of a
// plain and a word-order-independent normalized Levenshtein ratio.
func Similarity(a, b string) float64 {
	na, nb := normalize(a), normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 100
	}
	plain := levenshteinRatio(na, nb)
	if sorted := levenshteinRatio(sortWords(na), sortWords(nb)); sorted > plain {
		return sorted
	}
	return plain
}

func sortWords(s string) string {
	words := strings.Fields(s)
	sort.Strings(words)
	return strings.Join(words, " ")
}

func levenshteinRatio(a, b string) float64 {
	maxLen := max(len(a), len(b))
	if maxLen == 0 {
		return 100
	}
	return (1 - float64(levenshtein(a, b))/float64(maxLen)) * 100
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

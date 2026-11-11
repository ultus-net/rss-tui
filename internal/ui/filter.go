package ui

import "strings"

// fuzzyMatch reports whether every whitespace-separated token appears in text
// as an in-order subsequence, case-insensitively. Substring hits rank first,
// which is enough for a headline filter without a dependency.
func fuzzyMatch(query, text string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	t := strings.ToLower(text)
	for _, token := range strings.Fields(q) {
		if !subsequence(token, t) {
			return false
		}
	}
	return true
}

func subsequence(needle, hay string) bool {
	if needle == "" {
		return true
	}
	if strings.Contains(hay, needle) {
		return true
	}
	runes := []rune(needle)
	i := 0
	for _, r := range hay {
		if r == runes[i] {
			i++
			if i == len(runes) {
				return true
			}
		}
	}
	return false
}

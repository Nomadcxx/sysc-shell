package shell

import (
	"strings"
	"unicode/utf8"
)

// wrapLines breaks s into measured lines, using spaces where possible and
// rune boundaries for words wider than the line. It caps expanded toast text
// and marks truncated content with an ellipsis.
func wrapLines(s string, width int, measure func(string) int, maxLines int) []string {
	var lines []string
	line := ""
	flush := func() { lines = append(lines, line); line = "" }
	for _, word := range strings.Fields(s) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if measure(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			flush()
		}
		for measure(word) > width {
			cut := len(word)
			for cut > 0 && measure(word[:cut]) > width {
				_, size := utf8.DecodeLastRuneInString(word[:cut])
				cut -= size
			}
			if cut == 0 {
				_, cut = utf8.DecodeRuneInString(word)
			}
			line = word[:cut]
			flush()
			word = word[cut:]
		}
		line = word
	}
	if line != "" {
		flush()
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] += "…"
	}
	return lines
}

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
		if line != "" {
			if measure(line+" "+word) <= width {
				line += " " + word
				continue
			}
			flush()
		}
		if measure(word) <= width {
			line = word
			continue
		}
		for len(word) > 0 {
			cut := 0
			for runes := max(width/8, 1); cut < len(word) && runes > 0; runes-- {
				_, size := utf8.DecodeRuneInString(word[cut:])
				cut += size
			}
			for cut > 0 && measure(word[:cut]) > width {
				_, size := utf8.DecodeLastRuneInString(word[:cut])
				cut -= size
			}
			if cut == 0 {
				_, cut = utf8.DecodeRuneInString(word)
			}
			line = word[:cut]
			word = word[cut:]
			if word != "" {
				flush()
			}
		}
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

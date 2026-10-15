package main

import "strings"

// block is one fenced region of a markdown document, with the line the opening
// fence sits on so a finding can point at it.
type block struct {
	info    string
	marker  string
	line    int
	lines   []string
	skipped string
}

// checkedTags are the fence languages whose contents this gate understands.
// Anything else is prose that happens to sit in a box.
var checkedTags = map[string]bool{"json": true, "kdl": true, "sh": true, "bash": true}

// extract returns every fenced block, tagged or not, with 1-based line numbers.
func extract(data string) []block {
	var out []block
	open := -1
	for i, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if open < 0 {
			if marker, info, ok := fenceOpen(line); ok {
				out = append(out, block{info: info, marker: marker, line: i + 1})
				open = len(out) - 1
			}
			continue
		}
		if closes(line, out[open].marker) {
			open = -1
			continue
		}
		out[open].lines = append(out[open].lines, raw)
	}
	for i := range out {
		if !checkedTags[out[i].info] {
			out[i].skipped = "tag " + out[i].info
		}
	}
	return out
}

// fenceOpen reports the fence run and info string of a line that opens a
// fenced block. A backtick fence may not carry a backtick in its info string.
func fenceOpen(line string) (marker, info string, ok bool) {
	if !strings.HasPrefix(line, "```") && !strings.HasPrefix(line, "~~~") {
		return "", "", false
	}
	marker = line[:1]
	rest := line[1:]
	marker += rest[:len(rest)-len(strings.TrimLeft(rest, "`~"))]
	info = strings.TrimSpace(rest[len(marker)-1:])
	if strings.Contains(info, "`") {
		return "", "", false
	}
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		info = info[:i]
	}
	return marker, info, true
}

// closes reports whether a line ends a block opened by marker. A closing fence
// is at least as long and carries nothing else.
func closes(line, marker string) bool {
	if !strings.HasPrefix(line, marker[:1]) {
		return false
	}
	rest := strings.TrimLeft(line, "`~")
	return len(line)-len(rest) >= len(marker) && strings.TrimSpace(rest) == ""
}

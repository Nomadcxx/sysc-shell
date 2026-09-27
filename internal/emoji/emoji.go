// Package emoji is the launcher's embedded emoji table and search.
package emoji

import (
	_ "embed"
	"sort"
	"strings"
	"sync"
)

//go:embed emoji.tsv
var table string

type Emoji struct {
	Char     string
	Name     string
	Keywords []string
}

var (
	once sync.Once
	all  []Emoji
)

func All() []Emoji {
	once.Do(func() {
		for _, line := range strings.Split(strings.TrimRight(table, "\n"), "\n") {
			f := strings.Split(line, "\t")
			if len(f) < 2 {
				continue
			}
			e := Emoji{Char: f[0], Name: f[1]}
			if len(f) > 2 && f[2] != "" {
				e.Keywords = strings.Split(f[2], "|")
			}
			all = append(all, e)
		}
	})
	return all
}

// class orders matches: lower is better; -1 is no match.
func class(e Emoji, q string) int {
	name := strings.ToLower(e.Name)
	switch {
	case name == q:
		return 0
	case strings.HasPrefix(name, q+" "):
		return 1 // the query is the name's whole first word: "party popper"
	case strings.HasPrefix(name, q):
		return 2 // "partying face"
	}
	for _, w := range strings.Fields(name) {
		if strings.HasPrefix(w, q) {
			return 3
		}
	}
	for _, k := range e.Keywords {
		if strings.HasPrefix(strings.ToLower(k), q) {
			return 4
		}
	}
	if strings.Contains(name, q) {
		return 5
	}
	return -1
}

func Search(query string, limit int) []Emoji {
	q := strings.ToLower(strings.TrimSpace(query))
	src := All()
	if q == "" {
		return src[:min(limit, len(src))]
	}
	type hit struct {
		e     Emoji
		class int
		order int
	}
	var hits []hit
	for i, e := range src {
		if c := class(e, q); c >= 0 {
			hits = append(hits, hit{e, c, i})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].class != hits[j].class {
			return hits[i].class < hits[j].class
		}
		return hits[i].order < hits[j].order
	})
	out := make([]Emoji, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.e)
	}
	return out
}

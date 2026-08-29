package render

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
)

// faceCacheLimit bounds the resolved-face cache. Shell text touches a handful
// of families; without a bound the cache would grow with every fallback rune.
const faceCacheLimit = 16

// FontMap resolves faces from the system font set with per-rune fallback.
//
// It is owned by the Wayland owner goroutine, which is the only goroutine that
// paints. *font.Face is not safe for concurrent use, so one shared map is
// correct here and per-host duplication would be waste, not safety.
type FontMap struct {
	inner   *fontscan.FontMap
	primary *font.Face
	cache   map[rune]*font.Face
	order   []rune
}

// DefaultFontCacheDir is the fontscan disk-cache location.
func DefaultFontCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "sysc-shell", "fontscan")
}

// NewSystemFontMap scans system fonts and resolves the requested family, with a
// generic sans-serif fallback.
//
// A cache directory that cannot be created degrades to an uncached scan rather
// than failing: a missing font cache costs startup time, never correctness.
func NewSystemFontMap(family, cacheDir string) (*FontMap, error) {
	inner := fontscan.NewFontMap(nil)
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			cacheDir = ""
		}
	}
	_ = inner.UseSystemFonts(cacheDir)

	families := []string{"sans-serif"}
	if family != "" && family != "sans-serif" {
		families = append([]string{family}, families...)
	}
	inner.SetQuery(fontscan.Query{Families: families})

	primary := inner.ResolveFace('A')
	if primary == nil {
		return nil, fmt.Errorf("render: no system font resolved for %v", families)
	}
	return &FontMap{inner: inner, primary: primary, cache: make(map[rune]*font.Face)}, nil
}

// Primary is the face the configured family resolved to.
func (m *FontMap) Primary() *font.Face { return m.primary }

// Face resolves one rune, falling back per rune and caching the result.
//
// A rune nothing covers resolves to the primary face, which draws its notdef
// box. Text never fails a frame because of a missing glyph.
func (m *FontMap) Face(r rune) *font.Face {
	if face, ok := m.cache[r]; ok {
		return face
	}
	face := m.inner.ResolveFace(r)
	if face == nil {
		face = m.primary
	}
	if len(m.order) >= faceCacheLimit {
		delete(m.cache, m.order[0])
		m.order = m.order[1:]
	}
	m.cache[r] = face
	m.order = append(m.order, r)
	return face
}

// Run is one span of text shaped with a single face.
type Run struct {
	Face *font.Face
	Text string
}

// SplitRuns divides text at face boundaries so each run shapes with one face.
// A run boundary is where per-rune fallback changed the resolved face.
func (m *FontMap) SplitRuns(text string) []Run {
	var runs []Run
	for _, r := range text {
		face := m.Face(r)
		if n := len(runs); n > 0 && runs[n-1].Face == face {
			runs[n-1].Text += string(r)
			continue
		}
		runs = append(runs, Run{Face: face, Text: string(r)})
	}
	return runs
}

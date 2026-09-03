package render

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
)

// faceCacheLimit bounds the resolved-face cache. Shell text touches a handful
// of families, weights, and runes; without a bound the cache would grow with
// every fallback rune at every weight.
const faceCacheLimit = 64

// FaceRequest is the face one run of text asks for. It is the cache key
// alongside the rune, so a bold label and a regular one of the same string do
// not share an entry.
type FaceRequest struct {
	Family string
	Weight int
	Italic bool
}

// aspect converts the request to the scanner's own selector.
func (q FaceRequest) aspect() font.Aspect {
	style := font.StyleNormal
	if q.Italic {
		style = font.StyleItalic
	}
	weight := font.Weight(q.Weight)
	if q.Weight <= 0 {
		weight = font.WeightNormal
	}
	return font.Aspect{Style: style, Weight: weight}
}

// faceKey identifies one resolved face: the rune it had to cover and the
// request it was resolved for.
type faceKey struct {
	r      rune
	family string
	weight int
	italic bool
}

// FontMap resolves faces from the system font set with per-rune fallback.
//
// A bar owns its map and the Wayland owner goroutine is the only goroutine that
// shapes or paints through it. *font.Face and fontscan.FontMap are not safe for
// concurrent use.
type FontMap struct {
	inner   *fontscan.FontMap
	primary *font.Face
	// family is the configured family the map was built for. A request that
	// names no family resolves against it.
	family string
	cache  map[faceKey]*font.Face
	order  []faceKey
	// query is the request the scanner is currently set to, so a run of text
	// in one face does not re-set the query for every rune.
	query    FaceRequest
	querySet bool
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
	return &FontMap{
		inner:   inner,
		primary: primary,
		family:  family,
		cache:   make(map[faceKey]*font.Face),
	}, nil
}

// Family is the family this map was built for. A caller that asks for a
// different one still resolves through the same scanner.
func (m *FontMap) Family() string { return m.family }

// setQuery points the scanner at one family and aspect. fontscan reports the
// closest face it has rather than failing, so a family that is not installed
// or a weight that has no cut degrades to the nearest match instead of losing
// a frame.
func (m *FontMap) setQuery(req FaceRequest) {
	if m.querySet && m.query == req {
		return
	}
	family := req.Family
	if family == "" {
		family = m.family
	}
	families := []string{"sans-serif"}
	if family != "" && family != "sans-serif" {
		families = append([]string{family}, families...)
	}
	m.inner.SetQuery(fontscan.Query{Families: families, Aspect: req.aspect()})
	m.query, m.querySet = req, true
}

// Primary is the face the configured family resolved to.
func (m *FontMap) Primary() *font.Face { return m.primary }

// Face resolves one rune, falling back per rune and caching the result.
//
// A rune nothing covers resolves to the primary face, which draws its notdef
// box. Text never fails a frame because of a missing glyph. Bitmap (CBDT)
// coverage is kept so colour emoji can paint; COLR/SVG still degrade to notdef.
func (m *FontMap) Face(r rune, req FaceRequest) *font.Face {
	key := faceKey{r: r, family: req.Family, weight: req.Weight, italic: req.Italic}
	if face, ok := m.cache[key]; ok {
		return face
	}
	// The project face wins for its own range, so a system font that happens
	// to cover the private-use area can never take an icon rune. Weight and
	// style do not apply: the icon inventory has one cut.
	face := iconFaceFor(r)
	if face == nil {
		m.setQuery(req)
		// Emoji is Common, which is not a strong script. Without this, fontscan
		// never searches script fallbacks and Noto Color Emoji is never tried.
		m.inner.SetScript(language.LookupScript(r))
		face = outlineFaceForRune(m.inner.ResolveFace(r), m.primary, r)
	}
	if len(m.order) >= faceCacheLimit {
		delete(m.cache, m.order[0])
		m.order = m.order[1:]
	}
	m.cache[key] = face
	m.order = append(m.order, key)
	return face
}

// iconFaceFor returns the project face for an icon rune, or nil.
func iconFaceFor(r rune) *font.Face {
	inWeather := r >= iconRuneFirst && r <= iconRuneLast
	inBattery := r >= batteryRuneFirst && r <= batteryRuneLast
	inMetric := r >= metricRuneFirst && r <= metricRuneLast
	inRecorder := r >= recorderRuneFirst && r <= recorderRuneLast
	inNotify := r >= notifyRuneFirst && r <= notifyRuneLast
	if !inWeather && !inBattery && !inMetric && !inRecorder && !inNotify {
		return nil
	}
	return loadIconFace()
}

func outlineFaceForRune(candidate, primary *font.Face, r rune) *font.Face {
	if candidate == nil {
		return primary
	}
	gid, ok := candidate.NominalGlyph(r)
	if !ok {
		return primary
	}
	if _, ok := candidate.GlyphDataBitmap(gid); ok {
		return candidate
	}
	if _, ok := candidate.GlyphDataOutline(gid); ok {
		return candidate
	}
	return primary
}

// Run is one span of text shaped with a single face.
type Run struct {
	Face *font.Face
	Text string
}

// SplitRuns divides text at face boundaries so each run shapes with one face.
// A run boundary is where per-rune fallback changed the resolved face.
func (m *FontMap) SplitRuns(text string, req FaceRequest) []Run {
	var runs []Run
	for _, r := range text {
		face := m.Face(r, req)
		if n := len(runs); n > 0 && runs[n-1].Face == face {
			runs[n-1].Text += string(r)
			continue
		}
		runs = append(runs, Run{Face: face, Text: string(r)})
	}
	return runs
}

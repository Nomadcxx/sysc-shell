package render

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
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
	// icon is this map's own face for the project icon inventory, parsed on
	// first use. It is per-map for the same reason the material subset is
	// per-renderer: shaping mutates a face, so sharing one across surfaces
	// makes two goroutines write to the same glyph caches.
	icon       *font.Face
	iconLoaded bool
	// weighted holds one instance per variable font and requested weight.
	// It is separate from cache because every rune of a run must resolve to
	// the same instance, or SplitRuns breaks the run at each rune.
	weighted map[weightedKey]*font.Face
}

// weightedKey names one weight instance of one parsed font.
type weightedKey struct {
	font   *font.Font
	weight int
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
	face := m.iconFaceFor(r)
	if face == nil {
		m.setQuery(req)
		// Emoji is Common, which is not a strong script. Without this, fontscan
		// never searches script fallbacks and Noto Color Emoji is never tried.
		m.inner.SetScript(language.LookupScript(r))
		face = outlineFaceForRune(m.inner.ResolveFace(r), m.primary, r)
		face = m.atWeight(face, req.Weight)
	}
	if len(m.order) >= faceCacheLimit {
		delete(m.cache, m.order[0])
		m.order = m.order[1:]
	}
	m.cache[key] = face
	m.order = append(m.order, key)
	return face
}

// wghtAxis is the OpenType weight axis.
var wghtAxis = ot.MustNewTag("wght")

// atWeight returns face set to weight on its wght axis, or face itself when
// the font is not variable or the weight is its default.
//
// fontscan indexes a variable font once, as its default instance, so a
// request for 600 from a family shipped as one variable file (Inter Variable,
// the default family) resolves to the regular instance. Without this every
// weight in the type ramp painted at 400.
func (m *FontMap) atWeight(face *font.Face, weight int) *font.Face {
	if face == nil || weight <= 0 {
		return face
	}
	key := weightedKey{font: face.Font, weight: weight}
	if v, ok := m.weighted[key]; ok {
		return v
	}
	// The instance gets its own Font value, sharing the parsed tables. The
	// go-text shaper caches its HarfBuzz font per *font.Font and builds it
	// from the first face it sees, so instances sharing one Font would all
	// shape with that face's advances and kerning.
	inst := *face.Font
	v := font.NewFace(&inst)
	v.SetVariations([]font.Variation{{Tag: wghtAxis, Value: float32(weight)}})
	if !hasVariation(v.Coords()) {
		v = face
	}
	if m.weighted == nil {
		m.weighted = make(map[weightedKey]*font.Face)
	}
	m.weighted[key] = v
	return v
}

// hasVariation reports whether normalised coordinates move off the default
// instance. A static font has none; a default weight has only zeros.
func hasVariation[T comparable](coords []T) bool {
	var zero T
	for _, c := range coords {
		if c != zero {
			return true
		}
	}
	return false
}

// iconFaceFor returns this map's project face for an icon rune, or nil.
func (m *FontMap) iconFaceFor(r rune) *font.Face {
	inWeather := r >= iconRuneFirst && r <= iconRuneLast
	inBattery := r >= batteryRuneFirst && r <= batteryRuneLast
	inMetric := r >= metricRuneFirst && r <= metricRuneLast
	inRecorder := r >= recorderRuneFirst && r <= recorderRuneLast
	inNotify := r >= notifyRuneFirst && r <= notifyRuneLast
	inGauge := r >= gaugeRuneFirst && r <= gaugeRuneLast
	inNight := r >= iconClearNight && r <= iconPartlyCloudyNight
	inDetail := r >= detailRuneFirst && r <= detailRuneLast
	inDevice := r >= iconSmartphone && r <= iconSignalCellular4Bar
	inGPUMetric := r == iconGPU
	inCross := r == iconCross
	inCat := r >= catRuneFirst && r <= catRuneLast
	if !inDevice && !inWeather && !inBattery && !inMetric && !inRecorder && !inNotify && !inGauge && !inNight && !inDetail && !inGPUMetric && !inCross && !inCat {
		return nil
	}
	if !m.iconLoaded {
		m.icon, m.iconLoaded = newIconFace(), true
	}
	return m.icon
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

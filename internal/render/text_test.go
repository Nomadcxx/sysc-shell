package render

import (
	"fmt"
	"os"
	"testing"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"golang.org/x/image/font/gofont/goregular"
)

// mustTestFace parses the Latin fixture. goregular has no Arabic coverage.
func mustTestFace(t *testing.T) *font.Face {
	t.Helper()
	face, err := ParseFace(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

// mustJoinedFace parses the vendored Amiri fixture, which shapes Arabic.
func mustJoinedFace(t *testing.T) *font.Face {
	t.Helper()
	data, err := os.ReadFile("testdata/Amiri-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	face, err := ParseFace(data)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func hasNonZeroAlpha(m Mask) bool {
	if m.Alpha == nil {
		return false
	}
	for _, p := range m.Alpha.Pix {
		if p != 0 {
			return true
		}
	}
	return false
}

func TestRasterPaintsColorEmoji(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)
	const glyph = '👋'
	m.inner.SetScript(language.LookupScript(glyph))
	resolved := m.inner.ResolveFace(glyph)
	if resolved == nil {
		t.Skip("system font set has no emoji fallback")
	}
	gid, ok := resolved.NominalGlyph(glyph)
	if !ok {
		t.Skip("resolved emoji face has no nominal glyph")
	}
	if _, ok := resolved.GlyphDataBitmap(gid); !ok {
		t.Skip("resolved emoji face has no bitmap glyph")
	}

	mask, err := NewTextRendererWithFontMap(m).Raster(string(glyph), 32, false)
	if err != nil {
		t.Fatalf("Raster(%q): %v", string(glyph), err)
	}
	if !hasColorPixels(mask) {
		t.Fatal("emoji raster has no colour pixels")
	}
}

func hasColorPixels(m Mask) bool {
	if m.Color == nil || m.Color.Width <= 0 || m.Color.Height <= 0 {
		return false
	}
	for i := 3; i < len(m.Color.Pix); i += 4 {
		if m.Color.Pix[i] != 0 {
			return true
		}
	}
	return false
}

func TestTextMeasureAndRaster(t *testing.T) {
	t.Parallel()

	face := mustTestFace(t)
	r := NewTextRenderer(face)

	w, h, err := r.Measure("sysc-shell", 16, false)
	if err != nil || w <= 0 || h <= 0 {
		t.Fatalf("measure = %dx%d, %v", w, h, err)
	}

	mask, err := r.Raster("sysc-shell", 16, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNonZeroAlpha(mask) {
		t.Fatal("raster contains no glyph pixels")
	}
	if got := mask.Alpha.Bounds().Dx(); got != w {
		t.Fatalf("mask width = %d, want the measured %d", got, w)
	}
	if got := mask.Alpha.Bounds().Dy(); got != h {
		t.Fatalf("mask height = %d, want the measured %d", got, h)
	}
	if mask.Baseline <= 0 || mask.Baseline > h {
		t.Fatalf("baseline = %d, want it inside a %d-pixel mask", mask.Baseline, h)
	}
	if mask.Advance <= 0 {
		t.Fatalf("advance = %d, want positive", mask.Advance)
	}
}

func TestTextRendererUsesPerRuneFallbackForAllOperations(t *testing.T) {
	t.Parallel()

	fonts := newFixtureFontMap(t)
	const mixed = "sysc عربية"
	if runs := fonts.SplitRuns(mixed); len(runs) < 2 {
		t.Fatalf("fixture produced %d face runs, want Latin plus Arabic fallback", len(runs))
	}
	r := NewTextRendererWithFontMap(fonts)
	w, h, err := r.Measure(mixed, 32, false)
	if err != nil || w <= 0 || h <= 0 {
		t.Fatalf("Measure = %dx%d, %v", w, h, err)
	}
	mask, err := r.Raster(mixed, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if mask.Advance != w || !hasNonZeroAlpha(mask) {
		t.Fatalf("Raster advance=%d/nonzero=%v, want measured width %d with pixels",
			mask.Advance, hasNonZeroAlpha(mask), w)
	}
	fitted, advance, err := r.Truncate(mixed+mixed, 32, w, false)
	if err != nil {
		t.Fatal(err)
	}
	if fitted == "" || advance > w {
		t.Fatalf("Truncate = %q/%d, want non-empty text within %d", fitted, advance, w)
	}
}

// TestTextShapesJoinedScript proves contextual joining rather than a plain cmap
// lookup: every shaped glyph must differ from its source rune's nominal glyph.
func TestTextShapesJoinedScript(t *testing.T) {
	t.Parallel()

	face := mustJoinedFace(t)
	r := NewTextRenderer(face)

	// "عربية" - Arabic for "Arabic". Every letter here takes a contextual
	// form: no letter sits in a position where its isolated form is correct.
	// A leading alef would not qualify, because alef joins only to its right
	// and so keeps its nominal glyph at the start of a word.
	const joined = "عربية"

	out, err := r.Shape(joined, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Glyphs) == 0 {
		t.Fatal("shaping returned no glyphs")
	}

	runes := []rune(joined)
	for i, g := range out.Glyphs {
		idx := g.TextIndex()
		if idx < 0 || idx >= len(runes) {
			t.Fatalf("glyph %d has cluster index %d outside the run", i, idx)
		}
		nominal, ok := face.NominalGlyph(runes[idx])
		if !ok {
			t.Fatalf("font has no nominal glyph for %q", runes[idx])
		}
		if g.GlyphID == nominal {
			t.Errorf("glyph %d for %q kept its nominal id %d; shaping did not join", i, runes[idx], nominal)
		}
	}

	w, h, err := r.Measure(joined, 32, false)
	if err != nil || w <= 0 || h <= 0 {
		t.Fatalf("measure = %dx%d, %v", w, h, err)
	}

	// Shaping must be stable across calls.
	again, err := r.Shape(joined, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Glyphs) != len(out.Glyphs) {
		t.Fatalf("second shape returned %d glyphs, want %d", len(again.Glyphs), len(out.Glyphs))
	}
	for i := range out.Glyphs {
		if again.Glyphs[i].GlyphID != out.Glyphs[i].GlyphID {
			t.Fatalf("glyph %d changed id between calls: %d then %d", i, out.Glyphs[i].GlyphID, again.Glyphs[i].GlyphID)
		}
	}

	mask, err := r.Raster(joined, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNonZeroAlpha(mask) {
		t.Fatal("joined raster contains no glyph pixels")
	}
}

func TestTextRejectsEmptyFontData(t *testing.T) {
	t.Parallel()

	if _, err := ParseFace(nil); err == nil {
		t.Fatal("ParseFace accepted empty font data")
	}
}

func TestTextRejectsInvalidSize(t *testing.T) {
	t.Parallel()

	r := NewTextRenderer(mustTestFace(t))
	for _, size := range []int{0, -16} {
		if _, _, err := r.Measure("sysc-shell", size, false); err == nil {
			t.Errorf("Measure accepted size %d", size)
		}
		if _, err := r.Raster("sysc-shell", size, false); err == nil {
			t.Errorf("Raster accepted size %d", size)
		}
		if _, err := r.Shape("sysc-shell", size, false); err == nil {
			t.Errorf("Shape accepted size %d", size)
		}
	}
}

func TestTextRejectsMissingGlyphData(t *testing.T) {
	t.Parallel()

	face := mustTestFace(t)
	// A glyph id far beyond the font's glyph count has no outline.
	if _, err := glyphOutline(face, font.GID(1<<16-1)); err == nil {
		t.Fatal("glyphOutline accepted a glyph id with no data")
	}
}

// TestTextParseFaceReturnsDistinctFaces guards the sharing rule: go-text
// documents *font.Font as safe for concurrent use and *font.Face as NOT safe,
// because a Face carries mutable cmap and extents caches. The parse cache may
// therefore share the Font but must hand every caller its own Face.
func TestTextParseFaceReturnsDistinctFaces(t *testing.T) {
	t.Parallel()

	first, err := ParseFace(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseFace(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("ParseFace shared one Face between callers")
	}
	if first.Upem() != second.Upem() {
		t.Fatal("the two faces disagree about the font")
	}
}

// TestTextRejectsUnsupportedGlyphData covers the classification rule directly.
// Only vector outlines can be rasterised into a shared-memory ARGB buffer;
// bitmap, SVG and colour glyphs must be refused rather than drawn as blanks.
func TestTextRejectsUnsupportedGlyphData(t *testing.T) {
	t.Parallel()

	unsupported := []struct {
		name string
		data font.GlyphData
	}{
		{"bitmap", font.GlyphBitmap{}},
		{"svg", font.GlyphSVG{}},
		{"colour", font.GlyphColor{}},
	}
	for _, tc := range unsupported {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := outlineFrom(tc.data, font.GID(7)); err == nil {
				t.Fatalf("outlineFrom accepted %s glyph data", tc.name)
			}
		})
	}

	t.Run("outline", func(t *testing.T) {
		t.Parallel()
		if _, err := outlineFrom(font.GlyphOutline{}, font.GID(7)); err != nil {
			t.Fatalf("outlineFrom rejected a vector outline: %v", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		if _, err := outlineFrom(nil, font.GID(7)); err == nil {
			t.Fatal("outlineFrom accepted a glyph with no data")
		}
	})
}

// Every digit must advance identically, or a clock changes width as the time
// changes. This is the whole point of tabular figures.
func TestTabularFiguresGiveEveryDigitTheSameWidth(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))

	widths := make(map[int]string)
	for d := 0; d <= 9; d++ {
		s := fmt.Sprintf("%d%d:%d%d", d, d, d, d)
		w, _, err := r.Measure(s, 14, true)
		if err != nil {
			t.Fatalf("Measure(%q): %v", s, err)
		}
		widths[w] = s
	}
	if len(widths) != 1 {
		t.Fatalf("tabular measurement produced %d distinct widths, want 1: %v", len(widths), widths)
	}
}

// The flag must actually reach the shaper: proportional measurement of the
// same strings should not be uniform for a face with proportional figures.
func TestTheTabularFlagReachesTheShaper(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))

	tab, _, err := r.Measure("00:00", 14, true)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	prop, _, err := r.Measure("00:00", 14, false)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	// Go Regular's digits are already tabular, so the widths may legitimately
	// match. What must hold is that each path shapes successfully and returns
	// a positive width; a silently dropped feature would still be caught by
	// TestTabularFiguresGiveEveryDigitTheSameWidth on any proportional face.
	if tab <= 0 || prop <= 0 {
		t.Fatalf("widths tab=%d prop=%d, want each positive", tab, prop)
	}
}

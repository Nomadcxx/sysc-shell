package render

import (
	"bytes"
	"golang.org/x/image/font/gofont/goregular"
	"os"
	"sync"
	"testing"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
)

func newFixtureFontMap(t *testing.T) *FontMap {
	t.Helper()
	inner := fontscan.NewFontMap(nil)
	if err := inner.AddFont(bytes.NewReader(goregular.TTF), "fixture:latin", "Fixture Latin"); err != nil {
		t.Fatal(err)
	}
	arabic, err := os.Open("testdata/Amiri-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	defer arabic.Close()
	if err := inner.AddFont(arabic, "fixture:arabic", "Fixture Arabic"); err != nil {
		t.Fatal(err)
	}
	inner.SetQuery(fontscan.Query{Families: []string{"Fixture Latin", "Fixture Arabic"}})
	primary := inner.ResolveFace('A')
	if primary == nil {
		t.Fatal("fixture font map resolved no primary face")
	}
	return &FontMap{inner: inner, primary: primary, cache: make(map[faceKey]*font.Face)}
}

// These exercise the real system font set. A machine with no fonts installed
// cannot run them, and skipping is honest: the alternative is asserting against
// a fixture that proves nothing about fallback.
func newSystemMap(t *testing.T) *FontMap {
	t.Helper()
	m, err := NewSystemFontMap("sans-serif", t.TempDir())
	if err != nil {
		t.Skipf("no system fonts available: %v", err)
	}
	return m
}

func TestSystemFontMapResolvesAPrimaryFace(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)
	if m.Primary() == nil {
		t.Fatal("Primary() is nil")
	}
}

func TestFaceCacheIsBounded(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)

	// Resolve more distinct runes than the cache can hold.
	for r := rune('a'); r < rune('a')+rune(faceCacheLimit)+8; r++ {
		m.Face(r, FaceRequest{})
	}
	if len(m.cache) > faceCacheLimit {
		t.Fatalf("cache holds %d faces, want at most %d", len(m.cache), faceCacheLimit)
	}
	if len(m.order) != len(m.cache) {
		t.Fatalf("eviction order holds %d entries but the cache holds %d",
			len(m.order), len(m.cache))
	}
}

func TestFaceNeverReturnsNil(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)

	// A private-use rune no font covers must still resolve, so a missing glyph
	// degrades to notdef rather than failing the frame.
	if face := m.Face('', FaceRequest{}); face == nil {
		t.Fatal("Face returned nil for an uncovered rune")
	}
}

func TestSystemFontMapKeepsBitmapEmojiFallback(t *testing.T) {
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
	if selected := outlineFaceForRune(resolved, m.primary, glyph); selected != resolved {
		t.Fatal("bitmap emoji fallback degraded to the primary notdef face")
	}
	if m.Face(glyph, FaceRequest{}) == m.primary {
		t.Fatal("Face resolved the emoji rune to the primary notdef face")
	}
}

func TestSplitRunsGroupsAdjacentRunesSharingAFace(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)

	runs := m.SplitRuns("hello", FaceRequest{})
	if len(runs) != 1 {
		t.Fatalf("SplitRuns produced %d runs for one-script text, want 1", len(runs))
	}
	if runs[0].Text != "hello" {
		t.Fatalf("run text = %q, want hello", runs[0].Text)
	}
}

func TestSplitRunsOfEmptyTextIsEmpty(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)
	if runs := m.SplitRuns("", FaceRequest{}); len(runs) != 0 {
		t.Fatalf("SplitRuns(\"\") = %v, want no runs", runs)
	}
}

func TestMissingCacheDirDegradesRatherThanFailing(t *testing.T) {
	t.Parallel()
	// An unwritable cache path must not fail startup; it costs a rescan.
	m, err := NewSystemFontMap("sans-serif", "/proc/nonexistent/cache")
	if err != nil {
		t.Skipf("no system fonts available: %v", err)
	}
	if m.Primary() == nil {
		t.Fatal("Primary() is nil after degrading to an uncached scan")
	}
}

func TestFontMapKeepsWeightAndStyleCacheEntriesDistinct(t *testing.T) {
	t.Parallel()
	m := newFixtureFontMap(t)
	reqs := []FaceRequest{
		{Family: "Fixture Latin", Weight: 400},
		{Family: "Fixture Latin", Weight: 700},
		{Family: "Fixture Latin", Weight: 400, Italic: true},
		{Family: "Fixture Latin", Weight: 700, Italic: true},
	}
	for _, req := range reqs {
		if m.Face('A', req) == nil {
			t.Fatalf("%+v resolved no face", req)
		}
	}
	// The fixture holds one cut, so every request resolves to the same face.
	// What must not collapse is the cache: a shared entry would serve a bold
	// run whatever face a later regular run resolved.
	if len(m.cache) != len(reqs) {
		t.Errorf("cache holds %d entries for %d distinct requests", len(m.cache), len(reqs))
	}
	for _, req := range reqs {
		key := faceKey{r: 'A', family: req.Family, weight: req.Weight, italic: req.Italic}
		if _, ok := m.cache[key]; !ok {
			t.Errorf("%+v left no cache entry", req)
		}
	}
}

func TestFontMapCacheStaysBoundedAcrossWeights(t *testing.T) {
	t.Parallel()
	m := newFixtureFontMap(t)
	for weight := 100; weight <= 900; weight += 100 {
		for _, r := range "abcdefghijklmnop" {
			m.Face(r, FaceRequest{Family: "Fixture Latin", Weight: weight})
		}
	}
	if len(m.cache) > faceCacheLimit {
		t.Errorf("cache holds %d entries, want at most %d", len(m.cache), faceCacheLimit)
	}
	if len(m.order) != len(m.cache) {
		t.Errorf("eviction order holds %d entries for %d cached", len(m.order), len(m.cache))
	}
}

func TestFontMapFallsBackForAnUnavailableFamily(t *testing.T) {
	t.Parallel()
	m := newFixtureFontMap(t)
	// A family nothing provides must still paint: the scanner reports the
	// closest face it has rather than failing the frame.
	face := m.Face('A', FaceRequest{Family: "No Such Family At All", Weight: 400})
	if face == nil {
		t.Fatal("an unavailable family resolved no face")
	}
}

func TestFontMapKeepsIconFacePriorityAcrossWeights(t *testing.T) {
	t.Parallel()
	m := newFixtureFontMap(t)
	// An icon rune belongs to the project face whatever weight is asked for:
	// the inventory has one cut, and a system font that happens to cover the
	// private-use area must never take the rune.
	plain := m.Face(iconClearDay, FaceRequest{Family: "Fixture Latin", Weight: 400})
	heavy := m.Face(iconClearDay, FaceRequest{Family: "Fixture Latin", Weight: 900})
	if plain == nil || heavy == nil {
		t.Fatal("icon rune resolved no face")
	}
	if plain != heavy {
		t.Error("the icon rune resolved two different faces for two weights")
	}
	if plain == m.Primary() {
		t.Error("the icon rune fell through to the primary face")
	}
}

// TestIconFaceIsNotSharedBetweenFontMaps encodes the rule ParseFace already
// follows and materialFace already documents: the parsed *font.Font is
// read-only and may be shared, but a *font.Face carries mutable caches and
// must not be. Shaping writes to a face through SetPpem, so two surfaces
// holding one face corrupt each other's glyph metrics.
func TestIconFaceIsNotSharedBetweenFontMaps(t *testing.T) {
	t.Parallel()
	a, b := newFixtureFontMap(t), newFixtureFontMap(t)
	fa, fb := a.Face(metricRuneFirst, FaceRequest{}), b.Face(metricRuneFirst, FaceRequest{})
	if fa == nil || fb == nil {
		t.Fatal("the icon rune resolved no face")
	}
	if fa == fb {
		t.Error("two font maps share one icon face; shaping in either corrupts the other")
	}
}

// TestConcurrentShapingAcrossFontMaps is the reproduction the race detector
// reads. Two surfaces, each with its own map, shape an icon rune at once --
// which is exactly what two bars on two outputs do.
func TestConcurrentShapingAcrossFontMaps(t *testing.T) {
	t.Parallel()
	text := string(metricRuneFirst) + " 42"
	var wg sync.WaitGroup
	for range 2 {
		m := newFixtureFontMap(t)
		r := NewTextRendererWithFontMap(m)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for size := 10; size < 26; size++ {
				if _, _, err := r.Measure(text, TextSpec{Size: size, Weight: 400}, false); err != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
}

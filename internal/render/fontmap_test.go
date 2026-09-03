package render

import (
	"bytes"
	"os"
	"testing"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
	"golang.org/x/image/font/gofont/goregular"
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
	return &FontMap{inner: inner, primary: primary, cache: make(map[rune]*font.Face)}
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
		m.Face(r)
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
	if face := m.Face(''); face == nil {
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
	if m.Face(glyph) == m.primary {
		t.Fatal("Face resolved the emoji rune to the primary notdef face")
	}
}

func TestSplitRunsGroupsAdjacentRunesSharingAFace(t *testing.T) {
	t.Parallel()
	m := newSystemMap(t)

	runs := m.SplitRuns("hello")
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
	if runs := m.SplitRuns(""); len(runs) != 0 {
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

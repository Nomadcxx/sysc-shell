package render

import "testing"

// Every WMO code the API can return must map to one of the eight symbols. An
// unmapped code renders the cloud rather than a missing glyph.
func TestEveryWeatherCodeMapsToAnIcon(t *testing.T) {
	t.Parallel()
	for code := 0; code <= 99; code++ {
		r := IconRune(code)
		if r < iconRuneFirst || r > iconRuneLast {
			t.Fatalf("code %d mapped to %U, outside the icon range", code, r)
		}
	}
}

func TestKnownCodesMapToTheExpectedSymbol(t *testing.T) {
	t.Parallel()
	cases := map[int]rune{
		0:  iconClearDay,
		2:  iconPartlyCloudy,
		3:  iconCloud,
		45: iconFog,
		61: iconRain,
		71: iconSnow,
		75: iconHeavySnow,
		95: iconThunderstorm,
	}
	for code, want := range cases {
		if got := IconRune(code); got != want {
			t.Fatalf("code %d mapped to %U, want %U", code, got, want)
		}
	}
}

// An icon rune must resolve to the project face, never to whatever system font
// happens to cover the private-use area.
func TestIconRunesResolveToTheProjectFace(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}

	face := m.Face(iconClearDay)
	if face == nil {
		t.Fatal("an icon rune resolved to no face")
	}
	if face == m.Primary() {
		t.Fatal("an icon rune resolved to the primary text face, not the icon face")
	}
}

// SplitRuns must isolate an icon rune so it shapes with the icon face while
// the surrounding text keeps the primary one.
func TestSplitRunsIsolatesAnIconRune(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}

	runs := m.SplitRuns(string(iconClearDay) + " 18")
	if len(runs) < 2 {
		t.Fatalf("runs = %d, want the icon split from the text", len(runs))
	}
	if runs[0].Text != string(iconClearDay) {
		t.Fatalf("first run = %q, want the icon alone", runs[0].Text)
	}
	if runs[0].Face == runs[1].Face {
		t.Fatal("the icon and the text shaped with one face")
	}
}

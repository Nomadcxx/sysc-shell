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

// Plugins address glyphs by catalogue name. Each WMO group has to resolve to
// a name the host already knows, or a weather view would fail Convert.
func TestIconNameMapsWMOGroupsToCatalogueNames(t *testing.T) {
	t.Parallel()
	cases := map[int]string{
		0:  "clear-day",
		1:  "partly-cloudy",
		2:  "partly-cloudy",
		3:  "cloud",
		45: "fog",
		48: "fog",
		61: "rain",
		80: "rain",
		71: "snow",
		85: "snow",
		75: "heavy-snow",
		86: "heavy-snow",
		95: "thunderstorm",
		99: "thunderstorm",
	}
	for code, want := range cases {
		got := IconName(code)
		if got != want {
			t.Fatalf("code %d named %q, want %q", code, got, want)
		}
		r, ok := IconByName(got)
		if !ok {
			t.Fatalf("code %d named %q, which the catalogue does not have", code, got)
		}
		if r != IconRune(code) {
			t.Fatalf("code %d: IconName and IconRune picked different glyphs", code)
		}
	}
}

func TestIconNameFallsBackToCloudForAnUnknownCode(t *testing.T) {
	t.Parallel()
	for _, code := range []int{-1, 4, 20, 49, 70, 90} {
		if got := IconName(code); got != "cloud" {
			t.Fatalf("code %d named %q, want cloud", code, got)
		}
		if IconRune(code) != iconCloud {
			t.Fatalf("code %d rune %U, want the cloud fallback", code, IconRune(code))
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

// Every charge in range maps to a glyph in the battery band, in both states.
func TestEveryChargeMapsToABatteryIcon(t *testing.T) {
	t.Parallel()
	for _, charging := range []bool{false, true} {
		for step := 0; step <= 100; step++ {
			r := BatteryIconRune(float64(step)/100, charging, false)
			if r < batteryRuneFirst || r > batteryRuneLast {
				t.Fatalf("charge %d%% charging=%v mapped to %U, outside the battery band",
					step, charging, r)
			}
		}
	}
}

// The glyph must rise monotonically with charge: a fuller battery never shows
// a smaller glyph than an emptier one.
func TestBatteryGlyphsRiseWithCharge(t *testing.T) {
	t.Parallel()
	previous := BatteryIconRune(0, false, false)
	for step := 1; step <= 100; step++ {
		got := BatteryIconRune(float64(step)/100, false, false)
		if got < previous {
			t.Fatalf("charge %d%% mapped to %U, below the previous %U", step, got, previous)
		}
		previous = got
	}
}

// Charging and discharging must never share a glyph, or the state is invisible.
func TestChargingAndDischargingGlyphsAreDistinct(t *testing.T) {
	t.Parallel()
	for step := 0; step <= 100; step += 5 {
		charge := float64(step) / 100
		if BatteryIconRune(charge, false, false) == BatteryIconRune(charge, true, false) {
			t.Fatalf("charge %d%% renders the same glyph charging and discharging", step)
		}
	}
}

// Critical overrides the level entirely, at any charge.
func TestCriticalOverridesTheLevelGlyph(t *testing.T) {
	t.Parallel()
	for _, charge := range []float64{0, 0.1, 0.5, 1} {
		if got := BatteryIconRune(charge, false, true); got != iconBatteryCritical {
			t.Fatalf("critical at %.0f%% mapped to %U, want the critical glyph", charge*100, got)
		}
	}
}

// A charge outside zero through one is clamped rather than escaping the band.
func TestOutOfRangeChargeIsClamped(t *testing.T) {
	t.Parallel()
	for _, charge := range []float64{-1, -0.01, 1.01, 42} {
		r := BatteryIconRune(charge, false, false)
		if r < batteryRuneFirst || r > batteryRuneLast {
			t.Fatalf("charge %v mapped to %U, outside the battery band", charge, r)
		}
	}
}

// Battery glyphs resolve to the project face, like the weather ones.
func TestBatteryRunesResolveToTheProjectFace(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}
	face := m.Face(iconBatteryCritical)
	if face == nil || face == m.Primary() {
		t.Fatal("a battery rune did not resolve to the icon face")
	}
}

// coverage sums a rasterised glyph's alpha. Two battery levels that differ in
// fill must differ in ink.
func glyphCoverage(t *testing.T, r rune, size int) int {
	t.Helper()
	tr := NewTextRenderer(loadIconFace())
	mask, err := tr.Raster(string(r), size, false)
	if err != nil {
		t.Fatalf("raster %U: %v", r, err)
	}
	if mask.Alpha == nil {
		t.Fatalf("raster %U produced no coverage", r)
	}
	sum := 0
	for _, a := range mask.Alpha.Pix {
		sum += int(a)
	}
	return sum
}

// The level glyphs shipped identical once: every codepoint drew the same solid
// silhouette because the window subpath wound the same way as the body, so it
// filled instead of cutting a hole. BatteryIconRune was correct and the asset
// discarded the distinction, and the existing tests only checked the codepoint
// mapping, so nothing failed.
// Not parallel: loadIconFace returns one shared face and font.Face is not safe
// for concurrent use. The shell only ever shapes on the Wayland goroutine.
func TestBatteryLevelGlyphsDifferFromEachOther(t *testing.T) {
	for _, band := range []struct {
		name  string
		first rune
	}{
		{"discharging", iconBatteryLevel0},
		{"charging", iconBatteryCharging0},
	} {
		t.Run(band.name, func(t *testing.T) {
			seen := make(map[int]rune, batteryLevels)
			prev := -1
			for i := range batteryLevels {
				r := band.first + rune(i)
				got := glyphCoverage(t, r, 64)
				if other, clash := seen[got]; clash {
					t.Fatalf("level %d (%U) has the same ink as %U; the levels are indistinguishable", i, r, other)
				}
				seen[got] = r
				// More charge must never draw less ink.
				if got < prev {
					t.Errorf("level %d (%U) has less ink than the level below it", i, r)
				}
				prev = got
			}
		})
	}
}

// The bar paints at roughly this size, so the levels have to survive it.
func TestBatteryLevelsStayDistinctAtBarSize(t *testing.T) {
	empty := glyphCoverage(t, iconBatteryLevel0, 17)
	full := glyphCoverage(t, iconBatteryLevel0+rune(batteryLevels-1), 17)
	if empty == full {
		t.Fatal("an empty and a full battery rasterise identically at bar size")
	}
}

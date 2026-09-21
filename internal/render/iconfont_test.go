package render

import "testing"

func TestGhostLauncherIconIsInProjectFace(t *testing.T) {
	t.Parallel()
	r, ok := IconByName("ghost")
	if !ok || r != iconGhost {
		t.Fatalf("ghost = %U, %v", r, ok)
	}
	if r < notifyRuneFirst || r > notifyRuneLast {
		t.Fatalf("ghost rune %U is outside the project icon band", r)
	}
}

func TestAIUsageGlyphIsInCatalogueAndHasInk(t *testing.T) {
	t.Parallel()
	r, ok := IconByName("ai-usage")
	if !ok || r != iconAIUsage {
		t.Fatalf("ai-usage = %U, %v", r, ok)
	}
	if r != 0xE030 {
		t.Fatalf("ai-usage rune %U is not the codepoint after the detail band", r)
	}
	if got := glyphCoverage(t, r, 32); got == 0 {
		t.Fatalf("ai-usage glyph %U has no ink", r)
	}
	found := false
	for _, n := range IconNames() {
		if n == "ai-usage" {
			found = true
		}
	}
	if !found {
		t.Fatal("IconNames() does not list ai-usage")
	}
}

func TestGaugeIconsAreDistinctProjectGlyphs(t *testing.T) {
	t.Parallel()
	seen := map[rune]string{}
	for _, id := range []string{"cpu", "memory", "gpu"} {
		r, ok := GaugeIconRune(id)
		if !ok {
			t.Fatalf("%s has no gauge glyph", id)
		}
		if previous := seen[r]; previous != "" {
			t.Fatalf("%s and %s share glyph %U", previous, id, r)
		}
		seen[r] = id
		if got := glyphCoverage(t, r, 32); got == 0 {
			t.Fatalf("%s glyph %U has no ink", id, r)
		}
	}
	if _, ok := GaugeIconRune("temperature"); ok {
		t.Fatal("temperature mapped to an icon instead of its numeric value")
	}
}

func TestGaugeRunesResolveToTheProjectFace(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}
	face := m.Face(gaugeRuneFirst, FaceRequest{})
	if face == nil || face == m.Primary() {
		t.Fatal("a gauge rune did not resolve to the project icon face")
	}
}

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

	face := m.Face(iconClearDay, FaceRequest{})
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

	runs := m.SplitRuns(string(iconClearDay)+" 18", FaceRequest{})
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
	face := m.Face(iconBatteryCritical, FaceRequest{})
	if face == nil || face == m.Primary() {
		t.Fatal("a battery rune did not resolve to the icon face")
	}
}

// coverage sums a rasterised glyph's alpha. Two battery levels that differ in
// fill must differ in ink.
func glyphCoverage(t *testing.T, r rune, size int) int {
	t.Helper()
	tr := NewTextRenderer(newIconFace())
	mask, err := tr.Raster(string(r), TextSpec{Size: size, Weight: 400}, false)
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
func TestBatteryLevelGlyphsDifferFromEachOther(t *testing.T) {
	t.Parallel()
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

func TestRecorderCatalogueNamesResolve(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}

	for _, name := range []string{"camera", "camera-off", "record", "stop", "replay"} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("%q is missing from the catalogue", name)
		}
		face := m.Face(r, FaceRequest{})
		if face == nil {
			t.Fatalf("%q resolved to no face", name)
		}
		if face == m.Primary() {
			t.Fatalf("%q resolved to the primary text face, not the icon face", name)
		}
	}
}

func TestNotifyCatalogueNamesResolve(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}

	for _, name := range []string{"notifications", "notifications-off", "close", "schedule", "ghost"} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("%q is missing from the catalogue", name)
		}
		face := m.Face(r, FaceRequest{})
		if face == nil {
			t.Fatalf("%q resolved to no face", name)
		}
		if face == m.Primary() {
			t.Fatalf("%q resolved to the primary text face, not the icon face", name)
		}
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

// Metric SVGs are square 24px viewBoxes. A 900-unit advance on a 1200-unit
// outline clips the sides and the glyph reads as a stretched slab next to "8%".
func TestMetricIconRasterIsSquareAtBarSize(t *testing.T) {
	tr := NewTextRenderer(newIconFace())
	mask, err := tr.Raster(string(iconCPU), TextSpec{Size: 14, Weight: 400}, false)
	if err != nil {
		t.Fatal(err)
	}
	w, h := mask.Advance, mask.Alpha.Bounds().Dy()
	if abs(w-h) > 2 {
		t.Fatalf("cpu icon at bar size is %dx%d, want a square (Noctalia glyphs are a square of baseGlyphSize)", w, h)
	}
	cols := make([]int, w)
	a := mask.Alpha
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if a.Pix[y*a.Stride+x] > 32 {
				cols[x]++
			}
		}
	}
	empty := 0
	for _, c := range cols {
		if c == 0 {
			empty++
		}
	}
	if empty > w/4 {
		t.Fatalf("cpu icon has %d empty columns of %d; the glyph is a sparse slab", empty, w)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// The night variants extend the weather set: a clear or partly-cloudy sky at
// night shows the moon, and every other category reads the same by night.
func TestWeatherIconSelectsNightVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code  int
		isDay bool
		want  rune
	}{
		{0, true, iconClearDay},
		{0, false, iconClearNight},
		{1, true, iconPartlyCloudy},
		{1, false, iconPartlyCloudyNight},
		{2, false, iconPartlyCloudyNight},
		{3, false, iconCloud},
		{45, false, iconFog},
		{61, false, iconRain},
		{71, false, iconSnow},
		{95, false, iconThunderstorm},
	}
	for _, tc := range cases {
		if got := WeatherIcon(tc.code, tc.isDay); got != tc.want {
			t.Fatalf("WeatherIcon(%d, %v) = %U, want %U", tc.code, tc.isDay, got, tc.want)
		}
	}
}

func TestWeatherIconNameSelectsNightVariants(t *testing.T) {
	t.Parallel()
	if got := WeatherIconName(0, false); got != "clear-night" {
		t.Fatalf("night clear = %q, want clear-night", got)
	}
	if got := WeatherIconName(2, false); got != "partly-cloudy-night" {
		t.Fatalf("night partly cloudy = %q, want partly-cloudy-night", got)
	}
	for _, name := range []string{"clear-night", "partly-cloudy-night"} {
		if _, ok := IconByName(name); !ok {
			t.Fatalf("the catalogue does not carry %q", name)
		}
	}
}

func TestWeatherConditionNamesTheWMOCategories(t *testing.T) {
	t.Parallel()
	cases := map[int]string{
		0:   "Clear",
		1:   "Partly cloudy",
		2:   "Partly cloudy",
		3:   "Cloudy",
		45:  "Fog",
		48:  "Fog",
		51:  "Rain",
		61:  "Rain",
		80:  "Rain",
		71:  "Snow",
		75:  "Heavy snow",
		86:  "Heavy snow",
		95:  "Thunderstorm",
		99:  "Thunderstorm",
		100: "Thunderstorm", // above the documented domain; follows the glyph
		44:  "Cloudy",       // the unmapped range falls back to the cloud
	}
	for code, want := range cases {
		if got := WeatherCondition(code); got != want {
			t.Fatalf("WeatherCondition(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestNightGlyphsCarryInk(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"clear-night", "partly-cloudy-night"} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("the catalogue does not carry %q", name)
		}
		if got := glyphCoverage(t, r, 32); got == 0 {
			t.Fatalf("%s glyph %U has no ink", name, r)
		}
	}
}

func TestWeatherDetailGlyphsCarryInk(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"thermometer", "wind", "humidity", "sunrise", "sunset", "elevation"} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("the catalogue does not carry %q", name)
		}
		if got := glyphCoverage(t, r, 32); got == 0 {
			t.Fatalf("%s glyph %U has no ink", name, r)
		}
	}
}

func TestBatteryAndNetworkGlyphsResolveForPlugins(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"battery-0", "battery-1", "battery-2", "battery-3",
		"battery-4", "battery-5", "battery-6",
		"battery-charging-0", "battery-charging-1", "battery-charging-2",
		"battery-charging-3", "battery-charging-4", "battery-charging-5",
		"battery-charging-6", "battery-critical", "network",
	} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("the catalogue does not carry %q", name)
		}
		if got := glyphCoverage(t, r, 32); got == 0 {
			t.Fatalf("%s glyph %U has no ink", name, r)
		}
	}
}

func TestDeviceGlyphsCarryInk(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"smartphone", "phonelink-off", "tablet", "laptop", "desktop-windows",
		"tv", "devices", "phone-in-talk", "folder-open", "content-paste",
		"share", "sms", "notifications-active", "refresh",
	} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("the catalogue does not carry %q", name)
		}
		if got := glyphCoverage(t, r, 32); got == 0 {
			t.Fatalf("%s glyph %U has no ink", name, r)
		}
	}
}

func TestCellularGlyphsCarryInk(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"5g", "4g-mobiledata", "3g-mobiledata", "g-mobiledata",
		"signal-cellular-null", "signal-cellular-1-bar",
		"signal-cellular-2-bar", "signal-cellular-3-bar", "signal-cellular-4-bar",
	} {
		r, ok := IconByName(name)
		if !ok {
			t.Fatalf("the catalogue does not carry %q", name)
		}
		if got := glyphCoverage(t, r, 32); got == 0 {
			t.Fatalf("%s glyph %U has no ink", name, r)
		}
	}
}

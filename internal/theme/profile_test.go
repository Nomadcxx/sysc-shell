package theme

import (
	"slices"
	"testing"
	"time"
)

func TestPresetTablesMatchTheDesign(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		preset Preset
		want   Composition
	}{
		{PresetStandard, Composition{
			Density: DensityDefault, Radius: 12, InputRadius: 12,
			Motion: MotionStandard, MotionSpeed: 100,
			BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
			BlurRadius: 24,
			Elevation:  ElevationSubtle,
			FontFamily: DefaultFontFamily, MonoFontFamily: DefaultMonoFamily,
			FontScale: 100, FontWeight: 400,
		}},
		{PresetCompact, Composition{
			Density: DensityCompact, Radius: 8, InputRadius: 8,
			Motion: MotionStandard, MotionSpeed: 125,
			BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
			BlurRadius: 24,
			Elevation:  ElevationSubtle,
			FontFamily: DefaultFontFamily, MonoFontFamily: DefaultMonoFamily,
			FontScale: 100, FontWeight: 400,
		}},
		{PresetExpressive, Composition{
			Density: DensityDefault, Radius: 16, InputRadius: 16,
			Motion: MotionExpressive, MotionSpeed: 100,
			BarOpacity: 100, PanelOpacity: 95, OverlayOpacity: 95,
			BlurRadius: 24,
			Elevation:  ElevationStandard,
			FontFamily: DefaultFontFamily, MonoFontFamily: DefaultMonoFamily,
			FontScale: 100, FontWeight: 400,
		}},
	} {
		got, ok := PresetComposition(tc.preset)
		if !ok {
			t.Errorf("%s is not a preset", tc.preset)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.preset, got, tc.want)
		}
		if err := got.Valid(); err != nil {
			t.Errorf("%s does not validate: %v", tc.preset, err)
		}
	}
	if _, ok := PresetComposition("nonsense"); ok {
		t.Error("an unknown preset resolved")
	}
	if len(Presets()) != 3 {
		t.Errorf("Presets() = %v, want three", Presets())
	}
}

func TestProfileDensityTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		density Density
		want    Metrics
	}{
		{DensityMini, Metrics{
			BarHeight: 21, CapsuleHeight: 19, BarPadding: 1, BarSpacing: 1,
			BaseWidget: 22,
			IconButton: 23, Checkbox: 15, ToggleBase: 18, SliderKnob: 14,
			InputHeight: 24, TabHeight: 22,
			CompactControl: 22, StandardControl: 24,
			PanelPadding: 13, CardPadding: 9, CardGap: 9,
			CapsulePadding: 2, ButtonPadding: 4,
			IconSmall: 14, IconNormal: 16, IconLarge: 20,
			IconHero:    56,
			IconProfile: 16,
		}},
		{DensityCompact, Metrics{
			BarHeight: 25, CapsuleHeight: 21, BarPadding: 2, BarSpacing: 2,
			BaseWidget: 27,
			IconButton: 27, Checkbox: 19, ToggleBase: 22, SliderKnob: 18,
			InputHeight: 30, TabHeight: 26,
			CompactControl: 26, StandardControl: 30,
			PanelPadding: 13, CardPadding: 9, CardGap: 9,
			CapsulePadding: 4, ButtonPadding: 6,
			IconSmall: 16, IconNormal: 18, IconLarge: 24,
			IconHero:    64,
			IconProfile: 18,
		}},
		{DensityDefault, Metrics{
			BarHeight: 31, CapsuleHeight: 25, BarPadding: 2, BarSpacing: 4,
			BaseWidget: 33,
			IconButton: 33, Checkbox: 23, ToggleBase: 26, SliderKnob: 22,
			InputHeight: 36, TabHeight: 32,
			CompactControl: 32, StandardControl: 36,
			PanelPadding: 13, CardPadding: 9, CardGap: 9,
			CapsulePadding: 6, ButtonPadding: 9,
			IconSmall: 16, IconNormal: 20, IconLarge: 24,
			IconHero:    80,
			IconProfile: 18,
		}},
		{DensityComfortable, Metrics{
			BarHeight: 37, CapsuleHeight: 29, BarPadding: 4, BarSpacing: 6,
			BaseWidget: 39,
			IconButton: 39, Checkbox: 27, ToggleBase: 30, SliderKnob: 26,
			InputHeight: 42, TabHeight: 38,
			CompactControl: 38, StandardControl: 42,
			PanelPadding: 13, CardPadding: 9, CardGap: 9,
			CapsulePadding: 9, ButtonPadding: 13,
			IconSmall: 18, IconNormal: 22, IconLarge: 28,
			IconHero:    92,
			IconProfile: 20,
		}},
		{DensitySpacious, Metrics{
			BarHeight: 47, CapsuleHeight: 31, BarPadding: 6, BarSpacing: 9,
			BaseWidget: 50,
			IconButton: 51, Checkbox: 35, ToggleBase: 40, SliderKnob: 34,
			InputHeight: 54, TabHeight: 50,
			CompactControl: 50, StandardControl: 54,
			PanelPadding: 13, CardPadding: 9, CardGap: 9,
			CapsulePadding: 13, ButtonPadding: 18,
			IconSmall: 20, IconNormal: 24, IconLarge: 32,
			IconHero:    104,
			IconProfile: 22,
		}},
	} {
		got, ok := MetricsFor(tc.density)
		if !ok {
			t.Errorf("%s is not a density", tc.density)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.density, got, tc.want)
		}
	}
	if _, ok := MetricsFor("dense"); ok {
		t.Error("an unknown density resolved")
	}
	// The default row is the shipped bar, and it moved with the re-base: 48 to
	// 31, with the inset shrinking to fit a 25 px pill in the narrower band.
	// That is the intended parity change rather than drift. A file that never
	// set a height follows the new default; one that set a height keeps it,
	// which is what the migration relies on.
	std, _ := MetricsFor(DensityDefault)
	if std.BarHeight != 31 || std.BarPadding != 2 || std.BarSpacing != 4 {
		t.Errorf("default row drifted from the shipped bar: %+v", std)
	}
}

func TestProfileTypeRoles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		role       TextRole
		size       int
		weight     int
		mono       bool
		roleString string
	}{
		// Sizes are the reference ladder in logical pixels; points convert at
		// four thirds, so the pt source is given beside each rung.
		{RoleCaption, 12, 400, false, "caption"},   // 9 pt
		{RoleLabel, 15, 500, false, "label"},       // 11 pt -> 14.67
		{RoleBody, 15, 400, false, "body"},         // 11 pt
		{RoleTitle, 17, 600, false, "title"},       // 13 pt -> 17.33
		{RoleHeadline, 21, 600, false, "headline"}, // 16 pt -> 21.33
		{RoleDisplay, 24, 600, false, "display"},   // 18 pt
		{RoleMono, 13, 400, true, "mono"},          // 10 pt -> 13.33
	} {
		got := TypeFor(tc.role)
		if got.Size != tc.size || got.Weight != tc.weight || got.Mono != tc.mono {
			t.Errorf("%s = %+v, want size %d weight %d mono %v",
				tc.roleString, got, tc.size, tc.weight, tc.mono)
		}
		if tc.role.String() != tc.roleString {
			t.Errorf("String() = %q, want %q", tc.role.String(), tc.roleString)
		}
	}
	// The zero value must be body text: an unset node still has to measure.
	var unset TextRole
	if unset != RoleBody {
		t.Errorf("the zero TextRole is %v, want body", unset)
	}
}

func TestProfileTextSizeScales(t *testing.T) {
	t.Parallel()
	c, _ := PresetComposition(PresetStandard)
	if got := c.TextSize(RoleBody); got != 15 {
		t.Errorf("body at 100%% = %d, want 15", got)
	}
	for _, tc := range []struct {
		scale int
		body  int
	}{
		{75, 11}, // 11.25 truncates after the half-up rounding term
		{100, 15},
		{150, 23}, // 22.5 rounds up
		{200, 30},
	} {
		c.FontScale = tc.scale
		if got := c.TextSize(RoleBody); got != tc.body {
			t.Errorf("body at %d%% = %d, want %d", tc.scale, got, tc.body)
		}
	}
	// Out-of-range scales clamp rather than producing a zero-height line.
	c.FontScale = 0
	if got := c.TextSize(RoleBody); got != 11 {
		t.Errorf("clamped low scale = %d, want the 75%% size 11", got)
	}
	c.FontScale = 10000
	if got := c.TextSize(RoleBody); got != 30 {
		t.Errorf("clamped high scale = %d, want the 200%% size 30", got)
	}
}

func TestProfileTextWeightShiftsTheWholeRamp(t *testing.T) {
	t.Parallel()
	c, _ := PresetComposition(PresetStandard)
	if got := c.TextWeight(RoleTitle); got != 600 {
		t.Errorf("title at the default weight = %d, want 600", got)
	}
	c.FontWeight = 500
	if got := c.TextWeight(RoleBody); got != 500 {
		t.Errorf("body = %d, want the configured 500", got)
	}
	// A heavier body must keep titles heavier than body text, not flatten
	// the ramp onto one weight.
	if got := c.TextWeight(RoleTitle); got != 700 {
		t.Errorf("title = %d, want 700", got)
	}
	c.FontWeight = 900
	if got := c.TextWeight(RoleTitle); got != FontWeightMax {
		t.Errorf("title = %d, want the %d ceiling", got, FontWeightMax)
	}
}

func TestProfileFamilyFollowsTheRole(t *testing.T) {
	t.Parallel()
	c, _ := PresetComposition(PresetStandard)
	if got := c.Family(RoleBody); got != DefaultFontFamily {
		t.Errorf("body family = %q, want %q", got, DefaultFontFamily)
	}
	if got := c.Family(RoleMono); got != DefaultMonoFamily {
		t.Errorf("mono family = %q, want %q", got, DefaultMonoFamily)
	}
}

func TestProfileMotionSpeedDividesDurations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		speed  int
		medium time.Duration
	}{
		{25, 1200 * time.Millisecond},
		{100, 300 * time.Millisecond},
		{125, 240 * time.Millisecond},
		{400, 75 * time.Millisecond},
	} {
		got := BaseMotion.AtSpeed(tc.speed)
		if got.Medium != tc.medium {
			t.Errorf("medium at %d%% = %v, want %v", tc.speed, got.Medium, tc.medium)
		}
		if got.Instant != 0 {
			t.Errorf("instant at %d%% = %v, want 0", tc.speed, got.Instant)
		}
	}
	// Speed is bounded, so a wild value cannot stall or erase a transition.
	if got := BaseMotion.AtSpeed(1).Medium; got != BaseMotion.AtSpeed(SpeedMin).Medium {
		t.Errorf("a below-range speed did not clamp: %v", got)
	}
	if got := BaseMotion.AtSpeed(99999).Medium; got != BaseMotion.AtSpeed(SpeedMax).Medium {
		t.Errorf("an above-range speed did not clamp: %v", got)
	}
}

func TestProfileSpatialCurveFollowsMotionStyle(t *testing.T) {
	t.Parallel()
	if got := MotionStandard.SpatialCurve(); got != CurveOutCubic {
		t.Errorf("standard curve = %q, want out-cubic", got)
	}
	if got := MotionExpressive.SpatialCurve(); got != CurveOutQuart {
		t.Errorf("expressive curve = %q, want out-quart", got)
	}
}

func TestRebaseMovesUntouchedAxesAndKeepsDeviations(t *testing.T) {
	t.Parallel()
	std, _ := PresetComposition(PresetStandard)
	exp, _ := PresetComposition(PresetExpressive)

	// Nothing touched: every axis follows the new preset.
	if got := Rebase(std, std, exp); got != exp {
		t.Errorf("an untouched composition did not follow the preset:\n got %+v\nwant %+v", got, exp)
	}

	// One deviation survives; the rest still move.
	current := std
	current.Density = DensityComfortable
	current.FontScale = 125
	got := Rebase(current, std, exp)
	if got.Density != DensityComfortable {
		t.Errorf("density = %q, want the user's comfortable to survive", got.Density)
	}
	if got.FontScale != 125 {
		t.Errorf("font scale = %d, want the user's 125 to survive", got.FontScale)
	}
	if got.Radius != exp.Radius {
		t.Errorf("radius = %d, want the new preset's %d", got.Radius, exp.Radius)
	}
	if got.Motion != exp.Motion {
		t.Errorf("motion = %q, want the new preset's %q", got.Motion, exp.Motion)
	}
	if got.PanelOpacity != exp.PanelOpacity {
		t.Errorf("panel opacity = %d, want the new preset's %d", got.PanelOpacity, exp.PanelOpacity)
	}
}

func TestRebaseKeepsAValueThatMatchesTheNewPresetAnyway(t *testing.T) {
	t.Parallel()
	std, _ := PresetComposition(PresetStandard)
	cmp, _ := PresetComposition(PresetCompact)
	// A user who set radius to the compact value while on standard is a
	// deviation from standard, so it is preserved -- and it happens to equal
	// the incoming preset, which must not change the outcome.
	current := std
	current.Radius = cmp.Radius
	if got := Rebase(current, std, cmp); got.Radius != cmp.Radius {
		t.Errorf("radius = %d, want %d", got.Radius, cmp.Radius)
	}
}

func TestProfileValidRejectsEveryOutOfRangeAxis(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*Composition)
		want   string
	}{
		{"density", func(c *Composition) { c.Density = "dense" }, "density"},
		{"motion", func(c *Composition) { c.Motion = "springy" }, "motion"},
		{"elevation", func(c *Composition) { c.Elevation = "high" }, "elevation"},
		{"font scale low", func(c *Composition) { c.FontScale = 74 }, "font-scale"},
		{"font scale high", func(c *Composition) { c.FontScale = 201 }, "font-scale"},
		{"font weight low", func(c *Composition) { c.FontWeight = 99 }, "font-weight"},
		{"font weight high", func(c *Composition) { c.FontWeight = 901 }, "font-weight"},
		{"radius low", func(c *Composition) { c.Radius = -1 }, "radius"},
		{"radius high", func(c *Composition) { c.Radius = 33 }, "radius"},
		{"speed low", func(c *Composition) { c.MotionSpeed = 24 }, "motion-speed"},
		{"speed high", func(c *Composition) { c.MotionSpeed = 401 }, "motion-speed"},
		{"bar opacity", func(c *Composition) { c.BarOpacity = 79 }, "bar-opacity"},
		{"panel opacity", func(c *Composition) { c.PanelOpacity = 101 }, "panel-opacity"},
		{"overlay opacity", func(c *Composition) { c.OverlayOpacity = 0 }, "overlay-opacity"},
		{"empty family", func(c *Composition) { c.FontFamily = "" }, "font-family"},
		{"empty mono family", func(c *Composition) { c.MonoFontFamily = "" }, "mono-font-family"},
	} {
		c, _ := PresetComposition(PresetStandard)
		tc.mutate(&c)
		err := c.Valid()
		if err == nil {
			t.Errorf("%s: Valid() = nil, want an error", tc.name)
			continue
		}
		if !contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not name %q", tc.name, err, tc.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestMetricsCarryCapsuleAndButtonPadding pulls the last two fixed visual
// constants into the density table. They lived in the shell's flat-alias layer
// as literal 8 and 12, which meant a compact bar drew standard-sized padding
// inside its capsules -- density moved the pill but not what sat in it.
//
// The standard row must keep the old literals so the default theme does not
// shift, and every value has to land on the shared spacing scale rather than
// being multiplied out of the row above it.
func TestMetricsCarryCapsuleAndButtonPadding(t *testing.T) {
	t.Parallel()
	std, ok := MetricsFor(DensityDefault)
	if !ok {
		t.Fatal("no default row")
	}
	if std.CapsulePadding != 6 {
		t.Errorf("default capsule padding = %d, want the re-based 6", std.CapsulePadding)
	}
	if std.ButtonPadding != 9 {
		t.Errorf("default button padding = %d, want the re-based 9", std.ButtonPadding)
	}

	// Every row's insets are rungs of the shared ladder and grow down the
	// table. Card and panel padding deliberately do not: the reference draws
	// those per surface rather than scaling them per density.
	onScale := func(v int) bool { return slices.Contains(SpacingScale, v) }
	var last Metrics
	for i, d := range Densities() {
		m, ok := MetricsFor(d)
		if !ok {
			t.Fatalf("no %s row", d)
		}
		if !onScale(m.CapsulePadding) || !onScale(m.ButtonPadding) {
			t.Errorf("%s padding %d/%d is off the spacing scale %v",
				d, m.CapsulePadding, m.ButtonPadding, SpacingScale)
		}
		if i > 0 {
			if m.CapsulePadding <= last.CapsulePadding || m.ButtonPadding <= last.ButtonPadding {
				t.Errorf("%s padding %d/%d does not grow on the row above (%d/%d)",
					d, m.CapsulePadding, m.ButtonPadding, last.CapsulePadding, last.ButtonPadding)
			}
		}
		last = m
	}
}

func TestSpacingLadderMatchesTheReference(t *testing.T) {
	t.Parallel()
	// v4 Commons/Style.qml: marginXXXS..marginXL, logical px at scale 1.
	want := []int{1, 2, 4, 6, 9, 13, 18}
	if len(SpacingScale) != len(want) {
		t.Fatalf("ladder has %d rungs, want %d", len(SpacingScale), len(want))
	}
	for i, v := range want {
		if SpacingScale[i] != v {
			t.Errorf("rung %d = %d, want %d", i, SpacingScale[i], v)
		}
	}
}

func TestBarHeightsAreOddAtEveryDensity(t *testing.T) {
	t.Parallel()
	// An odd band has a true centre row, so a centred glyph lands on a pixel
	// instead of straddling two.
	for _, d := range Densities() {
		m, ok := MetricsFor(d)
		if !ok {
			t.Fatalf("no row for %v", d)
		}
		if m.BarHeight%2 == 0 {
			t.Errorf("%v bar height %d is even", d, m.BarHeight)
		}
		if m.CapsuleHeight >= m.BarHeight {
			t.Errorf("%v capsule %d is not smaller than the bar %d", d, m.CapsuleHeight, m.BarHeight)
		}
		if m.CapsuleHeight%2 == 0 {
			t.Errorf("%v capsule height %d is even", d, m.CapsuleHeight)
		}
		// The capsule also has to fit between the bar's own insets, or the pill
		// is taller than the band that holds it.
		if room := m.BarHeight - 2*m.BarPadding; m.CapsuleHeight > room {
			t.Errorf("%v capsule %d does not fit in %d of content (bar %d less padding %d twice)",
				d, m.CapsuleHeight, room, m.BarHeight, m.BarPadding)
		}
	}
}

func TestDensityRowsMatchTheReference(t *testing.T) {
	t.Parallel()
	want := map[Density]int{
		DensityMini: 21, DensityCompact: 25, DensityDefault: 31,
		DensityComfortable: 37, DensitySpacious: 47,
	}
	for d, h := range want {
		m, ok := MetricsFor(d)
		if !ok {
			t.Errorf("%v is not a density", d)
			continue
		}
		if m.BarHeight != h {
			t.Errorf("%v bar height = %d, want %d", d, m.BarHeight, h)
		}
	}
	if len(Densities()) != len(want) {
		t.Errorf("Densities() lists %d rows, want %d", len(Densities()), len(want))
	}
}

// TestLegacyDensityNameStillResolves keeps existing configuration loading. The
// wire value is the constant, so renaming the row would otherwise reject every
// file that names the old one -- and the loader validates a density by looking
// it up here.
func TestLegacyDensityNameStillResolves(t *testing.T) {
	t.Parallel()
	legacy, ok := MetricsFor(DensityStandard)
	if !ok {
		t.Fatal("the legacy density name no longer resolves; existing files would be rejected")
	}
	current, _ := MetricsFor(DensityDefault)
	if legacy.BarHeight != 48 || legacy.BarPadding != 6 || legacy.BarSpacing != 4 {
		t.Errorf("legacy bar = %d/%d/%d, want 48/6/4",
			legacy.BarHeight, legacy.BarPadding, legacy.BarSpacing)
	}
	legacy.BarHeight = current.BarHeight
	legacy.BarPadding = current.BarPadding
	legacy.BarSpacing = current.BarSpacing
	if legacy != current {
		t.Errorf("legacy non-bar metrics = %+v, want default %+v", legacy, current)
	}
	// It resolves, but it is not offered: the settings list and error messages
	// name the five current rows.
	for _, d := range Densities() {
		if d == DensityStandard {
			t.Error("the legacy name is still listed as a choice")
		}
	}
}

func TestControlSizesDeriveFromTheBaseWidget(t *testing.T) {
	t.Parallel()
	// The reference has one master control dimension and expresses every
	// control as a ratio of it. A table of absolutes cannot stay in proportion
	// when the base moves, which is the whole reason to derive.
	m, _ := MetricsFor(DensityDefault)
	if m.BaseWidget != 33 {
		t.Fatalf("base widget = %d, want 33", m.BaseWidget)
	}
	for _, tc := range []struct {
		name  string
		got   int
		ratio float64
		odd   bool
	}{
		{"icon button", m.IconButton, 1.0, true},
		{"checkbox", m.Checkbox, 0.7, true},
		{"toggle base", m.ToggleBase, 0.8, false},
		{"slider knob", m.SliderKnob, 0.7, false},
		{"input height", m.InputHeight, 1.1, false},
		{"tab height", m.TabHeight, 1.0, false},
	} {
		want := int(float64(m.BaseWidget)*tc.ratio + 0.5)
		if tc.odd {
			want = ToOdd(want)
		} else {
			want = ToEven(want)
		}
		if tc.got != want {
			t.Errorf("%s = %d, want %d (base %d x %.2f)", tc.name, tc.got, want, m.BaseWidget, tc.ratio)
		}
	}
}

func TestOddAndEvenForcingIsPerShape(t *testing.T) {
	t.Parallel()
	// Icon buttons and checkboxes force odd so a centred glyph lands on a pixel
	// row. Toggles and sliders force even so the two-sided inset stays
	// symmetric. This is deliberate in the reference, not incidental.
	for _, d := range Densities() {
		m, _ := MetricsFor(d)
		for name, v := range map[string]int{"icon button": m.IconButton, "checkbox": m.Checkbox} {
			if v%2 == 0 {
				t.Errorf("%v %s = %d, want odd", d, name, v)
			}
		}
		for name, v := range map[string]int{"toggle base": m.ToggleBase, "slider knob": m.SliderKnob} {
			if v%2 != 0 {
				t.Errorf("%v %s = %d, want even", d, name, v)
			}
		}
	}
}

func TestControlsStayInProportionWhenTheBaseMoves(t *testing.T) {
	t.Parallel()
	// The property a table of absolutes cannot hold.
	small, _ := MetricsFor(DensityCompact)
	large, _ := MetricsFor(DensitySpacious)
	if !(small.IconButton < large.IconButton && small.InputHeight < large.InputHeight) {
		t.Error("control sizes did not track the base width across densities")
	}
	if small.BaseWidget >= large.BaseWidget {
		t.Errorf("base width %d at compact is not below %d at spacious", small.BaseWidget, large.BaseWidget)
	}
}

// TestTheControlAliasesCarryTheDerivedValues holds the migration honest. The
// two older names are kept only so call sites can move gradually; if they ever
// stopped tracking what they alias, a surface reading the old name would size
// itself from a stale absolute while its neighbour used the derived one.
func TestTheControlAliasesCarryTheDerivedValues(t *testing.T) {
	t.Parallel()
	for _, d := range Densities() {
		m, _ := MetricsFor(d)
		if m.CompactControl != m.TabHeight {
			t.Errorf("%v compact control = %d, want the derived tab height %d", d, m.CompactControl, m.TabHeight)
		}
		if m.StandardControl != m.InputHeight {
			t.Errorf("%v standard control = %d, want the derived input height %d", d, m.StandardControl, m.InputHeight)
		}
	}
}

// TestPaddingResolvesToLadderRungs carries component parity D3: padding is not
// a constant in the reference, it is a rung chosen per surface. Every density is
// walked rather than the default alone, because the claim these three fields
// make is that they do not vary by row, and a test that reads one row cannot
// see that claim break.
// TestNamedRungsMatchTheSpacingScale keeps the named rungs and the ladder one
// fact rather than two. Call sites read the names, while MetricsFor and the
// conformance gate read the slice, so a rung renamed or re-based in one place
// and not the other would leave surfaces asking for a spacing the ladder no
// longer contains -- and nothing else would notice.
func TestNamedRungsMatchTheSpacingScale(t *testing.T) {
	t.Parallel()
	named := []int{MarginXXXS, MarginXXS, MarginXS, MarginS, MarginM, MarginL, MarginXL}
	if !slices.Equal(named, SpacingScale) {
		t.Errorf("named rungs = %v, want the ladder %v", named, SpacingScale)
	}
}

func TestPaddingResolvesToLadderRungs(t *testing.T) {
	t.Parallel()
	// Sourced from nine cards and seven panels: every panel insets at marginL
	// with a margin2L height reserve; the inter-card gap is marginM everywhere
	// except the control centre; card interiors are marginM.
	for _, density := range Densities() {
		m, ok := MetricsFor(density)
		if !ok {
			t.Fatalf("%s is not a density", density)
		}
		for name, v := range map[string]int{
			"panel padding": m.PanelPadding,
			"card padding":  m.CardPadding,
			"card gap":      m.CardGap,
		} {
			if !slices.Contains(SpacingScale, v) {
				t.Errorf("%s at %s = %d, which is not a rung of %v",
					name, density, v, SpacingScale)
			}
		}
		if m.PanelPadding != 13 {
			t.Errorf("panel padding at %s = %d, want marginL 13", density, m.PanelPadding)
		}
		if m.CardPadding != 9 || m.CardGap != 9 {
			t.Errorf("card padding/gap at %s = %d/%d, want marginM 9 each",
				density, m.CardPadding, m.CardGap)
		}
	}
}

func TestMotionDurationsMatchTheReference(t *testing.T) {
	t.Parallel()
	// The reference runs calmer, most visibly at the long end: 750 ms against
	// the 400 this replaces. Every token is checked, not a sample of them: a
	// table test that ignores four of its six values passes with them wrong.
	want := MotionTokens{
		Instant: 0, Shorter: 75 * time.Millisecond, Short: 150 * time.Millisecond,
		Medium: 300 * time.Millisecond, Long: 450 * time.Millisecond,
		ExtraLong: 750 * time.Millisecond,
	}
	for _, tc := range []struct {
		name      string
		got, want time.Duration
	}{
		{"instant", BaseMotion.Instant, want.Instant},
		{"shorter", BaseMotion.Shorter, want.Shorter},
		{"short", BaseMotion.Short, want.Short},
		{"medium", BaseMotion.Medium, want.Medium},
		{"long", BaseMotion.Long, want.Long},
		{"extra long", BaseMotion.ExtraLong, want.ExtraLong},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestFrameCapStaysBelowTheShortestToken carries the smoothness design's
// constraint across this re-base: a cap at or above the shortest duration makes
// a short transition visibly steppy, because the surface would be allowed to
// paint only once while the value travels.
func TestFrameCapStaysBelowTheShortestToken(t *testing.T) {
	t.Parallel()
	if BaseMotion.FrameCap >= BaseMotion.Shorter {
		t.Errorf("frame cap %v is not below the shortest token %v",
			BaseMotion.FrameCap, BaseMotion.Shorter)
	}
}

// TestMetricsCarryTheProfileIcon removes the last derived icon constant. The
// shell's flat layer computed the profile icon as IconSmall+2, which is a
// fixed offset masquerading as a scale: it happened to be right at standard
// density and was never checked at the others.
//
// The row keeps the values that offset produced, so nothing moves; what
// changes is that the table now states them.
func TestMetricsCarryTheProfileIcon(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		density Density
		want    int
	}{
		{DensityMini, 16},
		{DensityCompact, 18},
		{DensityDefault, 18},
		{DensityComfortable, 20},
		{DensitySpacious, 22},
	} {
		m, ok := MetricsFor(tc.density)
		if !ok {
			t.Fatalf("no %s row", tc.density)
		}
		if m.IconProfile != tc.want {
			t.Errorf("%s profile icon = %d, want %d", tc.density, m.IconProfile, tc.want)
		}
		if m.IconProfile < m.IconSmall {
			t.Errorf("%s profile icon %d is below the small icon %d", tc.density, m.IconProfile, m.IconSmall)
		}
	}
}

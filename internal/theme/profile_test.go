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
			Density: DensityStandard, Radius: 12,
			Motion: MotionStandard, MotionSpeed: 100,
			BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
			Elevation:  ElevationSubtle,
			FontFamily: DefaultFontFamily, MonoFontFamily: DefaultMonoFamily,
			FontScale: 100, FontWeight: 400,
		}},
		{PresetCompact, Composition{
			Density: DensityCompact, Radius: 8,
			Motion: MotionStandard, MotionSpeed: 125,
			BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
			Elevation:  ElevationSubtle,
			FontFamily: DefaultFontFamily, MonoFontFamily: DefaultMonoFamily,
			FontScale: 100, FontWeight: 400,
		}},
		{PresetExpressive, Composition{
			Density: DensityStandard, Radius: 16,
			Motion: MotionExpressive, MotionSpeed: 100,
			BarOpacity: 100, PanelOpacity: 95, OverlayOpacity: 95,
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
		{DensityCompact, Metrics{
			BarHeight: 40, BarPadding: 4, BarSpacing: 2,
			CompactControl: 32, StandardControl: 36,
			PanelPadding: 12, CardPadding: 10,
			IconSmall: 16, IconNormal: 18, IconLarge: 24,
		}},
		{DensityStandard, Metrics{
			BarHeight: 48, BarPadding: 6, BarSpacing: 4,
			CompactControl: 32, StandardControl: 40,
			PanelPadding: 16, CardPadding: 12,
			IconSmall: 16, IconNormal: 20, IconLarge: 24,
		}},
		{DensityComfortable, Metrics{
			BarHeight: 56, BarPadding: 8, BarSpacing: 6,
			CompactControl: 36, StandardControl: 44,
			PanelPadding: 20, CardPadding: 16,
			IconSmall: 18, IconNormal: 22, IconLarge: 28,
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
	// The standard row has to reproduce the shipped bar exactly, or an
	// existing file changes size the moment it is reloaded.
	std, _ := MetricsFor(DensityStandard)
	if std.BarHeight != 48 || std.BarPadding != 6 || std.BarSpacing != 4 {
		t.Errorf("standard row drifted from the shipped bar: %+v", std)
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
		{RoleCaption, 12, 400, false, "caption"},
		{RoleLabel, 14, 500, false, "label"},
		{RoleBody, 14, 400, false, "body"},
		{RoleTitle, 16, 600, false, "title"},
		{RoleHeadline, 20, 600, false, "headline"},
		{RoleMono, 13, 400, true, "mono"},
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
	if got := c.TextSize(RoleBody); got != 14 {
		t.Errorf("body at 100%% = %d, want 14", got)
	}
	for _, tc := range []struct {
		scale int
		body  int
	}{
		{75, 11}, // 10.5 rounds up
		{100, 14},
		{150, 21},
		{200, 28},
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
	if got := c.TextSize(RoleBody); got != 28 {
		t.Errorf("clamped high scale = %d, want the 200%% size 28", got)
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
		{25, 720 * time.Millisecond},
		{100, 180 * time.Millisecond},
		{125, 144 * time.Millisecond},
		{400, 45 * time.Millisecond},
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
	std, ok := MetricsFor(DensityStandard)
	if !ok {
		t.Fatal("no standard row")
	}
	if std.CapsulePadding != 8 {
		t.Errorf("standard capsule padding = %d, want the shipped 8", std.CapsulePadding)
	}
	if std.ButtonPadding != 12 {
		t.Errorf("standard button padding = %d, want the shipped 12", std.ButtonPadding)
	}

	onScale := func(v int) bool { return slices.Contains(SpacingScale, v) }
	var last Metrics
	for i, d := range []Density{DensityCompact, DensityStandard, DensityComfortable} {
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
		{DensityCompact, 18},
		{DensityStandard, 18},
		{DensityComfortable, 20},
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

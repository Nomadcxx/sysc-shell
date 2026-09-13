package theme

import (
	"fmt"
	"time"
)

// This file owns the finite composition tables: density metrics, type roles,
// motion durations, and the three bundled presets. It imports no
// configuration or shell package, so the tables can be read by the loader,
// the settings registry, and the resolver without any of them depending on
// each other.

// Density selects one row of the metric table. It is not a multiplier: a
// scale factor applied to arbitrary component geometry produces half-pixel
// controls and text that clips, so the rows are enumerated instead.
type Density string

const (
	DensityMini        Density = "mini"
	DensityCompact     Density = "compact"
	DensityDefault     Density = "default"
	DensityComfortable Density = "comfortable"
	DensitySpacious    Density = "spacious"
	// DensityStandard is the name this row carried before it was re-based onto
	// the reference. The constant is the wire value, so dropping it would
	// reject every configuration file that names it. MetricsFor folds it onto
	// DensityDefault; Densities does not offer it, and the settings list and
	// the loader's error message name only the five current rows.
	DensityStandard Density = "standard"
)

// MotionStyle selects the easing family. Expressive changes the curve for
// spatial recipes; it does not add springs or overshoot.
type MotionStyle string

const (
	MotionStandard   MotionStyle = "standard"
	MotionExpressive MotionStyle = "expressive"
)

// Elevation selects how much of the shadow renderer a floating surface uses.
type Elevation string

const (
	ElevationNone     Elevation = "none"
	ElevationSubtle   Elevation = "subtle"
	ElevationStandard Elevation = "standard"
)

// Preset names a bundled composition. A preset only supplies defaults; every
// axis stays independently overridable.
type Preset string

const (
	PresetStandard   Preset = "standard"
	PresetCompact    Preset = "compact"
	PresetExpressive Preset = "expressive"
)

// Bounds for the numeric axes, from design D3.
const (
	FontScaleMin  = 75
	FontScaleMax  = 200
	FontWeightMin = 100
	FontWeightMax = 900
	RadiusMin     = 0
	RadiusMax     = 32
	SpeedMin      = 25
	SpeedMax      = 400
	// Opacity stops at 80 because the shell has no portable compositor blur
	// behind text; below that, wallpaper detail reads through a label.
	OpacityMin = 80
	OpacityMax = 100
	// OpacityMinBlurred is the floor for a surface painting over a blurred
	// backdrop. The 80 above exists only because wallpaper detail reads through
	// a label, and a blurred ground removes that detail, so the limit becomes
	// taste rather than legibility. Panels only: the bar is docked and its text
	// sits directly over the wallpaper, which is a different problem.
	OpacityMinBlurred = 60
	// BlurRadius bounds the backdrop blur, in logical pixels at full
	// resolution. Kernel cost does not grow with radius -- the window slides --
	// so the ceiling is a matter of taste rather than budget.
	BlurRadiusMin = 0
	BlurRadiusMax = 64
)

// Metrics is one row of the density table from design D8.
//
// BarPadding and BarSpacing are part of the row rather than derived from the
// shared spacing scale: the standard row has to reproduce the shipped bar
// exactly, and its 6 px padding is not a step on that scale.
type Metrics struct {
	BarHeight int
	// CapsuleHeight is the pill inside the bar, a ratio of the band rather than
	// an independent constant, which is what keeps it proportional as density
	// moves. Derived as toOdd(round(BarHeight * r)) with r of 0.90, 0.85, 0.82,
	// 0.75 and 0.65 down the rows. Odd for the same reason the bar is: a shape
	// centred in an odd box lands on a pixel row instead of straddling two.
	CapsuleHeight   int
	BarPadding      int
	BarSpacing      int
	CompactControl  int
	StandardControl int
	PanelPadding    int
	CardPadding     int
	// CapsulePadding is the inset inside a bar pill; ButtonPadding is the
	// inset inside a control. Both were fixed constants in the shell's flat
	// alias layer, which left a compact bar drawing standard-sized padding
	// inside its capsules -- density moved the pill but not what sat in it.
	CapsulePadding int
	ButtonPadding  int
	IconSmall      int
	IconNormal     int
	IconLarge      int
	// IconProfile is the power-profile glyph in the session panel. It sits
	// between the small and normal icons; the shell derived it as IconSmall+2,
	// a fixed offset that was only ever checked at standard density.
	IconProfile int
}

// metrics is the density table, re-based onto the reference's five rows.
//
// Bar heights are the reference's own toOdd() results: 21, 25, 31, 37, 47. The
// capsule column is toOdd(round(bar * r)) with r of 0.90, 0.85, 0.82, 0.75 and
// 0.65, which puts the default row's pill at 25 -- the height measured off the
// reference capture.
//
// BarPadding shrinks with the band. A 31 px bar holding a 25 px pill has only
// 6 px to spend on both insets, so the old 6 px padding would leave the pill
// taller than the space it sits in.
//
// PanelPadding and CardPadding are ladder rungs and do not vary by density:
// the reference draws them per surface, marginL inside a panel and marginM
// inside a card, rather than scaling them per row.
var metrics = map[Density]Metrics{
	DensityMini: {
		BarHeight: 21, CapsuleHeight: 19, BarPadding: 1, BarSpacing: 1,
		CompactControl: 28, StandardControl: 32,
		PanelPadding: 13, CardPadding: 9,
		CapsulePadding: 2, ButtonPadding: 4,
		IconSmall: 14, IconNormal: 16, IconLarge: 20,
		IconProfile: 16,
	},
	DensityCompact: {
		BarHeight: 25, CapsuleHeight: 21, BarPadding: 2, BarSpacing: 2,
		CompactControl: 30, StandardControl: 36,
		PanelPadding: 13, CardPadding: 9,
		CapsulePadding: 4, ButtonPadding: 6,
		IconSmall: 16, IconNormal: 18, IconLarge: 24,
		IconProfile: 18,
	},
	DensityDefault: {
		BarHeight: 31, CapsuleHeight: 25, BarPadding: 2, BarSpacing: 4,
		CompactControl: 32, StandardControl: 40,
		PanelPadding: 13, CardPadding: 9,
		CapsulePadding: 6, ButtonPadding: 9,
		IconSmall: 16, IconNormal: 20, IconLarge: 24,
		IconProfile: 18,
	},
	DensityComfortable: {
		BarHeight: 37, CapsuleHeight: 29, BarPadding: 4, BarSpacing: 6,
		CompactControl: 36, StandardControl: 44,
		PanelPadding: 13, CardPadding: 9,
		CapsulePadding: 9, ButtonPadding: 13,
		IconSmall: 18, IconNormal: 22, IconLarge: 28,
		IconProfile: 20,
	},
	DensitySpacious: {
		BarHeight: 47, CapsuleHeight: 31, BarPadding: 6, BarSpacing: 9,
		CompactControl: 40, StandardControl: 48,
		PanelPadding: 13, CardPadding: 9,
		CapsulePadding: 13, ButtonPadding: 18,
		IconSmall: 20, IconNormal: 24, IconLarge: 32,
		IconProfile: 22,
	},
}

// MetricsFor returns the row for a density.
//
// The superseded name folds onto the row that replaced it, so a configuration
// file written against the old table still resolves. The loader validates a
// density by looking it up here, so the fold is what keeps such a file loading
// rather than being rejected outright.
func MetricsFor(d Density) (Metrics, bool) {
	if d == DensityStandard {
		d = DensityDefault
	}
	m, ok := metrics[d]
	return m, ok
}

// SpacingScale is the shared gap ladder. Semantic gaps pick a step; nothing
// multiplies a component dimension by an arbitrary factor.
//
// The rungs are the reference's margin ladder, marginXXXS through marginXL, in
// logical pixels at scale 1. They are deliberately not a rescaling of the
// previous {2, 4, 8, 12, 16, 24}: this ladder is denser in the middle, and that
// is what produces the reference's tighter grouping inside a card. Rounding the
// reference's geometry onto the old ladder would have put every padding one or
// two pixels out, compounding across nested containers.
var SpacingScale = []int{1, 2, 4, 6, 9, 13, 18}

// TextRole is the semantic type role a node asks for. Components name a role;
// they do not carry a point size.
type TextRole int

const (
	RoleBody TextRole = iota // the zero value, so an unset node measures as body text
	RoleCaption
	RoleLabel
	RoleTitle
	RoleHeadline
	RoleMono
	// RoleDisplay is the hero rung the ladder used to top out below: the
	// reference uses it for the calendar date header and similar treatments.
	// It is appended rather than inserted, so every role above keeps its iota
	// value, and textRoleCount in internal/render tracks it as the last role.
	RoleDisplay
)

// TypeSpec is one row of the type table from design D7, before font scaling.
type TypeSpec struct {
	Size   int
	Weight int
	Mono   bool
}

// typeRoles is the measured reference ladder. Its point sizes convert to
// logical pixels at four thirds, established by measuring the reference capture
// rather than assumed: 9 pt caption, 11 pt body and label, 13 pt title, 16 pt
// headline, 18 pt display, 10 pt mono.
//
// The visible consequence is that titles are now larger and no heavier: the
// reference reads lighter and larger than the ladder this replaces, and that
// single axis accounts for much of the tonal difference.
var typeRoles = map[TextRole]TypeSpec{
	RoleCaption:  {Size: 12, Weight: 400},
	RoleLabel:    {Size: 15, Weight: 500},
	RoleBody:     {Size: 15, Weight: 400},
	RoleTitle:    {Size: 17, Weight: 600},
	RoleHeadline: {Size: 21, Weight: 600},
	RoleDisplay:  {Size: 24, Weight: 600},
	RoleMono:     {Size: 13, Weight: 400, Mono: true},
}

// TypeFor returns the unscaled row for a role. An unknown role measures as
// body text rather than as nothing, because a frame still has to paint.
func TypeFor(role TextRole) TypeSpec {
	if spec, ok := typeRoles[role]; ok {
		return spec
	}
	return typeRoles[RoleBody]
}

func (r TextRole) String() string {
	switch r {
	case RoleCaption:
		return "caption"
	case RoleLabel:
		return "label"
	case RoleTitle:
		return "title"
	case RoleHeadline:
		return "headline"
	case RoleMono:
		return "mono"
	case RoleDisplay:
		return "display"
	default:
		return "body"
	}
}

// MotionTokens are the duration tokens from design D10 at 100 percent speed.
type MotionTokens struct {
	Instant   time.Duration
	Shorter   time.Duration
	Short     time.Duration
	Medium    time.Duration
	Long      time.Duration
	ExtraLong time.Duration
	// FrameCap is the shortest interval between repaints of an animating
	// surface. The ticker still runs at the frame cadence; this bounds how
	// often the surface is actually blitted, which is the expensive half. It
	// must stay below the shortest duration token, or a short transition
	// becomes visibly steppy.
	FrameCap time.Duration
}

// BaseMotion is the unscaled duration table.
var BaseMotion = MotionTokens{
	Instant:   0,
	Shorter:   80 * time.Millisecond,
	Short:     120 * time.Millisecond,
	Medium:    180 * time.Millisecond,
	Long:      250 * time.Millisecond,
	ExtraLong: 400 * time.Millisecond,
	FrameCap:  33 * time.Millisecond,
}

// AtSpeed divides every duration by the speed factor, so 400 percent is four
// times quicker and 25 percent is four times slower. The division happens once
// here rather than at each animation site, which is what keeps a recipe from
// scaling twice.
func (m MotionTokens) AtSpeed(percent int) MotionTokens {
	if percent < SpeedMin {
		percent = SpeedMin
	}
	if percent > SpeedMax {
		percent = SpeedMax
	}
	scale := func(d time.Duration) time.Duration {
		return d * 100 / time.Duration(percent)
	}
	return MotionTokens{
		Instant:   0,
		Shorter:   scale(m.Shorter),
		Short:     scale(m.Short),
		Medium:    scale(m.Medium),
		Long:      scale(m.Long),
		ExtraLong: scale(m.ExtraLong),
		// FrameCap is deliberately NOT scaled. It bounds how often a surface is
		// blitted, which is a cost of the machine rather than a property of the
		// animation, and measurement showed scaling defeats it at both ends: at
		// 400 percent it became 8.25 ms, below the 16 ms tick, and paced nothing
		// (62 publishes a second, identical to uncapped); at 25 percent it
		// became 132 ms, giving 7 a second, which is about two frames across a
		// 320 ms transition and visibly steppy. Held at one value it paces every
		// speed: roughly 30 a second.
		FrameCap: m.FrameCap,
	}
}

// Curve names an easing function. The set is closed: a theme picks from it and
// cannot supply an arbitrary curve.
type Curve string

const (
	CurveOutCubic Curve = "out-cubic"
	CurveOutQuart Curve = "out-quart"
)

// SpatialCurve is the easing a movement or size change uses. State and colour
// recipes stay on out-cubic in either style.
func (s MotionStyle) SpatialCurve() Curve {
	if s == MotionExpressive {
		return CurveOutQuart
	}
	return CurveOutCubic
}

// Composition is the resolved set of independent theme axes. It excludes
// palette source, seed, scheme, and mode, which theme-gen continues to own.
type Composition struct {
	Density        Density
	FontFamily     string
	MonoFontFamily string
	FontScale      int
	FontWeight     int
	Radius         int
	// InputRadius is the parallel ladder for interactive elements, scaled
	// independently of Radius. It is bounded like its sibling, and each preset
	// seeds it with that preset's Radius, so inputs keep their current shape
	// until someone sets the axis.
	InputRadius    int
	Motion         MotionStyle
	MotionSpeed    int
	BarOpacity     int
	PanelOpacity   int
	OverlayOpacity int
	// BlurBehind paints floating panels over a blurred capture of whatever sat
	// behind them when they opened, which is what allows PanelOpacity to fall
	// below OpacityMin. BlurRadius is that blur's radius.
	BlurBehind bool
	BlurRadius int
	Elevation  Elevation
}

// presets are the three bundled compositions from design D2.
//
// Compact reaches its shorter motion through the speed factor rather than a
// second duration table, so one table stays the source of every duration.
// Expressive makes the floating surfaces lightly translucent and leaves the
// bar opaque: the bar is docked, not floating, and text on it sits directly
// over the wallpaper.
var presets = map[Preset]Composition{
	PresetStandard: {
		Density:     DensityDefault,
		Radius:      12,
		InputRadius: 12,
		Motion:      MotionStandard, MotionSpeed: 100,
		BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
		BlurRadius: 24,
		Elevation:  ElevationSubtle,
	},
	PresetCompact: {
		Density:     DensityCompact,
		Radius:      8,
		InputRadius: 8,
		Motion:      MotionStandard, MotionSpeed: 125,
		BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
		BlurRadius: 24,
		Elevation:  ElevationSubtle,
	},
	PresetExpressive: {
		Density:     DensityDefault,
		Radius:      16,
		InputRadius: 16,
		Motion:      MotionExpressive, MotionSpeed: 100,
		BarOpacity: 100, PanelOpacity: 95, OverlayOpacity: 95,
		BlurRadius: 24,
		Elevation:  ElevationStandard,
	},
}

// Default font families, from design D7. Both resolve through the system
// scanner, which already chains a generic fallback, so an absent family
// degrades to the generic rather than failing a frame.
const (
	DefaultFontFamily = "Inter Variable"
	DefaultMonoFamily = "Fira Code"
)

// PresetComposition returns the bundled composition for a preset.
func PresetComposition(p Preset) (Composition, bool) {
	c, ok := presets[p]
	if !ok {
		return Composition{}, false
	}
	c.FontFamily = DefaultFontFamily
	c.MonoFontFamily = DefaultMonoFamily
	c.FontScale = 100
	c.FontWeight = 400
	return c, true
}

// Presets lists the bundled preset names in a stable order.
func Presets() []Preset {
	return []Preset{PresetStandard, PresetCompact, PresetExpressive}
}

// Densities, MotionStyles, and Elevations list each closed set in a stable
// order, for the settings registry and for error messages.
// Densities lists the rows a user may choose. The superseded name still
// resolves through MetricsFor, but it is deliberately not offered here.
func Densities() []Density {
	return []Density{DensityMini, DensityCompact, DensityDefault, DensityComfortable, DensitySpacious}
}

func MotionStyles() []MotionStyle {
	return []MotionStyle{MotionStandard, MotionExpressive}
}

func Elevations() []Elevation {
	return []Elevation{ElevationNone, ElevationSubtle, ElevationStandard}
}

// Rebase moves the axes that still sit on the old preset's defaults onto the
// new preset's, and leaves every deviation alone.
//
// This is what makes a preset a starting point rather than a reset: a user who
// chose comfortable density keeps it when switching to expressive, while the
// radius and motion they never touched follow the new preset.
func Rebase(current, from, to Composition) Composition {
	out := current
	rebaseString := func(cur, old, next string, dest *string) {
		if cur == old {
			*dest = next
		}
	}
	rebaseInt := func(cur, old, next int, dest *int) {
		if cur == old {
			*dest = next
		}
	}
	if current.Density == from.Density {
		out.Density = to.Density
	}
	if current.Motion == from.Motion {
		out.Motion = to.Motion
	}
	if current.Elevation == from.Elevation {
		out.Elevation = to.Elevation
	}
	rebaseString(current.FontFamily, from.FontFamily, to.FontFamily, &out.FontFamily)
	rebaseString(current.MonoFontFamily, from.MonoFontFamily, to.MonoFontFamily, &out.MonoFontFamily)
	rebaseInt(current.FontScale, from.FontScale, to.FontScale, &out.FontScale)
	rebaseInt(current.FontWeight, from.FontWeight, to.FontWeight, &out.FontWeight)
	rebaseInt(current.Radius, from.Radius, to.Radius, &out.Radius)
	rebaseInt(current.InputRadius, from.InputRadius, to.InputRadius, &out.InputRadius)
	rebaseInt(current.MotionSpeed, from.MotionSpeed, to.MotionSpeed, &out.MotionSpeed)
	rebaseInt(current.BarOpacity, from.BarOpacity, to.BarOpacity, &out.BarOpacity)
	rebaseInt(current.PanelOpacity, from.PanelOpacity, to.PanelOpacity, &out.PanelOpacity)
	rebaseInt(current.OverlayOpacity, from.OverlayOpacity, to.OverlayOpacity, &out.OverlayOpacity)
	return out
}

// Metrics returns the density row this composition selects.
func (c Composition) Metrics() Metrics {
	if m, ok := MetricsFor(c.Density); ok {
		return m
	}
	return metrics[DensityDefault]
}

// TextSize is the physical size for a role once font scaling applies.
// Rounding happens once, here, so measurement and paint cannot disagree.
func (c Composition) TextSize(role TextRole) int {
	scale := c.FontScale
	if scale < FontScaleMin {
		scale = FontScaleMin
	}
	if scale > FontScaleMax {
		scale = FontScaleMax
	}
	size := (TypeFor(role).Size*scale + 50) / 100
	if size < 1 {
		size = 1
	}
	return size
}

// TextWeight is the weight for a role. The configured weight shifts the whole
// ramp by the distance the theme moves the body weight, so a heavier setting
// keeps titles heavier than body text instead of flattening the ramp.
func (c Composition) TextWeight(role TextRole) int {
	delta := c.FontWeight - TypeFor(RoleBody).Weight
	w := TypeFor(role).Weight + delta
	if w < FontWeightMin {
		w = FontWeightMin
	}
	if w > FontWeightMax {
		w = FontWeightMax
	}
	return w
}

// Family is the font family a role resolves to.
func (c Composition) Family(role TextRole) string {
	if TypeFor(role).Mono {
		return c.MonoFontFamily
	}
	return c.FontFamily
}

// Motion durations for this composition, already divided by its speed.
func (c Composition) Durations() MotionTokens {
	return BaseMotion.AtSpeed(c.MotionSpeed)
}

// Valid reports whether every axis is in range. The loader adds the JSON path;
// this reports the axis and the bound it missed.
func (c Composition) Valid() error {
	if _, ok := MetricsFor(c.Density); !ok {
		return fmt.Errorf("density %q is not one of mini, compact, default, comfortable, spacious", c.Density)
	}
	if c.Motion != MotionStandard && c.Motion != MotionExpressive {
		return fmt.Errorf("motion %q is not one of standard, expressive", c.Motion)
	}
	switch c.Elevation {
	case ElevationNone, ElevationSubtle, ElevationStandard:
	default:
		return fmt.Errorf("elevation %q is not one of none, subtle, standard", c.Elevation)
	}
	for _, b := range []struct {
		name     string
		got      int
		min, max int
	}{
		{"font-scale", c.FontScale, FontScaleMin, FontScaleMax},
		{"font-weight", c.FontWeight, FontWeightMin, FontWeightMax},
		{"radius", c.Radius, RadiusMin, RadiusMax},
		{"input-radius", c.InputRadius, RadiusMin, RadiusMax},
		{"motion-speed", c.MotionSpeed, SpeedMin, SpeedMax},
		{"bar-opacity", c.BarOpacity, OpacityMin, OpacityMax},
		// Panels take the lower blurred floor here so the axis can be set at
		// all; opacityAlpha then clamps back to OpacityMin unless a backdrop is
		// actually present. Bounding it at 80 would make the blurred floor
		// unreachable, since the value would be rejected before anything asked
		// whether blur was on.
		{"panel-opacity", c.PanelOpacity, OpacityMinBlurred, OpacityMax},
		{"overlay-opacity", c.OverlayOpacity, OpacityMin, OpacityMax},
	} {
		if b.got < b.min || b.got > b.max {
			return fmt.Errorf("%s %d is outside %d..%d", b.name, b.got, b.min, b.max)
		}
	}
	if c.FontFamily == "" {
		return fmt.Errorf("font-family is empty")
	}
	if c.MonoFontFamily == "" {
		return fmt.Errorf("mono-font-family is empty")
	}
	return nil
}

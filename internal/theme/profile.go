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
	DensityCompact     Density = "compact"
	DensityStandard    Density = "standard"
	DensityComfortable Density = "comfortable"
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
)

// Metrics is one row of the density table from design D8.
//
// BarPadding and BarSpacing are part of the row rather than derived from the
// shared spacing scale: the standard row has to reproduce the shipped bar
// exactly, and its 6 px padding is not a step on that scale.
type Metrics struct {
	BarHeight       int
	BarPadding      int
	BarSpacing      int
	CompactControl  int
	StandardControl int
	PanelPadding    int
	CardPadding     int
	IconSmall       int
	IconNormal      int
	IconLarge       int
}

var metrics = map[Density]Metrics{
	DensityCompact: {
		BarHeight: 40, BarPadding: 4, BarSpacing: 2,
		CompactControl: 32, StandardControl: 36,
		PanelPadding: 12, CardPadding: 10,
		IconSmall: 16, IconNormal: 18, IconLarge: 24,
	},
	DensityStandard: {
		BarHeight: 48, BarPadding: 6, BarSpacing: 4,
		CompactControl: 32, StandardControl: 40,
		PanelPadding: 16, CardPadding: 12,
		IconSmall: 16, IconNormal: 20, IconLarge: 24,
	},
	DensityComfortable: {
		BarHeight: 56, BarPadding: 8, BarSpacing: 6,
		CompactControl: 36, StandardControl: 44,
		PanelPadding: 20, CardPadding: 16,
		IconSmall: 18, IconNormal: 22, IconLarge: 28,
	},
}

// MetricsFor returns the row for a density.
func MetricsFor(d Density) (Metrics, bool) {
	m, ok := metrics[d]
	return m, ok
}

// SpacingScale is the shared gap ladder. Semantic gaps pick a step; nothing
// multiplies a component dimension by an arbitrary factor.
var SpacingScale = []int{2, 4, 8, 12, 16, 24}

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
)

// TypeSpec is one row of the type table from design D7, before font scaling.
type TypeSpec struct {
	Size   int
	Weight int
	Mono   bool
}

var typeRoles = map[TextRole]TypeSpec{
	RoleCaption:  {Size: 12, Weight: 400},
	RoleLabel:    {Size: 14, Weight: 500},
	RoleBody:     {Size: 14, Weight: 400},
	RoleTitle:    {Size: 16, Weight: 600},
	RoleHeadline: {Size: 20, Weight: 600},
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
}

// BaseMotion is the unscaled duration table.
var BaseMotion = MotionTokens{
	Instant:   0,
	Shorter:   80 * time.Millisecond,
	Short:     120 * time.Millisecond,
	Medium:    180 * time.Millisecond,
	Long:      250 * time.Millisecond,
	ExtraLong: 400 * time.Millisecond,
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
	Motion         MotionStyle
	MotionSpeed    int
	BarOpacity     int
	PanelOpacity   int
	OverlayOpacity int
	Elevation      Elevation
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
		Density: DensityStandard,
		Radius:  12,
		Motion:  MotionStandard, MotionSpeed: 100,
		BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
		Elevation: ElevationSubtle,
	},
	PresetCompact: {
		Density: DensityCompact,
		Radius:  8,
		Motion:  MotionStandard, MotionSpeed: 125,
		BarOpacity: 100, PanelOpacity: 100, OverlayOpacity: 100,
		Elevation: ElevationSubtle,
	},
	PresetExpressive: {
		Density: DensityStandard,
		Radius:  16,
		Motion:  MotionExpressive, MotionSpeed: 100,
		BarOpacity: 100, PanelOpacity: 95, OverlayOpacity: 95,
		Elevation: ElevationStandard,
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
func Densities() []Density {
	return []Density{DensityCompact, DensityStandard, DensityComfortable}
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
	return metrics[DensityStandard]
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
		return fmt.Errorf("density %q is not one of compact, standard, comfortable", c.Density)
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
		{"motion-speed", c.MotionSpeed, SpeedMin, SpeedMax},
		{"bar-opacity", c.BarOpacity, OpacityMin, OpacityMax},
		{"panel-opacity", c.PanelOpacity, OpacityMin, OpacityMax},
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

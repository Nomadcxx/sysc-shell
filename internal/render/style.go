package render

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Style is the renderer-ready view of a resolved theme for one surface: the
// palette roles a node can ask for, the geometry that surface was measured
// at, and the semantic groups the paint recipes read.
//
// It never imports configuration or shell. A surface hands the renderer a
// Style; the renderer does not resolve one.
type Style struct {
	// Size is the logical font size; shaping happens at the physical size.
	Size int
	// Scale120 is the fractional render scale as a numerator over 120.
	Scale120 ui.Scale120
	// Body is the logical painted bar body inside the transparent layer
	// surface. Radius is its logical corner radius.
	Body   ui.Rect
	Radius int
	// AttachEdge is "top" or "bottom" when a panel sits on a bar. Those
	// corners stay square so the rounded body does not punch a wallpaper
	// seam against the bar.
	AttachEdge string

	Background Color
	Foreground Color
	// Capsule fills the pill that wraps a bar widget. Container fills a
	// workspace pill that is not focused. OnAccent and OnContainer are the
	// foregrounds their contents use.
	Capsule     Color
	Container   Color
	OnAccent    Color
	OnContainer Color
	Track       Color
	Accent      Color
	AccentOn    Color
	// Error paints text that reports a failure. It is a distinct field rather
	// than reusing AccentOn, which the bar already uses for a toggled control.
	Error Color
	// OnPrimary paints text on a Primary-filled button: the fill is the
	// Primary token, so its paired On token is the only legible label colour.
	OnPrimary Color
	// OnError is the paired foreground for an Error fill.
	OnError Color
	// ContainerHighest fills an idle control. Capsule above carries the high
	// container, which the bar's pills and the panels' cards share; a control
	// sitting on one of those needs the level above it to separate.
	ContainerHighest Color
	// Outline marks a meaningful control boundary and OutlineVariant is a
	// quieter divider. Both are set for every surface.
	Outline        Color
	OutlineVariant Color
	// Rim strokes the floating panel's own edge. It is deliberately separate
	// from Outline: every surface carries the outline token for its controls,
	// but only a panel draws a rim, so a bar or a toast leaves this zero.
	Rim Color
	// CardRadius is the corner a panel card keeps. Zero falls back to Radius,
	// which is what a bar pill uses.
	CardRadius int

	// Toggled swaps the accent used by the meter fill and the button.
	Toggled bool

	// Metrics is the density row this surface was measured at. Layout and
	// paint read the same row, so a control cannot be measured at one
	// density and painted at another.
	Metrics theme.Metrics
	// Shapes are the resolved corner radii. Stadium and circle geometry stay
	// independent of the base radius, so a zero radius still leaves a pill a
	// pill.
	Shapes Shapes
	// Type resolves a semantic text role to a family, physical size, and
	// weight.
	Type TypeSet
	// SurfaceOpacity is the alpha the root fill of this surface paints at,
	// from 0 to 255. Nested fills composite over the painted root rather
	// than inheriting it.
	SurfaceOpacity uint8
	// Elevation selects how much of the shadow renderer a floating surface
	// uses, and Shadow is the colour it casts.
	Elevation theme.Elevation
	Shadow    Color
	// Scrim dims what sits behind a modal surface.
	Scrim Color
	// Motion carries the duration tokens and the spatial curve in force.
	Motion MotionSet
}

// Shapes are the resolved corner radii for the shape roles in design D8.
type Shapes struct {
	Small  int
	Medium int
	Large  int
	Card   int
	Panel  int
}

// TypeSet resolves each semantic text role for one surface.
type TypeSet struct {
	Family     string
	MonoFamily string
	Roles      [textRoleCount]TextSpec
}

// TextSpec is one resolved role: the face to shape with and the size and
// weight to shape it at.
type TextSpec struct {
	Family string
	Size   int
	Weight int
	Italic bool
}

// textRoleCount bounds the role table. It tracks theme.RoleMono, the last
// role, so adding a role without extending the table fails to compile.
const textRoleCount = int(theme.RoleMono) + 1

// Spec returns the resolved spec for a role, falling back to body text for a
// role outside the table so a frame still paints.
func (t TypeSet) Spec(role theme.TextRole) TextSpec {
	if int(role) < 0 || int(role) >= textRoleCount {
		return t.Roles[theme.RoleBody]
	}
	return t.Roles[role]
}

// MotionSet is the duration table and curve a surface animates with.
type MotionSet struct {
	Durations theme.MotionTokens
	Spatial   theme.Curve
	// Reduced settles every spatial and state transition at once. Panel
	// visibility keeps a capped opacity-only fade, which the host applies.
	Reduced bool
}

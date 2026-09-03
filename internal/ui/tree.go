// Package ui holds the retained proof tree, its row layout, and hit testing.
// All coordinates are logical pixels; painting converts them to buffer pixels.
package ui

import "github.com/Nomadcxx/sysc-shell/internal/theme"

// Kind names the node types the proof tree supports.
type Kind uint8

const (
	KindRow Kind = iota
	KindText
	KindMeter
	KindButton
	KindGraph
	KindColumn
	KindSeparator
	KindTab
	KindToggle
	KindSlider
	KindMenu
	KindTextField
	KindScroll
	KindVirtualList
	KindImage

	// KindCapsule is a padded pill around one child, or an empty coloured dot
	// when it has no children and a Width. It is the bar's per-item chrome.
	KindCapsule
	// KindIcon is one named glyph from the shell's dedicated chrome icon face.
	KindIcon
	// KindSegmented owns equal-width, exclusive button segments.
	KindSegmented

	KindDragSource
	KindDropZone

	// kindCount is one past the last kind. It exists so a test can assert that
	// every declared kind is measurable, and it must stay last.
	kindCount
)

// Image is a decoded raster in premultiplied straight-alpha BGRA, the layout
// the shell's buffers use. Pix is never mutated after publication.
type Image struct {
	Width  int
	Height int
	Stride int
	Pix    []byte
}

// Rect is a logical-pixel rectangle.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the point lies inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Node is one retained element. Layout fills Bounds; every other field is
// supplied by the caller.
type Node struct {
	Kind Kind
	Text string
	// Icon names a glyph in the dedicated chrome icon inventory.
	Icon string
	// Key identifies a node across tree rebuilds so host-retained state -- an
	// editor buffer, a hover or press state, an in-flight transition -- follows
	// the node it belongs to. Action is the fallback for actionable nodes; see
	// StableKey.
	Key   string
	Value float64
	Min   float64
	Max   float64
	Step  float64
	// Preedit is composing text shown underlined; it is not committed.
	Preedit string
	// Cursor is a byte index into Text for KindTextField.
	Cursor int
	Width  int
	Height int
	// IconSize is the logical square reserved by KindIcon. Zero uses 20.
	IconSize int
	// Radius overrides the semantic radius for this node in logical pixels.
	// Zero defers to Shape, and a zero Shape defers to the surface's base.
	Radius int
	// Shape is the corner role this node asks for. It is the semantic form of
	// Radius: a component names the shape it is, and the theme decides how
	// round that is.
	Shape Shape
	// ScrollOffset is the viewport origin in logical pixels.
	ScrollOffset int
	ItemCount    int
	ItemHeight   int
	ContentH     int
	Item         func(int) *Node
	// Values are the graph's samples, oldest first, each already normalised to
	// zero through one by the widget. The node carries no scale of its own.
	Values []float64
	// MaxWidth caps a text node's measured width. Zero means unbounded. It
	// exists because a focused-window title is unbounded user text: without a
	// cap it would take a whole section's budget before anything truncated.
	MaxWidth int
	// MinWidthText floors a text node's width at the measured width of this
	// sample string, shaped through the same path as the node's own text.
	// Empty means natural width.
	//
	// It is a string rather than a pixel count because the floor is only
	// correct if it is measured on the face actually in use: a percentage sets
	// "100%" so its section does not reflow as the value crosses from one
	// digit to three, and tabular figures align digits but cannot fix a
	// changing digit count.
	MinWidthText string
	// Absent marks a node that has no reading to show. It still measures and
	// reserves its space, so a bar does not reflow when a source drops, but it
	// paints nothing: an empty meter track is indistinguishable from a genuine
	// zero, and a failed collector must not render as an idle machine.
	Absent bool
	// Tabular requests tabular (fixed-advance) figures when shaping this node.
	// A clock sets it: with proportional digits the rendered width changes as
	// the time changes, which visibly shifts a centred clock every minute.
	Tabular bool
	// TextRole is the semantic type role this node's text asks for. The zero
	// value is body text, and a button whose role is unset labels itself,
	// which is what keeps the common cases free of an explicit role.
	TextRole theme.TextRole
	// Bold, Italic, and Underline mark a styled run of body text. Cards carry
	// the notification body as separate styled runs, so the style lives on the
	// node rather than in the text. Bold and italic move the requested weight
	// and slant, so shaping resolves a real face; underline draws its own rule.
	Bold      bool
	Italic    bool
	Underline bool
	// Image is the raster a KindImage node draws. It is an immutable result
	// produced away from the Wayland owner; layout and paint only read it.
	// A nil image still measures, so a card does not reflow when an icon
	// resolves late or fails.
	Image *Image
	// ImageSize is the logical edge length a KindImage node reserves. The node
	// reserves its box whatever the raster turns out to be, so a decode that
	// arrives later cannot change the layout around it.
	ImageSize int
	// Tone selects the text colour. Zero is ToneNormal.
	Tone Tone
	// Fill selects a capsule's background, and a button's chrome. Zero is the
	// surface capsule / an unfilled button (the wrapping pill is the chrome).
	Fill Fill
	// Stroke is a capsule's border width in logical pixels. Zero means none.
	Stroke     int
	StrokeFill Fill
	State      Interaction
	Padding    int
	Gap        int
	Action     string
	// Tooltip is bounded hover text owned by the node's feature. The shared
	// dwell controller decides when and where to show it.
	Tooltip  string
	Bounds   Rect
	Children []*Node

	// Name and Role are required on every Focusable node.
	Focusable     bool
	Name          string
	Role          string
	DragType      string
	Payload       string
	Accept        []string
	Multiline     bool
	SubmitOnEnter bool
	Reseed        uint64
}

// StableKey reports the key animation and interaction state use across tree
// rebuilds. Actions are already unique within their owning surface.
func (n *Node) StableKey() string {
	if n == nil {
		return ""
	}
	if n.Key != "" {
		return n.Key
	}
	return n.Action
}

// Interaction is the resolved visual state supplied by the shell input host.
type Interaction uint8

const (
	StateHovered Interaction = 1 << iota
	StatePressed
	StateSelected
	StateDisabled
)

// Has reports whether a state bit is set.
func (s Interaction) Has(flag Interaction) bool { return s&flag != 0 }

func (n *Node) Active() int {
	if n == nil {
		return 0
	}
	return int(n.Value)
}

// Fill selects which theme colour paints a capsule, and with it the
// foreground its contents inherit.
type Fill uint8

const (
	// FillNone is the surface capsule that wraps an ordinary bar widget, and
	// an unfilled button: the pill is the chrome, not a highlight on the label.
	FillNone Fill = iota
	// FillAccent is the focused workspace pill, or an explicit filled chip.
	FillAccent
	// FillContainer is a workspace pill that is not focused.
	FillContainer
	// FillError is a destructive chip (Record) inside a surface pill.
	FillError
	// FillSoft is a muted accent wash. Contents keep the surface foreground,
	// so a selected launcher row is not a primary-on-white chip.
	FillSoft
	// FillContainerHigh is a panel card or nested high-emphasis container.
	FillContainerHigh
	// FillOutline is an idle control drawn on its parent with a boundary.
	FillOutline
)

// Tone selects which theme colour paints a text node.
//
// Error is for text that reports a failure instead of a value. A stale value
// is still a value and stays normal, carrying its age in the text; the muted
// token measures 1.47:1 against the background and cannot carry text at all.
type Tone uint8

const (
	ToneNormal Tone = iota
	ToneError
)

// Shape names the corner treatment a node asks for.
//
// Stadium and circle are geometric invariants, not radii: they stay half the
// box whatever the configurable base radius is, so a theme at radius zero
// still leaves a pill a pill and an avatar a circle.
type Shape uint8

const (
	// ShapeInherit takes the radius the surface passes down.
	ShapeInherit Shape = iota
	ShapeStadium
	ShapeCircle
	ShapeSmall
	ShapeMedium
	ShapeLarge
	ShapeCard
	ShapePanel
)

// MeasureText reports the logical width and height of a shaped string. The
// tabular flag is the node's, and reaches the shaper as an OpenType feature.
type MeasureText func(text string, attrs TextAttrs) (width, height int)

// TextAttrs is everything shaping needs to know about one text node. Layout
// passes it so a string is measured on the face it will be painted with: a
// label measured as regular body text and painted as a medium-weight label
// reserves the wrong width.
type TextAttrs struct {
	Role    theme.TextRole
	Tabular bool
	Bold    bool
	Italic  bool
}

// EffectiveTextRole is the role a node's text resolves to. A button labels
// itself unless it names another role, which is the one default that differs
// from body text.
func EffectiveTextRole(n *Node) theme.TextRole {
	if n == nil {
		return theme.RoleBody
	}
	if n.TextRole == theme.RoleBody && (n.Kind == KindButton || n.Kind == KindSegmented) {
		return theme.RoleLabel
	}
	return n.TextRole
}

// TextAttrsOf is the measurement request for one node.
func TextAttrsOf(n *Node) TextAttrs {
	if n == nil {
		return TextAttrs{}
	}
	return TextAttrs{
		Role:    EffectiveTextRole(n),
		Tabular: n.Tabular,
		Bold:    n.Bold,
		Italic:  n.Italic,
	}
}

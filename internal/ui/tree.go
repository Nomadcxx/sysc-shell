// Package ui holds the retained proof tree, its row layout, and hit testing.
// All coordinates are logical pixels; painting converts them to buffer pixels.
package ui

import (
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

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
	// KindEdgeFade softens a bar section into the surface at the edge where
	// overflow cut it. It is a signal, not a control: nothing to click, and it
	// takes no width, so reporting the overflow can never cause more of it.
	KindEdgeFade
	// KindIcon is one named glyph from the shell's dedicated chrome icon face.
	KindIcon
	// KindSegmented owns equal-width, exclusive button segments.
	KindSegmented

	KindDragSource
	KindDropZone
	// KindWordmark paints the shell's own SYSC mark from an embedded alpha
	// master, tinted with the accent colour. It is not KindIcon: that kind
	// reserves a square, and the mark is a wide lockup. The node carries the
	// box (ImageW/ImageH) so layout stays ignorant of the asset; the renderer
	// owns the mark's pixels and its aspect ratio.
	KindWordmark
	// KindRadialGauge is a compact labelled circular progress indicator used
	// by the bar's system summary.
	KindRadialGauge
	// KindStack lays every child into its own content box rather than flowing
	// them. Children paint in order, so the last is on top, and Hit already
	// walks children in reverse, so the topmost is hit first. It exists for a
	// card with a background image behind its content.
	KindStack
	// KindEffect is a non-interactive background layer for a stack.
	KindEffect

	// kindCount is one past the last kind. It exists so a test can assert that
	// every declared kind is measurable, and it must stay last.
	kindCount
)

// String names the kind the way a rejection message or a diagnostic reads.
// Only the string verbs use it: fmt keeps %d numeric for a Stringer, so the
// historical "kind %d" tails stay byte-identical.
func (k Kind) String() string {
	switch k {
	case KindRow:
		return "row"
	case KindText:
		return "text"
	case KindMeter:
		return "meter"
	case KindButton:
		return "button"
	case KindGraph:
		return "graph"
	case KindColumn:
		return "column"
	case KindSeparator:
		return "separator"
	case KindTab:
		return "tab"
	case KindToggle:
		return "toggle"
	case KindSlider:
		return "slider"
	case KindMenu:
		return "menu"
	case KindTextField:
		return "text_field"
	case KindScroll:
		return "scroll"
	case KindVirtualList:
		return "virtual_list"
	case KindImage:
		return "image"
	case KindCapsule:
		return "capsule"
	case KindEdgeFade:
		return "edge_fade"
	case KindIcon:
		return "icon"
	case KindSegmented:
		return "segmented"
	case KindDragSource:
		return "drag_source"
	case KindDropZone:
		return "drop_zone"
	case KindWordmark:
		return "wordmark"
	case KindRadialGauge:
		return "radial_gauge"
	case KindStack:
		return "stack"
	case KindEffect:
		return "effect"
	}
	return "kind " + strconv.Itoa(int(k))
}

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

type PaintRole uint8

const (
	PaintUnset PaintRole = iota
	PaintPrimary
	PaintSecondary
	PaintTertiary
	PaintOnSurfaceVariant
	PaintOnSurface
	PaintSurface
)

type GradientMotion uint8

const (
	GradientNone GradientMotion = iota
	GradientLoop
	GradientPingPong
)

type GradientStop struct {
	At   float64
	Role PaintRole
}

type GradientPaint struct {
	Stops    [4]GradientStop
	Count    int // 0 = solid path; else 2–4
	AngleDeg float64
	Motion   GradientMotion
	From, To float64
}

// Node is one retained element. Layout fills Bounds; every other field is
// supplied by the caller.
type Node struct {
	Kind Kind
	Text string
	// Placeholder is input guidance painted only while Text is empty. Name
	// remains the accessible identity.
	Placeholder string
	// ValueText is the compact formatted value painted inside a radial gauge.
	ValueText string
	// Icon names a glyph in the dedicated chrome icon inventory.
	Icon string
	// Key identifies a node across tree rebuilds so host-retained state -- an
	// editor buffer, a hover or press state, an in-flight transition -- follows
	// the node it belongs to. Action is the fallback for actionable nodes; see
	// StableKey.
	Key   string
	Value float64
	// Animate asks the surface animator to glide Value to each revision's
	// target instead of jumping. Only a meter or a radial gauge honours it,
	// and only when the node carries a stable key.
	Animate bool
	Min     float64
	Max     float64
	Step    float64
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
	// HideScrollbar keeps wheel, keyboard, and programmatic scrolling while
	// removing both the painted thumb and its pointer track.
	HideScrollbar bool
	ItemCount     int
	ItemHeight    int
	ContentH      int
	Item          func(int) *Node
	// Values are the graph's samples, oldest first, each already normalised to
	// zero through one by the widget. The node carries no scale of its own.
	Values []float64
	// SecondValues is an optional second graph series, drawn as a thin line
	// with no area in the secondary colour: upload beside download, writes
	// beside reads. Normalised to the same scale as Values.
	SecondValues []float64
	// Window is how many samples the graph's full width represents. Zero
	// means the samples present fill the width. A graph with a window draws a
	// short history against the right edge rather than stretching it.
	Window int
	// MaxWidth caps a text node's measured width. Zero means unbounded. It
	// exists because a focused-window title is unbounded user text: without a
	// cap it would take a whole section's budget before anything truncated.
	MaxWidth int
	// Marquee asks the bar resolver to replace truncation with a clipped,
	// wrapping text run when the measured text overflows this node's cell.
	Marquee bool
	// TextOffset is the resolved physical-pixel phase for a marquee copy. It
	// lives on the render copy, not the retained widget tree.
	TextOffset int
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
	// Effect is the host-owned descriptor for a KindEffect node.
	Effect EffectSpec
	// EffectPhase is the resolved animation phase supplied by the surface host.
	EffectPhase float64
	// ImageSize is the logical edge length a KindImage node reserves. The node
	// reserves its box whatever the raster turns out to be, so a decode that
	// arrives later cannot change the layout around it.
	ImageSize int
	// Background marks an image that fills a container rather than standing in
	// for an icon. It selects bilinear sampling: an icon is produced at the
	// size the node asked for, a background is scaled to whatever the card
	// measures.
	Background bool
	// Opacity is group opacity in percent. Zero means unset/full opacity; a
	// non-zero value composites the complete subtree once, so overlapping
	// children do not multiply their alpha.
	Opacity uint8
	// ImageW and ImageH are the landscape form of ImageSize, for a raster that
	// is not square: a wallpaper thumbnail rather than an icon. Both must be
	// positive to take effect, because half a box is not a box; otherwise the
	// node falls back to the square ImageSize. Painting scales the raster to
	// fill whichever box it is given, so a non-square source must already be
	// cropped to this ratio by whoever produced it.
	ImageW int
	ImageH int
	// ImagePath is the filesystem path a plugin image node names, recorded
	// so the decode registrar can find the nodes whose raster is still
	// missing and key the worker's cache by what was asked for. It is
	// host-side bookkeeping, like Image: layout and paint never read it.
	ImagePath string
	// Path is host bookkeeping too: the converter's wire path for this node
	// ("root.children[1].children[0]"). Layout and paint never read it; a
	// rejection message names the node with it, so a plugin author can find
	// the node the host refused without instrumenting anything.
	Path string
	// Mark names the embedded alpha master a KindWordmark node paints. Empty
	// selects the SYSC wordmark; "launcher" selects the launcher aperture
	// mark. Both are alpha-only and tinted at paint time, so they follow the
	// theme and its animated gradient ramp like the rest of the chrome.
	Mark string
	// Tone selects the text colour. Zero is ToneNormal.
	Tone Tone
	// CenterX centres this child within its column track instead of placing
	// it at the track's left edge. The child is measured first and its track
	// narrowed to that width, so a container centres with its contents rather
	// than laying out across the full width and leaving nothing to move. A
	// child at least as wide as the track keeps the full track.
	CenterX bool
	// CenterY centres a sole child within its column's content height. It is
	// useful for a fixed card whose content grows with typography without
	// changing the card's outer geometry.
	CenterY bool
	// PinEnd right-pins the last child of a two-child row to the row's inner
	// right edge. Without it, only a row whose first child is KindText pins:
	// that narrow case predates this flag and stays, because the callers
	// relying on it never set one.
	PinEnd bool
	// Fill selects a capsule's background, and a button's chrome. Zero is the
	// surface capsule / an unfilled button (the wrapping pill is the chrome).
	Fill Fill
	// Stroke is a capsule's border width in logical pixels. Zero means none.
	Stroke     int
	StrokeFill Fill
	// Gradient is a 2–4 stop token ramp. Count 0 is solid. GradientOffset is
	// the live sample shift; it is not part of the recipe.
	Gradient       GradientPaint
	GradientOffset float64
	State          Interaction
	Padding        int
	Gap            int
	Action         string
	// Tooltip is bounded hover text owned by the node's feature. The shared
	// dwell controller decides when and where to show it.
	Tooltip  string
	Bounds   Rect
	Children []*Node

	// Name and Role are required on every Focusable node.
	Focusable bool
	// AriaDisabled keeps a disabled destination in keyboard traversal so its
	// accessible name can explain why it is unavailable. Activation remains
	// blocked by StateDisabled.
	AriaDisabled  bool
	Name          string
	Role          string
	DragType      string
	Payload       string
	Accept        []string
	Multiline     bool
	SubmitOnEnter bool
	// Masked asks the renderer to draw one bullet per rune instead of the
	// rune. Text still carries the real value: the disguise is applied when
	// the field is measured and painted, nowhere else.
	Masked bool
	Reseed uint64
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
	// FillContainerHighest is a control or chip nested inside a capsule or a
	// card. Those share the high container, so anything sitting on one needs
	// the level above it to separate.
	FillContainerHighest
	// FillErrorContainer is a destructive control that has to hold a label
	// rather than shout: the quiet half of the error pair.
	FillErrorContainer
	// FillScrim dims the live content behind a modal surface. It is a wash,
	// not a plate; what sits underneath stays visible through it.
	FillScrim
	// Note fills are muted semantic tints paired with the theme foreground.
	FillNoteSun
	FillNoteMint
	FillNoteSky
	FillNoteRose
	FillNoteLilac
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
	// ToneAccent paints brand chrome -- the launcher's SYSC rail -- in the
	// accent. It is not a muted tone: the accent is a foreground-weight token
	// that carries text at full contrast, which the muted token cannot.
	ToneAccent
	// ToneSubtle paints secondary text in the theme's muted foreground. The
	// theme's own Muted token is legible here: it is derived from Material's
	// on_surface_variant, which carries text at full contrast, unlike the
	// low-contrast muted token the Tone doc rules out.
	ToneSubtle
	// ToneActivity marks a reading past its activity threshold but short of
	// critical. It paints the theme's Tertiary role: the theme has no warning
	// token, and Tertiary exists on every palette.
	ToneActivity
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

// imageBox returns the explicit landscape box of a KindImage node. Both edges
// must be positive: a node carrying only one of them has not described a box,
// and falls back to the square ImageSize rather than measuring to zero.
func imageBox(n *Node) (w, h int, ok bool) {
	if n.ImageW > 0 && n.ImageH > 0 {
		return n.ImageW, n.ImageH, true
	}
	return 0, 0, false
}

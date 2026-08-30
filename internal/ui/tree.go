// Package ui holds the retained proof tree, its row layout, and hit testing.
// All coordinates are logical pixels; painting converts them to buffer pixels.
package ui

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
)

// Rect is a logical-pixel rectangle.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the point lies inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Node is one retained element. Layout fills Bounds; every other field is
// supplied by the caller.
type Node struct {
	Kind  Kind
	Text  string
	Value float64
	Min   float64
	Max   float64
	Step  float64
	Width int
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
	// Tone selects the text colour. Zero is ToneNormal.
	Tone     Tone
	Padding  int
	Gap      int
	Action   string
	Bounds   Rect
	Children []*Node

	// Name and Role are required on every Focusable node.
	Focusable bool
	Name      string
	Role      string
}

func (n *Node) Active() int {
	if n == nil {
		return 0
	}
	return int(n.Value)
}

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

// MeasureText reports the logical width and height of a shaped string. The
// tabular flag is the node's, and reaches the shaper as an OpenType feature.
type MeasureText func(text string, tabular bool) (width, height int)

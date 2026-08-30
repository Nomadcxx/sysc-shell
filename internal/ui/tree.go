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
	Width int
	// Values are the graph's samples, oldest first, each already normalised to
	// zero through one by the widget. The node carries no scale of its own.
	Values []float64
	// MaxWidth caps a text node's measured width. Zero means unbounded. It
	// exists because a focused-window title is unbounded user text: without a
	// cap it would take a whole section's budget before anything truncated.
	MaxWidth int
	// MinWidth reserves a floor for a text node's measured width. Zero means
	// natural. A percentage sets it so its section does not reflow as the
	// value crosses from one digit to three; tabular figures align digits but
	// cannot fix a changing digit count.
	MinWidth int
	// Tabular requests tabular (fixed-advance) figures when shaping this node.
	// A clock sets it: with proportional digits the rendered width changes as
	// the time changes, which visibly shifts a centred clock every minute.
	Tabular  bool
	Padding  int
	Gap      int
	Action   string
	Bounds   Rect
	Children []*Node
}

// MeasureText reports the logical width and height of a shaped string. The
// tabular flag is the node's, and reaches the shaper as an OpenType feature.
type MeasureText func(text string, tabular bool) (width, height int)

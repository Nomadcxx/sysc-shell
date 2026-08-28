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
	Kind     Kind
	Text     string
	Value    float64
	Width    int
	Padding  int
	Gap      int
	Action   string
	Bounds   Rect
	Children []*Node
}

// MeasureText reports the logical width and height of a shaped string.
type MeasureText func(string) (width, height int)

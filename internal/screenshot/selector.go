package screenshot

import (
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Pointer buttons, as evdev codes.
const (
	ButtonLeft  uint32 = 0x110
	ButtonRight uint32 = 0x111
)

// Outcome is what an input asks of the selector's owner.
type Outcome uint8

const (
	// Pending keeps the selector open.
	Pending Outcome = iota
	// Confirm takes the current rectangle.
	Confirm
	// Cancel closes the selector without a capture.
	Cancel
)

// Selector is the region selector's state: one rectangle in surface-logical
// pixels, drawn by dragging with the left button. Release keeps the rectangle,
// Enter or Space confirms it, and Escape or any other button cancels. It is
// DMS's default interaction, without its move and resize handles.
type Selector struct {
	anchorX, anchorY float64
	curX, curY       float64
	started          bool
	dragging         bool
}

// Press handles a button press at x, y.
func (s *Selector) Press(button uint32, x, y float64) Outcome {
	if button != ButtonLeft {
		return Cancel
	}
	s.anchorX, s.anchorY, s.curX, s.curY = x, y, x, y
	s.started, s.dragging = true, true
	return Pending
}

// Motion extends a drag and reports whether the rectangle changed.
func (s *Selector) Motion(x, y float64) bool {
	if !s.dragging {
		return false
	}
	before, _ := s.Rect()
	s.curX, s.curY = x, y
	after, _ := s.Rect()
	return after != before
}

// Release ends a drag, keeping its rectangle.
func (s *Selector) Release() { s.dragging = false }

// Key handles a key press by keysym.
func (s *Selector) Key(sym uint32) Outcome {
	switch sym {
	case ui.SymEscape:
		return Cancel
	case ui.SymReturn, ui.SymKPEnter, ' ':
		if _, ok := s.Rect(); ok {
			return Confirm
		}
	}
	return Pending
}

// Rect is the selected rectangle, its edges rounded outward to whole logical
// pixels, and whether it has any area.
func (s *Selector) Rect() (ui.Rect, bool) {
	if !s.started {
		return ui.Rect{}, false
	}
	x0 := int(math.Floor(min(s.anchorX, s.curX)))
	y0 := int(math.Floor(min(s.anchorY, s.curY)))
	x1 := int(math.Ceil(max(s.anchorX, s.curX)))
	y1 := int(math.Ceil(max(s.anchorY, s.curY)))
	if x1 <= x0 || y1 <= y0 {
		return ui.Rect{}, false
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

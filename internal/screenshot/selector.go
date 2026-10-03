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

// Handle geometry, in logical pixels, as DMS sizes it: drawn at radius 12,
// grabbed within 16.
const (
	HandleRadius    = 12
	HandleHitRadius = HandleRadius + 4
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

// edgeMask names the rectangle edges a grabbed handle drags.
type edgeMask uint8

const (
	edgeNone   edgeMask = 0
	edgeLeft   edgeMask = 1 << 0
	edgeTop    edgeMask = 1 << 1
	edgeRight  edgeMask = 1 << 2
	edgeBottom edgeMask = 1 << 3
)

// mode is what the left button currently does with motion.
type mode uint8

const (
	modeIdle mode = iota
	modeDraw
	modeMove
	modeResize
)

// Selector is the region selector's state: one rectangle in surface-logical
// pixels. A left press draws it; with Ctrl held, a press on one of the eight
// handles resizes it, a press inside moves it, and either clamps to the
// surface bounds. Release keeps the rectangle, Enter or Space confirms it,
// and Escape or any other button cancels. DMS's interaction, widened from its
// four corner handles to eight.
type Selector struct {
	anchorX, anchorY float64
	curX, curY       float64
	started          bool
	mode             mode
	edges            edgeMask
	grabDX, grabDY   float64
	boundsW, boundsH float64
}

// SetBounds records the surface the active rectangle lives on, in logical
// pixels, so a move or resize clamps to it.
func (s *Selector) SetBounds(w, h int) {
	s.boundsW, s.boundsH = float64(w), float64(h)
}

// Press handles an unmodified button press at x, y.
func (s *Selector) Press(button uint32, x, y float64) Outcome {
	return s.PressMods(button, 0, x, y)
}

// PressMods handles a button press with the held modifiers.
func (s *Selector) PressMods(button uint32, mods ui.Mods, x, y float64) Outcome {
	if button != ButtonLeft {
		return Cancel
	}
	if s.started && mods.Has(ui.ModCtrl) {
		minX, minY, maxX, maxY := s.edges4()
		if e := s.handleAt(x, y); e != edgeNone {
			s.resizeTo(e, minX, minY, maxX, maxY)
			s.edges = e
			s.mode = modeResize
			return Pending
		}
		if x >= minX && x <= maxX && y >= minY && y <= maxY {
			s.anchorX, s.anchorY = minX, minY
			s.curX, s.curY = maxX, maxY
			s.grabDX, s.grabDY = x-minX, y-minY
			s.mode = modeMove
			return Pending
		}
	}
	s.anchorX, s.anchorY, s.curX, s.curY = x, y, x, y
	s.started, s.mode = true, modeDraw
	return Pending
}

// Motion applies to the active draw, move, or resize and reports whether the
// rectangle changed.
func (s *Selector) Motion(x, y float64) bool {
	switch s.mode {
	case modeDraw:
		before, _ := s.Rect()
		s.curX, s.curY = x, y
		after, _ := s.Rect()
		return after != before
	case modeMove:
		w, h := s.curX-s.anchorX, s.curY-s.anchorY
		nx := s.clamp(x-s.grabDX, 0, s.boundsW-w)
		ny := s.clamp(y-s.grabDY, 0, s.boundsH-h)
		before, _ := s.Rect()
		s.anchorX, s.anchorY = nx, ny
		s.curX, s.curY = nx+w, ny+h
		after, _ := s.Rect()
		return after != before
	case modeResize:
		before, _ := s.Rect()
		if s.edges&(edgeLeft|edgeRight) != 0 {
			s.curX = s.clamp(x, 0, s.boundsW)
		}
		if s.edges&(edgeTop|edgeBottom) != 0 {
			s.curY = s.clamp(y, 0, s.boundsH)
		}
		after, _ := s.Rect()
		return after != before
	}
	return false
}

// clamp restricts v to [lo, hi], ignoring an unset (non-positive) bound.
func (s *Selector) clamp(v, lo, hi float64) float64 {
	if hi < lo || s.boundsW <= 0 || s.boundsH <= 0 {
		return v
	}
	return min(max(v, lo), hi)
}

// Release ends the active draw, move, or resize, keeping its rectangle.
func (s *Selector) Release() { s.mode = modeIdle }

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
	minX, minY, maxX, maxY := s.edges4()
	x0, y0 := int(math.Floor(minX)), int(math.Floor(minY))
	x1, y1 := int(math.Ceil(maxX)), int(math.Ceil(maxY))
	if x1 <= x0 || y1 <= y0 {
		return ui.Rect{}, false
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

func (s *Selector) edges4() (minX, minY, maxX, maxY float64) {
	return min(s.anchorX, s.curX), min(s.anchorY, s.curY), max(s.anchorX, s.curX), max(s.anchorY, s.curY)
}

// handleAt returns the edges of the closest handle within HandleHitRadius of
// x, y, or edgeNone. Corners win over edge middles.
func (s *Selector) handleAt(x, y float64) edgeMask {
	if !s.started {
		return edgeNone
	}
	minX, minY, maxX, maxY := s.edges4()
	midX, midY := (minX+maxX)/2, (minY+maxY)/2
	best, bestD := edgeNone, math.Inf(1)
	for _, h := range []struct {
		e      edgeMask
		hx, hy float64
	}{
		{edgeLeft | edgeTop, minX, minY}, {edgeRight | edgeTop, maxX, minY},
		{edgeLeft | edgeBottom, minX, maxY}, {edgeRight | edgeBottom, maxX, maxY},
		{edgeTop, midX, minY}, {edgeBottom, midX, maxY},
		{edgeLeft, minX, midY}, {edgeRight, maxX, midY},
	} {
		d := (x-h.hx)*(x-h.hx) + (y-h.hy)*(y-h.hy)
		if d <= float64(HandleHitRadius*HandleHitRadius) && d < bestD {
			best, bestD = h.e, d
		}
	}
	return best
}

// resizeTo re-anchors the rectangle so the grabbed edges are the ones Motion
// moves and the fixed ones stay put.
func (s *Selector) resizeTo(e edgeMask, minX, minY, maxX, maxY float64) {
	s.anchorX, s.curX = minX, maxX
	s.anchorY, s.curY = minY, maxY
	if e&edgeLeft != 0 {
		s.anchorX, s.curX = maxX, minX
	}
	if e&edgeRight != 0 {
		s.anchorX, s.curX = minX, maxX
	}
	if e&edgeTop != 0 {
		s.anchorY, s.curY = maxY, minY
	}
	if e&edgeBottom != 0 {
		s.anchorY, s.curY = minY, maxY
	}
}

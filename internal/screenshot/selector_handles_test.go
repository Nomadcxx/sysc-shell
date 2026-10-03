package screenshot

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// pressed builds a released 100x50 rect at (200,100)-(300,150) with 400x300
// surface bounds, the fixture every handle case starts from.
func pressed(t *testing.T) *Selector {
	t.Helper()
	var s Selector
	s.SetBounds(400, 300)
	if got := s.Press(ButtonLeft, 200, 100); got != Pending {
		t.Fatalf("draw press: got %v", got)
	}
	s.Motion(300, 150)
	s.Release()
	return &s
}

func rectOf(t *testing.T, s *Selector) ui.Rect {
	t.Helper()
	r, ok := s.Rect()
	if !ok {
		t.Fatal("selector reports no rectangle")
	}
	return r
}

func TestSelectorHandleHitTest(t *testing.T) {
	s := pressed(t)
	minX, minY, maxX, maxY := 200.0, 100.0, 300.0, 150.0
	midX, midY := (minX+maxX)/2, (minY+maxY)/2
	cases := []struct {
		name string
		x, y float64
		want edgeMask
	}{
		{"top-left", minX, minY, edgeLeft | edgeTop},
		{"top-right", maxX, minY, edgeRight | edgeTop},
		{"bottom-left", minX, maxY, edgeLeft | edgeBottom},
		{"bottom-right", maxX, maxY, edgeRight | edgeBottom},
		{"top", midX, minY, edgeTop},
		{"bottom", midX, maxY, edgeBottom},
		{"left", minX, midY, edgeLeft},
		{"right", maxX, midY, edgeRight},
		{"near top-left", minX + 5, minY - 5, edgeLeft | edgeTop},
		{"just outside", minX - HandleHitRadius - 1, minY, edgeNone},
		{"inside", midX, midY, edgeNone},
		{"outside", 10, 10, edgeNone},
	}
	for _, tc := range cases {
		if got := s.handleAt(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: handleAt(%v,%v) = %04b, want %04b", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSelectorCtrlMotionModes(t *testing.T) {
	midX, midY := 250.0, 125.0
	cases := []struct {
		name    string
		pressX  float64
		pressY  float64
		motionX float64
		motionY float64
		want    ui.Rect
	}{
		{"move right", midX, midY, midX + 40, midY + 10, ui.Rect{X: 240, Y: 110, W: 100, H: 50}},
		{"move clamps left", midX, midY, -50, midY, ui.Rect{X: 0, Y: 100, W: 100, H: 50}},
		{"move clamps bottom", midX, midY, midX, 999, ui.Rect{X: 200, Y: 250, W: 100, H: 50}},
		{"resize top-left", 200, 100, 180, 80, ui.Rect{X: 180, Y: 80, W: 120, H: 70}},
		{"resize top", midX, 100, midX, 80, ui.Rect{X: 200, Y: 80, W: 100, H: 70}},
		{"resize bottom", midX, 150, midX, 170, ui.Rect{X: 200, Y: 100, W: 100, H: 70}},
		{"resize left", 200, midY, 150, midY, ui.Rect{X: 150, Y: 100, W: 150, H: 50}},
		{"resize right", 300, midY, 330, midY, ui.Rect{X: 200, Y: 100, W: 130, H: 50}},
		{"resize bottom-right", 300, 150, 320, 170, ui.Rect{X: 200, Y: 100, W: 120, H: 70}},
		{"resize clamps", 300, 150, 9999, 9999, ui.Rect{X: 200, Y: 100, W: 200, H: 200}},
	}
	for _, tc := range cases {
		s := pressed(t)
		if got := s.PressMods(ButtonLeft, ui.ModCtrl, tc.pressX, tc.pressY); got != Pending {
			t.Fatalf("%s: Ctrl press: got %v", tc.name, got)
		}
		s.Motion(tc.motionX, tc.motionY)
		s.Release()
		if got := rectOf(t, s); got != tc.want {
			t.Errorf("%s: rect = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestSelectorCtrlPressOutsideRedraws(t *testing.T) {
	s := pressed(t)
	if got := s.PressMods(ButtonLeft, ui.ModCtrl, 10, 10); got != Pending {
		t.Fatalf("got %v", got)
	}
	s.Motion(50, 50)
	s.Release()
	if got := rectOf(t, s); got != (ui.Rect{X: 10, Y: 10, W: 40, H: 40}) {
		t.Errorf("redraw outside: got %+v", got)
	}
}

func TestSelectorCtrlMoveKeepsRectOnZeroMotion(t *testing.T) {
	s := pressed(t)
	s.PressMods(ButtonLeft, ui.ModCtrl, 250, 125)
	s.Release()
	if got := rectOf(t, s); got != (ui.Rect{X: 200, Y: 100, W: 100, H: 50}) {
		t.Errorf("zero-motion Ctrl press changed the rect: got %+v", got)
	}
	if got := s.Key(ui.SymReturn); got != Confirm {
		t.Errorf("Key(Return) = %v, want Confirm", got)
	}
}

func TestSelectorHandlesNeedARectangle(t *testing.T) {
	var s Selector
	s.SetBounds(400, 300)
	if got := s.PressMods(ButtonLeft, ui.ModCtrl, 50, 50); got != Pending {
		t.Fatalf("got %v", got)
	}
	s.Motion(60, 60)
	s.Release()
	if s.handleAt(100, 100) != edgeNone {
		t.Error("hit far outside the small rectangle reported a handle")
	}
}

func TestSelectorResizeFlipKeepsArea(t *testing.T) {
	s := pressed(t)
	// Drag the left edge far past the right edge: the rectangle flips, not
	// collapses: Rect normalises the inverted anchors.
	s.PressMods(ButtonLeft, ui.ModCtrl, 200, 125)
	s.Motion(350, 125)
	s.Release()
	if got := rectOf(t, s); got != (ui.Rect{X: 300, Y: 100, W: 50, H: 50}) {
		t.Errorf("flipped resize: got %+v", got)
	}
}

func TestSelectorPressDefaultsToNoMods(t *testing.T) {
	s := pressed(t)
	// Press (no mods) inside the rect starts a fresh draw, never a move.
	s.Press(ButtonLeft, 250, 125)
	s.Motion(260, 135)
	if got := rectOf(t, s); got != (ui.Rect{X: 250, Y: 125, W: 10, H: 10}) {
		t.Errorf("plain press inside the rect: got %+v", got)
	}
}

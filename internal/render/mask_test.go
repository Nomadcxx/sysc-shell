package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRoundedMaskCornersTransparentCenterOpaque(t *testing.T) {
	m := RoundedMask(12, 100, 60)
	if m.AlphaAt(0, 0).A != 0 {
		t.Fatal("corner must be transparent")
	}
	if m.AlphaAt(50, 30).A != 255 {
		t.Fatal("center must be opaque")
	}
	if got := m.AlphaAt(3, 3).A; got == 0 || got == 255 {
		t.Fatalf("arc edge must have partial coverage, got %d", got)
	}
}

func TestRoundedMaskCacheReuses(t *testing.T) {
	a := RoundedMask(12, 100, 60)
	b := RoundedMask(12, 100, 60)
	if a != b {
		t.Fatal("same key must return same mask")
	}
}

func TestShadowTextureExtendsBeyondBounds(t *testing.T) {
	s := ShadowTexture(100, 60, 12, ElevPanel)
	if s.Bounds().Dx() <= 100 || s.Bounds().Dy() <= 60 {
		t.Fatal("shadow must spread beyond panel")
	}
	if s.AlphaAt(s.Bounds().Dx()/2, 12).A <= s.AlphaAt(0, 0).A {
		t.Fatal("shadow alpha must be stronger along the panel edge")
	}
}

func TestAttachedMaskTopCornersAreConcave(t *testing.T) {
	const w, h, radius, bulge = 60, 40, 12, 12
	m := AttachedMask(w, h, radius, bulge, "top")

	// The outermost top pixel is inside the shape: the panel is widest at the joint.
	if got := m.AlphaAt(0, 0).A; got != 255 {
		t.Errorf("top-left wing pixel = %d, want 255 (wing is filled at the bar)", got)
	}
	// One bulge down the left wing, inset from the disc's tangent (column 0
	// is antialiased, never fully empty). The edge has swept inward.
	if got := m.AlphaAt(2, bulge-1).A; got != 0 {
		t.Errorf("pixel below the wing = %d, want 0 (edge swept inward)", got)
	}
	// The far corners stay convex: the very corner pixel is outside.
	if got := m.AlphaAt(0, h-1).A; got != 0 {
		t.Errorf("bottom-left corner = %d, want 0 (convex)", got)
	}
	// bulge 0 is the plain rounded rect, which is where the reveal starts.
	flat := AttachedMask(w, h, radius, 0, "top")
	if flat.AlphaAt(0, 0).A != RoundedMask(radius, w, h).AlphaAt(0, 0).A {
		t.Error("bulge 0 must equal RoundedMask, so a hidden edge draws flat")
	}
}

func TestCanvasFillRoundedMatchesMask(t *testing.T) {
	c, err := NewCanvas(make([]byte, 40*40*4), 40, 40, 40*4)
	if err != nil {
		t.Fatal(err)
	}
	c.FillRounded(ui.Rect{W: 40, H: 40}, 12, Color{R: 255, A: 255})
	if got := c.Pix[3]; got != 0 {
		t.Fatalf("corner alpha = %d, want untouched", got)
	}
	if got := c.Pix[20*c.Stride+20*4+3]; got != 255 {
		t.Fatalf("center alpha = %d, want opaque", got)
	}
}

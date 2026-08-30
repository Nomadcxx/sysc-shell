package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOpaqueRectsExcludeCorners(t *testing.T) {
	t.Parallel()
	body := ui.Rect{X: 4, Y: 4, W: 200, H: 40}

	got := opaqueRects(body, 12, true)
	want := []ui.Rect{
		{X: 4, Y: 16, W: 200, H: 16}, // full width, inset vertically by the radius
		{X: 16, Y: 4, W: 176, H: 40}, // full height, inset horizontally by it
	}
	if len(got) != len(want) {
		t.Fatalf("opaqueRects returned %d rects, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rect %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestOpaqueRectsWithoutRadiusIsTheBody(t *testing.T) {
	t.Parallel()
	body := ui.Rect{X: 4, Y: 4, W: 200, H: 40}
	got := opaqueRects(body, 0, true)
	if len(got) != 1 || got[0] != body {
		t.Fatalf("opaqueRects = %+v, want just the body", got)
	}
}

func TestTranslucentBackgroundHasNoOpaqueRegion(t *testing.T) {
	t.Parallel()
	// Declaring a translucent bar opaque would let the compositor skip drawing
	// behind it, which shows as corruption rather than transparency.
	if got := opaqueRects(ui.Rect{X: 0, Y: 0, W: 10, H: 10}, 2, false); got != nil {
		t.Fatalf("opaqueRects = %+v, want nil for a translucent background", got)
	}
}

func TestOversizedRadiusIsClamped(t *testing.T) {
	t.Parallel()
	body := ui.Rect{X: 0, Y: 0, W: 10, H: 6}
	for _, r := range opaqueRects(body, 40, true) {
		if r.W < 0 || r.H < 0 {
			t.Fatalf("clamping produced a negative rect %+v", r)
		}
	}
}

func TestEmptyBodyHasNoOpaqueRegion(t *testing.T) {
	t.Parallel()
	if got := opaqueRects(ui.Rect{}, 4, true); got != nil {
		t.Fatalf("opaqueRects = %+v, want nil for an empty body", got)
	}
}

// The input region is the whole surface, including the transparent gap band.
// Milestone 2 declares no click-through pixels inside the surface, so a pointer
// slammed to the screen edge lands on the bar instead of a dead strip.
func TestInputRegionCoversTheWholeSurfaceIncludingTheGap(t *testing.T) {
	t.Parallel()
	surface := ui.Rect{X: 0, Y: 0, W: 3440, H: 44}
	body := ui.Rect{X: 4, Y: 4, W: 3432, H: 40}

	got := inputRect(surface)
	if got != surface {
		t.Fatalf("inputRect = %+v, want the whole surface %+v", got, surface)
	}
	if got.H == body.H {
		t.Fatal("the input region excluded the gap band, leaving a dead strip at the screen edge")
	}
}

func TestHostRegionGeometryUsesCurrentConfigureAndCandidateGap(t *testing.T) {
	t.Parallel()

	h := newHost(7, nil)
	h.bar.ss.configure(1200, 44)
	h.bar.ss.acknowledge()
	policy := h.policy
	policy.Gap = 6

	surface, body := hostRegionGeometry(h, policy)
	if surface != (ui.Rect{W: 1200, H: 44}) {
		t.Fatalf("surface = %+v, want current configure 1200x44", surface)
	}
	if body != (ui.Rect{X: 6, Y: 6, W: 1188, H: 38}) {
		t.Fatalf("body = %+v, want candidate 6px gap inside current configure", body)
	}
}

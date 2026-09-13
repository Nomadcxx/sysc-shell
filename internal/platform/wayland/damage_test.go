package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestDamageRectsFallBackToFullBuffer(t *testing.T) {
	t.Parallel()
	// A nil callback is every surface that has not opted in. It must produce
	// exactly one full-buffer rectangle, which is today's behaviour.
	got := damageRects(nil, 800, 600)
	want := []ui.Rect{{X: 0, Y: 0, W: 800, H: 600}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("nil callback gave %v, want %v", got, want)
	}
}

func TestDamageRectsEmptySliceIsAlsoFull(t *testing.T) {
	t.Parallel()
	// "I tracked nothing" and "nothing changed" must both repaint everything.
	got := damageRects(func() []ui.Rect { return nil }, 800, 600)
	if len(got) != 1 || got[0].W != 800 || got[0].H != 600 {
		t.Fatalf("empty result gave %v, want one full rect", got)
	}
}

func TestDamageRectsClipToTheBuffer(t *testing.T) {
	t.Parallel()
	// A rectangle outside the buffer is a protocol error waiting to happen.
	got := damageRects(func() []ui.Rect {
		return []ui.Rect{{X: -10, Y: -10, W: 40, H: 40}, {X: 790, Y: 590, W: 100, H: 100}}
	}, 800, 600)
	for _, r := range got {
		if r.X < 0 || r.Y < 0 || r.X+r.W > 800 || r.Y+r.H > 600 {
			t.Errorf("rect %+v escapes the 800x600 buffer", r)
		}
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("rect %+v is degenerate after clipping", r)
		}
	}
}

func TestDamageRectsDropsFullyOutsideRects(t *testing.T) {
	t.Parallel()
	got := damageRects(func() []ui.Rect {
		return []ui.Rect{{X: 2000, Y: 2000, W: 10, H: 10}}
	}, 800, 600)
	if len(got) != 0 {
		t.Errorf("a rect entirely outside the buffer produced %v", got)
	}
}

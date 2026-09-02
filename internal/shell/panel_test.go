package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestClampKeepsPanelInsideOutput(t *testing.T) {
	out := ui.Rect{W: 1920, H: 1080}
	cases := []struct{ desired, size, pad, want int }{
		{-50, 400, 8, 8}, {2000, 400, 8, 1512}, {760, 400, 8, 760},
	}
	for _, c := range cases {
		if got := clampAxis(c.desired, c.size, out.W, c.pad); got != c.want {
			t.Fatalf("clamp(%d,%d)=%d want %d", c.desired, c.size, got, c.want)
		}
	}
}

func TestPanelLargerThanOutputClampsToPadding(t *testing.T) {
	if got := clampAxis(0, 2000, 1080, 8); got != 8 {
		t.Fatalf("oversized panel must sit at padding, got %d", got)
	}
}

func TestAnchorMarginsForTopBar(t *testing.T) {
	g := Placement{BarEdge: "top", Output: ui.Rect{W: 1920, H: 1080}, BarZone: 40, Gap: 8, Padding: 8, Panel: ui.Rect{W: 700, H: 520}, Align: "center"}
	m := g.Margins()
	if m.Top != 48 || m.Left != 610 {
		t.Fatalf("margins wrong: %+v", m)
	}
}

// Attached panels sit on the exclusive-zone edge, centred, with no extra gap.
// The live IPC path used to centre against 1920 on a 1536 laptop, and to add
// Height+Gap below a 44-pixel bar, so Super+M opened down and to the right.
func TestAttachedPanelHugsTheBar(t *testing.T) {
	p := Placement{
		BarEdge: "top", Output: ui.Rect{W: 1536, H: 864},
		BarZone: 44, Gap: 0, Padding: 8, Panel: ui.Rect{W: 640, H: 480}, Align: "center",
	}
	m := p.Margins()
	if m.Top != 44 {
		t.Fatalf("top = %d, want the exclusive zone with no gap", m.Top)
	}
	if m.Left != (1536-640)/2 {
		t.Fatalf("left = %d, want centred on 1536", m.Left)
	}
}

func TestFittedSizeShrinksTallPanel(t *testing.T) {
	p := Placement{BarEdge: "top", Output: ui.Rect{W: 800, H: 600}, BarZone: 40, Gap: 8, Padding: 8, Panel: ui.Rect{W: 700, H: 900}}
	_, h := p.FittedSize()
	if h != 600-40-8-8 {
		t.Fatalf("height must shrink to fit: %d", h)
	}
}

func TestSingleInstanceToggleAndMove(t *testing.T) {
	ps := &PanelSet{}
	if ps.Toggle(PanelSession, 1) != Opened {
		t.Fatal("first toggle opens")
	}
	if ps.Toggle(PanelSession, 1) != Closed {
		t.Fatal("same-output toggle closes")
	}
	ps.Toggle(PanelSession, 1)
	if ps.Toggle(PanelSession, 2) != Moved {
		t.Fatal("other-output toggle closes+reopens there")
	}
	if where, ok := ps.Output(PanelSession); !ok || where != 2 {
		t.Fatal("panel must now live on output 2")
	}
	if ps.Toggle(PanelMonitor, 1) != Opened {
		t.Fatal("different panel id is independent")
	}
}

func TestMonitorPublicName(t *testing.T) {
	if PanelMonitor.String() != "system-monitor" {
		t.Fatalf("public name = %q, want system-monitor", PanelMonitor.String())
	}
}

func TestFittedSizeTinyOutputNonNegative(t *testing.T) {
	p := Placement{
		BarEdge: "top", Output: ui.Rect{W: 20, H: 10},
		BarZone: 40, Gap: 8, Padding: 8, Panel: ui.Rect{W: 700, H: 520},
	}
	w, h := p.FittedSize()
	if w < 0 || h < 0 {
		t.Fatalf("fitted size must stay non-negative, got %dx%d", w, h)
	}
	m := p.Margins()
	if m.Top < 0 || m.Bottom < 0 || m.Left < 0 || m.Right < 0 {
		t.Fatalf("margins must stay non-negative, got %+v", m)
	}
}

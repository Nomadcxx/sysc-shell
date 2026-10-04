package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestTooltipAtFindsTheInnermostHint(t *testing.T) {
	button := &ui.Node{Kind: ui.KindButton, Tooltip: "Delete note", Bounds: ui.Rect{X: 10, Y: 20, W: 36, H: 36}}
	root := &ui.Node{Kind: ui.KindColumn, Tooltip: "panel", Bounds: ui.Rect{W: 200, H: 100}, Children: []*ui.Node{
		{Kind: ui.KindRow, Bounds: ui.Rect{W: 200, H: 60}, Children: []*ui.Node{button}},
	}}
	if text, at := tooltipAt(root, 20, 30); text != "Delete note" || at != button.Bounds {
		t.Fatalf("over the button: %q at %+v, want its hint at %+v", text, at, button.Bounds)
	}
	if text, _ := tooltipAt(root, 150, 80); text != "panel" {
		t.Fatalf("off the button: %q, want the enclosing hint", text)
	}
	if text, _ := tooltipAt(root, 300, 300); text != "" {
		t.Fatalf("outside the panel: %q, want none", text)
	}
}

// A panel node's hint is placed on the output, so its surface rect moves by
// where the panel body sits there.
func TestPanelHoverArmsAHintAnchoredOnTheOutput(t *testing.T) {
	r := &Registry{dwell: newDwell(time.Millisecond)}
	t.Cleanup(r.dwell.stop)
	button := &ui.Node{Kind: ui.KindButton, Tooltip: "Open as sticky note", Bounds: ui.Rect{X: 10, Y: 20, W: 36, H: 36}}
	h := &PanelHost{
		output: 7,
		rect:   ui.Rect{X: 400, Y: 48, W: 200, H: 100},
		place:  Placement{BarEdge: "top", Detached: true, Output: ui.Rect{W: 1536, H: 960}, Panel: ui.Rect{W: 200, H: 100}},
		root:   &ui.Node{Kind: ui.KindColumn, Bounds: ui.Rect{W: 200, H: 100}, Children: []*ui.Node{button}},
		hoverX: 20, hoverY: 30,
	}
	h.driveTooltip(r)
	select {
	case req := <-r.dwell.requests():
		want := ui.Rect{X: 410, Y: 68, W: 36, H: 36}
		if req.Text != "Open as sticky note" || req.Anchor != want || !req.OnOutput || req.Global != 7 {
			t.Fatalf("request %+v, want the hint at %+v on output 7", req, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hovering a node with a hint armed nothing")
	}
}

// A bar widget's anchor is in the bar surface, so a bottom bar shifts it down
// the output; a panel node's is already on the output and must not move.
func TestAnOutputAnchorIgnoresTheBottomBarOffset(t *testing.T) {
	t.Parallel()
	r, h, hh := newTooltipFixture(t, true)
	// Take the registry lock: NewRegistry already runs the media relay
	// goroutine, which reads r.cfg under r.mu (viewLocked). Writing the
	// config from the test without the lock is a data race the detector
	// catches deterministically with -race -count.
	r.mu.Lock()
	r.cfg.Bar.Edge = "bottom"
	r.mu.Unlock()
	anchor := ui.Rect{X: 700, Y: 500, W: 36, H: 36}
	h.show(tooltipRequest{Global: 1, Anchor: anchor, Text: "Delete note", OnOutput: true})
	spec := onlyOpen(t, hh)
	if bottom := int(spec.MarginTop + spec.Height); bottom > anchor.Y || bottom < anchor.Y-2*tooltipGap {
		t.Fatalf("card spans %d..%d, want it just above the node at y=%d", spec.MarginTop, bottom, anchor.Y)
	}
}

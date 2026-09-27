package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The dwell timer must not fire immediately: a tooltip on every pointer
// crossing would flicker across the whole bar.
func TestADwellRequestArrivesOnlyAfterTheDelay(t *testing.T) {
	t.Parallel()
	d := newDwell(60 * time.Millisecond)
	t.Cleanup(d.stop)

	d.enter(1, ui.Rect{X: 10, Y: 0, W: 40, H: 44}, "Fixture tooltip", wayland.TooltipStyle{})

	select {
	case req := <-d.requests():
		t.Fatalf("a request arrived immediately: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case req := <-d.requests():
		if req.Text != "Fixture tooltip" || req.Global != 1 {
			t.Fatalf("request = %+v, want the entered widget", req)
		}
	case <-time.After(time.Second):
		t.Fatal("no request arrived after the dwell elapsed")
	}
}

// Leaving before the dwell elapses must cancel it outright.
func TestLeavingBeforeTheDwellCancelsIt(t *testing.T) {
	t.Parallel()
	d := newDwell(80 * time.Millisecond)
	t.Cleanup(d.stop)

	d.enter(1, ui.Rect{X: 10, Y: 0, W: 40, H: 44}, "Fixture tooltip", wayland.TooltipStyle{})
	d.leave()

	select {
	case req := <-d.requests():
		if req.Text != "" {
			t.Fatalf("a cancelled dwell produced a show request: %+v", req)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// Leaving after the tooltip is up must ask for it to be hidden.
func TestLeavingAfterTheDwellRequestsAHide(t *testing.T) {
	t.Parallel()
	d := newDwell(20 * time.Millisecond)
	t.Cleanup(d.stop)

	d.enter(1, ui.Rect{X: 10, Y: 0, W: 40, H: 44}, "Fixture tooltip", wayland.TooltipStyle{})
	<-d.requests() // the show
	d.leave()

	select {
	case req := <-d.requests():
		if req.Text != "" {
			t.Fatalf("leave produced %+v, want a hide", req)
		}
	case <-time.After(time.Second):
		t.Fatal("leaving produced no hide request")
	}
}

// Moving to another widget replaces the pending tooltip rather than queueing.
func TestMovingToAnotherWidgetReplacesThePending(t *testing.T) {
	t.Parallel()
	d := newDwell(40 * time.Millisecond)
	t.Cleanup(d.stop)

	d.enter(1, ui.Rect{X: 10, Y: 0, W: 40, H: 44}, "first", wayland.TooltipStyle{})
	d.enter(1, ui.Rect{X: 60, Y: 0, W: 40, H: 44}, "second", wayland.TooltipStyle{})

	select {
	case req := <-d.requests():
		if req.Text != "second" {
			t.Fatalf("request = %q, want the widget the pointer is on now", req.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("no request arrived")
	}
}

func TestDwellEnterRootShowsAStructuredTooltip(t *testing.T) {
	t.Parallel()
	d := newDwell(20 * time.Millisecond)
	t.Cleanup(d.stop)
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Humidity"},
		{Kind: ui.KindText, Text: "40%"},
	}}
	d.enterRoot(2, ui.Rect{X: 8, Y: 0, W: 40, H: 44}, root, wayland.TooltipStyle{})
	select {
	case req := <-d.requests():
		if req.Root == nil || req.Text != "" || req.Global != 2 {
			t.Fatalf("request = %+v", req)
		}
		if req.Root.Children[0].Text != "Humidity" {
			t.Fatalf("tree = %+v", req.Root)
		}
	case <-time.After(time.Second):
		t.Fatal("no structured tooltip request")
	}
}

func TestDwellEnterRootLeaveHides(t *testing.T) {
	t.Parallel()
	d := newDwell(20 * time.Millisecond)
	t.Cleanup(d.stop)
	d.enterRoot(1, ui.Rect{W: 10, H: 10}, &ui.Node{Kind: ui.KindColumn}, wayland.TooltipStyle{})
	<-d.requests()
	d.leave()
	select {
	case req := <-d.requests():
		if req.Text != "" || req.Root != nil {
			t.Fatalf("leave produced %+v, want a hide", req)
		}
	case <-time.After(time.Second):
		t.Fatal("leaving produced no hide request")
	}
}

func TestDwellEnterRootReplacesThePendingTree(t *testing.T) {
	t.Parallel()
	d := newDwell(40 * time.Millisecond)
	t.Cleanup(d.stop)
	d.enterRoot(1, ui.Rect{W: 10, H: 10}, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{{Kind: ui.KindText, Text: "first"}}}, wayland.TooltipStyle{})
	d.enterRoot(1, ui.Rect{W: 10, H: 10}, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{{Kind: ui.KindText, Text: "second"}}}, wayland.TooltipStyle{})
	select {
	case req := <-d.requests():
		if req.Root == nil || req.Root.Children[0].Text != "second" {
			t.Fatalf("request = %+v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("no request arrived")
	}
}

func TestMotionOverTheSameWidgetDoesNotRestartTheDwell(t *testing.T) {
	d := newDwell(time.Hour)
	t.Cleanup(d.stop)
	rect := ui.Rect{X: 10, Y: 0, W: 40, H: 44}
	d.enter(1, rect, "fixture", wayland.TooltipStyle{})

	d.mu.Lock()
	generation := d.generation
	d.mu.Unlock()

	d.enter(1, rect, "fixture", wayland.TooltipStyle{})
	d.mu.Lock()
	if d.generation != generation {
		d.mu.Unlock()
		t.Fatal("an identical request restarted the dwell")
	}
	d.mu.Unlock()

	d.enter(1, ui.Rect{X: 60, Y: 0, W: 40, H: 44}, "fixture", wayland.TooltipStyle{})
	d.mu.Lock()
	if d.generation == generation {
		d.mu.Unlock()
		t.Fatal("a different widget must restart the dwell")
	}
	d.mu.Unlock()

	// After a leave the memo is cleared, so re-entering re-arms once.
	d.leave()
	d.enter(1, rect, "fixture", wayland.TooltipStyle{})
	d.mu.Lock()
	afterLeave := d.generation
	d.mu.Unlock()
	d.enter(1, rect, "fixture", wayland.TooltipStyle{})
	d.mu.Lock()
	if d.generation != afterLeave {
		d.mu.Unlock()
		t.Fatal("identical re-entered requests must not restart the dwell")
	}
	d.mu.Unlock()
}

func TestStaleDwellCallbackDoesNotShowTooltip(t *testing.T) {
	d := newDwell(time.Hour)
	t.Cleanup(d.stop)
	d.enter(1, ui.Rect{X: 10, Y: 0, W: 40, H: 44}, "stale", wayland.TooltipStyle{})

	d.mu.Lock()
	generation := d.generation
	d.mu.Unlock()
	d.leave()
	d.fire(generation, wayland.TooltipRequest{Global: 1, Text: "stale"})

	select {
	case req := <-d.requests():
		t.Fatalf("stale callback produced request: %+v", req)
	default:
	}
}

func TestTooltipPaintsTheFloatingSurfaceRole(t *testing.T) {
	th := Theme{
		SurfaceContainerHigh: Color{R: 10, G: 20, B: 30, A: 255},
		OnSurface:            Color{R: 200, G: 210, B: 220, A: 255},
		Outline:              Color{R: 5, G: 6, B: 7, A: 9},
		Background:           Color{R: 1, G: 1, B: 1, A: 1},
		Surfaces:             Surfaces{Bar: 255, Overlay: 178},
	}
	style := tooltipStyleFor(th)
	if want := (Color{R: 10, G: 20, B: 30, A: 178}); style.Background != want {
		t.Fatalf("background = %+v, want the container high at overlay alpha %+v", style.Background, want)
	}
	if style.Foreground != th.OnSurface {
		t.Fatalf("foreground = %+v, want on-surface", style.Foreground)
	}
	if style.Border != th.Outline {
		t.Fatalf("border = %+v, want the outline token", style.Border)
	}
}

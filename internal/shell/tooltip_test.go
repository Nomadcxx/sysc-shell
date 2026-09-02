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

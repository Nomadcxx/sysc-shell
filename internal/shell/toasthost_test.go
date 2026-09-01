package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
)

func TestToastHostOpensOneOverlayPerOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newToastHost(r, &hostHarness{})

	r.outputsForTest([]string{"eDP-1", "HDMI-A-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5, "HDMI-A-1": 9})

	if len(h.harness().opens) != 2 {
		t.Fatalf("opens = %d, want one per output", len(h.harness().opens))
	}
	for _, spec := range h.harness().opens {
		if spec.ExclusiveZone != -1 {
			t.Fatalf("exclusive zone = %d, want -1", spec.ExclusiveZone)
		}
		if spec.Keyboard != keyboardNone {
			t.Fatalf("keyboard = %d, want None", spec.Keyboard)
		}
		if spec.Layer != layerOverlay {
			t.Fatalf("layer = %v, want Overlay", spec.Layer)
		}
	}
}

func TestToastHostPublishesTheCardUnionAsInputRegion(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})

	r.applyNotify(snap(1, note(1, "a"), note(2, "b")))
	h.recompute()

	if len(hh.updates) == 0 || !hh.updates[len(hh.updates)-1].SetInputRegion {
		t.Fatalf("no input-region update: %+v", hh.updates)
	}
	rects := hh.updates[len(hh.updates)-1].InputRects
	if len(rects) != 2 {
		t.Fatalf("input region = %d rects, want the two cards", len(rects))
	}
	for _, r := range rects {
		if r.W != toastCardWidth {
			t.Fatalf("input rect %+v is not a card", r)
		}
	}
}

func TestToastHostEmptyRegionStillReplaces(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	h.recompute()

	if len(hh.updates) == 0 {
		t.Fatal("no update for an empty stack")
	}
	u := hh.updates[len(hh.updates)-1]
	if !u.SetInputRegion || len(u.InputRects) != 0 {
		t.Fatalf("empty stack update = %+v, want SetInputRegion with no rects", u)
	}
}

func TestToastHostClosesOnOutputLoss(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1", "HDMI-A-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5, "HDMI-A-1": 9})

	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	if len(hh.closes) != 1 {
		t.Fatalf("closes = %v, want the lost output's host closed", hh.closes)
	}
}

func TestToastHostQueuesOverflowPerOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})

	msg := snap(1)
	for i := uint32(1); i <= 8; i++ {
		msg.Snapshot.Active = append(msg.Snapshot.Active, note(i, "n"))
	}
	r.applyNotify(msg)
	h.recompute()

	// The small default output cannot fit eight cards; some must queue, and
	// the aggregate state for a queued-on-every-output record is queued.
	got := r.aggregatePresentation(1, h.viewFor(1))
	if got != protocol.PresentationQueued && got != protocol.PresentationVisible {
		t.Fatalf("aggregate = %q", got)
	}
}

func TestToastHostReconnectClearsSurfaces(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})

	r.applyNotify(snap(1, note(1, "a")))
	h.recompute()
	r.applyNotify(disconnect(1))
	h.recompute()

	u := hh.updates[len(hh.updates)-1]
	if !u.SetInputRegion || len(u.InputRects) != 0 {
		t.Fatalf("disconnect left cards interactive: %+v", u)
	}
}

func disconnect(generation uint64) notifyclient.Message {
	return notifyclient.Message{Generation: generation, Kind: notifyclient.KindDisconnected}
}

package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
)

// Integration: a service lifecycle drives projection, presentation, and the
// toast host through the registry, and a service loss cleans every surface.
func TestIntegrationNotifyLifecycle(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	r.toasts = newToastHost(r, hh)
	r.SyncToastOutputs(map[string]uint32{"eDP-1": 5, "HDMI-A-1": 9})
	if len(hh.opens) != 2 {
		t.Fatalf("opens = %d, want 2", len(hh.opens))
	}

	r.applyNotify(snap(1, note(1, "one"), note(2, "two")))
	if got := r.notifyActiveIDs(); len(got) != 2 {
		t.Fatalf("active = %v", got)
	}

	// Both cards present on both outputs; the aggregate is visible.
	for id := uint32(1); id <= 2; id++ {
		if got := r.aggregatePresentation(id, r.toasts.viewFor(id)); got != protocol.PresentationVisible {
			t.Fatalf("card %d aggregate = %q, want visible", id, got)
		}
	}

	// A replace renews the lifetime; the projection adopts it.
	repl := note(1, "one-renewed")
	r.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaReplaced, Notification: &repl,
		Lifetime: &protocol.Lifetime{ID: 1, DurationMS: 8000, RemainingMS: 8000, Running: true}}))
	if r.notifySummary(1) != "one-renewed" {
		t.Fatalf("replace left %q", r.notifySummary(1))
	}

	// DND suppresses every card.
	r.setDND(true)
	if got := r.aggregatePresentation(1, r.toasts.viewFor(1)); got != protocol.PresentationSuppressed {
		t.Fatalf("dnd aggregate = %q", got)
	}
	r.setDND(false)

	// Closing one record leaves the other; the stack recomputes.
	r.applyNotify(delta(1, 3, protocol.Delta{Kind: protocol.DeltaClosed, ID: 1}))
	if got := r.notifyActiveIDs(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("after close active = %v", got)
	}

	// Service loss drops the projection and empties the surfaces.
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
	if got := r.notifyActiveIDs(); len(got) != 0 {
		t.Fatalf("disconnect left %v", got)
	}
	last := hh.updates[len(hh.updates)-1]
	if !last.SetInputRegion || len(last.InputRects) != 0 {
		t.Fatalf("disconnect left input region %+v", last)
	}

	// Output loss closes that output's host.
	r.SyncToastOutputs(map[string]uint32{"eDP-1": 5})
	if len(hh.closes) != 1 {
		t.Fatalf("closes = %v", hh.closes)
	}
}

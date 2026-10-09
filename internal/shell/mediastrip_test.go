package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func stripTestRegistry(t *testing.T, state services.MediaState) (*Registry, *mediaStripHost, *hostHarness) {
	t.Helper()
	r := newPanelRegistry(t)
	cfg := config.Default()
	cfg.Bar.Center = append(cfg.Bar.Center, config.Item{ID: "media"})
	withTestBar(t, r, 7, cfg)
	harness := &hostHarness{}
	r.mu.Lock()
	r.mediaState = state
	strip := newMediaStripHost(r, harness)
	r.mediaStrip = strip
	r.mu.Unlock()
	return r, strip, harness
}

func TestMediaStripPeekOpensOneSurfaceAndPinAddsTheShield(t *testing.T) {
	r, strip, harness := stripTestRegistry(t, services.MediaState{Available: true, Title: "Track", CanPause: true})
	r.mu.Lock()
	defer r.mu.Unlock()
	if !strip.openLocked(7, ui.Rect{X: 800, Y: 4, W: 160, H: 32}, false) {
		t.Fatal("peek did not open")
	}
	if len(harness.opens) != 1 || harness.opens[0].ID != mediaStripSurfaceID || harness.opens[0].Namespace != mediaStripNamespace {
		t.Fatalf("peek opens = %+v, want only the strip", harness.opens)
	}
	if !strip.togglePinLocked(7, ui.Rect{X: 800, Y: 4, W: 160, H: 32}) {
		t.Fatal("pin was not handled")
	}
	// Pinning reopens the strip above a fresh shield, so the shield is never on top.
	n := len(harness.opens)
	if n < 3 || harness.opens[n-2].ID != mediaStripShieldID || harness.opens[n-1].ID != mediaStripSurfaceID {
		t.Fatalf("pinned opens = %+v, want shield then strip last", harness.opens)
	}
	strip.togglePinLocked(7, ui.Rect{})
	if strip.open_ {
		t.Fatal("second click did not close the pinned strip")
	}
	closed := map[string]bool{}
	for _, id := range harness.closes {
		closed[id] = true
	}
	if !closed[mediaStripSurfaceID] || !closed[mediaStripShieldID] {
		t.Fatalf("closes = %v, want strip and shield", harness.closes)
	}
}

func TestMediaStripPeekDoesNotCloseAPinnedStrip(t *testing.T) {
	r, strip, _ := stripTestRegistry(t, services.MediaState{Available: true, Title: "Track"})
	r.mu.Lock()
	defer r.mu.Unlock()
	strip.openLocked(7, ui.Rect{X: 800, W: 160, H: 32}, true)
	strip.closePeekLocked()
	if !strip.open_ {
		t.Fatal("a hover grace closed the pinned strip")
	}
}

func TestMediaStripClosesWhenThePlayerGoes(t *testing.T) {
	r, strip, _ := stripTestRegistry(t, services.MediaState{Available: true, Title: "Track"})
	r.mu.Lock()
	defer r.mu.Unlock()
	strip.openLocked(7, ui.Rect{X: 800, W: 160, H: 32}, false)
	r.mediaState = services.MediaState{}
	if _, open := strip.refreshLocked(); open || strip.open_ {
		t.Fatal("strip stayed open with no player")
	}
}

func TestMediaStripReplacesAndIsReplacedByOtherRoots(t *testing.T) {
	r, strip, _ := stripTestRegistry(t, services.MediaState{Available: true, Title: "Track"})
	r.mu.Lock()
	strip.openLocked(7, ui.Rect{X: 800, W: 160, H: 32}, false)
	r.mu.Unlock()
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strip.open_ {
		t.Fatal("opening a panel left the strip open")
	}
}

func TestMediaStripKeysAndRelease(t *testing.T) {
	r, strip, _ := stripTestRegistry(t, services.MediaState{Available: true, Title: "Track", CanSeek: false, CanPause: true})
	r.mu.Lock()
	strip.openLocked(7, ui.Rect{X: 800, W: 160, H: 32}, true)
	before := strip.seekPending
	strip.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyRight})
	if strip.seekPending != before {
		r.mu.Unlock()
		t.Fatal("→ seeked a player that cannot seek")
	}
	strip.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc})
	open := strip.open_
	r.mu.Unlock()
	if open {
		t.Fatal("Escape did not close the strip")
	}
	// The lease is released off the owner; give it a moment, then expect no leak.
	time.Sleep(10 * time.Millisecond)
}

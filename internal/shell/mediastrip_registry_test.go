package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func mediaBarRegistry(t *testing.T) (*Registry, *Bar, *hostHarness) {
	t.Helper()
	r := newPanelRegistry(t)
	bar := &Bar{conn: "DP-1"}
	r.setTestBar(7, bar)
	harness := &hostHarness{}
	r.mu.Lock()
	// These tests install a synthetic snapshot directly, so stop the startup
	// relay before it can replace that snapshot with the unavailable service's
	// cached state.
	media := r.media
	if r.mediaRelayCancel != nil {
		close(r.mediaRelayCancel)
		r.mediaRelayCancel = nil
	}
	r.media = nil
	r.mediaState = services.MediaState{Available: true, Title: "Track", CanPause: true, Status: services.PlaybackPlaying}
	r.mediaStrip = newMediaStripHost(r, harness)
	r.bindBarPanelActionsLocked(7, bar)
	r.mu.Unlock()
	if media != nil {
		media.Close()
	}
	return r, bar, harness
}

func TestMediaPillPointerMap(t *testing.T) {
	r, bar, _ := mediaBarRegistry(t)
	if !bar.onAction(panelMediaAction, buttonLeft) {
		t.Fatal("left click on the pill was not handled")
	}
	r.mu.Lock()
	pinned := r.mediaStrip.open_ && r.mediaStrip.pinned
	r.mu.Unlock()
	if !pinned {
		t.Fatal("left click did not pin the strip")
	}
	if !bar.onAction(panelMediaAction, buttonLeft) {
		t.Fatal("second left click was not handled")
	}
	r.mu.Lock()
	open := r.mediaStrip.open_
	r.mu.Unlock()
	if open {
		t.Fatal("second left click did not close the strip")
	}
	if !bar.onAction(mediaPlayPauseAction, buttonLeft) {
		t.Fatal("the pill's play button was not handled")
	}
	if !bar.onAction(panelMediaAction, buttonMiddle) {
		t.Fatal("middle click no longer plays or pauses")
	}
	if !bar.onAction(panelMediaAction, buttonRight) {
		t.Fatal("right click was not handled")
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	if h == nil || h.section != "media" {
		t.Fatalf("right click opened %+v, want Control Centre › Media", h)
	}
	if bar.onAxis(panelMediaAction, 1) || bar.onAxis(panelMediaAction, -1) {
		t.Fatal("scrolling over the pill still does something; it must do nothing")
	}
}

func TestMediaStripClosesWithItsOutput(t *testing.T) {
	r, bar, harness := mediaBarRegistry(t)
	bar.onAction(panelMediaAction, buttonLeft)
	r.DropHost(7)
	r.mu.Lock()
	open := r.mediaStrip.open_
	r.mu.Unlock()
	if open {
		t.Fatal("strip outlived its output")
	}
	closed := map[string]bool{}
	for _, id := range harness.closes {
		closed[id] = true
	}
	if !closed[mediaStripSurfaceID] || !closed[mediaStripShieldID] {
		t.Fatalf("closes = %v, want strip and shield", harness.closes)
	}
}

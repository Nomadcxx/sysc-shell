package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func TestOpenPanelSendsShieldThenPanel(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{BarEdge: "top", BarZone: 40, Align: "center"}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if len(reqs) != 2 || reqs[0].Open == nil || reqs[1].Open == nil ||
		!strings.HasPrefix(reqs[0].Open.ID, "shield:") ||
		!strings.HasPrefix(reqs[1].Open.ID, "panel:") {
		t.Fatalf("expected shield then panel, got %+v", reqs)
	}
	if reqs[0].Open.ExclusiveZone != -1 || reqs[1].Open.ExclusiveZone != -1 {
		t.Fatal("both surfaces must use exclusive zone -1")
	}
	if reqs[1].Open.Keyboard != keyboardExclusive {
		t.Fatal("panel must request exclusive keyboard")
	}
}

func TestEscapeClosesPanel(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if !reg.Clock().Running() {
		t.Fatal("clock panel did not acquire a lease")
	}
	panel := reqs[1].Open
	if !panel.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: 1}) {
		t.Fatal("escape did not report a state change")
	}
	closes := drainAux(t, reg, 2)
	if len(closes) != 2 || closes[0].Open != nil || closes[1].Open != nil {
		t.Fatalf("expected two close requests, got %+v", closes)
	}
	if _, ok := reg.panels.Output(PanelClock); ok {
		t.Fatal("panel set still lists the closed clock")
	}
	if reg.Clock().Running() {
		t.Fatal("closing did not release the clock lease")
	}
}

func TestTabMovesRovingFocus(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	h := reg.panelHosts[PanelSession]
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyLeftShift})
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	if h.roving.Index() != 1 {
		t.Fatalf("focus index = %d, want 1", h.roving.Index())
	}
}

func TestSpaceActivatesFocusedButton(t *testing.T) {
	t.Parallel()
	old := runArgv
	t.Cleanup(func() { runArgv = old })
	runArgv = func([]string) error { return nil }
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keySpace})
	if _, ok := reg.panelHosts[PanelSession]; ok {
		t.Fatal("space did not activate the focused session action")
	}
}

func TestRevealAnimationInvalidatesUntilDone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	n := countSurfaceInvalidations(reg, 200*time.Millisecond)
	if n < 5 {
		t.Fatalf("got %d surface invalidations, want at least 5 during reveal", n)
	}

	still := config.Default()
	still.Accessibility.ReducedMotion = true
	quiet := NewRegistry(still)
	t.Cleanup(quiet.Close)
	if err := quiet.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	if got := countSurfaceInvalidations(quiet, 50*time.Millisecond); got != 1 {
		t.Fatalf("reduced motion produced %d invalidations, want 1", got)
	}
}

func TestTogglePanelByNameOpensSession(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.TogglePanelByName("session"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:session") {
		t.Fatalf("opened %q", reqs[1].Open.ID)
	}
}

func TestReloadKeepsOpenPanels(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, err := reg.PrepareConfig(config.Default(), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.panels.Output(PanelSession); !ok {
		t.Fatal("reload dropped the open panel from the set")
	}
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("reload sent an aux request: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestClosingDuringRevealStopsTicker(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.ClosePanel(PanelSession)
	drainInvalidations(reg)
	if got := countSurfaceInvalidations(reg, 50*time.Millisecond); got != 0 {
		t.Fatalf("ticker kept publishing after close: %d", got)
	}
}

func newPanelRegistry(t *testing.T) *Registry {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	return reg
}

func drainAux(t *testing.T, reg *Registry, n int) []wayland.AuxRequest {
	t.Helper()
	out := make([]wayland.AuxRequest, 0, n)
	deadline := time.After(time.Second)
	for len(out) < n {
		select {
		case req := <-reg.AuxRequests():
			out = append(out, req)
		case <-deadline:
			t.Fatalf("got %d aux requests, want %d", len(out), n)
		}
	}
	return out
}

func countSurfaceInvalidations(reg *Registry, d time.Duration) int {
	n := 0
	deadline := time.After(d)
	for {
		select {
		case inv := <-reg.Invalidations():
			if inv.SurfaceID != "" {
				n++
			}
		case <-deadline:
			return n
		}
	}
}

func drainInvalidations(reg *Registry) {
	for {
		select {
		case <-reg.Invalidations():
		default:
			return
		}
	}
}

func TestOpeningAnUnrelatedPanelClosesTheOldRoot(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, ok := reg.panelHosts[PanelClock]; !ok {
		t.Fatal("the first panel never opened")
	}

	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	// Closing the old root emits its panel and shield closes; the new root
	// then emits its own two opens.
	_ = drainAux(t, reg, 4)

	if _, ok := reg.panelHosts[PanelClock]; ok {
		t.Fatal("the replaced panel is still hosted")
	}
	if _, ok := reg.panels.Output(PanelClock); ok {
		t.Fatal("the replaced panel is still recorded as open")
	}
	if _, ok := reg.panelHosts[PanelSession]; !ok {
		t.Fatal("the new root did not open")
	}
	if !reg.roots.owns(panelRoot(PanelSession)) {
		t.Fatal("the chain owner is not the new panel")
	}
}

func TestMovingAPanelToAnotherOutputReplacesItsRoot(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	_, first, _ := reg.roots.current()

	if err := reg.OpenPanel(PanelClock, 8, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 4)

	_, second, ok := reg.roots.current()
	if !ok || second == first {
		t.Fatalf("moving outputs kept generation %d", second)
	}
	where, ok := reg.panels.Output(PanelClock)
	if !ok || where != 8 {
		t.Fatalf("panel output = %d (open=%v), want 8", where, ok)
	}
	if host := reg.panelHosts[PanelClock]; host == nil || host.output != 8 {
		t.Fatalf("panel host = %+v, want output 8", host)
	}
}

func TestTogglingTheSamePanelClosesItsRoot(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.TogglePanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if !reg.roots.owns(panelRoot(PanelMonitor)) {
		t.Fatal("toggling open did not publish a root")
	}

	if err := reg.TogglePanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, _, ok := reg.roots.current(); ok {
		t.Fatal("toggling closed left a root open")
	}
	if _, ok := reg.panelHosts[PanelMonitor]; ok {
		t.Fatal("toggling closed left the panel hosted")
	}
}

func TestEveryPanelCloseReleasesItsChainExactlyOnce(t *testing.T) {
	for name, closer := range map[string]func(*Registry){
		"ClosePanel":   func(r *Registry) { r.ClosePanel(PanelClock) },
		"TogglePanel":  func(r *Registry) { _ = r.TogglePanel(PanelClock, 7, Trigger{}) },
		"DropAux":      func(r *Registry) { r.DropAux(7, panelSurfaceID(PanelClock)) },
		"closeAll":     func(r *Registry) { r.mu.Lock(); r.closeAllPanelsLocked(); r.mu.Unlock() },
		"replacedRoot": func(r *Registry) { _ = r.OpenPanel(PanelSession, 7, Trigger{}) },
	} {
		t.Run(name, func(t *testing.T) {
			reg := newPanelRegistry(t)
			if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			_ = drainAux(t, reg, 2)

			released := 0
			_, generation, ok := reg.roots.current()
			if !ok {
				t.Fatal("no root was published")
			}
			reg.mu.Lock()
			reg.roots.onClose(generation, func() { released++ })
			reg.mu.Unlock()

			closer(reg)
			go func() {
				for range reg.AuxRequests() {
				}
			}()

			if released != 1 {
				t.Fatalf("chain released %d times, want exactly 1", released)
			}
			if _, ok := reg.panelHosts[PanelClock]; ok {
				t.Fatal("the panel host survived its close")
			}
			// A late close naming the released generation must do nothing.
			reg.mu.Lock()
			stale := reg.roots.closeRoot(generation)
			reg.mu.Unlock()
			if stale {
				t.Fatal("a stale close released the chain again")
			}
			if released != 1 {
				t.Fatalf("chain released %d times after a stale close", released)
			}
		})
	}
}

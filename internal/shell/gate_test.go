package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestGateAccessibleNamesAndRoles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Session.Locker = "swaylock"
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	for _, id := range []PanelID{PanelClock, PanelMonitor, PanelSession} {
		if err := reg.OpenPanel(id, 7, Trigger{}); err != nil {
			t.Fatal(err)
		}
		_ = drainAux(t, reg, 2)
		h := reg.panelHosts[id]
		for _, n := range ui.Focusables(h.root) {
			if n.Name == "" || n.Role == "" {
				t.Fatalf("%s focusable %q missing name=%q role=%q", id, n.Text, n.Name, n.Role)
			}
		}
		reg.ClosePanel(id)
		_ = drainAux(t, reg, 2)
	}
}

func TestGateKeyboardOnlyCoversControls(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Session.Locker = "swaylock"
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSession]
	want := h.roving.Count
	seen := map[int]bool{h.roving.Index(): true}
	handle := reqs[1].Open.Callbacks.Handle
	for i := 0; i < want+1; i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
		seen[h.roving.Index()] = true
	}
	if len(seen) != want {
		t.Fatalf("tab covered %d of %d focusables", len(seen), want)
	}
}

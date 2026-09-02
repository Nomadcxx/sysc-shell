package shell

import (
	"os/exec"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The catalogue's painter change is global: settings navigation, launcher
// results, notification actions, tray rows, and calendar navigation all
// inherit the new button geometry without being redesigned. This file is the
// evidence for that claim. It lays every panel out at its own narrowest
// supported width and fails if the inherited geometry no longer fits, so a
// consumer is only edited when something here proves it has to be.
func TestEveryPanelLaysOutAtItsNarrowestWidth(t *testing.T) {
	t.Parallel()
	for _, id := range []PanelID{PanelClock, PanelMonitor, PanelSession, PanelSettings, PanelLauncher} {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.Session.Locker = "swaylock"
			cfg.Accessibility.ReducedMotion = true
			reg := NewRegistry(cfg)
			reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
			t.Cleanup(reg.Close)

			if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
				t.Fatalf("open %s: %v", id, err)
			}
			_ = drainAux(t, reg, 2)
			h := reg.panelHosts[id]
			if h == nil {
				t.Fatalf("%s host is missing", id)
			}
			size := panelTargetSize(id)
			if err := h.configure(size.W, size.H, int(ui.ScaleUnit)); err != nil {
				t.Fatalf("%s does not lay out at %dx%d: %v", id, size.W, size.H, err)
			}
			assertLaidOut(t, id, h.root)
		})
	}
}

// assertLaidOut walks a laid-out tree and reports controls that were measured
// but never given a box, and any child painted outside its parent. Either is
// the clipping this slice is meant to catch.
func assertLaidOut(t *testing.T, id PanelID, root *ui.Node) {
	t.Helper()
	var walk func(*ui.Node, *ui.Node)
	walk = func(n, parent *ui.Node) {
		if n == nil {
			return
		}
		if interactive(n) && (n.Bounds.W <= 0 || n.Bounds.H <= 0) {
			t.Errorf("%s: control %q has no box: %+v", id, controlLabel(n), n.Bounds)
		}
		// Every composed glyph must be one the embedded subset carries. An
		// unknown name paints nothing, so it is a programmer error caught here
		// rather than an invisible control at runtime.
		if n.Kind == ui.KindIcon && !render.ValidMaterialIcon(n.Icon) {
			t.Errorf("%s: icon %q is outside the embedded subset", id, n.Icon)
		}
		if parent != nil && n.Bounds.W > 0 && parent.Bounds.W > 0 {
			if n.Bounds.X < parent.Bounds.X ||
				n.Bounds.X+n.Bounds.W > parent.Bounds.X+parent.Bounds.W {
				t.Errorf("%s: %q spills out of its parent: %+v in %+v",
					id, controlLabel(n), n.Bounds, parent.Bounds)
			}
		}
		for _, c := range n.Children {
			walk(c, n)
		}
	}
	walk(root, nil)
}

func controlLabel(n *ui.Node) string {
	if n.Name != "" {
		return n.Name
	}
	if n.Text != "" {
		return n.Text
	}
	return n.Action
}

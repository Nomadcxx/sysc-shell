package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// An open list stays on the panel: a menu near the lower edge opens upward
// when there is more room above, with every option it can show (sysc-2235).
func TestOpenMenuFitsThePanel(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(r)
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[PanelSettings]
	w, height := h.surfaceSize()
	if err := h.configure(w, height, 120); err != nil {
		t.Fatal(err)
	}
	h.section = "Bar"
	r.rebuildPanel(h)
	var trigger *ui.Node
	for _, n := range h.focus {
		if n.Kind == ui.KindMenu {
			trigger = n
			break
		}
	}
	if trigger == nil {
		t.Fatal("the Bar page has no menu")
	}
	path, _ := strings.CutPrefix(trigger.Action, "set:")
	m := h.menus[path]
	h.menu, h.menuPath = m, path
	m.Open()
	r.rebuildPanel(h)
	if err := h.configure(w, height, 120); err != nil {
		t.Fatal(err)
	}
	var open *ui.Node
	for _, n := range h.focus {
		if n.Kind == ui.KindMenu && n.Action == trigger.Action {
			open = n
		}
	}
	body := h.surfaceBody(h.logicalW, h.logicalH)
	popup := ui.MenuPopupBounds(open)
	if popup.Y < body.Y || popup.Y+popup.H > body.Y+body.H {
		t.Fatalf("popup %+v leaves the panel body %+v", popup, body)
	}
	if want := min(m.optionCount(), menuVisibleRows); len(open.Children) != want {
		t.Fatalf("popup shows %d options, want %d", len(open.Children), want)
	}
	if open.Bounds.Y+open.Bounds.H > body.Y+body.H-popup.H && popup.Y+popup.H > open.Bounds.Y {
		t.Fatalf("a low menu opened downward: trigger %+v popup %+v", open.Bounds, popup)
	}
	// The options still answer the pointer where they are drawn.
	last := open.Children[len(open.Children)-1]
	if !m.PickAt(open, last.Bounds.X+4, last.Bounds.Y+last.Bounds.H/2) {
		t.Fatal("the drawn option did not take the press")
	}
}

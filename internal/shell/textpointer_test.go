package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func pressAt(r *Registry, h *PanelHost, x, y int, mods ui.Mods) {
	handle := h.handle(r)
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: float64(x), Y: float64(y), Button: btnLeft, Mods: mods})
	handle(wayland.Event{Kind: wayland.EventPointerRelease, X: float64(x), Y: float64(y), Button: btnLeft, Mods: mods})
}

func TestClickPlacesCaretAndDoubleClickSelectsAWord(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("o"), txt("n"), txt("e"), txt(" "), txt("t"), txt("w"), txt("o"))
	r.mu.Lock()
	n := h.focused()
	box := render.FieldTextRect(n)
	r.mu.Unlock()
	y := box.Y + box.H/2
	pressAt(r, h, box.X+1, y, 0)
	r.mu.Lock()
	f := h.fieldFor(h.focused())
	if f.Cursor != 0 {
		r.mu.Unlock()
		t.Fatalf("click at the start put the caret at %d", f.Cursor)
	}
	r.mu.Unlock()
	h.clickAt = time.Now()
	pressAt(r, h, box.X+1, y, 0)
	r.mu.Lock()
	defer r.mu.Unlock()
	if got := h.fieldFor(h.focused()).SelectedText(); got != "one" {
		t.Fatalf("double click selected %q, want one", got)
	}
}

// Dragging with the button held extends the selection from the press.
func TestDragSelectsFromThePress(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("o"), txt("n"), txt("e"))
	r.mu.Lock()
	box := render.FieldTextRect(h.focused())
	r.mu.Unlock()
	y := float64(box.Y + box.H/2)
	handle := h.handle(r)
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: float64(box.X + 1), Y: y, Button: btnLeft})
	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(box.X + box.W - 1), Y: y})
	handle(wayland.Event{Kind: wayland.EventPointerRelease, X: float64(box.X + box.W - 1), Y: y, Button: btnLeft})
	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: float64(box.X + 1), Y: y})
	r.mu.Lock()
	defer r.mu.Unlock()
	if got := h.fieldFor(h.focused()).SelectedText(); got != "one" {
		t.Fatalf("drag selected %q, want one (and motion after release must not change it)", got)
	}
}

// A click in a plugin's text field focuses it and places the caret. It does
// not submit: Enter does that. Clicking to edit is routine now, and each
// click reaching the plugin as a submit would run its search or form.
func TestClickingAPluginFieldDoesNotSubmitIt(t *testing.T) {
	const pluginID = "org.sysc.shortcuts"
	reg := bindManifestPlugin(t, "ok", pluginID, testPanelShortcutManifest, []string{pluginID})
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")
	if _, err := reg.plugins.openPanel(pluginID, v1.PanelParams{Entry: "panel", Output: "DP-1", Generation: 7, Instance: pluginID + "-1"}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)

	reg.mu.Lock()
	host := reg.panelHosts[PanelPlugin]
	var editor *ui.Node
	for _, node := range host.focus {
		if node.Kind == ui.KindTextField {
			editor = node
		}
	}
	reg.mu.Unlock()
	if editor == nil {
		t.Fatal("helper panel has no text field")
	}
	before := len(reg.plugins.lastInputs())
	x, y := float64(editor.Bounds.X+editor.Bounds.W/2), float64(editor.Bounds.Y+editor.Bounds.H/2)
	handle := host.handle(reg)
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y, Button: btnLeft})
	handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y, Button: btnLeft})
	for _, input := range reg.plugins.lastInputs()[before:] {
		if input.Event == v1.EventSubmit {
			t.Fatalf("a click submitted the field: %+v", input)
		}
	}
}

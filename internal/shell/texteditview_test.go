package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Typing past the right edge of the clipboard search scrolls the painted
// copy so the caret stays in view.
func TestFocusedSearchScrollsToCaretOnPaint(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.clipboard = clipboardTestRegistry(nil).clipboard
	r.mu.Unlock()
	r.BindClipboard(&clipboardRecorder{})
	if err := r.OpenPanel(PanelClipboard, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, r, 2)
	h := r.panelHosts[PanelClipboard]
	if err := requests[1].Open.Callbacks.Configure(720, 560, 120); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		h.keyPress(r, 17) // KEY_W
	}
	r.mu.Lock()
	root := copyNode(h.root)
	h.applyEditorView(root)
	field := findEditing(root)
	r.mu.Unlock()
	if field == nil {
		t.Fatal("no editing field on the paint copy")
	}
	if field.ScrollX <= 0 {
		t.Fatalf("ScrollX = %d after 120 characters, want > 0", field.ScrollX)
	}
}

func findEditing(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindTextField && n.Editing {
		return n
	}
	for _, c := range n.Children {
		if f := findEditing(c); f != nil {
			return f
		}
	}
	return nil
}

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

// A field seeded from a node starts with its caret collapsed. An anchor left
// at zero would select everything before the caret, and the first key typed
// would replace a plugin's or a setting's prefilled value.
func TestSeededFieldsStartWithoutASelection(t *testing.T) {
	eds := map[string]*retainedEditor{}
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
		{Kind: ui.KindTextField, Key: "note", Action: "body", Text: "disk", Cursor: 4},
	}}
	overlayEditors(root, eds)
	if f := eds["note"].field; f.HasSelection() {
		s, e := f.Selection()
		t.Fatalf("plugin editor seeded with selection %d..%d", s, e)
	}
	h := &PanelHost{}
	setting := &ui.Node{Kind: ui.KindTextField, Action: "set:bar.label", Text: "clock", Cursor: 5}
	if f := h.fieldFor(setting); f == nil || f.HasSelection() {
		t.Fatalf("settings field seeded with a selection: %+v", f)
	}
}

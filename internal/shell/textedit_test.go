package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// keyEv is the press the platform delivers for an evdev code without a
// keymap: resolved through the US fallback, carrying the modifiers held.
func keyEv(code uint32, mods ui.Mods) wayland.Event {
	k := ui.FallbackKey(code, mods)
	return wayland.Event{Kind: wayland.EventKeyPress, Key: code, Sym: k.Sym, Text: k.Text, Mods: mods}
}

func openClipboardSearch(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
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
	return r, h
}

func typeKeys(r *Registry, h *PanelHost, ks ...ui.KeyInput) {
	for _, k := range ks {
		h.keyInput(r, k)
	}
}

func txt(s string) ui.KeyInput { return ui.KeyInput{Sym: uint32(s[0]), Text: s} }

func TestArrowsMoveTheCaretInsteadOfLeavingTheField(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("a"), txt("c"), ui.KeyInput{Sym: ui.SymLeft}, txt("b"))
	if h.query != "abc" {
		t.Fatalf("query = %q, want abc", h.query)
	}
	if got := h.focused(); got == nil || got.Name != "Search" {
		t.Fatalf("focus left the field: %+v", got)
	}
}

func TestCtrlShortcutsEditTheFocusedField(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("o"), txt("n"), txt("e"), txt(" "), txt("t"), txt("w"), txt("o"),
		ui.KeyInput{Sym: 'w', Text: "w", Mods: ui.ModCtrl})
	if h.query != "one " {
		t.Fatalf("after Ctrl+W query = %q", h.query)
	}
	typeKeys(r, h, ui.KeyInput{Sym: 'z', Text: "z", Mods: ui.ModCtrl})
	if h.query != "one two" {
		t.Fatalf("after Ctrl+Z query = %q", h.query)
	}
}

func TestTabStillLeavesTheField(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, ui.KeyInput{Sym: ui.SymTab, Code: keyTab})
	if got := h.focused(); got != nil && got.Name == "Search" {
		t.Fatal("Tab stayed in the search field")
	}
}

func TestCaretOnlyMoveKeepsTheQuery(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("a"), txt("b"))
	if !h.keyInput(r, ui.KeyInput{Sym: ui.SymLeft}) || h.query != "ab" {
		t.Fatalf("Left: query %q", h.query)
	}
	if f := h.fieldFor(h.focused()); f.Cursor != 1 {
		t.Fatalf("caret %d, want 1", f.Cursor)
	}
}

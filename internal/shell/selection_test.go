package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestCopyAndPasteRequestsReachThePlatform(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("h"), txt("i"), ui.KeyInput{Sym: 'a', Text: "a", Mods: ui.ModCtrl, Serial: 9},
		ui.KeyInput{Sym: 'c', Text: "c", Mods: ui.ModCtrl, Serial: 10},
		ui.KeyInput{Sym: 'v', Text: "v", Mods: ui.ModCtrl, Serial: 11})
	got := []wayland.SelectionRequest{nextSelection(t, r), nextSelection(t, r)}
	if got[0].Copy != "hi" || got[0].Serial != 10 || !got[1].Paste || got[1].Serial != 11 {
		t.Fatalf("requests %+v", got)
	}
}

// Review focus 3.
func TestPasteIntoSingleLineFieldFlattensAndIsOneUndoStep(t *testing.T) {
	r, h := openClipboardSearch(t)
	typeKeys(r, h, txt("x"))
	h.handle(r)(wayland.Event{Kind: wayland.EventPaste, Paste: "line one\nline\x00 two\r\nend"})
	if h.query != "xline one line two end" {
		t.Fatalf("query after paste = %q", h.query)
	}
	typeKeys(r, h, ui.KeyInput{Sym: 'z', Text: "z", Mods: ui.ModCtrl})
	if h.query != "x" {
		t.Fatalf("one undo after paste = %q, want x", h.query)
	}
}

// An open picker's filter well is a text field too: its copy and paste reach
// the clipboard, and a paste lands in the filter.
func TestMenuFilterCopiesAndPastes(t *testing.T) {
	r, h := openClipboardSearch(t)
	r.mu.Lock()
	h.menu = NewPicker([]string{"alpha", "beta"}, nil, 0)
	h.menu.Open()
	r.mu.Unlock()
	typeKeys(r, h, txt("a"), txt("l"),
		ui.KeyInput{Sym: 'a', Text: "a", Mods: ui.ModCtrl},
		ui.KeyInput{Sym: 'c', Text: "c", Mods: ui.ModCtrl, Serial: 20},
		ui.KeyInput{Sym: 'v', Text: "v", Mods: ui.ModCtrl, Serial: 21})
	got := []wayland.SelectionRequest{nextSelection(t, r), nextSelection(t, r)}
	if got[0].Copy != "al" || got[0].Serial != 20 || !got[1].Paste || got[1].Serial != 21 {
		t.Fatalf("requests %+v", got)
	}
	h.handle(r)(wayland.Event{Kind: wayland.EventPaste, Paste: "pha"})
	r.mu.Lock()
	defer r.mu.Unlock()
	if text := h.menu.filter.Text; text != "pha" {
		t.Fatalf("filter after paste over the selection = %q, want pha", text)
	}
}

func nextSelection(t *testing.T, r *Registry) wayland.SelectionRequest {
	t.Helper()
	select {
	case req := <-r.Selections():
		return req
	case <-time.After(time.Second):
		t.Fatal("no clipboard request reached the platform")
		return wayland.SelectionRequest{}
	}
}

package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAcceptSettingsConfiguresBarLive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	reloads := make(chan struct{}, 1)
	reg := newPanelRegistry(t)
	reg.BindPersist(p, reloads)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	h := reg.panelHosts[PanelSettings]
	for i := 0; i < 40 && (h.focused() == nil || h.focused().Name != "Height"); i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	}
	if h.focused() == nil || h.focused().Kind != ui.KindSlider || h.focused().Name != "Height" {
		t.Fatal("did not reach the bar height slider")
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyRight})
	got, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bar.Height == config.Default().Bar.Height {
		t.Fatal("slider did not persist a new bar height")
	}
	select {
	case <-reloads:
	default:
		t.Fatal("write did not signal reload")
	}
}

func TestAcceptKeyboardOnlyAllControls(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	h := reg.panelHosts[PanelSettings]
	if h.focused() == nil || h.focused().Kind != ui.KindTextField {
		t.Fatal("search field is not first focus")
	}
	handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "x"})
	if h.query != "x" {
		t.Fatalf("text field commit left query %q", h.query)
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc})

	for i := 0; i < 20 && (h.focused() == nil || h.focused().Kind != ui.KindToggle); i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	}
	if h.focused() == nil || h.focused().Kind != ui.KindToggle {
		t.Fatal("did not reach a toggle")
	}
	before := h.draft.Bar.Enabled
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keySpace})
	if h.draft.Bar.Enabled == before {
		t.Fatal("space did not flip the focused toggle")
	}

	for i := 0; i < 20 && (h.focused() == nil || h.focused().Kind != ui.KindSlider); i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	}
	if h.focused() == nil || h.focused().Kind != ui.KindSlider {
		t.Fatal("did not reach a slider")
	}
	slide := h.focused().Value
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyRight})
	if h.focused() == nil || h.focused().Value == slide {
		t.Fatal("right did not move the slider")
	}

	for i := 0; i < 20 && (h.focused() == nil || h.focused().Kind != ui.KindMenu); i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	}
	if h.focused() == nil || h.focused().Kind != ui.KindMenu {
		t.Fatal("did not reach a menu")
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keySpace})
	if h.menu == nil || !h.menu.Opened() {
		t.Fatal("space did not open the menu")
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc})

	s := findScroll(h.root)
	if s == nil {
		t.Fatal("settings tree has no scroll area")
	}
	if s.Kind != ui.KindVirtualList {
		t.Fatal("settings content is not a virtual list")
	}
	s.Bounds.H = 80
	if s.ItemHeight <= 0 {
		s.ItemHeight = 36
	}
	s.ContentH = s.ItemCount * s.ItemHeight
	if s.ContentH < 800 {
		s.ContentH = 800
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyPageDown})
	if s.ScrollOffset == 0 {
		t.Fatal("page down did not scroll the virtual list")
	}
}

func TestAcceptAccessibleNamesRoles4B(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]
	for _, n := range ui.Focusables(h.root) {
		if n.Name == "" || n.Role == "" {
			t.Fatalf("focusable %q missing name=%q role=%q", n.Text, n.Name, n.Role)
		}
	}
}

func TestAcceptOsdOnEachOutputExternalChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vol"), []byte("0.40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = get-volume ]; then printf 'Volume: %s\n' "$(cat '` + dir + `/vol')"; fi
`
	bin := filepath.Join(dir, "wpctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := newPanelRegistry(t)
	reg.setAudio(services.NewAudio(15*time.Millisecond, bin))
	reg.bars[1] = &Bar{conn: "DP-1"}
	reg.bars[2] = &Bar{conn: "DP-2"}
	time.Sleep(40 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "vol"), []byte("0.70\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case req := <-reg.AuxRequests():
			if req.Open != nil {
				seen[req.Open.ID] = true
			}
		case <-deadline:
			t.Fatalf("osd surfaces = %v, want osd:1 and osd:2", seen)
		}
	}
}

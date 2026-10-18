package shell

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPanelKeyboardLayoutIdentity(t *testing.T) {
	t.Parallel()
	if got := PanelKeyboardLayout.String(); got != "keyboard-layout" {
		t.Fatalf("panel name = %q", got)
	}
	if id, err := parsePanelName("keyboard-layout"); err != nil || id != PanelKeyboardLayout {
		t.Fatalf("parsePanelName = %v, %v", id, err)
	}
	if id, ok := panelIDFromAux("panel:keyboard-layout"); !ok || id != PanelKeyboardLayout {
		t.Fatalf("panelIDFromAux = %v, %v", id, ok)
	}
	if got := panelSurfaceID(PanelKeyboardLayout); got != "panel:keyboard-layout" {
		t.Fatalf("surface id = %q", got)
	}
}

func TestKeyboardLayoutBarItemShowsOnlyWithMultipleLayouts(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "keyboard-layout"}}, 6, standardMetrics())
	if len(widgets) != 1 || widgets[0].inner == nil || widgets[0].refresh == nil {
		t.Fatalf("keyboard-layout widgets = %+v, want one reactive widget", widgets)
	}
	w := widgets[0]
	w.refresh(barView{Layouts: niri.KeyboardLayouts{Names: []string{"English (US)"}, Current: 0}})
	if !w.node.Absent {
		t.Fatal("one configured layout should hide the optional bar item")
	}
	w.refresh(barView{Locale: "en", Layouts: niri.KeyboardLayouts{Names: []string{"English (US)", "German"}, Current: 1}})
	if w.node.Absent || w.inner.Text != "German" || !strings.Contains(w.inner.Name, "German") {
		t.Fatalf("bar item did not show the compositor's current layout: outer=%+v inner=%+v", w.node, w.inner)
	}
}

func TestKeyboardLayoutViewCopiesNiriState(t *testing.T) {
	r := newPanelRegistry(t)
	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German"}, Current: 1,
	}})

	r.mu.Lock()
	view := r.viewLocked("DP-1")
	r.mu.Unlock()
	if view.Layouts.Current != 1 || strings.Join(view.Layouts.Names, ",") != "English (US),German" {
		t.Fatalf("layout view = %+v", view.Layouts)
	}
	view.Layouts.Names[0] = "mutated"
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.niriSnapshot.Layouts.Names[0] != "English (US)" {
		t.Fatal("bar view aliases the compositor snapshot")
	}
}

func TestKeyboardLayoutPickerOpensFocusedOnCurrentLayout(t *testing.T) {
	_, h := openKeyboardLayoutPanel(t, 1)
	if focused := h.focused(); focused == nil || focused.Action != "keyboard-layout-select:1" {
		t.Fatalf("initial focus = %+v, want current layout option", focused)
	}
}

func TestKeyboardLayoutPickerUsesSessionLocale(t *testing.T) {
	r := newPanelRegistry(t)
	r.cfg.Session.Language = "es"
	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German"}, Current: 0,
	}})
	if err := r.OpenPanel(PanelKeyboardLayout, 7, Trigger{OutW: 1200, OutH: 800}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	node := findAction(r.panelHosts[PanelKeyboardLayout].root, "keyboard-layout-select:1")
	if node == nil || node.Name != "Seleccionar distribución: German" {
		t.Fatalf("localized layout option = %+v", node)
	}
}

func TestKeyboardLayoutBarActionTogglesPicker(t *testing.T) {
	r := newPanelRegistry(t)
	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German"}, Current: 0,
	}})
	bar := &Bar{conn: "DP-1"}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)
	r.mu.Unlock()

	if !bar.onAction(panelKeyboardLayoutAction, buttonLeft) {
		t.Fatal("layout action was not handled")
	}
	r.mu.Lock()
	h := r.panelHosts[PanelKeyboardLayout]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("bar action did not open the layout picker")
	}
	if !bar.onAction(panelKeyboardLayoutAction, buttonLeft) {
		t.Fatal("second layout action was not handled")
	}
	r.mu.Lock()
	h = r.panelHosts[PanelKeyboardLayout]
	r.mu.Unlock()
	if h != nil {
		t.Fatal("second layout action did not close the picker")
	}
}

func TestKeyboardLayoutPickerWaitsForNiriConfirmation(t *testing.T) {
	t.Parallel()
	r, h := openKeyboardLayoutPanel(t, 0)
	var sent []any
	r.niriSend = func(action any) error { sent = append(sent, action); return nil }
	node := findAction(h.root, "keyboard-layout-select:1")
	if node == nil || node.Name != "Select layout: German" || node.Role != "menuitem" || !node.Focusable {
		t.Fatalf("layout option = %+v", node)
	}
	r.mu.Lock()
	h.setFocus(node)
	handled := h.activate(r)
	requested := h.layoutRequested
	r.mu.Unlock()
	if !handled || requested != 1 {
		t.Fatalf("selection handled=%t, pending index=%d", handled, requested)
	}
	if len(sent) != 1 {
		t.Fatalf("Niri actions = %v, want one layout request", sent)
	}
	if got, ok := sent[0].(niri.SwitchLayout); !ok || got.Index != 1 {
		t.Fatalf("layout action = %#v, want SwitchLayout{Index: 1}", sent[0])
	}
	if got := r.niriSnapshot.Layouts.Current; got != 0 {
		t.Fatalf("request optimistically changed current index to %d", got)
	}
	if findAction(h.root, "keyboard-layout-select:0").Name != "Current layout: English (US)" {
		t.Fatal("picker reported success before a compositor event")
	}

	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{Names: []string{"English (US)", "German"}, Current: 1}})
	if h.layoutRequested != -1 {
		t.Fatalf("pending index after confirmation = %d, want none", h.layoutRequested)
	}
	if got := findAction(h.root, "keyboard-layout-select:1").Name; got != "Current layout: German" {
		t.Fatalf("confirmed current layout name = %q", got)
	}
	if h.layoutUnavailable {
		t.Fatal("confirmed layout change left the picker unavailable")
	}
	if node := findAction(h.root, "keyboard-layout-select:1"); node.State.Has(ui.StateDisabled) {
		t.Fatalf("confirmed layout remains disabled: %+v", node)
	}
}

func TestKeyboardLayoutPickerWaitsForTheRequestedIndex(t *testing.T) {
	r := newPanelRegistry(t)
	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German", "French"}, Current: 0,
	}})
	if err := r.OpenPanel(PanelKeyboardLayout, 7, Trigger{OutW: 1200, OutH: 800}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelKeyboardLayout]
	r.mu.Unlock()
	r.niriSend = func(any) error { return nil }
	node := findAction(h.root, "keyboard-layout-select:1")
	r.mu.Lock()
	h.setFocus(node)
	h.activate(r)
	r.mu.Unlock()

	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German", "French"}, Current: 2,
	}})
	r.mu.Lock()
	pending := h.layoutRequested
	status := findText(h.root, "Switching to German…")
	r.mu.Unlock()
	if pending != 1 || status == nil {
		t.Fatalf("unrelated layout event resolved request: pending=%d status=%+v", pending, status)
	}

	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German", "French"}, Current: 1,
	}})
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.layoutRequested != -1 || h.layoutUnavailable {
		t.Fatalf("requested layout confirmation left pending=%d unavailable=%t", h.layoutRequested, h.layoutUnavailable)
	}
	if node := findAction(h.root, "keyboard-layout-select:1"); node == nil || node.Name != "Current layout: German" {
		t.Fatalf("confirmed layout option = %+v", node)
	}
}

func TestKeyboardLayoutPickerExplainsUnavailableAction(t *testing.T) {
	t.Parallel()
	r, h := openKeyboardLayoutPanel(t, 0)
	r.niriSend = func(any) error { return errors.New("unsupported action") }
	node := findAction(h.root, "keyboard-layout-select:1")
	r.mu.Lock()
	h.setFocus(node)
	h.activate(r)
	r.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		done := h.layoutUnavailable
		r.mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !h.layoutUnavailable || findText(h.root, "Layout switching is unavailable") == nil {
		t.Fatal("refused optional action did not leave accessible unavailable feedback")
	}
	if r.niriSnapshot.Layouts.Current != 0 {
		t.Fatalf("failed request changed current index to %d", r.niriSnapshot.Layouts.Current)
	}
	if node := findAction(h.root, "keyboard-layout-select:1"); node == nil || !node.State.Has(ui.StateDisabled) {
		t.Fatalf("unavailable selection is not disabled: %+v", node)
	}
}

func TestKeyboardLayoutPickerConfirmationClearsUnavailableState(t *testing.T) {
	r, h := openKeyboardLayoutPanel(t, 0)
	r.niriSend = func(any) error { return errors.New("temporary action failure") }
	node := findAction(h.root, "keyboard-layout-select:1")
	r.mu.Lock()
	h.setFocus(node)
	h.activate(r)
	r.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		unavailable := h.layoutUnavailable
		r.mu.Unlock()
		if unavailable {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	wasUnavailable := h.layoutUnavailable
	r.mu.Unlock()
	if !wasUnavailable {
		t.Fatal("failed layout request never showed unavailable state")
	}
	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German"}, Current: 1,
	}})
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.layoutUnavailable || h.layoutRequested != -1 {
		t.Fatalf("confirmed state = unavailable %t, pending %d", h.layoutUnavailable, h.layoutRequested)
	}
	if node := findAction(h.root, "keyboard-layout-select:1"); node == nil || node.State.Has(ui.StateDisabled) {
		t.Fatalf("confirmed layout remains unavailable: %+v", node)
	}
}

func openKeyboardLayoutPanel(t *testing.T, current int) (*Registry, *PanelHost) {
	t.Helper()
	r := newPanelRegistry(t)
	r.UpdateNiri(niri.Snapshot{Layouts: niri.KeyboardLayouts{
		Names: []string{"English (US)", "German"}, Current: current,
	}})
	if err := r.OpenPanel(PanelKeyboardLayout, 7, Trigger{OutW: 1200, OutH: 800}); err != nil {
		t.Fatalf("open keyboard-layout panel: %v", err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelKeyboardLayout]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("keyboard-layout panel did not open")
	}
	return r, h
}

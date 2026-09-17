package shell

import (
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestSettingsSidebarSectionsAndFocus(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	tabs := byRole(h.root, "tab")
	want := settings.SectionNames()
	if len(tabs) != len(want) {
		t.Fatalf("sidebar tabs = %d, want the %d sections", len(tabs), len(want))
	}
	for i, name := range want {
		if tabs[i].Text != name {
			t.Fatalf("tab %d = %q, want %q", i, tabs[i].Text, name)
		}
	}
	focus := ui.Focusables(h.root)
	if len(focus) == 0 || focus[0].Kind != ui.KindTextField || focus[0].Name != "Search" {
		t.Fatalf("first focusable = %+v, want Search field", focus)
	}
}

func TestSettingsSearchSwapsSidebarForMatches(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "motion"})
	h := reg.panelHosts[PanelSettings]
	if tabs := byRole(h.root, "tab"); len(tabs) != 0 {
		t.Fatalf("search left %d sidebar tabs", len(tabs))
	}
	found := false
	for _, n := range walk(h.root) {
		if n.Text == "Reduced motion" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("search for motion did not list Reduced motion")
	}
}

func TestSettingsEntryRendersMatchingControl(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	seen := map[ui.Kind]bool{}
	for _, n := range walk(h.root) {
		seen[n.Kind] = true
	}
	if s := findScroll(h.root); s != nil && s.Item != nil {
		for i := 0; i < s.ItemCount; i++ {
			for _, n := range walk(s.Item(i)) {
				seen[n.Kind] = true
			}
		}
	}
	for _, k := range []ui.Kind{ui.KindToggle, ui.KindSlider, ui.KindMenu, ui.KindTextField} {
		if !seen[k] {
			t.Fatalf("Bar section missing kind %d", k)
		}
	}
}

func TestSettingsKeyboardOnlyTraversal(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	h := reg.panelHosts[PanelSettings]
	if h.focused() == nil || h.focused().Name != "Search" {
		t.Fatal("search field is not first focus")
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	if n := h.focused(); n == nil || n.Role != "tab" {
		t.Fatal("tab from search did not land on the sidebar")
	}
	sections := settings.SectionNames()
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown})
	if n := h.focused(); n == nil || n.Text != sections[1] {
		t.Fatalf("arrow moved to %q, want %q", nameOf(h.focused()), sections[1])
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyUp})
	if n := h.focused(); n == nil || n.Text != sections[0] {
		t.Fatalf("arrow moved to %q, want %q", nameOf(h.focused()), sections[0])
	}
	for i := 0; i < len(h.focus)+2 && (h.focused() == nil || h.focused().Kind != ui.KindToggle); i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	}
	n := h.focused()
	if n == nil || n.Kind != ui.KindToggle {
		t.Fatal("tab did not reach a toggle")
	}
	before := h.draft.Bar.Enabled
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keySpace})
	if h.draft.Bar.Enabled == before {
		t.Fatal("space did not flip the focused toggle")
	}
}

func TestSettingsApplyWritesConfig(t *testing.T) {
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
	for i := 0; i < len(h.focus)+2 && (h.focused() == nil || h.focused().Kind != ui.KindToggle); i++ {
		handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	}
	if h.focused() == nil || h.focused().Kind != ui.KindToggle {
		t.Fatal("did not reach a toggle")
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keySpace})
	got, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bar.Enabled {
		t.Fatal("write did not persist the toggled value")
	}
	select {
	case <-reloads:
	default:
		t.Fatal("write did not signal reload")
	}
}

func TestSettingsEscapeClearsQueryThenCloses(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "motion"})
	h := reg.panelHosts[PanelSettings]
	if h.query == "" {
		t.Fatal("query was not set")
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc})
	if _, ok := reg.panelHosts[PanelSettings]; !ok {
		t.Fatal("first escape closed the panel")
	}
	if h.query != "" {
		t.Fatalf("first escape left query %q", h.query)
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc})
	if _, ok := reg.panelHosts[PanelSettings]; ok {
		t.Fatal("second escape did not close the panel")
	}
}

func TestRegistryStatusReportsOpenPanelsAndTemplates(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	st := reg.Status()
	if st["version"] != "sysc-shell" {
		t.Fatalf("version = %v", st["version"])
	}
	if _, ok := st["audio"].(bool); !ok {
		t.Fatal("audio missing")
	}
	if _, ok := st["brightness"].(bool); !ok {
		t.Fatal("brightness missing")
	}
	if _, ok := st["matugen"].(bool); !ok {
		t.Fatal("matugen missing")
	}
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	st = reg.Status()
	panels, _ := st["panels"].([]string)
	found := false
	for _, p := range panels {
		if p == "settings" {
			found = true
		}
	}
	if !found {
		t.Fatalf("panels = %v, want settings", panels)
	}
	tm, _ := st["templates"].(map[string]bool)
	if !tm["niri"] {
		t.Fatalf("templates = %v, want niri on", tm)
	}
}

func newSettingsHost() *PanelHost {
	h := &PanelHost{
		id:      PanelSettings,
		set:     settings.Default(),
		draft:   config.Default(),
		section: "Bar",
		search:  ui.NewField(""),
		menus:   map[string]*Menu{},
		fields:  map[string]*ui.Field{},
	}
	h.root = settingsTree(nil, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}
	return h
}

func byRole(n *ui.Node, role string) []*ui.Node {
	var out []*ui.Node
	for _, c := range walk(n) {
		if c.Role == role {
			out = append(out, c)
		}
	}
	return out
}

func walk(n *ui.Node) []*ui.Node {
	if n == nil {
		return nil
	}
	out := []*ui.Node{n}
	for _, c := range n.Children {
		out = append(out, walk(c)...)
	}
	return out
}

func nameOf(n *ui.Node) string {
	if n == nil {
		return ""
	}
	return n.Text
}

// TestSettingsRowsCarryDescriptionsAndGroupHeadings is D3. KindVirtualList is
// strictly uniform stride — column.go boxes every item at ItemHeight and
// advances by it — so a caption under a label and a heading above a run of
// rows cannot exist under it. The pane lays the section out instead, which
// section partitioning keeps cheap.
func TestSettingsRowsCarryDescriptionsAndGroupHeadings(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	if findKind(h.root, ui.KindVirtualList) != nil {
		t.Fatal("settings still renders a virtual list, which cannot carry descriptions")
	}
	var caption, heading *ui.Node
	for _, n := range walk(h.root) {
		if n.Text == "" {
			continue
		}
		if n.TextRole == theme.RoleCaption && n.Tone == ui.ToneSubtle {
			caption = n
		}
		if n.TextRole == theme.RoleLabel {
			heading = n
		}
	}
	if caption == nil {
		t.Error("no row rendered a subtle caption description")
	}
	if heading == nil {
		t.Error("no group heading was rendered")
	}
	if findScroll(h.root) == nil {
		t.Error("the section does not scroll")
	}
}

// TestOnlyAChangedRowOffersItsReset is D5 at the surface: deviation is
// ambient, so a row that still sits on its default carries no reset and a
// changed one does. "What have I actually changed" is the question neither
// reference shell answers at rest.
func TestOnlyAChangedRowOffersItsReset(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	if got := byAction(h.root, "reset:bar.height"); got != nil {
		t.Fatal("an untouched row offered a reset")
	}

	h.draft.Bar.Height = h.draft.Bar.Height + 7
	h.root = settingsTree(nil, h)
	if got := byAction(h.root, "reset:bar.height"); got == nil {
		t.Fatal("a changed row did not reveal its reset")
	}
}

// TestResetReturnsTheRowToItsDefault drives the control rather than the
// registry, so the action wiring is covered and not just the resolution.
func TestResetReturnsTheRowToItsDefault(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]

	reg.mu.Lock()
	defer reg.mu.Unlock()
	want := h.draft.Bar.Height
	h.draft.Bar.Height = want + 7
	reg.rebuildPanel(h)
	reset := byAction(h.root, "reset:bar.height")
	if reset == nil {
		t.Fatal("changed row offered no reset")
	}
	h.setFocus(reset)
	h.activate(reg)
	if got := h.draft.Bar.Height; got != want {
		t.Fatalf("height = %d after reset, want the default %d", got, want)
	}
}

func byAction(n *ui.Node, action string) *ui.Node {
	for _, c := range walk(n) {
		if c.Action == action {
			return c
		}
	}
	return nil
}

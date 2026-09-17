package shell

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestSettingsSidebarSectionsAndFocus(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	// The rail's naming and glyphs are covered by the rail test; this one is
	// about the field reaching the keyboard first, so a user can type to find
	// a setting without first tabbing past twelve sections.
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
	// The rail stays put under search: a match is more useful when the user
	// can still see, and return to, the section it came from.
	if tabs := byRole(h.root, "tab"); len(tabs) != len(settings.SectionNames()) {
		t.Fatalf("search left %d rail tabs, want all of them", len(tabs))
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
	if n := h.focused(); n == nil || n.Name != sections[1] {
		t.Fatalf("arrow moved to %q, want %q", nameOf(h.focused()), sections[1])
	}
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyUp})
	if n := h.focused(); n == nil || n.Name != sections[0] {
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

// nameOf prefers the accessible name: a rail tab draws a glyph, so its Text
// is empty and only Name says which section it is.
func nameOf(n *ui.Node) string {
	switch {
	case n == nil:
		return ""
	case n.Name != "":
		return n.Name
	default:
		return n.Text
	}
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

// TestSettingsDebouncesFieldWrites is D4. Today every keystroke rewrites the
// whole configuration file. A toggle is one decision and writes at once; a
// field is a stream of them and writes once it settles.
func TestSettingsDebouncesFieldWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	reloads := make(chan struct{}, 8)
	reg := newPanelRegistry(t)
	reg.BindPersist(p, reloads)
	reg.writeDelay = 5 * time.Millisecond
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]

	field := &ui.Node{Kind: ui.KindTextField, Action: "set:session.locker"}
	for _, text := range []string{"one", "two", "three"} {
		reg.mu.Lock()
		field.Text = text
		h.applySetting(reg, field)
		reg.mu.Unlock()
	}
	if n := len(reloads); n != 0 {
		t.Fatalf("%d writes before the field settled", n)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(reloads) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(reloads); n != 1 {
		t.Fatalf("three keystrokes produced %d writes, want one", n)
	}
	got, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.Locker != "three" {
		t.Fatalf("file holds %q, want the last value typed", got.Session.Locker)
	}
}

// TestSettingsTogglesWriteAtOnce is the other half of D4: a decision the user
// has finished making must not wait on a timer.
func TestSettingsTogglesWriteAtOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	reloads := make(chan struct{}, 8)
	reg := newPanelRegistry(t)
	reg.BindPersist(p, reloads)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]

	reg.mu.Lock()
	h.applySetting(reg, &ui.Node{Kind: ui.KindToggle, Action: "set:accessibility.high-contrast", Value: 1})
	reg.mu.Unlock()
	if n := len(reloads); n != 1 {
		t.Fatalf("a toggle produced %d writes, want one immediately", n)
	}
	got, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accessibility.HighContrast {
		t.Fatal("the toggled value did not reach the file")
	}
}

// TestClosingSettingsFlushesAPendingWrite: a field that has not settled when
// the panel closes must not lose what was typed into it.
func TestClosingSettingsFlushesAPendingWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	reloads := make(chan struct{}, 8)
	reg := newPanelRegistry(t)
	reg.BindPersist(p, reloads)
	reg.writeDelay = time.Hour
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	reg.mu.Lock()
	h := reg.panelHosts[PanelSettings]
	h.applySetting(reg, &ui.Node{Kind: ui.KindTextField, Action: "set:session.locker", Text: "typed"})
	reg.closePanelLocked(PanelSettings)
	reg.mu.Unlock()

	got, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.Locker != "typed" {
		t.Fatalf("closing lost the pending value, file holds %q", got.Session.Locker)
	}
}

// TestSettingsRailCarriesAnIconPerSection: the rail mirrors the control centre
// so settings reads as the same product, and every section is reachable by
// keyboard with a name a screen reader can announce.
func TestSettingsRailCarriesAnIconPerSection(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	tabs := byRole(h.root, "tab")
	want := settings.SectionNames()
	if len(tabs) != len(want) {
		t.Fatalf("rail has %d tabs, want the %d sections", len(tabs), len(want))
	}
	for i, tab := range tabs {
		if tab.Name != want[i] {
			t.Errorf("tab %d is named %q, want %q", i, tab.Name, want[i])
		}
		if !tab.Focusable {
			t.Errorf("%s is not reachable by keyboard", want[i])
		}
		icon := findKind(tab, ui.KindIcon)
		if icon == nil {
			t.Errorf("%s has no icon", want[i])
			continue
		}
		if !render.ValidMaterialIcon(icon.Icon) {
			t.Errorf("%s draws %q, which the subset cannot shape", want[i], icon.Icon)
		}
	}
	selected := 0
	for _, tab := range tabs {
		if tab.Fill == ui.FillAccent {
			selected++
		}
	}
	if selected != 1 {
		t.Errorf("%d tabs read as selected, want exactly the open one", selected)
	}
}

// TestSearchGroupsMatchesUnderTheirSection: the shipped pane and Noctalia both
// replace the rail with a flat list, so a match gives no clue which section
// owns it. Grouping under the section heading is what makes search readable at
// two hundred entries.
func TestSearchGroupsMatchesUnderTheirSection(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.query = "motion"
	h.root = settingsTree(nil, h)

	headings := map[string]bool{}
	for _, n := range walk(h.root) {
		if n.TextRole == theme.RoleLabel && n.Text != "" {
			headings[n.Text] = true
		}
	}
	for _, want := range []string{"Appearance", "Accessibility"} {
		if !headings[want] {
			t.Errorf("no %q heading above its matches, headings were %v", want, headings)
		}
	}

	var labels []string
	for _, n := range walk(h.root) {
		if n.Text != "" {
			labels = append(labels, n.Text)
		}
	}
	if !slices.Contains(labels, "Reduced motion") {
		t.Errorf("the accessibility match was not listed, got %v", labels)
	}
}

// TestStepperMovesTheDraftByOneStep: a short range is worth a pixel at a time,
// which a slider a few pixels per step cannot give.
func TestStepperMovesTheDraftByOneStep(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	h.section = "Bar"
	reg.rebuildPanel(h)

	up := byAction(h.root, "step:up:bar.spacing")
	if up == nil {
		t.Fatal("bar.spacing rendered no stepper")
	}
	before := h.draft.Bar.Spacing
	h.setFocus(up)
	h.activate(reg)
	if got := h.draft.Bar.Spacing; got != before+1 {
		t.Fatalf("spacing = %d after one step, want %d", got, before+1)
	}
}

// TestInvalidHexLeavesTheDraftAlone: a colour is a validated field rather than
// a picker, so the validation is the whole safeguard.
func TestInvalidHexLeavesTheDraftAlone(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]

	if err := h.set.ByPath("appearance.source").Set(&h.draft, "hex"); err != nil {
		t.Fatal(err)
	}
	h.set = settings.DefaultFor(h.draft)
	seed := h.set.ByPath("appearance.seed")
	if seed.Kind != settings.KindHex {
		t.Fatalf("seed kind = %v, want a validated colour when the source is hex", seed.Kind)
	}

	want := h.draft.ThemeGen.Seed
	h.applySetting(reg, &ui.Node{Kind: ui.KindTextField, Action: "set:appearance.seed", Text: "not-a-colour"})
	if h.draft.ThemeGen.Seed != want {
		t.Fatalf("an invalid colour reached the draft as %q", h.draft.ThemeGen.Seed)
	}
	if h.errLabel == "" {
		t.Fatal("an invalid colour was rejected silently")
	}
	if !settingsValidHex("#7F67BE") || settingsValidHex("#xyzxyz") {
		t.Fatal("the field's own validation disagrees with the setter")
	}
}

// TestFontPickerListsDeduplicatedFamilies. The list is whatever this machine
// has, so what is asserted is the shape: no repeats and a stable order.
func TestFontPickerListsDeduplicatedFamilies(t *testing.T) {
	t.Parallel()
	families := settingsFontFamilies()
	seen := map[string]bool{}
	for _, f := range families {
		if seen[f] {
			t.Errorf("%q is listed more than once", f)
		}
		seen[f] = true
	}
	if !slices.IsSorted(families) {
		t.Error("families are not sorted, so the picker reorders itself between builds")
	}
}

// TestPathBrowseListsDirectories: os.ReadDir is the whole mechanism, and no
// portal is involved.
func TestPathBrowseListsDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"stills", "video", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a-file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := settingsBrowseOptions(root)
	for _, want := range []string{filepath.Join(root, "stills"), filepath.Join(root, "video"), filepath.Dir(root)} {
		if !slices.Contains(got, want) {
			t.Errorf("browse did not offer %q, got %v", want, got)
		}
	}
	for _, unwanted := range []string{filepath.Join(root, "a-file"), filepath.Join(root, ".hidden")} {
		if slices.Contains(got, unwanted) {
			t.Errorf("browse offered %q, which is not a directory to move into", unwanted)
		}
	}
}

// TestSettingsBodyLeavesRoomForTheRail: the rows right-pin their controls, so
// the column they sit in has to know how wide it actually is. Without that the
// controls pin to the panel's edge rather than the column's, and every enum
// pill and text field is clipped by the surface — which looks like nothing at
// all on a wide output and is obvious on a small one.
func TestSettingsBodyLeavesRoomForTheRail(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.place.Panel = ui.Rect{W: 900, H: 760}
	h.root = settingsTree(nil, h)

	body := findScroll(h.root)
	if body == nil {
		t.Fatal("no scrolling body")
	}
	if body.Width <= 0 {
		t.Fatal("the body has no width, so right-pinned controls pin to the panel edge")
	}
	if room := 900 - body.Width; room < settingsRailWidth {
		t.Errorf("body is %d wide of 900, leaving %d for a %d-wide rail and its gutter",
			body.Width, room, settingsRailWidth)
	}
}

// TestRowColumnsFitInsideTheBody is sysc-326. Neither column carried a width,
// so the description set the row's width and pushed the right-pinned control
// past the column's edge. A wide output has room to absorb that; a 1536-wide
// one at scale 1.25 clips every enum and field.
func TestRowColumnsFitInsideTheBody(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.place.Panel = ui.Rect{W: 900, H: 760}
	h.section = "Appearance"
	h.root = settingsTree(nil, h)

	body := settingsBodyWidth(h)
	rows := 0
	for _, n := range walk(h.root) {
		if n.Kind != ui.KindRow || len(n.Children) != 2 || !n.PinEnd {
			continue
		}
		label, trailing := n.Children[0], n.Children[1]
		if label.Kind != ui.KindColumn || label.Width <= 0 || trailing.Width <= 0 {
			continue
		}
		rows++
		if used := label.Width + trailing.Width; used > body {
			t.Errorf("a row measures %d wide inside a %d-wide column", used, body)
		}
	}
	if rows == 0 {
		t.Fatal("no sized rows were rendered")
	}
}

// TestFieldsFillTheirColumn: a fixed 200 read as a token field in a wide
// panel, whatever the surface was.
func TestFieldsFillTheirColumn(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.place.Panel = ui.Rect{W: 900, H: 760}
	h.section = "Session"
	h.root = settingsTree(nil, h)

	field := findKind(h.root, ui.KindTextField)
	if field == nil {
		t.Fatal("no field rendered")
	}
	if field.Width <= 200 {
		t.Errorf("field is %d wide; it should take the control column, not a fixed 200", field.Width)
	}
}

// TestRailTabsCarryTooltips: the rail draws glyphs only, so hovering has to
// say what each one is.
func TestRailTabsCarryTooltips(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	for _, tab := range byRole(h.root, "tab") {
		if tab.Tooltip != tab.Name {
			t.Errorf("tab %q has tooltip %q", tab.Name, tab.Tooltip)
		}
	}
}

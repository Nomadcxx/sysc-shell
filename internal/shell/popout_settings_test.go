package shell

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
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
	if s := findSettingsBody(h.root); s != nil && s.Item != nil {
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
		if n.TextRole == theme.RoleSection {
			heading = n
		}
	}
	if caption == nil {
		t.Error("no row rendered a subtle caption description")
	}
	if heading == nil {
		t.Error("no group heading was rendered")
	}
	if findSettingsBody(h.root) == nil {
		t.Error("the section does not scroll")
	}
}

func TestAppearancePagePolish(t *testing.T) {
	t.Parallel()
	h := &PanelHost{
		id: PanelSettings, set: settings.Default(), draft: config.Default(),
		menus: map[string]*Menu{}, fields: map[string]*ui.Field{},
	}
	h.place.Panel = ui.Rect{W: 1105, H: 760}
	h.set = settings.DefaultFor(h.draft)
	h.root = settingsTree(nil, h)

	for _, want := range []string{
		"theme and interface",
		"Choose a palette, then adjust type, surfaces, transparency, and motion.",
		"colours & mode",
		"style & layout",
		"typography & fonts",
		"corners & shape",
		"animation",
		"transparency",
		"blur & elevation",
		"Visual preset",
		"Control density",
		"Monospace font",
	} {
		if !strings.Contains(renderText(h.root), want) {
			t.Errorf("Appearance page is missing %q", want)
		}
	}

	var palette, mode *ui.Node
	pageHeading := false
	for _, n := range walk(h.root) {
		if isSettingsPageHeading(n, "Appearance") {
			pageHeading = true
		}
		if n.Action == "set:appearance.palette" && n.Kind == ui.KindMenu {
			palette = n
		}
		if n.Name == "Colour mode" && n.Kind == ui.KindSegmented {
			mode = n
		}
		if group := map[string]bool{
			"colours & mode": true, "style & layout": true, "typography & fonts": true,
			"corners & shape": true, "animation": true, "transparency": true,
			"blur & elevation": true,
		}[n.Text]; group {
			wantRole := theme.RoleSection
			if n.TextRole != wantRole || n.Bold || theme.TypeFor(n.TextRole).Weight < 700 || n.Tone != ui.ToneAccent {
				t.Errorf("Appearance heading %q has role=%v bold=%v weight=%d tone=%v, want semantic bold/accent",
					n.Text, n.TextRole, n.Bold, theme.TypeFor(n.TextRole).Weight, n.Tone)
			}
		}
	}
	if palette == nil {
		t.Fatal("Appearance palette did not render as a dropdown")
	}
	if !pageHeading {
		t.Fatal("Appearance page heading is not double-size, bold, and accented")
	}
	if palette.Width != 240 {
		t.Errorf("Appearance palette width = %d, want fixed width 240", palette.Width)
	}
	if want := h.metrics().ButtonPadding; palette.Padding != want {
		t.Errorf("Appearance dropdown padding = %d, want %d", palette.Padding, want)
	}
	if mode == nil {
		t.Fatal("Light/Dark mode is not a segmented control")
	}
	if mode.Width != 240 {
		t.Errorf("Light/Dark width = %d, want fixed width 240", mode.Width)
	}
	if want := h.metrics().CompactControl; mode.Height != want {
		t.Errorf("Light/Dark height = %d, want compact height %d", mode.Height, want)
	}
	seed := byAction(h.root, "set:appearance.seed")
	if seed == nil || seed.Kind != ui.KindTextField {
		t.Fatalf("Appearance seed field = %+v, want a text field", seed)
	}
	if want := h.metrics().ButtonPadding; seed.Padding != want {
		t.Errorf("Appearance field padding = %d, want %d", seed.Padding, want)
	}
	for _, n := range walk(h.root) {
		if n.Kind == ui.KindCapsule && n.Shape == ui.ShapeCard && n.Fill != ui.FillContainerHigh {
			t.Errorf("Appearance card fill = %v, want shaded container fill", n.Fill)
		}
	}

	if err := ui.LayoutColumn(h.root, h.place.Panel, settingsMeasure(h)); err != nil {
		t.Fatalf("layout Appearance: %v", err)
	}
	if len(mode.Children) != 2 || mode.Children[0].Bounds.W != mode.Children[1].Bounds.W {
		t.Errorf("Light/Dark segments are not equal width: %+v", mode.Children)
	}

	h.draft.ThemeGen.Source = "palette"
	h.draft.ThemeGen.Seed = theme.PaletteNames()[0]
	h.set = settings.DefaultFor(h.draft)
	h.root = settingsTree(nil, h)
	palette = byAction(h.root, "set:appearance.palette")
	if palette == nil || palette.Kind != ui.KindMenu {
		t.Fatalf("changed Appearance palette = %+v, want a dropdown", palette)
	}
	if palette.Width != 240 {
		t.Errorf("changed Appearance palette width = %d, want the same fixed width 240", palette.Width)
	}
	if byAction(h.root, "reset:appearance.palette") == nil {
		t.Fatal("changed Appearance palette did not retain its reset control")
	}
}

func TestSettingsSectionTitlesUseLowercase(t *testing.T) {
	t.Parallel()
	for _, section := range settings.SectionNames() {
		t.Run(section, func(t *testing.T) {
			h := newSettingsHost()
			h.section = section
			h.root = settingsTree(nil, h)

			var title *ui.Node
			for _, n := range walk(h.root) {
				if n.Kind == ui.KindText && n.Role == "heading" && n.Name == section {
					title = n
					break
				}
			}
			if title == nil {
				t.Fatalf("%s has no semantic section title", section)
			}
			if title.Text != strings.ToLower(section) {
				t.Errorf("%s title text = %q, want lowercase %q", section, title.Text, strings.ToLower(section))
			}
			if !isSettingsPageHeading(title, section) {
				t.Errorf("%s title is not a semantic bold heading: role=%v bold=%t tone=%v accessible-name=%q",
					section, title.TextRole, title.Bold, title.Tone, title.Name)
			}
			// Owner decision 2026-09-30 (appearance polish design): the
			// launcher's SYSC rail frames every section title, "////// title
			// //////", set left over the content rather than centred.
			var rail *ui.Node
			for _, n := range walk(h.root) {
				if n.Kind == ui.KindRow && len(n.Children) == 3 && n.Children[1] == title {
					rail = n
				}
			}
			if rail == nil {
				t.Fatalf("%s title is not framed by the slash rail", section)
			}
			for _, side := range []*ui.Node{rail.Children[0], rail.Children[2]} {
				if side.Text != launcherSlashRun || side.Tone != ui.ToneAccent || side.TextRole != title.TextRole || side.Name != "" {
					t.Errorf("%s rail side = %q tone=%v role=%v name=%q, want unnamed accent %q in the title's role",
						section, side.Text, side.Tone, side.TextRole, side.Name, launcherSlashRun)
				}
			}
			if rail.CenterX {
				t.Errorf("%s rail is centred, want it set left like the content", section)
			}
			if want := h.metrics().CardPadding; rail.Padding != want {
				t.Errorf("%s rail padding = %d, want the card padding %d so it lines up with card text", section, rail.Padding, want)
			}
		})
	}
}

func TestSettingsSectionCardsUseAppearanceHeadingStyle(t *testing.T) {
	t.Parallel()
	for _, section := range settings.SectionNames() {
		t.Run(section, func(t *testing.T) {
			h := newSettingsHost()
			h.set = nil
			page := settingsPageColumn(h, []settings.Entry{{
				Section: section, Group: "Example Group", Path: "example.enabled",
				Label: "Enabled", Kind: settings.KindBool,
			}})
			if len(page.Children) != 1 {
				t.Fatalf("%s page has %d groups, want one", section, len(page.Children))
			}
			heading := page.Children[0].Children[0].Children[0]
			if heading.Text != "example group" || heading.Name != "Example Group" {
				t.Errorf("%s group heading text/name=%q/%q, want lowercase display with canonical accessible name", section, heading.Text, heading.Name)
			}
			if heading.TextRole != theme.RoleSection || heading.Bold || theme.TypeFor(heading.TextRole).Weight < 700 || heading.Tone != ui.ToneAccent || heading.Role != "heading" {
				t.Errorf("%s group heading role=%v bold=%t weight=%d tone=%v semantic-role=%q, want semantic bold/accent",
					section, heading.TextRole, heading.Bold, theme.TypeFor(heading.TextRole).Weight, heading.Tone, heading.Role)
			}
		})
	}
}

// TestSettingsControlsEndAtTheCardEdge is sysc-858's alignment rule: every
// row's control ends at the row's inner right edge, with Reset directly to its
// left. A default toggle used to sit at the control column's left edge and a
// changed one at the card edge, so a control jumped across the card when its
// row gained Reset; a field without Reset stopped one margin short.
func TestSettingsControlsEndAtTheCardEdge(t *testing.T) {
	t.Parallel()
	for _, section := range []string{"Appearance", "Templates", "Wallpaper", "Bar", "Widgets", "Tray", "Panels"} {
		t.Run(section, func(t *testing.T) {
			h := newSettingsHost()
			h.section = section
			h.place.Panel = ui.Rect{W: 1105, H: 760}
			// One changed row per page, so each page lays out rows with
			// and without Reset side by side.
			h.draft.Templates = map[string]bool{"kitty": true}
			h.draft.Wallpaper.FadeDuration = 1.5
			h.draft.Panels.Gap = 4
			h.draft.Panels.OSD = "top-left"
			h.draft.Bar.Enabled = false
			h.draft.Tray.Hidden = []string{"id:blueman"}
			h.draft.Tray.Pinned = []string{"id:nm-applet"}
			h.draft.ThemeGen.Source = "palette"
			h.draft.ThemeGen.Seed = theme.PaletteNames()[0]
			h.draft.Bar.Left = append(h.draft.Bar.Left, config.Item{ID: "window-title", MaxWidth: 300})
			h.set = settings.DefaultFor(h.draft)
			h.root = settingsTree(nil, h)
			if err := ui.LayoutColumn(h.root, h.place.Panel, settingsMeasure(h)); err != nil {
				t.Fatalf("layout %s: %v", section, err)
			}

			rows, resets := 0, 0
			for _, row := range walk(h.root) {
				path := settingsRowPath(row)
				if path == "" {
					continue
				}
				rows++
				trailing := row.Children[1]
				control := trailing.Children[len(trailing.Children)-1]
				edge := row.Bounds.X + row.Bounds.W - row.Padding
				if got := control.Bounds.X + control.Bounds.W; got != edge {
					t.Errorf("%s: control ends at %d, want the row edge %d", path, got, edge)
				}
				if len(trailing.Children) == 2 {
					resets++
					reset := trailing.Children[0]
					if gap := control.Bounds.X - (reset.Bounds.X + reset.Bounds.W); gap != trailing.Gap {
						t.Errorf("%s: Reset sits %d from its control, want %d", path, gap, trailing.Gap)
					}
				}
			}
			if rows == 0 {
				t.Fatalf("%s rendered no setting rows", section)
			}
			if section != "Bar" && resets == 0 {
				t.Errorf("%s rendered no changed row to compare against", section)
			}
		})
	}
}

// TestSettingsDropdownsAndSegmentsMatchAppearance carries the accepted
// Appearance controls to every page: a dropdown keeps the fixed 240 width that
// earns it a chevron, and a segmented control's options share its width
// equally.
func TestSettingsDropdownsAndSegmentsMatchAppearance(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Panels"
	h.place.Panel = ui.Rect{W: 1105, H: 760}
	h.set = settings.DefaultFor(h.draft)
	h.root = settingsTree(nil, h)
	osd := byAction(h.root, "set:panels.osd")
	if osd == nil || osd.Kind != ui.KindMenu {
		t.Fatalf("OSD position = %+v, want a dropdown", osd)
	}
	if osd.Width != settingsDropdownFixedWidth {
		t.Errorf("OSD position width = %d, want %d", osd.Width, settingsDropdownFixedWidth)
	}
	if got := h.menus["panels.osd"].Value(); got != "bottom-center" {
		t.Errorf("OSD position holds %q, want the stored value bottom-center", got)
	}

	h.section = "Wallpaper"
	h.root = settingsTree(nil, h)
	if err := ui.LayoutColumn(h.root, h.place.Panel, settingsMeasure(h)); err != nil {
		t.Fatalf("layout Wallpaper: %v", err)
	}
	for _, name := range []string{"Scaling", "Frame cap", "When occluded"} {
		var seg *ui.Node
		for _, n := range walk(h.root) {
			if n.Kind == ui.KindSegmented && n.Name == name {
				seg = n
			}
		}
		if seg == nil {
			t.Fatalf("Wallpaper %s is not a segmented control", name)
		}
		if seg.Width < settingsDropdownFixedWidth {
			t.Errorf("%s width = %d, want at least %d", name, seg.Width, settingsDropdownFixedWidth)
		}
		for _, s := range seg.Children[1:] {
			if d := s.Bounds.W - seg.Children[0].Bounds.W; d < -1 || d > 1 {
				t.Errorf("%s segments differ in width: %d vs %d", name, s.Bounds.W, seg.Children[0].Bounds.W)
			}
		}
		for _, s := range seg.Children {
			if textW, _ := settingsMeasure(h)(s.Name, ui.TextAttrs{}); textW > s.Bounds.W {
				t.Errorf("%s segment %q is %d wide, narrower than its label %d", name, s.Name, s.Bounds.W, textW)
			}
		}
	}
}

func TestCompactBarEdgeMenuSelectsEveryEdge(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Bar"
	h.place.Panel = settingsPanelSize(800, 500)
	h.root = settingsTree(nil, h)
	if err := ui.LayoutColumn(h.root, h.place.Panel, settingsMeasure(h)); err != nil {
		t.Fatalf("layout compact Bar settings: %v", err)
	}

	e := h.set.ByPath("bar.edge")
	if e == nil {
		t.Fatal("missing bar.edge setting")
	}
	n := byAction(h.root, "set:bar.edge")
	if n == nil || n.Kind != ui.KindMenu {
		t.Fatalf("bar.edge control = %+v, want a compact menu", n)
	}
	m := h.menus[e.Path]
	if m == nil {
		t.Fatal("bar.edge menu was not retained")
	}
	m.Open()
	choices := m.Node().Children
	if len(choices) != len(e.Options) {
		t.Fatalf("menu exposes %d choices, want %d", len(choices), len(e.Options))
	}
	for i, label := range e.OptionLabels {
		if choices[i].Text != label {
			t.Errorf("menu choice %d = %q, want %q", i, choices[i].Text, label)
		}
	}
	m.Cancel()
	for i, edge := range e.Options {
		m.Open()
		if i > 0 {
			m.Next()
		}
		m.Select()
		if got := m.Value(); got != edge {
			t.Fatalf("menu selection %d = %q, want %q", i, got, edge)
		}
		if err := e.Set(&h.draft, m.Value()); err != nil || e.Get(h.draft) != edge {
			t.Fatalf("apply edge %q: getter=%q err=%v", edge, e.Get(h.draft), err)
		}
	}
}

// TestSettingsTextFieldsTakeTheControlHeight: the rail search and the plugin
// source fields used to size to their text, a strip about 22 px tall with the
// first glyph against the rounded edge, while the plugin store's search was a
// full control. Every Settings text field takes the button inset, and with it
// at least a standard control's height.
func TestSettingsTextFieldsTakeTheControlHeight(t *testing.T) {
	reg, h, _ := openPluginManagerTestPanelOn(t, config.Default(), store.State{}, ui.Rect{W: 1536, H: 864})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.pluginManagerTab = "sources"
	reg.rebuildPanel(h)
	m := h.metrics()
	var search *ui.Node
	for _, n := range walk(h.root) {
		if n.Kind == ui.KindTextField && n.Name == "Search" {
			search = n
		}
	}
	for name, n := range map[string]*ui.Node{
		"rail search":    search,
		"source name":    byAction(h.root, "plugins-source-name"),
		"repository URL": byAction(h.root, "plugins-source-url"),
	} {
		if n == nil {
			t.Fatalf("%s field missing", name)
		}
		if n.Padding != m.ButtonPadding {
			t.Errorf("%s padding = %d, want the button inset %d", name, n.Padding, m.ButtonPadding)
		}
		if n.Bounds.H < m.StandardControl {
			t.Errorf("%s is %d tall, want at least the standard control %d", name, n.Bounds.H, m.StandardControl)
		}
	}
	if search.Placeholder == "" {
		t.Error("rail search has no placeholder saying what it searches")
	}
}

// TestBarLayoutTitleUsesTheCardHeading keeps the lane editor's title in the
// same hierarchy as every other Settings card.
func TestBarLayoutTitleUsesTheCardHeading(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	var title *ui.Node
	for _, n := range walk(barLaneStrip(h)) {
		if n.Kind == ui.KindText && n.Name == "Layout" {
			title = n
		}
	}
	if title == nil {
		t.Fatal("Bar Layout has no title named Layout")
	}
	if title.Text != "layout" || title.Role != "heading" || title.TextRole != theme.RoleSection || title.Tone != ui.ToneAccent {
		t.Errorf("Bar Layout title text=%q role=%q text-role=%v tone=%v, want the card heading",
			title.Text, title.Role, title.TextRole, title.Tone)
	}
}

func isSettingsPageHeading(n *ui.Node, section string) bool {
	return n != nil && n.Kind == ui.KindText && n.Text == strings.ToLower(section) && n.Name == section &&
		n.Role == "heading" && n.TextRole == theme.RolePage && !n.Bold &&
		theme.TypeFor(n.TextRole).Weight >= 700 && n.Tone == ui.ToneAccent
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

func TestAppearanceResetUsesOutlinedButtonBesideSegments(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Appearance"
	e := h.set.ByPath("appearance.source")
	if e == nil {
		t.Fatal("appearance.source is not registered")
	}
	current := e.Get(h.draft)
	changed := false
	for _, option := range e.Options {
		if option != current {
			if err := e.Set(&h.draft, option); err != nil {
				t.Fatal(err)
			}
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("appearance.source has no alternate option")
	}
	row := settingsEntryRow(h, *e, 600)
	reset := byAction(row, "reset:appearance.source")
	segments := findNode(row, func(n *ui.Node) bool {
		return n.Kind == ui.KindSegmented && n.Key == "seg:appearance.source"
	})
	if reset == nil || segments == nil {
		t.Fatalf("theme source controls: reset=%+v segments=%+v", reset, segments)
	}
	if reset.Fill != ui.FillOutline {
		t.Errorf("Reset fill = %v, want outlined chrome beside the option group", reset.Fill)
	}
}

func TestTemplatesShareTheirDescriptionAndRemainSearchable(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Templates"
	h.root = settingsTree(nil, h)

	const description = "Write this application's colours when the theme changes."
	count := 0
	for _, n := range walk(h.root) {
		if n.Text == description {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("template description appears %d times, want once in the group card", count)
	}
	if got := len(h.set.Search("colours when the theme changes")); got < 2 {
		t.Fatalf("description search returned %d template entries, want several", got)
	}
}

func TestTraySettingsUseLiveNamesAndDisambiguateFallbacks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Tray.Hidden = []string{"id:org.blueman", "id:blueman", "title:blueman"}
	h := newSettingsHost()
	h.set = settings.DefaultFor(cfg)
	h.draft = cfg
	h.section = "Tray"
	r := &Registry{tray: newTrayState()}
	key := tray.ItemKey{}
	r.tray.items[key] = tray.Item{ID: "org.blueman", Title: "Bluetooth"}
	r.tray.order = []tray.ItemKey{key}
	h.root = settingsTree(r, h)

	got := map[string]bool{}
	for _, n := range walk(h.root) {
		if n.Role == "heading" && n.TextRole == theme.RoleSection {
			got[n.Name] = true
		}
	}
	for _, want := range []string{"Bluetooth", "blueman (app ID)", "blueman (title)"} {
		if !got[want] {
			t.Errorf("tray card title %q missing from %v", want, got)
		}
	}
}

func TestMonitorColorControlsUseReadableRolesAndThemeSwatches(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	for _, e := range h.set.Section("Monitor") {
		if e.Group != "Colours" {
			continue
		}
		control := settingsControl(h, e, settingsDropdownFixedWidth)
		menu := findNode(control, func(n *ui.Node) bool { return n.Kind == ui.KindMenu })
		if menu == nil || menu.Text != settingsOptionLabel(e.Get(h.draft)) {
			t.Errorf("%s selected label = %q, want %q", e.Path, textOf(menu), settingsOptionLabel(e.Get(h.draft)))
		}
		role, ok := ui.PaintRoleFor(e.Get(h.draft))
		if !ok {
			t.Fatalf("%s has unknown paint role %q", e.Path, e.Get(h.draft))
		}
		swatch := findNode(control, func(n *ui.Node) bool { return n.Kind == ui.KindCapsule && n.Fill == ui.FillRole })
		if swatch == nil || swatch.FillRole != role || swatch.Role != "img" || swatch.Name == "" {
			t.Errorf("%s swatch = %+v, want labelled fill role %v", e.Path, swatch, role)
		}
	}
}

func TestOpacityAndBarGeometryControlsShowUnitsAndUseSliders(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	units := map[string]string{
		"appearance.bar-opacity":     "%",
		"appearance.panel-opacity":   "%",
		"appearance.overlay-opacity": "%",
		"bar.height":                 "px",
		"bar.gap":                    "px",
		"bar.padding":                "px",
		"bar.spacing":                "px",
	}
	for path, unit := range units {
		e := h.set.ByPath(path)
		if e == nil {
			t.Fatalf("setting %s is not registered", path)
		}
		control := settingsControl(h, *e, 360)
		if findNode(control, func(n *ui.Node) bool { return n.Kind == ui.KindSlider }) == nil {
			t.Errorf("%s uses no slider", path)
		}
		value := findNode(control, func(n *ui.Node) bool { return n.Kind == ui.KindText && strings.HasSuffix(n.Text, unit) })
		if value == nil {
			t.Errorf("%s has no displayed %q unit", path, unit)
		}
	}
}

func TestWeatherRefreshDurationUsesConciseUnitForm(t *testing.T) {
	t.Parallel()
	e := settings.Default().ByPath("weather.interval")
	if e == nil {
		t.Fatal("weather.interval is not registered")
	}
	if got := e.Get(config.Default()); got != "15m" {
		t.Fatalf("weather interval = %q, want the input-style form 15m", got)
	}
}

func TestPluginSettingsUseFullWidthRadioTabsAndHideEmptyUpdates(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	root := pluginsTree(&Registry{}, h)
	tabs := findNode(root, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented && n.Name == "Plugins view" })
	if tabs == nil || tabs.Width != settingsBodyWidth(h) {
		t.Fatalf("plugin tabs width = %d, want %d", widthOf(tabs), settingsBodyWidth(h))
	}
	for _, child := range tabs.Children {
		if child.Role != "radio" {
			t.Errorf("plugin tab %q has role %q, want radio", child.Name, child.Role)
		}
	}
	if n := findNode(root, func(n *ui.Node) bool { return n.Kind == ui.KindText && n.Text == "Updates (0)" }); n != nil {
		t.Fatal("empty Updates card is visible")
	}
}

func TestAppearanceColorLabelsUseBritishSpelling(t *testing.T) {
	t.Parallel()
	set := settings.Default()
	for path, want := range map[string]string{
		"appearance.palette": "Colour palette",
		"appearance.scheme":  "Colour scheme",
	} {
		e := set.ByPath(path)
		if e == nil || e.Label != want {
			got := ""
			if e != nil {
				got = e.Label
			}
			t.Errorf("%s label = %q, want %q", path, got, want)
		}
	}
}

func textOf(n *ui.Node) string {
	if n == nil {
		return ""
	}
	return n.Text
}

func widthOf(n *ui.Node) int {
	if n == nil {
		return 0
	}
	return n.Width
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

	// Bar font size is still a stepper; spacing became a slider when the owner
	// asked for bar geometry to read alike (2026-10-01).
	up := byAction(h.root, "step:up:bar.font-size")
	if up == nil {
		t.Fatal("bar.font-size rendered no stepper")
	}
	before := h.draft.Bar.FontSize
	h.setFocus(up)
	h.activate(reg)
	if got := h.draft.Bar.FontSize; got != before+1 {
		t.Fatalf("font size = %d after one step, want %d", got, before+1)
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
	// The order is what a reader scans, so it is case-insensitive: a
	// byte-wise sort files "cursive" after every capitalised name, which
	// reads as no order at all.
	if !slices.IsSortedFunc(families, func(a, b string) int {
		if la, lb := strings.ToLower(a), strings.ToLower(b); la != lb {
			return strings.Compare(la, lb)
		}
		return strings.Compare(a, b)
	}) {
		t.Error("families are not sorted, so the picker reorders itself between builds")
	}
	// sysc-332. Footprint.Family is normalized; a name table is not. If every
	// family on a machine with real fonts installed is still lower case with
	// no spaces, the descriptive name is not being read.
	descriptive := 0
	for _, f := range families {
		if f != strings.ToLower(f) || strings.Contains(f, " ") {
			descriptive++
		}
	}
	if len(families) > 8 && descriptive == 0 {
		t.Errorf("all %d families are normalized; the picker is showing dejavusans rather than DejaVu Sans", len(families))
	}
}

// sysc-332 and sysc-330 together. The picker draws a descriptive name and
// writes one the configuration can carry, and every row it offers has to
// survive the loader — the round trip that found three defects in A.
func TestFontPickerRoundTripsEveryRowItOffers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	r := settings.DefaultFor(cfg)
	for _, path := range []string{"appearance.font-family", "appearance.mono-font-family", "bar.font-family"} {
		e := r.ByPath(path)
		if e == nil {
			t.Fatalf("%s is not registered", path)
		}
		if e.Kind != settings.KindFont {
			t.Errorf("%s is kind %d, want KindFont", path, e.Kind)
		}
		options, values := settingsFontOptions(*e)
		if len(options) != len(values) {
			t.Fatalf("%s: %d options against %d values; a row would write the wrong family", path, len(options), len(values))
		}
		if len(options) == 0 {
			t.Fatalf("%s: no options at all", path)
		}
		// The empty row exists exactly where the entry declares one, and it
		// is the first row when it does.
		if (values[0] == "") != (e.EmptyLabel != "") {
			t.Errorf("%s: first row writes %q against EmptyLabel %q", path, values[0], e.EmptyLabel)
		}
		for _, v := range values {
			c := cfg
			if err := e.Set(&c, v); err != nil {
				t.Fatalf("%s = %q: %v", path, v, err)
			}
			file := filepath.Join(t.TempDir(), "config.json")
			if err := config.Write(file, c); err != nil {
				t.Fatalf("%s = %q: write: %v", path, v, err)
			}
			if _, err := config.Load(file); err != nil {
				t.Fatalf("%s = %q: the picker offered a value the loader refuses: %v", path, v, err)
			}
		}
	}
}

// A configuration written before the picker showed descriptive names carries
// the normalized form. The row it names has to stay selected, or opening the
// pane silently claims the user chose whatever sorts first.
func TestTheFontPickerFindsARowForANormalizedValue(t *testing.T) {
	t.Parallel()
	options := []string{"Default", "DejaVu Sans", "Inter Variable"}
	values := []string{"", "DejaVu Sans", "Inter Variable"}
	if got := settingsOptionIndex(options, values, "intervariable"); got != 2 {
		t.Errorf("index for the normalized name = %d, want 2", got)
	}
	if got := settingsOptionIndex(options, values, "Inter Variable"); got != 2 {
		t.Errorf("index for the descriptive name = %d, want 2", got)
	}
	if got := settingsOptionIndex(options, values, ""); got != 0 {
		t.Errorf("index for the empty value = %d, want the Default row", got)
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

	body := findSettingsBody(h.root)
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

// TestHexFieldAndSetterShareOneRule holds the field's "is this typed colour
// good" answer to the setter's "will I accept this write" answer, and holds
// both to what config.Load will read back. The three used to be three separate
// copies of the same pattern, which is a shape that fails silently: a value
// marks itself valid in the field and is then refused by the very write it was
// typed for, and nothing catches the drift until a user hits it.
func TestHexFieldAndSetterShareOneRule(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.ThemeGen.Source = "hex"
	seed := settings.DefaultFor(cfg).ByPath("appearance.seed")
	if seed == nil || seed.Kind != settings.KindHex {
		t.Fatalf("appearance.seed = %+v, want a hex entry under a hex source", seed)
	}

	for _, v := range []string{
		"#ffffff", "#000000", "#A1B2C3", "#a1b2c3d4",
		"  #ffffff  ", "#fff", "ffffff", "#gggggg", "", "#ffffff ff",
	} {
		field := settingsValidHex(v)
		c := cfg
		accepted := seed.Set(&c, v) == nil
		if field != accepted {
			t.Fatalf("%q: field says valid=%v, setter says accepted=%v", v, field, accepted)
		}
		if !accepted {
			continue
		}
		// What the setter stored has to survive the loader, or the pane has
		// written a configuration the shell will refuse to start from.
		path := filepath.Join(t.TempDir(), "config.json")
		if err := config.Write(path, c); err != nil {
			t.Fatalf("%q: write: %v", v, err)
		}
		got, err := config.Load(path)
		if err != nil {
			t.Fatalf("%q: the setter accepted a value the loader refuses: %v", v, err)
		}
		if got.ThemeGen.Seed != c.ThemeGen.Seed {
			t.Fatalf("%q: seed round-tripped as %q, want %q", v, got.ThemeGen.Seed, c.ThemeGen.Seed)
		}
	}
}

// TestControlCentreShortcutReachesEverySection says out loud what the code
// currently only implies. The shortcut page builds from settingsSections, so
// it cannot drift today — but nothing recorded that it must not, and a page
// that grew its own list would silently strand whichever section it forgot,
// with no error anywhere. It also holds each row to a glyph, because a name
// the icon subset lacks shapes to nothing and paints an invisible link.
func TestControlCentreShortcutReachesEverySection(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	reached := map[string]bool{}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if name, ok := strings.CutPrefix(n.Action, "settings-section:"); ok {
			reached[name] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(ccSettings(h))

	for _, name := range settingsSections {
		if !reached[name] {
			t.Errorf("section %q has no shortcut row in the control centre", name)
		}
		if settingsSectionIcons[name] == "" {
			t.Errorf("section %q has no glyph, so its shortcut row paints nothing", name)
		}
	}
	if len(reached) != len(settingsSections) {
		t.Errorf("shortcut rows = %d, sections = %d; the page has its own list",
			len(reached), len(settingsSections))
	}
}

// TestEmptySectionSaysWhy covers the two sections that build their entries
// from what the configuration already names. A user who has never pinned a
// tray item or overridden an output opens Tray or Displays and, before this,
// saw an empty column: indistinguishable from a section that failed to build.
func TestEmptySectionSaysWhy(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	for _, section := range []string{"Tray", "Displays"} {
		if got := h.set.Section(section); len(got) != 0 {
			t.Fatalf("%s built %d entries from a default configuration; "+
				"this test no longer covers the empty case", section, len(got))
		}
		col := settingsSectionColumn(h, section, nil)
		var text string
		var walk func(*ui.Node)
		walk = func(n *ui.Node) {
			if n == nil {
				return
			}
			if n.Kind == ui.KindText && n.Text != "" && text == "" {
				text = n.Text
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(col)
		if text == "" {
			t.Errorf("%s renders an empty column with no explanation", section)
		}
		if text != settingsEmptySection[section] {
			t.Errorf("%s says %q, want its own note %q", section, text, settingsEmptySection[section])
		}
	}
}

func TestScreensaverSectionHasRouteAndGlyph(t *testing.T) {
	t.Parallel()
	section, page, ok := settingsAddress("Screensaver")
	if !ok || section != "Screensaver" || page != "" {
		t.Fatalf("Screensaver address = (%q, %q, %v)", section, page, ok)
	}
	if got := settingsSectionIcons["Screensaver"]; got != "schedule" {
		t.Fatalf("Screensaver glyph = %q, want schedule", got)
	}
}

func TestScreensaverSettingsExplainsAvailabilityAndOffersAccessibleActions(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Screensaver"
	h.root = settingsTree(nil, h)
	text := renderText(h.root)
	for _, want := range []string{"Service state unavailable", "Effect", "Theme", "Artwork", "Preview", "Reset", "Apply"} {
		if !strings.Contains(text, want) {
			t.Errorf("Screensaver settings omit %q: %s", want, text)
		}
	}
}

// A control that has a natural size should wear it and sit at the end of the
// row, not stretch across the column or float at its left. Reported by the
// owner: toggles and dropdowns read as taking the full panel width.
func TestNaturalSizedControlsDoNotFillTheColumn(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	body := settingsBodyWidth(h)
	column := settingsControlWidth(h)

	for _, tc := range []struct {
		path  string
		fills bool
	}{
		{"bar.enabled", false},              // toggle
		{"appearance.mode", false},          // enum
		{"bar.height", true},                // slider
		{"wallpaper.image-directory", true}, // path field
	} {
		e := h.set.ByPath(tc.path)
		if e == nil {
			t.Fatalf("%s is not registered", tc.path)
		}
		row := settingsEntryRow(h, *e, settingsBodyWidth(h))
		var trailing *ui.Node
		for _, c := range row.Children {
			if c.Kind == ui.KindRow {
				trailing = c
			}
		}
		if trailing == nil {
			t.Fatalf("%s: row has no trailing column", tc.path)
		}
		if tc.fills && trailing.Width != column {
			t.Errorf("%s: trailing width = %d, want the control column %d", tc.path, trailing.Width, column)
		}
		if !tc.fills && trailing.Width != 0 {
			t.Errorf("%s: trailing width = %d, want it to size to the control", tc.path, trailing.Width)
		}
	}
	if column >= body {
		t.Errorf("control column %d is not narrower than the body %d", column, body)
	}
}

// An open picker must stay a control, not become a column as tall as the
// font list. The pane lays out inside a bounded surface, and a menu whose
// open height is one row per family does not fit in it — layout refuses the
// tree, rebuildPanel takes the error, and the pane paints nothing.
func TestAnOpenFontPickerFitsThePane(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Appearance"
	h.root = settingsTree(nil, h)

	var menu *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || menu != nil {
			return
		}
		if n.Kind == ui.KindMenu && n.Action == "set:appearance.font-family" {
			menu = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if menu == nil {
		t.Fatal("the Appearance section has no font family menu")
	}
	m := h.menus["appearance.font-family"]
	if m == nil {
		t.Fatal("no retained menu for the font family entry")
	}
	if !m.Filtering() {
		t.Skip("this machine has fewer fonts than the picker threshold")
	}
	m.Open()
	h.root = settingsTree(nil, h)

	size := panelTargetSize(PanelSettings)
	err := ui.LayoutColumn(h.root, ui.Rect{W: size.W, H: size.H}, func(s string, _ ui.TextAttrs) (int, int) {
		return len(s) * 8, 16
	})
	if err != nil {
		t.Fatalf("an open font picker does not lay out: %v", err)
	}
}

// The filter well takes the ladder's input height. Measured from its own text
// it is exactly one line tall, which reads as a rule across the list rather
// than as something to type into.
func TestTheFilterWellTakesTheLadderHeight(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	e := h.set.ByPath("bar.font-family")
	if e == nil {
		t.Fatal("bar.font-family is not registered")
	}
	options, values := settingsFontOptions(*e)
	if len(options) <= settingsMenuLimit {
		t.Skip("this machine has too few fonts for the picker")
	}
	settingsPickerControl(h, *e, options, values, "", 200)
	m := h.menus["bar.font-family"]
	m.Open()
	n := settingsPickerControl(h, *e, options, values, "", 200)
	if len(n.Children) == 0 {
		t.Fatal("the open picker drew nothing")
	}
	if got, want := n.Children[0].Height, h.metrics().InputHeight; got != want {
		t.Errorf("filter well height = %d, want the ladder's %d", got, want)
	}
}

// The picker has to be reachable through the panel, not just through Menu.
// This walks the path a press and a keystroke actually take: activate opens
// the menu, keyPress routes text through editField into the well, and the
// tree that comes back carries the narrowed list. Unit tests over Menu prove
// the widget; only this proves the wiring between the panel and the widget.
func TestThePanelOpensAndFiltersTheFontPicker(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Appearance"
	h.root = settingsTree(nil, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}

	m := h.menus["appearance.font-family"]
	if m == nil {
		t.Fatal("the Appearance section built no font family menu, so the entry is not a picker")
	}
	if !m.Filtering() {
		t.Skip("this machine has fewer fonts than the picker threshold")
	}
	target := "appearance.font-family"

	// Focus the font family control the way the roving ring would.
	found := false
	for i, n := range h.focus {
		if n != nil && n.Action == "set:"+target {
			h.roving.Set(i)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the font family control is not focusable, so the keyboard cannot reach it")
	}

	r := &Registry{panelHosts: map[PanelID]*PanelHost{PanelSettings: h}}
	if !h.activate(r) {
		t.Fatal("activating the font family control did nothing")
	}
	if !m.Opened() {
		t.Fatal("the picker did not open")
	}

	// Type into the well through the panel's own key path.
	for _, key := range []uint32{50, 24, 49, 24} { // m, o, n, o
		if !h.keyPress(r, key) {
			t.Fatalf("the panel dropped a keystroke while the picker was open")
		}
	}
	if got := m.filter.Text; got != "mono" {
		t.Fatalf("the well holds %q, want \"mono\"; the keys did not reach it", got)
	}

	node := h.menus[target].Node()
	if len(node.Children) < 1 {
		t.Fatal("the open picker drew nothing")
	}
	if node.Children[0].Kind != ui.KindTextField {
		t.Error("the first child is not the filter well")
	}
	for _, c := range node.Children[1:] {
		if !strings.Contains(strings.ToLower(c.Text), "mono") {
			t.Errorf("row %q survived the filter", c.Text)
		}
	}
	if len(node.Children)-1 >= len(settingsFontFamilies()) {
		t.Error("the filter narrowed nothing")
	}
	t.Logf("filtered to %d of %d families", len(node.Children)-1, len(settingsFontFamilies()))
}

func TestMouseWheelScrollsAnOpenSettingsMenu(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	size := panelTargetSize(PanelSettings)
	if err := panel.Callbacks.Configure(size.W, size.H, 120); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelSettings]
	options := []string{"option0", "option1", "option2", "option3", "option4", "option5", "option6", "option7"}
	m := NewMenu(options, 0)
	m.Open()

	reg.mu.Lock()
	h.section = "Appearance"
	h.menus["appearance.font-family"] = m
	h.menu, h.menuPath = m, "appearance.font-family"
	reg.rebuildPanel(h)
	n := byAction(h.root, "set:appearance.font-family")
	var popup ui.Rect
	if n != nil {
		popup = ui.MenuPopupBounds(n)
	}
	reg.mu.Unlock()
	if n == nil || popup.W <= 0 || popup.H <= 0 {
		t.Fatal("open menu has no laid-out popup")
	}

	handle := panel.Callbacks.Handle
	x, y := popup.X+popup.W/2, popup.Y+popup.H/2
	handle(wayland.Event{Kind: wayland.EventPointerEnter, X: float64(x), Y: float64(y)})
	assertCursor := func(want int) {
		t.Helper()
		reg.mu.Lock()
		defer reg.mu.Unlock()
		if m.cursor != want {
			t.Fatalf("menu cursor = %d, want %d", m.cursor, want)
		}
	}
	if !handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: 1}) {
		t.Fatal("discrete wheel over the popup was not handled")
	}
	assertCursor(1)
	if !handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: 120}) {
		t.Fatal("value120 wheel over the popup was not handled")
	}
	assertCursor(2)
	if !handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: -120}) {
		t.Fatal("reverse value120 wheel over the popup was not handled")
	}
	assertCursor(1)
	for range 8 {
		if !handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: -1}) {
			t.Fatal("reverse wheel over the popup was not handled")
		}
	}
	assertCursor(0)
	for range 8 {
		if !handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: 1}) {
			t.Fatal("wheel over the popup was not handled")
		}
	}
	assertCursor(7)

	reg.mu.Lock()
	n = byAction(h.root, "set:appearance.font-family")
	cursor, selected := m.cursor, m.Index()
	rowCount, highlighted := 0, -1
	var visible []string
	if n != nil {
		rowCount = len(n.Children)
		visible = make([]string, len(n.Children))
		for i, child := range n.Children {
			visible[i] = child.Text
			if child.Value != 0 {
				highlighted = i
			}
		}
	}
	reg.mu.Unlock()
	if n == nil {
		t.Fatal("wheel removed the open menu")
	}
	if rowCount != menuVisibleRows {
		t.Fatalf("open menu rows = %d, want %d", rowCount, menuVisibleRows)
	}
	if got := visible[0]; got != "option2" {
		t.Errorf("first visible option = %q, want option2 after scrolling", got)
	}
	if got := visible[len(visible)-1]; got != "option7" {
		t.Errorf("last visible option = %q, want option7", got)
	}
	if highlighted != menuVisibleRows-1 {
		t.Errorf("highlighted row = %d, want %d", highlighted, menuVisibleRows-1)
	}
	if cursor != 7 {
		t.Errorf("menu cursor after wheel = %d, want 7", cursor)
	}
	if selected != 0 {
		t.Errorf("wheel committed option %d before activation", selected)
	}
}

func TestShortEnumsRenderSegmentedAndPickWrites(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	e := *h.set.ByPath("weather.unit")
	n := settingsControl(h, e, 200)
	if n.Kind != ui.KindSegmented || len(n.Children) != len(e.Options) {
		t.Fatalf("weather.unit control = kind %v with %d children, want a segmented of %d", n.Kind, len(n.Children), len(e.Options))
	}
	if n.Children[0].Action != "pick:weather.unit="+e.Options[0] {
		t.Fatalf("first segment action = %q", n.Children[0].Action)
	}
	selected := 0
	for _, c := range n.Children {
		if c.State&ui.StateSelected != 0 {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("%d segments selected, want 1", selected)
	}
	menu := e
	menu.Present = settings.PresentMenu
	if settingsControl(h, menu, 200).Kind == ui.KindSegmented {
		t.Error("PresentMenu still rendered segmented")
	}
	if osd := h.set.ByPath("panels.osd"); osd == nil || settingsControl(h, *osd, 200).Kind == ui.KindSegmented {
		t.Error("the nine-option OSD position rendered segmented, or is missing")
	}
}

func TestPickActionCommitsTheValue(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	h.focus = []*ui.Node{{Kind: ui.KindButton, Action: "pick:weather.unit=fahrenheit", Focusable: true}}
	h.roving = ui.Roving{Count: 1}
	if !h.activate(reg) {
		t.Fatal("pick was not handled")
	}
	if h.draft.Weather.Unit != "fahrenheit" {
		t.Fatalf("draft unit = %q after pick", h.draft.Weather.Unit)
	}
}

func TestGroupsRenderAsCardsWithRowsInside(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Appearance"
	h.place.Panel = ui.Rect{W: 1105, H: 760}
	h.root = settingsTree(nil, h)
	cards := 0
	inner := settingsCardInner(h)
	for _, n := range walk(h.root) {
		if n.Kind != ui.KindCapsule || n.Shape != ui.ShapeCard {
			continue
		}
		cards++
		col := n.Children[0]
		if col.Children[0].TextRole != theme.RoleSection || col.Children[0].Bold || theme.TypeFor(col.Children[0].TextRole).Weight < 700 || col.Children[0].Tone != ui.ToneAccent {
			t.Errorf("Appearance card does not open with its styled title: %+v", col.Children[0])
		}
		for _, row := range col.Children[1:] {
			if row.Kind == ui.KindRow && row.Width > inner {
				t.Errorf("row %d wide overruns its %d-wide card", row.Width, inner)
			}
		}
	}
	if cards == 0 {
		t.Fatal("Appearance rendered no group cards")
	}
	if err := ui.LayoutColumn(h.root, h.place.Panel, func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 8, 18 }); err != nil {
		t.Fatal(err)
	}
}

func TestSearchHitsNameTheirPage(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.query = "frost"
	h.root = settingsTree(nil, h)
	for _, n := range walk(h.root) {
		if n.Text == "Bar › Appearance" {
			return
		}
	}
	t.Fatal("a Bar frost hit is not captioned Bar › Appearance")
}

func TestRailIsLabelledAndClustered(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	rail := settingsRail(h, "Bar", nil)
	var captions, tabs []string
	for _, n := range walk(rail) {
		if n.Role == "tab" {
			tabs = append(tabs, n.Name)
			if findNode(n, func(c *ui.Node) bool { return c.Kind == ui.KindText && c.Text == n.Name }) == nil {
				t.Errorf("rail tab %s has no visible label", n.Name)
			}
		}
		if n.Role == "heading" && n.TextRole == theme.RoleCaption {
			captions = append(captions, n.Text)
		}
	}
	if !slices.Equal(tabs, settings.SectionNames()) {
		t.Errorf("tabs %v, want %v", tabs, settings.SectionNames())
	}
	if !slices.Equal(captions, []string{"Look", "Shell", "Surfaces", "Extensions", "System"}) {
		t.Errorf("captions %v", captions)
	}
}

func TestBarOpensOnAppearanceAndPagesSwitch(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	if findByName(h.root, "Style") == nil {
		t.Fatal("Bar did not open on Appearance: no Style row")
	}
	for _, page := range []string{"Appearance", "Layout", "Displays"} {
		if findAction(h.root, "page:"+page) == nil {
			t.Errorf("no tab for page %s", page)
		}
	}
	h.settingsPage = "Layout"
	h.root = settingsTree(nil, h)
	if findByName(h.root, "Style") != nil {
		t.Error("Style shows on the Layout page")
	}
}

func TestSettingsAddressesResolvePages(t *testing.T) {
	t.Parallel()
	for req, want := range map[string][2]string{
		"Displays":   {"Bar", "Displays"},
		"Bar":        {"Bar", "Appearance"},
		"Bar/Layout": {"Bar", "Layout"},
		"Appearance": {"Appearance", ""},
	} {
		s, p, ok := settingsAddress(req)
		if !ok || s != want[0] || p != want[1] {
			t.Errorf("%q → %q %q %v, want %v", req, s, p, ok, want)
		}
	}
	for _, bad := range []string{"Nowhere", "Bar/Nowhere", "Appearance/Layout"} {
		if _, _, ok := settingsAddress(bad); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestOpeningTheOldDisplaysSectionLandsOnBarDisplays(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if err := reg.selectPanelSectionLocked(PanelSettings, "Displays"); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelSettings]
	if h.section != "Bar" || h.settingsPage != "Displays" {
		t.Fatalf("landed on %q/%q", h.section, h.settingsPage)
	}
}

func TestClearingSearchReturnsToTheSamePage(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.settingsPage = "Layout"
	h.query = "frost"
	h.root = settingsTree(nil, h)
	if findAction(h.root, "page:Layout") != nil {
		t.Error("page tabs show while searching")
	}
	h.query = ""
	h.root = settingsTree(nil, h)
	if h.section != "Bar" || settingsCurrentPage(h, "Bar") != "Layout" {
		t.Fatalf("after search: %q/%q", h.section, settingsCurrentPage(h, "Bar"))
	}
	if n := findAction(h.root, "page:Layout"); n == nil || n.State&ui.StateSelected == 0 {
		t.Error("Layout is not the selected page after search")
	}
}

func TestSettingsSizeFollowsTheOutput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ w, h, wantW, wantH int }{
		{1536, 864, 1105, 760},
		{3440, 1440, 1120, 820},
		{1280, 720, 921, 633},
	} {
		if got := settingsPanelSize(tc.w, tc.h); got.W != tc.wantW || got.H != tc.wantH {
			t.Errorf("%dx%d → %+v, want %dx%d", tc.w, tc.h, got, tc.wantW, tc.wantH)
		}
	}
}

func TestSettingsPaintsAtLeastTheOpaqueFloor(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.theme = DefaultTheme()
	h.theme.Surfaces.Panel = 0x80
	if a := h.rootStyle(h.theme).SurfaceOpacity; a < settingsOpacityFloor {
		t.Fatalf("settings root alpha %#x, want at least %#x", a, settingsOpacityFloor)
	}
	other := &PanelHost{id: PanelClock, theme: h.theme}
	if a := other.rootStyle(other.theme).SurfaceOpacity; a != 0x80 {
		t.Fatalf("clock root alpha %#x changed", a)
	}
}

func TestSettingsTemplatesSurfaceRefusals(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Templates"
	r := &Registry{templateRefusals: map[string]string{
		"cava": "theming: target modified outside the shell: /home/u/.config/cava/config",
	}}
	h.root = settingsTree(r, h)
	if !strings.Contains(renderText(h.root), "user-modified") {
		t.Fatal("the refusal note is missing from the Templates section")
	}
	overwrite := findByName(h.root, "Overwrite cava")
	if overwrite == nil || overwrite.Action != "template-overwrite:cava" || !overwrite.Focusable {
		t.Fatalf("overwrite control = %+v", overwrite)
	}

	h.root = settingsTree(nil, h)
	if strings.Contains(renderText(h.root), "user-modified") {
		t.Fatal("a registry with no refusals rendered a refusal note")
	}
}

func TestTemplateOverwritePersistsTheDraftNotTheOldConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	r := &Registry{configPath: path, cfg: config.Default()}
	r.templateRefusals = map[string]string{"cava": "theming: target modified: x"}
	h := newSettingsHost()
	h.section = "Templates"
	h.draft = config.Default()
	h.draft.Templates = map[string]bool{"niri": true}
	h.root = settingsTree(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}
	for i, n := range h.focus {
		if n.Action == "template-overwrite:cava" {
			h.roving.Set(i)
			break
		}
	}
	if !h.activate(r) {
		t.Fatal("overwrite not handled")
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Templates["niri"] {
		t.Fatal("the overwrite discarded the unsaved draft")
	}
	r.templateMu.Lock()
	defer r.templateMu.Unlock()
	if !r.templateForce["cava"] {
		t.Fatal("the overwrite did not arm the force flag")
	}
}

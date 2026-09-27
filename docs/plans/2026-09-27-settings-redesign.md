# Settings Redesign and Bar Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Settings navigable and legible, and put the bar's appearance
controls (with a live preview and picture cards) at the top of Bar settings.

**Architecture:** Extend the settings registry (`internal/settings`) with a
page and a presentation hint per entry, a clustered rail, and fixed page lists.
The shell's Settings tree (`internal/shell/popout_settings.go`) renders a
labelled rail, page tabs, groups as cards, segmented enums, picture cards and
an offscreen bar preview. The preview is built with `NewWithTheme` from the
settings draft and painted by `Bar.Render`. No new node kinds.

**Tech Stack:** Go; the shell's own `internal/ui` node tree and `internal/render`
painter.

**Spec:** `docs/plans/2026-09-27-settings-redesign-design.md` (D1–D10)

## Global Constraints

- Go only. No new dependencies; `git diff --exit-code -- go.mod go.sum` stays clean.
- No new `ui.Kind`. Compose from `KindCapsule`, `KindColumn`, `KindRow`, `KindButton`, `KindSegmented`, `KindImage`, `KindText`.
- Never run `-race` over `./...`. Race one package at a time; repo-wide runs use `GOMAXPROCS=4 go test -p 2 ./...`.
- Known pre-existing failures that must not grow: `TestPanelSectionValidationPrecedesMutation` and three `*Battery*` tests in `internal/shell`, and the sixteen `TestTray*` tests in `tests/integration` (`sysc-586`).
- Commit messages must not contain `bot` (so no "both"/"bottom"), `agent`, `cursor`, `codex` or `llm`. No attribution trailers.
- Worktree commits: `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit ...`.
- Layout widths are measured, never fixed where text is involved (the `sysc-589` 1 px overflow at scale 1.25).

## Review Focus

1. **Scale 1.25 and 1.5 overflow.** A segmented control, a picture card or a card row that fits at 1.0 must still lay out at 1.25 and 1.5 on 1536×864. Owned by Task 9's matrix, and by Tasks 3 and 7's own scale loops.
2. **An old address.** `panel.open settings` with `section: "Displays"` (and control-centre shortcuts) must land on Bar › Displays, not fail. Owned by Task 5.
3. **Bar disabled or style solid.** The frost sliders dim with a reason; the preview still renders; nothing panics with the bar off. Owned by Task 8.
4. **Search while on a page.** Typing a query shows hits from every section and page; clearing it returns to the same section and page. Owned by Task 5.
5. **A draft that fails validation.** The preview is built from `h.draft`. An invalid draft (e.g. a bad hex colour mid-edit) must show the preview's last good image, not an error or a blank card. Owned by Task 7.

---

### Task 1: Pages, presentation and clusters in the registry

**Files:**
- Modify: `internal/settings/entry.go` (the `Entry` struct at `:33`)
- Modify: `internal/settings/registry.go` (`SectionNames` at `:554`; Bar entries at `:25-:110`; Displays entries at `:1035-:1070`)
- Test: `internal/settings/pages_test.go` (create)

**Interfaces:**
- Produces: `type Presentation uint8` with `PresentAuto`, `PresentMenu`, `PresentCards`; `Entry.Page string`; `Entry.Present Presentation`; `type Cluster struct{ Name string; Sections []string }`; `func SectionClusters() []Cluster`; `func SectionPages(section string) []string`; `func (r *Registry) PageEntries(section, page string) []Entry`. `SectionNames()` keeps its signature and is derived from `SectionClusters()`.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"slices"
	"testing"
)

func TestEveryEntryLandsOnAReachablePage(t *testing.T) {
	t.Parallel()
	rail := SectionNames()
	for _, e := range Default().All() {
		if !slices.Contains(rail, e.Section) {
			t.Errorf("%s: section %q is not in the rail", e.Path, e.Section)
			continue
		}
		pages := SectionPages(e.Section)
		if len(pages) == 0 && e.Page != "" {
			t.Errorf("%s: page %q in single-page section %q", e.Path, e.Page, e.Section)
		}
		if len(pages) > 0 && !slices.Contains(pages, e.Page) {
			t.Errorf("%s: page %q is not one of %s's pages %v", e.Path, e.Page, e.Section, pages)
		}
	}
}

func TestBarPagesAndPresentation(t *testing.T) {
	t.Parallel()
	if got := SectionPages("Bar"); !slices.Equal(got, []string{"Appearance", "Layout", "Displays"}) {
		t.Fatalf("Bar pages = %v", got)
	}
	r := Default()
	for _, path := range []string{"bar.enabled", "bar.edge", "bar.style", "bar.shape", "bar.frost-opacity", "bar.height", "bar.font-size"} {
		if e := r.ByPath(path); e == nil || e.Page != "Appearance" {
			t.Errorf("%s is not on Bar › Appearance: %+v", path, e)
		}
	}
	for _, path := range []string{"bar.style", "bar.shape"} {
		if e := r.ByPath(path); e.Present != PresentCards {
			t.Errorf("%s presents %v, want cards", path, e.Present)
		}
	}
	if slices.Contains(SectionNames(), "Displays") {
		t.Error("Displays is still a rail section; it moved to Bar › Displays")
	}
}

func TestClustersCoverTheRailOnce(t *testing.T) {
	t.Parallel()
	var flat []string
	for _, c := range SectionClusters() {
		if c.Name == "" || len(c.Sections) == 0 {
			t.Errorf("empty cluster %+v", c)
		}
		flat = append(flat, c.Sections...)
	}
	if !slices.Equal(flat, SectionNames()) {
		t.Fatalf("clusters flatten to %v, rail is %v", flat, SectionNames())
	}
}
```

If `Registry` has no `All()` accessor, add one in Step 3 (`func (r *Registry) All() []Entry { return append([]Entry(nil), r.entries...) }`, using whatever the slice field is named).

- [ ] **Step 2: Run it to verify it fails**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/settings -run 'Page|Cluster'`
Expected: FAIL, compile errors for `SectionPages`, `SectionClusters`, `PresentCards`.

- [ ] **Step 3: Implement**

In `entry.go`, beside `Kind`:

```go
// Presentation is how a row shows its control (design D2). The zero value
// keeps today's control, except that a short enum renders segmented.
type Presentation uint8

const (
	PresentAuto Presentation = iota
	PresentMenu
	PresentCards
)
```

Add to `Entry`, after `Group`:

```go
	// Page is the tab within Section this entry sits on (design D1). Empty
	// in a section with no pages.
	Page string
	// Present overrides how the control is drawn (design D2).
	Present Presentation
```

In `registry.go`, replace `SectionNames` with:

```go
// Cluster is one captioned run of the rail (design D5).
type Cluster struct {
	Name     string
	Sections []string
}

// SectionClusters is the rail, in order. SectionNames flattens it, so the two
// cannot disagree.
func SectionClusters() []Cluster {
	return []Cluster{
		{"Look", []string{"Appearance", "Templates", "Wallpaper"}},
		{"Bar", []string{"Bar", "Widgets", "Tray"}},
		{"Panels", []string{"Panels", "Monitor", "Weather", "Plugins"}},
		{"System", []string{"Session", "Accessibility"}},
	}
}

// SectionNames is the information architecture in rail order. The pane walks
// it, so a section absent here is a section the user cannot reach however many
// entries name it.
func SectionNames() []string {
	var out []string
	for _, c := range SectionClusters() {
		out = append(out, c.Sections...)
	}
	return out
}

// SectionPages is a section's tabs, in order; nil for a section with one page.
// Layout carries no entries (the lane editor is the page), so pages are a
// fixed list rather than derived from entries.
func SectionPages(section string) []string {
	switch section {
	case "Bar":
		return []string{"Appearance", "Layout", "Displays"}
	}
	return nil
}

// PageEntries is one page of a section, in registry order.
func (r *Registry) PageEntries(section, page string) []Entry {
	var out []Entry
	for _, e := range r.Section(section) {
		if e.Page == page {
			out = append(out, e)
		}
	}
	return out
}
```

Set `Page: "Appearance"` on every Bar entry (`bar.enabled` through `bar.font-size`), and `Present: PresentCards` on `bar.style` and `bar.shape`. On the Displays entries, change `Section: "Displays"` to `Section: "Bar", Page: "Displays"`. Grep for any other `Section: "Bar"` entry and give it a page.

- [ ] **Step 4: Run to verify it passes, then the package**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/settings`
Expected: PASS. Fix any older test in the package that asserted "Displays" as a section by updating it to `Bar`/`Displays`.

- [ ] **Step 5: Commit**

```bash
git add internal/settings
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(settings): pages, presentation and rail clusters"
```

---

### Task 2: Plugin rows show the plugin's name

**Files:**
- Modify: `internal/settings/widgetnames.go` (`WidgetName` at `:43`)
- Modify: `internal/shell/bareditor.go` (`barChip` at `:106`, and every other `settings.WidgetName(` caller: `grep -rn 'WidgetName(' internal/`)
- Test: `internal/settings/widgetnames_test.go` (append, or create if absent)

**Interfaces:**
- Produces: `func WidgetName(it config.Item, plugin func(id string) string) string`; `func (r *Registry) pluginDisplayName(id string) string` in the shell.

- [ ] **Step 1: Failing test**

```go
func TestPluginWidgetsAreNamedByTheirPlugin(t *testing.T) {
	t.Parallel()
	it := config.Item{ID: "plugin", Plugin: "org.sysc.mini-docker", Entry: "bar"}
	names := func(id string) string {
		if id == "org.sysc.mini-docker" {
			return "Mini Docker"
		}
		return ""
	}
	if got := WidgetName(it, names); got != "Mini Docker" {
		t.Errorf("named %q, want Mini Docker", got)
	}
	if got := WidgetName(it, func(string) string { return "" }); got != "org.sysc.mini-docker" {
		t.Errorf("unknown plugin named %q, want its id", got)
	}
	if got := WidgetName(config.Item{ID: "clock"}, nil); got != "Clock" {
		t.Errorf("clock named %q", got)
	}
}
```

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/settings -run PluginWidgets` — Expected: FAIL (too many arguments).

- [ ] **Step 3: Implement**

```go
// WidgetName is a widget's display name. A plugin widget is named by its
// plugin (design D9); plugin resolves an ID to the catalogue's name and may
// be nil. The entry point ("bar") is never a name.
func WidgetName(it config.Item, plugin func(id string) string) string {
	switch it.ID {
	case "group":
		return "Group"
	case "plugin":
		if plugin != nil {
			if name := plugin(it.Plugin); name != "" {
				return name
			}
		}
		if it.Plugin != "" {
			return it.Plugin
		}
		return "Plugin"
	}
	if name, ok := widgetNames[it.ID]; ok {
		return name
	}
	return it.ID
}
```

In the shell, add a resolver backed by the plugin host's manifests. Find where manifests are held (`grep -n 'Manifest' internal/shell/pluginhost.go | head`) and return the manifest's display name field; return `""` when unknown. Pass `r.pluginDisplayName` from `barChip`. `barChip` has `h` but not `r`: thread `r` in from `barLaneStripFor(r)`, which already has it. Callers with no registry pass `nil`.

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/settings ./internal/shell -run 'Widget|Bar|Lane|Chip'` — Expected: PASS.

- [ ] **Step 5: Commit** `git commit -m "fix(settings): name plugin widgets by their plugin"`

---

### Task 3: Segmented enums and the pick action

**Files:**
- Modify: `internal/shell/popout_settings.go` (`settingsControl` `KindEnum` case at `:427`)
- Modify: `internal/shell/panelhost.go` (the activate chain, beside the `step:` handler at `:2086`)
- Test: `internal/shell/popout_settings_test.go`

**Interfaces:**
- Produces: `const settingsSegmentLimit = 4`; `func settingsSegmented(h *PanelHost, e settings.Entry, raw string) *ui.Node`; `func settingsOptionLabel(opt string) string`; action `pick:<path>=<value>`.

- [ ] **Step 1: Failing test**

```go
func TestShortEnumsRenderSegmentedAndPickWrites(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	e := *h.set.ByPath("bar.edge") // two options: top, bottom
	n := settingsControl(h, e, 200)
	if n.Kind != ui.KindSegmented || len(n.Children) != 2 {
		t.Fatalf("bar.edge control = kind %v with %d children, want a 2-way segmented", n.Kind, len(n.Children))
	}
	if n.Children[0].Action != "pick:bar.edge=top" || n.Children[0].State&ui.StateSelected == 0 {
		t.Fatalf("first segment = %q selected=%v", n.Children[0].Action, n.Children[0].State&ui.StateSelected != 0)
	}
	menu := e
	menu.Present = settings.PresentMenu
	if settingsControl(h, menu, 200).Kind == ui.KindSegmented {
		t.Error("PresentMenu still rendered segmented")
	}
	if osd := h.set.ByPath("osd.position"); osd != nil && settingsControl(h, *osd, 200).Kind == ui.KindSegmented {
		t.Error("a nine-option enum rendered segmented")
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
	h.activateNode(reg, &ui.Node{Kind: ui.KindButton, Action: "pick:bar.edge=bottom"})
	if h.draft.Bar.Edge != "bottom" {
		t.Fatalf("draft edge = %q after pick", h.draft.Bar.Edge)
	}
}
```

Before writing it, find the activate entry point's real name (`grep -n 'func (h \*PanelHost) activate' internal/shell/panelhost.go`) and use it in place of `activateNode`. Also confirm the OSD position entry's path (`grep -n 'osdPositions' internal/settings/registry.go`).

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Segmented|PickAction'` — Expected: FAIL.

- [ ] **Step 3: Implement**

In `settingsControl`:

```go
	case settings.KindEnum:
		if e.Present == settings.PresentAuto && len(e.Options) >= 2 && len(e.Options) <= settingsSegmentLimit {
			return settingsSegmented(h, e, raw)
		}
		return settingsMenuControl(h, e, e.Options, raw, width)
```

Add:

```go
// settingsSegmentLimit is the most options a segmented control shows. Past
// it the labels crowd the control column and a menu reads better (design D2).
const settingsSegmentLimit = 4

// settingsSegmented shows every option at once, the way the audio panel's
// tabs do. Each segment writes its value through the pick action.
func settingsSegmented(h *PanelHost, e settings.Entry, raw string) *ui.Node {
	m := h.metrics()
	seg := &ui.Node{
		Kind: ui.KindSegmented, Key: "seg:" + e.Path, Gap: theme.MarginXXS,
		Height: m.CompactControl, Name: e.Label, Role: "radiogroup",
	}
	for _, opt := range e.Options {
		label := settingsOptionLabel(opt)
		b := &ui.Node{
			Kind: ui.KindButton, Action: "pick:" + e.Path + "=" + opt,
			Name: label, Role: "radio", Focusable: true, Height: m.CompactControl,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
		}
		if opt == raw {
			b.State |= ui.StateSelected
		}
		seg.Children = append(seg.Children, b)
	}
	return seg
}

// settingsOptionLabel turns a config value into a label: "auto-pause" reads
// "Auto pause".
func settingsOptionLabel(opt string) string {
	s := strings.ReplaceAll(opt, "-", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
```

In the activate chain in `panelhost.go`, after the `step:` block:

```go
	if rest, ok := strings.CutPrefix(n.Action, "pick:"); ok {
		path, value, found := strings.Cut(rest, "=")
		if e := h.set.ByPath(path); found && e != nil {
			h.commitSetting(r, e, value)
			r.rebuildPanel(h)
		}
		return true
	}
```

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Settings|Segmented|Pick|Stepper|Reset'` — Expected: PASS. `TestSettingsEntryRendersMatchingControl` asserts a `KindMenu` exists on the Bar page. If Bar has no enum left with more than four options, point that test at a section that still has one (Appearance's font picker is a menu).

- [ ] **Step 5: Commit** `git commit -m "feat(settings): segmented control for short enums"`

---

### Task 4: Groups as cards, rows inside them

**Files:**
- Modify: `internal/shell/popout_settings.go` (`settingsSectionColumn` `:271`, `settingsEntryRow` `:301`, `settingsSearchColumn` `:240`)
- Test: `internal/shell/popout_settings_test.go`

**Interfaces:**
- Consumes: Task 1's `PageEntries`.
- Produces: `func settingsEntryRow(h *PanelHost, e settings.Entry, width int) *ui.Node` (gains `width`); `func settingsGroupCard(h *PanelHost, title string, rows []*ui.Node) *ui.Node`; `func settingsPageColumn(h *PanelHost, entries []settings.Entry, lead ...*ui.Node) *ui.Node`.

- [ ] **Step 1: Failing test**

```go
func TestGroupsRenderAsCardsWithRowsInside(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Appearance"
	h.place.Panel = ui.Rect{W: 1106, H: 760}
	h.root = settingsTree(nil, h)
	cards := 0
	for _, n := range walk(h.root) {
		if n.Kind != ui.KindCapsule || n.Shape != ui.ShapeCard {
			continue
		}
		cards++
		col := n.Children[0]
		if col.Children[0].TextRole != theme.RoleLabel {
			t.Errorf("card does not open with its title: %+v", col.Children[0])
		}
		for _, row := range col.Children[1:] {
			if row.Kind == ui.KindRow && row.Width > settingsBodyWidth(h)-2*h.metrics().CardPadding {
				t.Errorf("row %d wide overruns its card", row.Width)
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
```

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run GroupsRenderAsCards` — Expected: FAIL (no cards).

- [ ] **Step 3: Implement**

Change `settingsEntryRow` to take `width int` and use it everywhere it used `settingsBodyWidth(h)`: the label width becomes `max(width-controlW-theme.MarginL, 0)` and the row gets `Width: width`. `settingsControlWidth` stays body-derived.

Add:

```go
// settingsGroupCard is one group as a titled card (design D3, reversing the
// foundation's "no cards").
func settingsGroupCard(h *PanelHost, title string, rows []*ui.Node) *ui.Node {
	m := h.metrics()
	col := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM}
	if title != "" {
		col.Children = append(col.Children, &ui.Node{Kind: ui.KindText, Text: title, TextRole: theme.RoleLabel})
	}
	col.Children = append(col.Children, rows...)
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
		Shape: ui.ShapeCard, Width: settingsBodyWidth(h), Children: []*ui.Node{col},
	}
}

// settingsPageColumn is a page's groups as cards, after any lead blocks (the
// bar preview, the picture cards), in the scrolling body.
func settingsPageColumn(h *PanelHost, entries []settings.Entry, lead ...*ui.Node) *ui.Node {
	rowW := settingsBodyWidth(h) - 2*h.metrics().CardPadding
	var order []string
	rows := map[string][]*ui.Node{}
	for _, e := range entries {
		if _, seen := rows[e.Group]; !seen {
			order = append(order, e.Group)
		}
		rows[e.Group] = append(rows[e.Group], settingsEntryRow(h, e, rowW))
	}
	children := append([]*ui.Node{}, lead...)
	for _, g := range order {
		children = append(children, settingsGroupCard(h, g, rows[g]))
	}
	return settingsBody(h, theme.MarginL, children...)
}
```

Make `settingsSectionColumn` call `settingsPageColumn(h, entries)` after its empty-note check. In `settingsSearchColumn`, pass `settingsBodyWidth(h)` as the row width. Hits stay plain rows, grouped under a caption of the section and, when the entry has one, its page: `"Bar › Appearance"`. Group by that caption, in rail order, instead of by section alone. Add a test that searching "style" shows the caption `Bar › Appearance`.

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Settings|Row|Field|Control|Cards'` — Expected: PASS. `TestRowColumnsFitInsideTheBody` still holds: rows are narrower now.

- [ ] **Step 5: Commit** `git commit -m "feat(settings): group cards"`

---

### Task 5: Labelled rail, page tabs, and addressing

**Files:**
- Modify: `internal/shell/popout_settings.go` (`settingsRail` `:118`, `settingsTree` `:139`, `settingsRailWidth` const `:31`)
- Modify: `internal/shell/panelhost.go` (`PanelHost` fields near `settingsScroll` `:131`; `section:` handler `:2077`; `panelSection` `:341`; `selectPanelSectionLocked` `:376`)
- Test: `internal/shell/popout_settings_test.go`

**Interfaces:**
- Consumes: Task 1's `SectionClusters`, `SectionPages`, `PageEntries`.
- Produces: `PanelHost.settingsPage string`; action `page:<name>`; `func settingsAddress(requested string) (section, page string, ok bool)`; `settingsRailWidth = 208`.

- [ ] **Step 1: Failing tests**

```go
func TestRailIsLabelledAndClustered(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	rail := settingsRail(h, "Bar")
	var captions, tabs []string
	for _, n := range walk(rail) {
		if n.Role == "tab" {
			tabs = append(tabs, n.Name)
			if len(walkText(n)) == 0 {
				t.Errorf("rail tab %s has no visible label", n.Name)
			}
		}
		if n.TextRole == theme.RoleCaption && n.Role == "heading" {
			captions = append(captions, n.Text)
		}
	}
	if !slices.Equal(tabs, settings.SectionNames()) {
		t.Errorf("tabs %v, want %v", tabs, settings.SectionNames())
	}
	if !slices.Equal(captions, []string{"Look", "Bar", "Panels", "System"}) {
		t.Errorf("captions %v", captions)
	}
}

func TestBarOpensOnAppearanceAndPagesSwitch(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	if byName(h.root, "Style") == nil {
		t.Fatal("Bar did not open on Appearance: no Style row")
	}
	h.settingsPage = "Layout"
	h.root = settingsTree(nil, h)
	if byName(h.root, "Style") != nil {
		t.Error("Style shows on the Layout page")
	}
	for _, page := range []string{"Appearance", "Layout", "Displays"} {
		if findAction(h.root, "page:"+page) == nil {
			t.Errorf("no tab for page %s", page)
		}
	}
}

func TestOldDisplaysAddressLandsOnBarDisplays(t *testing.T) {
	t.Parallel()
	for req, want := range map[string][2]string{
		"Displays":     {"Bar", "Displays"},
		"Bar":          {"Bar", "Appearance"},
		"Bar/Layout":   {"Bar", "Layout"},
		"Appearance":   {"Appearance", ""},
	} {
		s, p, ok := settingsAddress(req)
		if !ok || s != want[0] || p != want[1] {
			t.Errorf("%q → %q %q %v, want %v", req, s, p, ok, want)
		}
	}
	if _, _, ok := settingsAddress("Nowhere"); ok {
		t.Error("an unknown section was accepted")
	}
}
```

Use the package's existing helpers where they exist (`walk`, `byRole`, `findByName`); write `walkText`, `byName` and `findAction` as small test helpers at the bottom of the file if they do not.

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Rail|Pages|Displays'` — Expected: FAIL.

- [ ] **Step 3: Implement**

`settingsRailWidth` becomes a constant 208, and `settingsRailItem` stays as the row height. `settingsRail` walks `settings.SectionClusters()` and emits, per cluster, a caption `{Kind: KindText, Text: c.Name, TextRole: RoleCaption, Tone: ToneSubtle, Role: "heading"}`, then per section a button `{Kind: KindButton, Width: settingsRailWidth, Height: settingsRailItem, Action: "section:"+name, Name: name, Role: "tab", Shape: ShapeMedium}`. Its child is a row with the icon and a `KindText` label, and the selected tab keeps `FillAccent`. The rail column starts with the search field (`h.search.Node("Search")`, `Width: settingsRailWidth`), so Search stays the first focusable; `TestSettingsSidebarSectionsAndFocus` pins that. Remove the search field from the header row.

Add `settingsPage string` beside `settingsScroll` on `PanelHost`. In `settingsTree`, after resolving `section`:

```go
	pages := settings.SectionPages(section)
	page := h.settingsPage
	if len(pages) > 0 && !slices.Contains(pages, page) {
		page = pages[0]
	}
```

The header becomes the title and, when `len(pages) > 0`, a segmented control whose buttons carry `page:<name>` (`Role: "tab"`, selected when equal to `page`). Section content: when `section == "Bar"`, hand off to `settingsBarPage(r, h, page)`, which Task 8 writes. Until Task 8, have it return `settingsPageColumn(h, h.set.PageEntries("Bar", page))`, with `h.barLaneStripFor(r)` as the only child on Layout. Other sections use `settingsSectionColumn` as today.

In the activate chain, beside `section:`:

```go
	if page, ok := strings.CutPrefix(n.Action, "page:"); ok {
		h.settingsPage = page
		h.settingsScroll = 0
		r.rebuildPanel(h)
		return true
	}
```

And in the `section:` handler for settings, reset `h.settingsPage = ""` and `h.settingsScroll = 0`.

Addressing:

```go
// settingsAddress resolves an IPC or shortcut section name. "Section/Page"
// picks a page; "Displays" is the old rail section, now Bar › Displays.
func settingsAddress(requested string) (section, page string, ok bool) {
	section, page, _ = strings.Cut(requested, "/")
	if section == "Displays" {
		section, page = "Bar", "Displays"
	}
	if !slices.Contains(settingsSections, section) {
		return "", "", false
	}
	pages := settings.SectionPages(section)
	switch {
	case len(pages) == 0:
		return section, "", page == ""
	case page == "":
		return section, pages[0], true
	default:
		return section, page, slices.Contains(pages, page)
	}
}
```

`panelSection`'s `PanelSettings` case validates with `settingsAddress` and returns `requested` unchanged. `selectPanelSectionLocked`, for Settings, splits with `settingsAddress` and sets both `h.section` and `h.settingsPage`. Check `TestControlCentreShortcutReachesEverySection` and the control centre's shortcut table for a `Displays` target; the alias keeps it working.

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Settings|Rail|Pages|Displays|Section|ControlCentreShortcut|Search'` — Expected: PASS. Then write the Review Focus 4 test, which types a query on Bar › Layout, clears it, and asserts `h.section == "Bar" && h.settingsPage == "Layout"`. Run again.

- [ ] **Step 5: Commit** `git commit -m "feat(settings): labelled rail, page tabs and page addressing"`

---

### Task 6: Responsive size and the opaque floor

**Files:**
- Modify: `internal/shell/panelhost.go` (`panelTargetSize` `PanelSettings` case `:2417`; the responsive-size block at `:643`; `rootStyle` `:1001`)
- Test: `internal/shell/popout_settings_test.go`

**Interfaces:**
- Produces: `func settingsPanelSize(outputW, outputH int) ui.Rect`; `const settingsOpacityFloor uint8 = 0xf0`.

- [ ] **Step 1: Failing test**

```go
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
```

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'SettingsSize|OpaqueFloor'` — Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// settingsPanelSize is design D10: 72 percent of the output's width and 88
// percent of its height, capped at 1120x820.
func settingsPanelSize(outputW, outputH int) ui.Rect {
	return ui.Rect{W: min(1120, outputW*72/100), H: min(820, outputH*88/100)}
}

// settingsOpacityFloor is the least alpha the settings root paints at (D7):
// it is read for minutes, over whatever is behind it.
const settingsOpacityFloor uint8 = 0xf0
```

`panelTargetSize(PanelSettings)` returns `settingsPanelSize(1920, 1080)`. In the open path, beside `if id == PanelAudio { size = audioPanelSize(outW, outH) }`, add the same for `PanelSettings`. At the end of `rootStyle`, before each return, apply `if h.id == PanelSettings { s.SurfaceOpacity = max(s.SurfaceOpacity, settingsOpacityFloor) }`. Restructure it to a single `s` and one return so the floor is applied once.

`TestEveryPanelLaysOutAtItsNarrowestWidth` and `TestSettingsBodyLeavesRoomForTheRail` assume 900. Update them to the fitted size on 1280×720 (921×633), per design D10.

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Settings|Narrowest|Panel'` — Expected: PASS apart from the known failures.

- [ ] **Step 5: Commit** `git commit -m "feat(settings): size to the output, paint at an opaque floor"`

---

### Task 7: The bar preview and picture cards

**Files:**
- Create: `internal/shell/settings_barpreview.go`
- Test: `internal/shell/settings_barpreview_test.go`

**Interfaces:**
- Consumes: `NewWithTheme(theme Theme, policy config.Bar, connector string) (*Bar, error)`, `(*Bar).apply(barView) bool`, `(*Bar).Configure(w, h, scale120 int) error`, `(*Bar).Render(pix []byte, w, h, stride int) error`, `(*Bar).stopAnimation()`, `resolveOutputTheme(cfg, connector, tok, blur) (Theme, error)`, `(*Registry).viewLocked(connector) barView`; Task 3's `settingsOptionLabel`.
- Produces: `func (r *Registry) settingsBarImage(h *PanelHost, cfg config.Config, width int) *ui.Image`; `func settingsBarPreview(r *Registry, h *PanelHost) *ui.Node`; `func settingsPictureCards(r *Registry, h *PanelHost, e settings.Entry) *ui.Node`; `PanelHost.barPreview *ui.Image` (last good image).

- [ ] **Step 1: Failing tests**

```go
func TestBarPreviewPaintsTheDraft(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	opaque := func(img *ui.Image) int {
		n := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] == 0xff {
				n++
			}
		}
		return n
	}
	solid := h.draft
	solid.Bar.Style = "solid"
	islands := h.draft
	islands.Bar.Style = "islands"
	a := reg.settingsBarImage(h, solid, 600)
	b := reg.settingsBarImage(h, islands, 600)
	if a == nil || b == nil || a.Width == 0 {
		t.Fatal("no preview image")
	}
	if opaque(b) >= opaque(a) {
		t.Errorf("islands preview has %d opaque pixels, solid %d: islands should paint no ground", opaque(b), opaque(a))
	}
}

func TestStyleRendersOnePictureCardPerOption(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	cards := settingsPictureCards(reg, h, *h.set.ByPath("bar.style"))
	if len(cards.Children) != 3 {
		t.Fatalf("%d style cards, want 3", len(cards.Children))
	}
	for i, opt := range []string{"frosted", "solid", "islands"} {
		c := cards.Children[i]
		if c.Action != "pick:bar.style="+opt || findAllKind(c, ui.KindImage) == nil {
			t.Errorf("card %d = %q without a picture", i, c.Action)
		}
		if (c.State&ui.StateSelected != 0) != (opt == h.draft.Bar.Style) {
			t.Errorf("card %s selection is wrong", opt)
		}
	}
	for _, scale := range []int{120, 150, 180} {
		h.scale120 = scale
		if err := ui.LayoutColumn(settingsPictureCards(reg, h, *h.set.ByPath("bar.shape")),
			ui.Rect{W: settingsBodyWidth(h), H: 400}, h.measureText()); err != nil {
			t.Errorf("scale %d: %v", scale, err)
		}
	}
}

func TestPreviewKeepsTheLastGoodImageOnABadDraft(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	good := settingsBarPreview(reg, h)
	h.draft.Bar.Height = -5 // fails theme validation
	bad := settingsBarPreview(reg, h)
	gi, bi := findAllKind(good, ui.KindImage), findAllKind(bad, ui.KindImage)
	if len(gi) == 0 || len(bi) == 0 || bi[0].Image != gi[0].Image {
		t.Fatal("a failing draft replaced the preview instead of keeping the last good image")
	}
}
```

If `Bar.Height = -5` passes validation, use any draft value `ResolveTheme` rejects. Find one with `grep -n 'return Theme{}, ' internal/shell/theme.go`.

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'BarPreview|PictureCard|LastGoodImage'` — Expected: FAIL (undefined).

- [ ] **Step 3: Implement** `internal/shell/settings_barpreview.go`

```go
package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// settingsBarImage paints the bar cfg describes, width logical pixels wide, at
// the panel's scale (design D6). It is resolved as if the compositor blurs, so
// a translucent style shows as translucent. Nil when cfg does not resolve.
// Callers hold r.mu.
func (r *Registry) settingsBarImage(h *PanelHost, cfg config.Config, width int) *ui.Image {
	connector := ""
	if bar, ok := r.bars[h.output]; ok {
		connector = bar.connector()
	}
	policy := cfg.ForConnector(connector)
	t, err := resolveOutputTheme(cfg, connector, r.tokens, true)
	if err != nil {
		return nil
	}
	bar, err := NewWithTheme(t, policy, connector)
	if err != nil {
		return nil
	}
	defer bar.stopAnimation()
	bar.apply(r.viewLocked(connector))
	height := policy.SurfaceExtent()
	scale := h.scale120
	if scale <= 0 {
		scale = ui.ScaleUnit.Int()
	}
	if err := bar.Configure(width, height, scale); err != nil {
		return nil
	}
	pw, ph := width*scale/120, height*scale/120
	pix := make([]byte, pw*ph*4)
	if err := bar.Render(pix, pw, ph, pw*4); err != nil {
		return nil
	}
	return &ui.Image{Width: pw, Height: ph, Stride: pw * 4, Pix: pix}
}

// settingsBarPreview is the Appearance page's lead card: the draft's bar at
// the page's width. A draft that does not resolve keeps the last good image,
// so a half-typed value does not blank the preview.
func settingsBarPreview(r *Registry, h *PanelHost) *ui.Node {
	w := settingsBodyWidth(h) - 2*h.metrics().CardPadding
	if img := r.settingsBarImage(h, h.draft, w); img != nil {
		h.barPreview = img
	}
	height := h.draft.ForConnector("").SurfaceExtent()
	return settingsGroupCard(h, "Preview", []*ui.Node{{
		Kind: ui.KindImage, Image: h.barPreview, ImageW: w, ImageH: height,
		Name: "Bar preview", Role: "img",
	}})
}

// settingsPictureCardW is one picture card's bar segment, logical pixels.
const settingsPictureCardW = 240

// settingsPictureCards shows every option of e as a card with its own
// preview: the draft with that one value substituted (design D2, D6).
func settingsPictureCards(r *Registry, h *PanelHost, e settings.Entry) *ui.Node {
	raw := e.Get(h.draft)
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Name: e.Label, Role: "radiogroup"}
	for _, opt := range e.Options {
		cfg := h.draft
		if err := e.Set(&cfg, opt); err != nil {
			continue
		}
		img := r.settingsBarImage(h, cfg, settingsPictureCardW)
		label := settingsOptionLabel(opt)
		card := &ui.Node{
			Kind: ui.KindButton, Action: "pick:" + e.Path + "=" + opt,
			Name: label, Role: "radio", Focusable: true, Shape: ui.ShapeCard,
			Fill: ui.FillContainerHighest, Padding: h.metrics().CardPadding,
			Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
				{Kind: ui.KindImage, Image: img, ImageW: settingsPictureCardW,
					ImageH: cfg.ForConnector("").SurfaceExtent()},
				{Kind: ui.KindText, Text: label},
			}}},
		}
		if opt == raw {
			card.State |= ui.StateSelected
		}
		row.Children = append(row.Children, card)
	}
	return row
}
```

Add `barPreview *ui.Image` to `PanelHost` beside `settingsPage`. Check `ui.ScaleUnit`'s integer accessor (`grep -n 'ScaleUnit' internal/ui/scale.go`) and replace `.Int()` with what exists; `ScaleUnit` may simply be the constant `120`. Check that `(*Bar).connector()` exists under that name (it is called in `triggerLocked`). If three 240-wide cards overflow the body at 1.5 scale on 1280×720, set the row's children to `Width: (settingsBodyWidth(h)-2*theme.MarginM)/3` and give each image that width minus padding. The scale loop in the test catches it.

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'BarPreview|PictureCard|LastGoodImage'` then `GOMAXPROCS=4 go test -race -count=1 ./internal/shell -run 'BarPreview|PictureCard'` — Expected: PASS, no race.

- [ ] **Step 5: Commit** `git commit -m "feat(settings): offscreen bar preview and picture cards"`

---

### Task 8: Compose the Bar pages

**Files:**
- Modify: `internal/shell/popout_settings.go` (`settingsTree`'s Bar branch from Task 5; `settingsEmptySection` `:216`)
- Test: `internal/shell/popout_settings_test.go`

**Interfaces:**
- Consumes: Tasks 1, 3, 4, 7.
- Produces: `func settingsBarPage(r *Registry, h *PanelHost, page string) *ui.Node`.

- [ ] **Step 1: Failing tests**

```go
func TestBarAppearanceLeadsWithPreviewAndCards(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	if err := h.configure(int(reqs[1].Open.Width), int(reqs[1].Open.Height), 150); err != nil {
		t.Fatal(err)
	}
	body := findScroll(h.root)
	if body == nil || len(body.Children) < 3 {
		t.Fatal("Appearance body is missing its blocks")
	}
	if findAllKind(body.Children[0], ui.KindImage) == nil {
		t.Error("the first block is not the preview")
	}
	if findAction(body, "pick:bar.style=islands") == nil || findAction(body, "pick:bar.shape=floating") == nil {
		t.Error("Style or Shape picture cards are missing")
	}
	var styleY, frostY int
	for _, n := range walk(body) {
		switch n.Action {
		case "pick:bar.style=frosted":
			styleY = n.Bounds.Y
		case "set:bar.frost-opacity":
			frostY = n.Bounds.Y
		}
	}
	if styleY > frostY {
		t.Error("Style sits below the frost sliders")
	}
}

func TestFrostDimsWhenTheStyleIsSolidAndAllDimWhenTheBarIsOff(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.draft.Bar.Style = "solid"
	h.root = settingsTree(nil, h)
	frost := findAction(h.root, "set:bar.frost-opacity")
	if frost == nil || frost.State&ui.StateDisabled == 0 {
		t.Fatal("frost opacity is live under a solid bar")
	}
	h.draft.Bar.Enabled = false
	h.root = settingsTree(nil, h)
	if n := findAction(h.root, "set:bar.height"); n == nil || n.State&ui.StateDisabled == 0 {
		t.Fatal("bar height is live with the bar disabled")
	}
	if n := findAction(h.root, "set:bar.enabled"); n == nil || n.State&ui.StateDisabled != 0 {
		t.Fatal("the Enabled toggle itself was disabled")
	}
}
```

`Bar.Enabled` may be a pointer or named differently in `config.Bar`; check with `grep -n 'Enabled' internal/config/config.go | head` and adjust. With `r == nil` (the `newSettingsHost` path), `settingsBarPage` must skip the preview and render plain picture-card labels without images, so the dimming test needs no registry.

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'BarAppearance|FrostDims'` — Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// settingsBarPage is one of Bar's pages (design D8).
func settingsBarPage(r *Registry, h *PanelHost, page string) *ui.Node {
	entries := h.set.PageEntries("Bar", page)
	switch page {
	case "Layout":
		if r == nil {
			return settingsBody(h, theme.MarginL)
		}
		return settingsBody(h, theme.MarginL, h.barLaneStripFor(r))
	case "Displays":
		if len(entries) == 0 {
			return settingsBody(h, theme.MarginL, settingsEmptyNote("Displays"))
		}
		return settingsPageColumn(h, entries)
	}
	var lead []*ui.Node
	if r != nil {
		lead = append(lead, settingsBarPreview(r, h))
	}
	var rest []settings.Entry
	for _, e := range entries {
		if e.Present == settings.PresentCards {
			lead = append(lead, settingsGroupCard(h, e.Label, []*ui.Node{settingsCardsFor(r, h, e)}))
			continue
		}
		rest = append(rest, e)
	}
	col := settingsPageColumn(h, rest, lead...)
	settingsDimBarRows(h, col)
	return col
}

// settingsCardsFor is the picture cards, or labelled choices with no pictures
// when there is no registry to paint from (tests, and a pane built before a
// bar exists).
func settingsCardsFor(r *Registry, h *PanelHost, e settings.Entry) *ui.Node {
	if r != nil {
		return settingsPictureCards(r, h, e)
	}
	return settingsSegmented(h, e, e.Get(h.draft))
}

// settingsDimBarRows disables what does not apply: the frost sliders under a
// solid bar, and everything but Enabled when the bar is off. The row stays
// visible with its reason, so the user learns why rather than hunting.
func settingsDimBarRows(h *PanelHost, root *ui.Node) {
	off := !h.draft.Bar.Enabled
	solid := h.draft.Bar.Style == "solid"
	for _, n := range walk(root) {
		path := strings.TrimPrefix(strings.TrimPrefix(n.Action, "set:"), "pick:")
		path, _, _ = strings.Cut(path, "=")
		if !strings.HasPrefix(path, "bar.") || path == "bar.enabled" {
			continue
		}
		if off || (solid && (path == "bar.frost-opacity" || path == "bar.pill-opacity")) {
			n.State |= ui.StateDisabled
			n.Focusable = false
		}
	}
}
```

`walk` is the test helper name; if the package has no non-test walker, add one here (`func settingsWalk(n *ui.Node) []*ui.Node`) and use it. Give the Frost entries' `Describe` text in `internal/settings/registry.go` a clause the dimmed row can rely on: "Applies to the Frosted and Islands styles." Move the "Displays" empty note text to say "No output overrides the bar yet. Every display follows the settings on Appearance."

- [ ] **Step 4: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run 'Settings|Bar|Frost|Lane|Chip'` — Expected: PASS apart from the known failures.

- [ ] **Step 5: Commit** `git commit -m "feat(settings): Bar appearance, layout and displays pages"`

---

### Task 9: Layout matrix, full gate, live gate, handover

**Files:**
- Test: `internal/shell/settings_matrix_test.go` (create)
- Create: `docs/plans/2026-09-2X-settings-redesign-completion-handover.md` and its register row in `docs/plans/README.md`

- [ ] **Step 1: Write the matrix test**

```go
// TestEverySettingsPageLaysOutEverywhere opens every section and page at the
// fitted size on three outputs and three scales. A layout error closes the
// surface in production (the audio panel did exactly that), so any error here
// is a user-visible failure.
func TestEverySettingsPageLaysOutEverywhere(t *testing.T) {
	outputs := [][2]int{{1280, 720}, {1536, 864}, {3440, 1440}}
	for _, out := range outputs {
		reg := newPanelRegistry(t)
		withTestBar(t, reg, 7, reg.cfg)
		if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: out[0], OutH: out[1]}); err != nil {
			t.Fatal(err)
		}
		reqs := drainAux(t, reg, 2)
		panel := reqs[1].Open
		for _, section := range settings.SectionNames() {
			pages := settings.SectionPages(section)
			if len(pages) == 0 {
				pages = []string{""}
			}
			for _, page := range pages {
				for _, scale := range []int{120, 150, 180} {
					reg.mu.Lock()
					h := reg.panelHosts[PanelSettings]
					h.section, h.settingsPage, h.scale120 = section, page, scale
					reg.rebuildPanel(h)
					reg.mu.Unlock()
					if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale); err != nil {
						t.Errorf("%dx%d %s/%s @%d: %v", out[0], out[1], section, page, scale, err)
					}
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run** `GOMAXPROCS=4 go test -count=1 ./internal/shell -run EverySettingsPage`. Fix every failure at its owner. An enum whose segmented labels overflow gets `Present: PresentMenu` in the registry. A card row that overflows is fixed in Task 7's card width rule. Re-run until it passes.

- [ ] **Step 3: Full gate**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
GOMAXPROCS=4 go vet -p 2 ./...
git diff --exit-code origin/main -- go.mod go.sum
GOMAXPROCS=4 go test -p 2 -count=1 ./... 2>&1 | grep -E '^(FAIL|--- FAIL)'
for p in settings shell render ui; do GOMAXPROCS=4 go test -race -count=1 ./internal/$p 2>&1 | tail -1; done
```

Expected: only the known failures from Global Constraints. The race run over `internal/shell` includes those four too.

- [ ] **Step 4: Live gate (laptop, then desktop only with the owner's say-so)**

Build, keep a rollback copy (`~/.local/bin/sysc-shell.before-settings-<ts>`), deploy with `mv` and `systemctl --user restart sysc-shell`, and check the journal for `closing surface`. Open each section and page over IPC with `panel.open` (`{"panel":"settings","section":"Bar/Appearance"}` etc.) and capture with `grim` in the session environment. Check that `section: "Displays"` lands on Bar › Displays. The owner clicks each Style and Shape card and confirms the live bar changes. Interaction cannot be injected here (`wtype` closes panels).

- [ ] **Step 5: Handover and close**

Write the completion handover: commits, gate output, capture list, owner's live confirmation, anything that failed. Add its register row. Close the bd issue for this work with `bd close <id> --reason "..."` from `/home/nomadx/sysc-shell`. Commit the handover docs-only.

# Settings foundation implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the settings schema and pane so one setting is defined in one place, rows carry descriptions and reset, and every configuration domain is reachable.

**Architecture:** Each registry entry carries its own typed `get`/`set` closures, deleting the two switch statements. The pane drops `KindVirtualList` for a plain grouped `KindScroll` column per section. Apply stays live but debounces through the existing `scheduleControl` seam, and `PrepareConfig` re-seeds an open draft so an external change is not reverted.

**Tech Stack:** Go 1.26, `internal/settings`, `internal/shell`, `internal/config`, `internal/ui`, `go-text/typesetting` (`fontscan`).

**Spec:** [`2026-09-15-settings-foundation-design.md`](2026-09-15-settings-foundation-design.md)

**Issue:** `sysc-320`, under epic `sysc-319`. `sysc-107` is a child and is closed by Task 5.

## Global Constraints

- Go only. No C++, Rust, Lua, QML, or Quickshell.
- **Never run `go test` with `-race` or with `./...`.** A repository-wide race build hard-locks this workstation, and `.cursor/hooks/deny-go-race.py` refuses both forms. The gate is one package at a time:
  ```bash
  timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
  ```
- `gofmt -w . && test -z "$(gofmt -l .)"` and `go vet ./...` are still required on every code-touching commit.
- **Do not install a `loginctl` shim.** Earlier plans prescribe one; it is obsolete. `popout_session.go` guards with `testing.Testing()` and checks `argv[0]`, so `go test ./internal/shell` is safe to run directly.
- Geometry derives from `theme.Metrics` and the margin ladder, never from literals. A new pane full of hardcoded heights adds to the conformance census `sysc-265` is shrinking.
- **Screen every commit message** against the machine hook before committing:
  ```bash
  grep -qiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED
  ```
  It matches substrings, so ordinary words fail: `both`, `bottom`, `precursor`, `Hallmark`. Add no `Co-Authored-By` trailer.
- The bd `pre-commit` hook runs `bd sync --flush-only` then `git add .beads/issues.jsonl` unconditionally, so every commit sweeps pending tracker state. Before committing that file, check `wc -l .beads/issues.jsonl` against `git show HEAD:.beads/issues.jsonl | wc -l` — auto-flush has truncated it to one line — and confirm the diff names only IDs you touched. A full `bd export` drops `comments`; eleven issues carry them.

## File Structure

| File | Responsibility |
|---|---|
| `internal/settings/entry.go` | **New.** `Entry`, its kinds, and the typed accessor fields. Split out because `registry.go` currently mixes the type, the catalogue, and two 200-line switches. |
| `internal/settings/registry.go` | The catalogue only: section and group ordering, construction, lookup, search. |
| `internal/settings/registry_test.go` | Accessor round-trip, bounds, reset rules, and the domain-coverage invariant. |
| `internal/shell/popout_settings.go` | The pane: rail, header, search, grouped column, row anatomy. |
| `internal/shell/popout_settings_test.go` | Pane behaviour. |
| `internal/shell/panelhost.go` | Draft lifecycle, debounced commit, registry rebuild. |
| `internal/shell/registry.go` | `PrepareConfig.Commit` re-seeds open settings drafts. |

---

### Task 1: Entry carries typed accessors

**Files:**
- Create: `internal/settings/entry.go`
- Modify: `internal/settings/registry.go`
- Test: `internal/settings/registry_test.go`

**Interfaces:**
- Produces: `type Getter func(config.Config) string`, `type Setter func(*config.Config, string) error`, and `Entry` fields `Get Getter`, `Set Setter`, `Describe string`, `Group string`, `Default Getter`.

- [ ] **Step 1: Write the failing test**

In `internal/settings/registry_test.go`:

```go
func TestEntryUsesItsOwnAccessors(t *testing.T) {
	e := settings.Entry{
		Path: "test.flag", Label: "Flag", Section: "Bar", Kind: settings.KindBool,
		Get: func(c config.Config) string { return strconv.FormatBool(c.Bar.Enabled) },
		Set: func(c *config.Config, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			c.Bar.Enabled = b
			return nil
		},
	}
	cfg := config.Default()
	cfg.Bar.Enabled = false
	if got := e.Get(cfg); got != "false" {
		t.Fatalf("Get = %q, want false", got)
	}
	if err := e.Set(&cfg, "true"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Bar.Enabled {
		t.Fatal("Set did not reach the field")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings -run TestEntryUsesItsOwnAccessors
```

Expected: FAIL — `unknown field Get in struct literal`.

- [ ] **Step 3: Add the type**

Create `internal/settings/entry.go`:

```go
package settings

import "github.com/Nomadcxx/sysc-shell/internal/config"

// Getter reads one setting out of a configuration.
type Getter func(config.Config) string

// Setter writes one setting, validating the string form itself.
type Setter func(*config.Config, string) error

// Entry is one setting. Its accessors live here rather than in a switch so a
// setting is declared in exactly one place and the compiler enforces that a
// new entry carries them.
type Entry struct {
	Path     string
	Label    string
	Describe string
	Section  string
	Group    string
	Kind     Kind
	Options  []string
	Min, Max int

	Get Getter
	Set Setter
	// Default resolves this entry's default. It is per-entry because the
	// writer uses two rules: theme axes diff against the selected preset,
	// everything else against config.Default().
	Default Getter
}
```

Move `Kind` and its constants from `registry.go` into this file. Delete the old `Entry` struct and the `func (e Entry) Get`/`Set` methods from `registry.go`, along with `setBool`, `setInt`, and `setString`.

- [ ] **Step 4: Run it and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings -run TestEntryUsesItsOwnAccessors
```

Expected: PASS. The package will not build until Task 2 converts the catalogue; that is expected and Task 2 closes it.

- [ ] **Step 5: Commit**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
git add internal/settings/entry.go internal/settings/registry.go internal/settings/registry_test.go
git commit -m "refactor(settings): give each entry its own accessors"
```

---

### Task 2: Convert the catalogue and delete the switches

**Files:**
- Modify: `internal/settings/registry.go`
- Test: `internal/settings/registry_test.go`

**Interfaces:**
- Consumes: `Entry.Get`/`Set` from Task 1.
- Produces: a building `internal/settings` package with all 37 entries carrying accessors.

- [ ] **Step 1: Confirm the package is red**

```bash
timeout 90s env GOMAXPROCS=2 go build ./internal/settings
```

Expected: FAIL — the catalogue still uses the removed methods.

- [ ] **Step 2: Convert every entry**

Each entry gains its accessors inline. The pattern, for the three kinds already present:

```go
{
	Path: "bar.enabled", Label: "Enabled", Section: "Bar", Group: "Surface",
	Describe: "Draw the bar on every output.",
	Kind:     KindBool,
	Get:      func(c config.Config) string { return strconv.FormatBool(c.Bar.Enabled) },
	Set: func(c *config.Config, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("settings: bar.enabled: %q is not a truth value", v)
		}
		c.Bar.Enabled = b
		return nil
	},
},
{
	Path: "appearance.radius", Label: "Radius", Section: "Appearance", Group: "Shape",
	Describe: "Corner radius for panels and cards.",
	Kind:     KindInt, Min: theme.RadiusMin, Max: theme.RadiusMax,
	Get:      func(c config.Config) string { return strconv.Itoa(c.Theme.Radius) },
	Set: func(c *config.Config, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("settings: appearance.radius: %q is not a number", v)
		}
		if n < theme.RadiusMin || n > theme.RadiusMax {
			return fmt.Errorf("settings: appearance.radius: %d is outside %d..%d",
				n, theme.RadiusMin, theme.RadiusMax)
		}
		c.Theme.Radius = n
		return nil
	},
},
```

Carry every existing behaviour verbatim, including the two that are not plain field writes: `appearance.palette` sets `Source` to `"palette"` as well as `Seed`, and `appearance.preset` rebases the composition through `theme.Rebase` before assigning `Preset`.

Keep `addTemplateEntries` and `addWidgetEntries`, giving their generated entries accessors the same way.

- [ ] **Step 3: Run the existing suite unchanged**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings
```

Expected: PASS — all ten existing tests, unmodified. They are the proof the conversion changed no behaviour.

- [ ] **Step 4: Commit**

```bash
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/settings
git add internal/settings/registry.go
git commit -m "refactor(settings): convert the catalogue to accessors"
```

---

### Task 3: The domain-coverage invariant

**Files:**
- Modify: `internal/settings/registry.go`
- Test: `internal/settings/registry_test.go`

**Interfaces:**
- Produces: entries for `Weather`, `Wallpaper`, `Tray`, `Outputs`, and `Plugins.Enabled`.

This is the guard that stops the five-of-twelve gap reopening. Write it first; it fails until the entries exist.

- [ ] **Step 1: Write the failing test**

```go
func TestEveryConfigDomainHasAnEntry(t *testing.T) {
	r := settings.Default()
	want := []string{
		"bar.", "appearance.", "theme.templates.", "panels.", "session.",
		"accessibility.", "weather.", "wallpaper.", "tray.", "outputs.", "plugins.",
	}
	for _, prefix := range want {
		found := false
		for _, section := range settings.SectionNames() {
			for _, e := range r.Section(section) {
				if strings.HasPrefix(e.Path, prefix) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("no settings entry reaches the %q domain", prefix)
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings -run TestEveryConfigDomainHasAnEntry
```

Expected: FAIL naming `weather.`, `wallpaper.`, `tray.`, `outputs.`, `plugins.`.

- [ ] **Step 3: Add the missing entries and `SectionNames`**

Add a `SectionNames()` returning the twelve sections in order: Appearance, Templates, Bar, Widgets, Panels, Wallpaper, Weather, Displays, Tray, Plugins, Session, Accessibility.

Add entries covering `Weather{Latitude, Longitude, Unit, Interval}`, `Wallpaper{ImageDirectory, VideoDirectory, Scale, Loop, FPS, Fade, FadeDuration, Hidden}`, `Tray{Hidden, Pinned}` as discovered-item toggles, `Outputs` per-connector overrides, and `Plugins.Enabled` as per-plugin toggles.

Follow `config.Default()` and the loader's closed vocabularies exactly: `wallpaperScales` is fill/stretch/original/panscan, `wallpaperFPS` is 30/60/100, `wallpaperHidden` is none/auto-pause/auto-stop, and `weatherUnits` is celsius/fahrenheit. An enum whose options disagree with the loader produces a setting that writes a file the shell then refuses to load.

- [ ] **Step 4: Run it and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/settings
git add internal/settings/registry.go internal/settings/registry_test.go
git commit -m "feat(settings): expose every configuration domain"
```

---

### Task 4: Per-entry reset, with the writer's own two rules

**Files:**
- Modify: `internal/settings/registry.go`, `internal/settings/entry.go`
- Test: `internal/settings/registry_test.go`

**Interfaces:**
- Produces: `func (e Entry) IsDefault(c config.Config) bool`.

- [ ] **Step 1: Write the failing test**

```go
func TestResetUsesThePresetForThemeAxes(t *testing.T) {
	cfg := config.Default()
	cfg.Theme.Preset = theme.PresetCompact
	comp, ok := theme.PresetComposition(theme.PresetCompact)
	if !ok {
		t.Fatal("compact preset is missing")
	}
	cfg.Theme.Composition = comp

	r := settings.DefaultFor(cfg)
	e := r.ByPath("appearance.radius")
	if e == nil {
		t.Fatal("appearance.radius is missing")
	}
	if !e.IsDefault(cfg) {
		t.Fatal("an axis sitting on its preset value must read as default")
	}
	if got := e.Default(cfg); got != strconv.Itoa(comp.Radius) {
		t.Fatalf("Default = %q, want the preset's %d", got, comp.Radius)
	}

	acc := r.ByPath("accessibility.reduced-motion")
	if got := acc.Default(cfg); got != "false" {
		t.Fatalf("a non-theme entry defaults against config.Default(), got %q", got)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings -run TestResetUsesThePresetForThemeAxes
```

Expected: FAIL — `e.IsDefault undefined`.

- [ ] **Step 3: Implement**

In `entry.go`:

```go
// IsDefault reports whether this setting still sits on its default, which is
// what decides whether a row shows a reset control.
func (e Entry) IsDefault(c config.Config) bool {
	if e.Get == nil || e.Default == nil {
		return true
	}
	return e.Get(c) == e.Default(c)
}
```

Give every theme-axis entry a `Default` that reads `theme.PresetComposition(c.Theme.Preset)`, falling back to the standard composition when the preset is unknown — mirroring `themeDiff`. Give every other entry a `Default` that reads the same field out of `config.Default()`.

- [ ] **Step 4: Run it and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings
```

- [ ] **Step 5: Commit**

```bash
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/settings
git add internal/settings
git commit -m "feat(settings): resolve each entry's default by the writer's rule"
```

---

### Task 5: Options that depend on another setting

**Files:**
- Modify: `internal/settings/registry.go`, `internal/shell/panelhost.go`
- Test: `internal/settings/registry_test.go`

Closes `sysc-107`. The stock-theme picker needs `appearance.seed` to offer `theme.StockNames()` when `appearance.source` is `stock`, which requires the registry to be rebuilt from the draft after each change.

**Interfaces:**
- Consumes: `settings.DefaultFor(config.Config)`.

- [ ] **Step 1: Write the failing test**

```go
func TestSeedOffersStockNamesWhenSourceIsStock(t *testing.T) {
	cfg := config.Default()
	cfg.ThemeGen.Source = "stock"
	e := settings.DefaultFor(cfg).ByPath("appearance.seed")
	if e == nil {
		t.Fatal("appearance.seed is missing")
	}
	if e.Kind != settings.KindEnum {
		t.Fatalf("Kind = %v, want an enum when the source is stock", e.Kind)
	}
	if len(e.Options) != len(theme.StockNames()) {
		t.Fatalf("Options = %d, want the %d stock names", len(e.Options), len(theme.StockNames()))
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings -run TestSeedOffersStockNamesWhenSourceIsStock
```

Expected: FAIL — `Kind = KindString`.

- [ ] **Step 3: Implement**

In `DefaultFor`, build `appearance.seed` from the supplied config: when `cfg.ThemeGen.Source == "stock"`, emit `KindEnum` with `Options: theme.StockNames()`; otherwise keep `KindString`.

In `panelhost.go`, rebuild the registry from the draft whenever a setting is applied, so the dependent options refresh under live apply:

```go
h.set = settings.DefaultFor(h.draft)
```

Add that immediately after each successful `e.Set(&h.draft, ...)`, before `h.persistDraft(r)`.

- [ ] **Step 4: Run it and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettings
```

- [ ] **Step 5: Commit and close the issue**

```bash
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/settings ./internal/shell
bd close sysc-107 --reason "appearance.seed offers theme.StockNames() when the source is stock, and the registry rebuilds from the draft so dependent options refresh under live apply."
git add internal/settings internal/shell/panelhost.go .beads/issues.jsonl
git commit -m "fix(settings): offer the stock palette names as options"
```

Check the JSONL line count against HEAD before staging it; see Global Constraints.

---

### Task 6: The grouped column replaces the virtual list

**Files:**
- Modify: `internal/shell/popout_settings.go`
- Test: `internal/shell/popout_settings_test.go`, `internal/shell/gate4b_test.go`

**Interfaces:**
- Consumes: `Entry.Describe`, `Entry.Group`, `Entry.IsDefault`.

`KindVirtualList` is strictly uniform-stride: `column.go` computes `ContentH = ItemCount * ItemHeight` and boxes every item at exactly that height. Descriptions and group headings cannot exist under it.

- [ ] **Step 1: Write the failing test**

```go
func TestSettingsRowsCarryDescriptions(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]
	reg.mu.Lock()
	defer reg.mu.Unlock()

	var caption *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.TextRole == theme.RoleCaption && n.Tone == ui.ToneSubtle && n.Text != "" {
			caption = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if caption == nil {
		t.Fatal("no row rendered a subtle caption description")
	}
	if findKind(h.root, ui.KindVirtualList) != nil {
		t.Fatal("settings still renders a virtual list")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettingsRowsCarryDescriptions
```

Expected: FAIL — no caption node, and a virtual list is still present.

- [ ] **Step 3: Replace the content tree**

In `settingsTree`, build one `ui.KindScroll` column per section holding group headings and rows. One row:

```go
func settingsEntryRow(h *PanelHost, e settings.Entry) *ui.Node {
	m := h.metrics()
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: e.Label, Name: e.Label},
	}}
	if e.Describe != "" {
		label.Children = append(label.Children, &ui.Node{
			Kind: ui.KindText, Text: e.Describe,
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{}}
	if !e.IsDefault(h.draft) {
		trailing.Children = append(trailing.Children, settingsResetButton(e))
	}
	trailing.Children = append(trailing.Children, settingsControl(h, e))
	return &ui.Node{
		Kind: ui.KindRow, Height: m.StandardControl, PinEnd: true,
		Children: []*ui.Node{label, trailing},
	}
}
```

Group heading is `TextRole: theme.RoleLabel`. Spacing uses `PanelPadding` for the section inset, `MarginXL` between groups, `MarginS` from heading to first row, and `MarginM` between rows.

- [ ] **Step 4: Run it and confirm it passes**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettingsRowsCarryDescriptions
```

- [ ] **Step 5: Retire the two tests that pin the virtual list**

Delete `TestSettingsContentIsVirtualList` from `popout_settings_test.go`. In `gate4b_test.go`'s `TestAcceptKeyboardOnlyAllControls`, replace the `s.Kind != ui.KindVirtualList` assertion and its synthetic `ItemHeight`/`ContentH` fixture with a `KindScroll` assertion and a real `PageDown` over the built tree.

- [ ] **Step 6: Run the package and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/shell
git add internal/shell
git commit -m "feat(settings): render grouped rows with descriptions"
```

---

### Task 7: Debounced apply

**Files:**
- Modify: `internal/shell/panelhost.go`, `internal/shell/popout_settings.go`
- Test: `internal/shell/popout_settings_test.go`

Today every keystroke rewrites the whole configuration file.

**Interfaces:**
- Consumes: `Registry.scheduleControl(h, func() error)`.

- [ ] **Step 1: Write the failing test**

Count writes by pointing `r.configPath` at a temp file and wrapping the write. Assert that three rapid text-field commits produce one file write, and that a toggle writes immediately.

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettingsDebounces
```

- [ ] **Step 3: Implement**

Toggles, menus, and segmented controls keep committing on change. Sliders and text fields set a pending draft and arm a timer; on expiry the write runs through `scheduleControl`, which already re-takes `Registry.mu`, drops the result if the host was replaced, and rebuilds.

- [ ] **Step 4: Run and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettings
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/shell
git add internal/shell
git commit -m "perf(settings): debounce slider and field writes"
```

---

### Task 8: The draft survives an external reload

**Files:**
- Modify: `internal/shell/registry.go`
- Test: `internal/shell/panelhost_test.go`

`PanelHost.draft` is assigned once at open and never refreshed, while `PrepareConfig.Commit` replaces `r.cfg` without touching it. An external change is therefore reverted by the next control write. Live apply widens that window to the whole session.

- [ ] **Step 1: Write the failing test**

```go
func TestReloadReseedsAnOpenSettingsDraft(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	next := config.Default()
	next.Session.Locker = "externally-set"
	prepared, err := reg.PrepareConfig(next, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if got := reg.panelHosts[PanelSettings].draft.Session.Locker; got != "externally-set" {
		t.Fatalf("draft kept the stale value %q", got)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestReloadReseedsAnOpenSettingsDraft
```

Expected: FAIL — the draft still holds the default locker.

- [ ] **Step 3: Implement**

Inside `PrepareConfig`'s `Commit`, while it already holds `r.mu` and immediately after `r.cfg = cfg`:

```go
if h := r.panelHosts[PanelSettings]; h != nil {
	h.draft = cfg
	h.set = settings.DefaultFor(cfg)
}
```

- [ ] **Step 4: Run and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestReload
gofmt -w . && test -z "$(gofmt -l .)" && go vet ./internal/shell
git add internal/shell/registry.go internal/shell/panelhost_test.go
git commit -m "fix(settings): re-seed an open draft when configuration reloads"
```

---

### Task 9: The twelve-section rail

**Files:**
- Modify: `internal/shell/popout_settings.go`, `internal/render/materialfont.go`, `internal/render/icons/material/build.py`
- Test: `internal/shell/popout_settings_test.go`, `internal/render/materialfont_test.go`

- [ ] **Step 1: Confirm each new ligature exists in the pinned source**

Before adding a name, prove the font carries it. A name the font lacks shapes to nothing and paints an invisible control.

- [ ] **Step 2: Write the failing test** asserting the rail renders twelve tab entries with icons and that `render.ValidMaterialIcon` accepts each new name.

- [ ] **Step 3: Add the glyphs and the rail**, reusing `ccRail`'s shape: a 56-wide column of 40px `ShapeMedium` buttons, `FillAccent` when selected, `Role: "tab"`.

- [ ] **Step 4: Run and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run Material
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettings
git add internal/shell internal/render
git commit -m "feat(settings): add the section rail and its glyphs"
```

---

### Task 10: Search keeps its section context

**Files:**
- Modify: `internal/shell/popout_settings.go`
- Test: `internal/shell/popout_settings_test.go`

- [ ] **Step 1: Write the failing test** asserting a query matching entries in two sections renders both section headings above their matches, and that a term appearing only in a `Describe` still matches.

- [ ] **Step 2: Run it and confirm it fails.**

- [ ] **Step 3: Implement** — extend `Registry.Search` to match `Describe` as well as `Label`, and group results under their section heading instead of the flat list.

- [ ] **Step 4: Run and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettingsSearch
git add internal/shell internal/settings
git commit -m "feat(settings): group search matches under their section"
```

---

### Task 11: The remaining control vocabulary

**Files:**
- Modify: `internal/shell/popout_settings.go`
- Test: `internal/shell/popout_settings_test.go`

Ships stepper, validated hex field, searchable picker, and path browse. **No colour picker** — `internal/ui` carries no colour type and `ui.Fill` is a closed semantic enum; a picker would give the node tree its first non-semantic colour, against the direction of `sysc-265`.

- [ ] **Step 1: Write the failing tests** — a stepper composed from buttons and a text node changes the draft by its step; an invalid hex leaves the draft untouched and sets an error; the font picker lists deduplicated families.

- [ ] **Step 2: Run them and confirm they fail.**

- [ ] **Step 3: Implement.** The font picker enumerates through `fontscan.SystemFonts(nil, render.DefaultFontCacheDir())`, dedupes on `Footprint.Family`, and sorts. Note that `Family` is stored **normalized** — `dejavusans`, not `DejaVu Sans` — so either derive a display form or present the normalized names; do not assume pretty names appear on their own. Path browse lists directories with `os.ReadDir`; no portal is involved.

- [ ] **Step 4: Run and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestSettings
git add internal/shell
git commit -m "feat(settings): add stepper, hex, picker and path controls"
```

---

### Task 12: The control-centre shortcut

**Files:**
- Modify: `internal/shell/popout_controlcenter.go`, `internal/shell/controlcenter_pages.go`
- Test: `internal/shell/controlcenter_test.go`

The standalone panel stays the complete surface. The centre gains a curated page whose category links deep-link into it.

- [ ] **Step 1: Write the failing test** asserting a `settings` rail destination exists, and that activating a category link opens `PanelSettings` at that section.

- [ ] **Step 2: Run it and confirm it fails.**

- [ ] **Step 3: Implement** — add the section to `ccSections`, and route links through the existing addressing: `panelSection` already validates a requested settings section and `HandlePanelByName` already routes it through `selectPanelSectionLocked`. Add no new addressing.

- [ ] **Step 4: Run and commit**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestControlCentre
git add internal/shell
git commit -m "feat(settings): add the control centre shortcut page"
```

---

### Task 13: Gates and the live check

**Files:** none.

- [ ] **Step 1: Format and vet**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
git diff --exit-code -- go.mod go.sum
```

- [ ] **Step 2: Run each touched package**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/settings
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
```

Never `./...`, never `-race`.

- [ ] **Step 3: Live Niri check**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Open the panel, confirm: descriptions render and the pane scrolls; a changed row shows its reset and reverts on use; a slider writes once on release rather than per pixel; an external `SIGHUP` reload is not reverted by the next change; every section is keyboard reachable; and the centre shortcut opens the panel at the right section. Kill by pid from `pgrep -f 'scratchpad/<name>'`, never `pkill -f` a name you also typed.

- [ ] **Step 4: Record the result on `sysc-320`** and close it if the gate passes.

---

## Self-review

**Spec coverage.** D1 Task 1–2. D2 Task 3, 6. D3 Task 6. D4 Task 7. D5 Task 4. D6 Task 8. D7 Task 12. D8 Task 11. D9 Task 3. D10 Task 11. D11 panel height, folded into Task 6's tree. The twelve-section IA is Task 9; search is Task 10.

**Placeholders.** Tasks 7, 9, 10, 11 and 12 carry prose steps rather than full test bodies. That is deliberate: their assertions depend on helper names established in Tasks 1–6, and writing them now would guess at signatures. An executor writes the test first in each case, which the step order enforces.

**Type consistency.** `Getter`/`Setter`/`Entry.Get`/`Set`/`Default`/`IsDefault` are used consistently from Task 1 onward. `settings.DefaultFor` is the single construction entry point in Tasks 5, 8 and the pane.

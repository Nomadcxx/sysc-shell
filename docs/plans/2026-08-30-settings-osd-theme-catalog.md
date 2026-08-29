# Settings, OSD, and Theme Catalog Implementation Plan — Milestone 4, Tranche 4B

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the Milestone 4 breadth: the settings modal with schema-driven registry and live
config persistence, the remaining control vocabulary (toggle, slider, menu, text field, scroll
area, virtual list) each with its settings consumer, the OSD with shell-owned audio/brightness
services and hotkeys, stock themes, and the enforced app-theming template catalog.

**Architecture:** Everything here runs on Tranche 4A's machinery — panels on the shield/Exclusive
surface model, lease-counted services, matugen theming, IPC socket. The one cross-repo deliverable
is new sysc-wayland protocol bindings (text-input-v3, cursor-shape-v1) for the text field.

**Tech Stack:** Go 1.26 stdlib, sysc-wayland (new bindings + release), wpctl/brightnessctl (exec),
matugen (reuse 4A generator), Go `text/template` + `go:embed` for the catalog.

**Design:** [2026-08-30-settings-osd-theme-catalog-design.md](2026-08-30-settings-osd-theme-catalog-design.md)

**Depends on:** Tranche 4A merged (panel machinery, theming core, IPC v1, MetricsService pattern).

---

### Task 1: sysc-wayland — text-input-v3 and cursor-shape-v1 bindings

**Files (in /home/nomadx/sysc-wayland):**
- Create: `protocols/text-input-unstable-v3.xml`
- Create: `protocols/cursor-shape-v1.xml`
- Modify: `sysc-wayland-scanner` generation inputs (follow the existing layer-shell generation
  pattern exactly)
- Modify: `go.mod` version / tag a release

**Step 1: Obtain the protocol XML**

Fetch the canonical XML from wayland-protocols (text-input-unstable-v3.xml,
cursor-shape-v1.xml) into `protocols/`. These are the upstream files, unmodified.

**Step 2: Generate**

Run the scanner exactly as the existing protocols were generated (inspect how
`protocols/layer-shell` is wired: generation command, output package layout, doc comments).
Generate into `textinput` and `cursorshape` packages beside `layershell`.

**Step 3: Build and smoke-test**

```bash
go build ./... && go test ./...
```

Write a compile-only test asserting the surface-facing types exist:
`textinput.ZwpTextInputV3`, `cursorshape.WpCursorShapeDeviceV1` and their request/event methods
used below (Enable, Commit, SetSurfaces; SetShape).

**Step 4: Release**

Tag a new sysc-wayland version (v0.2.0); update sysc-shell `go.mod` to it (replace directive if
the build machine consumes the local checkout — match however 4A pinned sysc-metrics).

**Step 5: Commit in sysc-wayland, then the go.mod bump in sysc-shell**

```bash
git commit -m "feat: text-input-v3 and cursor-shape-v1 bindings"
# in sysc-shell:
git commit -m "chore: bump sysc-wayland for text input bindings"
```

---

### Task 2: Toggle and slider controls

**Files:**
- Modify: `internal/ui/tree.go` (kinds)
- Modify: `internal/ui/column.go` (intrinsic sizes)
- Modify: `internal/render/paint.go` or shell paint path (drawing)
- Test: `internal/ui/controls_test.go`

Consumers: settings booleans (toggle), settings numerics (slider).

**Step 1: Failing tests**

```go
func TestToggleActivatesOnSpaceAndClick(t *testing.T) {
	n := &Node{Kind: KindToggle, Focusable: true, Name: "Reduced motion", Role: "switch"}
	// Value 0/1 = off/on; activation flips Value and returns changed
}

func TestSliderClampsAndSteps(t *testing.T) {
	n := &Node{Kind: KindSlider, Min: 24, Max: 64, Step: 1, Value: 40}
	SliderSet(n, 100) // clamps to 64
	SliderSet(n, 41.4) // snaps to 41 with Step 1
}

func TestSliderArrowKeysAdjust(t *testing.T) {
	// left/right = -/+ Step, clamped; Home/End = Min/Max
}
```

**Step 2-3: Implement** — `KindToggle`, `KindSlider` + node fields `Min, Max, Step float64`.
Toggle: track 34x20, knob 16, off = `outline` track / on = `primary` track, knob `on_primary`
when on. Slider: track 4px `surface_container` high, filled portion `primary`, knob 14px circle
`primary`. Both paint via the 4A mask helpers. Keyboard per 4A roving model: Space toggles;
arrows adjust sliders. Accessible: Role "switch" / "slider", Value exposed for both.

**Step 4-5: Pass. Commit** `feat(ui): toggle and slider controls`

---

### Task 3: Menu (dropdown) control

**Files:**
- Modify: `internal/ui/tree.go`, paint path
- Create: `internal/shell/menu.go`
- Test: `internal/shell/menu_test.go`

Consumer: settings enums. Design: in-panel dropdown — the menu is a child region of the open
panel rendered above content with the second elevation shadow (no extra Wayland surface;
layer-shell popups would need xdg-positioner work the gate does not require).

**Step 1: Failing tests**

```go
func TestMenuOpensOnActivateAndSelectsOnEnter(t *testing.T) {
	m := NewMenu([]string{"dark", "light"}, 0)
	m.Open()
	m.Next(); m.Next() // wraps to 0
	if m.Select() != 0 { t.Fatal("wrap selection wrong") }
}

func TestMenuEscapeReturnsToField(t *testing.T) {
	// Escape while open closes menu without changing value; panel stays open
}
```

**Step 2-3: Implement** — `KindMenu` node: closed shows current value + chevron; open overlays a
column of options below the field, `ElevMenu` shadow, options navigable by arrows, Enter selects,
Escape cancels. Panel host key routing checks the open menu first (menu captures arrows/Enter/
Escape while open).

**Step 4-5: Pass. Commit** `feat(shell): in-panel dropdown menu`

---

### Task 4: Text field control with text-input-v3

**Files:**
- Create: `internal/platform/wayland/textinput.go`
- Modify: `internal/platform/wayland/client.go` (bind zwp_text_input_manager_v3, per-focused-
  surface enable/disable)
- Modify: `internal/ui/tree.go` (`KindTextField`, `Preedit`, `Cursor int` fields)
- Modify: `internal/shell/panelhost.go` (focus enables text input on the panel surface)
- Test: `internal/platform/wayland/textinput_test.go`
- Test: `internal/ui/textfield_test.go`

Consumer: settings search + string entries. Single-line only (design D4).

**Step 1: Failing tests**

```go
func TestTextFieldCommitAppendsAndPreeditRenders(t *testing.T) {
	f := NewField("")
	f.Preedit("hel")          // composing, underlined, not committed
	f.Commit("lo")            // committed text appends
	if f.Text != "lo" || f.PreeditText != "" { t.Fatal(...) }
}

func TestTextFieldBackspaceAndCursor(t *testing.T) {
	// KEY_BACKSPACE 14 deletes before cursor; cursor clamps
}

func TestCursorShapeSetOnFocus(t *testing.T) {
	// fake compositor: focusing a text field sends SetShape(Ibeam) on the
	// pointer's cursor-shape device; leaving restores default
}
```

**Step 2-3: Implement**

- `textinput.go`: bind the manager when the global appears; create one `zwp_text_input_v3` per
  seat. When a panel's text field gains roving focus, `Enable` with the panel surface; on blur,
  `Disable`. Wire events: `Preedit` (store string), `Commit` (append to field at cursor),
  `DeleteSurrounding` (apply), cursor index tracking. Every change returns true through Handle so
  the owner repaints.
- Field rendering: text + preedit (preedit drawn underlined in `on_surface_variant`), 1px caret
  at cursor index in `primary`, box `surface_container` background, radius 6.
- Cursor shape: bind `wp_cursor_shape_manager_v1`; on pointer enter of a text field node,
  `SetShape(serial, Ibeam)`; on leave, `SetShape(serial, Default)`. Serial comes from
  `wayland.Event.Serial` (recorded in M2 for exactly this milestone).
- Evdev fallback: with no IME active, printable keys are NOT synthesized (layout-dependent
  decoding was rejected in 4A) — document in the settings help text: string entry works with any
  active input method; bare keyboard entry without IME is a known ceiling.
  ponytail: revisit only if real users hit it; text-input-v3 + IME is the correct path.

**Step 4-5: Pass. Commit** `feat(shell): single-line text field with text-input-v3`

---

### Task 5: Scroll area and virtual list

**Files:**
- Modify: `internal/ui/tree.go` (`KindScroll`, `KindVirtualList`, `ScrollOffset int`,
  `ItemCount int`, `ItemHeight int`)
- Modify: `internal/ui/column.go` (viewport clipping + offset layout)
- Modify: `internal/platform/wayland/pointer.go` (axis events to focused unit — verify 4A
  already routes wl_pointer axis; if not, add)
- Test: `internal/ui/scroll_test.go`

Consumer: settings content. Virtual list materializes only visible items (settings entry counts
are small, but the roadmap names the component; the implementation is one lazy-range function,
not a framework).

**Step 1: Failing tests**

```go
func TestScrollClampsOffset(t *testing.T) {
	s := &Node{Kind: KindScroll, Children: tallContent(2000)}
	LayoutColumn(s, Rect{W: 400, H: 600}, measure)
	ScrollBy(s, 5000); // clamps to contentH - viewH
	ScrollBy(s, -5000) // clamps to 0
}

func TestVirtualListVisibleRange(t *testing.T) {
	v := &Node{Kind: KindVirtualList, ItemCount: 500, ItemHeight: 40}
	LayoutColumn(v, Rect{W: 400, H: 600}, measure)
	lo, hi := VisibleRange(v) // 0..15 + overscan 2
	if lo != 0 || hi > 18 { t.Fatalf("range %d..%d", lo, hi) }
	ScrollBy(v, 4000)
	lo, hi = VisibleRange(v) // ~100..115
}

func TestWheelScrollsByDetents(t *testing.T) { ... } // axis event -> ScrollBy, 3 lines per detent
```

**Step 2-3: Implement** — layout positions children offset by `ScrollOffset`, clips paint to the
viewport rect (canvas clip exists). Virtual list: builder callback
`func(i int) *Node` supplied by the settings registry; layout instantiates only the visible
range + 2 overscan each side. Keyboard: PageUp/PageDown (evdev 104/109), Home/End scroll; arrows
move focus, scrolling to keep the focused item visible.

**Step 4-5: Pass. Commit** `feat(ui): scroll area and virtual list`

---

### Task 6: Settings schema registry

**Files:**
- Create: `internal/settings/registry.go`
- Test: `internal/settings/registry_test.go`

Design D2: each entry = config path + label + type + enum options. The registry is the single
source for content rendering, search, and persistence.

**Step 1: Failing tests**

```go
func TestRegistryCoversAllSections(t *testing.T) {
	r := Default()
	sections := []string{"Bar", "Widgets", "Appearance", "Panels", "Session", "Accessibility"}
	for _, s := range sections {
		if len(r.Section(s)) == 0 { t.Fatalf("section %s empty", s) }
	}
}

func TestEntryGetSetRoundTrip(t *testing.T) {
	r := Default()
	e := r.ByPath("bar.height")
	if e.Kind != KindInt { t.Fatal(...) }
	if err := e.Set(cfg, "48"); err != nil { t.Fatal(err) }
	if got := e.Get(cfg); got != "48" { t.Fatal(...) }
}

func TestSetRejectsInvalidValues(t *testing.T) {
	e := Default().ByPath("bar.edge")
	if err := e.Set(cfg, "diagonal"); err == nil { t.Fatal("enum must reject") }
	e2 := Default().ByPath("bar.height")
	if err := e2.Set(cfg, "not-a-number"); err == nil { t.Fatal("int must reject") }
}

func TestSearchMatchesLabels(t *testing.T) {
	hits := Default().Search("motion") // matches Accessibility reduced motion
	if len(hits) == 0 || hits[0].Path != "accessibility.reduced-motion" { t.Fatal(...) }
}
```

**Step 2-3: Implement**

```go
type Kind uint8

const (
	KindBool Kind = iota
	KindInt
	KindEnum
	KindString
)

type Entry struct {
	Path    string   // dot path into config.Config
	Label   string
	Section string
	Kind    Kind
	Options []string // KindEnum only
	Min, Max int      // KindInt only
}

type Registry struct{ entries []Entry }

func Default() *Registry // the M4 entry set, one literal per setting
func (r *Registry) Section(name string) []Entry
func (r *Registry) ByPath(path string) *Entry
func (r *Registry) Search(q string) []Entry // case-insensitive substring on Label

func (e Entry) Get(c config.Config) string
func (e Entry) Set(c *config.Config, v string) error // validates by Kind
```

Get/Set switch on Kind and address the config field directly (no reflection: a switch over paths
is longer but obvious, and the compiler catches renamed fields — reflection is the clever
version, this is the boring one). Entries for the M4 set:

- Bar: enabled, edge, height, gap, padding, spacing, radius, font-family, font-size
- Widgets: per-widget options discovered from configured items (clock format, window-title
  max-width) — generated entries, path `widgets.<id>.<option>`
- Appearance: theme source, seed, scheme, mode, high-contrast, per-template toggles (Task 14
  registers these; the registry exposes `Register(entries ...Entry)` for that)
- Panels: gap, padding, osd position
- Session: locker
- Accessibility: reduced-motion, high-contrast

**Step 4-5: Pass. Commit** `feat(settings): schema-driven settings registry`

---

### Task 7: Settings modal panel

**Files:**
- Create: `internal/shell/popout_settings.go`
- Modify: `internal/shell/panelhost.go` (PanelSettings builder + centered placement)
- Test: `internal/shell/popout_settings_test.go`

Design D1: one more panel on the 4A machinery, centered on the focused output (Align "center",
no bar anchor needed — Trigger carries output size; placement clamping still applies).

**Step 1: Failing tests**

```go
func TestSettingsSidebarSectionsAndFocus(t *testing.T) {
	h := newSettingsHost(registryDefault, cfg)
	// sidebar lists 6 sections; first focusable is the search field
}

func TestSettingsSearchSwapsSidebarForMatches(t *testing.T) {
	typeInto(h.search, "motion")
	// sidebar hidden, match list shows reduced-motion entry
}

func TestSettingsEntryRendersMatchingControl(t *testing.T) {
	// bool -> KindToggle, int -> KindSlider, enum -> KindMenu, string -> KindTextField
}

func TestSettingsKeyboardOnlyTraversal(t *testing.T) {
	// Tab from search into sidebar, arrows between sections, Tab into content,
	// Space flips a toggle — no pointer events
}
```

**Step 2-3: Implement** — tree: row → [column (search field + sidebar virtual list) | column
(scroll area of the active section's entries)]. Each entry row: label + control bound via
registry Get/Set. Search field input filters: non-empty query hides sidebar, lists
`registry.Search(q)` matches; Enter on a match opens its section and focuses the control.
Escape in search clears the query first, then closes the panel (two-stage, matches Noctalia).
Size ~900x620, fitted by Task 6's FittedSize.

**Step 4-5: Pass. Commit** `feat(shell): settings modal panel`

---

### Task 8: Atomic config persistence with live reload

**Files:**
- Modify: `internal/config/load.go` (Write)
- Modify: `internal/shell/popout_settings.go` (apply on change)
- Test: `internal/config/write_test.go`

**Step 1: Failing tests**

```go
func TestWriteAtomicRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default(); c.Bar.Height = 42
	if err := Write(p, c); err != nil { t.Fatal(err) }
	got, err := Parse(readFile(p))
	if got.Bar.Height != 42 || err != nil { t.Fatal(...) }
}

func TestWriteLeavesNoTempOnSuccess(t *testing.T) { ... }

func TestWriteNeverTruncatesOnMarshalFailure(t *testing.T) {
	// original file content intact if Write errors before rename
}
```

**Step 2-3: Implement** — `Write(path string, c Config) error`: marshal the wire form (inverse of
Parse; only fields that differ from defaults are written — mirror the merge semantics so a
hand-edited file keeps its shape), write to `path + ".tmp"` (0600), `os.Rename`. Settings apply
path: entry.Set mutates a copy of the live config → `Write` → trigger the existing reload channel
(the wayland client's `Reloads`; acquire-before-release applies it live). Invalid input never
reaches the file: Set validates first (Task 6); Write failure shows an error row in the entry.
ponytail: settings-triggered reload reuses SIGHUP's reload path verbatim — one reload mechanism,
not two.

**Step 4-5: Pass. Commit** `feat(config): atomic write and settings-driven reload`

---

### Task 9: AudioService and BrightnessService

**Files:**
- Create: `internal/services/audio.go`
- Create: `internal/services/brightness.go`
- Test: `internal/services/audio_test.go`
- Test: `internal/services/brightness_test.go`

Design D5/D6. Both lease-counted like Clock/Metrics.

**Step 1: Failing tests (audio)**

```go
func TestAudioParsesWpctlVolume(t *testing.T) {
	// "Volume: 0.42" -> 42; "Volume: 1.00 [MUTED]" -> 100, muted
}

func TestAudioChangeEventsIncludeExternal(t *testing.T) {
	fake := fakeWpctl(t, "0.40") // PATH stub whose output the test can change
	a := NewAudio(10*time.Millisecond, fake.path)
	l, _ := a.Acquire()
	fake.set("0.55") // simulate an external client changing volume
	ev := waitForChange(t, a)
	if ev.Level != 55 { t.Fatal(...) }
	l.Release()
}

func TestAudioUnavailableWithoutWpctl(t *testing.T) {
	a := NewAudio(time.Second, "/nonexistent/wpctl")
	if a.Available() { t.Fatal("must be unavailable") }
}

func TestAudioSetInvokesWpctl(t *testing.T) {
	fake := fakeWpctlRecording(t)
	a := NewAudio(time.Second, fake.path)
	a.Set(30); a.SetMute(true)
	fake.expect("set-volume", "@DEFAULT_AUDIO_SINK@", "30%")
	fake.expect("set-volume", "@DEFAULT_AUDIO_SINK@", "mute")
}
```

**Step 2-3: Implement audio** — while leased: ticker ~500 ms running
`wpctl get-volume @DEFAULT_AUDIO_SINK@`, parse level/muted; any delta vs last known → change
event on `Changes() <-chan AudioState{Level int, Muted bool}`. `Step(delta)` →
`wpctl set-volume @DEFAULT_AUDIO_SINK@ +N%` / `-N%` (clamped 0..100 by wpctl), `Set(level)`,
`SetMute(bool)` via `mute`/`unmute`. Exec failures mark the service unavailable until the next
successful poll. First poll establishes baseline without emitting an event.

**Step 4: Failing tests (brightness)**

```go
func TestBrightnessReadsSysfs(t *testing.T) {
	root := fixtureSysfs(t, map[string]int{"intel_backlight": {cur: 400, max: 1000}})
	b := NewBrightness(root, "/nonexistent/brightnessctl", time.Second)
	if !b.Available() { t.Fatal("device present must be available") }
	if got := b.Level(); got != 40 { t.Fatalf("level %d", got) }
}

func TestBrightnessZeroDevicesUnavailable(t *testing.T) {
	b := NewBrightness(emptyDir, "brightnessctl", time.Second)
	if b.Available() { t.Fatal("no devices must be unavailable") }
}

func TestBrightnessStepShellsOut(t *testing.T) {
	fake := fakeBrightnessctlRecording(t)
	b := NewBrightness(fixtureSysfs(...), fake.path, time.Second)
	b.Step(+10)
	fake.expect("set", "+10%")
}
```

**Step 5-6: Implement brightness** — reads `<root>/<device>/brightness` + `max_brightness`
directly (no exec for reads); while leased polls ~500 ms for external changes → change events;
`Step(±N)` execs `brightnessctl set ±N%`. Root defaults to `/sys/class/backlight`; tests inject
a fixture tree (this desktop has zero backlight devices — the fixture is the only way to test).

**Step 7: Pass. Commit** `feat(services): audio and brightness with change detection`

---

### Task 10: OSD surfaces

**Files:**
- Create: `internal/shell/osd.go`
- Modify: `internal/shell/registry.go` (OSD manager wiring)
- Test: `internal/shell/osd_test.go`

Design D7: one OSD surface per output-with-bar, Overlay, keyboard none, exclusive_zone −1, no
shield; position token (8-corner + center, default bottom-center); ~1.5 s visibility with timer
reset; fade+slide gated by reduced-motion; content = icon glyph + label + level bar.

**Step 1: Failing tests**

```go
func TestOsdPositionMarginsBottomCenter(t *testing.T) {
	m := osdMargins("bottom-center", ui.Rect{W: 1920, H: 1080}, ui.Rect{W: 220, H: 64}, barZone(40), 8)
	// anchor bottom+left; margin.bottom = 40+8; margin.left = (1920-220)/2
}

func TestOsdPositionAllNineTokens(t *testing.T) {
	// every token produces margins keeping the OSD fully inside the output
}

func TestOsdTimerResetsOnRepeat(t *testing.T) {
	o := newTestOsd(50 * time.Millisecond)
	o.Show(AudioState{Level: 40})
	time.Sleep(30 * time.Millisecond)
	o.Show(AudioState{Level: 45}) // resets
	if o.Closed() { t.Fatal("timer must reset on repeated change") }
}

func TestOsdShownOnEveryOutputWithBar(t *testing.T) {
	// two outputs with bars, one without: exactly two OSD surfaces requested
}

func TestOsdReducedMotionNoAnimation(t *testing.T) { ... }
```

**Step 2-3: Implement** — `OSDManager` in the Registry: `Show(kind, label, level)` → for each
output with a bar, ensure an OSD aux surface (id `osd:<global>`) via the 4A AuxRequest path with
the position margins; render glyph + label + level bar (level 0..100 → bar fill `primary`); reset
a per-manager 1.5 s timer; expiry sends close requests. Change events from Audio/Brightness
services (Task 9) and `osd.step` (Task 11) both call Show. Reveal motion reuses the 4A panel
animation helper with the reduced-motion gate.

**Step 4-5: Pass. Commit** `feat(shell): per-output OSD surfaces`

---

### Task 11: osd.step IPC verb and media hotkeys

**Files:**
- Modify: `internal/ipc/server.go` (method)
- Modify: `cmd/sysc-shell/main.go` (handler wiring)
- Modify: `docs/niri-hotkeys.md`

**Step 1: Failing test**

```go
func TestOsdStepStepsAndShows(t *testing.T) {
	// handlers with fake audio service recording Step(+5);
	// Call "osd.step" {"kind":"audio","action":"up"} -> ok, service stepped,
	// OSD manager received Show with the new level
	// action "mute" -> SetMute toggle; kind "brightness" -> brightness service
}
```

**Step 2-3: Implement** — dispatch `osd.step` params `{"kind":"audio|brightness","action":"up|down|mute"}`:
step ±5 via the service (acquire a transient lease for the step so the poll observes our own
change), then `OSDManager.Show`. Unknown kind/action → error envelope. `status` gains service
availability fields. Hotkey doc additions:

```kdl
bind {
    XF86AudioRaiseVolume allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" `{"kind":"audio","action":"up"}`; }
    XF86AudioLowerVolume allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" `{"kind":"audio","action":"down"}`; }
    XF86AudioMute        allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" `{"kind":"audio","action":"mute"}`; }
    XF86MonBrightnessUp  allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" `{"kind":"brightness","action":"up"}`; }
    XF86MonBrightnessDown allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" `{"kind":"brightness","action":"down"}`; }
}
```

**Step 4-5: Pass. Commit** `feat(ipc): osd.step verb and media hotkeys`

---

### Task 12: Stock themes

**Files:**
- Modify: `internal/theme/theme.go` (stock seeds)
- Modify: `internal/settings/registry.go` (theme source enum entries)
- Test: `internal/theme/stock_test.go`

Design D8: ~10 curated seed hexes named as color families, run through the same matugen
pipeline (source kind "stock" resolves the seed name to its hex, then `matugen color hex`).

**Step 1: Failing tests**

```go
func TestStockSeedsResolve(t *testing.T) {
	for _, name := range StockNames() {
		hex, ok := StockSeed(name)
		if !ok || len(hex) != 7 || hex[0] != '#' {
			t.Fatalf("stock %q -> %q", name, hex)
		}
	}
	if len(StockNames()) < 10 { t.Fatal("expected ~10 families") }
}

func TestStockSourceGeneratesViaMatugen(t *testing.T) {
	// fake matugen records args; Generate(Source{Kind:"stock",Seed:"Blue"},...)
	// invokes "color hex <blue-seed>"
}
```

**Step 2-3: Implement** — `StockNames()`/`StockSeed(name)` table: Blue, Purple, Green, Orange,
Red, Cyan, Pink, Amber, Coral, Monochrome (seeds: curated mid-saturation hexes, e.g. Blue
`#3d63dd`, Monochrome `#808080`). Registry: theme source enum gains "stock"; seed entry accepts
stock names when source is stock (config validation from 4A Task 2 extended: stock source
requires a known stock name).

**Step 4-5: Pass. Commit** `feat(theme): stock color-family themes`

---

### Task 13: Template catalog and apply hooks

**Files:**
- Create: `internal/theming/catalog.go`
- Create: `internal/theming/apply.go`
- Create: `internal/theming/templates/<name>.tpl` × ~16
- Create: `NOTICE` (Noctalia attribution for the catalog design)
- Modify: `internal/settings/registry.go` (per-template toggles)
- Modify: `internal/shell/registry.go` (apply on palette change)
- Test: `internal/theming/catalog_test.go`
- Test: `internal/theming/apply_test.go`

Design D9. Templates: alacritty, foot, ghostty, kitty, wezterm; niri; gtk3, gtk4, qt,
kcolorscheme; emacs, helix; btop, cava, starship, scroll.

**Step 1: Failing tests**

```go
func TestCatalogEmbedsAllTemplates(t *testing.T) {
	c := Catalog()
	if len(c.Names()) != 16 { t.Fatalf("got %d", len(c.Names())) }
	for _, n := range c.Names() {
		if c.Template(n) == "" { t.Fatalf("empty template %s", n) }
	}
}

func TestRenderNiriTemplate(t *testing.T) {
	out := Render(Catalog().Template("niri"), tok) // tok = theme.Tokens fixture
	// contains focus-ring color, border color, window shadow color from tokens
	if !strings.Contains(out, tok.Primary) { t.Fatal(...) }
}

func TestRenderHandlesMissingTokensGracefully(t *testing.T) {
	// template referencing a token we don't generate renders empty string, no error
}

func TestApplyPlainWriteNeverOverwritesForeignContent(t *testing.T) {
	// target exists with user content not ours -> apply skips + reports
}

func TestNiriIncludeInjectionIdempotent(t *testing.T) {
	cfg := writeKdl(t, "keybinds { }\n")
	applyNiri(t, cfg, out)
	applyNiri(t, cfg, out) // second apply must not duplicate
	content := read(cfg)
	if strings.Count(content, `include "sysc-shell.kdl"`) != 1 { t.Fatal(...) }
}

func TestGtkThemeNameOnlySwitchedWhenUnsetOrOurs(t *testing.T) {
	// settings.ini gtk-theme-name=Adwaita -> untouched; =sysc-shell-Dark -> updated;
	// absent -> set
}
```

**Step 2-3: Implement**

`catalog.go`:

```go
//go:embed templates/*.tpl
var tplFS embed.FS

// Catalog lists the embedded templates. Names come from filenames.
func Catalog() *CatalogT
func Render(tpl string, tok theme.Tokens) string
```

Render uses `text/template` over a data struct exposing both modes:
`{{.Dark.Primary}}`, `{{.Light.Surface}}`, plus `{{.Mode}}`. (Noctalia's
`{{colors.primary.dark.hex}}` maps to this shape; templates are written fresh for our token set —
ported structure, not ported bytes, plus the NOTICE attribution.)

`apply.go` — one hook per template kind:

```go
type Hook uint8

const (
	HookWrite Hook = iota // render -> fixed target path (alacritty, foot, ... btop, cava, starship, scroll, emacs, helix, wezterm, ghostty, qt, kcolorscheme)
	HookNiri              // render ~/.config/niri/sysc-shell.kdl + include injection
	HookGtk               // write theme dir + conditional gtk-theme-name (gtk3, gtk4)
	HookKitty             // HookWrite + SIGUSR1 to running kitty instances
)
```

- Apply runs single-flight with generation supersede (same pattern as 4A theming: one apply in
  flight; a palette change mid-apply queues exactly one rerun).
- `HookWrite`: target path per template (XDG config locations); if the target exists and does not
  contain our marker header line (`# generated by sysc-shell — do not edit` as the first line,
  comment syntax per format), skip and report "skipped: user file". Never overwrite foreign
  content.
- `HookNiri`: write `~/.config/niri/sysc-shell.kdl` (ours, always safe to rewrite — marker
  checked), then append `include "sysc-shell.kdl"` to `config.kdl` only if that exact line is
  absent (grep-before-append; never modify other lines). Niri hot-reloads; no signal needed.
- `HookGtk`: write the generated theme into `~/.themes/sysc-shell-Dark/gtk-3.0/gtk.css` (and
  gtk-4.0 equivalent); edit `gtk-theme-name` in `settings.ini` only when absent, empty, or
  already `sysc-shell-*`.
- `HookKitty`: after write, `kill -USR1` each PID whose `/proc/<pid>/comm` is `kitty` (scan /proc;
  no pkill dependency). Other apps: file write only — reload is the app's business (documented
  per-template ceiling).
- Registry wiring: after every successful palette generation (4A path) and on toggle change,
  re-apply enabled templates. Toggles persist via settings (`theme.templates.<name>` bools,
  registered into the settings registry; defaults: niri on, others off).

**Step 4-5: Pass. Commit** `feat(theming): template catalog with apply hooks`

---

### Task 14: Tranche acceptance tests

**Files:**
- Create: `tests/integration/settings_gate_test.go`
- Modify: `tests/integration/README.md` (4B live checklist)

**Step 1: Fake-compositor acceptance tests**

```go
func TestAcceptSettingsConfiguresBarLive(t *testing.T) {
	// open settings, change bar.height via slider, Enter -> config file updated,
	// reload applied, bar reconfigured to new height (fake compositor configure)
}

func TestAcceptKeyboardOnlyAllControls(t *testing.T) {
	// drive each of toggle/slider/menu/textfield/scroll/virtuallist purely
	// with key events; assert state changes
}

func TestAcceptAccessibleNamesRoles4B(t *testing.T) {
	// walk settings tree: Focusable => Name/Role set
}

func TestAcceptOsdOnEachOutputExternalChange(t *testing.T) {
	// fake wpctl changes level; OSD surfaces appear on both bar outputs
}

func TestAcceptStockThemesGenerate(t *testing.T) {
	// every stock seed through fake matugen yields a valid Tokens set
}

func TestAcceptNiriTemplateLiveApply(t *testing.T) {
	// enable niri template with fixture $HOME: sysc-shell.kdl written once,
	// include appended once; palette change rewrites colors, not the include
}
```

**Step 2: Live checklist** (append to README, execute on the live session, record in commit body):

1. Settings opens centered, Escape closes, focus returns to the prior window (4A fall-through).
2. Change bar edge/height in settings → bar updates without restart.
3. Search "motion" → reduced-motion entry; toggle it; panel reveal becomes instant.
4. `wpctl set-volume` from a terminal → OSD fires on every output with a bar.
5. Brightness path on a machine with a backlight device (or record unavailable behavior here).
6. Enable niri template → focus-ring/border colors change without restarting niri.
7. Select each stock theme → palette regenerates; fallback intact with matugen renamed away.
8. XF86 media keys via documented binds step volume with OSD.

**Step 3: Run** — `go build ./... && go test ./...` green; checklist recorded.

**Step 4: Commit**

```bash
git add tests/
git commit -m "test(shell): tranche 4B acceptance coverage and live checklist"
```

---

## Done criteria

- `go build ./...` and `go test ./...` green; sysc-wayland bindings released and pinned.
- All Task 14 acceptance tests pass; live checklist recorded.
- Settings edits persist atomically and apply live; invalid input never reaches config.json.
- No template apply ever overwrites foreign user content; niri include injection idempotent.
- `gofmt -l .` empty.

## Skipped, and when to add

- Multi-line text input → first consumer that needs it (a terminal widget, maybe never).
- Direct character entry without IME → if users without an IME complain (needs xkb keymap
  parsing; text-input-v3 + IME is the correct path until then).
- Community template catalogs → when users ask to import Noctalia/others.
- Per-app volume OSD, media OSD, dock-aware OSD offsets → with their milestones.

# System Monitor Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild both pages of `PanelMonitor` after the Noctalia community `processes` plugin: a
shared header and info card, a grouped and sortable process table with a detail view, a sectioned
System page, and a `monitor` configuration block.

**Architecture:**
- **Pure projection.** `internal/shell/processlines.go` holds every grouping, filtering and sorting
  rule, and returns flat lines for the existing `KindVirtualList`.
- **Shared frame.** `internal/shell/monitorframe.go` builds the header and info card that both pages
  use.
- **Configurable colours.** `internal/theme` names the Material colour roles. `internal/ui` resolves
  them to paint roles, and `internal/render` paints a node-level role fill and a role hover.
- **Gated fields.** Fields that need new upstream releases go through three accessors
  (`processExecutable`, `processSwap`, `processIO`) that return "unavailable" until Tasks 12 and 13
  land.

**Tech Stack:** Go 1.26, sysc-metrics v0.5.1 (Task 13 moves the pin), sysc-clipboard v0.1.0 (Task 12
moves the pin), the embedded Material Symbols subset, fontTools for the subset rebuild in Task 1.

**Spec:** `docs/plans/2026-09-25-system-monitor-panel-design.md` (D1–D10).

## Global Constraints

- **Environment:** Go only; Niri only; no CGO; no new module outside the two gated pin bumps in Tasks
  12 and 13.
- **Panel:** `panelTargetSize(PanelMonitor)` is 800 × 650 logical (D1).
- **Process rows:** row pitch 32 logical (`processRowPitch`, unchanged), measured from the reference
  screenshot at 1.53 scale.
- **System page rows:** 50 logical (`ccMonRowH`).
- **Sort keys:** `name`, `cpu`, `mem`, `swap`, `io`, `pid`, `user`. Clicking the active key reverses
  it. The default is `mem` descending, as in the reference.
- **Values:**
  - A zero or unavailable value renders `—` (em dash, U+2014).
  - Memory and swap use `%.1f G`, `%.1f M` or `%.1f K`.
  - CPU uses `%.1f%%`, trimmed to `%.0f%%` for whole numbers.
- **Colour roles:** exactly the 26 names in `theme.ColorRoleNames`. The loader rejects any other name.
- **`monitor` config defaults:** `refresh` 1, `sort_column_background` `surface_variant`,
  `sort_column_color` `on_surface_variant`, `hover_background` `surface_variant`, `hover_color`
  `on_surface_variant`, `show_apps` true, `show_processes` true.
- **Signals:** only through `signalProcessDefault`, which validates the process identity first.
  `process:int:` sends SIGINT and `process:kill:` sends SIGKILL.
- **Locking:** panel `configure`/`render`/`handle` take `Registry.mu`. `UpdateMetrics` already holds
  it. `rebuildPanel` is the unlocked form, called with the lock held.
- **Tests:** per package only, with `GOMAXPROCS=4`. Never combine `./...` with `-race`; it has
  hard-locked this machine.
- **Commits:** code commits run `gofmt -w` on the touched packages, `go vet` on those packages, and
  `git diff --exit-code -- go.mod go.sum`, except in Tasks 12 and 13.
- **Commit messages:** each message passes `bash ~/.git-hooks/commit-msg <file>`, with no attribution
  trailer. Avoid the substrings `both`, `bot`, `agent`, `cursor` and `llm`.
- **Worktree:** work in `.worktrees/system-monitor-panel` on branch `feature/system-monitor-panel`.
  Run `bd` only from `/home/nomadx/sysc-shell`, and commit with
  `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.

## Review Focus

1. **Nested applications.** An application launched from another application's terminal: Firefox
   started from Foot. Firefox's PIDs are counted under Firefox, not under Foot as well. Descent stops
   at another application's window PID. Pinned in Task 5.
2. **Search matches a group member.** Searching a string that matches one member of an executable
   group keeps the group, and its totals cover only the matching members. Pinned in Task 5.
3. **A selected process exits.** The detail view reads "Process exited", both signal buttons disable,
   and nothing is signalled. Pinned in Task 8.
4. **PIDs recycled across refreshes.** A group stays expanded when its members' PIDs change, because
   expansion is keyed by `app:`/`exe:` and never by PID. Pinned in Task 5.
5. **Config reload with a bad colour role.** `monitor.hover_color: "magenta"` is refused with a path
   error and the running config is kept. It is never painted as a default silently. Pinned in Task 3.

---

### Task 1: Five icon glyphs

The info card and detail view need glyphs the embedded subset does not carry.

**Files:**
- Modify: `internal/render/icons/material/build.py` (`ICONS` list)
- Modify: `internal/render/materialfont.go` (`materialIcons`)
- Modify: `internal/render/icons/material/material-symbols-rounded.ttf` (rebuilt)
- Test: `internal/render/materialfont_test.go`

**Interfaces:**
- Produces: `render.ValidMaterialIcon` accepts `memory`, `developer_board`, `disabled_by_default`,
  `skull` and `cancel`.

- [ ] **Step 1: Write the failing test**

Append to `internal/render/materialfont_test.go`:

```go
func TestMonitorGlyphsAreInTheSubset(t *testing.T) {
	for _, name := range []string{"memory", "developer_board", "disabled_by_default", "skull", "cancel"} {
		if !ValidMaterialIcon(name) {
			t.Errorf("%q is not in the material subset", name)
		}
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `GOMAXPROCS=4 go test -count=1 -run TestMonitorGlyphsAreInTheSubset ./internal/render`
Expected: FAIL, naming each of the five glyphs.

- [ ] **Step 3: Add the names and rebuild the subset**

Append the five names to the end of `ICONS` in `build.py`, and the same five entries to
`materialIcons` in `materialfont.go` under a comment:

```go
	// The system monitor panel's info card and process detail view.
	"memory": {}, "developer_board": {}, "disabled_by_default": {}, "skull": {}, "cancel": {},
```

Rebuild the font from the pinned upstream. The URL and SHA-256 are in `SOURCE.md`, and `build.py`
verifies the hash before it reads the file:

```bash
S=/tmp/claude-1000/msym && mkdir -p $S
curl -fsSL -o "$S/MaterialSymbolsRounded.ttf" \
  'https://raw.githubusercontent.com/google/material-design-icons/84ccef280841abfac506afc4ad4a2782f6d0a1d0/variablefont/MaterialSymbolsRounded%5BFILL%2CGRAD%2Copsz%2Cwght%5D.ttf'
python3 internal/render/icons/material/build.py "$S/MaterialSymbolsRounded.ttf"
```

If `SOURCE.md` records the output SHA-256 or size, update those lines to what `build.py` prints.

- [ ] **Step 4: Run the render package**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/render`
Expected: PASS. The existing inventory test compares `build.py` with `materialIcons` and must stay
green.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/render && GOMAXPROCS=4 go vet ./internal/render
git add internal/render/materialfont.go internal/render/materialfont_test.go internal/render/icons/material/
git commit -F msg.txt   # "feat(render): add the system monitor glyphs to the icon subset"
```

---

### Task 2: Colour role vocabulary, role fills and role hover

D7 makes the sort column and row hover configurable by Material role name. The renderer resolves
only six paint roles today, so this task extends the paint roles and teaches paint to use them.

**Files:**
- Create: `internal/theme/colorroles.go`
- Create: `internal/theme/colorroles_test.go`
- Modify: `internal/ui/tree.go` (`PaintRole` constants near line 143, `Fill` constants near line 417, `Node` fields beside `Fill` near line 340)
- Create: `internal/ui/paintrole.go`
- Create: `internal/ui/paintrole_test.go`
- Modify: `internal/render/style.go` (`Style` gains `Roles`)
- Modify: `internal/render/paint.go` (`resolvePaintRole` near line 1236, `chromeFill` near line 971, the `ui.KindRow` case near line 375)
- Modify: `internal/shell/theme.go` (`Theme.Style()` near line 492 fills `Roles`)
- Test: `internal/render/paint_role_test.go`

**Interfaces:**
- Produces:
  - `theme.ColorRoleNames() []string`, a copy of the fixed 26-name list in the order below.
  - `theme.ValidColorRole(name string) bool`.
  - `ui.PaintRoleFor(name string) (ui.PaintRole, bool)`.
  - New `ui.PaintRole` constants, the `ui.FillRole` fill, and `Node` fields
    `FillRole, InkRole, HoverFill, HoverInk ui.PaintRole`.
  - `render.Style.Roles [ui.PaintRoleCount]render.Color`.

- [ ] **Step 1: Write the failing tests**

`internal/theme/colorroles_test.go`:

```go
package theme

import "testing"

func TestColorRoleNamesAreTheMaterialSet(t *testing.T) {
	names := ColorRoleNames()
	if len(names) != 26 {
		t.Fatalf("len = %d, want 26", len(names))
	}
	for _, n := range []string{"surface_variant", "on_surface_variant", "primary_container", "outline"} {
		if !ValidColorRole(n) {
			t.Errorf("%q rejected", n)
		}
	}
	for _, n := range []string{"", "magenta", "Surface_Variant", "#ff00ff"} {
		if ValidColorRole(n) {
			t.Errorf("%q accepted", n)
		}
	}
	names[0] = "mutated"
	if ColorRoleNames()[0] == "mutated" {
		t.Fatal("ColorRoleNames returned the backing array")
	}
}
```

`internal/ui/paintrole_test.go`:

```go
package ui

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestEveryColorRoleNameMapsToAPaintRole(t *testing.T) {
	seen := map[PaintRole]string{}
	for _, name := range theme.ColorRoleNames() {
		role, ok := PaintRoleFor(name)
		if !ok || role == PaintUnset {
			t.Fatalf("%q has no paint role", name)
		}
		if prev, dup := seen[role]; dup {
			t.Fatalf("%q and %q share role %d", prev, name, role)
		}
		seen[role] = name
	}
	if _, ok := PaintRoleFor("magenta"); ok {
		t.Fatal("unknown name resolved")
	}
}
```

`internal/render/paint_role_test.go`:

```go
package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func roleStyle() Style {
	s := Style{Accent: Color{R: 1, A: 255}, Foreground: Color{G: 1, A: 255}}
	s.Roles[ui.PaintSurfaceVariant] = Color{R: 10, G: 20, B: 30, A: 255}
	s.Roles[ui.PaintOnSurfaceVariant] = Color{R: 200, G: 210, B: 220, A: 255}
	return s
}

func TestFillRolePaintsTheNamedRoles(t *testing.T) {
	n := &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillRole, FillRole: ui.PaintSurfaceVariant, InkRole: ui.PaintOnSurfaceVariant}
	fill, fg := chromeFill(roleStyle(), n, Color{})
	if fill != (Color{R: 10, G: 20, B: 30, A: 255}) || fg != (Color{R: 200, G: 210, B: 220, A: 255}) {
		t.Fatalf("fill %v fg %v", fill, fg)
	}
}

func TestHoverRoleReplacesTheStateLayerOnARow(t *testing.T) {
	n := &ui.Node{Kind: ui.KindRow, HoverFill: ui.PaintSurfaceVariant, HoverInk: ui.PaintOnSurfaceVariant, State: ui.StateHovered}
	fill, fg, layered := rowFill(roleStyle(), n)
	if layered {
		t.Fatal("a role hover also drew the state layer")
	}
	if fill != (Color{R: 10, G: 20, B: 30, A: 255}) || fg != (Color{R: 200, G: 210, B: 220, A: 255}) {
		t.Fatalf("fill %v fg %v", fill, fg)
	}
	n.State = 0
	if fill, _, _ := rowFill(roleStyle(), n); fill.A != 0 {
		t.Fatalf("resting row filled %v", fill)
	}
}

func TestExistingPaintRolesKeepTheirTokens(t *testing.T) {
	s := roleStyle()
	s.Track = Color{B: 9, A: 255}
	if got := resolvePaintRole(s, ui.PaintOnSurfaceVariant); got != s.Track {
		t.Fatalf("OnSurfaceVariant = %v, want Track %v", got, s.Track)
	}
	if got := resolvePaintRole(s, ui.PaintSurfaceVariant); got != s.Roles[ui.PaintSurfaceVariant] {
		t.Fatalf("SurfaceVariant = %v", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'ColorRole|PaintRole|FillRole|HoverRole|ExistingPaintRoles' ./internal/theme ./internal/ui ./internal/render`
Expected: build failures for `ColorRoleNames`, `PaintRoleFor`, `FillRole` and `rowFill`.

- [ ] **Step 3: Implement**

`internal/theme/colorroles.go`:

```go
package theme

// colorRoleNames is the Material colour-role vocabulary a configuration may
// name. It lives here because theme imports nothing: config validates against
// it and ui maps it to paint roles without either importing the other.
var colorRoleNames = []string{
	"primary", "on_primary", "primary_container", "on_primary_container",
	"secondary", "on_secondary", "secondary_container", "on_secondary_container",
	"tertiary", "on_tertiary", "tertiary_container", "on_tertiary_container",
	"error", "on_error", "error_container", "on_error_container",
	"surface", "on_surface", "surface_variant", "on_surface_variant",
	"surface_container_low", "surface_container", "surface_container_high", "surface_container_highest",
	"outline", "outline_variant",
}

// ColorRoleNames returns the vocabulary in a stable order.
func ColorRoleNames() []string { return append([]string(nil), colorRoleNames...) }

// ValidColorRole reports whether name is in the vocabulary. Matching is exact.
func ValidColorRole(name string) bool {
	for _, n := range colorRoleNames {
		if n == name {
			return true
		}
	}
	return false
}
```

In `internal/ui/tree.go`, extend the `PaintRole` block. The existing six keep their values, and the
count constant stays last:

```go
const (
	PaintUnset PaintRole = iota
	PaintPrimary
	PaintSecondary
	PaintTertiary
	PaintOnSurfaceVariant
	PaintOnSurface
	PaintSurface
	// The remaining Material roles, reachable by name through PaintRoleFor.
	PaintOnPrimary
	PaintPrimaryContainer
	PaintOnPrimaryContainer
	PaintOnSecondary
	PaintSecondaryContainer
	PaintOnSecondaryContainer
	PaintOnTertiary
	PaintTertiaryContainer
	PaintOnTertiaryContainer
	PaintError
	PaintOnError
	PaintErrorContainer
	PaintOnErrorContainer
	PaintSurfaceVariant
	PaintSurfaceContainerLow
	PaintSurfaceContainer
	PaintSurfaceContainerHigh
	PaintSurfaceContainerHighest
	PaintOutline
	PaintOutlineVariant

	// PaintRoleCount sizes the render role table. It must stay last.
	PaintRoleCount
)
```

Add `FillRole` as the last constant in the `Fill` block:

```go
	// FillRole paints the node's FillRole behind its content and InkRole as
	// the foreground its children inherit. It is for colours a user chose by
	// role name; built-in chrome keeps the named fills above.
	FillRole
```

Add `Node` fields beside `Fill`:

```go
	// FillRole and InkRole are read only when Fill is FillRole.
	FillRole PaintRole
	InkRole  PaintRole
	// HoverFill and HoverInk, when set, replace the translucent hover layer
	// on a hovered row with a solid role fill and foreground.
	HoverFill PaintRole
	HoverInk  PaintRole
```

`internal/ui/paintrole.go`:

```go
package ui

var paintRoleByName = map[string]PaintRole{
	"primary": PaintPrimary, "on_primary": PaintOnPrimary,
	"primary_container": PaintPrimaryContainer, "on_primary_container": PaintOnPrimaryContainer,
	"secondary": PaintSecondary, "on_secondary": PaintOnSecondary,
	"secondary_container": PaintSecondaryContainer, "on_secondary_container": PaintOnSecondaryContainer,
	"tertiary": PaintTertiary, "on_tertiary": PaintOnTertiary,
	"tertiary_container": PaintTertiaryContainer, "on_tertiary_container": PaintOnTertiaryContainer,
	"error": PaintError, "on_error": PaintOnError,
	"error_container": PaintErrorContainer, "on_error_container": PaintOnErrorContainer,
	"surface": PaintSurface, "on_surface": PaintOnSurface,
	"surface_variant": PaintSurfaceVariant, "on_surface_variant": PaintOnSurfaceVariant,
	"surface_container_low": PaintSurfaceContainerLow, "surface_container": PaintSurfaceContainer,
	"surface_container_high": PaintSurfaceContainerHigh, "surface_container_highest": PaintSurfaceContainerHighest,
	"outline": PaintOutline, "outline_variant": PaintOutlineVariant,
}

// PaintRoleFor resolves a theme.ColorRoleNames entry.
func PaintRoleFor(name string) (PaintRole, bool) {
	r, ok := paintRoleByName[name]
	return r, ok
}
```

In `internal/render/style.go`, add to `Style`:

```go
	// Roles is every Material role by paint role, for nodes that name one.
	// The named fields above stay the vocabulary built-in chrome paints from.
	Roles [ui.PaintRoleCount]Color
```

In `internal/render/paint.go`, keep the six existing cases of `resolvePaintRole` and change its
`default` so it reads the table:

```go
	default:
		if role > ui.PaintUnset && role < ui.PaintRoleCount && style.Roles[role].A > 0 {
			return style.Roles[role]
		}
		return style.Accent
```

At the top of `chromeFill`, below the selection branch, add:

```go
	if n.Fill == ui.FillRole {
		return resolvePaintRole(style, n.FillRole), resolvePaintRole(style, n.InkRole)
	}
```

Replace the body of the `ui.KindRow` case's fill lines with a helper:

```go
	case ui.KindRow:
		fill, fg, layered := rowFill(style, n)
		box := style.Scale120.PhysicalRect(n.Bounds)
		if n.Shape != ui.ShapeInherit {
			mask := RoundedMask(chromeRadius(style, nodeRadius(style, n, 0), box), box.W, box.H)
			blendMask(c, mask, box.X, box.Y, fill)
			if layered {
				blendMask(c, mask, box.X, box.Y, stateLayer(fg, n.State))
			}
		} else {
			fillRect(c, box, fill)
			if layered {
				fillRect(c, box, stateLayer(fg, n.State))
			}
		}
		inner := style
		inner.Foreground = fg
		// (existing child loop unchanged)
```

Then add:

```go
// rowFill resolves a table row's wash. A hovered row that names hover roles
// takes them as a solid fill and foreground instead of the translucent state
// layer; any other row keeps its declared fill and the layer.
func rowFill(style Style, n *ui.Node) (fill, fg Color, layered bool) {
	if n.State.Has(ui.StateHovered) && n.HoverFill != ui.PaintUnset {
		ink := style.Foreground
		if n.HoverInk != ui.PaintUnset {
			ink = resolvePaintRole(style, n.HoverInk)
		}
		return resolvePaintRole(style, n.HoverFill), ink, false
	}
	if n.Fill == ui.FillRole {
		return resolvePaintRole(style, n.FillRole), resolvePaintRole(style, n.InkRole), true
	}
	fill, fg = fillPair(style, n.Fill, Color{})
	return fill, fg, true
}
```

In `internal/shell/theme.go` `Theme.Style()`, after the struct literal is built, fill the table:

```go
	s := render.Style{ /* existing literal */ }
	s.Roles = paletteRoles(p)
	return s
```

and add:

```go
// paletteRoles lays the palette out by paint role for nodes that name a role.
func paletteRoles(p Palette) [ui.PaintRoleCount]render.Color {
	var r [ui.PaintRoleCount]render.Color
	set := map[ui.PaintRole]Color{
		ui.PaintPrimary: p.Primary, ui.PaintOnPrimary: p.OnPrimary,
		ui.PaintPrimaryContainer: p.PrimaryContainer, ui.PaintOnPrimaryContainer: p.OnPrimaryContainer,
		ui.PaintSecondary: p.Secondary, ui.PaintOnSecondary: p.OnSecondary,
		ui.PaintSecondaryContainer: p.SecondaryContainer, ui.PaintOnSecondaryContainer: p.OnSecondaryContainer,
		ui.PaintTertiary: p.Tertiary, ui.PaintOnTertiary: p.OnTertiary,
		ui.PaintTertiaryContainer: p.TertiaryContainer, ui.PaintOnTertiaryContainer: p.OnTertiaryContainer,
		ui.PaintError: p.Error, ui.PaintOnError: p.OnError,
		ui.PaintErrorContainer: p.ErrorContainer, ui.PaintOnErrorContainer: p.OnErrorContainer,
		ui.PaintSurface: p.Surface, ui.PaintOnSurface: p.OnSurface,
		ui.PaintSurfaceVariant: p.SurfaceVariant, ui.PaintOnSurfaceVariant: p.OnSurfaceVariant,
		ui.PaintSurfaceContainerLow: p.SurfaceContainerLow, ui.PaintSurfaceContainer: p.SurfaceContainer,
		ui.PaintSurfaceContainerHigh: p.SurfaceContainerHigh, ui.PaintSurfaceContainerHighest: p.SurfaceContainerHighest,
		ui.PaintOutline: p.Outline, ui.PaintOutlineVariant: p.OutlineVariant,
	}
	for role, c := range set {
		r[role] = render.Color(c)
	}
	return r
}
```

If `shell.Color` and `render.Color` are not the same type, use the conversion the existing literal
uses for `p.Primary` → `Accent`. Read the literal first and copy its form.

Add a shell test in `internal/shell/theme_test.go`:

```go
func TestThemeStyleCarriesEveryPaletteRole(t *testing.T) {
	s := testTheme().Style()
	for _, name := range theme.ColorRoleNames() {
		role, _ := ui.PaintRoleFor(name)
		if s.Roles[role].A == 0 {
			t.Errorf("role %q is transparent in the style table", name)
		}
	}
}
```

`testTheme()` stands for the constructor the other tests in `theme_test.go` already use; use that
helper's real name.

- [ ] **Step 4: Run the packages**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/theme ./internal/ui ./internal/render && GOMAXPROCS=4 go test -count=1 -run 'Theme|Style' ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/theme internal/ui internal/render internal/shell
GOMAXPROCS=4 go vet ./internal/theme ./internal/ui ./internal/render ./internal/shell
git add internal/theme internal/ui internal/render internal/shell/theme.go internal/shell/theme_test.go
git commit -F msg.txt   # "feat(render): paint named colour roles and a role hover"
```

---

### Task 3: The `monitor` configuration block and its Settings section

**Files:**
- Modify: `internal/config/config.go` (new `Monitor` type; `Config.Monitor`; `Default()`)
- Modify: `internal/config/load.go` (`wireMonitor`, `wireConfig.Monitor`, `applyMonitor`)
- Modify: `internal/config/write.go` (emit the block when it differs from the default)
- Modify: `internal/settings/registry.go` (seven entries; `"Monitor"` in `SectionNames` after `"Panels"`)
- Test: `internal/config/monitor_test.go`
- Test: `internal/settings/registry_test.go`

**Interfaces:**
- Produces: `config.Monitor{Refresh int; SortBackground, SortColor, HoverBackground, HoverColor string; ShowApps, ShowProcesses bool}` and `config.Config.Monitor`.

- [ ] **Step 1: Write the failing tests**

`internal/config/monitor_test.go`:

```go
package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMonitorDefaults(t *testing.T) {
	m := Default().Monitor
	want := Monitor{Refresh: 1, SortBackground: "surface_variant", SortColor: "on_surface_variant",
		HoverBackground: "surface_variant", HoverColor: "on_surface_variant", ShowApps: true, ShowProcesses: true}
	if m != want {
		t.Fatalf("Default().Monitor = %+v, want %+v", m, want)
	}
}

func TestMonitorBlockValidates(t *testing.T) {
	tests := []struct {
		name, json, errPart string
	}{
		{"refresh low", `{"monitor":{"refresh":0}}`, "monitor.refresh"},
		{"refresh high", `{"monitor":{"refresh":11}}`, "monitor.refresh"},
		{"unknown role", `{"monitor":{"hover_color":"magenta"}}`, "monitor.hover_color"},
		{"hex is not a role", `{"monitor":{"sort_column_background":"#ff00ff"}}`, "monitor.sort_column_background"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseForTest(t, tt.json)
			if err == nil || !strings.Contains(err.Error(), tt.errPart) {
				t.Fatalf("err = %v, want mention of %s", err, tt.errPart)
			}
		})
	}
}

func TestMonitorBlockRoundTrips(t *testing.T) {
	c := Default()
	c.Monitor.Refresh, c.Monitor.HoverColor, c.Monitor.ShowApps = 3, "on_primary_container", false
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Monitor != c.Monitor {
		t.Fatalf("round trip = %+v, want %+v", got.Monitor, c.Monitor)
	}
}

func TestDefaultMonitorWritesNoBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	if raw := readFileForTest(t, path); strings.Contains(raw, `"monitor"`) {
		t.Fatalf("default config wrote a monitor block:\n%s", raw)
	}
}
```

`parseForTest` and `readFileForTest` stand for the helpers `config_test.go` already uses to parse a
JSON string and read a written file. Use their real names; if either does not exist, add it to
`monitor_test.go`, built on `Load` over a temp file and on `os.ReadFile`.

Append to `internal/settings/registry_test.go`:

```go
func TestMonitorSectionEntries(t *testing.T) {
	r := NewRegistry()
	got := map[string]Kind{}
	for _, e := range r.Section("Monitor") {
		got[e.Path] = e.Kind
	}
	want := map[string]Kind{
		"monitor.refresh": KindInt, "monitor.sort-column-background": KindEnum,
		"monitor.sort-column-color": KindEnum, "monitor.hover-background": KindEnum,
		"monitor.hover-color": KindEnum, "monitor.show-apps": KindBool, "monitor.show-processes": KindBool,
	}
	if len(got) != len(want) {
		t.Fatalf("Monitor entries = %v", got)
	}
	for path, kind := range want {
		if got[path] != kind {
			t.Errorf("%s kind = %v, want %v", path, got[path], kind)
		}
	}
	c := config.Default()
	for _, e := range r.Section("Monitor") {
		if e.Path == "monitor.hover-color" {
			if err := e.Set(&c, "magenta"); err == nil {
				t.Fatal("hover-color accepted magenta")
			}
			if err := e.Set(&c, "primary"); err != nil || c.Monitor.HoverColor != "primary" {
				t.Fatalf("set primary: %v, %q", err, c.Monitor.HoverColor)
			}
		}
	}
}
```

Use whatever constructor `TestRegistryCoversAllSections` uses in place of `NewRegistry()`, and
mirror that test.

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'Monitor' ./internal/config ./internal/settings`
Expected: build failure on `Monitor`.

- [ ] **Step 3: Implement**

`config.go`:

```go
// Monitor is the system monitor panel. Colours are theme role names, not
// hex, so they follow the wallpaper palette; theme.ColorRoleNames is the
// vocabulary.
type Monitor struct {
	Refresh         int // seconds, 1..10
	SortBackground  string
	SortColor       string
	HoverBackground string
	HoverColor      string
	ShowApps        bool
	ShowProcesses   bool
}

func defaultMonitor() Monitor {
	return Monitor{Refresh: 1, SortBackground: "surface_variant", SortColor: "on_surface_variant",
		HoverBackground: "surface_variant", HoverColor: "on_surface_variant", ShowApps: true, ShowProcesses: true}
}
```

Add `Monitor Monitor` to `Config` after `Media`, and `Monitor: defaultMonitor(),` to the `Default()`
literal.

`load.go`:

```go
type wireMonitor struct {
	Refresh         *int    `json:"refresh,omitempty"`
	SortBackground  *string `json:"sort_column_background,omitempty"`
	SortColor       *string `json:"sort_column_color,omitempty"`
	HoverBackground *string `json:"hover_background,omitempty"`
	HoverColor      *string `json:"hover_color,omitempty"`
	ShowApps        *bool   `json:"show_apps,omitempty"`
	ShowProcesses   *bool   `json:"show_processes,omitempty"`
}
```

Add `Monitor *wireMonitor `json:"monitor,omitempty"`` to `wireConfig`. Next to the `wire.Media` block
in the loader, add:

```go
	if wire.Monitor != nil {
		monitor, err := applyMonitor(*wire.Monitor, "monitor")
		if err != nil {
			return Config{}, err
		}
		cfg.Monitor = monitor
	}
```

Match the error-return form the surrounding blocks use. Then add:

```go
func applyMonitor(w wireMonitor, path string) (Monitor, error) {
	out := defaultMonitor()
	if w.Refresh != nil {
		if *w.Refresh < 1 || *w.Refresh > 10 {
			return Monitor{}, pathErr(path+".refresh", "%d is outside 1 through 10", *w.Refresh)
		}
		out.Refresh = *w.Refresh
	}
	for _, f := range []struct {
		key string
		in  *string
		out *string
	}{
		{"sort_column_background", w.SortBackground, &out.SortBackground},
		{"sort_column_color", w.SortColor, &out.SortColor},
		{"hover_background", w.HoverBackground, &out.HoverBackground},
		{"hover_color", w.HoverColor, &out.HoverColor},
	} {
		if f.in == nil {
			continue
		}
		if !theme.ValidColorRole(*f.in) {
			return Monitor{}, pathErr(path+"."+f.key, "%q is not a theme colour role", *f.in)
		}
		*f.out = *f.in
	}
	if w.ShowApps != nil {
		out.ShowApps = *w.ShowApps
	}
	if w.ShowProcesses != nil {
		out.ShowProcesses = *w.ShowProcesses
	}
	return out, nil
}
```

In `write.go`, beside the media emission:

```go
	if c.Monitor != defaultMonitor() {
		w.Monitor = monitorWire(c.Monitor)
	}
```

and:

```go
func monitorWire(m Monitor) *wireMonitor {
	d := defaultMonitor()
	out := &wireMonitor{}
	if m.Refresh != d.Refresh {
		out.Refresh = &m.Refresh
	}
	pick := func(v, def string) *string {
		if v == def {
			return nil
		}
		return &v
	}
	out.SortBackground = pick(m.SortBackground, d.SortBackground)
	out.SortColor = pick(m.SortColor, d.SortColor)
	out.HoverBackground = pick(m.HoverBackground, d.HoverBackground)
	out.HoverColor = pick(m.HoverColor, d.HoverColor)
	if m.ShowApps != d.ShowApps {
		out.ShowApps = &m.ShowApps
	}
	if m.ShowProcesses != d.ShowProcesses {
		out.ShowProcesses = &m.ShowProcesses
	}
	return out
}
```

Beside the media check at the top of `Write`, validate before writing:

```go
	if _, err := applyMonitor(*monitorWire(c.Monitor), "monitor"); err != nil {
		return err
	}
```

`settings/registry.go`: add `"Monitor"` after `"Panels"` in `SectionNames()`, and add these entries
after the Panels entries:

```go
		{
			Path: "monitor.refresh", Label: "Refresh every", Section: "Monitor", Group: "Sampling",
			Describe: "Seconds between process and metric samples while the panel is open.",
			Kind:     KindInt, Min: 1, Max: 10,
			Get: getInt(func(c config.Config) int { return c.Monitor.Refresh }),
			Set: setInt("monitor.refresh", 1, 10, func(c *config.Config, n int) { c.Monitor.Refresh = n }),
		},
		monitorRoleEntry("monitor.sort-column-background", "Sort column background", "Fill behind the sorted column.",
			func(c *config.Config) *string { return &c.Monitor.SortBackground }),
		monitorRoleEntry("monitor.sort-column-color", "Sort column text", "Text colour in the sorted column.",
			func(c *config.Config) *string { return &c.Monitor.SortColor }),
		monitorRoleEntry("monitor.hover-background", "Hover background", "Fill behind the hovered row.",
			func(c *config.Config) *string { return &c.Monitor.HoverBackground }),
		monitorRoleEntry("monitor.hover-color", "Hover text", "Text colour in the hovered row.",
			func(c *config.Config) *string { return &c.Monitor.HoverColor }),
		{
			Path: "monitor.show-apps", Label: "Show applications", Section: "Monitor", Group: "Sections",
			Describe: "Group processes under their open application windows.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Monitor.ShowApps }),
			Set:      setBool("monitor.show-apps", func(c *config.Config, v bool) { c.Monitor.ShowApps = v }),
		},
		{
			Path: "monitor.show-processes", Label: "Show processes", Section: "Monitor", Group: "Sections",
			Describe: "List every process grouped by executable.",
			Kind:     KindBool,
			Get:      getBool(func(c config.Config) bool { return c.Monitor.ShowProcesses }),
			Set:      setBool("monitor.show-processes", func(c *config.Config, v bool) { c.Monitor.ShowProcesses = v }),
		},
```

with the helper:

```go
func monitorRoleEntry(path, label, describe string, field func(*config.Config) *string) Entry {
	roles := theme.ColorRoleNames()
	return Entry{
		Path: path, Label: label, Describe: describe, Section: "Monitor", Group: "Colours",
		Kind: KindEnum, Options: roles,
		Get: func(c config.Config) string { return *field(&c) },
		Set: setEnum(path, roles, func(c *config.Config, v string) { *field(c) = v }),
	}
}
```

If the settings rail requires one icon per section (`settingsRail` in
`internal/shell/popout_settings.go`), map `"Monitor"` to `"memory"` from Task 1.

- [ ] **Step 4: Run the packages**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/config ./internal/settings && GOMAXPROCS=4 go test -count=1 -run 'Settings' ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config internal/settings internal/shell
GOMAXPROCS=4 go vet ./internal/config ./internal/settings ./internal/shell
git add internal/config internal/settings internal/shell/popout_settings.go
git commit -F msg.txt   # "feat(config): add the monitor block and its settings section"
```

---

### Task 4: Application names and a username cache

**Files:**
- Modify: `internal/shell/runningapps.go` (`runningAppEntry.Name`, `runningAppSlot.Name`, filled in `loadRunningAppEntries` and `groupRunningApps`)
- Create: `internal/shell/usernames.go`
- Test: `internal/shell/runningapps_test.go`
- Test: `internal/shell/usernames_test.go`

**Interfaces:**
- Produces:
  - `runningAppSlot.Name string`: the desktop entry's `Name=`, or the raw `app_id` when no entry
    matches.
  - `type usernameCache`, `newUsernameCache(lookup func(uid string) (string, error)) *usernameCache`,
    `(*usernameCache).Name(uid uint32) string`.

- [ ] **Step 1: Write the failing tests**

Append to `runningapps_test.go`:

```go
func TestRunningAppSlotsCarryTheDesktopName(t *testing.T) {
	entries := []runningAppEntry{{ID: "org.mozilla.firefox", Name: "Firefox", Icon: "firefox"}}
	slots := groupRunningApps([]niri.Window{
		{ID: 1, AppID: "org.mozilla.firefox", Pid: 100},
		{ID: 2, AppID: "foot", Pid: 200},
	}, entries)
	if slots[0].Name != "Firefox" {
		t.Errorf("matched slot name = %q, want Firefox", slots[0].Name)
	}
	if slots[1].Name != "foot" {
		t.Errorf("unmatched slot name = %q, want the app id", slots[1].Name)
	}
}
```

`usernames_test.go`:

```go
package shell

import (
	"errors"
	"testing"
)

func TestUsernameCacheLooksUpOncePerUID(t *testing.T) {
	calls := map[string]int{}
	c := newUsernameCache(func(uid string) (string, error) {
		calls[uid]++
		if uid == "1000" {
			return "nomadx", nil
		}
		return "", errors.New("unknown user")
	})
	for i := 0; i < 3; i++ {
		if got := c.Name(1000); got != "nomadx" {
			t.Fatalf("Name(1000) = %q", got)
		}
		if got := c.Name(4242); got != "4242" {
			t.Fatalf("Name(4242) = %q, want the numeric id", got)
		}
	}
	if calls["1000"] != 1 || calls["4242"] != 1 {
		t.Fatalf("lookups = %v, want one per uid", calls)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'DesktopName|UsernameCache' ./internal/shell`
Expected: build failure.

- [ ] **Step 3: Implement**

In `runningapps.go`: add `Name string` to both structs. Set `Name: de.Name` in the
`loadRunningAppEntries` literal; use the parsed entry's name field, whatever `desktopentry` calls it.
In `groupRunningApps`, when a slot is created:

```go
			slot = &runningAppSlot{Key: key, Name: w.AppID}
			if ok {
				slot.Icon = entry.Icon
				slot.Actions = entry.Actions
				if entry.Name != "" {
					slot.Name = entry.Name
				}
			}
```

`usernames.go`:

```go
package shell

import (
	"os/user"
	"strconv"
	"sync"
)

// usernameCache resolves process owners for the monitor's USER column. It
// has its own lock because the tree reads it under Registry.mu, and a lookup
// can reach NSS; each uid is resolved once for the shell's lifetime.
type usernameCache struct {
	mu     sync.Mutex
	names  map[uint32]string
	lookup func(uid string) (string, error)
}

func newUsernameCache(lookup func(uid string) (string, error)) *usernameCache {
	if lookup == nil {
		lookup = func(uid string) (string, error) {
			u, err := user.LookupId(uid)
			if err != nil {
				return "", err
			}
			return u.Username, nil
		}
	}
	return &usernameCache{names: map[uint32]string{}, lookup: lookup}
}

// Name returns the user name, or the numeric id when the lookup fails.
func (c *usernameCache) Name(uid uint32) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name, ok := c.names[uid]; ok {
		return name
	}
	id := strconv.FormatUint(uint64(uid), 10)
	name, err := c.lookup(id)
	if err != nil || name == "" {
		name = id
	}
	c.names[uid] = name
	return name
}
```

Add a `usernames *usernameCache` field to `Registry`, and initialise it where the Registry's other
lazily built caches are set up. The panel reads it through `r.usernameCache()`:

```go
func (r *Registry) usernameCache() *usernameCache {
	if r.usernames == nil {
		r.usernames = newUsernameCache(nil)
	}
	return r.usernames
}
```

Call this only while holding `r.mu`.

- [ ] **Step 4: Run the package tests**

Run: `GOMAXPROCS=4 go test -count=1 -run 'RunningApp|UsernameCache' ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/runningapps.go internal/shell/runningapps_test.go internal/shell/usernames.go internal/shell/usernames_test.go internal/shell/registry.go
git commit -F msg.txt   # "feat(shell): name running apps and cache process owners"
```

---

### Task 5: The process-line projection

This task holds every rule in D4 and D5, as one pure function.

**Files:**
- Create: `internal/shell/processlines.go`
- Create: `internal/shell/processlines_test.go`

**Interfaces:**
- Consumes: `runningAppSlot.Name`, `runningAppSlot.Icon`, `runningAppSlot.Members[i].Pid` (Task 4).
- Produces:

```go
type processLineKind uint8 // lineSection, lineGroup, lineProcess
type processTotals struct {
	CPU      float64; CPUValid      bool
	Resident uint64;  ResidentValid bool
	Swap     uint64;  SwapValid     bool
	IO       float64; IOValid       bool
}
type processLine struct {
	Kind       processLineKind
	Key        string // "section:apps", "section:procs", "app:<slot key>", "exe:<group key>", "pid:<pid>:<start>"
	Depth      int
	Name, Icon string
	Expandable, Expanded bool
	Identity   services.ProcessIdentity // lineProcess only
	User       string                   // "" when members differ
	PIDText    string                   // "" on groups and sections
	Totals     processTotals
}
type processLineInput struct {
	Processes     []services.Process
	Apps          []runningAppSlot
	CurrentUID    uint32
	Query, Owner  string // Owner: "all" | "user" | "system"
	Sort          string // name cpu mem swap io pid user
	Desc          bool
	Expanded      map[string]bool // group keys the user opened
	Collapsed     map[string]bool // section keys the user closed
	ShowApps, ShowProcesses bool
	Username      func(uint32) string
	AppIcon       func(name string) string // desktop icon for an executable base name; "" for none
}
func projectProcessLines(in processLineInput) []processLine
func processExecutable(p services.Process) string          // "" until Task 13
func processSwap(p services.Process) (uint64, bool)        // (0, false) until Task 13
func processIO(p services.Process) (float64, bool)         // (0, false) until Task 13
```

- [ ] **Step 1: Write the failing tests**

`processlines_test.go`:

```go
package shell

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func proc(pid, ppid int, name string, uid uint32, rss uint64, cpu float64) services.Process {
	return services.Process{
		Identity: services.ProcessIdentity{PID: pid, StartTimeTicks: uint64(pid) * 10},
		Name:     name, ParentPID: ppid, UID: uid, UIDValid: true,
		ResidentBytes: rss, ResidentValid: true,
		CPU: services.ProcessCPU{Fraction: cpu, Valid: true},
	}
}

func lineInput(procs []services.Process, apps []runningAppSlot) processLineInput {
	return processLineInput{
		Processes: procs, Apps: apps, CurrentUID: 1000, Owner: "all", Sort: "mem", Desc: true,
		Expanded: map[string]bool{}, Collapsed: map[string]bool{},
		ShowApps: true, ShowProcesses: true,
		Username: func(uid uint32) string { return map[uint32]string{0: "root", 1000: "nomadx"}[uid] },
		AppIcon:  func(string) string { return "" },
	}
}

func lineKeys(lines []processLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Key
	}
	return out
}

func findLine(t *testing.T, lines []processLine, key string) processLine {
	t.Helper()
	for _, l := range lines {
		if l.Key == key {
			return l
		}
	}
	t.Fatalf("no line %q in %v", key, lineKeys(lines))
	return processLine{}
}

// A desktop: foot (pid 10) runs zsh (11) which launched firefox (20);
// firefox has two children. systemd (1) is root's.
func desktopFixture() ([]services.Process, []runningAppSlot) {
	procs := []services.Process{
		proc(1, 0, "systemd", 0, 10<<20, 0),
		proc(10, 1, "foot", 1000, 40<<20, .01),
		proc(11, 10, "zsh", 1000, 5<<20, 0),
		proc(20, 11, "firefox", 1000, 900<<20, .05),
		proc(21, 20, "firefox", 1000, 600<<20, .02),
		proc(22, 20, "firefox", 1000, 500<<20, .01),
	}
	apps := []runningAppSlot{
		{Key: "foot", Name: "Foot", Icon: "foot", Members: []niri.Window{{ID: 1, AppID: "foot", Pid: 10}}},
		{Key: "org.mozilla.firefox", Name: "Firefox", Icon: "firefox", Members: []niri.Window{{ID: 2, AppID: "org.mozilla.firefox", Pid: 20}}},
	}
	return procs, apps
}

func TestApplicationsSumTheirWindowTreeAndStopAtAnotherApp(t *testing.T) {
	procs, apps := desktopFixture()
	lines := projectProcessLines(lineInput(procs, apps))
	ff := findLine(t, lines, "app:org.mozilla.firefox")
	if ff.Totals.Resident != 2000<<20 {
		t.Errorf("firefox rss = %d MiB, want 2000", ff.Totals.Resident>>20)
	}
	foot := findLine(t, lines, "app:foot")
	if foot.Totals.Resident != 45<<20 {
		t.Errorf("foot rss = %d MiB, want 45 (foot + zsh, not firefox)", foot.Totals.Resident>>20)
	}
	if !ff.Expandable || ff.Expanded || ff.PIDText != "" || ff.Name != "Firefox" || ff.Icon != "firefox" {
		t.Errorf("firefox app line = %+v", ff)
	}
}

func TestSectionOrderAndGroupingByName(t *testing.T) {
	procs, apps := desktopFixture()
	got := lineKeys(projectProcessLines(lineInput(procs, apps)))
	want := []string{
		"section:apps", "app:org.mozilla.firefox", "app:foot",
		"section:procs", "exe:name:firefox", "pid:10:100", "pid:1:10", "pid:11:110",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys =\n%v\nwant\n%v", got, want)
	}
}

func TestSingleMemberGroupIsAPlainRowWithItsPID(t *testing.T) {
	procs, apps := desktopFixture()
	lines := projectProcessLines(lineInput(procs, apps))
	foot := findLine(t, lines, "pid:10:100")
	if foot.Kind != lineProcess || foot.PIDText != "10" || foot.Expandable || foot.User != "nomadx" {
		t.Fatalf("foot row = %+v", foot)
	}
}

func TestExpandingAGroupListsItsMembersSorted(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Expanded["exe:name:firefox"] = true
	got := lineKeys(projectProcessLines(in))
	i := indexOf(got, "exe:name:firefox")
	if !reflect.DeepEqual(got[i+1:i+4], []string{"pid:20:200", "pid:21:210", "pid:22:220"}) {
		t.Fatalf("members after the group = %v", got[i+1:])
	}
	if l := findLine(t, projectProcessLines(in), "pid:21:210"); l.Depth != 1 {
		t.Fatalf("member depth = %d", l.Depth)
	}
}

func TestExpansionSurvivesRecycledPIDs(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Expanded["exe:name:firefox"] = true
	for i := range in.Processes {
		if in.Processes[i].Name == "firefox" {
			in.Processes[i].Identity.PID += 1000
			in.Processes[i].Identity.StartTimeTicks += 1
		}
	}
	if !findLine(t, projectProcessLines(in), "exe:name:firefox").Expanded {
		t.Fatal("group collapsed when its PIDs changed")
	}
}

func TestOwnerFilter(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Owner = "system"
	got := lineKeys(projectProcessLines(in))
	want := []string{"section:procs", "pid:1:10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("system owner = %v, want %v", got, want)
	}
}

func TestSearchKeepsAGroupWithOnlyTheMatchingMembersCounted(t *testing.T) {
	procs, apps := desktopFixture()
	procs[5].Args = []string{"firefox", "-contentproc", "tab"}
	in := lineInput(procs, apps)
	in.Query = "CONTENTPROC"
	in.ShowApps = false
	lines := projectProcessLines(in)
	if got := lineKeys(lines); !reflect.DeepEqual(got, []string{"section:procs", "pid:22:220"}) {
		t.Fatalf("keys = %v", got)
	}
}

func TestSortKeysAndInvalidLast(t *testing.T) {
	procs := []services.Process{
		proc(3, 1, "c", 1000, 300, .3),
		proc(1, 1, "a", 1000, 100, .1),
		proc(2, 1, "b", 1000, 200, .2),
		{Identity: services.ProcessIdentity{PID: 4, StartTimeTicks: 40}, Name: "d"},
	}
	tests := []struct {
		sort string
		desc bool
		want []string
	}{
		{"mem", true, []string{"pid:3:30", "pid:2:20", "pid:1:10", "pid:4:40"}},
		{"mem", false, []string{"pid:1:10", "pid:2:20", "pid:3:30", "pid:4:40"}},
		{"cpu", true, []string{"pid:3:30", "pid:2:20", "pid:1:10", "pid:4:40"}},
		{"name", false, []string{"pid:1:10", "pid:2:20", "pid:3:30", "pid:4:40"}},
		{"pid", true, []string{"pid:4:40", "pid:3:30", "pid:2:20", "pid:1:10"}},
	}
	for _, tt := range tests {
		in := lineInput(procs, nil)
		in.Sort, in.Desc = tt.sort, tt.desc
		got := lineKeys(projectProcessLines(in))[1:] // drop section:procs
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s desc=%v = %v, want %v", tt.sort, tt.desc, got, tt.want)
		}
	}
}

func TestSectionsCollapseAndHide(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Collapsed["section:apps"] = true
	in.ShowProcesses = false
	got := lineKeys(projectProcessLines(in))
	if !reflect.DeepEqual(got, []string{"section:apps"}) {
		t.Fatalf("keys = %v", got)
	}
	if findLine(t, projectProcessLines(in), "section:apps").Expanded {
		t.Fatal("collapsed section reports expanded")
	}
}

func TestNoWindowsHidesTheApplicationsSection(t *testing.T) {
	procs, _ := desktopFixture()
	for _, k := range lineKeys(projectProcessLines(lineInput(procs, nil))) {
		if k == "section:apps" {
			t.Fatal("applications section shown with no windows")
		}
	}
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'Applications|SectionOrder|SingleMember|ExpandingAGroup|ExpansionSurvives|OwnerFilter|SearchKeeps|SortKeys|SectionsCollapse|NoWindows' ./internal/shell`
Expected: build failure on `projectProcessLines`.

- [ ] **Step 3: Implement `processlines.go`**

```go
package shell

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

type processLineKind uint8

const (
	lineSection processLineKind = iota
	lineGroup
	lineProcess
)

const (
	sectionApps  = "section:apps"
	sectionProcs = "section:procs"
)

type processTotals struct {
	CPU           float64
	CPUValid      bool
	Resident      uint64
	ResidentValid bool
	Swap          uint64
	SwapValid     bool
	IO            float64
	IOValid       bool
}

type processLine struct {
	Kind       processLineKind
	Key        string
	Depth      int
	Name, Icon string
	Expandable bool
	Expanded   bool
	Identity   services.ProcessIdentity
	User       string
	PIDText    string
	Totals     processTotals
	PID        int // sort key only; zero on groups
}

type processLineInput struct {
	Processes     []services.Process
	Apps          []runningAppSlot
	CurrentUID    uint32
	Query, Owner  string
	Sort          string
	Desc          bool
	Expanded      map[string]bool
	Collapsed     map[string]bool
	ShowApps      bool
	ShowProcesses bool
	Username      func(uint32) string
	AppIcon       func(name string) string
}

// processExecutable, processSwap and processIO read fields the pinned
// sysc-metrics release does not carry yet. Task 13 of the system monitor
// panel plan makes them read the new fields; until then every process is
// grouped by name and SWAP and DISK are unavailable.
func processExecutable(p services.Process) string   { return "" }
func processSwap(p services.Process) (uint64, bool) { return 0, false }
func processIO(p services.Process) (float64, bool)  { return 0, false }

// projectProcessLines is the whole process table as flat lines: an
// Applications section keyed by open windows and a Processes section grouped
// by executable. Filters apply to processes before grouping, so a group's
// totals cover only the members that survived them.
func projectProcessLines(in processLineInput) []processLine {
	kept := make([]services.Process, 0, len(in.Processes))
	for _, p := range in.Processes {
		if keepProcess(p, in) {
			kept = append(kept, p)
		}
	}
	var out []processLine
	if in.ShowApps && len(in.Apps) > 0 {
		out = appendSection(out, in, sectionApps, "Applications", appGroups(in, kept))
	}
	if in.ShowProcesses {
		out = appendSection(out, in, sectionProcs, "Processes", exeGroups(in, kept))
	}
	return out
}

type processGroup struct {
	line    processLine
	members []services.Process
}

func keepProcess(p services.Process, in processLineInput) bool {
	switch in.Owner {
	case "user":
		if !p.UIDValid || p.UID != in.CurrentUID {
			return false
		}
	case "system":
		if !p.UIDValid || p.UID == in.CurrentUID {
			return false
		}
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	if q == "" {
		return true
	}
	user := ""
	if p.UIDValid && in.Username != nil {
		user = in.Username(p.UID)
	}
	hay := strings.ToLower(strings.Join(append([]string{p.Name, processExecutable(p), user}, p.Args...), "\x00"))
	return strings.Contains(hay, q)
}

// appGroups assigns each kept process to at most one application: the one
// whose window PID it descends from. Descent stops at another application's
// window PID, so a browser started from a terminal is not counted twice.
func appGroups(in processLineInput, kept []services.Process) []processGroup {
	byPID := make(map[int]services.Process, len(in.Processes))
	children := map[int][]int{}
	for _, p := range in.Processes {
		byPID[p.Identity.PID] = p
		children[p.ParentPID] = append(children[p.ParentPID], p.Identity.PID)
	}
	keptPID := make(map[int]bool, len(kept))
	for _, p := range kept {
		keptPID[p.Identity.PID] = true
	}
	windowOwner := map[int]string{}
	for _, slot := range in.Apps {
		for _, w := range slot.Members {
			if w.Pid > 0 {
				windowOwner[w.Pid] = slot.Key
			}
		}
	}
	var groups []processGroup
	for _, slot := range in.Apps {
		seen := map[int]bool{}
		var members []services.Process
		var walk func(pid int)
		walk = func(pid int) {
			if seen[pid] {
				return
			}
			if owner, ok := windowOwner[pid]; ok && owner != slot.Key {
				return
			}
			seen[pid] = true
			if p, ok := byPID[pid]; ok && keptPID[pid] {
				members = append(members, p)
			}
			for _, c := range children[pid] {
				walk(c)
			}
		}
		for _, w := range slot.Members {
			if w.Pid > 0 {
				walk(w.Pid)
			}
		}
		if len(members) == 0 {
			continue
		}
		key := "app:" + slot.Key
		groups = append(groups, processGroup{
			line: processLine{Kind: lineGroup, Key: key, Name: slot.Name, Icon: slot.Icon,
				Expandable: true, Expanded: in.Expanded[key]},
			members: members,
		})
	}
	return groups
}

func exeGroups(in processLineInput, kept []services.Process) []processGroup {
	order := []string{}
	byKey := map[string]*processGroup{}
	for _, p := range kept {
		exe := processExecutable(p)
		key, name := "exe:"+exe, filepath.Base(exe)
		if exe == "" {
			key, name = "exe:name:"+p.Name, p.Name
		}
		g, ok := byKey[key]
		if !ok {
			icon := ""
			if in.AppIcon != nil {
				icon = in.AppIcon(name)
			}
			g = &processGroup{line: processLine{Kind: lineGroup, Key: key, Name: name, Icon: icon,
				Expandable: true, Expanded: in.Expanded[key]}}
			byKey[key] = g
			order = append(order, key)
		}
		g.members = append(g.members, p)
	}
	groups := make([]processGroup, 0, len(order))
	for _, key := range order {
		g := *byKey[key]
		if len(g.members) == 1 {
			// A group of one is the process itself.
			leaf := leafLine(in, g.members[0], 0)
			leaf.Icon = g.line.Icon
			g.line = leaf
		}
		groups = append(groups, g)
	}
	return groups
}

func appendSection(out []processLine, in processLineInput, key, title string, groups []processGroup) []processLine {
	if len(groups) == 0 {
		return out
	}
	open := !in.Collapsed[key]
	out = append(out, processLine{Kind: lineSection, Key: key, Name: title, Expandable: true, Expanded: open})
	if !open {
		return out
	}
	for i := range groups {
		if groups[i].line.Kind == lineGroup {
			groups[i].line.Totals, groups[i].line.User = groupTotals(in, groups[i].members)
		}
	}
	slices.SortStableFunc(groups, func(a, b processGroup) int { return compareLines(in, a.line, b.line) })
	for _, g := range groups {
		out = append(out, g.line)
		if g.line.Kind != lineGroup || !g.line.Expanded {
			continue
		}
		leaves := make([]processLine, len(g.members))
		for i, p := range g.members {
			leaves[i] = leafLine(in, p, 1)
		}
		slices.SortStableFunc(leaves, func(a, b processLine) int { return compareLines(in, a, b) })
		out = append(out, leaves...)
	}
	return out
}

func leafLine(in processLineInput, p services.Process, depth int) processLine {
	totals, user := groupTotals(in, []services.Process{p})
	return processLine{
		Kind: lineProcess, Depth: depth, Name: p.Name, Identity: p.Identity, User: user,
		Key:     "pid:" + strconv.Itoa(p.Identity.PID) + ":" + strconv.FormatUint(p.Identity.StartTimeTicks, 10),
		PIDText: strconv.Itoa(p.Identity.PID), PID: p.Identity.PID, Totals: totals,
	}
}

func groupTotals(in processLineInput, members []services.Process) (processTotals, string) {
	var t processTotals
	user, mixed := "", false
	for i, p := range members {
		if p.CPU.Valid {
			t.CPU, t.CPUValid = t.CPU+p.CPU.Fraction, true
		}
		if p.ResidentValid {
			t.Resident, t.ResidentValid = t.Resident+p.ResidentBytes, true
		}
		if s, ok := processSwap(p); ok {
			t.Swap, t.SwapValid = t.Swap+s, true
		}
		if r, ok := processIO(p); ok {
			t.IO, t.IOValid = t.IO+r, true
		}
		name := ""
		if p.UIDValid && in.Username != nil {
			name = in.Username(p.UID)
		}
		if i == 0 {
			user = name
		} else if name != user {
			mixed = true
		}
	}
	if mixed {
		user = ""
	}
	return t, user
}

// compareLines orders two lines by the sort key. A line whose value for that
// key is unavailable sorts last in either direction.
func compareLines(in processLineInput, a, b processLine) int {
	validity := func(l processLine) bool {
		switch in.Sort {
		case "cpu":
			return l.Totals.CPUValid
		case "mem":
			return l.Totals.ResidentValid
		case "swap":
			return l.Totals.SwapValid
		case "io":
			return l.Totals.IOValid
		}
		return true
	}
	if va, vb := validity(a), validity(b); va != vb {
		if va {
			return -1
		}
		return 1
	}
	var order int
	switch in.Sort {
	case "name":
		order = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	case "cpu":
		order = cmp.Compare(a.Totals.CPU, b.Totals.CPU)
	case "mem":
		order = cmp.Compare(a.Totals.Resident, b.Totals.Resident)
	case "swap":
		order = cmp.Compare(a.Totals.Swap, b.Totals.Swap)
	case "io":
		order = cmp.Compare(a.Totals.IO, b.Totals.IO)
	case "user":
		order = cmp.Compare(a.User, b.User)
	default:
		order = cmp.Compare(a.PID, b.PID)
	}
	if in.Desc {
		order = -order
	}
	if order == 0 {
		order = cmp.Compare(a.Key, b.Key)
	}
	return order
}
```

In `TestSortKeysAndInvalidLast`, the process with no values is PID 4. It sorts last for `mem` and
`cpu`, and for `pid` descending it sorts first on value. That is the expected order in the table
above. `TestSectionOrderAndGroupingByName` sorts by `mem` descending: the firefox group (2000 MiB),
then foot (40), systemd (10) and zsh (5).

- [ ] **Step 4: Run the tests**

Run: `GOMAXPROCS=4 go test -count=1 -run 'Applications|SectionOrder|SingleMember|ExpandingAGroup|ExpansionSurvives|OwnerFilter|SearchKeeps|SortKeys|SectionsCollapse|NoWindows' ./internal/shell`
Expected: PASS. If a `want` order in a test disagrees with the rules in this task, fix the test
rather than the rules, and record the change in the commit body.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/processlines.go internal/shell/processlines_test.go
git commit -F msg.txt   # "feat(shell): project processes into application and executable groups"
```

---

### Task 6: Machine facts and the shared frame

**Files:**
- Modify: `internal/shell/popout_monitor.go` (`machineFacts`, `readMachineFacts`, `formatUptime` callers)
- Create: `internal/shell/monitorframe.go`
- Create: `internal/shell/monitorframe_test.go`
- Modify: `internal/shell/panelhost.go` (`panelTargetSize(PanelMonitor)` → `{W: 800, H: 650}`; new `PanelHost` fields `monitorOptions bool`, `processExpanded map[string]bool`, `processCollapsed map[string]bool`)

**Interfaces:**
- Produces:
  - `machineFacts{Distro, Kernel, CPU, Board, Uptime, Logo, LogoLetter string}`. `GPU`, `OS` and `WM`
    are removed.
  - `parseOSReleaseField(text, key string) string`.
  - `formatUptimeLong(d time.Duration) string` → `"0 days 9 hours 25 minutes"`.
  - `monitorHeader(h *PanelHost, page string) *ui.Node`.
  - `monitorInfoCard(in monitorView, middle *ui.Node) *ui.Node`.
  - `monitorFactsColumn(f machineFacts) *ui.Node`, `monitorOptionsColumn(h *PanelHost, in monitorView) *ui.Node`.
  - The `monitorView` struct below, which `panelhost.go` builds once per rebuild.

```go
type monitorView struct {
	Snap     services.Snapshot
	History  map[services.Selector][]float64
	Facts    machineFacts
	Apps     []runningAppSlot
	Config   config.Monitor
	UID      uint32
	Iface    string
	Device   string
	Icon     func(name string, size int) *ui.Image // nil in tests
	Username func(uint32) string
	AppIcon  func(name string) string
}
```

- [ ] **Step 1: Write the failing tests**

`monitorframe_test.go`:

```go
package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestFormatUptimeLongAlwaysNamesThreeUnits(t *testing.T) {
	cases := map[time.Duration]string{
		9*time.Hour + 25*time.Minute:          "0 days 9 hours 25 minutes",
		26*time.Hour + time.Minute:            "1 day 2 hours 1 minute",
		0:                                     "0 days 0 hours 0 minutes",
	}
	for d, want := range cases {
		if got := formatUptimeLong(d); got != want {
			t.Errorf("formatUptimeLong(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestParseOSReleaseField(t *testing.T) {
	text := "NAME=\"Arch Linux\"\nPRETTY_NAME=\"Arch Linux\"\nLOGO=archlinux-logo\n"
	if got := parseOSReleaseField(text, "LOGO"); got != "archlinux-logo" {
		t.Fatalf("LOGO = %q", got)
	}
	if got := parseOSReleaseField(text, "NAME"); got != "Arch Linux" {
		t.Fatalf("NAME = %q", got)
	}
}

func testMonitorView() monitorView {
	return monitorView{
		Facts: machineFacts{Distro: "Arch Linux", Kernel: "7.2.6-arch2-1", CPU: "AMD Ryzen 5 5600X",
			Board: "Gigabyte Technology Co., Ltd. B550M DS3H AC", Uptime: "0 days 9 hours 25 minutes",
			Logo: "archlinux-logo", LogoLetter: "A"},
		Config:   config.Default().Monitor,
		Username: func(uint32) string { return "nomadx" },
		AppIcon:  func(string) string { return "" },
		Snap: services.Snapshot{
			CPU: &services.CPUSnapshot{Usage: services.CPUUsage{Fraction: .04, Valid: true}},
		},
	}
}

func TestInfoCardShowsFiveFactsAndTwoGauges(t *testing.T) {
	card := monitorInfoCard(testMonitorView(), monitorFactsColumn(testMonitorView().Facts))
	for _, text := range []string{"Arch Linux", "7.2.6-arch2-1", "AMD Ryzen 5 5600X",
		"Gigabyte Technology Co., Ltd. B550M DS3H AC", "0 days 9 hours 25 minutes"} {
		if !treeHasNameOrText(card, text) {
			t.Errorf("info card missing %q", text)
		}
	}
	gauges := 0
	walkNodes(card, func(n *ui.Node) {
		if n.Kind == ui.KindRadialGauge {
			gauges++
		}
	})
	if gauges != 2 {
		t.Fatalf("gauges = %d, want 2", gauges)
	}
	if !treeHasNameOrText(card, "A") {
		t.Fatal("no logo letter tile without an icon image")
	}
}

func TestHeaderCarriesPagePillsAndProcessControlsOnlyOnProcesses(t *testing.T) {
	h := &PanelHost{search: ui.NewField(""), monitorPage: monitorPageProcesses}
	head := monitorHeader(h, monitorPageProcesses)
	for _, name := range []string{"Processes", "System", "Search", "Clear search", "View options", "Monitor settings", "Close"} {
		if findByName(head, name) == nil && !treeHasNameOrText(head, name) {
			t.Errorf("processes header missing %q", name)
		}
	}
	sys := monitorHeader(h, monitorPageMetrics)
	for _, name := range []string{"Search", "Clear search", "View options"} {
		if findByName(sys, name) != nil {
			t.Errorf("system header has %q", name)
		}
	}
}
```

If `walkNodes` does not exist among the shell test helpers, add it to `monitorframe_test.go`:

```go
func walkNodes(n *ui.Node, visit func(*ui.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for _, c := range n.Children {
		walkNodes(c, visit)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'FormatUptimeLong|ParseOSReleaseField|InfoCard|HeaderCarries' ./internal/shell`
Expected: build failure.

- [ ] **Step 3: Implement**

**Facts.** Replace `machineFacts` and `readMachineFacts` in `popout_monitor.go`:

```go
// machineFacts is the info card: the five identity rows the reference draws
// beside the distro logo. Logo is an icon-theme name from os-release; the
// letter tile stands in when it is empty or unresolvable.
type machineFacts struct {
	Distro, Kernel, CPU, Board, Uptime string
	Logo, LogoLetter                   string
}

func readMachineFacts() machineFacts {
	cpu, _ := os.ReadFile("/proc/cpuinfo")
	osrel, _ := os.ReadFile("/etc/os-release")
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	vendor, _ := os.ReadFile("/sys/class/dmi/id/board_vendor")
	board, _ := os.ReadFile("/sys/class/dmi/id/board_name")
	uptime := ""
	if d, ok := services.ReadUptime(); ok {
		uptime = formatUptimeLong(d)
	}
	name := parseOSReleaseField(string(osrel), "NAME")
	letter := ""
	for _, r := range name {
		letter = strings.ToUpper(string(r))
		break
	}
	return machineFacts{
		Distro:     parseOSRelease(string(osrel)),
		Kernel:     strings.TrimSpace(string(release)),
		CPU:        parseCPUModel(string(cpu)),
		Board:      strings.TrimSpace(strings.TrimSpace(string(vendor)) + " " + strings.TrimSpace(string(board))),
		Uptime:     uptime,
		Logo:       parseOSReleaseField(string(osrel), "LOGO"),
		LogoLetter: letter,
	}
}

func parseOSReleaseField(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

func formatUptimeLong(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int64(d / (24 * time.Hour))
	hours := int64(d % (24 * time.Hour) / time.Hour)
	mins := int64(d % time.Hour / time.Minute)
	unit := func(n int64, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.FormatInt(n, 10) + " " + many
	}
	return unit(days, "day", "days") + " " + unit(hours, "hour", "hours") + " " + unit(mins, "minute", "minutes")
}
```

Delete `factsWithGPU`, `kernelLabel`, `compositorLabel` and `formatUptime`. Delete their tests
(`TestFormatUptime`, and any test the compiler names that uses the removed fields). `countUnit`
stays only if another caller still uses it; otherwise delete it.

**Frame.** `monitorframe.go`:

```go
package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The reference's proportions at 1.53 scale, in logical pixels.
const (
	monitorHeaderH   = 36
	monitorInfoH     = 142
	monitorLogoSize  = 110
	monitorGaugeSize = 118
	monitorFactIcon  = 18
)

type monitorView struct {
	Snap     services.Snapshot
	History  map[services.Selector][]float64
	Facts    machineFacts
	Apps     []runningAppSlot
	Config   config.Monitor
	UID      uint32
	Iface    string
	Device   string
	Icon     func(name string, size int) *ui.Image
	Username func(uint32) string
	AppIcon  func(name string) string
}

// monitorHeader is the one header row both pages share: title, page pills,
// the Processes page's search and options, then settings and close.
func monitorHeader(h *PanelHost, page string) *ui.Node {
	title := "Processes"
	if page == monitorPageMetrics {
		title = "System"
	}
	pill := func(id, label, icon string) *ui.Node {
		n := &ui.Node{Kind: ui.KindButton, Action: "monitor:page:" + id, Name: label, Role: "tab",
			Focusable: true, Height: monitorHeaderH, Fill: ui.FillContainerHighest, Shape: ui.ShapeSmall,
			Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: icon, IconSize: monitorFactIcon},
				{Kind: ui.KindText, Text: label},
			}}}}
		if page == id {
			n.State |= ui.StateSelected
		}
		return n
	}
	left := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "desktop_windows", IconSize: centreIconSize},
			{Kind: ui.KindText, Text: title, TextRole: theme.RoleHeadline, Bold: true},
		}},
		{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
			pill(monitorPageProcesses, "Processes", "apps"),
			pill(monitorPageMetrics, "System", "memory"),
		}},
	}}
	if page == monitorPageProcesses {
		if h.search == nil {
			h.search = ui.NewField("")
		}
		field := h.search.Node("Search")
		field.Width, field.Height, field.Placeholder = 300, monitorHeaderH, "type to search"
		left.Children = append(left.Children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "search", IconSize: centreIconSize},
			field,
			{Kind: ui.KindButton, Action: "monitor:clear", Name: "Clear search", Role: "button", Focusable: true,
				Width: monitorHeaderH, Height: monitorHeaderH, Fill: ui.FillContainerHighest, Shape: ui.ShapeSmall,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: monitorFactIcon}}},
		}})
	}
	right := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true}
	if page == monitorPageProcesses {
		options := centreIconButton("tune", "monitor:options", "View options")
		if h.monitorOptions {
			options.State |= ui.StateSelected
		}
		right.Children = append(right.Children, options)
	}
	right.Children = append(right.Children,
		centreIconButton("settings", "monitor:settings", "Monitor settings"),
		centreIconButton("close", "monitor:close", "Close"))
	return &ui.Node{Kind: ui.KindRow, Height: monitorHeaderH, PinEnd: true, CenterY: true, Children: []*ui.Node{left, right}}
}

// monitorInfoCard is the logo, a middle column the caller chooses (facts,
// view options, or the process detail replaces the whole card), and the CPU
// and memory rings.
func monitorInfoCard(in monitorView, middle *ui.Node) *ui.Node {
	var logo *ui.Node
	if in.Icon != nil && in.Facts.Logo != "" {
		if img := in.Icon(in.Facts.Logo, monitorLogoSize); img != nil {
			logo = &ui.Node{Kind: ui.KindImage, Image: img, Width: monitorLogoSize, Height: monitorLogoSize}
		}
	}
	if logo == nil {
		logo = &ui.Node{Kind: ui.KindCapsule, Width: monitorLogoSize, Height: monitorLogoSize,
			Fill: ui.FillContainer, Shape: ui.ShapeLarge, Children: []*ui.Node{
				{Kind: ui.KindText, Text: in.Facts.LogoLetter, TextRole: theme.RoleDisplay, CenterX: true, CenterY: true},
			}}
	}
	return &ui.Node{Kind: ui.KindCapsule, Height: monitorInfoH, Padding: theme.MarginL,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, PinEnd: true, Children: []*ui.Node{
				{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: []*ui.Node{logo, middle}},
				{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: monitorGauges(in.Snap)},
			}},
		}}
}

func monitorFactsColumn(f machineFacts) *ui.Node {
	row := func(icon, text string) *ui.Node {
		if text == "" {
			text = ccDash
		}
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon, IconSize: monitorFactIcon, Tone: ui.ToneSubtle},
			{Kind: ui.KindText, Text: text, MaxWidth: 360},
		}}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		row("desktop_windows", f.Distro),
		row("content_copy", f.Kernel),
		row("memory", f.CPU),
		row("developer_board", f.Board),
		row("schedule", f.Uptime),
	}}
}

// monitorOptionsColumn replaces the facts while the options button is on,
// as the reference's view options do: two section toggles and the owner
// filter the header pills gave up their place to.
func monitorOptionsColumn(h *PanelHost, in monitorView) *ui.Node {
	toggle := func(action, label string, on bool) *ui.Node {
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, PinEnd: true, Width: 320, Children: []*ui.Node{
			{Kind: ui.KindText, Text: label, Tone: ui.ToneSubtle},
			{Kind: ui.KindToggle, Action: action, Name: label, Role: "switch", Focusable: true, Value: boolValue(on)},
		}}
	}
	owners := &ui.Node{Kind: ui.KindSegmented, Key: "process-owner", Height: 28}
	for _, o := range []struct{ id, label string }{{"all", "All"}, {"user", "User"}, {"system", "System"}} {
		n := &ui.Node{Kind: ui.KindButton, Action: "monitor:owner:" + o.id, Name: o.label, Role: "tab",
			Focusable: true, Height: 28, Children: []*ui.Node{{Kind: ui.KindText, Text: o.label}}}
		if h.processFilter == o.id {
			n.State |= ui.StateSelected
		}
		owners.Children = append(owners.Children, n)
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
		toggle("monitor:show:apps", "Show applications", in.Config.ShowApps),
		toggle("monitor:show:procs", "Show processes", in.Config.ShowProcesses),
		owners,
	}}
}

func boolValue(on bool) float64 {
	if on {
		return 1
	}
	return 0
}

// monitorGauges are the reference's two rings: CPU usage over temperature,
// memory used over memory available.
func monitorGauges(snap services.Snapshot) []*ui.Node {
	gauge := func(name, label, value, under string, fraction float64, ok bool) *ui.Node {
		return &ui.Node{Kind: ui.KindColumn, Width: monitorGaugeSize, Gap: theme.MarginXXS, Name: name, Role: "group", Children: []*ui.Node{
			{Kind: ui.KindRadialGauge, Width: monitorGaugeSize, Height: monitorGaugeSize, Value: fraction,
				ValueText: value, Absent: !ok, Name: name, Role: "img", Tooltip: label + ": " + value},
			{Kind: ui.KindText, Text: label, Tone: ui.ToneAccent, CenterX: true},
			{Kind: ui.KindText, Text: under, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, CenterX: true, Tabular: true},
		}}
	}
	cpuValue, cpuUnder, cpuFrac, cpuOK := ccDash, "", 0.0, false
	if snap.CPU != nil && snap.CPU.Usage.Valid {
		cpuFrac, cpuOK = snap.CPU.Usage.Fraction, true
		cpuValue = fmt.Sprintf("%.0f%%", cpuFrac*100)
	}
	if snap.Thermal != nil && snap.Thermal.Valid {
		cpuUnder = fmt.Sprintf("%.0f°", snap.Thermal.Celsius)
	}
	memValue, memUnder, memFrac, memOK := ccDash, "", 0.0, false
	if snap.Memory != nil && snap.Memory.Memory.TotalBytes > 0 {
		c := snap.Memory.Memory
		memFrac, memOK = float64(c.UsedBytes)/float64(c.TotalBytes), true
		memValue = formatProcessBytes(c.UsedBytes)
		memUnder = "+" + formatProcessBytes(c.AvailableBytes)
	}
	return []*ui.Node{
		gauge("CPU gauge", "CPU", cpuValue, cpuUnder, cpuFrac, cpuOK),
		gauge("Memory gauge", "Memory", memValue, memUnder, memFrac, memOK),
	}
}

// formatProcessBytes is the reference's compact size: "2.4G", "651.1M".
func formatProcessBytes(b uint64) string {
	const k = 1024
	switch {
	case b >= k*k*k:
		return fmt.Sprintf("%.1fG", float64(b)/(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1fM", float64(b)/(k*k))
	case b >= k:
		return fmt.Sprintf("%.1fK", float64(b)/k)
	}
	return fmt.Sprintf("%dB", b)
}
```

In the table, `formatProcessBytes` output gets a space before the unit (`2.4 G`) through
`formatProcessCell` in Task 7. The gauges keep the reference's unspaced form.

`theme.RoleDisplay`, `theme.RoleHeadline`, `ui.KindToggle`'s `Value` convention, `centreIconSize`
and `ccDash` are existing names. Check each against `internal/theme` and the existing toggle
builders before use. If `RoleDisplay` does not exist, use the largest role the theme defines.

- [ ] **Step 4: Run the tests**

Run: `GOMAXPROCS=4 go test -count=1 -run 'FormatUptimeLong|ParseOSReleaseField|InfoCard|HeaderCarries|ParseCPUModel|ParseOSRelease' ./internal/shell`
Expected: PASS. The package may still fail to build at `monitorPanelTree` callers because of the
removed fact fields. If so, fix `monitorSystemCard` by deleting it; Task 10 replaces its page.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/monitorframe.go internal/shell/monitorframe_test.go internal/shell/popout_monitor.go internal/shell/popout_monitor_test.go internal/shell/panelhost.go
git commit -F msg.txt   # "feat(shell): shared monitor header and info card"
```

---

### Task 7: The Processes page

**Files:**
- Modify: `internal/shell/popout_process.go` (rewrite `monitorPanelTree`, `processMonitorTree`, `processHeader`, `processRow`, `processColumns`, `activateMonitor`; delete `monitorPageSwitcher`, `processFilterSwitcher`, `projectProcesses`, the `Kill` pill)
- Modify: `internal/shell/panelhost.go` (the `PanelMonitor` case near line 2230 builds `monitorView`; the reset near line 689 sets `processSort, processDesc = "mem", true`)
- Test: `internal/shell/popout_process_test.go` (rewrite)

**Interfaces:**
- Consumes: `projectProcessLines` (Task 5); `monitorHeader`, `monitorInfoCard`, `monitorFactsColumn`,
  `monitorOptionsColumn`, `monitorView` and `formatProcessBytes` (Task 6); `config.Monitor` (Task 3);
  `ui.FillRole` and `Node.HoverFill`/`HoverInk` (Task 2).
- Produces:
  - `monitorPanelTree(h *PanelHost, in monitorView) *ui.Node`.
  - `processTableTree(h *PanelHost, in monitorView) *ui.Node`.
  - `formatProcessCell(kind string, t processTotals) string`.
  - Actions: `monitor:page:<id>`, `monitor:sort:<key>`, `monitor:toggle:<line key>`,
    `monitor:options`, `monitor:owner:<id>`, `monitor:show:apps|procs`, `monitor:clear`,
    `monitor:settings`, `monitor:close`, and `monitor:select:<pid>:<start>` (unchanged form).

- [ ] **Step 1: Write the failing tests**

Replace `popout_process_test.go`. Keep `processFixture`, `TestHiddenProcessScrollbarKeepsWheelAndKeyboardPaging`,
`TestProcessRowPointerActionUsesLaidOutControl`, `TestScrolledOutProcessButtonCannotActivateAtItsOldPosition`,
`TestKeyboardFocusRevealsAProcessAction` and `TestProcessSignalRunsUnlockedAndReportsIdentityFailure`.
Adapt each one's node lookups to the new row actions (`monitor:select:`). Delete
`TestProjectProcessesFiltersSearchesAndSortsStably`, `TestProcessTableUsesCompactChromeAndOneKillAction`
and `TestProcessIdentityActionsAcceptSelectionAndTermOnly`; Tasks 5 and 8 replace them. Add:

```go
func processView(procs []services.Process) monitorView {
	v := testMonitorView()
	v.Snap.Processes = &services.ProcessSnapshot{Processes: procs}
	v.UID = 1000
	return v
}

func processHost() *PanelHost {
	return &PanelHost{
		place: Placement{Panel: panelTargetSize(PanelMonitor)}, theme: Theme{Metrics: standardMetrics()},
		monitorPage: monitorPageProcesses, processFilter: "all", processSort: "mem", processDesc: true,
		search: ui.NewField(""), processExpanded: map[string]bool{}, processCollapsed: map[string]bool{},
	}
}

func TestProcessPageMatchesTheReferenceLayout(t *testing.T) {
	root := monitorPanelTree(processHost(), processView(processFixture()))
	for _, label := range []string{"Processes", "System", "Search", "Name", "CPU", "MEM", "SWAP", "DISK", "PID", "USER", "Processes"} {
		if !treeHasNameOrText(root, label) {
			t.Errorf("processes page missing %q", label)
		}
	}
	if findKind(root, ui.KindVirtualList) == nil {
		t.Fatal("no virtual list")
	}
	if treeHasNameOrText(root, "Kill") {
		t.Fatal("per-row Kill pill is still drawn")
	}
	if panelTargetSize(PanelMonitor) != (ui.Rect{W: 800, H: 650}) {
		t.Fatalf("panel size = %+v", panelTargetSize(PanelMonitor))
	}
}

func TestSortedColumnUsesTheConfiguredRoles(t *testing.T) {
	h := processHost()
	v := processView(processFixture())
	v.Config.SortBackground, v.Config.SortColor = "primary_container", "on_primary_container"
	root := monitorPanelTree(h, v)
	head := findByName(root, "Sort by MEM")
	if head == nil || head.Fill != ui.FillRole || head.FillRole != ui.PaintPrimaryContainer || head.InkRole != ui.PaintOnPrimaryContainer {
		t.Fatalf("MEM header = %+v", head)
	}
	if !treeHasNameOrText(head, "▼ MEM") {
		t.Fatal("descending MEM header lacks ▼")
	}
	list := findKind(root, ui.KindVirtualList)
	row := list.Item(1) // 0 is the Processes section header
	cell := findByName(row, "MEM value")
	if cell == nil || cell.Fill != ui.FillRole || cell.FillRole != ui.PaintPrimaryContainer {
		t.Fatalf("MEM cell = %+v", cell)
	}
}

func TestRowsHoverWithTheConfiguredRoles(t *testing.T) {
	v := processView(processFixture())
	v.Config.HoverBackground, v.Config.HoverColor = "secondary_container", "on_secondary_container"
	list := findKind(monitorPanelTree(processHost(), v), ui.KindVirtualList)
	row := list.Item(1)
	if row.HoverFill != ui.PaintSecondaryContainer || row.HoverInk != ui.PaintOnSecondaryContainer || row.Shape == ui.ShapeInherit {
		t.Fatalf("row hover = fill %v ink %v shape %v", row.HoverFill, row.HoverInk, row.Shape)
	}
}

func TestGroupRowHasNoPIDAndTogglesByKey(t *testing.T) {
	procs := append(processFixture(),
		services.Process{Identity: services.ProcessIdentity{PID: 31, StartTimeTicks: 310}, Name: "gamma", UID: 1000, UIDValid: true, ResidentBytes: 1, ResidentValid: true})
	root := monitorPanelTree(processHost(), processView(procs))
	group := findAction(root, "monitor:toggle:exe:name:gamma")
	if group == nil {
		t.Fatal("gamma group has no toggle action")
	}
	if treeHasNameOrText(group, "30") || treeHasNameOrText(group, "31") {
		t.Fatal("group row shows a PID")
	}
}

func TestFormatProcessCell(t *testing.T) {
	cases := []struct {
		kind string
		t    processTotals
		want string
	}{
		{"cpu", processTotals{CPU: .002, CPUValid: true}, "0.2%"},
		{"cpu", processTotals{CPU: .01, CPUValid: true}, "1%"},
		{"cpu", processTotals{CPU: 0, CPUValid: true}, "—"},
		{"cpu", processTotals{}, "—"},
		{"mem", processTotals{Resident: 2576980377, ResidentValid: true}, "2.4 G"},
		{"mem", processTotals{Resident: 682699161, ResidentValid: true}, "651.1 M"},
		{"swap", processTotals{}, "—"},
		{"io", processTotals{IO: 1536, IOValid: true}, "1.5 K/s"},
	}
	for _, c := range cases {
		if got := formatProcessCell(c.kind, c.t); got != c.want {
			t.Errorf("%s %+v = %q, want %q", c.kind, c.t, got, c.want)
		}
	}
}
```

The `Item(1)` lookups assume `processFixture` has no windows, so the only section is Processes.
Every fixture name is distinct, so each process is a single-member row.

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'ProcessPage|SortedColumn|RowsHover|GroupRowHasNoPID|FormatProcessCell' ./internal/shell`
Expected: build failure on the new `monitorPanelTree` signature.

- [ ] **Step 3: Implement**

In `panelhost.go`, the `PanelMonitor` render case becomes:

```go
	case PanelMonitor:
		return monitorPanelTree(h, r.monitorViewLocked(h))
```

and add, in `popout_process.go`:

```go
func (r *Registry) monitorViewLocked(h *PanelHost) monitorView {
	users := r.usernameCache()
	entries := r.runningIndex
	return monitorView{
		Snap: r.sample, History: r.historyLocked(), Facts: r.machineFacts, Apps: r.running,
		Config: r.cfg.Monitor, UID: uint32(os.Getuid()), Iface: h.ccIface, Device: h.ccDevice,
		Icon: func(name string, size int) *ui.Image { return monitorLookupIcon(r, h, name, size) },
		Username: users.Name,
		AppIcon: func(name string) string {
			if e, ok := lookupRunningApp(name, entries); ok {
				return e.Icon
			}
			return ""
		},
	}
}

// monitorLookupIcon is launcherLookupIcon at an arbitrary logical size.
func monitorLookupIcon(r *Registry, h *PanelHost, name string, logical int) *ui.Image {
	if r == nil || r.trayIcons == nil || name == "" {
		return nil
	}
	size := logical
	if scale := ui.Scale120(h.scale120); scale.Valid() {
		size = max(scale.Physical(logical), 1)
	}
	key := icons.Square(name, size)
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}
```

In `applyTrayIcon` (`tray.go`), where it rebuilds the launcher after an icon lands, rebuild an open
`PanelMonitor` in the same way. Copy the launcher block with `PanelMonitor` substituted.

Rewrite the page in `popout_process.go`:

```go
const (
	processRowPitch     = 32
	processRowHeight    = 30
	processHeaderHeight = 32
	processTablePadding = 6
	processIconSize     = 20
	processIndent       = 20
	processFooterHeight = 20
)

// processColumn is one table column after Name. Widths are the reference's
// proportions at 800 logical; Name takes what is left.
type processColumn struct{ key, label string; width int }

var processColumnsAfterName = []processColumn{
	{"cpu", "CPU", 72}, {"mem", "MEM", 100}, {"swap", "SWAP", 84},
	{"io", "DISK", 96}, {"pid", "PID", 72}, {"user", "USER", 88},
}

func processNameWidth(h *PanelHost) int {
	w := h.place.Panel.W - 2*h.metrics().PanelPadding - 2*processTablePadding
	for _, c := range processColumnsAfterName {
		w -= c.width + theme.MarginM
	}
	return max(w, 120)
}

func monitorPanelTree(h *PanelHost, in monitorView) *ui.Node {
	if h.monitorPage == "" {
		h.monitorPage = monitorPageProcesses
	}
	if h.monitorPage == monitorPageMetrics {
		return systemPageTree(h, in) // Task 10; until then return the previous monitorTree body here
	}
	return processTableTree(h, in)
}

func processTableTree(h *PanelHost, in monitorView) *ui.Node {
	if h.processFilter == "" {
		h.processFilter = "all"
	}
	if h.processSort == "" {
		h.processSort, h.processDesc = "mem", true
	}
	if h.processExpanded == nil {
		h.processExpanded = map[string]bool{}
	}
	if h.processCollapsed == nil {
		h.processCollapsed = map[string]bool{}
	}
	var snapshot services.ProcessSnapshot
	if in.Snap.Processes != nil {
		snapshot = *in.Snap.Processes
	}
	lines := projectProcessLines(processLineInput{
		Processes: snapshot.Processes, Apps: in.Apps, CurrentUID: in.UID,
		Query: h.query, Owner: h.processFilter, Sort: h.processSort, Desc: h.processDesc,
		Expanded: h.processExpanded, Collapsed: h.processCollapsed,
		ShowApps: in.Config.ShowApps, ShowProcesses: in.Config.ShowProcesses,
		Username: in.Username, AppIcon: in.AppIcon,
	})

	middle := monitorFactsColumn(in.Facts)
	if h.monitorOptions {
		middle = monitorOptionsColumn(h, in)
	}
	info := monitorInfoCard(in, middle)
	if detail := processDetailCard(h, in, snapshot); detail != nil { // Task 8; until then omit this if
		info = detail
	}

	pad := h.metrics().PanelPadding
	used := 2*pad + monitorHeaderH + monitorInfoH + processHeaderHeight + processFooterHeight + 4*theme.MarginM
	tableH := max(h.place.Panel.H-used, processRowPitch+2*processTablePadding)
	rows := make([]*ui.Node, len(lines))
	list := &ui.Node{
		Kind: ui.KindVirtualList, Height: tableH - 2*processTablePadding,
		ItemCount: len(lines), ItemHeight: processRowPitch, HideScrollbar: true,
		Item: func(i int) *ui.Node {
			if i < 0 || i >= len(lines) {
				return nil
			}
			if rows[i] == nil {
				rows[i] = processLineRow(h, in, lines[i])
			}
			return rows[i]
		},
	}
	table := &ui.Node{Kind: ui.KindCapsule, Padding: processTablePadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
			processTableHeader(h, in),
			{Kind: ui.KindCapsule, Height: tableH, Fill: ui.FillContainerHighest, Shape: ui.ShapeCard,
				Padding: processTablePadding, Children: []*ui.Node{list}},
		}}}}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: pad, Children: []*ui.Node{
		monitorHeader(h, monitorPageProcesses), info, table, processFooter(h, snapshot, in.UID),
	}}
}

func monitorRole(name string, fallback ui.PaintRole) ui.PaintRole {
	if r, ok := ui.PaintRoleFor(name); ok {
		return r
	}
	return fallback
}

func processTableHeader(h *PanelHost, in monitorView) *ui.Node {
	cell := func(key, label string, width int) *ui.Node {
		text := label
		n := &ui.Node{Kind: ui.KindButton, Action: "monitor:sort:" + key, Name: "Sort by " + label,
			Role: "button", Focusable: true, Width: width, Height: processHeaderHeight, Shape: ui.ShapeSmall,
			Children: []*ui.Node{{Kind: ui.KindText, Text: text, CenterX: true}}}
		if h.processSort == key {
			arrow := "▲ "
			if h.processDesc {
				arrow = "▼ "
			}
			n.Children[0].Text = arrow + label
			n.Fill = ui.FillRole
			n.FillRole = monitorRole(in.Config.SortBackground, ui.PaintSurfaceVariant)
			n.InkRole = monitorRole(in.Config.SortColor, ui.PaintOnSurfaceVariant)
		}
		return n
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Padding: processTablePadding, Height: processHeaderHeight,
		Children: []*ui.Node{cell("name", "Name", processNameWidth(h))}}
	for _, c := range processColumnsAfterName {
		row.Children = append(row.Children, cell(c.key, c.label, c.width))
	}
	return row
}

func processLineRow(h *PanelHost, in monitorView, l processLine) *ui.Node {
	if l.Kind == lineSection {
		chevron := "chevron_right"
		if l.Expanded {
			chevron = "expand_more"
		}
		return &ui.Node{Kind: ui.KindRow, Height: processRowHeight, Gap: theme.MarginS, CenterY: true,
			Action: "monitor:toggle:" + l.Key, Name: l.Name, Role: "button", Focusable: true, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: chevron, IconSize: processIconSize},
				{Kind: ui.KindText, Text: l.Name, TextRole: theme.RoleTitle, Tone: ui.ToneActivity, Bold: true},
			}}
	}
	nameCell := &ui.Node{Kind: ui.KindRow, Width: processNameWidth(h), Gap: theme.MarginS, CenterY: true}
	nameCell.Children = append(nameCell.Children, &ui.Node{Kind: ui.KindColumn, Width: processIndent * (l.Depth + 1)})
	if l.Expandable {
		chevron := "chevron_right"
		if l.Expanded {
			chevron = "expand_more"
		}
		nameCell.Children[0] = &ui.Node{Kind: ui.KindRow, Width: processIndent * (l.Depth + 1), PinEnd: true,
			Children: []*ui.Node{{Kind: ui.KindColumn}, {Kind: ui.KindIcon, Icon: chevron, IconSize: processIconSize - 4}}}
	}
	nameCell.Children = append(nameCell.Children, processLineIcon(in, l), &ui.Node{Kind: ui.KindText, Text: l.Name,
		MaxWidth: processNameWidth(h) - processIndent*(l.Depth+1) - processIconSize - 2*theme.MarginS})
	row := &ui.Node{Kind: ui.KindRow, Height: processRowHeight, Padding: 2, Gap: theme.MarginM, CenterY: true,
		Shape: ui.ShapeSmall, Role: "row", Focusable: true, Name: l.Name,
		HoverFill: monitorRole(in.Config.HoverBackground, ui.PaintSurfaceVariant),
		HoverInk:  monitorRole(in.Config.HoverColor, ui.PaintOnSurfaceVariant),
		Children:  []*ui.Node{nameCell}}
	if l.Kind == lineGroup {
		row.Action = "monitor:toggle:" + l.Key
	} else {
		row.Action = fmt.Sprintf("monitor:select:%d:%d", l.Identity.PID, l.Identity.StartTimeTicks)
		if h.processSelected == l.Identity {
			row.Fill = ui.FillSoft
		}
	}
	for _, c := range processColumnsAfterName {
		var text string
		switch c.key {
		case "pid":
			text = l.PIDText
		case "user":
			text = l.User
		default:
			text = formatProcessCell(c.key, l.Totals)
		}
		cell := &ui.Node{Kind: ui.KindCapsule, Width: c.width, Height: processRowHeight - 4, Name: c.label + " value",
			Shape: ui.ShapeSmall, Children: []*ui.Node{{Kind: ui.KindText, Text: text, Tabular: true, MaxWidth: c.width - 8, CenterY: true}}}
		if c.key == "cpu" || c.key == "mem" || c.key == "swap" || c.key == "io" {
			cell.Children[0].PinEnd = true
		}
		if h.processSort == c.key {
			cell.Fill = ui.FillRole
			cell.FillRole = monitorRole(in.Config.SortBackground, ui.PaintSurfaceVariant)
			cell.InkRole = monitorRole(in.Config.SortColor, ui.PaintOnSurfaceVariant)
		}
		row.Children = append(row.Children, cell)
	}
	return row
}

func processLineIcon(in monitorView, l processLine) *ui.Node {
	if in.Icon != nil && l.Icon != "" {
		if img := in.Icon(l.Icon, processIconSize); img != nil {
			return &ui.Node{Kind: ui.KindImage, Image: img, Width: processIconSize, Height: processIconSize}
		}
	}
	return &ui.Node{Kind: ui.KindCapsule, Width: processIconSize, Height: processIconSize, Fill: ui.FillContainer,
		Shape: ui.ShapeSmall, Children: []*ui.Node{{Kind: ui.KindText, Text: launcherGlyph(l.Name),
			TextRole: theme.RoleCaption, CenterX: true, CenterY: true}}}
}

// formatProcessCell is one numeric cell in the reference's form. Zero and
// unavailable are the same em dash, as they are there.
func formatProcessCell(kind string, t processTotals) string {
	spaced := func(b uint64) string {
		s := formatProcessBytes(b)
		return s[:len(s)-1] + " " + s[len(s)-1:]
	}
	switch kind {
	case "cpu":
		if !t.CPUValid || t.CPU*100 < 0.05 {
			return ccDash
		}
		pct := t.CPU * 100
		if pct == math.Trunc(pct) {
			return fmt.Sprintf("%.0f%%", pct)
		}
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", pct), "0"), ".") + "%"
	case "mem":
		if !t.ResidentValid || t.Resident == 0 {
			return ccDash
		}
		return spaced(t.Resident)
	case "swap":
		if !t.SwapValid || t.Swap == 0 {
			return ccDash
		}
		return spaced(t.Swap)
	case "io":
		if !t.IOValid || t.IO < 1 {
			return ccDash
		}
		return spaced(uint64(t.IO)) + "/s"
	}
	return ccDash
}

func processFooter(h *PanelHost, snapshot services.ProcessSnapshot, uid uint32) *ui.Node {
	users := 0
	for _, p := range snapshot.Processes {
		if p.UIDValid && p.UID == uid {
			users++
		}
	}
	text := fmt.Sprintf("Total processes: %d (user: %d  system: %d)", len(snapshot.Processes), users, len(snapshot.Processes)-users)
	tone := ui.ToneSubtle
	switch {
	case h.processStatus != "":
		text, tone = h.processStatus, ui.ToneNormal
		if h.processStatusErr != nil {
			tone = ui.ToneError
		}
	case len(snapshot.Issues) > 0:
		text += fmt.Sprintf(" · %d could not be read", len(snapshot.Issues))
	}
	return &ui.Node{Kind: ui.KindText, Text: text, Tone: tone, TextRole: theme.RoleCaption, Height: processFooterHeight}
}
```

In `TestFormatProcessCell`, `.01` gives `pct == 1` exactly and takes the `%.0f` branch. `.002` gives
`0.2`, which trims to `0.2%`. If floating-point representation breaks either case, round `pct` to one
decimal (`math.Round(pct*10)/10`) before the comparison.

`activateMonitor` replaces the page, filter and sort cases, and keeps the select and signal handling
below them:

```go
func (h *PanelHost) activateMonitor(r *Registry, n *ui.Node) bool {
	rebuild := func() bool { r.rebuildPanel(h); return true }
	switch a := n.Action; {
	case a == "monitor:close":
		r.closePanelLocked(h.id)
		return true
	case a == "monitor:settings":
		return r.openSettingsAtLocked(h.output, "Monitor")
	case a == "monitor:options":
		h.monitorOptions = !h.monitorOptions
		return rebuild()
	case a == "monitor:clear":
		h.query, h.search = "", ui.NewField("")
		return rebuild()
	case strings.HasPrefix(a, "monitor:page:"):
		page := strings.TrimPrefix(a, "monitor:page:")
		if page != monitorPageProcesses && page != monitorPageMetrics {
			return false
		}
		h.monitorPage = page
		return rebuild()
	case strings.HasPrefix(a, "monitor:owner:"):
		o := strings.TrimPrefix(a, "monitor:owner:")
		if o != "all" && o != "user" && o != "system" {
			return false
		}
		h.processFilter = o
		return rebuild()
	case strings.HasPrefix(a, "monitor:sort:"):
		key := strings.TrimPrefix(a, "monitor:sort:")
		if !validProcessSort(key) {
			return false
		}
		if h.processSort == key {
			h.processDesc = !h.processDesc
		} else {
			h.processSort, h.processDesc = key, key != "name" && key != "user" && key != "pid"
		}
		return rebuild()
	case strings.HasPrefix(a, "monitor:toggle:"):
		key := strings.TrimPrefix(a, "monitor:toggle:")
		if strings.HasPrefix(key, "section:") {
			h.processCollapsed[key] = !h.processCollapsed[key]
		} else {
			h.processExpanded[key] = !h.processExpanded[key]
		}
		return rebuild()
	case a == "monitor:show:apps" || a == "monitor:show:procs":
		c := r.cfg
		if a == "monitor:show:apps" {
			c.Monitor.ShowApps = !c.Monitor.ShowApps
		} else {
			c.Monitor.ShowProcesses = !c.Monitor.ShowProcesses
		}
		if err := r.writeConfig(c); err != nil {
			h.processStatus, h.processStatusErr = "Could not save view options: "+err.Error(), err
		}
		return rebuild()
	}
	// (existing monitor:select and signal handling follows unchanged)
```

```go
func validProcessSort(key string) bool {
	switch key {
	case "name", "cpu", "mem", "swap", "io", "pid", "user":
		return true
	}
	return false
}
```

Check whether `r.writeConfig` updates `r.cfg` itself, and whether it takes `r.mu`. Read it in
`popout_settings.go` near line 666. If it expects the lock to be released, follow the pattern
`persistDraft` uses, and note the adjustment in the commit body.

The search field feeds `h.query`, as it does today. Keep whatever path currently copies
`h.search`'s text into `h.query` for this panel.

In the `panelhost.go` reset near line 689, set `h.processSort, h.processDesc = "mem", true` and
`h.processExpanded, h.processCollapsed = map[string]bool{}, map[string]bool{}`.

Until Task 10, `systemPageTree` does not exist. In this task, have `monitorPanelTree` call the old
`monitorTree(h.metrics(), monitorSelectors(...), in.Snap, in.History, in.Facts)` wrapped in a scroll,
exactly as the old code did. Task 10 replaces that branch.

- [ ] **Step 4: Run the shell package**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS. A failure in an old monitor or process test that asserts removed chrome (the
segmented page switcher, the per-row Kill, the Memory column name) is expected. Update that test to
the new names, or delete it if a test in this task already covers it, and list each one in the commit
body.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/popout_process.go internal/shell/popout_process_test.go internal/shell/panelhost.go internal/shell/tray.go
git commit -F msg.txt   # "feat(shell): rebuild the processes page after the reference"
```

---

### Task 8: The process detail view, SIGINT, SIGKILL and Escape

**Files:**
- Create: `internal/shell/processdetail.go`
- Create: `internal/shell/processdetail_test.go`
- Modify: `internal/shell/popout_process.go` (`parseProcessAction`, `scheduleProcessSignal` status text)
- Modify: `internal/shell/panelhost.go` (the `keyEsc` case near line 1428)

**Interfaces:**
- Consumes: `monitorView` (Task 6), `processTableTree` (Task 7).
- Produces:
  - `processDetailCard(h *PanelHost, in monitorView, snap services.ProcessSnapshot) *ui.Node`.
  - `parseProcessAction(action string) (services.ProcessIdentity, syscall.Signal, bool)`, which accepts
    `process:int:` and `process:kill:`.
  - The `monitor:detail:close` action.

- [ ] **Step 1: Write the failing tests**

`processdetail_test.go`:

```go
package shell

import (
	"syscall"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestProcessActionsParseIntAndKillOnly(t *testing.T) {
	cases := []struct {
		action string
		sig    syscall.Signal
		ok     bool
	}{
		{"process:int:20:200", syscall.SIGINT, true},
		{"process:kill:20:200", syscall.SIGKILL, true},
		{"process:term:20:200", 0, false},
		{"process:kill:0:200", 0, false},
		{"process:kill:20", 0, false},
	}
	for _, c := range cases {
		id, sig, ok := parseProcessAction(c.action)
		if ok != c.ok || ok && (sig != c.sig || id != (services.ProcessIdentity{PID: 20, StartTimeTicks: 200})) {
			t.Errorf("%s = %v %v %v", c.action, id, sig, ok)
		}
	}
}

func TestDetailViewShowsTheSelectedProcessAndItsActions(t *testing.T) {
	h := processHost()
	h.processSelected = services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	if card == nil {
		t.Fatal("no detail card")
	}
	for _, text := range []string{"comm:", "beta", "PID:", "20", "cmdline:", "beta --Chrome-Helper"} {
		if !treeHasNameOrText(card, text) {
			t.Errorf("detail missing %q", text)
		}
	}
	kill := findAction(card, "process:int:20:200")
	force := findAction(card, "process:kill:20:200")
	if kill == nil || force == nil || findAction(card, "monitor:detail:close") == nil {
		t.Fatal("detail lacks its three actions")
	}
	if kill.AriaDisabled || force.AriaDisabled {
		t.Fatal("own process actions are disabled")
	}
}

func TestDetailActionsDisableForAForeignProcess(t *testing.T) {
	h := processHost()
	h.processSelected = services.ProcessIdentity{PID: 10, StartTimeTicks: 100} // uid 0
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	for _, a := range []string{"process:int:10:100", "process:kill:10:100"} {
		n := findAction(card, a)
		if n == nil || !n.AriaDisabled || n.State&ui.StateDisabled == 0 {
			t.Errorf("%s not disabled: %+v", a, n)
		}
	}
}

func TestDetailViewReportsAnExitedProcess(t *testing.T) {
	h := processHost()
	h.processSelected = services.ProcessIdentity{PID: 99, StartTimeTicks: 990}
	v := processView(processFixture())
	card := processDetailCard(h, v, *v.Snap.Processes)
	if card == nil || !treeHasNameOrText(card, "Process exited") {
		t.Fatal("exited process not reported")
	}
	if findAction(card, "process:int:99:990") != nil && !findAction(card, "process:int:99:990").AriaDisabled {
		t.Fatal("exited process can still be signalled")
	}
}

func TestNoSelectionMeansNoDetail(t *testing.T) {
	v := processView(processFixture())
	if processDetailCard(processHost(), v, *v.Snap.Processes) != nil {
		t.Fatal("detail without a selection")
	}
}
```

Add to the same file an Escape test against a real registry. Use the pattern
`TestProcessesAreTheDefaultMonitorPage` uses (`newPanelRegistry`, `OpenPanel`, `drainAux`), and the
key helper the other panel key tests use:

```go
func TestEscapeClosesTheDetailBeforeThePanel(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	h.processSelected = services.ProcessIdentity{PID: 20, StartTimeTicks: 200}
	pressKeyForTest(t, reg, h, keyEsc)
	if reg.panelHosts[PanelMonitor] == nil {
		t.Fatal("Escape closed the panel while the detail was open")
	}
	if h.processSelected != (services.ProcessIdentity{}) {
		t.Fatal("Escape left the detail open")
	}
	pressKeyForTest(t, reg, h, keyEsc)
	if reg.panelHosts[PanelMonitor] != nil {
		t.Fatal("second Escape did not close the panel")
	}
}
```

`pressKeyForTest` stands for the existing key-dispatch helper in the panel host tests (search
`keyEsc` in `*_test.go`). Use its real name.

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'ProcessActionsParse|DetailView|DetailActions|NoSelection|EscapeCloses' ./internal/shell`
Expected: build failure on `processDetailCard`.

- [ ] **Step 3: Implement**

`processdetail.go`:

```go
package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// processDetailCard replaces the info card while a process is selected, as
// the reference's process view does. It returns nil with no selection.
func processDetailCard(h *PanelHost, in monitorView, snap services.ProcessSnapshot) *ui.Node {
	id := h.processSelected
	if id == (services.ProcessIdentity{}) {
		return nil
	}
	var p services.Process
	found := false
	for _, candidate := range snap.Processes {
		if candidate.Identity == id {
			p, found = candidate, true
			break
		}
	}
	suffix := fmt.Sprintf(":%d:%d", id.PID, id.StartTimeTicks)
	own := found && p.UIDValid && p.UID == in.UID
	action := func(icon, act, name string) *ui.Node {
		n := centreIconButton(icon, act+suffix, name)
		n.Tone = ui.ToneError
		if !own {
			n.AriaDisabled = true
			n.State |= ui.StateDisabled
		}
		return n
	}
	buttons := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		action("disabled_by_default", "process:int", "Kill (SIGINT)"),
		action("skull", "process:kill", "Force kill (SIGKILL)"),
		centreIconButton("cancel", "monitor:detail:close", "Close process view"),
	}}
	if !found {
		return detailFrame(buttons, &ui.Node{Kind: ui.KindText, Text: "Process exited", Tone: ui.ToneSubtle}, nil)
	}
	cpu := ccDash
	if p.CPU.Valid {
		cpu = fmt.Sprintf("%.1f%%", p.CPU.Fraction*100)
	}
	mem := ccDash
	if p.ResidentValid {
		mem = formatProcessBytes(p.ResidentBytes)
	}
	swap := ccDash
	if s, ok := processSwap(p); ok {
		swap = formatProcessBytes(s)
	}
	exe := processExecutable(p)
	if exe == "" {
		exe = ccDash
	}
	ioRead, ioWrite := ccDash, ccDash // Task 13 fills these from the per-process I/O rates
	private, shared := ccDash, ccDash // Task 13 fills these from the per-process memory detail
	left := detailPairs([][2]string{
		{"comm:", p.Name}, {"PID:", strconv.Itoa(p.Identity.PID)}, {"PPID:", strconv.Itoa(p.ParentPID)},
		{"exe:", exe}, {"cmdline:", strings.Join(p.Args, " ")}, {"cpu:", cpu},
	}, 420)
	right := detailPairs([][2]string{
		{"private:", private}, {"shared:", shared}, {"mem:", mem},
		{"swap:", swap}, {"io read:", ioRead}, {"io write:", ioWrite},
	}, 160)
	return detailFrame(buttons, left, right)
}

func detailPairs(pairs [][2]string, valueWidth int) *ui.Node {
	col := &ui.Node{Kind: ui.KindColumn, Gap: 2}
	for _, kv := range pairs {
		value := kv[1]
		if value == "" {
			value = ccDash
		}
		col.Children = append(col.Children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: kv[0], Tone: ui.ToneSubtle, MinWidthText: "io write:"},
			{Kind: ui.KindText, Text: value, MaxWidth: valueWidth, Tabular: true, Name: kv[0] + " value"},
		}})
	}
	return col
}

func detailFrame(buttons, left, right *ui.Node) *ui.Node {
	children := []*ui.Node{buttons, {Kind: ui.KindSeparator}, left}
	if right != nil {
		children = append(children, &ui.Node{Kind: ui.KindSeparator}, right)
	}
	return &ui.Node{Kind: ui.KindCapsule, Height: monitorInfoH, Padding: theme.MarginL,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginL, Children: children},
		}}
}
```

`parseProcessAction` in `popout_process.go` becomes:

```go
func parseProcessAction(action string) (services.ProcessIdentity, syscall.Signal, bool) {
	if id, ok := parseProcessIdentityAction(action, "process:int"); ok {
		return id, syscall.SIGINT, true
	}
	if id, ok := parseProcessIdentityAction(action, "process:kill"); ok {
		return id, syscall.SIGKILL, true
	}
	return services.ProcessIdentity{}, 0, false
}
```

In `scheduleProcessSignal`, the success status names the signal:

```go
			name := "INT"
			if signal == syscall.SIGKILL {
				name = "KILL"
			}
			current.processStatus = fmt.Sprintf("Sent %s to PID %d", name, identity.PID)
			current.processSelected = services.ProcessIdentity{}
```

In `activateMonitor`, before the select handling, add:

```go
	if n.Action == "monitor:detail:close" {
		h.processSelected = services.ProcessIdentity{}
		return rebuild()
	}
```

The disabled buttons must not dispatch. Confirm `StateDisabled` blocks activation in the host's
activate path; if it does not, return early in `activateMonitor` for a signal action whose target
node carries `AriaDisabled`.

In `panelhost.go`, in the `keyEsc` case before `r.closePanelLocked`:

```go
		if h.id == PanelMonitor && h.processSelected != (services.ProcessIdentity{}) {
			h.processSelected = services.ProcessIdentity{}
			r.rebuildPanel(h)
			return true
		}
```

In `processTableTree` (Task 7), keep the `if detail := processDetailCard(...)` block.

- [ ] **Step 4: Run the tests**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS. `TestProcessSignalRunsUnlockedAndReportsIdentityFailure` must still pass; update its
action string to `process:int:` if it used `process:term:`.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/processdetail.go internal/shell/processdetail_test.go internal/shell/popout_process.go internal/shell/popout_process_test.go internal/shell/panelhost.go
git commit -F msg.txt   # "feat(shell): process detail view with interrupt and force kill"
```

---

### Task 9: Open the panel sorted

`panel.open system-monitor` takes the reference's `order_by` as its section:
`{"panel":"system-monitor","section":"-mem"}`.

**Files:**
- Modify: `internal/shell/panelhost.go` (`panelSection` near line 325, `selectPanelSectionLocked` near line 354)
- Test: `internal/shell/popout_process_test.go`

**Interfaces:**
- Produces: `parseProcessOrder(s string) (key string, desc bool, ok bool)`, which accepts
  `name|cpu|mem|swap|io|pid|user`, each optionally prefixed with `-`.

- [ ] **Step 1: Write the failing tests**

```go
func TestParseProcessOrder(t *testing.T) {
	cases := []struct {
		in   string
		key  string
		desc bool
		ok   bool
	}{
		{"mem", "mem", true, true},
		{"-mem", "mem", false, true},
		{"name", "name", false, true},
		{"-name", "name", true, true},
		{"io", "io", true, true},
		{"memory", "", false, false},
		{"", "", false, false},
	}
	for _, c := range cases {
		key, desc, ok := parseProcessOrder(c.in)
		if key != c.key || desc != c.desc || ok != c.ok {
			t.Errorf("%q = %q %v %v", c.in, key, desc, ok)
		}
	}
}

func TestPanelOpenWithAnOrderSortsTheTable(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.HandlePanelByName("open", "system-monitor", "-cpu"); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	if h.processSort != "cpu" || h.processDesc || h.monitorPage != monitorPageProcesses {
		t.Fatalf("sort = %q desc=%v page=%q", h.processSort, h.processDesc, h.monitorPage)
	}
	if err := reg.HandlePanelByName("open", "system-monitor", "memory"); err == nil {
		t.Fatal("unknown order accepted")
	}
}
```

The reference's default direction: descending for numeric keys, and ascending for `name` and
`user`. A `-` prefix flips it. `pid` is numeric, so `pid` alone sorts descending.

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'ParseProcessOrder|PanelOpenWithAnOrder' ./internal/shell`
Expected: build failure on `parseProcessOrder`.

- [ ] **Step 3: Implement**

```go
func parseProcessOrder(s string) (string, bool, bool) {
	flip := strings.HasPrefix(s, "-")
	key := strings.TrimPrefix(s, "-")
	if !validProcessSort(key) {
		return "", false, false
	}
	desc := key != "name" && key != "user"
	if flip {
		desc = !desc
	}
	return key, desc, true
}
```

In `panelSection`, add a case:

```go
	case PanelMonitor:
		if _, _, ok := parseProcessOrder(requested); ok {
			return requested, nil
		}
```

In `selectPanelSectionLocked`, before `if h.section == section`:

```go
	if id == PanelMonitor {
		key, desc, _ := parseProcessOrder(section)
		h.monitorPage, h.processSort, h.processDesc = monitorPageProcesses, key, desc
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(id))
		return nil
	}
```

Move `validProcessSort` next to `parseProcessOrder` if they are in different files.

- [ ] **Step 4: Run the tests**

Run: `GOMAXPROCS=4 go test -count=1 -run 'ParseProcessOrder|PanelOpenWithAnOrder|Panel' ./internal/shell ./internal/ipc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/panelhost.go internal/shell/popout_process.go internal/shell/popout_process_test.go
git commit -F msg.txt   # "feat(shell): open the system monitor at a sort order"
```

---

### Task 10: The System page, shared leases and the refresh interval

**Files:**
- Create: `internal/shell/popout_system.go`
- Create: `internal/shell/popout_system_test.go`
- Modify: `internal/shell/controlcenter_leases.go` (`syncControlCentreSubjectsLocked` → `syncRateSubjectsLocked(h, snap, interval)`)
- Modify: `internal/shell/controlcenter_monitor.go` (`ccMonGPURow` and `ccMonStorageRow` split into value builders the System page reuses)
- Modify: `internal/shell/panelhost.go` (`acquirePanelLeases` `PanelMonitor` case; the Control Centre selector list becomes `monitorLeaseSelectors()`)
- Modify: `internal/shell/registry.go` (`UpdateMetrics` syncs subjects for `PanelMonitor` too)
- Modify: `internal/shell/popout_monitor.go` (delete `monitorTree`, `monitorRows`, `monitorMetricCard`, `monitorLegend`, `monitorSystemCard`, `monitorResourcesCard`, `monitorCapacityRow`, `monitorKeyValue`, `monitorGraphValues`, `formatMonitorMetric`, `selectorLabel`, and `monitorSelectors` with its test; keep `selectGPU`, `monitorHistory`, `monitorCard`, `monitorCardTitle`, `monitorIconRune` and the fact readers)
- Modify: `internal/shell/popout_monitor_test.go` (delete the tests of deleted builders)

**Interfaces:**
- Consumes: `monitorHeader`, `monitorInfoCard`, `monitorFactsColumn` and `monitorView` (Task 6);
  `config.Monitor.Refresh` (Task 3); `ccMonRow`, `ccMonValue`, `ccMonCaption`, `ccMonGraph`,
  `thresholdTone`, `rateCeiling`, `scaleSeries` and `peakCaption` (existing).
- Produces:
  - `systemPageTree(h *PanelHost, in monitorView) *ui.Node`.
  - `monitorLeaseSelectors() []services.Selector`.
  - `(*Registry).syncRateSubjectsLocked(h *PanelHost, snap services.Snapshot, interval time.Duration)`.

- [ ] **Step 1: Write the failing tests**

`popout_system_test.go`:

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func systemHost() *PanelHost {
	h := processHost()
	h.monitorPage = monitorPageMetrics
	return h
}

func TestSystemPageHasThreeSectionsOfRows(t *testing.T) {
	root := monitorPanelTree(systemHost(), testMonitorView())
	for _, text := range []string{"Compute", "Memory", "Storage & network", "CPU", "GPU", "RAM", "Swap", "/", "Disk I/O", "Network"} {
		if !treeHasNameOrText(root, text) {
			t.Errorf("system page missing %q", text)
		}
	}
	if findByName(root, "Search") != nil {
		t.Fatal("system page shows the process search")
	}
	if findKind(root, ui.KindVirtualList) != nil {
		t.Fatal("system page shows the process table")
	}
}

func TestSystemPageDashesAnAbsentSourceAndPaintsNoMark(t *testing.T) {
	root := monitorPanelTree(systemHost(), testMonitorView()) // no GPU, memory or network in the view
	gpu := findByName(root, "GPU usage")
	if gpu == nil || gpu.Text != ccDash {
		t.Fatalf("GPU value = %+v", gpu)
	}
	walkNodes(root, func(n *ui.Node) {
		if n.Kind == ui.KindGraph && !n.Absent && len(n.Values) == 0 {
			t.Errorf("graph with no values is not absent: %+v", n)
		}
	})
}

func TestSystemPageSectionsCollapse(t *testing.T) {
	h := systemHost()
	h.processCollapsed["section:compute"] = true
	root := monitorPanelTree(h, testMonitorView())
	if findByName(root, "CPU usage") != nil {
		t.Fatal("collapsed Compute still shows CPU")
	}
	if findAction(root, "monitor:toggle:section:compute") == nil {
		t.Fatal("Compute header has no toggle")
	}
}

func TestMonitorLeasesMatchTheControlCentreSet(t *testing.T) {
	want := map[services.Selector]bool{
		{Source: services.SourceCPU}: true, {Source: services.SourceMemory}: true,
		{Source: services.SourceCPU, Subject: "temperature"}: true, {Source: services.SourceGPU}: true,
		{Source: services.SourceFilesystem, Subject: "/"}: true,
		{Source: services.SourceNetwork}: true, {Source: services.SourceBlock}: true,
	}
	for _, sel := range monitorLeaseSelectors() {
		delete(want, sel)
	}
	if len(want) != 0 {
		t.Fatalf("missing leases: %v", want)
	}
}
```

Extend `TestMonitorLeaseReusesM3Service` in `popout_monitor_test.go`, or write a sibling test with its
fixture. It opens the monitor with `cfg.Monitor.Refresh = 3` and asserts that the process lease was
acquired at `3 * time.Second`. It asserts the interval through the fake metrics service's recorded
calls; use whatever that test already inspects.

- [ ] **Step 2: Run them to see them fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'SystemPage|MonitorLeases|MonitorLease' ./internal/shell`
Expected: build failure on `systemPageTree` and `monitorLeaseSelectors`.

- [ ] **Step 3: Implement**

`panelhost.go`: extract the Control Centre selector list into:

```go
// monitorLeaseSelectors are the sources the Control Centre's Monitor page and
// the System page chart. Battery is the Control Centre's alone.
func monitorLeaseSelectors() []services.Selector {
	return []services.Selector{
		{Source: services.SourceCPU},
		{Source: services.SourceMemory},
		{Source: services.SourceCPU, Subject: "temperature"},
		{Source: services.SourceGPU},
		{Source: services.SourceFilesystem, Subject: "/"},
		{Source: services.SourceNetwork},
		{Source: services.SourceBlock},
	}
}
```

The Control Centre case iterates `append(monitorLeaseSelectors(), services.Selector{Source: services.SourceBattery})`.
The `PanelMonitor` case becomes:

```go
	case PanelMonitor:
		interval := time.Duration(max(r.cfg.Monitor.Refresh, 1)) * time.Second
		for _, sel := range append(monitorLeaseSelectors(), services.Selector{Source: services.SourceProcess}) {
			lease, err := r.metrics.Acquire(sel, interval)
			if err != nil {
				releaseAll(h.leases)
				h.leases = nil
				return err
			}
			h.leases = append(h.leases, lease)
		}
```

`controlcenter_leases.go`: rename the function to `syncRateSubjectsLocked`, add an
`interval time.Duration` parameter, and use it in `Acquire`. Update the doc comment: it serves any
host that charts one interface and one device. In `registry.go` `UpdateMetrics`, the Control Centre
call passes `time.Second`. In the `PanelMonitor` block, call it before the rebuild:

```go
	if h := r.panelHosts[PanelMonitor]; h != nil {
		r.syncRateSubjectsLocked(h, snap, time.Duration(max(r.cfg.Monitor.Refresh, 1))*time.Second)
		r.rebuildPanel(h)
		monitorOut, monitorOK = h.output, true
	}
```

`controlcenter_monitor.go`: the System page needs the RAM and Swap meters as separate rows, and a
GPU caption that also carries temperature. Add, and use from the existing rows where they overlap:

```go
// ccMonCapacity is one used-of-total reading: percent value, bytes caption,
// meter fraction, threshold tone.
func ccMonCapacity(metric monitorMetric, used, total uint64) (value, caption string, fraction float64, tone ui.Tone, ok bool) {
	if total == 0 {
		return ccDash, "", 0, ui.ToneNormal, false
	}
	fraction = float64(used) / float64(total)
	tone = ui.ToneNormal
	if metric != "" {
		tone = thresholdTone(metric, fraction*100)
	}
	return fmt.Sprintf("%.0f%%", fraction*100), formatBytes(float64(used)) + " / " + formatBytes(float64(total)), fraction, tone, true
}
```

`monitorMetric` stands for the type `thresholdTone`'s first parameter already has in
`monitorscale.go`; use that type. An empty value means no threshold (swap has none).

`popout_system.go`:

```go
package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// systemPageTree is the System page: the shared header and info card over one
// card of collapsible sections in the process table's style. Row content is
// the Control Centre Monitor page's, so the two surfaces never disagree.
func systemPageTree(h *PanelHost, in monitorView) *ui.Node {
	snap, history := in.Snap, in.History
	if history == nil {
		history = map[services.Selector][]float64{}
	}
	cpuSel := services.Selector{Source: services.SourceCPU}
	cpuValue, cpuTone, cpuCaption := ccDash, ui.ToneNormal, ""
	var cpuSamples []float64
	if snap.CPU != nil && snap.CPU.Usage.Valid {
		pct := snap.CPU.Usage.Fraction * 100
		cpuValue, cpuTone, cpuSamples = fmt.Sprintf("%.0f%%", pct), thresholdTone(metricCPU, pct), history[cpuSel]
	}
	if snap.Thermal != nil && snap.Thermal.Valid {
		cpuCaption = fmt.Sprintf("%.0f°C", snap.Thermal.Celsius)
	}
	if ghz, ok := meanCoreGHz(snap); ok {
		cpuCaption = joinCaption(cpuCaption, fmt.Sprintf("%.2f GHz", ghz))
	}
	if snap.CPU != nil && snap.CPU.LoadValid {
		cpuCaption = joinCaption(cpuCaption, fmt.Sprintf("load %.2f %.2f %.2f", snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15))
	}
	cpu := ccMonRow("cpu", "CPU", ccMonValue("CPU usage", cpuValue, cpuTone, theme.RoleBody),
		ccMonCaption(cpuCaption), ccMonGraph(ccMonMarkW, ccMonMarkH, cpuSamples, nil, cpuTone))

	var ram, swap *ui.Node
	{
		var used, total, sUsed, sTotal uint64
		if snap.Memory != nil {
			used, total = snap.Memory.Memory.UsedBytes, snap.Memory.Memory.TotalBytes
			sUsed, sTotal = snap.Memory.Swap.UsedBytes, snap.Memory.Swap.TotalBytes
		}
		v, c, f, tone, ok := ccMonCapacity(metricMemory, used, total)
		ram = ccMonRow("memory", "RAM", ccMonValue("RAM used", v, tone, theme.RoleBody), ccMonCaption(c),
			&ui.Node{Kind: ui.KindMeter, Width: ccMonMarkW, Height: ccMonMemMeterH, Value: f, Max: 1, Tone: tone, Absent: !ok})
		v, c, f, tone, ok = ccMonCapacity("", sUsed, sTotal)
		swap = ccMonRow("memory", "Swap", ccMonValue("Swap used", v, tone, theme.RoleBody), ccMonCaption(c),
			&ui.Node{Kind: ui.KindMeter, Width: ccMonMarkW, Height: ccMonMemMeterH, Value: f, Max: 1, Tone: tone, Absent: !ok})
	}

	sections := []struct {
		key, title string
		rows       []*ui.Node
	}{
		{"section:compute", "Compute", []*ui.Node{cpu, ccMonGPURow(snap, history)}},
		{"section:memory", "Memory", []*ui.Node{ram, swap}},
		{"section:storage", "Storage & network", []*ui.Node{
			ccMonStorageRow(snap),
			ccMonRateRow("filesystem", "Disk I/O", in.Device, snap, history,
				services.Selector{Source: services.SourceBlock, Subject: in.Device},
				services.Selector{Source: services.SourceBlock, Subject: in.Device, Direction: "write"},
				"R ", "W ", diskRateFloor),
			ccMonRateRow("network", "Network", in.Iface, snap, history,
				services.Selector{Source: services.SourceNetwork, Subject: in.Iface, Direction: "rx"},
				services.Selector{Source: services.SourceNetwork, Subject: in.Iface, Direction: "tx"},
				"↓ ", "↑ ", networkRateFloor),
		}},
	}
	if h.processCollapsed == nil {
		h.processCollapsed = map[string]bool{}
	}
	body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS}
	for _, s := range sections {
		open := !h.processCollapsed[s.key]
		body.Children = append(body.Children, processLineRow(h, in, processLine{Kind: lineSection, Key: s.key, Name: s.title, Expanded: open}))
		if open {
			for _, r := range s.rows {
				r.Padding = processIndent
				body.Children = append(body.Children, r)
			}
		}
	}
	pad := h.metrics().PanelPadding
	bodyH := max(h.place.Panel.H-2*pad-monitorHeaderH-monitorInfoH-2*theme.MarginM, processRowPitch)
	card := &ui.Node{Kind: ui.KindCapsule, Padding: processTablePadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindScroll, Height: bodyH - 2*processTablePadding, Children: []*ui.Node{body}}}}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: pad, Children: []*ui.Node{
		monitorHeader(h, monitorPageMetrics), monitorInfoCard(in, monitorFactsColumn(in.Facts)), card,
	}}
}
```

`ccMonStorageRow` labels its row "Storage". The System page names that row `/`. Give `ccMonStorageRow`
a label parameter, passing `"Storage"` from `ccMonitor` and `"/"` from here. The existing
`TestControlCentreMonitor*` tests pin the Control Centre label.

The GPU caption with temperature (D8) is `name · VRAM · °C`. VRAM arrives with `sysc-522`. Add the
temperature part now inside `ccMonGPURow`, which changes the Control Centre caption too, and update
its test in `controlcenter_monitor_test.go`:

```go
			if g.TempValid {
				name = joinCaption(name, fmt.Sprintf("%.0f°C", g.Celsius))
			}
```

In `monitorPanelTree` (Task 7), replace the old `monitorTree` branch with `return systemPageTree(h, in)`.

Delete the listed builders from `popout_monitor.go`. `go vet ./internal/shell` then names every test
in `popout_monitor_test.go` that referenced them; delete each of those tests. Keep
`TestSelectGPUUsesAStableIdentity`, `TestSelectedGPUFractionPreservesValidity`, `TestParseCPUModel`,
`TestParseOSReleasePrettyName` and `TestMonitorSurfaceHeightCoversATallTree`.

- [ ] **Step 4: Run the shell package**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell
git commit -F msg.txt   # "feat(shell): system page on the monitor frame with shared leases"
```

---

### Task 11: Package gates

- [ ] **Step 1: Format, vet and test the touched packages with the race detector, one at a time**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
GOMAXPROCS=4 go vet -p 2 ./...
for p in ./internal/theme ./internal/ui ./internal/render ./internal/config ./internal/settings ./internal/shell ./internal/ipc; do
  GOMAXPROCS=4 go test -race -count=1 "$p" || break
done
GOMAXPROCS=4 go test -count=1 -p 2 ./...
git diff --exit-code -- go.mod go.sum
```

Expected: every command exits 0. Per the repository rule, `go test -race ./...` is replaced by the
per-package race loop; record that substitution in the handover.

- [ ] **Step 2: Fix anything that fails in the task that owns it, and re-run**

---

### Task 12 (gated): Copy on click

**Gate:** sysc-clipboard has released a version with a `copy` protocol message and a client method
`Copy(text string) error` (D9). This is a separate plan in the sysc-clipboard repository. The bd gate
issue is in this repository.

**Files:**
- Modify: `go.mod`, `go.sum`, `README.md` (pin table)
- Modify: `internal/shell/clipboard.go` (`clipboardCommandSender.Copy`)
- Modify: `internal/shell/processdetail.go` (`detailPairs` values become buttons)
- Modify: `internal/shell/monitorframe.go` (facts rows become buttons)
- Test: `internal/shell/processdetail_test.go`

- [ ] **Step 1: Bump the pin**

```bash
GOMAXPROCS=4 go get github.com/Nomadcxx/sysc-clipboard@<released tag>
```

Update the README pin table row.

- [ ] **Step 2: Write the failing test**

```go
func TestClickingADetailValueCopiesIt(t *testing.T) {
	reg := newPanelRegistry(t)
	sender := &recordingClipboardSender{}
	reg.BindClipboard(sender)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	n := &ui.Node{Action: "monitor:copy:" + url.QueryEscape("beta --Chrome-Helper")}
	reg.mu.Lock()
	h.activateMonitor(reg, n)
	reg.mu.Unlock()
	if len(sender.copied) != 1 || sender.copied[0] != "beta --Chrome-Helper" {
		t.Fatalf("copied = %v", sender.copied)
	}
}
```

`recordingClipboardSender` is the fake the clipboard tests already use. Add a `copied []string` field
and a `Copy` method to it.

- [ ] **Step 3: Implement**

- Add `Copy(string) error` to `clipboardCommandSender`.
- In `detailPairs` and `monitorFactsColumn`, give each value node
  `Action: "monitor:copy:" + url.QueryEscape(value)`, `Role: "button"` and `Focusable: true`,
  unless the value is `ccDash`.
- In `activateMonitor`, `monitor:copy:` unescapes the value and calls `r.clipboardSender.Copy(v)`
  from a goroutine, as `scheduleProcessSignal` does. It sets `h.processStatus` to
  `Copied to clipboard`, or to the error.

- [ ] **Step 4: Run and commit**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`, then commit with `go.mod`, `go.sum` and `README.md`:
`feat(shell): copy monitor values to the clipboard`.

---

### Task 13 (gated): Executable, swap, I/O and the memory breakdown

**Gate:** sysc-metrics has released a version, stacked on v0.6.1, whose `Process` carries the D10
fields. This is a separate plan in the sysc-metrics repository. The shell consumes these exact names;
the upstream plan must adopt them or this task must rename them:

```go
type Process struct {
	// existing fields …
	Executable      string
	ExecutableValid bool
	SwapBytes       uint64
	SwapValid       bool
	IO              ProcessIO
}
type ProcessIO struct {
	ReadBytesPerSecond, WriteBytesPerSecond float64
	Valid                                   bool
}
type ProcessMemory struct {
	PrivateBytes, SharedBytes uint64
}
func ReadProcessMemory(identity ProcessIdentity) (ProcessMemory, error)
```

**Files:**
- Modify: `go.mod`, `go.sum`, `README.md`
- Modify: `internal/services/metrics.go` (alias `ProcessMemory`; wrap `ReadProcessMemory`)
- Modify: `internal/shell/processlines.go` (the three accessors)
- Modify: `internal/shell/processdetail.go` (I/O rows and an off-thread memory read)
- Test: `internal/shell/processlines_test.go`, `internal/shell/processdetail_test.go`

- [ ] **Step 1: Bump the pin and alias the new types**

```bash
GOMAXPROCS=4 go get github.com/Nomadcxx/sysc-metrics@<released tag>
```

In `internal/services/metrics.go`:

```go
type ProcessMemory = metrics.ProcessMemory

func ReadProcessMemory(identity ProcessIdentity) (ProcessMemory, error) {
	return metrics.ReadProcessMemory(identity)
}
```

- [ ] **Step 2: Write the failing tests**

Add to `processlines_test.go`:

```go
func TestExecutableGroupingAndNewColumns(t *testing.T) {
	a := proc(1, 0, "Web Content", 1000, 100, .1)
	a.Executable, a.ExecutableValid = "/usr/lib/firefox/firefox", true
	a.SwapBytes, a.SwapValid = 10, true
	a.IO = services.ProcessIO{ReadBytesPerSecond: 5, WriteBytesPerSecond: 7, Valid: true}
	b := proc(2, 0, "firefox", 1000, 200, .2)
	b.Executable, b.ExecutableValid = "/usr/lib/firefox/firefox", true
	kthread := proc(3, 0, "kworker/0:1", 0, 0, 0)
	lines := projectProcessLines(lineInput([]services.Process{a, b, kthread}, nil))
	g := findLine(t, lines, "exe:/usr/lib/firefox/firefox")
	if g.Name != "firefox" || g.Totals.Swap != 10 || !g.Totals.SwapValid || g.Totals.IO != 12 {
		t.Fatalf("group = %+v", g)
	}
	findLine(t, lines, "pid:3:30") // unreadable exe falls back to a name group of one
}
```

Add to `processdetail_test.go` a test that the detail view shows `io read:` and `io write:` from
`IO`. Add another that `h.processMemory` (below), once set for the selected identity, fills
`private:` and `shared:`.

- [ ] **Step 3: Implement**

```go
func processExecutable(p services.Process) string {
	if p.ExecutableValid {
		return p.Executable
	}
	return ""
}

func processSwap(p services.Process) (uint64, bool) { return p.SwapBytes, p.SwapValid }

func processIO(p services.Process) (float64, bool) {
	return p.IO.ReadBytesPerSecond + p.IO.WriteBytesPerSecond, p.IO.Valid
}
```

In `processDetailCard`, fill `ioRead` and `ioWrite` from `p.IO` with `formatProcessBytes(...) + "/s"`
when `p.IO.Valid`. For `private` and `shared`, add to `PanelHost`:

```go
	processMemory   services.ProcessMemory
	processMemoryOf services.ProcessIdentity
```

When `monitor:select:` changes the selection, start a goroutine that calls
`services.ReadProcessMemory(identity)`. On success it takes `r.mu`, checks that the host is still
current and the selection unchanged, stores the result, rebuilds and publishes. This is the same
shape as `scheduleProcessSignal`. `processDetailCard` shows the values only when
`processMemoryOf == id`. `smaps_rollup` is read once per selection, never on every tick.

- [ ] **Step 4: Run and commit**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/services ./internal/shell`, then commit with `go.mod`,
`go.sum` and `README.md`: `feat(shell): executable groups, swap, disk and memory detail`.

---

### Task 14: Live Niri gate and completion handover

Run after Task 11, and again after each of Tasks 12 and 13 lands.

- [ ] **Step 1: Build and deploy**

Deploy the build as a systemd service swap (the shell runs from systemd). First check that no other
session's build is live: look at the timestamp of `~/.local/bin/sysc-shell` and any `before-*`
copies.

```bash
GOMAXPROCS=4 go build -p 2 -o /tmp/claude-1000/sysc-shell ./cmd/sysc-shell
cp ~/.local/bin/sysc-shell ~/.local/bin/sysc-shell.before-system-monitor-panel
mv /tmp/claude-1000/sysc-shell ~/.local/bin/sysc-shell && systemctl --user restart sysc-shell
```

- [ ] **Step 2: Open each page and capture it**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1) WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000
S=/run/user/1000/sysc-shell/ipc.v1.sock
echo '{"id":1,"method":"panel.open","params":{"panel":"system-monitor","section":"mem"}}' | socat -t2 - UNIX-CONNECT:$S
sleep 2; niri msg -j layers | grep -c sysc-shell-panel   # expect 1
grim /tmp/claude-1000/monitor-processes.png
```

Switch to the System page through the pill; this machine cannot drive it by keyboard injection,
because `wtype` unmaps the panel. Capture it with grim. Crop the two shots, and set the processes
shot beside `screenshots/panel.png` from the reference repository.

- [ ] **Step 3: Assert the observable facts**

- Apply this step to the application row of any running app that has a window. Sum the `VmRSS` of the
  window's PID and every descendant from `/proc`, excluding descendants that own another window. The
  row's MEM value matches that sum to the displayed precision.
- Run `sleep 600 &`, select it (search `sleep`), and use Kill. `kill -0 <pid>` then fails.
- After Task 12: click the PID value and `wl-paste` prints it.
- After Task 13: the SWAP and DISK columns show values for at least one process.

- [ ] **Step 4: Write the completion handover**

Write `docs/plans/2026-09-25-system-monitor-panel-completion-handover.md`, and add its register row in
`docs/plans/README.md` in the same commit. It records:

- the gate output from Task 11;
- the shots and the side-by-side comparison;
- the Step 3 observations;
- anything that differs from the reference and why.

A multi-output check is unrunnable here, since this machine has one output; say so.

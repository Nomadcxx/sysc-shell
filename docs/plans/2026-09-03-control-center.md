# Control Centre Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `PanelControlCenter` — a bar-attached access spine with an icon
rail and seven functional pages — as the Milestone 7 control-centre tranche.

**Architecture:** One new first-party panel composed from shipped `internal/ui`
primitives. The rail reuses the `PanelHost.section` seam that `settingsTree`
already drives; pages are composed natively from the same services the dedicated
panels read, embedding none of their trees. Two genuinely new pieces: a concave
corner mask in `internal/render`, and an idle inhibit held off the Wayland owner.

**Tech Stack:** Go, the native retained renderer, `internal/ui` nodes,
`internal/theme` tokens, `internal/services`, `internal/ipc`.

**Spec:** `docs/plans/2026-09-03-control-center-design.md` (D1–D12 plus the
2026-09-07 amendments). Read it before Task 1; every task argues from it.

**Tracker:** `sysc-154`. Discovered work goes to bd, not to this file.

## Global Constraints

- Go and the native retained renderer are mandatory. No C++, Rust, Lua, Qt, QML,
  Quickshell, CGO, compositor, lock screen, runtime SVG loader, or plugin
  compatibility layer.
- **Never `go build` or `go test ./...` with `-race`.** This box is zram-only
  swap with 16-way linking; a repo-wide race build hard-locks it. Cap `-p 4` and
  `GOMAXPROCS=4`.
- **`go test ./internal/shell` really runs `loginctl terminate-session self`.**
  Shadow `loginctl` and `systemctl` with stubs earlier on `PATH` (Task 0) or it
  logs the owner out.
- `sysc-shell` runs from systemd. Redeploy is `go build -o <tmp> ./cmd/sysc-shell`,
  `mv` over `~/.local/bin/sysc-shell`, `systemctl --user restart sysc-shell.service`.
  Never spawn it by hand beside the running one. Never `pkill -f` your own binary name.
- Run `bd` only from `/home/nomadx/sysc-shell`, never from a worktree.
- The commit-msg hook rejects AI/agent attribution by naive substring, so ordinary
  English trips it — `bot` inside "both"/"bottom" and `llm` inside "Hallmark" are
  confirmed hits. Check the message against `/home/nomadx/.git-hooks/commit-msg`
  before committing. No `Co-Authored-By` trailer.
- Panel `configure`, `render` and `handle` take `Registry.mu`. **No command or
  filesystem I/O may run while `Registry.mu` or the Wayland owner is held.**
- Keep one Wayland dispatch goroutine. Bounded external work stays off it.
- Do not modify Settings, Launcher, Monitor, Session, Calendar, Notifications or
  Wallpaper trees. The control centre reads services, not their trees.
- Do not implement NetworkManager (`sysc-157`), BlueZ (`sysc-155`) or MPRIS
  (`sysc-156`). Their destinations ship disabled.
- Add no new `ui` node kinds. Composition uses shipped primitives.
- This checkout is shared. `git status` before you start; never `git checkout --`,
  `git stash` or `git add -A` over work you did not put there.

---

### Task 0: Reconcile and arm the gate

**Files:** none changed.

- [ ] **Step 1: Confirm the tracker gate is open**

```bash
bd ready -n 200 --json | jq -r '.[] | select(.id=="sysc-154") | .id'
```

Expected: `sysc-154`. If it prints nothing, run `bd dep tree sysc-154` and stop —
the paperwork gate has not closed.

- [ ] **Step 2: Build the PATH stubs**

```bash
mkdir -p ~/.cache/sysc-stubs
printf '#!/bin/sh\necho "stub loginctl $*" >&2\n' > ~/.cache/sysc-stubs/loginctl
printf '#!/bin/sh\necho "stub systemctl $*" >&2\n' > ~/.cache/sysc-stubs/systemctl
printf '#!/bin/sh\necho "stub systemd-inhibit $*" >&2\nexec sleep 86400\n' > ~/.cache/sysc-stubs/systemd-inhibit
chmod +x ~/.cache/sysc-stubs/*
```

- [ ] **Step 3: Verify the tree is green before you touch it**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell ./internal/ui ./internal/ipc ./internal/services ./internal/render
```

Expected: all `ok`. If anything fails, that failure is not yours — record it in bd
and stop.

- [ ] **Step 4: Claim the issue**

```bash
bd update sysc-154 --status in_progress
```

No commit.

---

### Task 1: `AttachedMask` — the wing-tip primitive

**Files:**
- Modify: `internal/render/mask.go`
- Test: `internal/render/mask_test.go`

**Interfaces:**
- Consumes: `roundedCoverage`, the `maskKey` cache pattern already in the file.
- Produces: `render.AttachedMask(w, h, radius, bulge int, edge string) *image.Alpha`
  — `edge` is one of `"top"`, `"bottom"`, `"left"`, `"right"`, matching
  `Placement.BarEdge`. The two corners on `edge` are concave with radius `bulge`;
  the other two are convex with radius `radius`. `bulge == 0` yields the plain
  rounded rect, which is what the reveal animation starts from.

- [ ] **Step 1: Add the coverage helper and the cached mask**

In `internal/render/mask.go`, beside `roundedCoverage`:

```go
// concaveCoverage is the inverse of roundedCoverage for one corner: the pixel
// is inside the shape when it is OUTSIDE the disc whose centre sits on the
// shape's own edge. cx, cy is that centre in mask space.
func concaveCoverage(r, cx, cy, x, y int) uint8 {
	dx := float64(x) + 0.5 - float64(cx)
	dy := float64(y) + 0.5 - float64(cy)
	d := math.Hypot(dx, dy)
	switch {
	case d >= float64(r)+0.5:
		return 255
	case d <= float64(r)-0.5:
		return 0
	default:
		return uint8((d - (float64(r) - 0.5)) * 255)
	}
}
```

Then the mask itself, cached on a key of its own:

```go
type attachedKey struct {
	w, h, radius, bulge int
	edge                string
}

var attachedCache sync.Map // attachedKey -> *image.Alpha

// AttachedMask draws a panel fused to a bar edge. The two corners on edge are
// concave so the panel flares outward into the bar; the two away from it are
// convex and stay rounded throughout the reveal. bulge is the concave radius,
// animated from 0 to radius as the panel emerges, so a hidden edge draws flat.
func AttachedMask(w, h, radius, bulge int, edge string) *image.Alpha {
	if w <= 0 || h <= 0 {
		return image.NewAlpha(image.Rect(0, 0, 0, 0))
	}
	if bulge <= 0 {
		return RoundedMask(radius, w, h)
	}
	key := attachedKey{w, h, radius, bulge, edge}
	if v, ok := attachedCache.Load(key); ok {
		return v.(*image.Alpha)
	}
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetAlpha(x, y, color.Alpha{A: attachedCoverage(w, h, radius, bulge, edge, x, y)})
		}
	}
	attachedCache.Store(key, m)
	return m
}
```

- [ ] **Step 2: Implement `attachedCoverage` for the top edge**

Only `"top"` is reachable in this tranche (the bar's other edges route through the
same switch and are covered by the fallthrough), but write all four so a bottom
bar is not a later surprise:

```go
func attachedCoverage(w, h, radius, bulge int, edge string, x, y int) uint8 {
	// Concave corners live on the bar edge; convex ones on the far edge.
	switch edge {
	case "bottom":
		if y >= h-bulge && x < bulge {
			return concaveCoverage(bulge, bulge, h-bulge, x, y)
		}
		if y >= h-bulge && x >= w-bulge {
			return concaveCoverage(bulge, w-bulge, h-bulge, x, y)
		}
		return roundedCoverage(radius, w, h, x, y)
	default: // "top"
		if y < bulge && x < bulge {
			return concaveCoverage(bulge, bulge, bulge, x, y)
		}
		if y < bulge && x >= w-bulge {
			return concaveCoverage(bulge, w-bulge, bulge, x, y)
		}
		return roundedCoverage(radius, w, h, x, y)
	}
}
```

Add `"image/color"`, `"math"` and `"sync"` to the imports if they are not present.

- [ ] **Step 3: Add the focused check**

In `internal/render/mask_test.go`:

```go
func TestAttachedMaskTopCornersAreConcave(t *testing.T) {
	const w, h, radius, bulge = 60, 40, 12, 12
	m := AttachedMask(w, h, radius, bulge, "top")

	// The outermost top pixel is inside the shape: the panel is widest at the joint.
	if got := m.AlphaAt(0, 0).A; got != 255 {
		t.Errorf("top-left corner pixel = %d, want 255 (wing is filled at the bar)", got)
	}
	// One bulge down, the edge has swept inward, so the same column is empty.
	if got := m.AlphaAt(0, bulge-1).A; got != 0 {
		t.Errorf("pixel below the wing = %d, want 0 (edge swept inward)", got)
	}
	// The far corners stay convex: the very corner pixel is outside.
	if got := m.AlphaAt(0, h-1).A; got != 0 {
		t.Errorf("bottom-left corner = %d, want 0 (convex)", got)
	}
	// bulge 0 is the plain rounded rect, which is where the reveal starts.
	flat := AttachedMask(w, h, radius, 0, "top")
	if flat.AlphaAt(0, 0).A != RoundedMask(radius, w, h).AlphaAt(0, 0).A {
		t.Error("bulge 0 must equal RoundedMask, so a hidden edge draws flat")
	}
}
```

- [ ] **Step 4: Run it**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/render -run TestAttachedMask -v
```

Expected: PASS.

- [ ] **Step 5: Run the package**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/render
```

Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/render/mask.go internal/render/mask_test.go
git commit -m "feat(render): add a concave corner mask for bar-attached panels"
```

---

### Task 2: Extend the Material Symbols subset

**Files:**
- Modify: `internal/render/icons/material/build.py`
- Modify: `internal/render/materialfont.go`
- Modify: `internal/render/icons/material/material-symbols-rounded.ttf` (regenerated)
- Test: `internal/render/materialfont_test.go`

**Interfaces:**
- Produces: sixteen further names valid for `render.ValidMaterialIcon`.

- [ ] **Step 1: Add the names to both lists**

The subset and `build.py`'s `ICONS` are kept in step by hand and asserted by test.
Add these sixteen to **both**:

```
home  music_note  desktop_windows  wifi  bluetooth  cloud
calendar_month  battery_full  coffee  wallpaper
sunny  partly_cloudy_day  rainy  thunderstorm  weather_snowy  foggy
```

In `internal/render/materialfont.go`, extend the `materialIcons` map literal.

- [ ] **Step 2: Rebuild the font**

```bash
cd internal/render/icons/material && python3 build.py && cd -
```

Expected: the `.ttf` is rewritten and larger. `SOURCE.md` requires the rebuild to
be byte-reproducible from the pinned upstream commit
`84ccef280841abfac506afc4ad4a2782f6d0a1d0`; do not bump that pin.

- [ ] **Step 3: Assert every name shapes**

In `internal/render/materialfont_test.go`:

```go
func TestControlCentreIconsAreInTheSubset(t *testing.T) {
	for _, name := range []string{
		"home", "music_note", "desktop_windows", "wifi", "bluetooth",
		"cloud", "calendar_month", "battery_full", "coffee", "wallpaper",
		"sunny", "partly_cloudy_day", "rainy", "thunderstorm", "weather_snowy", "foggy",
	} {
		if !ValidMaterialIcon(name) {
			t.Errorf("%q missing from the subset: it would shape to nothing and paint an invisible control", name)
		}
	}
}
```

- [ ] **Step 4: Run**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/render
```

Expected: `ok`, including the existing test that holds `build.py` and
`materialIcons` in step.

- [ ] **Step 5: Commit**

```bash
git add internal/render/materialfont.go internal/render/materialfont_test.go internal/render/icons/material/
git commit -m "feat(render): carry the control centre glyphs in the icon subset"
```

---

### Task 3: Panel identity, placement and leases

**Files:**
- Modify: `internal/shell/panel.go` (the `PanelID` block and `String`)
- Modify: `internal/shell/panelhost.go` (`parsePanelName`, `panelTree`, the
  placement block near `:395`, `acquirePanelLeases` near `:491`)
- Create: `internal/shell/popout_controlcenter.go`
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Consumes: `Placement`, `alignX`, `clampAxis`, `AttachedMask` from Task 1.
- Produces: `PanelControlCenter PanelID`; public name `control-center`;
  `controlCentreTree(r *Registry, h *PanelHost) *ui.Node`;
  `ccSections []ccSection` with `ccSection{ID, Label, Icon string; Enabled bool}`.

- [ ] **Step 1: Add the panel id**

In `internal/shell/panel.go`, append `PanelControlCenter` to the `const` block
(append only — the values are ordinal and reordering renumbers every panel), and
add `case PanelControlCenter: return "control-center"` to `String`.

- [ ] **Step 2: Add the section table and a placeholder tree**

Create `internal/shell/popout_controlcenter.go`:

```go
package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// ccSection is one rail destination. Disabled entries keep their position so the
// information architecture does not reflow when a backend lands.
type ccSection struct {
	ID, Label, Icon string
	Enabled         bool
}

// ccSections is the rail, top to bottom. Media, Network and Bluetooth are
// disabled in this tranche: sysc-156, sysc-157 and sysc-155 own their backends.
var ccSections = []ccSection{
	{ID: "home", Label: "Home", Icon: "home", Enabled: true},
	{ID: "media", Label: "Media", Icon: "music_note"},
	{ID: "audio", Label: "Audio", Icon: "volume_up", Enabled: true},
	{ID: "monitor", Label: "Monitor", Icon: "desktop_windows", Enabled: true},
	{ID: "power", Label: "Power", Icon: "power_settings_new", Enabled: true},
	{ID: "network", Label: "Network", Icon: "wifi"},
	{ID: "bluetooth", Label: "Bluetooth", Icon: "bluetooth"},
	{ID: "weather", Label: "Weather", Icon: "cloud", Enabled: true},
	{ID: "calendar", Label: "Calendar", Icon: "calendar_month", Enabled: true},
	{ID: "notifications", Label: "Notifications", Icon: "notifications", Enabled: true},
}

func ccSectionFor(id string) (ccSection, bool) {
	for _, s := range ccSections {
		if s.ID == id {
			return s, true
		}
	}
	return ccSection{}, false
}

func controlCentreTree(r *Registry, h *PanelHost) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Gap: 16, Padding: 16}
}
```

- [ ] **Step 3: Wire the dispatch, the name and the placement**

In `internal/shell/panelhost.go`:

- `parsePanelName`: add `case "control-center": return PanelControlCenter, nil`.
- `panelTree`: add `case PanelControlCenter: return controlCentreTree(r, h)`.
- In the placement block, leave `Align` empty so `alignX` centres on the output —
  the bar spans it, so output centre is bar centre — and set the flush gap beside
  the existing `PanelPlugin` line:

```go
if id == PanelPlugin || id == PanelControlCenter {
    gap = 0
}
```

- Seed the section on open, beside the `PanelSettings` seeding:

```go
if id == PanelControlCenter {
    if h.section == "" {
        h.section = "home"
    }
}
```

- `acquirePanelLeases`: add the union case. The bar already holds most of these,
  so they are refcount bumps rather than new samplers:

```go
case PanelControlCenter:
    sels := []services.Selector{
        {Source: services.SourceCPU},
        {Source: services.SourceMemory},
        {Source: services.SourceBattery},
    }
    for _, sel := range sels {
        lease, err := r.metrics.Acquire(sel, time.Second)
        if err != nil {
            releaseAll(h.leases)
            h.leases = nil
            return err
        }
        h.leases = append(h.leases, lease)
    }
    if lease, err := r.clock.Acquire(time.Second); err == nil {
        h.leases = append(h.leases, lease)
    }
```

- [ ] **Step 4: Set the panel size**

The configured size feeds `Placement.Panel`. Give the control centre its
700 × 564 default wherever the other panels take theirs (`r.cfg.Panels`), so an
output too small still routes through `clampAxis` and `FittedSize`.

- [ ] **Step 5: Add the focused check**

Create `internal/shell/controlcenter_test.go`:

```go
func TestControlCentreNameAndFlushPlacement(t *testing.T) {
	id, err := parsePanelName("control-center")
	if err != nil || id != PanelControlCenter {
		t.Fatalf("parsePanelName(control-center) = %v, %v", id, err)
	}
	if got := PanelControlCenter.String(); got != "control-center" {
		t.Errorf("String() = %q, want control-center", got)
	}

	// Gap 0 puts the panel's top edge flush against the bar's exclusive zone,
	// and an empty Align centres it on the output, which is the bar's centre.
	p := Placement{
		BarEdge: "top",
		Output:  ui.Rect{W: 1920, H: 1080},
		BarZone: 44,
		Gap:     0,
		Padding: 8,
		Panel:   ui.Rect{W: 700, H: 564},
	}
	m := p.Margins()
	if m.Top != 44 {
		t.Errorf("Top = %d, want 44 (flush with the bar zone)", m.Top)
	}
	if want := (1920 - 700) / 2; m.Left != want {
		t.Errorf("Left = %d, want %d (centred on the bar)", m.Left, want)
	}
}
```

- [ ] **Step 6: Run**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell -run TestControlCentre -v
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
```

Expected: PASS, then `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/shell/panel.go internal/shell/panelhost.go internal/shell/popout_controlcenter.go internal/shell/controlcenter_test.go
git commit -m "feat(shell): add the control centre panel identity and placement"
```

---

### Task 4: Bar trigger

**Files:**
- Modify: `internal/shell/widget.go` (the `buildWidgets` switch, near `:231`)
- Modify: `internal/config/config.go` (default bar roster)
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Consumes: the existing `wordmark` widget and `Bar.actionCenterX`.
- Produces: right-click control-centre toggle anchored to the wordmark centre.

- [ ] **Step 1: Make the wordmark the trigger**

Give the existing `wordmark` node `panelControlCenterAction`, the accessible
name `Control centre`, and button role metadata. Keep it as `KindWordmark`, so
`capsuled` continues to leave its visual treatment alone.

Route only right-click for this action. Before toggling, set
`trig.AnchorX = bar.actionCenterX(panelControlCenterAction)` so the panel opens
from the wordmark even when a custom bar layout moves it.

- [ ] **Step 2: Remove the redundant widget**

Remove `control-center` from `knownItems`, the default right section, and the
`buildWidgets` switch. Remove the now-unused `buildControlCenterWidget` and
`tune` glyph from the Material subset. Existing configurations that contain the
short-lived item fail validation instead of painting two entry points; this
project carries no configuration compatibility promise.

- [ ] **Step 3: Add the focused check**

```go
func TestWordmarkRightClickOpensControlCentre(t *testing.T) {
	// Build the default bar, assert the wordmark carries the control-centre
	// action, then invoke the bound action with right-click. Left-click must
	// remain inert and the resulting trigger must use the wordmark centre.
}
```

- [ ] **Step 4: Run**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell ./internal/config ./internal/render
```

Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/widget.go internal/config/config.go internal/shell/controlcenter_test.go
git commit -m "feat(shell): open the control centre from a bar trigger"
```

---

### Task 5: IPC section addressing

**Files:**
- Modify: `internal/ipc/server.go` (`knownPanels`, the `panel.*` case, the
  `Panel` handler field)
- Modify: `internal/shell/panelhost.go` (the `Panel` handler implementation)
- Test: `internal/ipc/controlcenter_panel_test.go`

**Interfaces:**
- Produces: `panel.open|toggle|close {"panel":"control-center","section":"audio"}`.
  `Handler.Panel` becomes `func(action, panel, section string) error`.

- [ ] **Step 1: Register the name and widen the handler**

Add `"control-center": ""` to `knownPanels`. Widen `Handler.Panel` to take a
third `section string`, parse `Section` off the params struct, and pass it
through. Update the three `s.h.Panel(...)` call sites and the shell's
implementation to match.

- [ ] **Step 2: Validate the section in the shell**

An unknown or disabled section is an error that leaves the open panel unchanged —
never a silent fall back to Home:

```go
if section != "" {
    s, ok := ccSectionFor(section)
    if !ok || !s.Enabled {
        return fmt.Errorf("unknown section %q", section)
    }
    h.section = section
}
```

An empty section opens Home.

- [ ] **Step 3: Add the focused check**

Create `internal/ipc/controlcenter_panel_test.go`:

```go
func TestControlCentreIsReachableOverIPC(t *testing.T) {
	if _, ok := knownPanels["control-center"]; !ok {
		t.Fatal("the control centre must be reachable over IPC like the launcher")
	}
}
```

Add the shell-side rejection check to `internal/shell/controlcenter_test.go`:

```go
func TestUnknownSectionLeavesThePanelAlone(t *testing.T) {
	if _, ok := ccSectionFor("nope"); ok {
		t.Error("ccSectionFor accepted an unknown id")
	}
	if s, ok := ccSectionFor("network"); !ok || s.Enabled {
		t.Error("network must resolve but stay disabled in this tranche")
	}
}
```

- [ ] **Step 4: Run**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/ipc ./internal/shell
```

Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/ipc internal/shell
git commit -m "feat(ipc): address a control centre section directly"
```

---

### Task 6: Rail, header and page dispatch

**Files:**
- Modify: `internal/shell/popout_controlcenter.go`
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Consumes: `ccSections`, `PanelHost.section`, the `"section:"` action prefix that
  `panelhost.go:1309` already handles.
- Produces: `ccRail(h) *ui.Node`, `ccHeader(h) *ui.Node`, `ccPage(r, h) *ui.Node`.

- [ ] **Step 1: Build the rail**

56 px column, 40 px items, a grouping step before Media, Network and Weather so
ten identical squares do not read as one undifferentiated column. Disabled entries
are **not** given a bare tooltip: the reason goes in the node's accessible `Name`,
because a tooltip is unreachable by keyboard and touch.

```go
func ccRail(h *PanelHost) *ui.Node {
	col := &ui.Node{Kind: ui.KindColumn, Width: 56, Gap: 8}
	for _, s := range ccSections {
		if s.ID == "media" || s.ID == "network" || s.ID == "weather" {
			col.Children = append(col.Children, &ui.Node{Kind: ui.KindSpacer, Height: 8})
		}
		name := s.Label
		if !s.Enabled {
			name = s.Label + " — not available yet"
		}
		col.Children = append(col.Children, &ui.Node{
			Kind: ui.KindButton, Text: s.Icon, Name: name, Role: "tab",
			Action:    ccSectionAction(s),
			Focusable: s.Enabled,
			Selected:  h.section == s.ID,
			Disabled:  !s.Enabled,
		})
	}
	return col
}

// ccSectionAction is empty for a disabled destination, so activating one is
// inert rather than an error the user has to read.
func ccSectionAction(s ccSection) string {
	if !s.Enabled {
		return ""
	}
	return "section:" + s.ID
}
```

Use whichever field names `ui.Node` actually carries for selected/disabled state —
follow `settingsEntryRow` and `wallpaperSegment`. Add no new node kinds.

- [ ] **Step 2: Build the header and the page dispatch**

Header is 40 tall: the active section's label, then settings, power and close
round buttons routed through the existing panel-open path.

```go
func ccPage(r *Registry, h *PanelHost) *ui.Node {
	switch h.section {
	case "audio":
		return ccAudio(r, h)
	case "monitor":
		return ccMonitor(r, h)
	case "power":
		return ccPower(r, h)
	case "weather":
		return ccWeather(r, h)
	case "calendar":
		return ccCalendar(r, h)
	case "notifications":
		return ccNotifications(r, h)
	default:
		return ccHome(r, h)
	}
}
```

Stub the six non-Home builders to return an empty column for now; Tasks 9–11 fill
them. `controlCentreTree` becomes the row of `ccRail` beside a column of
`ccHeader` over `ccPage`.

- [ ] **Step 3: Add the focused check**

```go
func TestRailOrderAndDisabledEntries(t *testing.T) {
	h := &PanelHost{section: "home"}
	rail := ccRail(h)
	var labels []string
	for _, n := range rail.Children {
		if n.Kind == ui.KindButton {
			labels = append(labels, n.Name)
		}
	}
	if len(labels) != 10 {
		t.Fatalf("rail has %d entries, want 10", len(labels))
	}
	if labels[0] != "Home" {
		t.Errorf("first entry = %q, want Home", labels[0])
	}
	// The reason a destination is inert must be in the accessible name: a
	// tooltip is unreachable by keyboard and touch.
	if labels[1] != "Media — not available yet" {
		t.Errorf("disabled entry name = %q, want the reason in the name", labels[1])
	}
}
```

- [ ] **Step 4: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): compose the control centre rail and page dispatch"
```

---

### Task 7: The off-owner control seam

**Files:**
- Modify: `internal/shell/registry.go`
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Consumes: the `scheduleLoadProfiles` pattern in `popout_session.go:217`.
- Produces: `(*Registry).scheduleControl(h *PanelHost, run func() error)`.

- [ ] **Step 1: Add the seam**

A panel handler holds `Registry.mu`, so it cannot call `stepAudio` the way IPC
does — that would run `wpctl` under the lock. Everything command-backed goes
through here:

```go
// scheduleControl runs one bounded command off the Wayland owner and without
// r.mu, then reapplies under the lock. Caller holds r.mu. The host check is the
// staleness guard: the panel may have closed while the command ran.
func (r *Registry) scheduleControl(h *PanelHost, run func() error) {
	go func() {
		err := run()
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.panelHosts[PanelControlCenter] != h {
			return
		}
		if err != nil {
			h.errLabel = err.Error()
		} else {
			h.errLabel = ""
		}
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(h.id))
	}()
}
```

- [ ] **Step 2: Route the control actions**

In the control centre's action handling: `cc:volume` and `cc:mute` through
`r.audio`, `cc:brightness` through `r.brightness`, `cc:profile:<name>` through
`powerProfileSetArgv`, session actions through the existing argv path — each
wrapped in `scheduleControl`. **DND is the exception**: `r.setDND` is pure memory,
so it stays synchronous.

- [ ] **Step 3: Add the focused check**

```go
func TestScheduleControlSurvivesAClosedPanel(t *testing.T) {
	// The package has no registry fixture; tests build one directly, as
	// popout_plugins_test.go does. A nil panelHosts map makes the staleness
	// guard fire before rebuildPanel is reached, which is the path under test.
	r := &Registry{}
	h := &PanelHost{id: PanelControlCenter}
	done := make(chan struct{})
	r.mu.Lock()
	r.scheduleControl(h, func() error { close(done); return errors.New("boom") })
	r.mu.Unlock()
	<-done
	// h was never registered as the live host, so the result must be dropped
	// rather than applied to a panel that is gone.
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.errLabel != "" {
		t.Errorf("errLabel = %q, want empty: the host was stale", h.errLabel)
	}
}
```

- [ ] **Step 4: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): run control centre commands off the Wayland owner"
```

---

### Task 8: Caffeine — the idle inhibit

**Files:**
- Modify: `internal/shell/registry.go` (the hook, the field, `Close`)
- Modify: `internal/shell/popout_session.go` (`runArgvDefault` allowlist)
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Produces: `(*Registry).setCaffeine(h *PanelHost, on bool)`; `Registry.inhibit`
  holding the live hold; `Registry.startInhibit func() (io.Closer, error)` so
  tests stub it the way `runArgv` is stubbed.

- [ ] **Step 1: Add the hook and the field**

`runArgv` returns only an error, so it cannot hold a killable child. Give the
inhibit its own hook, held on the Registry for the same reason `runArgv` is a
field rather than a package variable — parallel tests each replace their own:

```go
// startInhibitDefault holds an idle inhibit for as long as the returned Closer
// is open. systemd-inhibit holds the lock while its child runs, so the child is
// the hold and closing it releases.
func startInhibitDefault() (io.Closer, error) {
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, "--what=idle:sleep", "--who=sysc-shell",
		"--why=Caffeine", "--mode=block", "sleep", "infinity")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &processHold{cmd: cmd}, nil
}

type processHold struct{ cmd *exec.Cmd }

func (p *processHold) Close() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	err := p.cmd.Process.Kill()
	_ = p.cmd.Wait() // reap, so the hold does not linger as a zombie
	return err
}
```

- [ ] **Step 2: Toggle it through the off-owner seam**

```go
func (r *Registry) setCaffeine(h *PanelHost, on bool) {
	if on == (r.inhibit != nil) {
		return
	}
	if !on {
		hold := r.inhibit
		r.inhibit = nil
		r.scheduleControl(h, func() error { return hold.Close() })
		return
	}
	start := r.startInhibit
	r.scheduleControl(h, func() error {
		hold, err := start()
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.inhibit = hold
		r.mu.Unlock()
		return nil
	})
}
```

- [ ] **Step 3: Release it on shutdown**

In `Registry.Close`, beside the lease release, close any live hold. An inhibit
that outlives the shell is the orphan `sysc-140` records for
`gpu-screen-recorder`; do not repeat it.

- [ ] **Step 4: Allow the binary**

Add `systemd-inhibit` to the timeout-free branch of `runArgvDefault` only if the
argv path is used at all; the hook above bypasses `runArgv`, so prefer leaving the
allowlist untouched and note that in the commit body.

- [ ] **Step 5: Add the focused check**

```go
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestCaffeineHoldIsReleasedOnClose(t *testing.T) {
	// Assert the invariant that matters without racing the goroutine: a hold
	// that is live when the shell stops must be released by Close.
	closed := false
	r := &Registry{inhibit: closerFunc(func() error { closed = true; return nil })}

	r.Close()

	if !closed {
		t.Error("the idle inhibit outlived the shell: that is the sysc-140 orphan")
	}
}
```

- [ ] **Step 6: Check the toggle path too**

```go
func TestCaffeineTogglesThroughTheStubbedHook(t *testing.T) {
	r := &Registry{startInhibit: func() (io.Closer, error) {
		return closerFunc(func() error { return nil }), nil
	}}
	if r.startInhibit == nil {
		t.Fatal("startInhibit must be a field so parallel tests each replace their own")
	}
	hold, err := r.startInhibit()
	if err != nil || hold == nil {
		t.Fatalf("startInhibit() = %v, %v", hold, err)
	}
}
- [ ] **Step 7: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): hold an idle inhibit for caffeine and drop it on shutdown"
```

---

### Task 9: Home

**Files:**
- Create: `internal/shell/controlcenter_pages.go`
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Consumes: `services.Snapshot`, `r.audio.State()`, `r.brightness.State()`,
  `r.notify`, `readMachineFacts`, `services.ReadUptime`, `scheduleControl`.
- Produces: `ccHome(r *Registry, h *PanelHost) *ui.Node`.

- [ ] **Step 1: Compose Home**

Top to bottom, at the contract's sizes: identity card 96; toggle pill 48 holding
Caffeine and Wallpaper in one capsule; a 184 split of a clock/date/weather card
over a sysmon card on the left (356) beside a 2 × 2 grid on the right (228); then
a 116 sliders block. Total 480, which is the whole body — Home never scrolls.

Three rules the design locks:

- The identity card shows name, `user@host` and uptime. **No version string** —
  none exists in the tree, and inventing one is fake data.
- The battery cell is an **outlined readout, not a toggle**: a status that cannot
  be pressed must not look pressable.
- A value with no sample yet renders as a dash, never a zero. `0%` CPU is a lie.

- [ ] **Step 2: Add the focused check**

```go
func TestHomeShowsDashesBeforeTheFirstSample(t *testing.T) {
	// A zero Registry is the "nothing sampled, nothing available" case, which
	// is exactly the state D11's stale treatment has to render.
	r := &Registry{}
	h := &PanelHost{id: PanelControlCenter, section: "home"}
	got := renderText(ccHome(r, h)) // walk the tree, join every KindText
	if strings.Contains(got, "0%") {
		t.Error("Home rendered 0% before a sample landed: stale must read as a dash")
	}
	if !strings.Contains(got, "—") {
		t.Error("Home rendered no dash for an unsampled value")
	}
}
```

Write `renderText` as a small recursive helper in the test file if the package has
no equivalent.

- [ ] **Step 3: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): build the control centre home page"
```

---

### Task 10: Weather — daily plumbing and the page

**Files:**
- Modify: `internal/services/weather.go` (`Reading`, `requestURLLocked` at `:223`,
  `fetch` at `:311`)
- Modify: `internal/shell/controlcenter_pages.go` (`ccWeather`)
- Test: `internal/services/weather_test.go`

**Interfaces:**
- Consumes: `weather.Query.Daily`, `weather.Forecast.Daily`, `weather.Day` — all
  already shipped in `weather/model.go` and `weather/client.go:21`.
- Produces: `services.Reading.Daily []Day` where `type Day = weather.Day`.

- [ ] **Step 1: Ask for the daily block**

The wire layer already emits
`daily=weather_code,temperature_2m_max,temperature_2m_min,sunrise,sunset`,
`forecast_days=7` and `timezone=auto` when `Query.Daily` is set. The service
simply never asks. Set `Daily: true` at **both** construction sites — `:223` and
`:311` — or the URL the panel reports and the URL it fetches disagree.

- [ ] **Step 2: Carry the days onto the reading**

```go
// Day is one forecast day. Aliased from the wire package the way Unit is, so the
// shell does not restate a shape the decoder already produces.
type Day = weather.Day

type Reading struct {
	// ... existing fields ...
	Daily []Day
}
```

Map `forecast.Daily` into the reading in `fetch`.

- [ ] **Step 3: Compose the page**

Today at the top — condition glyph from the WMO code, temperature, condition
text, location and fetch time — growing to fill, then a four-column Tue–Fri strip
at 132. Map WMO codes to the six glyphs Task 2 added; anything unmapped falls back
to `cloud`. A reading with no `Daily` renders the strip's cells as dashes rather
than omitting the strip, so the page does not reflow when the forecast arrives.

- [ ] **Step 4: Add the focused check**

```go
func TestRequestAsksForTheDailyBlock(t *testing.T) {
	w := NewWeather( /* the constructor the package already uses */ )
	got := w.RequestURL()
	for _, want := range []string{"daily=", "forecast_days=", "timezone=auto"} {
		if !strings.Contains(got, want) {
			t.Errorf("RequestURL() = %q, missing %q: the page cannot draw a week without it", got, want)
		}
	}
}
```

- [ ] **Step 5: Run and commit**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/services ./weather
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/services internal/shell
git commit -m "feat(services): carry the daily forecast the wire layer already decodes"
```

---

### Task 11: Audio, Monitor, Power, Calendar and Notifications

**Files:**
- Modify: `internal/shell/controlcenter_pages.go`
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Produces: `ccAudio`, `ccMonitor`, `ccPower`, `ccCalendar`, `ccNotifications`,
  each `func(r *Registry, h *PanelHost) *ui.Node`.

- [ ] **Step 1: Compose the five pages**

- **Audio** — volume slider and mute. **No output-device picker**: `AudioState`
  carries `Level` and `Muted` only, and nothing is drawn the service cannot report.
- **Monitor** — CPU, memory and network cards with sparklines that grow to fill.
- **Power** — battery detail, a profile segmented control, and session actions as
  a **list**, not a third grid of icon squares.
- **Calendar** — month grid paged by `h.monthDelta`, which `panelhost.go` already
  tracks.
- **Notifications** — DND controls over history, with the empty state filling.

Every page shorter than Home grows its principal block, so no page shows a dead
band below its content.

- [ ] **Step 2: Add the focused check**

```go
func TestAudioPageHasNoDevicePicker(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelControlCenter, section: "audio"}
	got := renderText(ccAudio(r, h))
	for _, banned := range []string{"Output device", "Sink", "Device"} {
		if strings.Contains(got, banned) {
			t.Errorf("audio page drew %q: the service reports level and mute only", banned)
		}
	}
}
```

- [ ] **Step 3: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): build the remaining control centre pages"
```

---

### Task 12: Focus, scrolling, Escape and the reveal

**Files:**
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/popout_controlcenter.go`
- Test: `internal/shell/controlcenter_test.go`

**Interfaces:**
- Consumes: `ui.Focusables`, `ui.Roving`, `scrollAt`, `animator`.

- [ ] **Step 1: Confirm focus needs no new model**

One flat roving ring already covers rail then body in tree order. Because the rail
is first and never changes length, the roving index is stable across page swaps —
preserving it leaves focus on the rail entry just activated. Add no second
traversal. Escape closes the panel through `closePanelLocked`, as every other
panel does; add no back-to-Home step.

- [ ] **Step 2: Animate the reveal and the wings**

Only the active page is in the tree, so hidden pages leave hit testing and focus
order by construction. The incoming page animates opacity and a small offset whose
direction follows the rail index delta. Drive the **bulge from the same reveal
progress**: 0 while the panel's top edge is still behind the bar, ramping to the
panel radius as it finishes emerging, and pass it to `AttachedMask`. Reduced
motion settles at once, at full bulge.

- [ ] **Step 3: Add the focused check**

```go
func TestRailIndexIsStableAcrossPageSwaps(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelControlCenter, section: "home"}
	h.root = controlCentreTree(r, h)
	before := len(ui.Focusables(h.root))

	h.section = "power"
	h.root = controlCentreTree(r, h)
	after := ui.Focusables(h.root)

	// The rail is first in tree order and fixed in length, so a page swap must
	// not renumber the rail's own focus slots.
	if len(after) < before-before { // guard against an empty tree
		t.Fatal("focus ring collapsed")
	}
	if after[0].Name != "Home" {
		t.Errorf("first focusable = %q, want the rail's Home entry", after[0].Name)
	}
}
```

- [ ] **Step 4: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell ./internal/ui
git add internal/shell
git commit -m "feat(shell): reveal the control centre and settle its focus order"
```

---

### Task 13: Gates

**Files:** none changed unless a gate fails.

- [ ] **Step 1: Automated gate**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
  ./internal/shell ./internal/ui ./internal/ipc ./internal/services ./internal/render ./weather
```

Expected: every package `ok`. Never add `-race` and never widen this to `./...`.

- [ ] **Step 2: Redeploy**

```bash
go build -o /tmp/sysc-shell.new ./cmd/sysc-shell
mv /tmp/sysc-shell.new ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service
systemctl --user status sysc-shell.service --no-pager
```

Expected: `active (running)` **and** a painted bar. Active with nothing painted is
the recovered-panic failure: check the journal for `dispatch: panic handling`.

- [ ] **Step 3: Live Niri matrix**

`NIRI_SOCKET` is unset here; derive it rather than assuming. Record each result in
`sysc-154`:

1. The bar trigger opens the panel on the focused output, centred on the bar and
   flush with it, with the wing-tips fused to the bar.
2. `panel.open` with no `section` opens Home; with `section` opens that page; with
   an unknown or disabled section returns an error and changes nothing.
3. Rail navigation reaches all seven functional pages.
4. Media, Network and Bluetooth are visible, inert and not focusable, and their
   reason is readable without hovering.
5. Volume, brightness, DND, caffeine and power profile change real system state.
   Confirm the inhibit with `systemd-inhibit --list`.
6. Caffeine on, then `systemctl --user restart sysc-shell.service`: the hold is
   gone from `systemd-inhibit --list`. No orphan.
7. Weather shows today plus four days; with no location set it shows the dash
   treatment, not an empty page.
8. Escape closes. Tab walks rail then body. Reduced motion settles at once.
9. No page shows a dead band below its content.

- [ ] **Step 4: Close out**

```bash
bd close sysc-154 --reason "Control centre shipped: <commit range>. Live matrix passed."
git add .beads/issues.jsonl
git commit -m "chore(beads): close the control centre spine"
```

---

## Stop condition

This plan is complete when the automated gate passes, the live matrix above is
recorded against `sysc-154`, and `sysc-154` is closed with the JSONL committed.

**Explicitly out of scope — do not start these from this plan:**

- NetworkManager (`sysc-157`), BlueZ (`sysc-155`), MPRIS (`sysc-156`). Each needs
  its own approved design.
- Rendering qualification (M8). It needs a measured failing case against `wl_shm`
  first, and none has been recorded.
- The `setSessionProfile` lock violation in `popout_session.go:243` — pre-existing,
  filed separately, and not to be fixed inside a control-centre commit.
- Concave corners on the bar's own screen-edge ends.

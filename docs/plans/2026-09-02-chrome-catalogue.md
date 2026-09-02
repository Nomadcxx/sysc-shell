# Chrome Catalogue Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the shell's sharp always-Primary buttons with reusable native pills, semantic states, Material chrome icons, surface-local transitions, and a corrected 420 px session/power layout.

**Architecture:** Extend the existing retained `ui.Node`, layout, renderer, shell input hosts, and `internal/theme` pipeline. Shell hosts own resolved interaction and animation state; the painter consumes immutable semantic state. Keep one animation clock per active surface and preserve the single Wayland-owner dispatch path.

**Tech Stack:** Go 1.25, `go-text/typesetting`, the existing pure-Go canvas and Wayland runtime, matugen JSON tokens, Python FontTools at author time, Material Symbols Rounded under Apache-2.0.

---

## Scope and invariants

Read `docs/plans/2026-09-02-chrome-catalogue-design.md` before editing. Work in a dedicated implementation worktree, run `bd` only from `/home/nomadx/sysc-shell`, and claim `sysc-141`. Do not implement on `main`.

The owner directed implementation-first development for this tranche. For each
slice, trace the owning behavior, implement the smallest coherent change, add a
focused regression check for non-trivial logic, and run the affected packages
before committing. Do not manufacture pre-implementation failures. Commit
`.beads/issues.jsonl` with the final code state.

Preserve these invariants throughout:

- no new module, CGO, QML, runtime SVG, scene graph, or second theme engine;
- UI and animation state remain under the existing shell locks;
- Wayland dispatch remains on its one owner goroutine;
- pointer movement invalidates only when the resolved hit target changes;
- frame requests stop when transitions settle and remain frame-callback/buffer-release driven;
- right-click battery, inert left-click, `power` alias, Super+X, session argv, optional-tool hiding, focus order, and close behavior do not change;
- full theme parity is `sysc-142`; this plan adds only roles consumed by shipped chrome.

## Task 0: Confirm the regression surface

Run:

```bash
rg -n "KindButton" internal/shell --glob '*.go'
go test ./...
```

Expected: the baseline passes, and every construction site maps to this table. If the branch has gained a site, add it to the table and classify it before proceeding.

| Construction site | Treatment in this slice |
|---|---|
| `internal/shell/popout_session.go` profiles/actions | Explicit segmented/action layout; first consumer |
| `internal/shell/popout_clock.go` month arrows | Inherit compact circular paint; replace ASCII with Material chevrons |
| `internal/shell/panelhost.go` shared panel button creation | Inherit painter; preserve focus and activation |
| `internal/shell/popout_settings.go` navigation | Inherit painter; adjust only if focused layout test clips |
| `internal/shell/popout_launcher.go` result row | Inherit painter; retain existing 12 px padding |
| `internal/shell/popout_notifications.go` header actions | Inherit painter; use named notification/DND icons only where already semantically present |
| `internal/shell/notifycard.go` notification actions | Inherit painter; retain action labels |
| `internal/shell/traymenuhost.go` menu rows | Inherit painter; retain row sizing |
| `internal/shell/traydrawer.go` drawer rows | Inherit painter; retain row sizing |
| `internal/shell/bar.go` overflow button | Inherit compact pill paint and bar interaction state |
| `internal/shell/popout_plugins.go` rescan/retry | Inherit painter; retain existing row sizing |
| `internal/shell/pluginhost.go` failure actions | Inherit painter; retain Close/Retry/Disable labels |
| `internal/shell/pluginwidget.go` status mark | Inherit compact pill paint and bar interaction state |

Do not commit from Task 0 unless a new construction site required a plan correction.

The last three rows were added during Task 0: plugin-host work reached `main`
after this plan was written. Each is a plain text button carrying
`Action`/`Name`/`Role`/`Focusable`, structurally identical to the settings and
notification rows above, so each inherits the global painter without a
site-specific change. `pluginwidget.go` is a bar widget and takes bar
interaction state rather than panel state. Stop and update the plan if a site cannot safely inherit the global painter.

## Task 1: Add the semantic chrome theme roles

**Files:**

- Modify: `internal/theme/theme.go`
- Modify: `internal/theme/generate.go`
- Modify: `internal/theme/matugen/tpl.json`
- Modify: `internal/theme/theme_test.go`
- Modify: `internal/shell/theme.go`
- Modify: `internal/shell/theme_test.go`

**Step 1: Implement the token mapping**

Add these fields to `Tokens`, its fallback, generator, and matugen template:

```go
SurfaceContainerHigh    string
SurfaceContainerHighest string
OutlineVariant          string
```

Add only the generated fields above. Extend `shell.Theme` with semantic colour pairs and the catalogue metrics it consumes:

```go
Surface, OnSurface             Color
SurfaceContainer, OnContainer  Color
SurfaceContainerHigh           Color
SurfaceContainerHighest        Color
Primary, OnPrimary             Color
Outline, OutlineVariant        Color
Error, OnError                 Color
ControlHeight, CompactHeight   int
ButtonPadding, IconSize        int
CardRadius                     int
```

Keep existing field names temporarily where removing them would widen this task; map them from the new semantic roles in one place. Add no configuration keys. Use the design defaults: 40/32 px heights, 12 px default horizontal padding, 20 px chrome icon, and current configured radius for cards.

**Step 2: Add focused token tests**

Extend the token fixtures and assertions so generated light/dark tokens reach
the new fields, every fallback field parses, and the semantic pairs meet the
design's text/icon/boundary contrast gates. Add a focused test showing a panel
card resolves `SurfaceContainerHigh`, not `SurfaceContainer`.

**Step 3: Verify the slice**

Run:

```bash
go test ./internal/theme ./internal/shell -run 'Token|Theme|Contrast|SurfaceContainerHigh'
go test ./internal/theme ./internal/shell
```

**Step 4: Commit**

```bash
git add internal/theme internal/shell/theme.go internal/shell/theme_test.go
git commit -m "feat(theme): add semantic chrome roles"
```

## Task 2: Extend the retained UI primitives

**Files:**

- Modify: `internal/ui/tree.go`
- Modify: `internal/ui/layout.go`
- Modify: `internal/ui/column.go`
- Modify: `internal/ui/layout_test.go`
- Modify: `internal/ui/column_test.go`
- Modify: `internal/ui/kindcoverage_test.go`

**Step 1: Implement the retained-node contracts**

Add tests for these public contracts:

```go
const (
    KindIcon Kind = ...
    KindSegmented Kind = ...
)

type Interaction uint8
const (
    StateHovered Interaction = 1 << iota
    StatePressed
    StateSelected
    StateDisabled
)
```

Extend `Node` with `Key`, `Icon`, `State`, and button children. Keep `Text` as
shorthand when `Children` is empty. When children are present, lay them out as
one centered row. `KindSegmented` owns equal allocation, validates one selected
child at most, and gives its children the parent height. Add no general flex API.

Use `Action` as the default key only when non-empty; otherwise require an
explicit `Key` for animated nodes. Disabled nodes remain measurable and
paintable but are excluded by existing focus/hit traversal in the later host
task.

**Step 2: Add focused layout tests**

Tests must show:

- existing text-only buttons still measure using `Text` shorthand;
- a 40 px icon+text button lays out icon, gap, and label inside padding;
- a compact icon-only button is 32×32;
- three segmented children receive equal stable widths and the `Balanced` label fits inside the 420 px session content width;
- duplicate or empty animated keys return a useful layout/validation error;
- every new kind remains covered by the kind-coverage test.

Use existing test measure functions; do not introduce a fixture framework.

**Step 3: Verify the slice**

```bash
go test ./internal/ui -run 'Button|Icon|Segmented|KindCoverage'
go test ./internal/ui
```

**Step 4: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): add chrome content and segments"
```

## Task 3: Add pure transition values

**Files:**

- Create: `internal/ui/transition.go`
- Create: `internal/ui/transition_test.go`
- Modify: `internal/render/canvas.go`
- Create: `internal/render/canvas_test.go`

**Step 1: Implement the pure helpers**

The geometry API is deliberately small:

```go
func LerpRect(from, to Rect, progress float64) Rect
func EaseOutCubic(progress float64) float64
func EaseOutQuart(progress float64) float64
```

Clamp once at the responsible helper. Keep timing and clocks out of `internal/ui`; this task only supplies deterministic values. Put colour interpolation beside the existing `render.Color` type.

**Step 2: Add focused value tests**

Table tests cover negative/over-one progress, exact endpoints, midpoint results,
reverse-from-current behavior, and integer rounding. Add a render colour
interpolation test that includes alpha.

**Step 3: Verify the slice**

```bash
go test ./internal/ui ./internal/render -run 'Lerp|Ease|Colour'
go test ./internal/ui ./internal/render
```

**Step 4: Commit and checkpoint**

```bash
git add internal/ui/transition.go internal/ui/transition_test.go internal/render/canvas.go internal/render/canvas_test.go
git commit -m "feat(ui): add native transition values"
```

Checkpoint: rerun `go test ./internal/theme ./internal/ui ./internal/render ./internal/shell` before beginning painter work.

## Task 4: Paint semantic buttons and cards

**Files:**

- Modify: `internal/render/paint.go`
- Modify: `internal/render/paint_test.go`
- Modify: `internal/render/canvas.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/toasthost.go`
- Modify: `internal/shell/traydrawer.go`
- Modify: `internal/shell/traymenuhost.go`

**Step 1: Implement semantic paint**

Replace the `KindButton` special-case fill with one rounded path shared with
capsules. Resolve fill and paired foreground once, composite the state layer
over that resolved fill, and recurse into children. Add the minimum outline
helper to the canvas; do not add shadows or a general stroke engine.

Pass every semantic colour from `shell.Theme` through existing `ProofStyle`
construction sites. Keep compatibility aliases only where needed to avoid
unrelated rewrites, and delete them once all construction sites use the
semantic names.

**Step 2: Add focused pixel tests**

Using the existing `paintTree`/colour-at-point style, prove:

- an idle default button is a 40 px stadium with transparent corners and `SurfaceContainerHighest` center;
- `FillOutline` paints the parent fill plus a 3:1 outline, not Primary;
- selected uses `Primary`/`OnPrimary`;
- destructive outlined uses `Error`/`OnError` state layers;
- hover overlays paired foreground at 8%; pressed at 12%; disabled is lower emphasis;
- focus paints the existing 2 px rounded ring independently;
- `KindCapsule` and a panel card both use `SurfaceContainerHigh` (see D4); a
  card is a capsule with an explicit theme radius, while a button clamps to a
  stadium;
- an icon+text button paints children after its state layer.

**Step 3: Verify the slice**

```bash
go test ./internal/render -run 'Button|Capsule|Outline|StateLayer|Focus'
go test ./internal/render ./internal/shell
```

**Step 4: Commit**

```bash
git add internal/render internal/shell/bar.go internal/shell/panelhost.go internal/shell/toasthost.go internal/shell/traydrawer.go internal/shell/traymenuhost.go
git commit -m "feat(render): paint semantic pill chrome"
```

## Task 5: Build and embed the Material Symbols subset

**Files:**

- Create: `internal/render/icons/material/build.py`
- Create: `internal/render/icons/material/LICENSE`
- Create: `internal/render/icons/material/SOURCE.md`
- Create: `internal/render/icons/material/material-symbols-rounded.ttf`
- Create: `internal/render/materialfont.go`
- Create: `internal/render/materialfont_test.go`
- Modify: `internal/render/text.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/render/paint_test.go`

**Step 1: Write the author-time builder**

Pin source commit `84ccef280841abfac506afc4ad4a2782f6d0a1d0` and verify source SHA-256 `c4416e02739ed6865e3218c19dcd62c5a88fb97b8bcc445f24ae8017d11cc2d0` before processing. Instantiate axes to `FILL=1`, `wght=400`, `GRAD=0`, `opsz=24`, subset the exact ligature inventory while retaining the required GSUB ligature feature, and emit a deterministic static TTF. `SOURCE.md` records URL, commit, hashes, axes, command, inventory, and Apache-2.0 provenance.

Run:

```bash
python3 internal/render/icons/material/build.py /path/to/pinned/MaterialSymbolsRounded.ttf
```

Expected: the committed font is far smaller than the 14.6 MB source and a
second run produces the same SHA-256.

**Step 2: Embed and paint**

Cache the parsed read-only font but give each `TextRenderer` its own face,
matching the existing face-safety rule. Add one name-validation function used
by `KindIcon`; do not route ligature text through the system font map.

**Step 3: Add focused inventory tests**

Table-test exactly these names:

```text
lock logout bedtime restart_alt power_settings_new
speed balance energy_savings_leaf check
close chevron_left chevron_right
search settings notifications do_not_disturb_on
volume_up volume_off brightness_high
```

The test must reject an unknown name, prove each approved name shapes to nonzero coverage at 18/20/24 px, and prove `KindIcon` uses the embedded Material face rather than the primary text face or a system fallback.

**Step 4: Verify the slice**

```bash
go test ./internal/render -run 'Material|KindIcon'
python3 internal/render/icons/material/build.py /path/to/pinned/MaterialSymbolsRounded.ttf
sha256sum internal/render/icons/material/material-symbols-rounded.ttf
git diff --exit-code -- internal/render/icons/material/material-symbols-rounded.ttf
go test ./internal/render
```

Expected: all tests pass and rebuilding leaves the committed TTF unchanged.

**Step 5: Commit**

```bash
git add internal/render/icons/material internal/render/materialfont.go internal/render/materialfont_test.go internal/render/text.go internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(render): embed material chrome icons"
```

## Task 6: Add one surface-local animation clock

**Files:**

- Create: `internal/shell/animation.go`
- Create: `internal/shell/animation_test.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/panelhost_test.go`
- Modify: `internal/shell/osd.go`
- Modify: `internal/shell/osd_test.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/ui/tree.go`

**Step 1: Implement the surface clock**

Keep the animator concrete and package-private. It is keyed by stable node key
and returns resolved visual values for the tree. Store no renderer objects in
it. Replace `revealLoop` with the clock's frame scheduling and reuse the same
scheduling path for OSD visibility.

Panel enter begins 8 logical px from its anchored edge at zero opacity. Press
scales the resolved visual rect/content to 0.98 without changing layout bounds.
Do not schedule a ticker when all targets are settled.

**Step 2: Add focused clock tests**

With a fake `now` function, prove one animator per surface:

- uses 80/120 ms press, 120 ms hover, 180 ms selection, 200/150 ms panel visibility;
- uses out-cubic except out-quart selection;
- carries current colour, opacity, translation, scale, bounds, and radius into a reversal;
- stops requesting frames when settled;
- snaps hover/press/selection under reduced motion;
- uses opacity-only panel visibility of no more than 150 ms under reduced motion;
- replaces the existing independent panel reveal ticker and does not add an OSD timer when one clock already owns that surface.

**Step 3: Verify the slice**

```bash
go test ./internal/shell -run 'Anim|Reveal|ReducedMotion|Reverse|Settled'
go test ./internal/shell ./internal/render ./internal/ui
```

**Step 4: Commit and checkpoint**

```bash
git add internal/shell/animation.go internal/shell/animation_test.go internal/shell/panelhost.go internal/shell/panelhost_test.go internal/shell/osd.go internal/shell/osd_test.go internal/render/paint.go internal/ui/tree.go
git commit -m "feat(shell): unify surface transitions"
```

Checkpoint: run `go test -race -count=1 ./internal/ui ./internal/render ./internal/shell` and inspect the diff for any second clock or off-owner state mutation.

## Task 7: Wire hover, press, and disabled state

**Files:**

- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/panelhost_test.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/bar_test.go`
- Modify: `internal/shell/traydrawer.go`
- Modify: `internal/shell/traydrawer_test.go`
- Modify: `internal/shell/traymenuhost.go`
- Modify: `internal/shell/traymenuhost_test.go`
- Modify: `internal/shell/notifyactions.go`
- Modify: `internal/shell/notifyactions_test.go`

**Step 1: Implement host state resolution**

Use existing hit testing and host locks. Store stable keys rather than stale
node pointers across rebuilds. Resolve the state mask onto the current tree
before paint and submit a surface invalidation only when the key/state target
changes. Keep activation semantics unchanged.

**Step 2: Add focused host tests**

For each host family, prove entering a new clickable node sets hover and
invalidates once, motion within the same node does not invalidate, leaving
clears hover/press, press sets state, release clears it, and disabled nodes
neither focus nor activate. Add reduced-motion assertions for settled state
without animation frames. Bar tests must prove only clickable capsules animate;
CPU/memory display groups do not.

**Step 3: Verify the slice**

```bash
go test ./internal/shell -run 'Hover|Press|Disabled|PointerMotion|Invalidat'
go test -race -count=1 ./internal/shell
```

**Step 4: Commit**

```bash
git add internal/shell
git commit -m "feat(shell): expose chrome interaction states"
```

## Task 8: Build the segmented session panel consumer

**Files:**

- Modify: `internal/shell/popout_session.go`
- Modify: `internal/shell/popout_session_test.go`
- Modify: `internal/shell/popout_monitor.go`
- Modify: `internal/shell/popout_monitor_test.go`
- Modify: `internal/ui/layout_test.go`

**Step 1: Implement the session composition**

Use the theme metrics and kinds already added. Do not add session-only paint
code. Preserve the 420 px panel and existing cards. Use 18 px profile icons,
20 px action icons, and the exact names from D10. Keep profile labels visible
even when the selected check replaces an icon.

**Step 2: Add focused session tests**

At the real 420 px panel width, assert:

- Battery, Power profile, and Session remain distinct high-container cards;
- battery percentage reserves the width of `100%`;
- the profile row is `KindSegmented`, all available labels fit, exactly the active child is selected, and selected check/icon behavior is deterministic;
- the profile card is absent when `powerprofilesctl` is unavailable;
- action buttons are full-width 40 px icon+text stadiums;
- Lock is absent without a locker;
- Reboot and Power off are error-toned outlines, not solid red;
- session action IDs and argv remain byte-for-byte unchanged.

**Step 3: Verify the slice**

```bash
go test ./internal/shell ./internal/ui -run 'Session|Profile|Balanced|Battery|Action'
go test ./internal/shell ./internal/ui ./internal/render
```

**Step 4: Commit**

```bash
git add internal/shell/popout_session.go internal/shell/popout_session_test.go internal/shell/popout_monitor.go internal/shell/popout_monitor_test.go internal/ui/layout_test.go
git commit -m "feat(shell): compose session chrome"
```

## Task 9: Apply the finite inherited icon and padding corrections

**Files:**

- Modify: `internal/shell/popout_clock.go`
- Modify: `internal/shell/popout_clock_test.go`
- Modify as proved necessary: `internal/shell/bar.go`
- Modify as proved necessary: `internal/shell/popout_settings.go`
- Modify as proved necessary: `internal/shell/popout_launcher.go`
- Modify as proved necessary: `internal/shell/popout_notifications.go`
- Modify as proved necessary: `internal/shell/notifycard.go`
- Modify as proved necessary: `internal/shell/traymenuhost.go`
- Modify as proved necessary: `internal/shell/traydrawer.go`
- Test alongside each changed consumer in its existing `_test.go`

**Step 1: Apply the finite corrections**

Replace calendar ASCII arrows with `chevron_left` and `chevron_right` compact
icon buttons. For every other Task 0 site, run its existing layout path at the
narrowest supported width. Apply Material icons already in the approved
inventory and adjust padding only when inspection or an existing check shows
clipping, lost accessible naming, or a changed hit target.

Do not redesign settings, launcher, notifications, or tray trees. Do not add an
icon to a control merely because one exists in the subset.

**Step 2: Add focused regression tests**

Update calendar expectations. For any other adjusted consumer, add an assertion
that captures the observed clipping, naming, or hit-target regression. Do not
add tests or edits for consumers that already fit.

**Step 3: Verify the slice**

```bash
go test ./internal/shell -run 'Clock|Settings|Launcher|Notif|Tray|Overflow'
go test ./internal/shell
```

**Step 4: Commit and checkpoint**

```bash
git add internal/shell
git commit -m "fix(shell): fit inherited pill controls"
```

Checkpoint: inspect `git show --stat` and remove any consumer edit that is not
backed by an observed regression and a focused check.

## Task 10: Crossfade valid theme changes

**Files:**

- Modify: `internal/shell/registry.go`
- Modify: `internal/shell/registry_test.go`
- Modify: `internal/shell/animation.go`
- Modify: `internal/shell/animation_test.go`
- Modify: `internal/shell/theme_test.go`

**Step 1: Implement validated theme transitions**

Validate one complete theme before publishing it. Retarget colour transitions
on each active surface animator. Do not introduce settings or a theme service.

**Step 2: Add focused reload tests**

Prove a valid generated theme change retargets the current surface colours from
their rendered values, reduced motion snaps to the new colours, and invalid
tokens retain the previous complete semantic theme. A failed reload must not
leave a mix of old and new roles.

**Step 3: Verify the slice**

```bash
go test ./internal/shell -run 'Theme.*Reload|Palette.*Transition|InvalidTheme'
go test -race -count=1 ./internal/shell ./internal/theme
```

**Step 4: Commit**

```bash
git add internal/shell/registry.go internal/shell/registry_test.go internal/shell/animation.go internal/shell/animation_test.go internal/shell/theme_test.go
git commit -m "feat(theme): transition valid palette changes"
```

## Task 11: Run the full automated gate

**Files:**

- Modify only if verification exposes a catalogue regression
- Update: `.beads/issues.jsonl` through the primary checkout when closing `sysc-141`

Run in this order:

```bash
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
git diff --check
```

Expected: all commands exit zero; `go.mod` and `go.sum` are unchanged.

Then inspect ownership and inventory:

```bash
rg -n "time.NewTicker|time.NewTimer" internal/shell
rg -n "KindButton" internal/shell --glob '*.go'
du -h internal/render/icons/material/material-symbols-rounded.ttf
git status --short
```

Expected: one surface animation scheduling path, every button site classified by
Task 0, only the static subset is committed, and no unrelated files are present.

If the automated gate is clean, run from `/home/nomadx/sysc-shell`:

```bash
bd close sysc-141 --reason "Shared native chrome, transitions, Material subset, and session consumer implemented and verified"
bd sync --flush-only
```

Copy the resulting `.beads/issues.jsonl` change into the implementation
worktree, review the diff, and commit it with any final verification-only fixes.
Do not close `sysc-134`, `sysc-110`, or `sysc-142`.

## Task 12: Run or honestly defer the live Niri gate

Do not disrupt another compositor gate. When the session is available, set:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Start the worktree binary without `pkill -f`; stop an old scratch binary only by
its verified PID. Exercise:

1. Right-click the battery; confirm left-click remains inert.
2. Open the same panel with Super+X and `panel.toggle {"panel":"power"}`.
3. Capture resting, hover, pressed, keyboard-focus, and selected profile states.
4. Switch every advertised power profile and confirm the selected segment moves without label clipping.
5. Confirm Lock visibility follows locker configuration.
6. Trigger Escape and outside-click closing.
7. Apply a valid light/dark theme, an invalid theme, and reduced motion; confirm crossfade, retention, and snap behavior respectively.
8. Run `niri msg -j layers` and confirm the panel and shield map and cleanly disappear.
9. Capture a grim showing Battery, Power profile, and Session cards at 420 px.

The machine currently has only DP-1 at 3440×1440 scale 1.0. Record the
two-output case as unrunnable; do not claim it. If the recorder or owner defers
live Niri, leave the live gate open in beads and report exactly which checks
were not run. The automated gate remains mandatory.

## Final stop condition

Stop after the shared semantic chrome, finite icon subset, native transition
mechanism, inherited regression checks, and session/power consumer pass their
gates. Do not pull `sysc-142` full theme parity, a settings redesign, a control
centre, new panel types, blur/shadows/EGL, plugin changes, a lock screen, or
gamer-mode behavior into this branch.
{"content_type":"terminal","tokens_before":6190,"tokens_after":6190,"token_count_basis":"o200k_base","ratio":0,"basis":"inferred"}

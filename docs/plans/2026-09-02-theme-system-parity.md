# Theme System Parity Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Resolve palette, density, typography, shape, opacity, elevation, motion, and accessibility into one live native-Go theme consumed by every first-party shell surface.

**Architecture:** Extend `internal/theme`, `internal/config`, and `internal/shell.Theme`; do not add another theme service. The registry validates one complete candidate and supplies renderer-ready semantic values to retained UI trees. Presets seed independent axes, explicit settings override them, and accessibility constraints apply last.

**Tech Stack:** Go 1.26, the existing pure-Go shared-memory renderer, `go-text/typesetting`, matugen JSON generation, the `sysc-141` surface animator, and the current Wayland/Niri runtime.

---

## Scope and execution rules

Read `docs/plans/2026-09-02-theme-system-parity-design.md` and
`docs/plans/2026-09-02-chrome-catalogue-design.md` before editing. This plan
depends on `sysc-141`. Start only after its implementation has landed on
`main` and its automated gate passes.

Create a dedicated `feature/theme-system-parity` worktree from that `main`.
Run `bd` only from `/home/nomadx/sysc-shell`; set
`BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` for commits made from the
worktree.

The owner directed implementation-first development. For each task:

1. trace the current owner and callers;
2. implement the smallest coherent slice;
3. add one focused runnable regression check for non-trivial logic;
4. run the affected packages;
5. commit the slice.

Do not manufacture a failing test before implementation. Do not add a module,
CGO, QML, runtime SVG, compositor blur, a theme daemon, or a runtime theme-pack
format.

Preserve these invariants:

- one Wayland-owner dispatch goroutine;
- one animator per active surface and no frames after settlement;
- all host configure/render/handle paths retain their current locks;
- invalid configuration or palette input retains the previous complete theme;
- open auxiliary surfaces and their interaction state survive valid reloads;
- project and Material icon faces retain priority over system font fallback;
- `theme-gen` continues to own palette source, seed, scheme, and mode;
- existing bar and output settings remain explicit local overrides;
- components request semantic roles and never carry theme RGB values.

## Task 0: Reconcile the post-chrome tree

**Files:**

- Read: `docs/plans/2026-09-02-chrome-catalogue.md`
- Read: `internal/theme/theme.go`
- Read: `internal/shell/theme.go`
- Read: `internal/render/paint.go`
- Read: `internal/shell/animation.go`
- Read: `internal/ui/tree.go`

From the primary checkout, confirm the dependency:

```bash
bd show sysc-141
git merge-base --is-ancestor <sysc-141-final-commit> main
```

Expected: `sysc-141` is closed and its final commit is an ancestor of `main`.
Stop if either condition fails.

In the new worktree, inventory provisional and legacy theme use:

```bash
rg -n 'ProofStyle|TextSize|\.Background|\.Foreground|\.Accent|\.Muted|\.Capsule|\.Container|Bold:|FontSize|FontFamily|time\.(NewTicker|NewTimer)' internal --glob '*.go'
go test ./...
```

Expected: the suite passes. Record the inventory in the implementation notes
or commit message, not in a status section in this plan. If `sysc-141` changed
the named fields, update later task file lists before code work.

Do not commit Task 0 unless the executable plan needs a path correction.

## Task 1: Complete and validate the Material palette

**Files:**

- Modify: `internal/theme/theme.go`
- Modify: `internal/theme/generate.go`
- Modify: `internal/theme/matugen/tpl.json`
- Create: `internal/theme/contrast.go`
- Modify: `internal/theme/theme_test.go`
- Create: `internal/theme/contrast_test.go`

### Implementation

Expand `Tokens` and `Fallback` to the complete role family in design D4. Keep
the struct concrete. Do not replace it with a string map.

Add these pure entry points:

```go
func (t Tokens) Valid(highContrast bool) error
func ContrastRatio(a, b Color) float64
func EnsureContrast(foreground, background Color, ratio float64) Color
```

Use one parsed theme colour type inside `internal/theme`; do not import the
renderer. `EnsureContrast` chooses the black or white direction with the best
reachable ratio and binary-searches the smallest sRGB change. Preserve alpha.

Change generation error behavior:

- cold start may return `Fallback` with the generation error;
- successful generation returns a complete validated palette;
- the caller can distinguish fallback from success;
- high contrast still passes `--contrast 1` and runs local validation.

### Focused checks

Cover dark and light template completeness, fallback completeness, malformed
fields, every foreground/background pair, deterministic contrast repair,
unreachable requested ratios, alpha preservation, and the matugen failure
result.

Run:

```bash
go test ./internal/theme -run 'Token|Palette|Contrast|Generate'
go test ./internal/theme
```

Expected: both commands exit zero.

### Commit

```bash
git add internal/theme
git commit -m "feat(theme): validate full palette roles"
```

## Task 2: Add composition profiles and sparse configuration

**Files:**

- Create: `internal/theme/profile.go`
- Create: `internal/theme/profile_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/write.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/write_test.go`

### Implementation

Add concrete theme-axis types for density, typography, shape, surface opacity,
elevation, and motion. `internal/theme/profile.go` owns the three preset tables
and the finite density/type/motion tables. It imports no configuration or shell
package.

Extend `config.Theme` with the design D3 fields. Decode them through pointer
wire fields, validate every enum and range with its full JSON path, and resolve
in this order:

```text
selected preset -> explicit theme fields -> derived base bar -> explicit bar -> output bar
```

Keep `Config` fully resolved. `toWire` compares theme fields against the
selected preset and compares base-bar values against the bar derived from that
resolved theme.

Add a rebase helper used by settings later. For each axis, replace the current
value with the new preset value only when it equals the old preset value. Apply
the same rule to derived base-bar values. Preserve explicit deviations.

Treat existing files as `standard`. Their current radius, font, height,
padding, and spacing retain the same effective values after parsing and sparse
rewriting.

### Focused checks

Table-test all preset values, each invalid enum/range, preset plus override
precedence, bar/output precedence, old-file migration, preset rebasing, and
write/read/write stability.

Run:

```bash
go test ./internal/theme ./internal/config -run 'Profile|Theme|Preset|Rebase|RoundTrip'
go test ./internal/theme ./internal/config
```

### Commit

```bash
git add internal/theme/profile.go internal/theme/profile_test.go internal/config
git commit -m "feat(config): resolve theme composition profiles"
```

## Task 3: Make `shell.Theme` the sole resolved contract

**Files:**

- Modify: `internal/shell/theme.go`
- Modify: `internal/shell/theme_test.go`
- Create: `internal/render/style.go`
- Create: `internal/render/style_test.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/toasthost.go`
- Modify: `internal/shell/traydrawer.go`
- Modify: `internal/shell/traymenuhost.go`
- Modify: `internal/shell/osd.go`

### Implementation

Replace the growing provisional field list with grouped concrete values inside
`shell.Theme`: palette, type, metrics, shapes, surfaces, elevation, and motion.
Keep temporary legacy aliases only where Task 0 found a live caller.

Add one resolver:

```go
func ResolveTheme(cfg config.Config, bar config.Bar, tok theme.Tokens) (Theme, error)
```

It parses palette colours, applies composition values and bar overrides,
enforces accessibility, and calls `Valid` before returning. Delete the
registry-free fallback path once all real surfaces use this resolver. Tests may
call it with `theme.Fallback`.

Rename `render.ProofStyle` to `render.Style`. A temporary type alias may keep
the change reviewable for one commit. `Style` holds renderer-ready semantic
groups and never imports config or shell.

Update each host construction path in the file list to receive the resolved
style. Do not change visual output in this task.

### Focused checks

Prove every active surface resolves through registry tokens, all groups
validate, accessibility applies after bar overrides, and no shell file outside
`theme.go` assembles palette or composition values.

Run:

```bash
go test ./internal/theme ./internal/config ./internal/render ./internal/shell -run 'ResolveTheme|Style|SurfaceTheme|Valid'
go test ./internal/render ./internal/shell
```

### Commit

```bash
git add internal/render/style.go internal/render/style_test.go internal/render/paint.go internal/shell
git commit -m "refactor(theme): centralize resolved shell style"
```

## Task 4: Resolve semantic typography with real faces

**Files:**

- Modify: `internal/ui/tree.go`
- Modify: `internal/ui/layout.go`
- Modify: `internal/ui/column.go`
- Modify: `internal/ui/layout_test.go`
- Modify: `internal/ui/column_test.go`
- Modify: `internal/render/fontmap.go`
- Modify: `internal/render/fontmap_test.go`
- Modify: `internal/render/text.go`
- Modify: `internal/render/text_test.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/render/paint_test.go`

### Implementation

Add `TextRole` values for Caption, Label, Body, Title, Headline, and Mono.
`KindText` defaults to Body. Button labels default to Label. Layout and paint
resolve one `TextSpec` containing family, physical size, weight, and italic
state.

Extend `FontMap` resolution and its bounded cache key to include rune, family
class, requested weight, and italic state. Use `fontscan.Query.Aspect` from the
installed dependency. Keep per-rune fallback and icon-face priority.

Remove synthetic bold and italic only after real-face resolution covers their
callers. If the requested face has no matching aspect, use the closest face
reported by `fontscan`; do not fail a frame.

### Focused checks

Prove measure and paint choose the same role and face, a deterministic
test-only font map keeps weight/style cache entries distinct, an unavailable
family falls back, Unicode fallback stays bounded, and project/Material icons
retain their dedicated faces. The live gate checks installed Inter weights;
unit tests must not depend on host fonts.

Run:

```bash
go test ./internal/ui ./internal/render -run 'TextRole|Font|Weight|Fallback|Measure'
go test -race -count=1 ./internal/ui ./internal/render
```

### Commit

```bash
git add internal/ui internal/render/fontmap.go internal/render/fontmap_test.go internal/render/text.go internal/render/text_test.go internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(render): resolve semantic type roles"
```

## Task 5: Apply density and shape metrics to retained layout

**Files:**

- Modify: `internal/ui/tree.go`
- Modify: `internal/ui/layout.go`
- Modify: `internal/ui/column.go`
- Modify: `internal/ui/layout_test.go`
- Modify: `internal/ui/column_test.go`
- Modify: `internal/shell/theme.go`
- Modify: `internal/shell/theme_test.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/bar_test.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/panelhost_test.go`

### Implementation

Add semantic metric and shape roles only where current components consume
them. Resolve spacing, padding, control height, icon size, and radius from the
theme before measurement. Preserve an explicit node dimension and the measured
420 px session panel.

Keep circle and stadium geometry independent of the configurable base radius.
Cards and panels use bounded semantic radii. Remove local constants when a
theme role now owns the same value; retain protocol geometry and measured
component widths.

### Focused checks

Run the same representative bar, button, segmented row, card, settings row,
notification, tray row, and panel tree through compact, standard, and
comfortable density. Assert no clipping, stable hit bounds, and circle/stadium
shape at radius zero.

Run:

```bash
go test ./internal/ui ./internal/shell -run 'Density|Metric|Shape|Radius|Layout'
go test ./internal/ui ./internal/shell
```

### Commit and checkpoint 1

```bash
git add internal/ui internal/shell/theme.go internal/shell/theme_test.go internal/shell/bar.go internal/shell/bar_test.go internal/shell/panelhost.go internal/shell/panelhost_test.go
git commit -m "feat(ui): apply theme density and shapes"
go test -race -count=1 ./internal/theme ./internal/config ./internal/ui ./internal/render ./internal/shell
```

Review the first five commits before continuing. Reject any second resolver,
unbounded cache, arbitrary layout multiplier, or configuration read below the
shell registry.

## Task 6: Paint the semantic composition recipes

**Files:**

- Modify: `internal/ui/tree.go`
- Modify: `internal/render/style.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/render/paint_test.go`
- Modify: `internal/render/canvas.go`
- Modify: `internal/render/canvas_test.go`

### Implementation

Expand the finite fill/foreground roles from `sysc-141` to match design D6.
Resolve the pair once for each node, then paint its state layer and children.
Add tonal selection, error container, scrim, and structural outline only where
the current shell uses them.

Use the existing premultiplied source-over canvas path. Do not add a generic
CSS-style cascade or per-node RGB escape hatch.

### Focused checks

Pixel-test every composition row in D6 in dark and light mode. Cover nested
source-over order, normal state-layer opacities, disabled emphasis, panel-card
separation, and structural outline fallback.

Run:

```bash
go test ./internal/render ./internal/ui -run 'Semantic|Composition|StateLayer|Surface|Outline'
go test ./internal/render ./internal/ui
```

### Commit

```bash
git add internal/ui/tree.go internal/render
git commit -m "feat(render): paint semantic theme layers"
```

## Task 7: Migrate every first-party surface and tree

**Files:**

- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/toasthost.go`
- Modify: `internal/shell/traydrawer.go`
- Modify: `internal/shell/traymenuhost.go`
- Modify: `internal/shell/osd.go`
- Modify: `internal/shell/popout_clock.go`
- Modify: `internal/shell/popout_monitor.go`
- Modify: `internal/shell/popout_session.go`
- Modify: `internal/shell/popout_settings.go`
- Modify: `internal/shell/popout_launcher.go`
- Modify: `internal/shell/popout_notifications.go`
- Modify: `internal/shell/notifycard.go`
- Modify alongside each file: its existing `_test.go`

### Implementation

Assign surface, text, metric, icon, and shape roles to each existing tree.
Panel roots, cards, nested controls, selected controls, destructive actions,
shields, focus rings, and boundaries must match design D6. Preserve component
behavior and action IDs.

Delete legacy theme aliases and fixed visual constants after the last caller
moves. Keep a package test that scans for forbidden legacy field assembly and
direct theme RGB values.

If Task 0 found a new first-party surface, add its exact source and test file to
this task before editing it.

### Focused checks

Run each surface family at compact and comfortable density, with standard and
expressive shape. Assert role selection, focus order, hit targets, and panel
width constraints. Existing behavioral tests must remain unchanged unless a
semantic paint expectation replaces an old raw colour.

Run:

```bash
go test ./internal/shell -run 'Bar|Panel|Toast|Tray|OSD|Clock|Monitor|Session|Settings|Launcher|Notif|Theme'
go test -race -count=1 ./internal/shell
```

### Commit

```bash
git add internal/shell
git commit -m "feat(shell): compose surfaces from theme roles"
```

## Task 8: Apply opacity and elevation

**Files:**

- Modify: `internal/render/style.go`
- Modify: `internal/render/paint.go`
- Modify: `internal/render/paint_test.go`
- Modify: `internal/render/mask.go`
- Modify: `internal/render/mask_test.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/toasthost.go`
- Modify: `internal/shell/traydrawer.go`
- Modify: `internal/shell/traymenuhost.go`
- Modify: `internal/shell/osd.go`
- Modify alongside each host: its existing `_test.go`

### Implementation

Apply bar, panel, and overlay opacity to root fills. Paint nested surfaces over
their root so cards retain the intended hierarchy. Resolve `none`, `subtle`,
and `standard` onto the existing shadow texture and the Material Shadow role.

High contrast overrides root opacity to 100 and enables the structural
outline. Keep opaque-region reporting correct: a surface with root alpha below
255 must not claim an opaque background.

### Focused checks

Pixel-test source-over results, corner alpha, shadow role and level, outline
fallback, opaque-region policy, and high-contrast forcing. Include a nested
card over a 90-percent panel.

Run:

```bash
go test ./internal/render ./internal/shell -run 'Opacity|Alpha|Elevation|Shadow|Opaque|HighContrast'
go test ./internal/render ./internal/shell
```

### Commit

```bash
git add internal/render internal/shell
git commit -m "feat(theme): apply surface depth and opacity"
```

## Task 9: Replace hard-coded animation timing with motion tokens

**Files:**

- Modify: `internal/shell/animation.go`
- Modify: `internal/shell/animation_test.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/panelhost_test.go`
- Modify: `internal/shell/osd.go`
- Modify: `internal/shell/osd_test.go`
- Modify: `internal/render/style.go`

### Implementation

Pass one resolved motion table to each surface animator. Map hover, press,
selection, visibility, palette, and layout recipes to the duration tokens in
design D10. Divide by the bounded speed factor once. Standard uses out-cubic;
expressive spatial recipes use out-quart.

Retarget active colour, opacity, translation, scale, bounds, and radius from
their displayed values when motion settings change. Reduced motion settles
all spatial and state transitions and caps panel opacity-only visibility at
150 ms.

### Focused checks

Use the existing fake clock to cover every duration at 25, 100, and 400
percent, standard versus expressive curves, mid-transition retargeting,
reduced motion, and no scheduled frame after settlement.

Run:

```bash
go test ./internal/shell ./internal/render -run 'Motion|Duration|Animator|Reduced|Settled|Retarget'
go test -race -count=1 ./internal/shell
```

### Commit

```bash
git add internal/shell/animation.go internal/shell/animation_test.go internal/shell/panelhost.go internal/shell/panelhost_test.go internal/shell/osd.go internal/shell/osd_test.go internal/render/style.go
git commit -m "feat(theme): drive transitions from motion roles"
```

## Task 10: Expose presets and axes in settings

**Files:**

- Modify: `internal/settings/registry.go`
- Modify: `internal/settings/registry_test.go`
- Modify: `internal/shell/popout_settings.go`
- Modify: `internal/shell/popout_settings_test.go`
- Modify: `internal/config/write.go`
- Modify: `internal/config/write_test.go`

### Implementation

Register the D3 fields under Appearance. Keep reduced motion and high contrast
under Accessibility. Map percent and weight fields through the existing
integer control; add no float setting kind.

Preset selection calls the Task 2 rebase helper. Any other edited axis becomes
an explicit deviation through the sparse writer. Replace `bar.radius` with
`appearance.radius`; keep the remaining bar entries as local overrides.

Resolve and validate the draft through the same candidate path used by reload
before writing it. Display a missing configured font's fallback without
rejecting the draft.

### Focused checks

Cover discovery/search of every entry, enum options and integer bounds, preset
rebase with preserved overrides, radius persistence, missing font fallback,
atomic write failure, and settings-panel survival after reload.

Run:

```bash
go test ./internal/settings ./internal/config ./internal/shell -run 'Appearance|Preset|Setting|Radius|Font|Persist'
go test ./internal/settings ./internal/config ./internal/shell
```

### Commit

```bash
git add internal/settings internal/config/write.go internal/config/write_test.go internal/shell/popout_settings.go internal/shell/popout_settings_test.go
git commit -m "feat(settings): expose theme composition axes"
```

## Task 11: Publish theme changes atomically to live surfaces

**Files:**

- Modify: `internal/shell/registry.go`
- Modify: `internal/shell/registry_test.go`
- Modify: `internal/shell/theme.go`
- Modify: `internal/shell/theme_test.go`
- Modify: `internal/shell/bar.go`
- Modify: `internal/shell/panelhost.go`
- Modify: `internal/shell/toasthost.go`
- Modify: `internal/shell/traydrawer.go`
- Modify: `internal/shell/traymenuhost.go`
- Modify: `internal/shell/osd.go`
- Modify alongside each host: its existing `_test.go`

### Implementation

Generate and resolve a complete candidate before mutating registry state.
Build and measure replacement trees while holding the established owner lock,
then publish tokens, resolved themes, trees, font maps, and host dimensions as
one accepted state.

Palette and opacity changes retarget active animations. Geometry changes use
one Wayland configure and may animate stable child bounds afterward. Preserve
mapped auxiliary surfaces, focus keys, hover/press keys, panel drafts, toast
state, and OSD state.

On generation, parse, font, layout, or validation error, retain the prior
tokens and all resolved axes. Return the error through the existing reload and
settings path. External templates must not apply from a rejected candidate.

### Focused checks

Table-test palette-only, motion-only, typography, density, shape, opacity,
high-contrast, and invalid changes with open panel, tray, toast, and OSD hosts.
Assert one publication, correct invalidations, no mixed old/new roles, no
surface dismissal, and no off-owner mutation under the race detector.

Run:

```bash
go test ./internal/shell -run 'Theme.*Reload|Atomic|Candidate|Mapped|Focus|Invalid'
go test -race -count=1 ./internal/shell ./internal/theme ./internal/config
```

### Commit and checkpoint 2

```bash
git add internal/shell
git commit -m "feat(theme): apply live themes atomically"
go test -race -count=1 ./internal/theme ./internal/config ./internal/settings ./internal/ui ./internal/render ./internal/shell
```

Review the migration commits. Confirm one resolver, one animation clock per
surface, one accepted-state publication, and no remaining provisional theme
aliases.

## Task 12: Export the complete application palette

**Files:**

- Modify: `internal/theming/catalog.go`
- Modify: `internal/theming/catalog_test.go`
- Modify: `internal/theming/enabled.go`
- Modify: `internal/theming/enabled_test.go`
- Modify as needed: `internal/theming/templates/*.tpl`

### Implementation

Export every validated Material token plus mode and source metadata to the
existing template context. Do not export shell density, type, shape, opacity,
elevation, or motion values.

Apply templates only after registry candidate acceptance. Preserve serialized
application, atomic replacement, reversal, and template enablement behavior.

### Focused checks

Render a template that references each token, reject an unknown token, prove a
rejected candidate writes no files, and rerun the existing enable/disable and
concurrent-apply checks.

Run:

```bash
go test ./internal/theming -run 'Catalog|Render|Apply|Invalid|Concurrent'
go test -race -count=1 ./internal/theming
```

### Commit

```bash
git add internal/theming
git commit -m "feat(theme): export complete application palette"
```

## Task 13: Run the automated and live gates

**Files:**

- Modify only when a gate exposes a theme-system defect
- Update through `bd`: `.beads/issues.jsonl`

### Automated gate

Run in this order:

```bash
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
git diff --check
```

Expected: every command exits zero and module files remain unchanged.

Inspect ownership and leftovers:

```bash
rg -n 'ProofStyle|TextSize|\.Background|\.Foreground|\.Accent|\.Muted|\.Capsule|\.Container|Bold:' internal --glob '*.go'
rg -n 'time\.(NewTicker|NewTimer)' internal/shell --glob '*.go'
rg -n '#[0-9a-fA-F]{6,8}' internal/shell internal/ui --glob '*.go'
git status --short
```

Expected: only documented compatibility sites remain, animation scheduling has
one owner per surface, component code contains no theme RGB, and the worktree
contains no unrelated changes.

### Live Niri gate

Set the compositor environment:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Run the worktree binary without `pkill -f`. Stop an old scratch binary by its
verified PID.

Exercise these configurations through the settings panel while the bar, one
panel, one tray surface when available, a toast when available, and OSD are
visible:

1. standard, compact, and expressive presets;
2. compact and comfortable density with a font-scale change;
3. radius 0 and 32, checking circles and stadiums;
4. bar, panel, and overlay opacity at 80 and 100;
5. no, subtle, and standard elevation;
6. light and dark wallpaper/hex/stock palettes;
7. motion at 25, 100, and 400 percent;
8. reduced motion and high contrast;
9. an invalid palette, an unavailable font family, and malformed manual config;
10. reload while a settings draft has focus and another panel has hover state.

For each case, confirm text does not clip, hit targets stay aligned, cards
remain distinct, focus remains visible, mapped surfaces stay open, invalid
palette or config input retains the previous complete theme, an unavailable
font reports its fallback, and transitions stop repainting.
Run `niri msg -j layers` before and after closing the surfaces.

This machine has one DP-1 output at 3440x1440 scale 1.0. Record the two-output
and fractional-scale live cases as unrunnable here. Do not claim them.

### Close and final commit

From `/home/nomadx/sysc-shell`:

```bash
bd close sysc-142 --reason "Native theme composition, settings, accessibility, and live application implemented and verified"
bd sync --flush-only
```

Review `.beads/issues.jsonl`, transfer its exact change to the implementation
worktree, and commit it with any final gate correction. Do not close unrelated
live Niri issues.

## Final stop condition

Stop after every current first-party surface consumes one resolved theme,
settings apply every approved axis, template export uses the accepted palette,
invalid candidates retain the previous state, and active transitions settle.
Leave theme scheduling, runtime theme packs, compositor blur, DMS/Noctalia
configuration compatibility, and new shell components for separately approved
work.

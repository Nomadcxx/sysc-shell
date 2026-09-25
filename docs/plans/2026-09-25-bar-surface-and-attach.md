# Bar Surface Styles and Attached Panels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the frosted, solid and islands bar styles, with frosted as the default. Blur comes from
the compositor through `ext-background-effect-v1`. Add an attached bar shape with concave end fillets,
and make every panel except Settings, the launcher and the clipboard attach to the bar with full
concave joints.

**Architecture:** `internal/platform/wayland` gains a generated `backgroundeffect` binding, an optional
global, a `Capabilities` callback, and per-surface blur regions supplied as logical rectangles.
`internal/ui` gains a pure `BlurStrips` that turns a painted silhouette into row rectangles. `config.Bar`
gains `Style`, `Shape`, `FrostOpacity`, `PillOpacity` and a derived `Overhang`. `internal/shell`
resolves style and capability into the bar's alpha and its capsule colour, and publishes blur shapes.
Panel placement is rewritten so each panel attaches, snaps flush, or floats. `internal/render` paints
bar end fillets, per-side joints, and screen-edge fillets with the existing fillet coverage.

**Tech Stack:** Go 1.26; `github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1` for the
binding; wayland-protocols 1.45 `staging/ext-background-effect/ext-background-effect-v1.xml`.

**Spec:** `docs/plans/2026-09-25-bar-surface-and-attach-design.md` (D1–D11). No bd issue exists yet;
create an epic and one issue per phase before starting (see Tracking).

## Global Constraints

- No new module. `git diff --exit-code -- go.mod go.sum` stays clean. `go run pkg@version` for the
  scanner does not touch `go.mod`.
- The blur capability is `flags & 1`. Never test the generated `Blur` constant, which is `0` in 1.45.
- One effect object per `wl_surface`, created lazily on the first non-empty region and destroyed with
  the surface through the existing cleanup stack.
- Blur regions are logical, surface-local rectangles. A row is included where fillet or rounded
  coverage is at least 128 of 255, matching `filletCoverage` and `roundedInset`.
- Fillet radius is the theme's `Fillet` (12). The overlap at a panel joint is 1 logical px.
- Defaults: `bar.style = "frosted"`, `bar.shape = "attached"`, `bar.frost-opacity = 65`,
  `bar.pill-opacity = 70`. Ranges 40–100.
- High contrast forces `solid` at full opacity and floating joints stay as they are.
- `panel configure/render/handle` take `Registry.mu`; `rebuildPanel` already holds it.
- Test commands: per package, `GOMAXPROCS=4`. Never `./...` with `-race`.
- Commit messages pass `bash ~/.git-hooks/commit-msg <file>`.

## Tracking

Before Task 1, from `/home/nomadx/sysc-shell`, create an epic and one issue per phase, linked with the
repo's usual `bd` parent/dependency flags:

- Compositor blur: ext-background-effect binding, capability, regions (Tasks 1–4)
- Bar styles: frosted, solid, islands (Tasks 5–8)
- Attached bar shape with end fillets (Tasks 9–10)
- Attach every panel except Settings, launcher and clipboard (Tasks 11–14)
- Niri blur documentation and live gate (Tasks 15–16)

Record the two follow-ups as discovered work (`bd create "..." --deps discovered-from:<epic>`):

- Scale the pill lift by wallpaper luminance (design D3)
- Segmented bar groups, DMS "segments" style (design, "What exists")

## Review Focus

1. A compositor that advertises the manager but sends `capabilities 0`. Frost must fall back, and no
   region may be sent. Pinned in Task 2.
2. The capability changes while surfaces are mapped. Regions are cleared or re-sent, and the bar
   restyles without remapping. Pinned in Tasks 2 and 7.
3. Islands with media appearing and disappearing. The blur union follows the capsules and never keeps a
   stale rect. Pinned in Task 8.
4. A right-aligned panel on a 1366 px output in attached shape. It snaps flush, keeps its inner joint,
   and grows its surface down for the screen-edge fillet with that band click-through. Pinned in Task 12.
5. Scale 1.25. The joint overlap and the strips stay inside the painted edge at the physical rounding.
   Pinned in Tasks 3 and 13.
6. `bar.shape` changed live. The surface re-anchors with the new overhang, and the exclusive zone never
   includes the overhang. Pinned in Task 9.

---

## Phase A: compositor blur

### Task 1: Vendor the protocol and generate the binding

**Files:**
- Create: `protocols/ext-background-effect-v1.xml` (wayland-protocols 1.45, SHA-256
  `9aa5011f38752c0146014f8569f3d3981db7dbb8c04e578a80beef6c98fa9653`)
- Create: `internal/platform/wayland/backgroundeffect/generate.go`
- Create (generated): `internal/platform/wayland/backgroundeffect/background_effect.go`

- [ ] **Step 1:** Copy the XML from the wayland-protocols 1.45 release tarball or distribution package
  (`/usr/share/wayland-protocols/staging/ext-background-effect/ext-background-effect-v1.xml`). Check
  the SHA-256 above.
- [ ] **Step 2:** Write `generate.go` after `layershell/generate.go`, with the upstream, version and
  SHA-256 in the package comment. Note there that the `blur` capability value in this revision is `0`
  and that the wire mask is `1`:

```go
// Package backgroundeffect holds the generated ext-background-effect-v1 binding.
//
// Upstream: wayland-protocols 1.45, staging/ext-background-effect/ext-background-effect-v1.xml.
// SHA-256:  9aa5011f38752c0146014f8569f3d3981db7dbb8c04e578a80beef6c98fa9653
// Niri 26.04 advertises ext_background_effect_manager_v1 version 1.
//
// The capability enum in this revision declares blur as 0 in a bitfield. The
// corrected wire mask is 1; callers test flags&1, never the generated constant.
package backgroundeffect

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg backgroundeffect -o background_effect.go -i ../../../../protocols/ext-background-effect-v1.xml
```

- [ ] **Step 3:** `go generate ./internal/platform/wayland/backgroundeffect/ && go build ./...`.
  Expected: `NewExtBackgroundEffectManagerV1`, `GetBackgroundEffect`, `SetBlurRegion`, and a
  capabilities event handler.
- [ ] **Step 4:** Commit: `feat(wayland): generate ext-background-effect-v1 binding`.

### Task 2: Bind the optional global and report the capability

**Files:**
- Modify: `internal/platform/wayland/client.go`: an owner field beside `screencopy` (line ~193), a bind
  beside the screencopy bind (line ~347), destroy (line ~448); `Callbacks.Capabilities`
- Create: `internal/platform/wayland/capabilities.go`
- Test: `internal/platform/wayland/capabilities_test.go`
- Modify: `cmd/sysc-shell/main.go:297` (wire `Capabilities: registry.SetCapabilities`)
- Modify: `internal/shell/registry.go` (`SetCapabilities`, stores `blurAvailable`)

**Interfaces:**
- Produces: `wayland.Capabilities{Blur bool}`, `Callbacks.Capabilities func(Capabilities)`,
  `wayland.blurCapable(flags uint32) bool`, `(*shell.Registry).SetCapabilities(wayland.Capabilities)`.

- [ ] **Step 1: Failing test**

```go
func TestBlurCapabilityUsesTheCorrectedMask(t *testing.T) {
	for _, tc := range []struct {
		flags uint32
		want  bool
	}{{0, false}, {1, true}, {3, true}, {2, false}} {
		if got := blurCapable(tc.flags); got != tc.want {
			t.Errorf("blurCapable(%d) = %v, want %v", tc.flags, got, tc.want)
		}
	}
}
```

- [ ] **Step 2:** Implement `blurCapable(flags) bool { return flags&1 != 0 }`. Bind the manager as
  optional, like screencopy (not in `interfaceMaximum`; comment why). Record the latest flags on the
  owner. Call `Callbacks.Capabilities` once after the initial roundtrip, before any `NewHost`, and
  again on every change.
- [ ] **Step 3:** `Registry.SetCapabilities` stores the flag under `r.mu`. When the flag changes after
  bars exist, it re-resolves every bar's theme through the existing retheme path (Task 7 makes the
  theme depend on it).
- [ ] **Step 4:** `go test ./internal/platform/wayland/ ./internal/shell/ -run 'Blur|Capabilit'`.
- [ ] **Step 5:** Commit: `feat(wayland): report compositor blur capability`.

### Task 3: `BlurStrips`, the painted silhouette as row rectangles

**Files:**
- Create: `internal/ui/blurshape.go`
- Test: `internal/ui/blurshape_test.go`

**Interfaces:**
- Produces: `ui.SurfaceShape` (fields as in design D4) and `ui.BlurStrips(ui.SurfaceShape) []ui.Rect`.
- Consumes: the coverage rules. Move `filletCoverage` and `roundedInset`'s row maths into `internal/ui`
  as exported pure helpers (`ui.FilletExtent(row, r int) int`, `ui.RoundedInset(row, h, r int) int`),
  and have `internal/render/canvas.go` call them, so paint and region share one rule.

- [ ] **Step 1: Failing table test.** Cases:
  - square body → one rect;
  - radius 12 on a 40-high body → one centre band plus 12 rows at the top and 12 at the bottom, with the
    first row inset by `RoundedInset(0,40,12)`;
  - `SquareEdge "top"` with radius 12 → no inset rows at the top;
  - attached bar body `{0,0,1920,40}` with `EndFillets 12` → the body plus 12 rows below it at each end,
    widths `FilletExtent(y,12)` anchored at x=0 and x=1920;
  - panel with `JointLeft 12, JointRight 5` → wedge rows of 12 and 5 beside the square edge;
  - `FlushRight` → no right joint, plus a screen-edge wedge below the body at the right;
  - radius 40 on a 28-high body → clamped to 14.
  Every case asserts that no rect lies outside the painted mask. Compute that with the same helpers.
- [ ] **Step 2:** Implement. Merge adjacent rows of equal span so a straight edge is one rect.
- [ ] **Step 3:** `go test ./internal/ui/ ./internal/render/ -count=1` (the render fillet tests must
  still pass after the helper move).
- [ ] **Step 4:** Commit: `feat(ui): describe painted silhouettes as blur strips`.

### Task 4: Apply blur regions per surface

**Files:**
- Modify: `internal/platform/wayland/host.go` (`HostCallbacks.BlurShape func() []ui.Rect`;
  `surfaceUnit` gains `effect *backgroundeffect.ExtBackgroundEffectSurfaceV1` and `blurRects []ui.Rect`)
- Modify: `internal/platform/wayland/aux_surface.go` (same for aux surfaces)
- Modify: `internal/platform/wayland/regions.go` (`applyBlurRegion`)
- Test: `internal/platform/wayland/regions_test.go`

- [ ] **Step 1: Failing tests** for the pure decision `blurRegionUpdate(prev, next []ui.Rect,
  capable bool) (send bool, clear bool)`: equal slices → nothing; empty to empty → nothing; capable
  false → clear when prev is non-empty and never send; a changed slice → send.
- [ ] **Step 2:** In the commit path, after `Render` and before `wl_surface.commit`, call `BlurShape`.
  Pass its result through `blurRegionUpdate`. Create the effect object on the first send, build a
  `wl_region` from the rects, call `SetBlurRegion`, then destroy the region. A clear sends
  `SetBlurRegion(nil)`. Push the effect's `Destroy` onto the unit's cleanup stack before the surface's.
- [ ] **Step 3:** When the capability is lost, clear every mapped unit's region.
- [ ] **Step 4:** `go test ./internal/platform/wayland/ -count=1`.
- [ ] **Step 5:** Commit: `feat(wayland): publish compositor blur regions`.

## Phase B: bar styles

### Task 5: Config fields

**Files:**
- Modify: `internal/config/config.go` (`Bar.Style`, `Bar.Shape`, `Bar.FrostOpacity`, `Bar.PillOpacity`,
  `Bar.Overhang`; `Default()`; `deriveBar` sets `Overhang`)
- Modify: `internal/config/load.go` (`wireBar` gains `style`, `shape`, `frost-opacity`, `pill-opacity`;
  validation)
- Modify: `internal/config/write.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Failing tests**
  - a document without the keys → frosted, attached, 65, 70;
  - `"style":"glass"` → error naming `bar.style` and the three options;
  - `frost-opacity: 30` → error naming the 40–100 range;
  - round trip through `Write` → emits only non-default keys;
  - `Extent()` for attached and floating (see Task 9 for the surface extent).
- [ ] **Step 2:** Implement. The option sets are package vars (`BarStyles`, `BarShapes`), and the
  settings registry reads them.
- [ ] **Step 3:** `go test ./internal/config/ -count=1`.
- [ ] **Step 4:** Commit: `feat(config): bar style, shape and frost opacities`.

### Task 6: Settings entries

**Files:**
- Modify: `internal/settings/registry.go` (four entries in section `Bar`: Style and Shape in group
  `Surface`, Frost opacity and Pill opacity in group `Frost`; relabel `appearance.bar-opacity` to
  "Solid bar opacity"; reword "Blur behind panels")
- Test: `internal/settings/registry_test.go`

- [ ] **Step 1: Failing tests:** each entry's `Set` rejects out-of-range and unknown values; `Get`
  reflects the config; `Default` matches `config.Default()`; Style's describe text names Niri 26.04.
- [ ] **Step 2:** Implement with `setEnum` and `setInt` as the neighbouring entries do.
- [ ] **Step 3:** `go test ./internal/settings/ ./internal/shell/ -run 'Settings|Registry' -count=1`.
- [ ] **Step 4:** Commit: `feat(settings): bar style, shape and frost controls`.

### Task 7: Theme resolves style and capability

**Files:**
- Modify: `internal/shell/theme.go` (`Theme.BarStyle`, `Theme.Frosted` (effective), `Theme.PillAlpha`;
  `resolveSurfaces` takes the style, frost opacity and capability; `barStyle` applies pill alpha and
  lift to `Style.Capsule`)
- Modify: `internal/shell/registry.go` (pass `blurAvailable` into theme resolution for bars and
  panels)
- Test: `internal/shell/theme_test.go`

- [ ] **Step 1: Failing table test** over style × capable × high contrast. Expected bar alpha:
  - solid → `opacityAlpha(bar-opacity, false)`;
  - frosted, capable → frost opacity with floor 40;
  - frosted, not capable → as solid;
  - islands → 0.

  Expected pill alpha: 255 for solid; pill opacity, floor 40, for frosted and islands when capable;
  the solid floor when not capable. High contrast → solid, 255, 255.
- [ ] **Step 2: Failing test** for the lift: pill opacity 70 lifts `Capsule` 9% toward `Foreground`,
  and 100 lifts 0%.
- [ ] **Step 3:** Implement. `BackgroundOpaque()` is false whenever the bar alpha is below 255, which
  already turns off the opaque-region hint.
- [ ] **Step 4:** `go test ./internal/shell/ -run Theme -count=1`.
- [ ] **Step 5:** Commit: `feat(shell): resolve bar style into ground and pill alpha`.

### Task 8: The bar publishes its blur shape

**Files:**
- Modify: `internal/shell/bar.go` (`(*Bar).blurShape() []ui.Rect`)
- Modify: `internal/shell/registry.go:1813` (add `BlurShape: bar.blurShape` to the bar's
  `HostCallbacks`)
- Test: `internal/shell/bar_test.go`

- [ ] **Step 1: Failing tests** at 1200 × extent:
  - solid → nil;
  - frosted floating → `BlurStrips` of the body with radius 12;
  - frosted attached → the body plus end fillets;
  - islands → one rounded rect's strips per visible capsule. Apply a media snapshot and assert the media
    capsule's strips appear; clear it and assert they are gone;
  - not capable → nil for every style.
- [ ] **Step 2:** Implement from the arranged sections under `b.mu`. Capsule radius comes from
  `Shapes.For(capsule.Shape, radius)`.
- [ ] **Step 3:** `go test ./internal/shell/ -run 'Bar|Blur' -count=1`.
- [ ] **Step 4:** Commit: `feat(shell): publish the bar's blur shape per style`.

## Phase C: attached bar

### Task 9: Geometry

**Files:**
- Modify: `internal/config/config.go`: `Extent()` is the body height when attached, today's value when
  floating; new `SurfaceExtent() = Extent() + Overhang`; `ExclusiveZone()` unchanged in meaning (the
  default follows `Extent()`)
- Modify: `internal/platform/wayland/host.go:172` (`surfaceHeight` uses `SurfaceExtent()`)
- Modify: `internal/platform/wayland/regions.go` (`hostRegionGeometry`: attached body at x=0, full
  width, y=0 for top and y=overhang for bottom; input region is the body only)
- Modify: `internal/shell/bar.go:415` (`bodyLocked` honours shape and edge)
- Modify: `internal/shell/theme.go` (`Geometry` returns the attached values)
- Test: `internal/config/config_test.go`, `internal/platform/wayland/regions_test.go`,
  `internal/shell/bar_test.go`

- [ ] **Step 1: Failing tests:**
  - Height 48, Gap 4 → attached `Extent` 40, `SurfaceExtent` 52, zone 40; floating 44 / 44 / 44;
  - a bottom-edge attached body sits at y=12 in a 52-high surface;
  - the input rect excludes the overhang;
  - the shell content band matches the platform body in both shapes and both edges.
- [ ] **Step 2:** Implement. The reload path already re-sends layer geometry. Add a test that a shape
  change produces a new size request and zone.
- [ ] **Step 3:** `go test ./internal/config/ ./internal/platform/wayland/ ./internal/shell/ -count=1`.
- [ ] **Step 4:** Commit: `feat(bar): attached shape geometry`.

### Task 10: Paint the end fillets

**Files:**
- Modify: `internal/render/style.go` (`Style.EndFillets int`, `Style.EndEdge string`)
- Modify: `internal/render/paint.go` (after the root fill: `fillEndFillets`; attached bar root radius 0)
- Modify: `internal/render/canvas.go` (`fillEndFillets`; `clearOutsideRoundedRect` keeps the end wedges)
- Modify: `internal/shell/bar.go` (set the style fields when attached)
- Test: `internal/render/paint_test.go`

- [ ] **Step 1: Failing pixel tests** on a 200 × 52 canvas with the body `{0,0,200,40}`, top edge,
  fillet 12:
  - pixel (0,40) is the root colour;
  - (11,51) is transparent;
  - (1,41) is near full coverage;
  - the mirrored pixels at the right end match;
  - a bottom-edge case is the vertical mirror.
- [ ] **Step 2:** Implement with `ui.FilletExtent`. The wedge fill is `rootFill()`, so a frosted
  bar's wedges share its alpha and the strips from Task 8 cover them.
- [ ] **Step 3:** `go test ./internal/render/ -count=1`.
- [ ] **Step 4:** Commit: `feat(render): concave end fillets for an attached bar`.

## Phase D: attached panels

### Task 11: Placement for every panel

**Files:**
- Modify: `internal/shell/panelhost.go:598` (`spawnPanelLocked`: remove `CenterY` from Wallpaper;
  Launcher and Clipboard keep `CenterY`; Settings becomes `CenterY`; islands style or no bar on the output → `Detached` with `Gap: theme.MarginS`)
- Modify: `internal/shell/panel.go` (`Placement.Detached bool`)
- Test: `internal/shell/panelhost_test.go`

- [ ] **Step 1: Failing table test** over every `PanelID` × {frosted-attached, solid-floating, islands,
  bar disabled}. It asserts `CenterY`, `Detached`, `Align`, and whether the style sets `AttachEdge`.
  Settings, Launcher and Clipboard are always `CenterY`. Islands and bar-disabled are always `Detached`.
- [ ] **Step 2:** Implement. Keep today's per-panel `Align`.
- [ ] **Step 3:** `go test ./internal/shell/ -run 'Placement|Spawn|Panel' -count=1`.
- [ ] **Step 4:** Commit: `feat(panels): attach every panel except the floating three`.

### Task 12: Per-side joints and flush panels

**Files:**
- Modify: `internal/shell/panel.go` (`Placement.Joints(bar BarShape) (left, right int, flushL, flushR
  bool)`; `Margins` snaps flush)
- Modify: `internal/shell/panelhost.go` (`filletMargin` → joints; surface width
  `Panel.W + left + right`; height `+ Fillet` when flush; input region is the body only)
- Modify: `internal/render/style.go`, `internal/render/canvas.go`, `internal/render/paint.go`
  (`JointLeft`, `JointRight`, `FlushLeft`, `FlushRight`; `fillAttachFillets` per side; a screen-edge
  wedge at the flush bottom corner)
- Test: `internal/shell/panel_test.go`, `internal/render/paint_test.go`

- [ ] **Step 1: Failing tests** for `Joints` on a 1920 output:
  - a 340-wide centred panel → 12 / 12;
  - `AnchorX` 60 in attached shape → flush left, right joint 12;
  - right-aligned in attached shape → flush right;
  - right-aligned floating with radius 12 and gap 4 → right room is `Padding − Gap − Radius` clamped at
    0, so no right joint;
  - 1366 output, 380-wide right-aligned → flush right.
- [ ] **Step 2: Failing pixel tests:** asymmetric joints paint 12 and 5 wide wedges; a flush-right panel
  paints no right joint and a screen-edge wedge below its bottom-right corner.
- [ ] **Step 3:** Implement. `panelReveal` scales each joint by opacity, as it scales `Fillet` today.
- [ ] **Step 4:** `go test ./internal/shell/ ./internal/render/ -count=1`.
- [ ] **Step 5:** Commit: `feat(panels): full concave joints and flush edge panels`.

### Task 13: One joined ground

**Files:**
- Modify: `internal/shell/panelhost.go` (attached panels: no `Rim`; a 1 px overlap into the bar on the
  attached edge; `BlurShape` from `ui.BlurStrips` of the panel body, joints and flush wedges when
  frosted)
- Test: `internal/shell/panelhost_test.go`

- [ ] **Step 1: Failing tests:**
  - an attached panel's style has a zero `Rim`, and Settings keeps it;
  - the top margin is `BarZone − 1` and the body starts at y=0 (top edge; mirrored for bottom);
  - the blur shape covers body, joints and flush wedge in frosted style, and is nil in solid.
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** `go test ./internal/shell/ -run Panel -count=1`.
- [ ] **Step 4:** Commit: `feat(panels): join attached panels to the bar as one ground`.

### Task 14: Panel blur prefers the compositor

**Files:**
- Modify: `internal/shell/panelhost.go:986` (`panelSpec`: when `blur-behind` is on and
  `blurAvailable`, set `BlurShape` and leave `BlurRegion` (capture) nil; otherwise today's capture)
- Test: `internal/shell/panelhost_test.go`

- [ ] **Step 1: Failing test:** with capability, `panelSpec` has no capture region and a non-nil
  `BlurShape`; without it, the capture region is today's.
- [ ] **Step 2:** Implement. `rootStyle` keeps `PanelStyle` for a detached blurred panel, as today.
- [ ] **Step 3:** `go test ./internal/shell/ -run Panel -count=1`.
- [ ] **Step 4:** Commit: `feat(panels): use compositor blur when available`.

## Phase E: documentation and gate

### Task 15: Niri documentation

**Files:**
- Create: `docs/niri-blur.md` (the requirement, namespaces, xray advice, a `layer-rule` example for
  `sysc-shell-panel`)
- Modify: `README.md` (one line linking it)

- [ ] **Step 1:** Write the page from design D11.
- [ ] **Step 2:** Commit: `docs: niri blur configuration`.

### Task 16: Gates and completion handover

- [ ] **Step 1:** Per-package tests with `GOMAXPROCS=4`, `gofmt -l .`, `go vet ./...`,
  `git diff --exit-code -- go.mod go.sum`.
- [ ] **Step 2:** Owner runs the live gate in the design on Niri 26.04+.
- [ ] **Step 3:** Write `docs/plans/<date>-bar-surface-and-attach-completion-handover.md` with gate
  output and live observations, and register it.

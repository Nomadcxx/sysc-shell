# Bar work 2026-09-25: execution handover

Date: 2026-09-25. Covers everything from one working session. It covers:

- the centre pill;
- the variable-font weight fix;
- the survey of DMS, Noctalia and Caelestia bars;
- the bar surface and attached panels design and plan;
- Phase A of that plan.

It commissions Phases B–E. Status lives in bd, and none of these issues exist
there yet. Create them first (see "Tracking").

Line numbers were checked on `bar-surface` at `e06cf93`. Names
are stable, but line numbers drift.

Read this first, then:

- `2026-09-25-bar-surface-and-attach-design.md` (D1–D11)
- `2026-09-25-bar-surface-and-attach.md` (Tasks 1–16)

---

## 1. Branches, PRs and commits

| Branch | PR | Base | State |
|---|---|---|---|
| PR #13's branch | #13 | `main` | The centre pill. **Its tip is wrong.** It points at `3f5c2fa` (Phase A), because Phase A was pushed there before the owner asked for a separate branch. A force-push back to `294c708` was refused by the session's permission guard. |
| `bar-surface` | #15 | `main` | A superset of #13, plus the launcher/clipboard amendment, Phase A, and this handover. |

Fix #13 in one of two ways:

- `git push --force-with-lease origin 294c708:<the PR #13 branch>`,
  then merge #13, then #15; or
- close #13 and merge #15 alone.

Commits on `bar-surface`, oldest first:

| Commit | What |
|---|---|
| `8a82a7c` | docs: centre pill design (`2026-09-25-centre-pill-design.md`) |
| `6eec0d4` | feat(shell): centre pill, `RoleFigure`, variable-font weights, `PaddingX`, `FillOutlineVariant`, separator `Height` |
| `7040ae1` | docs: bar surface and attach design and plan |
| `294c708` | **owner:** fix(ui): respect `PaddingX` in `CheckFit`, `columnChildHeight` and `placeColumnChild` |
| `fc7d8e2` | docs: the launcher and clipboard stay floating |
| `0d93063` | Phase A Task 1: vendored `ext-background-effect-v1.xml` and the generated binding |
| `4f23a17` | Phase A Task 2: optional global, `Capabilities` callback, `Registry.SetCapabilities` |
| `9fd26d9` | Phase A: concave fillet shape fix; coverage maths moved to `internal/ui/coverage.go` |
| `0e821fd` | Phase A Task 3: `ui.SurfaceShape`, `ui.BlurStrips` |
| `3f5c2fa` | Phase A Task 4: `HostCallbacks.BlurShape` applied in `renderJob` |
| `e06cf93` | this handover (first version) |

The remote branch was rewritten once during the session (the same content
under new hashes, plus `294c708`). Always `git fetch` and rebase before
pushing. Never force over someone else's commits.

---

## 2. Owner decisions, in order

1. **The centre pill uses option A, "hairline split".** It is one outlined
   pill: the SYSC mark, a hairline, the time and the date. The whole pill is
   the control-centre button. It was picked from five mockups (A hairline, B
   nameplate, C gradient rim, D balanced, E hanging tab). E, the hanging tab,
   was noted as the "signature" follow-up.
2. **Font parity with the mockup is required.** This led to the variable-font
   weight fix (§3.2).
3. **The frosted bar is the default.** "No frost" (solid) and floating islands
   are options in Settings.
4. **Segmented groups:** the owner believed they existed. They do not (§6.4),
   so they are deferred.
5. **The attached bar is wanted, "for a very long time".** The first ask was
   concave joints for every panel except Settings. **Amended: the launcher
   and clipboard also stay floating**, because attaching them would confuse
   users.
6. **The work is split into design, plan, then implementation.** Phase A is
   done. Stop there for review.
7. **New work goes on a new branch, not the #13 branch.**

---

## 3. Work completed

### 3.1 Centre pill (#13, `6eec0d4`)

Design: `2026-09-25-centre-pill-design.md`. Mockups and a shell-render
comparison: `assets/2026-09-25-centre-pill-design/centre-pill-mockups.html`.

**Problem fixed.** `clockFloorFor` gave every clock a "Wed 30 Sep" width floor
when two clocks sat next to a wordmark, so "15:04" ended up in a pill about
35 px too wide. The mark was 19 px tall and bare. Only the mark opened the
control centre.

**How it works:**

- A bar `group` whose members include `wordmark` is built by
  `buildCentrePill` (`internal/shell/centrepill.go`) as one capsule. Any other
  group is unchanged.
- The capsule has `Key` "centre", `ShapeMedium`, `Padding` = `CapsulePadding`,
  `PaddingX` = 11, and `Stroke` 1 with `FillOutlineVariant`.
- It carries `Action` = `panelControlCenterAction`, `Name` "Control centre",
  `Role` "button", and a tooltip set on refresh with the format
  `"Monday 2 January 2006"`.
- **Row:** the mark at 11 px (80 × 11, animated gradient, decorative: no
  action or name); a `KindSeparator` with `Height` 13
  (`centreRuleHeight = centreMarkHeight + 2`); the first clock in
  `RoleFigure`; later clocks in `ToneSubtle`. The row gap is `MarginM` (9).
  Clocks get no width floor.
- **The hairline must be a member.** `visibleWidgetNode`
  (`internal/shell/bar.go:162`) rebuilds a group's row from `members` on every
  layout, so a node that is not a member disappears. That bug was caught by
  the bar-level test, not the widget test.
- **The default centre** is `group{wordmark, clock "15:04", clock "Mon 2 Jan"}`
  then `media`.
- `ArrangeBar`'s anchored path (`internal/ui/bar.go`, `anchorsWordmark`) now
  also anchors a capsule whose row holds the wordmark. The pill stays centred
  while media appears beside it.

**New primitives:**

| Primitive | Where | Notes |
|---|---|---|
| `Node.PaddingX` | `internal/ui/tree.go`, used via `ui.CapsulePadX` | the owner's `294c708` extended it to `CheckFit` and column layout |
| `FillOutlineVariant` | stroke colour → `style.outlineVariant()` | not exposed to plugins |
| row `KindSeparator` honours `Height` | `internal/ui/layout.go`, `ownHeight` | |
| `theme.RoleFigure` | 15 px / 600, appended after `RoleDisplay` | `textRoleCount` follows it; `applyBarFontSize` (`internal/shell/theme.go:251`) sets bar font size on body and figure |

**User config migration.** A config with separate `clock`, `wordmark`,
`clock` items keeps the old layout, including its floor. The owner's
live config may be like that. Replace the centre with:

```json
"center": [
  {"id": "group", "items": [{"id": "wordmark"}, {"id": "clock", "format": "15:04"}, {"id": "clock", "format": "Mon 2 Jan"}]},
  {"id": "media"}
]
```

**Deferred:**

- An "open" state on the pill (an accent wash and `Outline` stroke while the
  control centre shows). The bar has no "my panel is open" signal yet.
- The live DP-1 check; the owner will run it.

### 3.2 Variable-font weights (`6eec0d4`)

`fontscan` indexes a variable font once, at its default instance. Inter
Variable, the default family, therefore painted every weight at 400.

`FontMap.atWeight` (`internal/render/fontmap.go`) builds one instance per
`(*font.Font, weight)`, calls `SetVariations(wght)`, and keeps the scanned
face when the coordinates are all zero or the font is static.

**Each instance needs its own copy of the `Font` value** (`inst :=
*face.Font`). The go-text `HarfbuzzShaper` caches its HarfBuzz font per
`*font.Font`, built from the first face it sees, so instances that share a
`Font` shape with the first instance's advances.

**Proof:** `TestVariableFontResolvesTheRequestedWeight` runs on
`internal/render/testdata/InterVariable-digits.ttf`, an 11 KB `pyftsubset` of
Inter 4.1 covering digits and a colon, with `OFL-Inter.txt` alongside. It fails
without the `Font` copy.

Inter keeps its tabular figures at one width across the weight axis, so the
test measures proportional figures.

**Consequence:** every role above 400 now paints heavier across the shell:
labels at 500, titles and headlines at 600, and bold runs at 700.

### 3.3 Phase A: compositor blur foundation (#15)

- **Task 1.** `protocols/ext-background-effect-v1.xml` comes from
  wayland-protocols 1.45, SHA-256
  `9aa5011f38752c0146014f8569f3d3981db7dbb8c04e578a80beef6c98fa9653`. The
  binding lives in `internal/platform/wayland/backgroundeffect/` and is
  generated with `sysc-wayland-scanner@v0.1.1`.
- **Task 2.** `internal/platform/wayland/capabilities.go` holds
  `Capabilities{Blur}`, `blurCapable(flags) = flags&1`, and `capabilityState`,
  which reports the first event and afterwards only changes.
  - The manager is bound in `bindGlobals` (`client.go:371`) before the
    outputs, so the capability reaches the shell before the first `NewHost`.
  - It is optional in `interfaceMaximum` (version 1), like screencopy.
  - `Callbacks.Capabilities` is wired in `cmd/sysc-shell/main.go`.
  - `Registry.SetCapabilities` (`internal/shell/registry.go:1940`) stores the
    flag, and `blurAvailableLocked()` reads it. **Nothing restyles on a change
    yet.** That is Task 7.
- **Fillet fix.** `filletCoverage` drew a convex quarter disc centred on the
  junction. `ui.FilletCoverage` now centres the arc a radius away from both
  edges, so it runs tangent into each.
  - `internal/ui/coverage.go` holds `RoundedInset`, `FilletCoverage`,
    `FilletExtent` (any coverage, used by painting and clearing) and
    `FilletSpan` (≥ 128, used by regions).
  - `internal/render/canvas.go` calls them.
  - The existing render tests still pass.
- **Task 3.** `ui.SurfaceShape{Body, Radius, AttachEdge, JointLeft,
  JointRight, EdgeFillet, EdgeLeft, EdgeRight}` and `ui.BlurStrips` produce
  per-row spans, unioned and merged vertically.
  - One `EdgeFillet` with side flags serves both the attached bar's ends and
    flush panels. The plan's older wording ("EndFillets", "Flush*") predates
    this; follow the type.
- **Task 4.** `HostCallbacks.BlurShape func() []ui.Rect`
  (`internal/platform/wayland/host.go`) is read by `applyBlurShape`
  (`regions.go:129`), which is called in `renderJob` (`client.go:896`) after
  `Render` and before `Commit`, for the bar and aux panels alike.
  - `blurRegionUpdate` (`regions.go:118`) sends only on change and clears
    when the region empties or blur is lost.
  - The effect object is created lazily, and its cleanup is pushed after the
    surface's, so it unwinds first; the cleanup also nils `u.effect` and
    `u.blurRects`.
  - **No surface sets `BlurShape` yet.**

---

## 4. Remaining work, task by task

Default choices below follow the design. Where this section adds detail the
plan lacks, this section wins.

### Tracking (do first)

Create one epic and five issues (Phases A–E; Phase A can be closed at once,
citing #15). Add these follow-ups as discovered work:

- the pill lift scaled by wallpaper luminance;
- segmented bar groups;
- the centre-pill open state;
- the workspace track (§6.2);
- the live centre island (§6.2);
- the hanging-tab centre element (§6.3).

### Phase B: bar styles

**Task 5, config** (`internal/config/config.go:138` for `Bar`, `:410` for
`Default`, `:484` for `deriveBar`; `internal/config/load.go:81` for `wireBar`;
`write.go`).

- Add `Style string`, `Shape string`, `FrostOpacity int`, `PillOpacity int`
  and `Overhang int`, the last derived and never written.
- JSON keys under `bar`: `style`, `shape`, `frost-opacity`, `pill-opacity`.
- Option vars: `BarStyles = []string{"frosted","solid","islands"}` and
  `BarShapes = []string{"attached","floating"}`, for the settings registry.
- Defaults: frosted, attached, 65, 70. Ranges 40–100. Error messages name the
  path and the allowed values, like the existing `bar.edge` error.
- `deriveBar` sets `Overhang` = the theme fillet (12) when attached, else 0.
  The fillet currently lives only in `internal/shell/theme.go:187`
  (`Fillet: 12`). Move it to a `theme` package constant (say
  `theme.FilletRadius`) that `config` can read.
- `RebaseDerivedBar` must carry `Overhang` too.
- `ForConnector` (per-output overrides) should inherit the four fields.

**Task 6, settings** (`internal/settings/registry.go`).

- Entries go next to `bar.edge` (line 32): `bar.style` and `bar.shape`
  (`KindEnum`, Group "Surface"), and `bar.frost-opacity` and
  `bar.pill-opacity` (`KindInt`, Group "Frost").
- These are `bar.*`, so they reset against `config.Default()` (the writer's
  `barDiff`). Do **not** add them to `presetAxisPaths` (line ~476).
- Relabel `appearance.bar-opacity` (line 233) to "Solid bar opacity", and its
  describe text to "How opaque the bar is when Style is Solid."
- Reword `appearance.blur-behind` (line 262): "Blur what is behind a panel.
  Uses the compositor on Niri 26.04 and later."
- Style's describe text: "Frosted needs Niri 26.04 or later; elsewhere it
  looks Solid."
- The settings pane renders from the registry, so no UI code is needed. Check
  that `SectionNames()` (line 525) still lists "Bar" first.

**Task 7, theme and capability** (`internal/shell/theme.go`).

- **Keep theme resolution a pure function of config.** Several entry points
  resolve a theme: `ResolveTheme:149`, `ThemeFrom:420`, `DefaultTheme:394`,
  `resolveOutputTheme` (`registry.go:801`).
- Add one post-step, `func (t Theme) WithCompositor(blur bool) Theme`, that
  sets:
  - `t.Frosted = blur && style != solid && !highContrast`;
  - `Surfaces.Bar`: frosted → frost alpha (floor 40); islands → 0; solid or
    no blur → today's `opacityAlpha(BarOpacity, false)`;
  - `t.PillAlpha`: 255 for solid; the pill alpha (floor 40) when frosted or
    islands and capable; the 80 floor when islands without blur.
- Call it everywhere the registry builds a bar or panel theme: `NewHost`
  (`registry.go:982`), the reload path, `panelThemeFor` (`registry.go:805`),
  and the live palette path (`popout_wallpaper.go:1276`, `bar.retheme`).
- `SetCapabilities` must re-run it and call `bar.retheme` (and `h.retheme` for
  open panels, as `registry.go:1924` does) when the flag changes, then
  invalidate.
- **Pill colour:** in `barStyle` (`bar.go:952`), set `style.Capsule` to the
  capsule mixed toward `Foreground` by `(1 − α) × 0.3`, with alpha `α`.
  `fillPair` (`render/paint.go:1006`) reads `style.Capsule` for bar capsules,
  so no painter change is needed.
- **The centre pill's hairline stroke is `FillOutlineVariant`,** which is
  opaque. On a translucent pill it may read heavy. Check it live; if so, mix
  it at the pill alpha.
- `BackgroundOpaque()` (`theme.go:571`) is already false below 255, which
  turns off the opaque-region hint.

**Task 8, the bar's blur shape** (`internal/shell/bar.go`;
`registry.go:1821` for the bar's `HostCallbacks`).

- `func (b *Bar) blurShape() []ui.Rect` takes `b.mu`.
- **Solid:** nil.
- **Frosted:** `ui.BlurStrips` of the body:
  - floating: `Radius` = theme radius, no `AttachEdge`;
  - attached: `Radius` 0, `AttachEdge` = the bar edge,
    `EdgeFillet` = `Overhang`, both ends.
- **Islands:** for each visible top-level node of `b.sections()` (line 197)
  that is a `KindCapsule` with non-zero bounds, the strips of that capsule's
  rounded rect. The radius comes from `style.Shapes.For(n.Shape,
  style.Radius)` (`render/style.go:136`). `ShapeHalf` means half the short
  side. Concatenate, then rely on `mergeRows` or send as is.
- **Coordinates are surface-local logical,** the same space as
  `bodyLocked`/`contentLocked` (lines 415 and 406).
- Absent media collapses its capsule to zero bounds, so it drops out. Test
  this.

### Phase C: attached bar

**Task 9, geometry.**

- `config.Bar.Extent()` (`config.go:170`) becomes the body height when
  attached, and today's `Gap + Body` when floating. Add
  `SurfaceExtent() = Extent() + Overhang`.
- The platform's `surfaceHeight` (`host.go:183`) uses `SurfaceExtent()`.
  `applyGeometryRequests` (`client.go:688`) keeps the exclusive zone from
  `ExclusiveZone()`, which follows `Extent()`.
- `hostRegionGeometry` (`regions.go:22`): an attached body is
  `{0, 0, W, Body}` for a top edge and `{0, Overhang, W, Body}` for a bottom
  edge. `inputRect` (`regions.go:17`) must return the body only when attached;
  today it returns the whole surface.
- **The shell must agree with the platform.** Update `bodyLocked`
  (`bar.go:415`) and `Theme.Geometry` (`theme.go`, `barGeometry`) the same
  way. `exclusiveBarZone` (`panel.go:133`) feeds panel placement, so panels
  sit at the body edge, not past the overhang.
- A bottom edge mirrors everything. Left and right bars are gated
  (`supportedEdges`, `config.go:405`).

**Task 10, painting the ends** (`render/paint.go:160` for `Paint`,
`canvas.go:171` and `:204`).

- Add `Style.EdgeFillet int`, `Style.EdgeLeft bool`, `Style.EdgeRight bool`,
  mirroring `SurfaceShape`.
- Draw each wedge below the far edge with `ui.FilletCoverage(x, y, r)`:
  - x is the distance from the output edge inward;
  - y is the distance from the body's far edge outward.
- Extend `clearOutsideRoundedRect` so it keeps those pixels.
- An attached bar paints with `Radius` 0.
- Pixel tests: top and bottom edges, left and right ends.

### Phase D: panels

**Task 11, placement** (`panelhost.go:599` `spawnPanelLocked`; `panel.go:61`
`Placement`).

- Today:
  - line 628: Settings aligns centre (and attaches);
  - line 634: Launcher and Wallpaper are `CenterY`;
  - line 637: Clipboard is `CenterY` with `BarZone` 0 (a true modal).
- **After:**
  - Wallpaper drops `CenterY` and attaches, centred.
  - Settings becomes `CenterY` (floating).
  - Launcher and Clipboard are unchanged (floating).
- Islands style, or no bar on that output: add `Placement.Detached`. It has no
  joints and no `AttachEdge`, a `Gap` of `theme.MarginS`, and keeps
  `PanelStyle` and its rim.
- `render` (`panelhost.go:1144`) sets `style.AttachEdge` only when not
  `CenterY` and not `Detached`. `rootStyle` (line 951) uses
  `AttachedPanelStyle` only for attached panels without a capture backdrop.

**Task 12, per-side joints and flush panels.**

- Replace `filletMargin` (`panelhost.go:1031`). Today it caps at
  `Panels.Padding − BarGap` = 4, which is why joints were invisible. The
  Control Centre alone computed real room.
- New `Placement.Joints(shape, barRadius, fillet)` returns
  `(left, right int, flushLeft, flushRight bool)`.
  - Room on a side is the distance from the panel's edge to where the bar's
    straight edge ends. Attached: the output edge. Floating: body end (gap)
    plus the bar radius.
  - Each joint is `min(fillet, room)`.
  - Attached, a panel whose edge would land within `fillet` of the output
    edge snaps flush (x = 0 or W − w) with no joint on that side. Its far
    corner on that side gets the screen-edge wedge (`EdgeFillet`), and its
    surface grows by `fillet` on the far edge.
- The surface is `Panel.W + left + right` wide, and the body is inset by
  `left`, not by a symmetric margin as at lines 1099 and 1160.
- The input region is the body only (see `AuxUpdate.SetInputRegion` in
  `aux_surface.go`).
- `panelReveal` (line 1127) scales each joint by opacity.
- Render `Style` gains `JointLeft`, `JointRight`, `EdgeFillet`, `EdgeLeft`
  and `EdgeRight`; `fillAttachFillets` (`canvas.go:204`) paints each side at
  its own width.
- **Session and Notifications** are right-aligned (line ~631), so they are the
  flush cases on normal outputs. Test a 1366 px output.

**Task 13, one joined ground.**

- Attached panels set no `Rim`. Line 1186 sets `style.Rim` for everything but
  Audio; the audio exception exists because a rim reads as a seam. Settings
  and detached panels keep the rim.
- Add a 1 px overlap: the top margin becomes `BarZone − 1` (bottom mirrored).
- `BlurShape` is `ui.BlurStrips` of the panel's shape when the theme is
  frosted.
- An attached panel already resolves at the bar alpha (`AttachedPanelStyle`,
  `theme.go:562`), so with frost the two surfaces share the frosted ground.

**Task 14, compositor panel blur** (`panelSpec`, `panelhost.go:958`).

- With `BlurBehind` on and capability present, set `BlurShape` and leave
  `BlurRegion` (the capture) nil. Otherwise keep today's capture.
- `opacityAlpha(…, blurred=true)` already lowers the panel floor to 60.

### Phase E

**Task 15.** Write `docs/niri-blur.md`:

- Niri ≥ 26.04 (earlier versions reject the config).
- Namespaces `sysc-shell:bar` (`client.go:26`) and `sysc-shell-panel`.
- Xray is fine for the bar. For panels:

```kdl
layer-rule {
  match namespace="^sysc-shell-panel$"
  background-effect { xray false }
}
blur { passes 2; offset 3.0; noise 0.03; saturation 1.0 }   // optional tuning
```

- With xray, rounded anti-aliased edges can show faint wallpaper-coloured
  pixels. Noctalia documents the same.

**Task 16.** Gates:

- per-package tests with `GOMAXPROCS=4`;
- never `./...` with `-race` on the owner's machine (it has hard-locked it);
- `gofmt`, `go vet`, go.mod/go.sum unchanged.

Then the owner's live gate (the design's "Live gate"), then a completion
handover.

---

## 5. Facts not to rediscover

- **Capability bit.** The 1.45 XML says `blur = 0`; the wire mask is `1`.
  Noctalia notes the same (`src/wayland/wayland_connection.cpp`,
  `kExtBackgroundEffectBlurCapabilityMask = 1`).
- **One effect object per `wl_surface`.** A second is a protocol error.
- **Blur regions are integer rects in surface-local logical coordinates.** The
  compositor clips them to the surface.
- **Blur strength is the compositor's** (Niri `blur {}`). The protocol has no
  radius. `appearance.blur-radius` only drives the CPU capture fallback.
- **Namespaces:**
  - bar `sysc-shell:bar`
  - panels `sysc-shell-panel`
  - shield `sysc-shell-shield`
  - OSD `sysc-shell-osd`
  - toasts `toastNamespace` (`toasthost.go`)
- **Every panel** gets a shield and `keyboardExclusive`. The clipboard is not
  special in that respect.
- **Default metrics:** density standard, `Bar.Height` 48, `Gap` 4, body 40,
  pills 28 tall, radius 12, capsule padding 6, bar padding 6, spacing 4,
  `Theme.Fillet` 12.
- **Fallback palette tokens** (for mockups and tests):
  - Surface `#1d2025`
  - SurfaceContainerHigh `#3a4149` (bar capsule)
  - OnSurface `#e6e6e6`
  - OnSurfaceVariant `#9aa0a6` (`ToneSubtle`)
  - Outline `#737d89`
  - OutlineVariant `#59616b`
  - Primary `#0080ff`
  - Secondary `#bec6dc`
  - Tertiary `#ddbce0`
- **Opacity floors:** `theme.OpacityMin` 80, `OpacityMinBlurred` 60 (panels
  behind blur), new frost floor 40.
- **The literal gate.** A test in `internal/shell/surfacerole_test.go`
  fails literal `Gap:`, `Padding:`, `Height:`, `Stroke:` and similar in shell
  sources unless the line carries `// token-exempt: <reason>`. It also
  rejects `Bold: true`; use a text role.
- **The commit-msg hook** on the owner's machine rejects `agent`, `cursor`,
  `codex`, `llm`, `both`, `Hallmark` and similar words.
- **Four `internal/shell` tests fail in a container** without a battery or
  running as root, identically on `main`:
  - `TestABatteryWidgetOpensTheSessionPanel`
  - `TestRightClickingTheBarBatteryOpensSession`
  - `TestRightClickingBatteryCapsulePaddingOpensSession`
  - `TestPanelSectionValidationPrecedesMutation`

  Also container-only: `tests/integration` tray tests (19), and
  `TestReadOnlyNiriConfigIsReportedNotRewritten` and
  `TestEngineDoesNotRemoveReplacedSocket`. Session-popout fit tests fail when
  Inter is not installed.

---

## 6. Research record

### 6.1 Reference shells read on 2026-09-25

**Noctalia v5** (`noctalia-dev/noctalia-shell` `2734393`, now C++).

- `src/config/config_types.h` `BarConfig`: `background_opacity`, `border`,
  per-corner radius, `concaveEdgeCorners`, `marginEnds`, `marginEdge`
  (floating), `shadow`, `contactShadow` (a gradient between an attached panel
  and the bar), `panelOverlap` (1 px), `capsuleThickness` 0.76,
  `widgetCapsuleOpacity`, `widgetCapsuleBorder`, `widgetCapsuleGroups`,
  `hoverHighlight`, `autoHide`/`smartAutoHide`, dead-zone actions.
- `src/shell/bar/bar.cpp` `applyBarCompositorBlur` tessellates the bar shape,
  concave corners included (`Surface::tessellateShape`), into a blur region.
- `docs/user/compositor-settings/niri.mdx` covers blur (Niri 26.04), xray
  advice and layer rules.

**DMS** (`AvengeMedia/DankMaterialShell` `10e8086`, QML).

- `Modules/Settings/DankBarAppearanceTab.qml` has:
  - Frame mode (a screen-edge frame, the bar inside it);
  - corner style Rounded, Flush or Square, plus "Goth corners" (concave);
  - Island mode with "satellites";
  - bar length by percentage;
  - widget style pills, segments or flat;
  - widget opacity and outline.
- `Modules/DankBar/BarMetrics.qml`: `segmentInnerRadius` = `cornerRadiusS`,
  `pressedRadius` (the press "squish" morph), `segmentGap`.
- `Modules/DankIsland/`: a dynamic island with `MediaCompact`,
  `NotificationCompact` and `SystemLevelCompact` activities.
- `Widgets/WindowBlur.qml` handles compositor blur.

**Caelestia** (`caelestia-dots/shell` `20e625d`, QML, Hyprland).

- A vertical bar in a screen border; drawers grow out of it.
- `services/Colours.qml` `layer()`/`alterColour()`: translucent nested layers
  are lifted by an offset scaled by wallpaper luminance.
- `modules/bar/components/workspaces/OccupiedBg.qml`: adjacent occupied
  workspaces merge into one shape.
- `ActiveIndicator.qml`: leading and trailing edges animate with different
  durations, a stretch.
- `Bar.qml`: hover popouts follow the hovered item; scroll changes
  workspaces, volume (top half) and brightness (bottom half).

### 6.2 Survey output: six upgrades

Mockups: `assets/2026-09-25-bar-surface-and-attach-design/bar-survey-mockups.html`.

1. **Frosted bar.** Designed as D1–D3. Mock values: bar 62%, pills 55%, blur
   18 px, luminance lift on, a 1 px white rim at 7–9% and a soft shadow.
2. **Floating islands.** Designed as D1.
3. **Segmented groups:** a 2 px gap, 5 px inner corners, full outer corners,
   and a press squish to 6 px. **Deferred.** Needs per-corner radius in
   `RoundedMask`/`StrokeRounded`, which Task 12's per-side joints do not give.
4. **Attached bar.** Designed as D5–D8.
5. **Workspace track.** Not designed.
   - Sketch: occupied runs share one `caph` track 16 px tall.
   - The indicator's leading edge takes 180 ms (`cubic-bezier(.2,0,0,1)`) and
     its trailing edge 420 ms (`cubic-bezier(.3,0,0,1)`). Number-free, as
     `workspacepills.go` requires.
   - The Niri projection already carries occupied and urgent.
6. **Live centre island.** Not designed.
   - The centre pill grows in place: notification for 4 s, media (the time
     stays, then art and a title, plus a 2 px progress hairline), volume for
     1.8 s (an inline 90 px slider and a percentage, replacing the bottom OSD).
   - The anchored layout already keeps it centred.

Also seen and not pursued: auto-hide, scroll actions on the bar, Noctalia's
hover tint, DMS frame mode, and dead-zone actions.

### 6.3 Centre element alternatives (first mockup page)

- **B nameplate:** the mark on an inset `FillSoft` plate.
- **C gradient rim:** the gradient moves to the 1 px stroke. Needs a gradient
  stroke in `paintChrome`.
- **D balanced:** the time, mark and date in equal slots, with the date
  shortened to "Thu 25".
- **E hanging tab:** 44 px tall, square top, 13 px bottom radius, and the
  control centre grows out of it seamlessly. The most distinctive option. It
  needs per-corner radius and a surface taller than its zone, both of which
  Phase C and D now largely provide.

### 6.4 Segmented: what exists

`KindSegmented` (`internal/ui/layout.go` `measureSegmented`/`layoutSegmented`)
is a row of equal-width buttons, each its own stadium. It is used only for tab
rows inside panels (audio, network, wallpaper, notifications, process, power
profiles). Bar `group` is one capsule with flat members.

---

## 7. Environment notes for the next session

- **Container network:** GitHub, the Go proxy, PyPI and Ubuntu archives work.
  `gitlab.freedesktop.org` and jsDelivr are blocked. The protocol XML came
  from `apt-get download wayland-protocols` (1.45 in noble-updates).
- **`bd` is not installed in the cloud container.** Create issues on the
  owner's machine.
- **Rendering the real bar offscreen** for visual checks:
  1. Copy `InterVariable.ttf` into `/usr/local/share/fonts`.
  2. Write a throwaway `_test.go` in `internal/shell` that builds
     `NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-1")`, applies a
     `barView`, calls `Configure(w, cfg.Bar.Extent(), 240)` for 2×, and calls
     `Render` into a `[]byte`.
  3. The buffer is premultiplied BGRA. Convert it to PNG, and delete the file
     afterwards.
- **Inter 4.1** comes from the GitHub release zip `Inter-4.1.zip`. Subset
  fixtures with `pyftsubset --text=... --layout-features='tnum,kern'`.
- **The mockups** are committed under `docs/plans/assets/` as standalone HTML
  (open them in a browser; fonts load from Google Fonts and fall back offline).
  They use the same geometry
  and tokens as §5. The CSS unit
  variable `--u` scales them.
- **The owner reviews PRs** before merging and may rewrite branch history.
  Fetch and rebase before every push.

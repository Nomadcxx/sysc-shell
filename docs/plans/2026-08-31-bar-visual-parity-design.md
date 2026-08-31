# Bar visual parity design

Research: [2026-08-31-bar-visual-parity-research.md](2026-08-31-bar-visual-parity-research.md). Live grim of DMS on this laptop is the visual reference. Noctalia contributes workspace-as-pills and confirms capsules; its live pixels were not captured (package not installed; v4 backup would not start).

This is chrome for widgets we already ship. It is not a DMS or Noctalia widget-roster port, not a lock screen, not a compositor, and not QML compatibility.

## Scope

In:

- Per-item capsules on the bar: padding, radius, theme-aware fill.
- Map `theme.Tokens.SurfaceContainer` onto a `Theme.Capsule` / `ProofStyle.Capsule` fill.
- Workspace as dots inside one capsule: focused = `Accent`, occupied and empty = dimmer fill. Same-output workspaces only.
- Zero-width content (empty title) still measures zero — no empty pill.

Out:

- Launcher, tray, notifications, MPRIS, clipboard, control center, network, VPN, brightness-on-bar.
- New icon font / Material glyphs.
- Click-to-activate workspace (no consumer yet; dots are display).
- Transparent bar with capsules only (Noctalia `capsuleColorKey: none` on a simple bar is not the live DMS look).
- Scale-1.25 measure/paint truncation (recorded in the research doc, not this design).
- Panel popouts (clock/session/monitor/settings). Those are not the bar.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Keep the existing outer slab (height 48, gap 4, radius 12) **and** wrap each bar widget in an inner capsule. | Capsules-only on a transparent exclusive zone — not what the live DMS grim shows. One fused slab — what we have now. |
| D2 | Add `ui.KindCapsule`: one child (or none, for a square dot). Measure = child + `2*Padding` wide, full content-band tall; empty child width 0 → measure 0. Paint `FillRounded` then the child. | Paint-behind existing text bounds without a kind — clips padding and cannot nest the workspace row. Three section-level capsules — not the grim. |
| D3 | Capsule fill is `Tokens.SurfaceContainer` via new `Theme.Capsule`. Accent fill is only for the focused workspace **dot** (`ToneAccent` on that node), not the clock or title capsule. | Reuse `Theme.Muted` after `ThemeFromTokens` — that field is `OnSurfaceVariant` (text colour) and already the meter track. Filling the clock capsule with `Primary` — not the grim. |
| D4 | Capsule padding is a theme token `CapsulePadding` default **8** (logical). Not a new config key this tranche. Bar `Padding` stays the slab inset (6). | Copy DMS's `widgetPadding ?? 12` scaled by thickness — we do not have `widgetThickness`. Copy Noctalia `contentPadding: 2` — owner asked for padding; 2 is flush. |
| D5 | Capsule radius is `Theme.Radius` (already 12). Dots are empty `KindCapsule` with `Width` = 8, `Radius` painted as half the short side so they read as circles. | Unicode `●`/`○` — font-dependent and not the grim. A new `KindDot` — YAGNI; empty capsule is the circle. |
| D6 | `projectOutputs` grows a `[]workspaceDot` per connector (index, occupied, focused) for workspaces whose `Output` matches. Occupied = `HasActiveWindow` or any window with that `WorkspaceID`. The existing `Workspace` string stays for tests that assert the label. | Keep the single label and style it as a pill — not dots. Hide unoccupied — Noctalia `hideUnoccupied: false` and the grim showed four dots. |
| D7 | Wrap every bar widget root in `KindCapsule` at `buildWidgets`. Workspace's inner child is a `KindRow` of dots rebuilt on apply. Empty title: inner text width 0 → capsule measures 0. | Opt-in per item in config — default bar would ship without capsules. Wrapping only clock — misses workspace/title, the actual grim gap. |
| D8 | Do not extend `sysc-icons.ttf` in this tranche. Default items are dots + text. Weather/battery already have glyphs. | Port DMS's icon set. Draw launcher/wifi/bell glyphs with no widgets to put them in. |
| D9 | `applyLocked` gains a `refresh` func on the widget so workspace can rebuild children. Dirty detection must not depend on `node.Text` alone. | Stuff a signature into `Text` and parse it at paint — paint stays dumb; the tree is the model. Always rebuild every apply — wastes layout for clocks. |

## Tree and paint

`KindCapsule` appends to the existing iota. `measureNode` and `Paint` must handle it; leaving it on the `unsupported kind` path is how Super+M dies today for `KindTab`.

Layout of a bar row: each child may be a capsule. Capsule height is the row's padded content height so every pill is the same band (live DMS). The child is centred inside. Nested `KindRow` (workspace dots) is `Layout`'d in the inner box.

Paint: `FillRounded(bounds, min(radius, half short side), fill)` then recurse. Fill is `ProofStyle.Capsule` unless `Tone == ToneAccent`, then `ProofStyle.Accent`. Bar chrome (`style.Body`) is still `Background` (`Surface`).

Hit already recurses children; a capsule with no `Action` falls through to `""` unless a child has one. Workspace stays display-only.

Column layout does not need capsules this tranche. If a capsule is placed in a column, `columnChildHeight` must not hit `unsupported kind` — treat it like a padded child. Cheaper than another crash.

## Theme mapping

```
Surface            → Background   (slab)
SurfaceContainer   → Capsule      (pills)   // new
OnSurface          → Foreground
OnSurfaceVariant   → Muted        (track; unchanged)
Primary            → Accent       (focused dot, meters)
Error              → Error
```

`DefaultTheme` Capsule is `#181a1d` (the compiled-in `Fallback.SurfaceContainer`). CapsulePadding is 8.

`ProofStyle` gains `Capsule Color`. `Bar` copies it from `theme.Capsule` next to the existing fields. OSD and panels do not wrap in capsules.

## Amendment 2026-08-31: numbered workspace pills

The owner chose Noctalia-style numbered pills over DMS-style bare dots after seeing both live.
This supersedes D5's "no numerals" line and the D6 fill and width table below. Everything else in
D5 and D6 stands: pills live inside one capsule, in Niri index order, for this output only.

Measured from `assets/2026-08-31-bar-visual-parity/refs/noctalia-bar-idle.png`, which is 1920x1080
physical at scale 1.25.

| Property | Focused | Not focused |
|---|---|---|
| Fill | `#37F499`, the palette `Primary` | `#1183A2`, the palette `PrimaryContainer` |
| Numeral | `#212337`, `OnPrimary` | `#212337`, `OnPrimaryContainer` |
| Physical size | 40 x 20 | 17 x 19 |
| Shape | 2:1 stadium | 1:1 circle |
| Gap | 9 to 10 physical, 8 logical | |

The unfocused fill is a distinct container colour, not `SurfaceContainer`. D6 assumed the capsule
fill; live pixels disagree. `theme.Tokens` already declares `Primary`, `OnPrimary`,
`PrimaryContainer` and `OnPrimaryContainer`; `ThemeFromTokens` maps none of the last three. Mapping
them is the whole colour change.

### Sizing follows proportion, not absolute pixels

Noctalia's bar is about 42 physical, 34 logical, and its pills are 20 physical, 16 logical: close to
half the bar. Copying 16 logical into a 48-logical bar would look undersized, so the pill takes the
capsule's inner band instead.

With bar height 48 and bar padding 6 the content band is 36. A capsule at `CapsulePadding` 8 leaves
an inner band of 20 logical. Therefore:

- pill height is the capsule inner height;
- a focused pill is twice that wide, matching the measured 2:1;
- an unfocused pill is square, matching the measured near-1:1, and paints as a circle;
- the radius is half the short side, so each is fully rounded;
- the gap between pills is 8 logical.

### Consequences for the plan

- Task 5's expectation of widths 8, 8 and 6 no longer holds. Occupancy no longer changes width;
  focus does. Occupied and empty differ only if a later pass gives them different fills.
- A pill is a `KindCapsule` with a `KindText` child holding the index, not an empty capsule. The
  empty-capsule dot from D5 keeps its measure rules and loses its only consumer.
- `ToneAccent` alone cannot express this. A pill needs a fill and a matching numeral colour, so the
  capsule needs a fill selector rather than a single accent flag.

## Workspace dots

Per output, in Niri index order, one empty `KindCapsule` per workspace:

| State | Fill |
|---|---|
| Focused | `ToneAccent` → `Accent` |
| Occupied, not focused | `Capsule` (or a slightly brighter sibling if one pixel-test needs it — start with Capsule, dimmer than Accent is enough on the grim) |
| Empty | `Capsule` at the same fill; smaller `Width` (6) so occupied reads heavier, matching Noctalia `pillSize` without a second colour token |

Do not draw index numerals on empty dots (`showLabelsOnlyWhenOccupied` on the live Noctalia config; DMS grim is dots only).

## Widget wrap

`buildWidgets` returns the capsule as `textWidget.node`. The current text/meter/graph node becomes the capsule's child. `format` still writes `child.Text`. Workspace sets `refresh` and leaves `format` unused.

`copyNode` already deep-copies children, so `renderViewLocked` keeps working.

## Settings

No new registry entries. Capsule padding is a theme constant until a second consumer wants it configurable. Bar height/gap/padding/spacing already exist.

## Acceptance

Unit: capsule measure includes padding; zero-width child → zero; `ThemeFromTokens` maps `SurfaceContainer`; projection emits dots for two workspaces on one output; focused dot is `ToneAccent`; paint writes `Capsule` pixels inside a pill and `Background` on the slab.

Live (owner, not a converted `sysc-3`): grim of sysc-shell default bar on `eDP-1` shows four (or N) distinct inner pills, padded text, focused workspace as an accent disc. Side-by-side with `docs/plans/assets/2026-08-31-bar-visual-parity/dms-bar.png` is the eyeball test. Widget roster will not match DMS; chrome should.

## Follow-ups, not this design

- Measure-at-physical-size so scale 1.25 does not ellipsize the clock.
- `sysc-41` / `sysc-42` panel paint and settings teardown.
- Click workspace → `niri msg action focus-workspace`.
- Icon font when M5/M7 widgets exist.

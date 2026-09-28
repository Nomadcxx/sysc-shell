# Bar tooltip redesign

Bar tooltips look assembled rather than designed: cramped or missing padding, a fixed-width box with
dead space, a near-pill shape with a hard rim, bar-sized regular text with no hierarchy, and an opaque
ground with no blur, beside panels and toasts that are translucent and frosted. This plan moves the
tooltip onto the auxiliary-surface path the toasts already use, and gives it one card style.

Base: `main` at `278deeb`.

## What is wrong, and why

Rendered offline through the real `measureTooltip`/`paintTooltip` at 1.25× (the laptop's scale) with
the default config:

| Symptom | Cause |
|---|---|
| Structured tooltips (weather, plugins) have **no padding**; the first line touches the top edge and the rounded corner clips it. | `paintTooltip` lays a `Root` tree straight into `style.Body` (`internal/platform/wayland/tooltip.go`, `paintTooltip`); the only inset is the tree's own `Padding`. Weather's tree (`weatherTooltipTree`) and every plugin tooltip in sysc-plugins (timer, notes, …) set none. |
| Structured tooltips are always **280 px wide**, mostly empty. | `measureTooltipTree` uses `tooltipMaxWidth` (280) or `root.MaxWidth` as the width; it never shrinks to the content. |
| Text tooltips are **cramped** (6 px inset) and nearly a **pill**. | `tooltipPad = 6`; the height is text height + 12, so a 32 px box with the theme's radius 12 reads as a stadium. |
| A **hard rim** around a small box. | `tooltipStyleFor` strokes `Outline`, a full-strength rim designed for panels hundreds of pixels across. |
| **Bar-sized regular text**, no hierarchy. | The owner paints with the bar's family and size (`tooltipSpec`, Inter 15, weight 400) through its own font map, not the shell's resolved `TypeSet`; a node's text role (a plugin's `Size: "label"` title) has no role table to resolve against. |
| **Opaque**, although `panel-opacity` is 65. | The ground's alpha is `Surfaces.Overlay` (`overlay-opacity`, default 100), not the panel opacity the panels and toast cards follow. |
| **No blur**, although both machines run Niri 26.04 with `ext_background_effect_v1` ("the compositor blurs behind surfaces"). | The tooltip is a hand-built surface outside `surfaceUnit`, so `applyBlurShape` never reaches it. Its only blur is a screencopy capture, and that is painted *under* the opaque fill, so it never shows. |

The shared root cause is ownership. Decision D6 of the weather-and-visual-vocabulary design (2026-08-30)
built the tooltip by hand on the Wayland owner, before the aux-surface path existed in its current form.
The aux path now owns every property the tooltip lacks: compositor blur (`BlurShape`), the captured
backdrop fallback (`AuxSpec.BlurRegion`), fractional scale, and painting with the shell's theme, type
roles and shapes. The toast cards (#28/#30) are the closest existing surface: a small floating card on
the Overlay layer with blur and a rim.

## Decisions

| # | Decision | Instead of |
|---|---|---|
| T1 | The tooltip becomes a shell-owned aux surface, like the toast and OSD surfaces. D6's shape stays: Overlay layer, `exclusive_zone −1`, keyboard none, no dismiss shield. Only the ownership moves: the shell places, lays out and paints; the owner treats it as any aux unit. | Patching the owner's hand-built surface and re-plumbing blur into it, which duplicates what `surfaceUnit` already does. |
| T2 | The ground follows the toast card rule: under compositor blur, `PanelStyle` (the `panel-opacity` alpha, 65 here) with a blur region shaped to the card; without compositor blur, `OverlayStyle` over the captured backdrop when `blur-behind` is on. | Keeping `overlay-opacity` for tooltips. It is a separate setting that users do not expect to govern a bar hover, and it is why the tooltip is opaque today. Owner confirmed 2026-09-28. |
| T3 | Every tooltip gets the same host inset from the density metrics (horizontal `MarginM`, vertical `MarginS`), whether it is text or a tree. Trees do not supply their own padding, and a root `Padding` a tree does set is ignored rather than stacked. | Asking weather and every plugin to add padding, or honouring per-tree padding and getting a different inset per widget. |
| T4 | The card shrink-wraps its content up to a cap: width is the widest laid-out line plus the inset, capped at `lint.TooltipWidth` (280). A single text line longer than the cap wraps (the toast's `wrapLines`) instead of clipping. | The fixed 280 px box. |
| T5 | Shape: the `ShapeSmall` corner role, half the theme radius (6 at the default 12), so a one-line tooltip about 30 px tall reads as a card and not a pill; it still follows the theme radius. Rim: `OutlineVariant`, the quieter outline; `Outline` is kept for panel-sized surfaces. | The theme's base radius (what `ShapeCard` and `ShapeMedium` also resolve to by default) on a ~30 px box, and a full-strength rim. |
| T6 | Type: a text tooltip uses the `label` role. A tree's nodes resolve their own roles and tones through the shell's `TypeSet`, so a plugin's bold label title and subtle second line render as intended. Weather's first line (the condition) becomes a label-role title. | The bar's family and size at weight 400 for everything. |
| T7 | Moving the pointer from one widget to another while a tooltip is showing updates the surface in place (`AuxUpdate` with size and margins) instead of destroying and recreating it, so sweeping along the bar does not flicker. | A close and a fresh open per widget. |
| T8 | Placement keeps D5 (centred on the widget, clamped inside the output), computed in the shell from the output's logical size and the bar zone it already holds for panels. The gap from the bar becomes `MarginS` rather than a hardcoded 4. | The owner-side `tooltipPlacement`. |

## Tasks

Each task is test-first: write the named test, watch it fail for the stated reason, then implement.
Gates per task: `gofmt`, `go build -p 2 ./...`, `go vet ./internal/...`, the package tests, and
`-race` per touched package.

1. **Tooltip card model (shell, pure).** `tooltipCard(text string, root *ui.Node, t Theme, measure) (*ui.Node, ui.Rect)`
   returns the laid-out card and its size. Tests:
   - A one-word text tooltip is inset by `MarginM`/`MarginS` on each side and narrower than the cap.
   - A tree with five short lines is as wide as its widest line plus the inset, not 280.
   - A root `Padding` is ignored.
   - A long single line wraps at the cap.
   - A plugin tree with a `label` title and a subtle line keeps those roles and tones.
   - Weather's condition line is a label-role title.
2. **Placement in the shell.** Port `tooltipPlacement` with its table tests (top and bottom bar, clamping at
   both edges, wider than the output), with the new gap. Test: a tooltip for a widget at the right edge
   of a 1536 logical output stays inside it.
3. **`tooltipHost`: an aux surface.** Build the spec, and the `Configure`/`Render`/`BlurShape` callbacks,
   painted with `render.Paint` and the card style from T2/T5. Tests:
   - Under `caps.Blur` the ground's alpha is the panel alpha, and `BlurShape` returns the card's rounded
     strips.
   - Without it, the ground is `OverlayStyle`, the spec carries a `BlurRegion`, and `BlurShape` is empty.
   - The rim is `OutlineVariant` and the radius is `ShapeSmall`.
   - Painting at scale 1.25 leaves the inset free of glyph pixels.
4. **Dwell drives the host.** The dwell's fire calls the host; it no longer sends `TooltipRequest`s to
   the owner. Tests:
   - A first show opens one aux surface.
   - A second widget while shown sends one `AuxUpdate` with the new size and margins, and no close.
   - Leave, reload and output loss close it.
   - A compositor close (`DropAux` on the tooltip id) forgets it, so the next hover opens afresh.
5. **Remove the owner's tooltip.** Delete `tooltip.go`'s surface, measure and paint code, the
   `tooltipConfigure` seam and `failTooltip` (#36's containment is now `failUnit`'s, like any aux
   surface), `Callbacks.Tooltips`, the wake bridge's tooltip branch, and `tooltipRenderer`/`tooltipFont`
   on the owner. Move any still-meaningful test to the shell side. Build and vet must show nothing left
   referencing them.
6. **Plugin tooltip views.** A plugin's tooltip tree keeps `lint.TooltipWidth`/`TooltipHeight` as its
   cap and gets the host inset inside it. Render the timer, notes and weather tooltips offline (a test
   that paints each card to a PNG in `t.TempDir()` and asserts the inset and the bounds) and check them
   by eye before the live gate.
7. **Live gate.** Deploy with `scripts/deploy --host both` from a clean origin/main worktree. On the
   laptop (1.25×, the reference bench) and the desktop, hover volume, network, battery, weather, a tray
   item and a plugin (timer or notes). Screenshot each and check that:
   - the ground is translucent at `panel-opacity` with the blur visible behind it;
   - the inset is even;
   - there is no dead width;
   - the text sits clear of the corners;
   - sweeping along the bar does not flicker;
   - the journal has no `closing surface` for a tooltip.
   Hovering needs the pointer: the owner drives it, or confirms the laptop is free for `ydotool`.
8. **Records.** In this plan's register row, mark D6's ownership clause as superseded by T1 (the shape
   stands). Open bd issues under one epic per task, and close them with the landing commits.

## Out of scope

- A fade or slide on show. Reduced motion would have to govern it, and it is polish on top of a correct
  card; a follow-up can add it once this lands.
- Rich tooltip content for built-in widgets beyond weather's title line (for example a network tooltip
  with SSID and signal). Content is each widget's own concern.
- Keyboard-triggered tooltips.

# Centre pill design

Date: 2026-09-25. Owner-approved on 2026-09-25 as option A ("hairline split")
of five mocked alternatives. Amends
[the centre clock, media and wordmark design](2026-09-16-centre-clock-media-wordmark-design.md):
D1's separate wordmark button and D2's anchor on the bare mark.

The five options, the diagnosis of the old centre, and the shell's own render
of A against the mock are in
`assets/2026-09-25-centre-pill-design/centre-pill-mockups.html` (published at
<https://claude.ai/artifact/Lpc8oG86pkLDknbeJzZiN1>).

## Problem

The centre read as three objects: a time pill, the bare SYSC mark, and a
date pill. Measured on the default density row:

- `clockFloorFor` gave every clock the "Wed 30 Sep" width floor when two
  clocks and a wordmark shared the centre, so "15:04" sat in a pill about
  35 px wider than its text.
- The mark was 19 px tall beside text whose cap height is about 11 px, with
  no chrome, so it lined up with nothing.
- Only the mark carried `panelControlCenterAction`. The clock pills looked
  like buttons and did nothing, and the mark had no hover state.
- The bar's 4 px spacing, the capsule's 6 px padding and the mark's zero
  padding gave each edge of the group a different gap.

A second defect sat underneath. `fontscan` indexes a variable font once, at
its default instance, and nothing set the `wght` axis, so every weight the
type ramp asked of Inter Variable (the default family) painted at 400. The
go-text shaper also caches its HarfBuzz font per `*font.Font`, so weight
instances that share one `Font` shape with the first instance's advances.

## Decisions

### D1. One outlined pill is the control

A bar `group` whose members include `wordmark` is built as the centre pill:
one capsule holding the mark, a hairline, the time and the date. The capsule
carries the control-centre action, the accessible name "Control centre", the
button role, and a tooltip with the full date ("Friday 25 September 2026").
The mark inside is decorative. Any other group is unchanged.

The default centre becomes `group{wordmark, clock "15:04", clock "Mon 2 Jan"}`
then `media`. A configuration that lists `clock`, `wordmark`, `clock` as
separate items keeps the old layout, including its clock floor.

### D2. Geometry, at 1× on the default row

| Part | Value | Source |
|---|---|---|
| Pill | band height, `ShapeMedium` | existing capsule |
| Stroke | 1 px, `outline_variant` | new `FillOutlineVariant` stroke colour |
| Horizontal padding | 11 px from the outer edge | new `Node.PaddingX` |
| Vertical padding | `CapsulePadding` | unchanged |
| Gap | `MarginM` (9 px) | spacing ladder |
| Mark | 11 × 80, animated gradient | cap height of 15 px Inter |
| Hairline | 1 × 13, `outline_variant` | mark height + 2; `KindSeparator` honours `Height` in a row |
| Time | `RoleFigure`: body size, weight 600, tabular | new role |
| Date | body, `ToneSubtle` | existing tone |

`RoleFigure` is a type role rather than a flag, following the rule that
components name a role and never ask for synthetic bold. The bar's explicit
font size applies to it as it does to body text.

Clocks in the pill get no width floor. Tabular figures already hold the time
still, and Inter keeps its tabular figures at one width across the weight
axis.

### D3. The pill is the anchor

`ArrangeBar`'s anchored path recognises the capsule whose row holds the
wordmark, as well as a bare wordmark. The pill stays on the content band's
centre while media appears beside it, and across minute ticks.

### D4. Variable weights

`FontMap` sets the `wght` axis on a variable face when a request names a
weight. Each weight gets its own instance, cached per font and weight so
every rune of a run resolves to one face, and each instance gets its own
copy of the `Font` value (sharing the parsed tables) so the shaper builds a
HarfBuzz font per weight. A static font, or the default weight, keeps the
scanned face.

This changes every surface that names a heavier role: labels paint at 500 and
titles at 600, as the ramp always specified.

## Proof

- `TestVariableFontResolvesTheRequestedWeight`, on an 11 KB subset of Inter
  Variable 4.1 (OFL, `internal/render/testdata`). It fails without the `Font`
  copy.
- `TestDefaultCentreBuildsOnePillAndMedia`, `TestCentrePillSeparatesTheMarkOnEitherSide`.
- `TestDefaultCentrePillStaysAnchoredAcrossMediaAndClockChanges`, including
  a hit on the time opening the control centre and the hairline surviving the
  bar's member rebuild.
- `TestCapsulePaddingXWidensOnlyTheEnds`, `TestRowSeparatorKeepsANamedHeight`,
  `TestPaintStrokeInOutlineVariantUsesTheQuietBoundary`.

## Deferred

- An open state while the control centre is showing (accent wash, `outline`
  stroke). The bar has no signal for "this button's panel is open" today.
- Media inside the pill. It stays a separate item so the pill does not move.

## Live gate

On Niri `DP-1`, 3440×1440, scale 1.0: capture the default bar with and
without a player, hover the time and the date, click each, and confirm the
labels and titles in the Control Centre now paint at their ramp weights.

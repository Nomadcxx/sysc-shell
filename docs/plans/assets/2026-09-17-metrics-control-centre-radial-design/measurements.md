# Measurements

Source revision: `4119ce5`. Every figure below was produced by laying out the
real `ccHome` tree with the real font stack, not by arithmetic over the
constants. Where a number contradicts a comment in the code, the comment is
wrong and is named.

## Method

A temporary test in `package shell` built `ccHome`, laid it out with
`ui.LayoutColumn` at the real panel geometry, and measured text through
`render.NewSystemFontMap("Inter", …)` and `TextRenderer.Measure` — the same path
`PanelHost.measureText` uses in production. Font scale was moved through
`cfg.Theme.Composition.FontScale` with `config.RebaseDerivedBar` applied, because
`Bar.FontSize` overrides `RoleBody` and a harness that skips the rebase reports
body text that never scales. The harness was deleted after measuring; it is not
a project check. `generate.py` in this directory reproduces the artifacts from
the same constants.

## Fixed surface

| Thing | Value | Owner |
| --- | --- | --- |
| Control Centre panel | 700 × 564 | `panelTargetSize(PanelControlCenter)` |
| Body width | 596 | `700 − 2·16 − 56 − 16` |
| Scroll viewport height | 480 | `564 − 2·16 − 40 − 12` |
| Home page height | 480 | `ccPageH` |
| Left column width | 356 | `ccLeftColumnW` |
| System card | 356 × 88 | `ccLeftColumnW`, `ccCardH` |
| Card padding | 9 at every density | `theme.Metrics.CardPadding` |
| Card interior | 338 × 70 | derived |
| Card radius | 12 | `render.Style.CardRadius` |

Home page budget at font scale 100%: identity 96 + 13 + toggle 48 + 13 +
split 182 + 13 + sliders 113 = **478 of 480**. Two pixels of slack.

## Text metrics, Inter, scale 1.0

Heights are what the layout engine reserves; widths are shaped advances.

| Font scale | title h | caption h | body h | "System" w | "CPU temperature" w | "100%" w | "65°C" w |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 75% | 17 | 12 | 14 | 46 | 74 | 33 | 28 |
| 100% | 22 | 15 | 19 | 60 | 99 | 44 | 38 |
| 125% | 27 | 19 | 24 | 74 | 123 | 56 | 48 |
| 150% | 33 | 23 | 29 | 92 | 148 | 68 | 58 |
| 200% | 42 | 30 | 38 | 120 | 197 | 88 | 75 |

Point sizes are caption 12, body 15, title 17 at 100% (`theme.typeRoles`).

**The title is 22px tall at the default font scale, not 16.** The comment above
`ccResourceRowH` derives the current layout's fit from 16, and
`TestControlCentreHomeSystemGaugesFitInsideCardBounds` passes only because its
stub returns `(len(s)*8, 16)` for every string.

## Gauge primitive

From `paintRadialGauge` in `internal/render/radial.go`:

| Property | Value | Note |
| --- | --- | --- |
| Stroke | `Scale120.Physical(2)`, min 1 | independent of diameter |
| Icon glyph | `Scale120.Physical(11)` | **fixed**, independent of diameter |
| Value text | `Scale120.Physical(8)` | **fixed**, independent of diameter |
| Arc origin | twelve o'clock, clockwise | `atan2(dx, −dy)` |
| Caps | round, both ends | `radialCapCoverage` |
| Track | full circle, always drawn | `col = style.Track` default |
| Icon vs value | **mutually exclusive** | non-empty `Icon` paints and returns |

The thermal ramp applies only when `Icon == "" && ValueText != ""`: below 60 °C
accent; 60–75 accent→amber; 75–85 amber→error; above 85 error.
`GaugeIconName("temperature")` returns `("", false)`, which is what puts the
temperature gauge on the value-text path today.

## Composition budget against the 70px interior

Content heights at each font scale. Bold fits.

| Composition | 75% | 100% | 125% | 150% |
| --- | --- | --- | --- | --- |
| Current: 22px 2×2 + title | **69** | 74 | 79 | 85 |
| A: 40px 2×2 + title | 105 | 110 | 115 | 121 |
| B1: 40px 1×4 + caption + title | 75 | 83 | 92 | 102 |
| **B2 (recommended): 40px 1×4 + caption, no title** | **54** | **57** | **61** | **65** |
| B4: 36px 1×4 + caption, no title | **50** | **53** | **57** | **61** |
| C: 40px 2×2 + title, card 128 | 105 | 110 | 115 | 121 |

B2 at 200% is 72 against 70. The Home page already fails to lay out at 200%
before this card is reached, so the ceiling is pre-existing.

## Measured overflow of the current layout

Bottom edge of the second row against the padded card bound, from the real
laid-out bounds:

| Font scale | Overflow |
| --- | --- |
| 75% | none |
| 100% | 4px |
| 125% | 13px |
| 150% | 29px |
| 200% | `ui: child … does not fit in 24x38` |

At 150% the GPU value box is squeezed to 18px wide and clips.

## Horizontal budget

Left column interior 338.

| Columns | Slot |
| --- | --- |
| 2 | 164 |
| 4 | 77 |

Caption widths against a 77px slot:

| Caption | 100% | 125% | 150% | Fits 77 at 150%? |
| --- | --- | --- | --- | --- |
| CPU | 26 | 32 | 39 | yes |
| Memory | 47 | 59 | 71 | yes |
| Temp | 33 | 41 | 49 | yes |
| GPU | 26 | 32 | 39 | yes |
| CPU temperature | 99 | 123 | 148 | **no, fails from 100%** |

Recommended row: 4 × 77 + 3 × 9 = 335 of 338, leaving 3px of residual.

## Recommended composition, exact

| Element | Value |
| --- | --- |
| Card | 356 × 88, radius 12, padding 9 |
| Slots | 4 × 77, gap 9 (`MarginM`), 3px residual |
| Ring | 40 × 40, stroke 2, centred in its slot |
| Ring → caption gap | 2 (`MarginXXS`) |
| Caption | `RoleCaption`, 12px, `Subtle` |
| Content height | 40 + 2 + 15 = 57 |
| Vertical residual | 13, split by centring |
| Ring value text | 14px; 13px for a three-character temperature; 12px at `100%` |

## Palette, resolved

`Theme.PanelStyle()` at the default theme:

| Role | Value |
| --- | --- |
| Background | `#1d2025` |
| Capsule (card) | `#3a4149` |
| Track | `#9aa0a6` |
| Accent | `#0080ff` |
| Secondary | `#bec6dc` |
| Error | `#ff5449` |
| Subtle / caption | `#9aa0a6` |
| Foreground | `#e6e6e6` |

Caption `#9aa0a6` on card `#3a4149` is 3.6:1, which clears 3:1 but not 4.5:1.
See the checklist: the caption is a label beside a value carrying full contrast,
and the existing `Subtle` role already carries every caption on this surface.
Changing it is a theme-wide decision, not this card's.

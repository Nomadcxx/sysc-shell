# Component parity — Design

Date: 2026-09-11. Status lives in bd.

Written after an evidence audit of `2026-09-11-noctalia-parity-design.md` found
that document sourced the global ladders correctly and the component layer not
at all. It had read 2 of v4.7.7's 52 components and none of its 261 composition
modules, and it carried two constants imported from **v5**, which is a different
generation with a different system.

This design covers what the parity design left out: how a control gets its size,
and how a surface gets its padding. Behaviour and visual reference only; no QML
is imported.

## What the parity design got wrong

| Claim | Reality |
|---|---|
| Parity D4: "Panel and card padding move to v4's `panelPadding`/`cardPadding` of 14 each" | **Neither constant exists in v4.** `grep -c` returns 0 for each in `Commons/Style.qml`. They are v5 `style.h` values. v4 draws padding from the margin ladder, per surface. |
| Parity implicitly treats control sizes as absolute, matching our `CompactControl`/`StandardControl` | v4 derives **every** control from `Style.baseWidgetSize` (33) times a per-control ratio times `uiScaleRatio`. |
| Stacking D5: the weather card is the consumer for a background image | `WeatherCard` layers a **`ShaderEffect`** over its own rendered content for rain and snow. The full-bleed background pattern is `MediaCard`. |

Sources (read, not imported), all at tag **v4.7.7**:

- `Commons/Style.qml`
- `Widgets/`: `NToggle`, `NSlider`, `NButton`, `NIconButton`, `NTabBar`,
  `NTabButton`, `NComboBox`, `NTextInput`, `NRadioButton`, `NCheckbox`,
  `NScrollView`, `NDivider`, `NCircleStat`, `NLinearGauge`, `NBox`, `NHeader`
  — 16 of 52
- `Modules/Panels/`: `ControlCenterPanel`, `Audio/AudioPanel`,
  `Settings/SettingsPanel` — 3 of 178
- `Modules/Cards/`: all 9

## Goal and scope

In:

- The control derivation system, as a rule rather than a table of numbers.
- The composition rhythms: which margin rung each surface actually uses.
- The display-type multiplier pattern.
- Corrections to the parity and stacking designs.

Out:

- Shader effects. `WeatherCard`'s rain and snow are GPU shaders over a
  `ShaderEffectSource`; this shell has no shader stage and this design does not
  propose one.
- The remaining 175 composition modules. Named in D6 as the residual risk.
- Any change to the global ladders, which the parity design sourced correctly.

## Decisions

### D1 — One master control dimension, and ratios from it

`Style.baseWidgetSize` is **33** logical pixels. Every control derives from it.
Our `Metrics` gains `BaseWidget int` and the control sizes become derived rather
than tabulated, because a table of absolutes cannot stay in proportion when the
base moves.

| Control | v4 derivation |
|---|---|
| `NIconButton` | `toOdd(33 × scale)`; radius `min(iRadiusL, w/2)`; glyph `toOdd(w × 0.48)` |
| `NToggle` | base `33 × 0.8`; track `round(base × 0.85)×2` by `round(base × 0.5)×2`; knob `round(base × 0.4)×2` |
| `NSlider` | knob `round((33 × 0.7 × scale)/2)×2`; track height `round((knob × 0.4 × scale)/2)×2`; radius `min(iRadiusL, track/2)`; default width `sliderWidth` 200 |
| `NCheckbox` | `toOdd(33 × 0.7)`; radius `iRadiusXS × (size/default)` |
| `NRadioButton` | `33 × 0.625 × (pointSize/fontSizeM)`; border `borderM` |
| `NTextInput` | height `33 × 1.1 × scale`; radius `iRadiusM`; left inset `marginM` |
| `NComboBox` | height `round(33 × 1.1)`; popup width `round(33 × 3.75)`; insets `marginL` |
| `NTabBar` | height `33`; radius `iRadiusM`; spacing `marginXS` |
| `NButton` | width `content + fontSize×2`; height `content + fontSize`; radius `iRadiusS` |
| `NScrollView` | handle `round(6 × scale)`; radius `iRadiusM`; gap `marginXS` |
| `NDivider` | `borderS` (1) |
| `NCircleStat` | gauge 60, line 6, arc radius `30 − 5` — **hardcoded, not ladder-derived** |
| `NLinearGauge` | radius is half the short side |

`NCircleStat` is recorded as it is rather than tidied. Our radial gauge already
ships; matching it to 60/6 is a later judgement, not a parity requirement.

### D2 — Odd and even are forced per control, deliberately

`NIconButton` and `NCheckbox` force **odd** through `toOdd`. `NToggle` and
`NSlider` force **even** through `round(x/2)×2`. `Style.pixelAlignCenter` then
centres content without subpixel positioning.

This is not incidental. A shape centred inside an odd box lands on a pixel row;
an even box gives a symmetric two-sided inset. Parity D4 adopted `toOdd` for the
bar alone; it belongs to the control layer too, per shape.

### D3 — Padding comes from the margin ladder, per surface

There is no padding constant. Measured across nine cards and three panels:

| Surface | Inset | Gap between children |
|---|---|---|
| Panel outer column | `marginL` (13) | — |
| Control-centre cards | `marginL` (13) | `marginL` (13) |
| Audio panel cards | `marginL` outer | `marginM` (9) |
| `NBox` card interior | `marginM` (9) | `marginM` (9) |
| Dense card (`SystemMonitorCard`) | `marginS` (6) | — |
| `WeatherCard` | `marginXL` (18) | `marginM` (9) |
| `NTabBar` inside a panel | `marginS` (6) | — |

`NBox` itself defines **no** padding — it is a `Rectangle` with a radius, a fill
and an optional border. Padding is the consumer's choice from the ladder.

Our `Metrics.CardPadding` and `PanelPadding` therefore stop being single values.
`CardPadding` becomes `marginM`, `PanelPadding` becomes `marginL`, and a surface
that needs a different rhythm names its own rung — which is what the token
conformance gate permits, since a ladder rung is not a literal.

### D4 — Display type is a multiplier, not a role

v4 multiplies ladder sizes inline for hero text: `fontSizeXXXL × 1.5` (36 pt) for
the calendar day, `× 1.75` (42 pt) for the temperature, `fontSizeXXL × 1.6`,
`fontSizeXL × 1.6`, `fontSizeXL × 1.1`.

The parity design's single `Display` role at 18 pt captures none of that. Keep
the role — it is a real rung, `fontSizeXXL` — and add a documented multiplier on
the node for hero treatments, rather than inventing five more roles for five
one-off sizes.

### D5 — The inverted hero card needs no new primitive

`CalendarHeaderCard` is a plain `Rectangle` at `Color.mPrimary` with
`Color.mOnPrimary` text, `implicitHeight: (60 × scale) + margin2M`. That is
`FillAccent` with its paired foreground, which this tree already has.

This confirms the earlier guess and removes a candidate primitive.

### D6 — Stacking's consumer is the media card, which changes its sequencing

`MediaCard` is the full-bleed pattern: an `Image` at `anchors.fill: parent` with
`fillMode: PreserveAspectCrop`, sourced from `trackArtUrl` or, when no track art
exists, a cached **wallpaper thumbnail** — so the card always has a background —
then a scrim at `opacity: 0.65`, then content at `0.8`.

`WeatherCard` is not that. Its layering is a `Loader` holding a `ShaderEffect`
that takes the card's own content as a `ShaderEffectSource` and distorts it for
rain and snow. Without a shader stage we cannot reproduce it, and it needs no
stack.

Consequences for `2026-09-11-surface-stacking-design.md`:

- Its D5 gate names the wrong consumer. Corrected in place.
- Its real consumer is the media card, which needs the media service. Stacking
  therefore sequences **after** media, not before it.
- The wallpaper-thumbnail fallback means the stack is worth building even before
  MPRIS lands, because the shell already owns wallpaper thumbnails — but that is
  a consumer decision for the media page's plan, not a reason to land the
  primitive early.

### D7 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`.**

- Control sizes derive from `BaseWidget` and stay in proportion when it changes.
- `toOdd` controls are odd and `round(x/2)×2` controls are even, at every scale.
- Card and panel padding resolve to ladder rungs, not to literals, so the
  conformance gate stays green.
- A hero text node with a multiplier resolves to the expected physical size.

### D8 — Open risk: what is still unread

**175 of 178 composition modules are unread**, including every settings pane,
the launcher, notifications and the lock screen. `SettingsPanel.qml` delegates
almost all geometry to children this design has not opened; its own file carries
two geometry lines.

Panel dimensions also diverge and are **not** reconciled here: v4's control
centre is `round(440 × scale)` and its settings panel `840 × 910`, against our
700×564 and 900×620. Our panel sizes are set by their own approved designs, and
this design does not overrule them.

So: the ladders are sourced, the control system is sourced, the card rhythms are
sourced from all nine cards, and per-pane composition is not. Parity at the
"near-indistinguishable" level is achievable for chrome and controls; for the
interior of any given pane it remains inferred.

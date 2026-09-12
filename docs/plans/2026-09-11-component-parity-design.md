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

There is no padding constant. Measured across nine cards, **seven panels**, a
settings pane and the launcher delegate:

| Surface | Inset | Gap between children |
|---|---|---|
| **Every panel's outer column** | `marginL` (13), height reserve `margin2L` (26) | — |
| Panel inter-card gap | — | **`marginM` (9)** — Clock, SystemStats, NotificationHistory, Wallpaper, Audio |
| Control centre inter-card gap | — | `marginL` (13) — **the lone exception** |
| `NBox` card interior | `marginM` (9) | `marginM` (9), height reserve `margin2M` (18) |
| Dense card (`SystemMonitorCard`, stat cards) | `marginS` (6) | `marginXS` (4) |
| Large header card (`WallpaperPanel`) | `marginL` (13) | `marginM` (9) |
| `WeatherCard` | `marginXL` (18) | `marginM` (9) |
| `NTabBar` inside a panel | `marginS` (6) | `marginXS` (4) |
| **Settings pane** (`VolumesSubTab`) | column at `marginL` (13) | groups `marginS` (6), label stacks `marginXXS` (2) |
| Launcher list delegate | `marginM` (9), or `marginXS` (4) at compact density | same |

Two corrections to this design's own first draft. The `marginM` inter-card gap is
**dominant**, not one of two equal variants — the control centre is the single
outlier across seven panels. And the outer inset is invariant: every panel read
uses `marginL` with a `margin2L` height reserve, without exception.

One structural finding worth more than the numbers: **a settings pane uses no
cards at all.** `VolumesSubTab` is a plain column of `NToggle` and `NLabel` rows.
Our settings panel wraps its rows in card chrome, so matching v4 there is a
composition change, not a padding change — and it is not in this design's scope.

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

**Updated 2026-09-12.** The first draft said "175 of 178 composition modules are
unread". Eleven have since been read, chosen as the ones we actually build:
`ControlCenterPanel`, `AudioPanel`, `ClockPanel`, `SystemStatsPanel`,
`NotificationHistoryPanel`, `WallpaperPanel`, `SettingsPanel`, `SettingsContent`,
`VolumesSubTab`, `Launcher`, `LauncherListDelegate` — plus all nine cards and
sixteen components.

What remains unread is now mostly what we **do not** build: the lock screen, the
dock, desktop widgets, and the 130 per-widget settings dialogs. That is a
different kind of residual from the first draft's, and a much less alarming one.

Two gaps survive and are not closed by this design.

**Panel dimensions diverge and are deliberately not reconciled.** v4's standard
panel is `round(440 × scale)` — control centre, system stats, notification
history all share it — against our 640, 416 and 700. Wallpaper is 800×650 against
our 980×1100; settings 840×910 against our 900×620; the launcher is a 0.25 width
ratio at 600 tall. Our panel sizes come from their own approved designs and this
design does not overrule them, but anyone expecting side-by-side parity should
know the surfaces are different sizes before they start.

**Settings composition differs structurally.** A v4 settings pane uses no cards —
`VolumesSubTab` is a plain column of `NToggle` and `NLabel` rows at `marginL`,
grouped at `marginS`. Ours wraps rows in card chrome. Matching that is a
composition change owned by the chrome catalogue, not a padding change, and it is
out of scope here.

So: ladders sourced, control system sourced, card rhythms sourced from all nine
cards, panel rhythm sourced from seven panels, and the panes we build sourced.
Near-indistinguishable is now reachable for chrome, controls, cards and panel
rhythm. It is **not** reachable for panel dimensions or settings composition
without separate decisions that this design deliberately leaves open.

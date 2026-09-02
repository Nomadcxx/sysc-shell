# Plugin visual polish design

Research: [2026-09-02-plugin-visual-polish-research.md](2026-09-02-plugin-visual-polish-research.md).
Audit: [2026-09-02-plugin-visual-polish-audit-report.md](2026-09-02-plugin-visual-polish-audit-report.md).

Approved 2026-09-02. This is chrome for plugin surfaces we already owe from
Milestone 6. It is not a DMS or Noctalia widget-roster port, not QML/Luau
compatibility, not a lock screen, and not Control Center.

Execute only after the Milestone 6 epic (`sysc-66`) closes. Implementation is a
later worktree, not `milestone/plugin-host` while M6C–F are open.

## Scope

In:

- Timer and World Clock bar/panel trees.
- Host paint for buttons, drag handles, and drop gaps.
- Settings Plugins list.
- Failed bar placeholder and failed plugin panel chrome.
- Notes / Weather / Recorder **follow this IA when those plugins are written**;
  do not block T1 on them, and do not rewrite them in T1 if they already match.

Out:

- Pixel-clone of Noctalia panels.
- Plugin store.
- Icon-font expansion “for completeness.” Hourglass / globe / grip / trash land
  only when a shipped view has no honest text alternative (same rule as bar-parity D8).
- `fontSize`, hex, or Material tokens on `plugin/v1.Node`.
- Hallmark catalog themes, a second typeface, or page macrostructures.
- Control Center plugin tiles (M7).
- Changing M6 process model, protocol freeze, or reference-plugin behavior
  except presentation and the bar-vs-panel split of controls.

## Approaches considered

| | Approach | Reject |
|---|---|---|
| A | Tree-only rewrite with kinds v1 already has | Fast. Ceiling: every button stays accent, drop zones stay invisible, countdown cannot be display-sized. |
| B | Host chrome + trees (this design) | Small host mapping from existing kinds/`Tone`, then rewrite trees to Noctalia IA. |
| C | Wire visual system (size tiers, colours, KindCard) | Violates M6 D4 and the charter. |

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Two tranches after M6: T1 prior-art IA, T2 Hallmark voices. T2 does not start until T1's Timer and World Clock panels match the research table. | One mixed pass. Hallmark-only on the current Gap-8 rows. |
| D2 | Host owns pixels. Plugins name meaning with v1 kinds, `Tone`, `Tabular`, `Padding`, and `Height`. | Plugins pick colours or font sizes. Host infers layout from node ids. |
| D3 | Take Noctalia IA for Timer, World Clock, Notes, Recorder. Take DMS bar pill (already shipped). | Pixel parity. DMS popout header chrome on every plugin panel. |
| D4 | Bar widget = one readout; click / activate opens the attached panel. Timer Start/Pause/Reset are panel-only. World Clock bar is first-city time (glyph later). | Keep Start/Reset on the Timer bar. Icon-only World Clock before the catalogue has a globe. |
| D5 | Button variants from **existing** v1 `Tone` + kind, no new wire field in v1: `KindButton` normal = primary (`FillAccent`); `KindButton` + `ToneError` = destructive (Reset, confirm-delete); `KindDragSource` = ghost handle. | New `emphasis` field before protocol freeze. Magic by node ID. |
| D6 | A panel `KindRow` with `Padding >= 6` paints as a container-filled rounded rect (the Noctalia zone/card). Padding 0 stays a naked row. No `KindCard` on v1. | New kind. Host wraps every list child. |
| D7 | Drop zones are **siblings between rows**, `Height` 3, not children of the zone row. Host paints nothing at rest and a track-coloured gap while a matching drag is active. Convert copies `Height` onto `KindDropZone`. | Invisible extra child at the end of each row. |
| D8 | Confirm-delete is inline in the row (Noctalia check/close). T2 may replace that with undo if notification actions exist. No modal. | Footer “Remove X?”. Always-undo in T1. |
| D9 | Failed bar slot keeps width 48 and shows the plugin **name** at `ToneError`. Failed panel: reason + Close / Retry / Disable, Retry primary, Disable destructive, Close ghost. | `"!"`. Equal-weight three accent buttons. |
| D10 | Settings Plugins is a row: name; muted version · source · state; enable. Retry and stderr only when failed. Settings keys stay nested rows. | Concatenated meta string. Store tiles. |
| D11 | Do not extend `sysc-icons.ttf` in T1. Grips are host-drawn (short vertical marks or a muted `"≡"` from the UI face, one face only). Named glyphs (hourglass, globe, trash) are a follow-on when a plugin has no text alternative. | Mix ASCII `"="` / `"!"` with weather icons. Port Material. |
| D12 | T2 gives each plugin a distinct voice on the same tokens: Timer = one instrument card; World Clock = stacked place rows; Notes = document (header + list or editor). No second typeface, no catalog theme. | Restyle T1 until it looks like a landing page. Copy Noctalia 36px by putting size on the wire. |
| D13 | v1.1 `ToneMuted` / `TonePrimary` only if T1 cannot express hierarchy with `Tabular`, `Padding`, and D5. Default is: no protocol bump. | Size tiers. Colour on the wire. |

## Tree shapes (T1)

Timer bar: one `KindText` remaining, `Tabular`, activate opens panel (host already
routes a widget click; if the text is not interactive, wrap a single `KindButton`
whose label is the remaining time — still one control).

Timer panel: column, gap 12, padding 16. Inner padded row (D6 card) with tabular
remaining + duration field. Footer row: Reset `ToneError`, Start/Pause normal.

World Clock bar: tabular first-city time; activate opens panel.

World Clock panel: add field; `KindList` of (drop gap, padded zone row)* + final
drop gap. Zone row: drag source, zone name, tabular time, offset, ghost/trash
or inline confirm. Empty list: one muted line plus the add field, not a blank
scroll.

## Host paint

`paint.go` `KindButton` / `KindDragSource` split. Destructive uses `style.Error`
fill and on-error text. Ghost uses no fill (or hairline `Track`) and foreground
text. Drag source measures a square handle, not a labelled accent chip.

Drop-zone paint reads panel drag state already on `PanelHost`; without an active
matching drag the node is layout-only.

## Tests

Table tests on convert + paint (button fills, drop-zone height, padded-row card).
View tests on Timer/World Clock trees (bar has no Start/Reset; panel has them;
drop zones are between rows; confirm is on the row). Manager test asserts name
and toggle without a capabilities dump on the title line. No screenshot gate in
T1; live eyeball is owner judgment after M6's live Niri gate.

## Ceiling

D6's padded-row-as-card will also card a toolbar that sets `Padding >= 6`.
Plugins that want a naked toolbar use padding 0. Upgrade path: v1.1 `KindCard`
if a second consumer needs an explicit kind (`ponytail:` global convention, lift
when a second non-list row needs a card without padding).

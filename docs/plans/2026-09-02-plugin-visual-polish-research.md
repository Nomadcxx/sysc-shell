# Plugin visual polish — prior-art research

Evidence gathered 2026-09-02. This is chrome and information architecture for
plugin bar widgets, attached panels, and the Settings Plugins list. It is not a
QML or Luau compatibility survey and it does not reopen Milestone 6's process
model.

Bar-parity already decided that plugins were out of that tranche:
`docs/plans/assets/2026-08-31-bar-visual-parity/refs/README.md` ("Plugins are
not parity targets"). They belong here, after the five reference plugins exist.

## Sources

| Source | Path | What it supplied |
|---|---|---|
| Noctalia v5 host | `/home/nomadx/noctalia` | Capsule chrome, `PluginWidget`, `PluginPanel`, Settings Plugins rows, plugin store tiles |
| Official plugins | `noctalia-dev/official-plugins` (local cache under `/tmp/noctalia-official-plugins` this session) | Timer, World Clock, Notes, Screen Recorder |
| DMS | `/home/nomadx/Documents/GitHub/DankMaterialShell` | `BasePill`, `PluginComponent`, `PluginPopout`, Settings `PluginsTab` |
| sysc-shell | `milestone/plugin-host` at `eb27ea2` | Timer and World Clock trees, host convert/paint, plugin manager |
| Earlier bar grim | `docs/plans/2026-08-31-bar-visual-parity-research.md` | Live DMS capsules; Noctalia core bar. Not plugin panels. |

No live grim of a sysc-shell Timer or World Clock panel exists yet. Panel
comparisons below are source-to-source.

## How Noctalia structures a plugin

Host owns chrome. A plugin supplies a thin tree.

1. Manifest declares `[[widget]]` / `[[panel]]` / `[[service]]`.
2. Enable starts the plugin; the user places `author/plugin:entry` on the bar.
3. Bar: host capsule (`barCapsulePadding = 6`). Plugin draws a compact glyph
   row (`gap` 4–6). Click typically `togglePanel`. The bar forbids input,
   select, and scroll.
4. Panel: host column, padding, optional drag overlay. Size and placement come
   from the manifest (`attached`, `open_near_click`, or floating fill-height).
5. Typical split: headless `[[service]]` plus `noctalia.state`; UI entries are
   clients of that state.

### Timer

Bar (`timer/bar.luau`): vertical = hourglass glyph only; horizontal = glyph +
bold countdown, colour by state (`on_surface` / `primary` / `error`). Click
opens the panel, except a finished timer resets instead of opening.

Panel (`timer/panel.luau`): column `gap=12 padding=16`. A
`surface_variant/0.4` card, radius 8, holds a 36px bold countdown and the
duration field (disabled while running). Footer: Reset destructive, Start/Pause
primary. Panel closes on start.

### World Clock

Bar: globe glyph, click opens panel.

Panel: title + close; add field + plus; scroll list. Each zone is a filled row
(`paddingH 6`, `radius 6`, `surface_variant/0.35`) with a `menu-2` grip,
stacked display name + IANA id, stacked time + offset, ghost trash. Confirm
delete is **inline** (check / close). Drop gaps are **between** rows, height 3,
`expandOnDrag`. Empty list is muted "empty" copy, not a blank column.

### Notes

Bar: configurable glyph, tooltip, click opens a floating fill-height panel.

Panel: header (title, sort, plus, close); list rows with pin/trash glyphs, or a
multiline editor. Confirm delete is inline.

### Screen Recorder

Bar glyph by state. **No panel.** Service + Control Center shortcut.

### Weather

Not an official plugin. Native bar widget; panel is a Control Center tab.

### Plugin manager

Settings → Plugins is **rows**: glyph | name + Official/Community + version |
enable toggle, plus settings/trash. The store is a tile grid. We have no store
and this polish does not add one.

## How DMS structures a plugin

Injection: bar layout stores an id; `WidgetHost` maps built-ins first, then
`PluginService.getWidgetComponents()[pluginId]`. Variants are `pluginId:variantId`.

Chrome: `PluginComponent` wraps plugin content in `BasePill` (radius =
`Theme.cornerRadius`, `widgetBaseBackgroundColor`). Click opens `PluginPopout`
(`Theme.popupBackground()`) with optional `PopoutComponent` header, or runs
`pillClickAction`.

Clock, weather, and notes are **built-ins**, not plugins. Timer is not in the
DMS tree. The in-repo examples are emoji / variants / CC toggle.

Manager: Settings → Plugins accordion rows (icon, name, `vX by Author`,
permission chips, enable toggle). Expanded settings load plugin QML.

## What sysc-shell ships today

Host capsule wrapping is already done for plugin bar slots
(`internal/shell/widget.go` `capsuled`). Convert maps `plugin/v1.Node` onto
`ui.Node`. Paint treats every `KindButton` and `KindDragSource` as a full
accent slab (`internal/render/paint.go`). Wire `Tone` is only normal/error;
size tiers are absent on purpose (M6 D4). The icon font is weather + battery.

| Surface | sysc-shell now | Prior art |
|---|---|---|
| Timer bar | Countdown + Start/Pause + Reset | Glyph + countdown; click opens panel |
| Timer panel | Same remaining text, duration field, same bar buttons | Display countdown in a card; Reset/Start pair with variants |
| World Clock bar | Time + zone **button** | Globe glyph; click opens |
| World Clock panel | Flat row: `=` grip, zone, time, offset, Remove; drop zones **inside** the row; confirm as a footer | Filled row, ghost grip/trash, drop **gaps**, inline confirm |
| Notes | Not written (M6C) | Glyph bar; header + list/editor |
| Failure placeholder | `"!"` at `ToneError`, width 48 | Named status in the capsule |
| Manager | Concatenated `name version · source · state · caps` + toggle + Retry | Glyph, name, badge, version, toggle |

## Take / leave

Take the **chrome language and information architecture**, not pixels, QML, or
Luau.

| Take | From | Why |
|---|---|---|
| Bar = one readout; controls live on the panel | Noctalia Timer/Notes/World Clock; DMS pill click | A 30px capsule cannot host Start/Reset without wrapping |
| Host-owned capsule | Already shipped; DMS `BasePill`; Noctalia capsule | Do not let plugins draw a second pill |
| Panel: one display object + one action row | Noctalia Timer | Hierarchy the current Gap-8 column lacks |
| Filled list rows, grip, ghost delete, inter-row drop gaps, inline confirm | Noctalia World Clock | Matches M6B behavior we already implemented, poorly presented |
| Manager as rows, not a meta dump | Noctalia and DMS | Settings already uses rows elsewhere |
| Recorder stays bar + service | Noctalia | M6E; do not invent a panel |

| Leave | Why |
|---|---|
| Luau/QML, `noctalia.state`, DMS variants QML | Charter: no compatibility |
| Plugin store tiles | No catalog in M6 |
| 36px / `fontSize` on the wire | M6 D4; host maps roles, not sizes |
| Material icon font wholesale | Bar-parity D8: expand PUA only when a shipped widget needs a named glyph |
| CC weather tab / CC recorder shortcut | M7 |
| Pixel-clone of Noctalia cards | Hallmark tranche exists so we do not become a clone that also looks generated |

## Hallmark audit

Full punch list: `2026-09-02-plugin-visual-polish-audit-report.md`.
Headline: 2 critical · 6 major · 2 minor. The structural tell is the equal-weight
`Gap: 8` control row reused as Timer bar, Timer panel, World Clock panel, manager
card, and panel-error chrome.

# Bar visual parity — live findings

Evidence gathered 2026-08-31 on the owner's laptop (`eDP-1`, 1920×1080, Niri scale 1.25, logical 1536×864). Screenshots are grim of a running compositor, not GitHub READMEs.

| Shot | File |
|---|---|
| DMS bar, full width | [assets/2026-08-31-bar-visual-parity/dms-bar.png](assets/2026-08-31-bar-visual-parity/dms-bar.png) |
| DMS left | [assets/2026-08-31-bar-visual-parity/dms-bar-left.png](assets/2026-08-31-bar-visual-parity/dms-bar-left.png) |
| DMS center | [assets/2026-08-31-bar-visual-parity/dms-bar-center.png](assets/2026-08-31-bar-visual-parity/dms-bar-center.png) |
| DMS right | [assets/2026-08-31-bar-visual-parity/dms-bar-right.png](assets/2026-08-31-bar-visual-parity/dms-bar-right.png) |
| sysc-shell bar on the same output, `main` `7ba7cce` | [assets/2026-08-31-bar-visual-parity/sysc-bar.png](assets/2026-08-31-bar-visual-parity/sysc-bar.png) |

Noctalia v5 was **not** screenshotted. `extra/noctalia 5.0.0_beta.9-3` is in the Arch repos and not installed (sudo). The v4 backup at `~/Backups/noctalia-v4-20260801-001048/quickshell-noctalia-shell` was started with `qs -p` and failed before any layer mapped (`HyprlandService` / `PwAudioSpectrum` unavailable). Noctalia evidence below is the owner's live `~/.config/noctalia/settings.json` from when they last ran it, plus that failed start.

DMS evidence is both the grim and `~/.config/DankMaterialShell/settings.json` plus `/usr/share/quickshell/dms/Modules/Plugins/BasePill.qml` on the same machine.

## What the live DMS bar actually is

Outer chrome: a floating rounded slab inset from the screen edge (gap + radius 12). Inner chrome: **every widget sits in its own capsule** with horizontal padding, the same corner radius, and a fill lighter than the slab (`widgetBackgroundColor: "sch"` → Material surface-container-high). Spacing between capsules is 4; bar `innerPadding` is 4.

Accent is not a random highlight. It is the theme primary (here a lavender from the wallpaper matugen run) used as fill on **active / on** states:

- Workspace: one capsule holding four dots; the focused workspace is a filled primary disc; the others are dim discs.
- Connected / charging-class widgets (battery, network in this session) tint their capsule toward primary.

Clock and window title are text-only capsules. They do not get an accent fill.

DMS `BasePill.qml` on this install: capsule `Rectangle` with `radius: Theme.cornerRadius`, horizontal padding from `widgetPadding ?? 12` scaled by `widgetThickness/30`, background `Theme.widgetBaseBackgroundColor`. Clock widgets wrap `BasePill`.

### Widget inventory on the grim (eDP-1)

The laptop is not named in either `barConfigs.screenPreferences` (those target DP-3 and DP-1). What actually painted:

| Section | Widgets (left → right) |
|---|---|
| Left | workspace switcher (dots in one capsule), launcher button, focused window title |
| Center | MPRIS / music, clock `11:37`, weather (sun glyph + temperature) |
| Right | clipboard, system tray icons, CPU %, memory %, notifications, battery (glyph + %), network, control-center |

`settings.json` `cornerRadius` is 12. `widgetBackgroundColor` is `"sch"`. Battery pill style is off (`batteryPillStyle: false`) — battery is still a BasePill capsule, not a separate battery-shaped path.

### What we take from DMS for sysc-shell visuals

Take the **chrome language**, not the widget roster.

| Take | Why |
|---|---|
| Per-widget capsule, padded, radius = bar radius | Owner: critical. Live grim. BasePill. |
| Capsule fill from surface-container, not from the slab `surface` | Theme-aware; `SurfaceContainer` is already generated and unused. |
| Accent fill only on active/on (focused workspace dot; optional charging/connected later) | Live grim. Clock/title stay surface-container. |
| Workspace as dots inside one capsule, not a workspace name string | Live grim. Noctalia Workspace widget agrees. |
| Outer floating slab kept | Already shipped (48 / gap 4 / radius 12). DMS has both slab and capsules. |
| Item spacing 4 | Already the default. Matches both live configs. |

### What we do not take from DMS

Launcher, MPRIS, clipboard, tray, notifications, control center, network-speed, system-update, cpu-temp-on-bar. Those are later milestones (M5 tray/notifications, M7 launcher). Shipping empty capsules for them would be fake parity.

App icons on the focused-window capsule (DMS draws the window's icon). We have no icon loader; title stays text.

A general Material icon font. See Icons below.

## What the live Noctalia config actually is

`bar.barType: "simple"`, `showCapsule: true`, `capsuleColorKey: "none"`, `contentPadding: 2`, `widgetSpacing: 6`, `frameRadius: 12`, `marginHorizontal/Vertical: 5`.

Workspace is **centered**, configured as pills:

- `focusedColor: "primary"`
- `occupiedColor: "secondary"`
- `emptyColor: "secondary"`
- `pillSize: 0.62`
- `hideUnoccupied: false`
- `labelMode: "index"`
- `showLabelsOnlyWhenOccupied: true`

Left: SystemMonitor (CPU + mem + CPU temp, `usePadding: false`), ActiveWindow (`maxWidth` 145, `showIcon: true`), MediaMini, privacy-indicator plugin.

Right: Tray, NotificationHistory, Battery, Volume, VPN, Brightness, Clock, ControlCenter, weather plugin, plus further plugins.

Noctalia's inner padding is much tighter than DMS (2 vs ~12). The owner asked for capsules **with padding**; the live DMS grim is the padding reference. Noctalia contributes the workspace-as-pills model (primary / secondary / empty) and confirmation that capsules are the default, not an optional skin.

## What sysc-shell actually paints today

Default items (`internal/config/config.go`): left `workspace` + `window-title`, center `clock`, right date `clock`. Geometry already matches the DMS content band test: height 48, gap 4, padding 6, spacing 4, radius 12.

Live grim: one fused 40px slab. Workspace is the label `"1"` (or a name) jammed against a truncated title. Clock truncates to `"11:…"` at scale 1.25. No inner capsules. No dots. Accent exists as a token (`Primary` → `Theme.Accent`) and is used by meters, buttons, OSD, and calendar-today — not by the bar items.

`theme.Tokens` already has `SurfaceContainer`. `ThemeFromTokens` maps Surface, OnSurface, Primary, OnSurfaceVariant, Error. **SurfaceContainer is dropped on the floor.** `Theme.Muted` after a generated theme is `OnSurfaceVariant` (a text colour) and is the meter track — it is the wrong fill for a capsule.

Workspace projection (`internal/shell/projection.go`) reduces each output to one string (focused-or-active name/index) plus one title. The snapshot already has every workspace on that output (`niri.Workspace` with `Index`, `Focused`, `Active`, `HasActiveWindow`, `ID`). The extra dots are a projection change, not a Niri IPC change.

## Icons

The owner assumed a general icon set already existed. It does not.

`internal/render/iconfont.go` embeds `sysc-icons.ttf` with weather (8 PUA glyphs) and battery (15 states) only. Default bar items (workspace, title, clock, date) do not use it. Weather and battery widgets, when configured, already go through that font.

Default-bar visual parity therefore does **not** wait on a new icon font. Workspace dots are filled circles, not glyphs. Clock and title are text. Expanding the PUA set is justified only when a shipped widget needs a glyph that is not weather or battery (OSD already paints an accent square; M5/M7 widgets will need their own story).

## Scale truncation (related, not this tranche)

At Niri scale 1.25 the clock paints `"11:…"` and the title clips early. Layout measures in logical pixels; shaping for paint happens at the physical size. Capsules around truncated text would still look wrong. Recorded here so it is not forgotten; it is a measure/paint scale bug, not a missing DMS widget. Do not fold it into the capsule tasks.

## Other live defects, not visual-parity scope

These were on the same session and must not be converted into a visual-parity pass:

- Super+M: `ui: child 0: unsupported kind 7` (`KindTab` in `measureNode`) then Wayland configure `fail()` kills the process (`sysc-41`).
- Super+P / Super+X / Super+Comma: panel chrome, no `Paint` of content (`sysc-41`).
- Settings close: `dispatch: panic … invalid server object ID` (`sysc-42`).
- `sysc-3` live Niri gate remains open. A visual grim is not that gate.

## Gap list (visual, default bar)

| # | Gap | Evidence |
|---|---|---|
| G1 | No per-widget capsule | sysc-bar.png vs dms-bar.png |
| G2 | No capsule padding | BasePill default 12; our items are flush in the slab |
| G3 | `SurfaceContainer` unused; no capsule fill token | `ThemeFromTokens` |
| G4 | Accent not applied to focused workspace | Dots vs text `"1"` |
| G5 | Workspace is a single label, not a pill row | `projectOutputs` + `widget.go` |
| G6 | Empty title still occupies layout as text; a naive wrap would leave an empty pill | empty title already measures zero — keep that |
| G7 | Assumed icon font is only weather+battery | `iconfont.go` |

Non-gaps: outer floating bar, radius 12, spacing 4, matugen seed pipeline, Accent token existence, weather/battery glyphs for those widgets.

# Running-apps bar pill — Design

Date: 2026-09-05. Status lives in bd, not here.

Prior art: `2026-09-05-running-apps-pill-prior-art.md`. Owner-approved in
session 2026-09-05 after the prior-art note was corrected for `.desktop`
`Actions=` (Steam is not special; Spotify on this machine ships none).
D8/D11 amended the same session: the pill must work when the user does
not use the sysc-launcher; sysc-shell owns XDG identity and spawn.

## Goal

One bar capsule of compact application icons for every UI window in the Niri
session. One icon per application. Left-click focuses or cycles that app's
windows. Right-click offers that app's desktop-file actions plus Close all.
The pill hides when nothing is open. It does not replace `window-title`.

## Out of scope

Pins, a dock surface, per-window titles in the pill, wheel-to-cycle, grouping
by workspace or output, `wlr-foreign-toplevel`, `/proc` scanning, the tray's
live D-Bus menu, SVG-only theme icons (same M5 raster ceiling as the launcher),
and folding this into the launcher windows provider (`sysc-81`).

## Decisions

### Product

- **D1 — Surface: bar capsule, not a dock.** DMS RunningApps chrome: one
  `KindCapsule` that grows with the slot count and is omitted at zero.
  Noctalia Dock / DMS Dock are a second surface and are not this work.
- **D2 — Slot identity: one icon per application.** Group Niri windows by
  resolved desktop-entry id. Two Firefox windows are one Firefox icon.
  `steam_app_*` with no matching desktop file folds into Steam so a game does
  not sprout a second slot. Unmatched `app_id` values stay their own slot
  with a letter fallback.
- **D3 — Scope: the whole session.** Every bar paints the same slot list.
  This is the DMS RunningApps default (`runningAppsCurrentWorkspace: false`).
  Per-output and per-workspace filters are follow-ups.
- **D4 — Placement: default right, before status.**
  `running-apps`, then the cpu/memory group, battery, notifications.
  Left stays `workspace` + `window-title` (the focus cluster in both
  references' defaults). DMS and Noctalia do **not** put Running Apps /
  Taskbar on the default bar; the icons the owner remembered on the right
  are the system tray. This pill is an explicit roster addition, placed on
  the right so the left remains "what is focused."
- **D5 — Compact icons only.** 18 px rasters in 24 px tiles, DMS compact
  numbers. No title column. `window-title` stays on the left.

### Interaction

- **D6 — Left click: focus or cycle.** If no window of that slot is focused,
  focus the most recently used member: max `FocusTimestamp`, else the first
  member in list order. If one already is focused, focus the next member in
  Niri window-list order, wrapping. Matches DMS Dock / Noctalia
  `groupClickAction: cycle`. No sticky last-focused id across the time
  when the slot is unfocused; that memory would need registry state and is
  a follow-up.
- **D7 — Right click: desktop `Actions=` plus Close all.** The app-specific
  rows are `[Desktop Action …]` from the matched `.desktop` file — Steam
  Store/Library/Friends, Firefox New Window/Private, LibreOffice Writer/Calc,
  Spectacle's capture targets, terminals' New Window, and every other file
  that shipped `Actions=` (38 visible files on this machine, 2026-09-05).
  An app that shipped none (Spotify, Discord, mpv here) contributes zero
  extra rows. Close all is always present for a running slot. Pin, window
  title list, and "Launch on dGPU" are out. This is not the tray
  StatusNotifierItem menu.
- **D8 — Action launch is a shell spawn, not `launcher.Service`.** Expand
  the matched `[Desktop Action …]` `Exec` with `go-freedesktop/desktopentry`
  `ExpandExec` and run `niri msg action spawn --` plus that argv. Same
  compositor seam the launcher uses internally; do not call
  `Service.Activate` (that records launcher usage history and requires the
  service). Failures log and leave the pill up (D15).

### Data and transport

- **D9 — Niri windows are the model.** No process scan. A window in the
  event stream is a UI app. Extend `niri.Window` with `Focused` from
  `is_focused` and `FocusTimestamp` as monotonic nanoseconds. Live wire
  shape (niri 26.04, also the M3 audit) is
  `"focus_timestamp":{"secs":N,"nanos":M}` or `null` — not a bare int.
  `WindowFocusChanged` updates which window is focused after the initial
  snapshot. `pid` stays dropped.
- **D10 — Focus and close are short-lived Niri JSON requests.** The event
  stream stays read-only. A helper in `internal/platform/niri` dials
  `$NIRI_SOCKET`, writes `{"Action":{"FocusWindow":{"id":N}}}` or
  `CloseWindow`, reads `Ok`, disconnects. Same shape as Noctalia's
  `NiriRuntime::requestAction`. Not `niri msg` argv (the launcher keeps
  argv spawn for `Exec`; compositor window actions stay in the niri
  package). Requests run off the Wayland owner, through the existing
  command/channel pattern.
- **D11 — Identity lookup lives in sysc-shell.** The launcher widget is
  optional; the bar is not. Do not start `launcher.Service`, do not call
  `Query`/`Results`/`Activate`, and do not treat an empty launcher panel as
  a missing catalogue. Scan XDG application dirs with the already-pinned
  `go-freedesktop/desktopentry` (promote it to a direct require). Match in
  order: equal-fold desktop-file `ID`; equal-fold `StartupWMClass`; last
  `.`/`/` segment of `app_id` against `ID`. Icon name is the matched
  `Icon=`; `internal/icons` resolves a raster; SVG-only names stay a
  letter. Keep `Hidden` tombstones out. Keep `NoDisplay` entries in: a
  running window still needs its icon and `Actions=` even when the
  launcher would hide it. This is a focused identity index, not a second
  launcher (no ranking, no history, no TryExec menu filter).

### Chrome and menu host

- **D12 — No per-tile focus chrome.** Idle and focused tiles look the
  same: 18 px icon centred in a 24 px cell, transparent on the shared
  capsule. The window-title widget already names the focused app; a
  Primary fill on a 24 px tile reads as a stray chip and does not
  centre the icon. Hover is the existing bar hover path.
- **D13 — The context menu is an Overlay popup, not a bar child.** A
  `KindMenu` in the bar strip would blow the bar height. Host it as an
  Overlay auxiliary surface in the process-wide interactive-root chain,
  same family as the tray menu fallback (`trayMenuHost`: Overlay because
  the xdg_popup-on-layer-shell probe is still pending). Rows are action
  names then Close all. Selecting a row either spawns (D8) or
  `CloseWindow`s every member (D10). If the slot disappears while the
  menu is open, the menu closes.
- **D14 — Empty capsule is absent.** Zero slots → the item measures
  nothing and is not hit-tested. The right section shrinks. No placeholder
  glyph.

### Failures

- **D15 — Compositor and lookup failures are non-fatal.** A failed
  FocusWindow/CloseWindow is logged and ignored; the pill stays. Close all
  is best-effort per window and does not abort the rest. A missing desktop
  file still produces a slot keyed by `app_id`, letter icon, Close all
  only. A missing raster is the same letter. Icon decode is async; the
  tile keeps its box until a raster lands or never does.

## Layout

Default `Bar.Right` after this design:

```
running-apps | cpu+memory group | battery | notifications
```

Tile geometry (logical px, DMS compact):

```
capsule padding  spacingS (~8)
tile             24 × 24
icon             18 × 18, centred
gap between tiles spacingXS (~4)
```

Slot order is first appearance in the Niri window list, so a newly opened
app appends. Reordering by usage is a follow-up.

## Checks

Table tests, one package each, no `./...`, no `-race`:

- `niri.Window` projects `is_focused` / `focus_timestamp`.
- Grouping: two windows with the same desktop id → one slot; distinct ids →
  two; `steam_app_123` with no desktop file → Steam; `steam_app_123` with its
  own desktop file → its own slot; unknown app_id → its own slot.
- Focus-or-cycle: unfocused slot focuses MRU; focused slot advances and
  wraps.
- Menu rows: Steam-like `Actions=` plus Close all; empty `Actions=` is
  Close all only.
- Empty set omits the capsule.
- `Default()` right section leads with `running-apps`.

Live Niri gate (owner machine, after the tables pass): Steam and Firefox
icons appear when those apps have windows; left-click cycles; right-click
shows that file's `Actions=` plus Close all; closing the last window of an
app removes its icon.

## Follow-ups (not this design)

Wheel-to-cycle (DMS RunningApps). Per-output / per-workspace filter. Pins.
Per-window title column. `xdg_popup` parenting once that probe lands.
SVG theme icons (`sysc-86`). Launcher windows provider (`sysc-81`).
Sticky last-focused member while a slot is unfocused. Sharing this
desktop index with the launcher so a session without the launcher widget
does not still scan twice is a later cut.

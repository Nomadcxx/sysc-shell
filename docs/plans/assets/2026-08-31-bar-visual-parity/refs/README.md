# Parity reference captures

Live `grim` on the owner's laptop (`eDP-1`, 1920x1080 physical, Niri scale 1.25, logical 1536x864),
2026-08-31. One shell runs at a time; `sysc-shell` was stopped for the duration.

Noctalia v5 evidence is live pixels from this session. It supersedes the config-only Noctalia evidence
in `2026-08-31-bar-visual-parity-research.md`, which was written when only the failing v4 backup existed.

| File | Shell | Shows |
|---|---|---|
| `dms-dash-default.png` | DMS 1.5.3 | Dash opened from the centre clock. Tab row, time card, weather card, profile card, calendar, sliders, media. |
| `dms-process-element.png` | DMS 1.5.3 | Processes panel: segmented filter, search field, radial gauges, sortable table with value chips. |
| `noctalia-bar-idle.png` | Noctalia 5.0.0-beta.9 | Bar. Tight capsules, numbered workspace pills, core widget group. |
| `noctalia-control-center.png` | Noctalia 5.0.0-beta.9 | Control centre: vertical nav rail, profile card, quick-toggle tiles, clock card. |
| `noctalia-settings.png` | Noctalia 5.0.0-beta.9 | Settings window: nav list, section headers, row controls, segmented groups, toggles, sliders. |
| `noctalia-weather.png` | Noctalia 5.0.0-beta.9 | Weather pane: current-conditions card, key/value detail list, Daily/Hourly segmented control, forecast rows. |
| `noctalia-sysmon.png` | Noctalia 5.0.0-beta.9 | System pane: 2x3 card grid. CPU/Memory/GPU/Network sparklines with footer chips, plus System and Resources key/value cards. |
| `noctalia-calendar.png` | Noctalia 5.0.0-beta.9 | Calendar pane: month nav, accent weekday headers, dimmed adjacent months, accent-filled today, events card. |
| `noctalia-notifications.png` | Noctalia 5.0.0-beta.9 | Notification centre: clear-all and DND header buttons, All/Today/Yesterday/Older segmented control, scrolling cards with app icon, relative time, per-card dismiss, summary, body and an action button. |
| `noctalia-toast-basic.png` | Noctalia 5.0.0-beta.9 | Popup toast, normal urgency, app icon and body. |
| `noctalia-toast-critical-progress.png` | Noctalia 5.0.0-beta.9 | Popup toast, critical urgency: error border, no countdown. Sent with `-h int:value:12`, which rendered no progress bar. |
| `noctalia-toast-actions.png` | Noctalia 5.0.0-beta.9 | Popup toast with three actions, accent countdown line on the top edge, app name in the footer. |

## Plugins are not parity targets

Much of the Noctalia bar is plugin-supplied, not core chrome. The owner authors several of them.
Loaded during capture: `nomadcxx/gamer-mode` (the centre usage element with the `5h` bar and `Wk`),
`nomadcxx/codexbar-plugin` (`Orac 15B`), `nomadcxx/gslapper`, `nomadcxx/hermes-agent`,
`yuuto/arch-updater`, `dotnetrob/cat`, `8bury/mini-docker`.

Core Noctalia bar chrome is: launcher glyph, workspace pills, clock, the system-monitor group
(CPU, temperature, memory, disk, network rates), then wifi, Bluetooth, notifications, clipboard,
volume and battery. Only that set is a parity target here. Plugin equivalents belong to Milestone 6,
which is where an external widget host is designed.

## Divergences the current design has already decided

- **Workspace numerals.** D5/D6 chose dots with no index numerals, citing the DMS grim. Live Noctalia
  numbers every pill (focused is an elongated accent pill carrying `1`; the rest are numbered discs).
  The two references disagree; the design follows DMS.
- **Capsule padding.** Noctalia's inner padding is visibly tighter than DMS's. D4 picked 8, between the
  two. Live pixels now support that choice from evidence rather than from config alone.

## Vocabulary these panels need that `internal/ui` does not have

Recorded as scope evidence, not as a commitment.

| Element | Seen in | Status in `internal/ui` |
|---|---|---|
| Radial arc gauge | DMS processes (CPU, memory) | Absent. `KindMeter` is linear; `KindGraph` is a sparkline. |
| Segmented control | DMS processes, Noctalia settings, Noctalia weather, Noctalia notifications | Absent. `KindTab` exists but is measured as text. **Four sightings across two shells; the strongest candidate in this set.** |
| Sortable table with header row | DMS processes | Absent. |
| Vertical nav rail with active state | Noctalia control centre | Absent. |
| Quick-toggle tile, accent-filled when on | Noctalia control centre | `KindToggle` is a switch, not a tile. |
| Card with image background | Noctalia control centre, DMS dash | Absent. |
| Dropdown / select | Noctalia settings | Absent. |
| Slider paired with a numeric value box | Noctalia settings | `KindSlider` exists; the paired readout does not. |
| Key/value row: icon, label, right-aligned value | Noctalia weather | Absent. Reusable well beyond weather. |
| Large display numeral with a secondary accent line | Noctalia weather, DMS dash | Absent. |

## What we already have

Not every panel is new construction. Recording this so the panel design does not rebuild what exists.

- **System pane.** The four sparklines are `KindGraph`, which the monitor popout already builds at
  `Width: 240`. The gap is per-metric colour, a card wrapper and a footer chip row, not a new
  primitive. The popout that crashes in `sysc-41` is the same panel.
- **Calendar pane.** `PanelClock` already builds `clockTree` with `monthDelta` navigation. The gap is
  accent weekday headers, dimmed adjacent months and a filled today marker. DMS fills a circle and
  Noctalia a rounded square; the radius is the only difference. The events card needs a calendar
  source that is not scoped anywhere.
- **Notification centre.** Milestone 5 Tranche 5A already specifies a centre, a virtual list, DND and
  clear controls in the header, per-card dismiss, actions and relative time. `noctalia-notifications.png`
  is a live picture of that spec. One divergence: the 5A design groups by desktop entry then
  application name, while Noctalia groups into All / Today / Yesterday / Older time buckets through a
  segmented control. The design's grouping stands unless the owner changes it.

## Popup toasts

The centre is history; a popup is a separate surface, and Tranche 5A specifies both. These were
triggered deliberately with `notify-send` rather than waited for.

- **Normal urgency** carries a thin accent countdown line along the top edge of the card, an app icon,
  a bold summary, a body, a close affordance, an optional row of action buttons, and the application
  name in the footer.
- **Critical urgency** replaces the countdown with an error-coloured border and does not expire, which
  is why no countdown is drawn.
- **Progress was not rendered.** The critical toast was sent with `-h int:value:12` and Noctalia drew
  no value bar. 5A specifies an independent `value` bar, so the specification is ahead of this
  reference here. Do not treat the absence as evidence against it.

Not captured, because no reference shell implements them: inline reply, and swipe-to-dismiss at the
35 percent threshold. Both are 5A requirements with no live comparator.

## Upstream dependencies

The DMS processes panel needs per-process CPU and memory enumeration and a CPU temperature.
`sysc-metrics v0.2.0` is recorded as core counters plus a sysfs battery aggregate, with thermal
omitted, and it exposes no process list. That panel is blocked on upstream work in `sysc-metrics`,
not on shell work.

The Noctalia weather pane needs daily and hourly forecast series, wind, sunrise, sunset, elevation,
UV index and timezone. Tranche 3D fetches Open-Meteo for a current condition, a temperature and a
staleness marker only, and its design explicitly refused to widen that request to fill a tooltip.
A weather panel widens it deliberately, which is a 3D design amendment rather than a panel-only
change.

The Noctalia system pane additionally needs GPU usage, CPU temperature, load averages, swap and disk
capacity. Of those, `sysc-metrics v0.2.0` supplies none: it is core counters plus a sysfs battery
aggregate, thermal omitted, no GPU. The visual work for that pane is cheap and the data work is not.

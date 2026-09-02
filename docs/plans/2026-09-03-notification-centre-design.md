# Notification centre — Design

Date: 2026-09-03. Status lives in bd (`sysc-151`).

5A shipped toast surfaces, a projection, and a `centerTree` stub that is never
opened from the bar. This slice makes the centre a first-party panel and brings
toast chrome up to the same visual target.

Clone target: **DMS 1.5.3** (`/usr/share/quickshell/dms`), not Noctalia's
control-centre tab. Pixel parity means the DMS notification-centre popout, its
bar button, and its popup toast chrome. Noctalia remains prior art for
behaviour the owner already locked (one Overlay per output, exclusive −1,
swipe 35%). It is not the layout.

Sources (read, not imported):

- `Modules/Notifications/Center/NotificationCenterPopout.qml`
- `Modules/Notifications/Center/NotificationHeader.qml`
- `Modules/Notifications/Center/NotificationCard.qml`
- `Modules/Notifications/Center/HistoryNotificationCard.qml`
- `Modules/Notifications/Center/HistoryNotificationList.qml`
- `Modules/Notifications/Center/NotificationEmptyState.qml`
- `Modules/Notifications/Center/DndDurationMenu.qml`
- `Modules/DankBar/Widgets/NotificationCenterButton.qml`
- `Modules/Notifications/Popup/NotificationPopup.qml`
- `Services/NotificationService.qml` (`getGroupKey`, `formatHistoryTime`)
- `Common/Theme.qml` notification metrics
- 5A research D10 (`2026-08-30-notifications-and-tray-research.md`)
- Integration design (`2026-08-31-notifications-and-tray-integration-design.md`)

## Goal and scope

A first-party `PanelNotifications` that a bar bell opens. The panel clones the
DMS notification-centre popout. Toasts clone DMS popup chrome. Header actions
reach `sysc-notify` (`sysc-150`).

In:

- Bar item `notifications`: glyph, unread dot, DND glyph, gestures.
- Dedicated panel, IPC name `notifications`.
- Current tab: grouped active cards.
- History tab: flat closed cards plus time chips.
- DND (shell-owned) with DMS duration presets.
- Toast: circular app icon, bottom timeout bar, critical primary border and
  left chip, image-data and `AppIcon` filled.
- Unclip the live toast: measure real card height, stack below the bar.
- `history.mark-seen` on open; `active.dismiss-all` / `history.clear` from
  Clear; per-card dismiss; per-history remove once the service command exists.

Out:

- A control-centre shell or Noctalia nav rail.
- A plugin process hosting the centre. Plugin cap `notifications` stays "plugin
  may send a notification."
- DMS in-panel settings drawer (timeouts, compact, overlay, rules, mute).
- Keyboard-hint overlay and right-click rules/mute menu.
- Compact-mode sizes. v1 always uses DMS *normal* metrics.
- Reimplementing D-Bus. `sysc-notify` stays owner.
- Shrinking the toast Wayland surface (lag follow-on, not this slice).

## Amendments

This design amends 5A where the clone target and the stub disagree.

| Topic | 5A / D10 | This slice |
|---|---|---|
| Surface | D10 rejected a DankPopout | Dedicated panel that **is** the DMS popout: 416×content, bar-hugging, Exclusive |
| Grouping | Desktop-entry then app name for history | **Current** tab only. History is a flat list with chips |
| History actions | None | Per-card close (`history.remove`) matching DMS |
| Header | DND / 1h / Dismiss all / Clear history as four text buttons | Title + DND + schedule + Clear; Current/History tabs |
| Toast urgency | `"!"` on the summary | DMS critical border + left primary chip; timeout bar colour |
| Toast countdown | Seconds as text | 3 px bar on the **bottom** edge (DMS), not Noctalia's top edge |
| Toast height | Hard-coded 96 px | `ui.ContentHeight` plus two radii, same as sysmon |
| Toast origin | 12 px from the output edge | Below the bar exclusive zone (top bar) or above it (bottom bar) |
| Bar | Unread count with no widget | Bell pill on the default bar |

D11 (shell-owned DND, suppress toasts not history) stands. D4's `value` hint
meter stays on the toast body when the sender sends it; DMS has no `value` bar,
the extra meter is already shipped and stays.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | First-party `PanelNotifications`. Public name `notifications`. | Plugin panel. Noctalia CC tab. Waiting for a control centre |
| D2 | Pixel-clone DMS 1.5.3 centre + toast chrome. | Pixel-clone Noctalia's notifications tab. Hybrid "Noctalia cards in a DMS frame" |
| D3 | Width 416 logical (`400 + spacingL`). Height from content, min 300, max the smaller of 80% of the output and header + 600 list. Same intrinsic-height path sysmon already uses | D10's fixed 400×620. Full-output height. Settings-sized 900 |
| D4 | Placement hugs the bar, `Align: "right"` (trailing), Exclusive keyboard. Escape closes | Centre like settings. Overlay-None. Pixel-perfect pill tracking (needs a trigger X on every bar) |
| D5 | Bar item `notifications` on the default right section, after battery. Left-click toggles the panel. Middle-click toggles DND. Right-click opens the duration menu without opening the panel | Left-only. Opening the panel on right-click (session battery). Hide-when-no-unread |
| D6 | Current tab groups **active** records by `desktop_entry` else `app_name` (case-folded). Sort critical first, then newest. Count badge when `count > 1`. Click expands the group | Time buckets on Current. One card per active id with no grouping |
| D7 | History tab is ungrouped, newest first. Chips: All, Last hour, Today, Yesterday, 7 days; Older only under DMS's retention visibility rule. Service retention stays 7 days | Noctalia All/Today/Yesterday/Older as the only IA. Grouping history by app |
| D8 | Header: "Notifications"; DND glyph; schedule glyph (duration menu); Clear (tab-scoped: dismiss-all on Current, history.clear on History). No settings or keyboard-hint buttons | Four 5A text buttons. One trash that also clears history from Current |
| D9 | DND presets match DMS: 15m, 30m, 1h, 3h, 8h, until tomorrow 08:00, until turned off. Permanent has no end time. Already modelled on `notifyState` | Only 1h. Persist DND in sysc-notify |
| D10 | History card close needs `history.remove` on sysc-notify. The `history-removed` delta already exists for cap eviction; v0.1.0-rc.2 has no presenter command. Shell paints the close control once the pin has the command | Fake-remove in the projection. Wait forever and omit the button |
| D11 | Card chrome is `KindCapsule` at theme radius (DMS `cornerRadius`), fill `SurfaceContainerHigh`. Critical: 2 px Primary stroke and an 8 px left Primary chip (~2% of 416). Circular 56 px icon | 32 px square `KindImage`. `"!"` urgency. Noctalia 36 px history icon |
| D12 | Relative time is English DMS copy, clock from `barView.Now` / registry `now`. History older than today: weekday + time | Noctalia "just now" / "N min ago". Live ticking every 15s as a separate clock |
| D13 | Toast width 380 (clamp 320–400). Bottom timeout bar 3 px, inset by radius, Primary, Error when critical, hidden when persistent (`DurationMS == 0`) unless critical (DMS still shows critical chrome without a draining bar). Icon 56 px circular | Noctalia top 3 px bar. Full-output paint rects (follow-on) |
| D14 | Icon: `image-data` → `KindImage`; else `AppIcon` through the existing `icons.Worker`; else a one-letter `KindText` fallback. Blank reserved slot is a defect | Theme SVG rasterizer (still unpinned). Drop the slot when missing |
| D15 | Opening the panel sets `centerOpen`, suppresses toasts, and sends `history.mark-seen` for unseen ids. Closing clears `centerOpen` | Leave toasts up over the centre |
| D16 | No new node kinds. Stroke on `KindCapsule`, optional `Height` on `KindMeter`, catalogue glyphs. Filter chips are `KindButton` `Role: "tab"` in a row, same as power profiles | `KindSegmented` (chrome catalogue, not shipped). A second panel host |
| D17 | Toast layout uses measured `ContentHeight` and a bar inset. The 96 px stub and 12 px-from-edge origin are defects, not a style | Keep 96 and hope DMS chrome is shorter. Grow exclusive zone (toasts must not steal clicks from windows) |

## Pixel contract (DMS 1.5.3, fontScale 1)

Logical pixels at scale 1.0. This machine is scale 1.0 on DP-1 and DP-3.

| Token | Value | Source |
|---|---|---|
| Panel width | 416 | `popupWidth: 400 + Theme.spacingL` |
| Panel padding | 16 | `anchors.margins: Theme.spacingL` |
| Header / list gap | 12 | `spacing: Theme.spacingM` |
| Tab row height | 32 | `DankButtonGroup.buttonHeight` |
| Title size | 16 | `fontSizeLarge` |
| Meta / body size | 12 | `fontSizeSmall` |
| Summary size | 14 | `fontSizeMedium` |
| Card padding | 12 | `notificationCardPadding` |
| Card icon | 56 circular | `notificationIconSizeNormal` |
| Action height | 24 | `actionButtonHeight` |
| Action min width | 48 | `notificationActionMinWidth` |
| Unread dot | 6, `Error`, top-right of glyph | `NotificationCenterButton.qml` |
| Timeout bar | 3 px, bottom, inset by radius | `NotificationPopup.qml` |
| Critical stroke | 2 px Primary | popup + centre cards |
| Critical chip | Primary, first 2% of width | horizontal gradient 0–0.02 |
| Empty list | 200 tall, "Nothing to see here" | `NotificationEmptyState.qml` |
| Swipe dismiss | 35% of card width | already `swipeCommitFraction` |
| Group sort | urgency desc, then time desc | `_calcGroupedNotifications` |

Relative time (`formatHistoryTime`):

- < 1 min → `now`
- < 1 h → `Nm ago`
- today, ≥ 1 h → `15:04` (24h; this shell has no 12h setting yet)
- older → `Monday, 15:04` (weekday in English)

Empty Current: glyph + "Nothing to see here". Empty History with a chip
selected: keep the same empty card rather than inventing Noctalia's
"Nothing here / No notifications match this filter."

## Information architecture

```
Bar  [notifications glyph]  unread Error dot
       left  → toggle PanelNotifications
       middle → DND on/off
       right  → duration menu

Panel (416 × content, trailing, Exclusive)
  Notifications   [bell] [schedule]              [Clear]
  [ Current (n) ] [ History (n) ]
  ── Current ──
    grouped KindCapsule cards (active only)
  ── History ──
    chip row (All / Last hour / Today / …)
    flat KindCapsule cards (closed only)
```

Clear on Current sends `active.dismiss-all`. Clear on History sends
`history.clear`. They stay separate commands.

A group of one looks like a single notification. A group of N shows a Primary
count badge on the icon and expand/collapse. Expanding lists up to 10 members
(DMS `slice(0, 10)`); extra actives in that app stay grouped but not listed
until a later slice.

## Commands

| UI | Command / local |
|---|---|
| Body default | `action.invoke` key `default` (already wired on toasts) |
| Action button | `action.invoke` |
| Dismiss / swipe / group close | `notification.dismiss` per id, or dismiss each member |
| Clear (Current) | `active.dismiss-all` |
| Clear (History) | `history.clear` |
| History card close | `history.remove` (D10; new on the service) |
| Open panel | `history.mark-seen` for unseen ids |
| DND | local `notifyState`; no notify command |

Busy writer queue: the button stays, the command is not retried. Same as toasts.

## Service addendum (sysc-notify)

`history-removed` is already a delta. Cap eviction is the only producer on
`v0.1.0-rc.2`. Add:

```text
kind: history.remove
id:   <history entry id>
```

Validate like `notification.dismiss`: unknown id → `not_found`. Success
publishes `history-removed`. Do not invent a second history file format.

The shell pin stays on the current tag until that command ships as a new rc.
History close buttons are not painted against a pin that will reject them.

## Toast clipping (live defect)

The toast the owner sees is clipped. Two independent bugs, both in this slice:

1. **Guessed height.** `toastHost.cardHeight` returns 96 for every record.
   `LayoutColumn` then packs the real tree into that box. Body, actions, and
   the 12 px pad overflow and the painter clips them. Sysmon already solved
   this with `ui.ContentHeight` plus two radii so rounded AA does not eat the
   last row (`monitorSurfaceHeight`). Toasts use the same measure.

2. **Origin under the bar.** Layout starts 12 px from the output edge
   (`toastMargin`). Default bar is 48 px on the top edge, exclusive zone −1
   so Niri does not push the overlay. The card sits under the bar. Inset the
   stack by the bar exclusive zone plus `toastMargin` on that edge (panels
   already pass `BarZone`). Bottom-edge bars inset from the bottom. Do not
   raise `ExclusiveZone` above −1.

Paint the scratch buffer at the measured rect. A card whose measure fails
falls back to 96 rather than disappearing.

## Failure behaviour

Service loss already tears toasts down. The centre closes with the rest of the
interactive root. A missing icon keeps the 56 px circle and paints the letter.
A missing `image-data` decode keeps the circle empty of raster, not a reflow.
DND survives a notify reconnect because it is shell state.

## Gate

Automated: grouping and sort, time buckets, relative time, header actions emit
the right commands, bar gestures, panel id/IPC, toast tree has bottom meter +
critical stroke, icon fallback letter, toast height follows `ContentHeight`
and clears a 48 px top bar.

Live Niri (owner-deferred, two outputs): bell maps, panel Exclusive, Clear
dismisses vs clears, DND suppresses toasts, unread dot, toast timeout bar
drains, critical chrome, swipe still 35%, toast fully visible below the bar.

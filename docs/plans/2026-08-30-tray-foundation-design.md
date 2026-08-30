# Tray Foundation Design — Milestone 5, Tranche 5B

Date: 2026-08-30
Status: Owner-locked (D5, D6 consume, D12, D14).
Branch: `milestone/notifications-tray`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`

Second tranche of Milestone 5. Builds on [5A](2026-08-30-notifications-foundation-design.md) for the image/icon package. Research: [research](2026-08-30-notifications-and-tray-research.md), [prior art](2026-08-30-notifications-and-tray-prior-art.md).

## Ordering constraints

Same gate/merge rule as 5A. Consumes tagged `sysc-tray`. Consumes 5A `internal/icons`. Consumes M4 panel machinery (overflow drawer) and M2 bar host (xdg_popup child).

## Scope

Tranche 5B ships:

- Outbound IPC client to sysc-tray: snapshot of items, ordered property changes, on-demand menu trees, reconnect.
- Tray bar widget: normal / attention / overlay icons, tooltip text via existing tooltip host if M4 has one, else Name/Role only until tooltips exist.
- Pointer: left = Activate, right = open DBusMenu (or SecondaryActivate if no menu), middle = SecondaryActivate, wheel = Scroll (roadmap requires it even though neither prior art forwards wheel).
- DBusMenu as `xdg_popup` parented to the icon rect. Keyboard-accessible. Fallback: M4 panel + KindMenu if niri cannot parent a popup to a layer-shell bar.
- Overflow drawer panel when icons do not fit the bar slot.

No XEmbed, no watcher implementation in the shell, no icon-theme in the service (shell owns lookup).

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D5 | Menu host = xdg_popup on the bar surface. Live-test niri parenting; fallback = M4 panel + KindMenu. | DMS dedicated overflow window for every menu. |
| D6 | Consume 5A `internal/icons` for named icons; pixmaps come on the snapshot as ARGB32 already bounded by the service. | Re-implement lookup in the tray widget. |
| D12 | Overflow drawer in 5B: leftover items in a panel. No hidden/pin lists. | Defer overflow; DMS hide-id chrome. |
| D15 | Track items by unique owner + object path from the snapshot. Icon pixmap preferred when present; else Lookup(name). Attention pixmap/name wins when `needsAttention`. Overlay composited on top. | DMS (ignore attention/overlay). |
| D16 | Scroll events forwarded to the service (`Scroll` dx/dy). Prior art does not; the roadmap and sysc-tray design do. | Match prior art and skip scroll. |
| D17 | One menu open process-wide. Opening another closes the first. | Stacked menus. |

## IPC client

`$XDG_RUNTIME_DIR/sysc-tray/ipc.v1.sock` until M0 pins the path. Handshake + snapshot of items. Deltas: `item-added`, `item-removed`, `item-updated`. Menu: shell sends `menu.open` `{owner,path}`; service replies with a revisioned tree; `menu.event` `{id,event}`; `menu.about-to-show`. Menu failure must not remove the icon (service rule; shell shows the icon without a menu).

Shell intents: `activate`, `secondary-activate`, `scroll` `{delta,orientation}`, `menu.open`, `menu.select`.

Reconnect: full item snapshot; bar rebuilds widgets by owner+path; no duplicate icons.

## Bar widget

M3 item id `tray`. Options none in v1. Renders a row of icon buttons (size = bar height − 2×padding). Each node: `Focusable`, `Name` = title or id, `Role` = button.

Icon source, in order: attention pixmap if `needsAttention`, else icon pixmap, else `icons.Lookup(attentionName|iconName)`. Overlay pixmap or overlay name drawn in the corner. Attention accent = theme error color on the glyph if pixmaps are empty.

Fit: measure icons left-to-right. Icons that do not fit are omitted from the bar and listed in the overflow drawer. A chevron button (Name "Tray overflow") opens panel id `tray-drawer`.

## xdg_popup

M2 `OutputHost` / bar surface gains `OpenPopup(anchorRect, size, callbacks) (close func, error)` using existing xdg-shell bindings (`xdg_wm_base.get_xdg_surface` + `get_popup` + `xdg_positioner` with bar-edge gravity). Keyboard: the popup grab, not Exclusive overlay. Roving focus inside the menu (4A model): arrows, Enter activate, Escape cancel. Accessible name/role on every entry.

Live-test first (plan Task 1): parent a 1×1 popup to the bar layer surface on niri. If `xdg_popup` is rejected, implement D5 fallback in the same task (panel + KindMenu, no second design cycle).

Clamp: compositor constraint via positioner `set_constraint_adjustment` slide/flip. Additional shell clamp to output minus padding if the compositor returns an unconstrained size.

## Overflow drawer

Panel `tray-drawer`, Exclusive, floating off the tray widget (D5 bar-anchored placement). Content: the overflowed items as a wrap/grid of the same icon buttons, same activate/menu/scroll handlers. Closes on activate or Escape.

## Gate (tray half)

| Roadmap item | 5B evidence |
|---|---|
| Registration | snapshot items appear in the bar |
| Property updates | `item-updated` changes icon/attention without flicker-remove |
| Activation | left-click → `activate` |
| Scrolling | wheel → `scroll` |
| Menus | xdg_popup (or fallback panel), keyboard traversal, `menu.select` |
| Owner replacement | unique owner+path; stale menu dropped |
| Restart | shell reconnect restores the set; no duplicate ids |
| Malformed pixmap | that item omitted or placeholder; others stay |
| Shell absence | cannot discard tray registration (service-side) |

## Risks

- niri xdg_popup-from-layer-shell is the load-bearing live test. Fallback is specified.
- Tray M0 name variants / watcher policy not pinned. Client talks only to sysc-tray's socket; the shell never claims `StatusNotifierWatcher`.
- Tooltip host may not exist (M3 skipped tooltips). Gate does not require tooltip surfaces; Name/Role on the icon is the accessibility floor.
- Scroll on a bar icon may fight bar-level axis handlers. The tray widget consumes the wheel when the pointer is over an icon.

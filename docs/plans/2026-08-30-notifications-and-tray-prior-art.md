# Milestone 5 — Notifications and System Tray Prior Art

Date: 2026-08-30
Status: Research inventory (read-only)
Branch: `milestone/notifications-tray`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`
Sources: Noctalia (`/home/nomadx/noctalia`, meson 5.0.0), DMS v1.5.3 (`/usr/share/quickshell/dms`), `sysc-notify` and `sysc-tray` approved designs (2026-08-27).

This document feeds the Milestone 5 design. It does not decide product scope. Every claim below is from source or from the approved service designs.

---

## 0. Architecture fork that M5 must resolve

M4 designed `ipc.v1.sock` as a **shell-owned server** (`$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock`) and noted that sysc-notify / sysc-tray would later share method namespaces on that socket.

The approved service designs say the opposite:

- Each service owns a **private Unix socket**. The shell is a presentation **client**.
- The service must complete `Notify` / item registration **without waiting for the shell**.
- Zero clients is a supported state. Slow or malformed shell is disconnected; reconnect gets a fresh snapshot.
- Shell absence must not block D-Bus availability.

Implication: M5 adds **outbound IPC clients** (dial, handshake, snapshot, ordered changes, reconnect) alongside the existing inbound CLI/hotkey server. Do not fold notify/tray verbs onto `ipc.v1.sock`.

---

## 1. sysc-notify (approved design, docs-only repo)

Path: `/home/nomadx/sysc-notify/docs/plans/2026-08-27-sysc-notify-design.md` and `docs/roadmap.md`.

**Boundary.** Service owns D-Bus name, methods/signals, IDs, replacement, expiry, active state, bounded in-memory history, sender metadata, presentation-client connection. Shell owns Wayland surfaces, visual policy, interaction, Niri state.

**Spec.** Notification specification 1.3. `Notify` returns the supplied non-zero replacement ID when that notification exists, else allocates a new non-zero ID. Close reasons: expiry, user dismiss, `CloseNotification`, undefined removal. Actions emit `ActionInvoked`. Activation tokens deferred until shell supplies `xdg_activation_v1`.

**Honest capabilities.** Do not advertise body markup, persistence, sound, action icons, or activation tokens until those paths exist end-to-end.

**Trust.** Bound strings, action count, hint count, image dimensions, stride, decoded bytes, active count, history count, per-client rate. Overflow-safe image arithmetic. Malformed values must not drop already-held notifications.

**PID lineage.** Service queries the bus daemon for the sender Unix PID. May record bounded `/proc` parent lineage in ephemeral state. No `BecomeMonitor`, no persisted PIDs, no Niri in the service. Shell matches ancestry against cached Niri windows, stores ephemeral notification-ID → window-ID, and **does not focus** when several windows match. Window IDs do not survive a shell restart.

**IPC.** Private Unix socket, peer-credential checks, versioned length-bounded protocol. Initial snapshot then ordered changes. Fall-behind → drop connection; reconnect + snapshot, not an unbounded queue. `Notify` must not wait on the shell.

**State.** v1 history is memory-only. D-Bus name loss terminates the service. Shell disconnect preserves notification state.

**Roadmap gates (service side).** M0 contract → M1 headless D-Bus → M2 shell transport → M3 shell presentation → M4 tag `v0.1.0`. Persistent history and activation tokens are later.

---

## 2. sysc-tray (approved design, docs-only repo)

Path: `/home/nomadx/sysc-tray/docs/plans/2026-08-27-sysc-tray-design.md` and `docs/roadmap.md`.

**Boundary.** Service owns watcher acquisition or attachment, host registration, item proxies, property-change signals, item method calls, menu trees, menu events. Shell owns Wayland surfaces, bar layout, **icon-theme resolution**, pixels, pointer/keyboard, menu placement, theme.

**D-Bus.** StatusNotifierWatcher / Host / Item + DBusMenu as used by current Linux apps. M0 pins name variants, watcher takeover, host identity, item-address forms. Track items by **unique bus owner + object path**, not a reusable well-known name. Coalesce property bursts; discard stale replies after owner change. DBusMenu layout revision numbers order tree updates. Menus requested **on demand**. Renderer-neutral properties only.

**Trust.** Bound item count, menu depth/node count, strings, property maps, pixmap dimensions, decoded bytes, update rate. Malformed item/menu must not remove healthy items. Drop in-flight replies and cached menus when unique owner disappears.

**IPC.** Same shape as notify: private Unix socket, peer creds, versioned length-bounded protocol, snapshot then ordered changes. Shell sends intents: activate, secondary activate, scroll, open menu, select menu item.

**Recovery.** Own the watcher **or** attach as host; do not fight a healthy owner in a name-acquisition loop. Item disappearance removes only that item. DBusMenu failure leaves the icon usable. Shell loss preserves item state.

**Prior art named in the design.** `nekorg/pawbar` (BSD-3) for watcher/host; DMS tray recovery as behavioral reference.

**Roadmap.** M0 contract → M1 watcher/host/items → M2 shell transport → M3 DBusMenu → M4 shell presentation + tag `v0.1.0`. Legacy XEmbed out of scope.

---

## 3. Noctalia — notifications

Monolithic C++ process. D-Bus via sdbus-c++. Implements `org.freedesktop.Notifications` **in-process**. Unix-socket JSON IPC is a second-invocation CLI (`notification-show`, `notification-dnd-*`, `notification-clear-*`, `notification-invoke-latest`), not the presentation seam.

### 3.1 Toast surface

| Field | Value | Source |
|---|---|---|
| Namespace | `noctalia-notification` | `notification_toast.cpp:1894` |
| Layer | Top or Overlay from `config.notification.layer` | `:1827`, `:1894` |
| Anchor | Top/Bottom × Left/Right/Center from position suffix | `:336-352` |
| Exclusive zone | `0` | `:1899` |
| Keyboard | `None`, flipped to `OnDemand` only while inline-reply Input focused | `:2763-2771` |
| Height | `0` (full output height via dual vertical anchors; cards positioned inside) | `:1900` |
| Width | `360 * scale` plus pads and edge inset | `:106-116`, `:1852` |

**Per-output.** One layer surface per matching output (`:1866-1947`). Empty monitor filter = all outputs; unplugged configured outputs fall back to all so toasts never vanish (`:1881-1890`).

**Stacking.** One surface per output, one card node per active notification on that surface. Uncapped until overflow; overflowed cards queued at `y = -1`, expiry **paused** off-screen and resumed at full duration when a slot opens (`:32`, `:1089-1108`). Shared placement skeleton measured in the tallest output, remapped per instance (`:1628-1712`). `top_*` stacks down, `bottom_*` stacks up (`:1553`).

**Motion.** Entry from anchored edge, `animNormal`, EaseOutCubic, clip via `setFrameSize` reveal (`:218-291`, `:982-984`). Exit reverse, EaseInOutQuad (`:1217-1239`). Stack collapse slide on dismiss (`:1788-1857`). Hover-pause flash 0.7→1.0 over `animFast` (`:771-778`).

**Expiry.** Default 6000 ms (`notification_manager.h:32`). Spec mapping: `0` persistent, `<0` server default, `>0` that value (`h:36-43`). Hover increments `hoverOwners` and pauses both visual bar and manager timer (`notification_toast.cpp:1238-1474`). Critical keeps the colored bar even without timeout (`:208-214`, `:1309-1316`). Transient excluded from history (`notification_manager.cpp:48-50`).

**Dismiss.** Left-click body = default action + close (`notification_toast.cpp:2181-2193`). Right-click / × button = `CloseReason::Dismissed`. No swipe.

### 3.2 Data model and rendering

Consumed: summary, body (UTF-8 truncated 1024), app_name (desktop-entry pretty name), app_icon, `image-data`/`image_data`/`icon_data` `(iiibiiay)`, `image-path` (read file → decode → repack as image-data so history survives file rewrite), actions (max 6 pairs, empty keys dropped, `default` filtered from button row), urgency, category, desktop-entry, transient.

**Not consumed:** `value` progress hint, `resident` hint.

**Icon resolve** (`notification_toast.cpp:2862-2921`): `noctalia-glyph:` → glyph atlas; remote URL → `/tmp/noctalia-notification-icons/<hash>.img`; absolute readable path; else `IconResolver` (gsettings GTK theme, gtk-3/4 settings.ini, `Inherits` chain, **hicolor always last**, `$XDG_DATA_HOME/icons`, `~/.icons`, `$XDG_DATA_DIRS/icons`, pixmaps fallback, SVG preferred). Fallback glyph `bell`.

**Image decode.** wuffs (PNG/BMP/JPEG/ICO), libwebp, libjxl. History persist: RGBA downsample to 96 px, WebP quality 65, content-addressed sidecar (`notification_history_store.cpp:130-178`).

**Markup.** `sanitizeMarkup`: `<br>` → newline, XML entities decoded, all other tags stripped (`string_utils.h:380-420`).

**Actions.** Button rows; `inline-reply` swaps in an Input and flips keyboard to OnDemand; D-Bus key `inline-reply::<text>` (KDE convention). `invokeAction` emits `ActionInvoked` and closes with Dismissed.

**Progress UI.** Countdown bar only (urgency color). No `value` hint bar.

### 3.3 Center / history / DND

Center is a **control-center tab**, not a separate panel (`control_center_panel.h:109`). Disk history: `$XDG_STATE_HOME/notification_history.json` + WebP sidecars, cap 100, retention-hours timer, filters (app/category/desktop-entry/regex, per-filter toast/history/sound/permanent/duration/urgency). Clear-all emits pending D-Bus closes. Unread `seen` flag drives bar badge.

DND: boolean. Suppresses **toasts and sound**, not history. IPC verbs `notification-dnd-set|toggle|status`. OSD feedback on toggle.

### 3.4 D-Bus service

Object `/org/freedesktop/Notifications`. Methods: Notify, GetCapabilities (`actions`, `body`, `persistence`, `inline-reply`), GetServerInformation (`noctalia`, spec 1.2), GetNotifications (Noctalia extension), CloseNotification, InvokeAction (KDE-style extension). Signals: NotificationClosed, ActionInvoked. Close reasons 1/2/3 match the spec. `replaces_id` overwrites in place. Implicit duplicate window 1 s. KDE path: register as NotificationWatcher and Inhibit Plasma's UI.

**Sender PID is not used for notifications.** (Tray does use PID.)

---

## 4. Noctalia — system tray

In-process `TrayService` (`src/dbus/tray/tray_service.cpp`, ~2183 lines). sdbus-c++. Object `/StatusNotifierWatcher`, interface `org.kde.StatusNotifierWatcher`.

### 4.1 Watcher ownership

- Name already owned → **client mode**: `RegisterStatusNotifierHost` as `org.kde.StatusNotifierHost-<pid>`, subscribe to item registered/unregistered, reconnect on watcher `NameOwnerChanged`.
- Else → **owner mode**, emit `HostRegistered`.
- Non-KDE: always owner (`startLegacyOwner`).
- Discovery: `ListNames` + `NameOwnerChanged`. Path-only registration uses sender unique name. Bus-only names probed 5×250 ms. Track prefixes `org.kde.|org.freedesktop.|org.ayatana.StatusNotifierItem-`.

### 4.2 Item model (`TrayItemInfo`, `tray_service.h:14-40`)

`id`, `busName`, `objectPath`, `iconName`, `iconThemePath`, `overlayIconName`, `attentionIconName`, `menuObjectPath`, titles, `status`, ARGB32 pixmaps + sizes for icon/overlay/attention, `needsAttention`.

Render priority in `tray_widget.cpp:646-650`: if `needsAttention` and attention pixmap present, use attention pixmap (same for named icons at `:894`). Overlay drawn on top (`:702`). Attention accent = Error role (`:740`). Tooltip via `InputArea::setTooltip` → existing wl_popup tooltip host (`:788`).

### 4.3 Activation

Bar widget accepts `BTN_LEFT` + `BTN_RIGHT` (`tray_widget.cpp:766-782`). Left → `Activate`. Right → `openContextMenu`. **No `Scroll` method call exists anywhere in the tree** — Noctalia does not forward wheel to items.

### 4.4 DBusMenu

`TrayMenuEntry`: id, label, iconName, iconData, enabled, visible, separator, hasSubmenu, checkmark, radio, toggleState (`tray_service.h:43-57`). `GetLayout` with retry; retry halted while the user is interacting (`tray_menu.cpp:316`). `LayoutUpdated` refreshes via `onTrayChanged`.

**Surface: xdg-shell popup**, not layer-shell. `popup_chrome` + `xdg_positioner` anchored to the icon rectangle (`tray_menu.cpp:122-167`). While open, parent bar layer keyboard is set to **OnDemand**, restored to **None** on close (`:744-745`, `:835-836`). Keyboard and pointer handled on `TrayMenu` (`:354`, `:365`).

### 4.5 Drawer

`TrayDrawerPanel` is a Panel, keyboard OnDemand, reuses `TrayWidget`. Hidden/pinned item lists from config (`tray_drawer_panel.h`).

---

## 5. DMS v1.5.3 — notifications

Single quickshell process. **Does not implement** `org.freedesktop.Notifications`. Instantiates Quickshell `NotificationServer` (`Services/NotificationService.qml:655-687`) with the full capability set (`actions`, actionIcons, bodyHyperlinks, bodyImages, bodyMarkup, image, inlineReply, persistence).

### 5.1 Popup surface

| Field | Value | Source |
|---|---|---|
| Namespace | `dms:notification-popup` | `NotificationPopup.qml:55` |
| Exclusive zone | `-1` | `:163` |
| Keyboard | `None` | `:164` |
| Layer | Overlay if overlay-enabled or Critical, else Top; `DMS_NOTIFICATION_LAYER` override | `:155-160` |
| Anchors | top+left only (positioning via margins) | `:280-283` |

**Per-output.** `Variants` over screens (`DMSShell.qml:363-366`). Optional focused-monitor filter via `CompositorService.getFocusedScreen()` (niri `currentOutput`).

**Stacking.** **One PanelWindow per notification**, cap `maxVisibleNotifications: 4`, queue 32 (`NotificationService.qml:27,36`). Evict oldest unhovered (else oldest) by `seq`. Manager `_repositionAll` stacks from the chosen corner, pinned (hovered) slots hold place (`NotificationPopupManager.qml:215-242`).

**Motion.** Enter/exit from `Theme.notification*Duration` (base × 0.875 / 0.75). Swipe-to-dismiss at 35% width (`NotificationPopup.qml:1180-1324`). Expand/collapse for long body.

**Expiry.** Wrapper timer: sender `expireTimeout >= 0` wins, else settings Low/Normal/Critical (defaults 5000 / 5000 / **0 persistent**) (`NotificationService.qml:466-499`, `SettingsData.qml:912-914`). Hover stops timer; leave restarts (`NotificationPopup.qml:753-766`). Timeout drain bar optional (`notificationShowTimeoutBar`).

**Dismiss.** Close button; hover action buttons (max 2); body click = expand or first action or dismiss; right-click context menu (rules / mute app / dismiss) as Overlay `keyboardFocus: None`; swipe.

### 5.2 Data model

Quickshell `Notification` wrapped as `NotifWrapper`. Fields: summary/body with `<img>` stripped, htmlBody via markdown2html + entity decode, appIcon, appName (desktop-entry heuristic fallback), desktopEntry, image (`image://qsimage/` for image-data), actions, urgency, timestamp.

**Icon.** `Paths.resolveIconPath` → `IconThemeService` (walk `index.theme` Inherits, `find` svg/png) → `Quickshell.iconPath` → DesktopService.

**Image-data persist.** `grabToImage` → PNG in `$XDG_CACHE_HOME/.../notification_images/notif_<ts>_<id>.png` so history survives the live `qsimage` handle.

**No `value` hint progress.** Timeout bar only.

**Center overlay disables popups** (`onOverlayOpen` sets `popupsDisabled`, clears visible + queue).

### 5.3 Center / history / DND

`NotificationCenterPopout` is a DankPopout (`dms:notification-center-popout`, full-height, Exclusive keyboard on Niri). Groups by desktopEntry else appName, urgency then time sort. Keyboard list: Up/Down, Enter = first action, etc.

History: FileView JSON, cap 50, max age 7 days, per-urgency save toggles, image cache cleanup. History cards have **no actions**.

DND: `SessionData.doNotDisturb` + optional until-timestamp with presets (15m…until tomorrow 8am). Suppresses popups, not history. Enabling DND hides visible popups and clears the queue.

---

## 6. DMS v1.5.3 — system tray

Quickshell `SystemTray.items` (`Modules/DankBar/Widgets/SystemTrayBar.qml`). DMS does **not** reimplement the watcher.

**Icons.** `item.icon` only. `?path=` rewritten to `file://`. Dropbox status files remapped to hicolor. **Attention / overlay / Status / tooltip surface are not consumed.** Fallback = first letter of id.

**Clicks.** Left → `trayItem.activate()`. Right → if `hasMenu`, in-window DBusMenu drill-down (`showForTrayItem`); else `contextMenu(x,y)` fallback. **No wheel/Scroll forwarding found.**

**Menu.** Custom QML inside overflow `PanelWindow` (`dms:tray-overflow-menu`, Overlay or Top, exclusiveZone `-1`, keyboard via `KeyboardFocus` helper). Model = `trayItem.menu` (Quickshell DBusMenu): `hasChildren`, `activate`/`triggered`, `isSeparator`, `buttonType`. Submenus via `entryStack` + Back. Visibility toggle (hide / show / promote from auto-overflow) lives in the same menu chrome.

**Overflow.** Inline vs overflow popout from widget setting. Hidden ids in `SessionData.hiddenTrayIds`; order map `trayItemOrder`.

**Restart.** Relies on Quickshell's StatusNotifierItem tracking. No separate daemon, so a shell restart is a watcher/host restart from the application's point of view.

---

## 7. Cross-shell comparison (parity implications)

| Topic | Noctalia | DMS | sysc design so far |
|---|---|---|---|
| D-Bus owner | In-process Notifications + Watcher | Quickshell in-process | **Separate processes** (sysc-notify, sysc-tray) |
| Toast keyboard | None (OnDemand only for inline-reply) | None | Overlay + None matches both; Exclusive would steal keys from the focused window |
| Toast exclusive zone | `0` | `-1` | Prefer `-1` (M4 OSD precedent: do not reserve) |
| Stacking | One surface per output, N cards | One surface per notification, cap 4 | One-surface-per-output cheaper (M4 OSD/aux already exists); cap needed |
| Progress | Countdown only | Countdown only | Roadmap lists progress — **neither prior art implements `value` hint**. Decide honestly. |
| Markup | Strip tags, keep `<br>` | StyledText HTML + markdown | sysc-notify: do not advertise markup until e2e works |
| History | Disk, cap 100, control-center tab | Disk, cap 50, dedicated popout | sysc-notify v1 = memory only; disk is a later service gate |
| DND | Suppress toast+sound, keep history | Suppress popup, keep history | Same pattern if M5 ships DND |
| Sender focus | Unused for notifications | Unused | **sysc-notify unique:** PID lineage + unambiguous-only focus |
| Tray watcher | Own or attach (KDE) | Quickshell owns | sysc-tray own-or-attach, no fight loop |
| Tray attention/overlay | Consumed | Ignored | Roadmap requires both |
| Tray scroll | Not forwarded | Not forwarded | Roadmap requires scrolling — **prior art does not**; still ship (spec + design) |
| Tray menu surface | xdg_popup parented to bar | layer-shell overflow window | M4 has no xdg_popup. Layer-shell in-panel KindMenu (4B) or new xdg_popup. Need a decision. |
| Tray tooltip | wl_popup | None | No tooltip primitive in M3/M4 |

---

## 8. Niri-relevant facts already held (from M4 research)

- Overlay beats fullscreen; Top is hidden by fullscreen. Notification popups that must appear over fullscreen games → Overlay.
- Exclusive keyboard on Overlay steals keys from windows. Toasts with keyboard None keep compositor binds and window typing working. Inline-reply is the only reason Noctalia flips OnDemand.
- Unmap of an exclusive surface restores layout focus automatically. Toasts never take Exclusive, so this is irrelevant for popups. Tray **menus** that take keyboard need the same fall-through as M4 panels, or an xdg_popup grab.
- Niri does not animate layer surfaces. Entry/exit motion is the shell's job, gated by reduced-motion (M4 D13).
- Niri Window events carry `pid` (M3 audit). Lineage matching is shell-side against that cache.
- No notification or tray protocol in niri itself.

---

## 9. Shell work M4 does not cover

1. **Image pipeline.** Decode PNG/JPEG (stdlib `image` + `x/image` already in go.mod). Raw `image-data` (RGB/RGBA, stride, 8-bit). Bound dimensions. Optional persist-to-cache for history (DMS pattern) — only if M5 ships history.
2. **Icon-theme lookup.** Freedesktop `index.theme` + Inherits + hicolor + pixmaps. sysc-tray design assigns this to the **shell**, not the service. Noctalia `IconResolver` and DMS `IconThemeService` are the references.
3. **Notification popup surfaces.** Corner-anchored, keyboard None, exclusive_zone −1, Overlay, per-output, stack or cap, hover-pause expiry, actions, dismiss, urgency chrome.
4. **PID → window matching.** Ephemeral, unambiguous-only focus. Ambiguous = no focus (gate).
5. **Tray bar widget.** Normal / attention / overlay icons. Activate / secondary / context menu / scroll intents back to sysc-tray.
6. **DBusMenu surface.** Keyboard-accessible, clamped to output. 4B KindMenu is in-panel; tray menus today in prior art are either xdg_popup (Noctalia) or a dedicated layer window (DMS).
7. **IPC clients.** Dial service sockets, version handshake, snapshot + ordered deltas, reconnect, peer-cred. Distinct from `ipc.v1.sock`.

---

## 10. Suggested first decisions (not yet taken)

These are the forks the design must settle. Research for each is above.

1. **IPC topology** — shell dials service sockets (honors approved designs) vs services connect to `ipc.v1.sock` (contradicts them).
2. **M5 surface set** — popups only (roadmap minimum) vs popups + notification center + DND (+ memory history from the service snapshot).
3. **Progress** — implement `value` hint (roadmap) vs countdown-only (both prior arts) vs both.
4. **Tray menu host** — xdg_popup (needs new sysc-wayland bindings + M2 surface type) vs layer-shell panel reusing 4B KindMenu vs DMS-style dedicated overflow window.
5. **Image/icon lookup** — land inside M5 vs a tiny prerequisite tranche on the M4 branch.
6. **Inline-reply / markup / swipe / disk history** — later, matching honest sysc-notify capabilities.

---

## Skipped

- Full DBusMenu GetLayout wire format (belongs in sysc-tray M0, not this shell design).
- Pawbar source extraction (named by sysc-tray design; not on disk here).
- Notification sound / per-app filters (Noctalia-only; not in sysc-notify v1).
- Connected-frame chrome (DMS; niri-only shell has no equivalent).

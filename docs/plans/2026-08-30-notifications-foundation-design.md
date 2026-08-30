# Notifications Foundation Design — Milestone 5, Tranche 5A

Date: 2026-08-30
Status: Owner-locked (D1–D4, D6–D11, D13–D14; D9 chosen by owner).
Branch: `milestone/notifications-tray`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`

Milestone 5 is split like Milestone 4:

- **Tranche 5A (this document)** — notify IPC client, sysc-notify persistence addendum, image/icon package, toast surfaces, notification center, DND, PID lineage.
- **Tranche 5B** — tray IPC client, bar widget, xdg_popup DBusMenu, overflow drawer. See [the 5B design](2026-08-30-tray-foundation-design.md).

Image/icon lookup ships here because toasts consume it; 5B reuses the same package.

Research: [research](2026-08-30-notifications-and-tray-research.md), [prior art](2026-08-30-notifications-and-tray-prior-art.md), [notify persist](2026-08-30-sysc-notify-persistence-design.md).

## Ordering constraints

- No product code on this branch until M2 live Niri gate, M3, and M4 merge (roadmap: later work does not enter until the current milestone gate passes).
- Shell consumes **tagged** `sysc-notify` / `sysc-tray` releases, not local replace (roadmap:178–179). Persistence work happens in the notify repo first; 5A's shell client assumes the snapshot shape in the persist addendum.
- Docs-only worktree from main is allowed now (M3/M4 precedent).

## Scope

Tranche 5A ships:

- Outbound IPC client to sysc-notify (D3): dial, version handshake, snapshot, ordered deltas, reconnect, peer-cred. Distinct from M4 `ipc.v1.sock`.
- Image decode + icon-theme lookup (`internal/icons`).
- Toast host: one Overlay aux surface per output with a bar (D7, D8), keyboard None / OnDemand for inline-reply (D9), exclusive_zone −1, no shield.
- Cards: summary, body (sanitized markup), app icon or image-data, actions, countdown + optional `value` bar (D4), swipe-to-dismiss, hover-pause expiry, default-action click, close.
- Notification center: M4 panel id `notifications` (D10), history from snapshot, clear-all, grouping by desktop-entry else app-name.
- DND: shell bool + optional until-timestamp; suppresses toasts only (D11).
- PID lineage: match sender ancestry against cached Niri windows; focus only an unambiguous match.

It ships no tray, no `xdg_activation_v1`, no sound, no per-app filters, no control-center tab.

## Decisions

Research D1–D14 apply. Tranche-local restatement:

| # | Decision | Rejected alternative |
|---|---|---|
| D3 | Shell dials `$XDG_RUNTIME_DIR/sysc-notify/ipc.v1.sock` (path pinned by notify M0; this is the working name). `ipc.v1.sock` on the shell stays CLI/hotkeys. | Fold notify methods onto the shell socket. |
| D7/D8 | One Overlay aux per output, exclusive_zone −1, height 0 / dual vertical anchors, cards positioned inside. Namespace `sysc-shell-notification`. | One window per toast; Top layer. |
| D9 | Keyboard None; OnDemand only while the inline-reply field is focused. | None always; Exclusive on the toast surface. |
| D10 | Center = panel `notifications`, centered, Exclusive, ~400×620 fitted. | Control-center tab; bar-attached full-height popout. |
| D11 | DND in shell session/config. Hides toasts and skips enqueue; history and `Notify` continue. | Store DND in the service. |
| D13 | History file is notify-owned. Shell renders `history[]` from the snapshot. | Shell-side JSON cache. |
| 5A-1 | Markup: strip tags except `<br>` → newline, decode XML entities (Noctalia `sanitizeMarkup`). Render as plain text. Do not advertise `body-markup` until a real HTML path exists. Owner asked for markup parity; sanitizing to text is the honest subset without a web renderer. | Qt StyledText HTML; raw HTML. |
| 5A-2 | Swipe: pointer drag on the card, threshold 35% of card width (DMS), dismiss with CloseReason Dismissed. Instant under reduced-motion (no swipe animation). | No swipe. |
| 5A-3 | Expiry: sender `expire_timeout` ≥ 0 wins; `< 0` uses shell defaults Low=5000 / Normal=5000 / Critical=0 (persistent). Hover pauses. Overflowed (queued) cards pause and resume at **full** duration when shown (Noctalia). | DMS eviction of oldest unhovered. |
| 5A-4 | Default action = left-click card body when not expanding/swiping. `default` key is not a button. Max 6 action buttons. Inline-reply key swaps the button row for a single-line field (4B text field) and flips OnDemand. | Body click always dismisses. |
| 5A-5 | PID match: service snapshot carries `sender_pid` + optional parent pids. Shell compares to cached Niri `Window.pid`. Zero or >1 match → store target as ambiguous, **do not** `focus-window`. Window ids are ephemeral; dropped on shell restart. | Focus first match; persist window ids. |

## Toast surface

Reuse M4 aux: `AuxSpec{ID: "notify:<global>", Namespace: "sysc-shell-notification", Layer: Overlay, ExclusiveZone: -1, Keyboard: None, Anchor: top+bottom+left or +right from position token}`. Default position top-right, offset by bar reserved zone + 8 px padding. Shown on every output that has a bar (DMS/Noctalia per-output).

No dismiss shield. Clicks on empty surface pixels are not consumed (input region = union of card rects).

Stack: `top_*` down, `bottom_*` up. Card width 360×scale. When the next card would overflow the output, queue it (`queued`, expiry paused). When a slot opens, dequeue, restart timeout at full duration, play enter motion (M4 fade+8 px slide, reduced-motion instant).

## Card

Icon: image-data / image-path decoded by `internal/icons` → 48 px; else `Lookup(app_icon)`; else glyph bell.

Body: 5A-1 plain text, max ~4 lines then expand-on-click (DMS). Actions row under body. Countdown bar 3 px, urgency color (critical = error, else primary). `value` hint bar only when present, 0–100, separate from countdown.

Center overlay: opening panel `notifications` hides all toasts and pauses enqueue (DMS `popupsDisabled`). Closing resumes.

## IPC client

`internal/ipc/notifyclient`: dial, `SO_PEERCRED` uid check, length-prefixed or newline JSON (match notify M0; until pinned, newline JSON like M4). First message from server = snapshot. Client sends `action.invoke` `{id,key}`, `dismiss` `{id}`, `inline-reply` `{id,text}`, `history.clear`, `focus-ack` unused.

Slow drain: if the read buffer exceeds the bound, close and reconnect. `Notify` on the bus must not wait on this socket (service rule).

Absence of the socket: shell runs, no toasts, center empty. Not a fatal error. Status IPC field `notify: unavailable`.

## PID lineage

On `added`, compute matches. If exactly one Niri window pid is in the sender pid or its recorded parents, store `notifID → windowID` in process memory. Focus that window only when the user activates the notification (default action or an explicit "focus" policy). Ambiguous or zero: no focus. Do not call `niri msg action focus-window` for newly appeared toasts automatically — that would steal focus (neither prior art does). The roadmap gate is "ambiguous focus pass": activating an ambiguous notification must not pick a window.

## Center

Panel `notifications` on 4A machinery. Sidebar chips All / Today / … optional later; v1 = one virtual list of `history[]` grouped by desktop-entry else app-name, newest first. Header: DND toggle, clear-all. Search field filters labels. History cards have no action buttons. Active notifications that are also in history appear once (prefer active row with actions).

## DND

`session.doNotDisturb` bool + `doNotDisturbUntil` unix ms. Settings registry entries in 4B style (or a header toggle only). When true: toast host ignores `added` for display; history still updates from snapshot. Optional until-timestamp: a timer clears the flag.

## Image package

`internal/icons`:

- `DecodeFile(path) (image.Image, error)` — PNG/JPEG via stdlib. Limit encoded bytes before decode,
  call `image.DecodeConfig` first, and reject dimensions, decoded bytes, and pixel work above the fixed
  protocol limits before allocating the destination.
- `DecodeRaw(width,height,stride,hasAlpha,bits,channels,data []byte) (image.Image, error)` — 8-bit
  RGB/RGBA. Reject non-positive or over-limit dimensions, overflow in `width*channels` and
  `stride*height`, invalid stride, inconsistent alpha/channel metadata, and any data length other than
  the validated stride times height before allocating.
- `Lookup(name, size, scale, themeState) (string, error)` — parse freedesktop `index.theme` directory
  metadata and `Inherits`, always append hicolor, and search the XDG icon roots plus pixmaps. Reject
  traversal in icon names and theme inheritance, and reject candidates whose resolved symlink target
  escapes the root being searched. Cache source identity, size, scale, and theme generation in a bounded
  LRU; invalidate it when the selected theme or roots change.

No new module. SVG rasterize is a documented ceiling: if only SVG exists, pass the path through and skip drawing until a rasterizer exists, or draw the fallback glyph. First green tests use PNG fixtures.

## Gate (notification half)

| Roadmap item | 5A evidence |
|---|---|
| Replacement | `replaced` updates the existing card in place |
| Actions | button / default-click → `action.invoke`; `ActionInvoked` is the service's job |
| Close reasons | dismiss / expiry / CloseNotification reflected as card removal; service emits the spec reason |
| Expiry | timer + hover-pause + overflow pause; Critical persistent default |
| Shell restart | reconnect snapshot restores active popups |
| Ambiguous focus | no `focus-window` when 0 or >1 pid matches |
| Malformed image | that notification still shows text; decode error does not drop others |
| Shell absence | `Notify` still succeeds (service-side; live checklist) |

## Risks

- Notify M0 has not pinned the byte protocol. 5A plan Task 1 is a contract freeze against the tagged release; if the release differs, the client adapter changes, not the surface model.
- Inline-reply OnDemand on a height-0 full-output surface: input region must include the field or niri will not deliver keys. Live-test with the card's input region, not the whole output.
- Markup "parity" without an HTML renderer: 5A-1 is the honest subset. A future HTML path would be a new capability claim.
- Persistence is a notify-repo release gate. Pure shell work that does not depend on the service contract
  may land earlier, but Tranche 5A cannot pass its gate or ship the center until the tagged release carries
  D13's `active[]` plus `history[]` contract and persistence capability.

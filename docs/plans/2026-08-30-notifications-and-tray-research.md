# Milestone 5 — Notifications and Tray Research

Date: 2026-08-30
Status: Decisions locked (D1–D14)
Branch: `milestone/notifications-tray`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`

Method: niri-capability + prior-art before each decision. Inventory: `2026-08-30-notifications-and-tray-prior-art.md`. Owner stopped the Q&A after D9 and ordered the design and plans written; D10–D14 are locked from D1 (full parity) plus M4 machinery, not re-asked.

---

## Owner decisions

| ID | Topic | Decision | Rejected |
|---|---|---|---|
| D1 | Parity boundary | Full Noctalia/DMS parity: popups + notification center + DND + disk history + inline-reply + swipe + markup | Roadmap-minimum only; roadmap + center/DND without disk |
| D2 | Disk history owner | Expand **sysc-notify** to persist. Design persistence now. History rides the snapshot; shell never owns the file | Shell-side JSON cache; memory-only in M5 |
| D3 | IPC topology | Shell **dials** each service socket as a presentation client. `ipc.v1.sock` stays CLI/hotkeys only | Services connect to `ipc.v1.sock`; dual topology |
| D4 | Progress UI | **Both**: countdown bar always (Critical keeps bar with no timeout). Separate `value` hint bar when the sender sends it | Countdown only; `value` hint only |
| D5 | Tray menu host | **xdg_popup parented to bar**. Live-test: niri must parent `xdg_popup` to a layer-shell bar; if not, fall back to M4 panel + KindMenu | Layer-shell panel + 4B KindMenu; DMS overflow window |
| D6 | Image + icon-theme | **Inside M5** (tranche 5A). PNG/JPEG + raw `image-data` + freedesktop `index.theme` lookup. Consumers: popups + tray | Prerequisite tranche on M4; split decode vs lookup |
| D7 | Toast stacking | **One layer surface per output**. Overflow queues off-screen, pauses expiry, resumes full duration | One surface per notification; hard cap with no queue |
| D8 | Toast layer | **Overlay always**. Exclusive zone −1. No config knob | Overlay-for-Critical; Top\|Overlay setting |
| D9 | Toast keyboard | **None**, flipped to **OnDemand only while inline-reply is focused** (owner chose 1) | None always; Exclusive while any toast visible |
| D10 | Notification center | M4 **panel** id `notifications`, centered like settings (~400×620 fitted). No control-center tab exists. Not a bar-attached full-height popout | Noctalia control-center tab; DMS DankPopout |
| D11 | DND | Shell-owned bool + optional until-timestamp in session/config. Suppresses **toasts only**, not history, not D-Bus `Notify` | Persist DND in sysc-notify; suppress history |
| D12 | Tray overflow | **In 5B**: items that do not fit the bar slot go in a drawer panel (M4 panel, Exclusive). Chevron on the tray widget opens it. Hidden/pin lists deferred | Overflow later; DMS hide-id chrome |
| D13 | Persist format | `$XDG_STATE_HOME/sysc-notify/history.json` (0600) + PNG sidecars under `images/<sha256>.png`. Cap 100. Retention 7 days default. Transient excluded. Image-data downscaled to 96 px. Active notifications are not the disk file — disk is closed history only; snapshot sends `active[]` + `history[]` | WebP extra dep; persist raw pixels; world-readable paths |
| D14 | Tranche split | **5A Notifications** (IPC client, persist in notify, image/icon, toasts, center, DND, PID). **5B Tray** (IPC client, bar widget, xdg_popup menu, scroll, overflow). Image package ships in 5A; 5B consumes it | One mega-plan; image as its own tranche |

---

## Implications

**Two repos.** 5A includes a sysc-notify persistence addendum (`2026-08-30-sysc-notify-persistence-design.md`) that belongs in `/home/nomadx/sysc-notify` at implementation time. Shell product code still waits on tagged releases (roadmap:178–179).

**Honest capabilities.** Advertise `body`, `actions`, `persistence`, `inline-reply` only after those paths exist end-to-end. Do not advertise sound, action-icons, or activation tokens.

**M4 D12 override.** M4 noted notify/tray would share method namespaces on `ipc.v1.sock`. D3 supersedes that: each service owns its socket; the shell is a client.

**xdg_popup risk.** Confirm niri parents a popup to a layer-shell bar before 5B product code. Fallback is already named (D5).

**SVG.** Named-icon SVG path preferred in lookup; rasterize later. First ship PNG/JPEG + raw `image-data`.

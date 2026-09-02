# Power panel — Design

Date: 2026-09-02. Status lives in bd (`sysc-134`).

Tranche 3C (`2026-08-31-power-design.md`) shipped the bar battery and deferred
a detail popout. M4 shipped session as a button list behind Super+X. This
design joins those: one attached panel that shows battery status, the
power-profiles-daemon profile, and the existing session actions.

Owner choices, 2026-09-02:

- Combine status, profiles, and session on one surface (option C).
- Reuse gamer-mode's `powerprofilesctl` parse/set and attached-panel chrome.
- Do not port gamer-mode's freeze/kill engine into first-party code.

Source: `/home/nomadx/noctalia-gamermode` (`Nomadcxx/noctalia-gamermode`).
Screenshot: `/home/nomadx/Pictures/gamer-monitor-panel.png`.

## Goal and scope

Right-click the bar battery to toggle the session panel. The same surface
opens from Super+X and from `panel.toggle {"panel":"session"}`.

In:

- Battery card from the existing `sysc-metrics` aggregate (glyph, percent,
  `KindMeter`, state, time remaining, watts).
- Power profile row from `powerprofilesctl list` / `set`, hidden when the
  binary is missing.
- Session actions already in `sessionArgv` (Lock / Log out / Suspend / Reboot /
  Power off). Lock still hidden without `session.locker`.

Out:

- Health, design capacity, per-device list, HID peripherals. The metrics pin
  does not supply them.
- Gamer-mode enable/disable: freeze/stop processes, user/system units, timers,
  Docker, `session.json`, light/heavy kill lists, `pkexec`, auto-performance.
  That stays a Milestone 6 plugin. This panel must not grow a kill-list.
- UPower D-Bus, godbus, brightness-on-scroll, profile OSD, a lock screen.

## Prior art

| | Gesture | Contents |
|---|---|---|
| Noctalia | Left battery → control-center Power tab | Status, profile segmented control, health, HID |
| DMS | Left → `BatteryPopout` (400px). Right cycles the profile | Status, Health/Capacity chips, profile group |
| Gamer-mode | Right-click as a bar gesture; attached panel ~420px | Metrics, `powerprofilesctl` select, plus the suspend engine |
| sysc-shell today | Battery has no action. Super+X opens session | Lock/logout/suspend/reboot/poweroff only |

The owner asked for **right-click** on the battery even though Noctalia and DMS
open their battery UI on **left**. Left-click on the battery stays inert in v1.

Take from gamer-mode (`gamer-mode/service.luau` ~1015–1078):

- `parsePowerProfiles` on `powerprofilesctl list` (`* balanced:` lines).
- `powerprofilesctl set <name>` only for a name that appeared in `list`.
- Hide the profile row when `LookPath("powerprofilesctl")` fails.

Do not take the rest of that plugin.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | One `PanelSession`. Public name stays `session` so Super+X and existing binds keep working. IPC `power` is an alias that resolves to the same ID. Surface ID remains `panel:session`. | A new `PanelPower` that forks IPC and leaves a second button-only tree. |
| D2 | Three cards, stacked: Battery, Power profile, Session. Card chrome is the existing sysmon `KindCapsule` (`monitorCard`). Skip Battery when the snapshot has no present battery. Skip Power profile when the tool is missing or `list` yields no names. Session is always present. | Separate panels. A single undifferentiated column. |
| D3 | Target width 420 logical (gamer-mode / DMS neighbourhood). Height from `ui.ContentHeight` plus two radii, the same intrinsic-height path sysmon already uses. | Keep `panelTargetSize` 280×200. A settings-sized modal. |
| D4 | `PanelSession` always `Align: "right"` (trailing output edge minus padding). Bar click and Super+X share that clamp. | Pixel-perfect pill tracking (needs a trigger X on `Trigger`). Leave session centred. |
| D5 | Right-click on the battery widget toggles the panel. Left-click on the battery does nothing. CPU/Memory keep left-click → sysmon. `Bar.Handle` records `event.Button` and passes it to `onAction`. Linux `BTN_LEFT` is `0x110`, `BTN_RIGHT` is `0x111` (already `buttonRight` in `tray.go`). | Left-click battery (the refs). Cycling the profile on right-click (DMS). |
| D6 | Profiles through `powerprofilesctl` argv after a successful `LookPath`. No godbus. Same rule as `loginctl` for session (M4 D11). `set` uses `CommandContext` 5s `Run`, then `list` again, then rebuild. Refuse a name that was not in the last `list`. | Direct D-Bus `net.hadess.PowerProfiles`. Fire-and-forget `Start`. |
| D7 | Profile choices are a `KindRow` of `KindButton` (`Role: "tab"`), one per listed name. The active name is `Bold`. `KindButton` already paints accent fill; this slice does not teach it a segmented track. Display labels: `power-saver` → Power saver, `balanced` → Balanced, `performance` → Performance; any other name is shown as listed. | `KindTab` (paints as text, not a control). A `KindMenu` dropdown. Porting gamer-mode's `ui.select`. |
| D8 | Session actions stay `sessionArgv` / `runSessionAction`. A successful action still closes the panel. | Re-derive `systemctl` poweroff. Confirm dialogs. |
| D9 | Battery card reads `services.Snapshot.Battery` only: glyph (`BatteryIconRune`), percent, `KindMeter` 0–1, state text, `batteryDuration` when `TimeValid`, watts when `RateValid`. | Health/capacity chips. UPower DisplayDevice over D-Bus. Bumping the metrics pin for fields it does not have. |
| D10 | Opening `PanelSession` acquires a `SourceBattery` lease so Super+X still fills the battery card when the bar has no battery widget. | Depend on the bar's lease. Skip the card unless a bar battery exists. |
| D11 | Gamer-mode's suspend engine stays a plugin. First-party code may cite `parsePowerProfiles`; it may not grow freeze lists, unit stops, Docker, or `pkexec`. | Fold gamer-mode into the shell. A "Gamer mode" toggle on this panel. |
| D12 | No lock screen. Lock is the configured locker argv, unchanged. | Implement a locker. Hide Lock forever. |

## Information architecture

Attached under the bar, 420 wide, height from content, trailing-edge aligned.

```
┌─ Battery ─────────────────────────────┐
│  [glyph]  84%                         │
│  ████████████░░░░  Discharging        │
│  2h14m remaining           4.2 W      │
└───────────────────────────────────────┘
┌─ Power profile ───────────────────────┐
│  [Power saver] [Balanced] [Performance]│
└───────────────────────────────────────┘
┌─ Session ─────────────────────────────┐
│  Lock                                 │
│  Log out                              │
│  Suspend                              │
│  Reboot                               │
│  Power off                            │
└───────────────────────────────────────┘
```

Desktop with no battery: first card omitted. Machine without
`powerprofilesctl`: second card omitted. Super+X on that desktop is session
buttons only, which is today's panel plus trailing-edge placement.

## Code seams

- Battery widget: `internal/shell/batterywidget.go` / `widget.go` case
  `"battery"`. Today it has no `Action`. Give the node
  `panelSessionAction = "panel:session"`.
- Bar click: `Bar.Handle` currently ignores `event.Button` for non-tray hits.
  `bindBarPanelActionsLocked` already toggles sysmon from `panelMonitorAction`.
  Extend that handler: left + `panel:system-monitor` → `PanelMonitor`; right +
  `panel:session` → `PanelSession`.
- Tree: grow `sessionTree` in `internal/shell/popout_session.go`. Reuse
  `monitorCard` / `monitorCardTitle` rather than a third card helper.
- Profiles: new `internal/shell/powerprofiles.go` with table tests. Port the
  line parser from gamer-mode; do not import Luau.
- Size: `panelTargetSize(PanelSession)` 420×(fallback 200); after build, same
  `monitorSurfaceHeight` call sysmon already makes.
- IPC: `knownPanels["power"]` and `parsePanelName("power")` → `PanelSession`.
- Hotkeys: `docs/niri-hotkeys.md` Super+X stays `session`. Mention that the
  surface now includes battery and profiles. Do not add a second bind.

## Follow-ups (not this slice)

- Pixel-align the panel to the battery pill (`Trigger` X).
- Segmented-track paint for the profile row.
- Health / capacity / HID once `sysc-metrics` exposes them.
- Left-click battery (if the owner later wants the refs' gesture).
- Profile OSD, brightness-on-scroll.
- Shipping gamer-mode as a Milestone 6 plugin.

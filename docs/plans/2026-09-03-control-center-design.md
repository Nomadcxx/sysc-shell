# Control centre — Design

Date: 2026-09-06. The filename keeps the commissioned `2026-09-03` stem from
`2026-09-03-control-center-execution-handover.md`. Status lives in bd
(`sysc-158`; implementation is `sysc-154`).

The control centre is the shell's access spine: one surface that reaches live
controls, settings, launcher, weather, monitoring, notifications and power.
It is **its own UI**, not a compacted restaging of the dedicated panels. It
shares their data sources and nothing else, and it does not replace them —
Settings, Launcher, Monitor, Session, Calendar, Notifications and Wallpaper all
remain first-party surfaces with their own entries.

Composition target: **Noctalia v5** for information architecture and overall
shape (`docs/plans/assets/2026-08-31-bar-visual-parity/refs/noctalia-control-center.png`,
source snapshot `/home/nomadx/noctalia/src/shell/control_center/`).
**DMS** for compact quick-setting tiles, icon wells and control density
(`/home/nomadx/Documents/GitHub/DankMaterialShell/Modules/ControlCenter/`;
the handover's `/usr/share/quickshell/dms/...` path no longer exists).
Both are behaviour and visual references. No C++, QML, configuration or plugin
API is imported.

Sources (read, not imported):

- `internal/shell/panel.go`, `panelhost.go`, `registry.go`
- `internal/shell/popout_settings.go` — the shipped rail/body composition
- `internal/shell/popout_session.go` — power profiles and off-owner command work
- `internal/shell/popout_notifications.go` — DND and unread state
- `internal/shell/popout_monitor.go` — machine facts, metric cards
- `internal/shell/animation.go` — the `sysc-141` surface animator
- `internal/services/` — audio, brightness, clock, metrics, weather, leases
- `internal/render/materialfont.go`, `icons/material/SOURCE.md`
- `internal/ipc/server.go`
- `internal/theme/profile.go` — density metrics, text roles, spacing scale

## Goal and scope

A first-party `PanelControlCenter`, public IPC name `control-center`, opened by
a bar trigger or by IPC, composed of a fixed icon rail and one scrollable body.

In:

- Panel identity, bar trigger, IPC entry with direct section addressing.
- Icon rail of ten destinations: seven functional (Home included), three disabled.
- Home, Audio, Monitor, Power, Weather, Calendar and Notifications pages built
  natively from live service data.
- Audio, brightness, DND and power-profile controls through one off-owner seam.
- Focus, scrolling, Escape, page transitions and reduced motion.

Out:

- NetworkManager (`sysc-157`), BlueZ (`sysc-155`) and MPRIS (`sysc-156`)
  backends. Their destinations appear disabled; no backend is written here.
- Any change to Settings, Launcher, Monitor, Session, Calendar, Notifications
  or Wallpaper trees. The control centre reads services, not their trees.
- A general split-pane or page framework, a settings rewrite, wallpaper
  hosting, desktop widgets, or a plugin page API.
- New UI node kinds. Composition uses shipped primitives only.

## Decisions

Numbered to the twelve the commission requires.

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | `PanelControlCenter`, 700×520 logical, bar-hugging, `Align` from the trigger like Monitor. No `CenterY` (that is launcher/wallpaper float behaviour). Existing `clampAxis` handles an output too small to fit; no new fitting path. Exclusive keyboard and the one-interactive-root rule inherited unchanged | A floating centred panel. Full-output height. A bespoke fitting routine |
| D2 | Rail is a fixed 56 px column, ten entries top-to-bottom: Home, Media, Audio, Monitor, Power, Network, Bluetooth, Weather, Calendar, Notifications. Active entry is an accent-filled rounded well; disabled entries (Media, Network, Bluetooth) render at reduced emphasis, are not focusable, carry no action, and keep their true rail position so the IA does not reflow when a backend lands | Hiding disabled destinations until their backend exists. Text labels in the rail. Reordering to put disabled entries last |
| D3 | Home is asymmetric: full-width identity card (name, `user@host`, uptime); left column with a disabled Media card and a clock/date/weather card; right column with a 2×2 quick-tile grid (audio mute, DND, power profile, battery); full-width volume and brightness sliders. Every value updates live from its service while Home is visible; identity facts are read once at open | Painting Caffeine/Night Light tiles with no backing state. A wallpaper-backed identity card in v1 (deferred: it puts an image decode on Home's build path for decoration) |
| D4 | Each functional page is composed natively for the body width from the same services the dedicated panels read. No dedicated panel tree is embedded, and no shared content builder is extracted | Calling `monitorTree`/`sessionTree`/`centerTreeFor` inside the body (built for wider panels, and couples the control centre to trees other sessions are editing). Refactoring shipped panels into width-parameterised builders mid-flight |
| D5 | One bar widget, default right section, Material ligature `tune`. It supplies `inner` **and** `format`, satisfying `Bar.applyLocked`. Left-click toggles the panel | A widget with neither seam — that is the blank-shell panic recorded in the 2026-09-06 handover. A launcher-style global keybind as the only entry |
| D6 | IPC gains an additive `section` param: `panel.open {"panel":"control-center","section":"audio"}`. Omitted opens Home. An unknown or disabled section returns an error and leaves the open panel unchanged. Because `PanelHost.section` already drives Settings, the same param works there for free | A compound `"control-center:audio"` name (puts grammar in the name field). A dedicated `control-center.section` method for one panel. Silently falling back to Home |
| D7 | One flat roving ring over `ui.Focusables(h.root)`, rail first in tree order. Because the rail's length never changes, the roving index is stable across page swaps, so preserving it leaves focus on the rail entry just activated. Body scrolls; pointer-routed `scrollAt` already picks the deepest scrollable. Escape: an open menu consumes it, otherwise `closePanelLocked` | A two-region rail/body focus model with its own traversal. An Escape step that returns to Home before closing (every other panel closes on Escape) |
| D8 | Acquire the union of leases at open (metrics CPU/memory/battery, clock, weather), release at close — the shipped `acquirePanelLeases` shape. The bar already holds most of these, so they are refcount bumps rather than new samplers | Per-section acquire/release (a new lease lifecycle on a path that runs under `Registry.mu`). A single snapshot at open, which leaves Home's CPU, memory and battery visibly stale |
| D9 | One helper, `scheduleControl(h, run)`, modelled on `scheduleLoadProfiles`: capture under `r.mu`, run with the lock released, re-take, verify `r.panelHosts[PanelControlCenter] == h`, then set/clear `errLabel`, rebuild and publish. Audio set/mute, brightness set, profile set and session actions all route through it. DND is pure memory (`r.setDND`) and stays synchronous | Calling `r.stepAudio`/`stepBrightness` from the action handler — those are lock-free only because IPC reaches them off the owner; a panel handler holds `Registry.mu`, so that would run `wpctl` under the lock |
| D10 | Only the active page is in the tree, so hidden pages leave hit testing and focus order by construction. The incoming page animates opacity and a small offset whose direction follows the rail index delta, on the existing `sysc-141` animator already wired at `panelhost.go:486`. `animator.Retarget()` handles a rail click mid-transition. Reduced motion (`cfg.Accessibility.ReducedMotion`) settles the page at once | Keeping the outgoing page mounted for a cross-fade (it would stay hit-testable and focusable). A second animator or a per-page ticker |
| D11 | Four states with one vocabulary shared with disabled rail entries. **Unavailable**: `Available()` false, no `Snapshot.Battery`, no `powerprofilesctl` — disabled control, short reason, no action. **Empty**: service present, nothing to show — centred empty state. **Stale**: leased but no sample yet — chrome with dashes, never zeros. **Failure**: existing `h.errLabel`, inline at the top of the affected page, cleared on next success. A control in flight shows the requested value and reconciles from the next sample; a failure restores the last known value | Rendering zeros while a sample is pending (0% CPU is a lie). A modal error. Swallowing a failed command |
| D12 | Automated: `go test ./internal/shell ./internal/ui ./internal/ipc ./internal/services` with `-p 4` and `GOMAXPROCS=4`, never `-race` repo-wide, and **`loginctl`/`systemctl` shadowed by stubs earlier on `PATH`**. Live Niri: bar trigger and both IPC forms, rail navigation, disabled destinations inert, real audio/brightness/DND/profile effect, Escape, focus order, reduced motion, and one explicit check that the bar trigger paints | A repo-wide `-race` build (this box is zram-only swap; it hard-locks). Running `./internal/shell` unshadowed — it really executes `loginctl terminate-session self` |

## Pixel contract (standard density, fontScale 1)

Values are `theme.Metrics` tokens at `DensityStandard`, not literals to copy.

| Region | Size | Token |
|---|---|---|
| Panel | 700 × 520 | design constant |
| Panel inset | 16 | `PanelPadding` |
| Rail | 56 wide | design constant |
| Rail item | 40 square, centred | `StandardControl` |
| Rail ↔ body gap | 16 | `SpacingScale[4]` |
| Body | 596 wide | `700 − 2×16 − 56 − 16` |
| Header | 40 tall | `StandardControl` |
| Body content | 436 tall | `520 − 2×16 − 40 − 12` |
| Card inset | 12 | `CardPadding` |
| Home left column | 356 | body − right − 12 |
| Home right column | 228 | two tiles + gap |
| Quick tile | 110 × 88 | `(228 − 8) / 2` |
| Icon well | 24 | `IconLarge` |

Type uses semantic roles only: `RoleTitle` for the header and identity name,
`RoleLabel` for tile and card labels, `RoleCaption` for secondary lines,
`RoleBody` elsewhere. Colour, radius, opacity, elevation and motion come from
the shipped theme. No new colours, timing constants or per-control tickers.

Borders use `render.RingMask` (added `5da46b3`). A border is never built as the
difference of two fills.

## Information architecture

```
PanelControlCenter (700×520, bar-hugging)
├── rail (56, KindColumn, section: actions)
│   Home · Media° · Audio · Monitor · Power
│   Network° · Bluetooth° · Weather · Calendar · Notifications
│                                            ° disabled
└── body (596, scrollable)
    ├── header: active section title · settings · power · close
    └── active page (one only)
```

| Page | Content | Source |
|---|---|---|
| Home | identity, media (disabled), clock/weather, 2×2 tiles, sliders | mixed |
| Audio | volume slider, mute | `services.Audio` |
| Monitor | CPU, memory, battery, network cards | `services.Snapshot` |
| Power | battery detail, profile segmented control, session actions | `Snapshot.Battery`, `powerprofilesctl`, `loginctl` |
| Weather | current reading | `services.Weather` |
| Calendar | month grid, `monthDelta` paging | clock |
| Notifications | history, DND controls, clear | `notifyState` |

The Audio page has **no output-device picker**: `AudioState` carries `Level`
and `Muted` only. Nothing is drawn that the service cannot report.

Header actions open the existing Settings, Power and close controls. Opening a
dedicated panel replaces the control centre through the current root owner.

## Commands

| Entry | Shape | Behaviour |
|---|---|---|
| IPC | `panel.open\|toggle\|close {"panel":"control-center","section":"..."}` | `section` optional; unknown or disabled section errors without changing the open panel |
| Rail | `section:<name>` action | reuses the shipped `PanelHost.section` seam |
| Tiles | `cc:mute`, `cc:dnd`, `cc:profile:<name>` | through `scheduleControl`, except DND |
| Sliders | `cc:volume`, `cc:brightness` | through `scheduleControl` |
| Header | `panel:settings`, `panel:session`, `close` | existing panel open path |

`knownPanels` gains `"control-center"`. `parsePanelName` gains the matching
case. Registering the name and the `PanelID` is one change, asserted by test the
way `wallpaper_panel_test.go` asserts the wallpaper panel.

## Icon inventory

`internal/render/materialfont.go` carries a hand-kept subset asserted against
`ICONS` in `icons/material/build.py`; a name the font lacks shapes to nothing
and paints an invisible control. Already present and reused: `volume_up`,
`volume_off`, `brightness_high`, `do_not_disturb_on`, `notifications`,
`settings`, `close`, `power_settings_new`, `speed`, `balance`,
`energy_savings_leaf`.

Eight ligatures must be added to both `build.py` and `materialIcons`, the font
rebuilt, and the larger `.ttf` committed:

`tune` (bar trigger), `home`, `music_note` (Media), `desktop_windows`
(Monitor), `wifi` (Network), `bluetooth`, `cloud`
(Weather), `calendar_month` (Calendar).

Licence and provenance are already recorded in
`internal/render/icons/material/SOURCE.md` (upstream commit
`84ccef280841abfac506afc4ad4a2782f6d0a1d0`, Apache-2.0 copied verbatim to
`internal/render/icons/material/LICENSE`). Adding icons does not change the
licence position; it changes the pinned rebuild output, which `SOURCE.md`
already requires to be byte-reproducible.

## Failure behaviour

- A service that never becomes available renders its control unavailable, not
  absent, so the layout does not shift when it appears.
- A command failure sets `errLabel` and restores the last known control value.
- A panic in a widget seam presents as a blank shell, not a crash: `sysc-wayland`
  v0.2.1 recovers handler panics into `dispatch: panic handling opcode=N`, and
  `wl_output.done` (opcode 2) runs `NewHost` → `buildBar` → `bar.apply`. The bar
  trigger therefore has an explicit test that it satisfies `Bar.applyLocked`.
- Closing the panel releases every lease (`releaseAll(h.leases)`) and forgets
  animator state for its nodes.

## Gate

Automated, per D12, with `loginctl` and `systemctl` shadowed:

```
PATH=<stubs>:$PATH GOMAXPROCS=4 go test -p 4 \
  ./internal/shell ./internal/ui ./internal/ipc ./internal/services
```

Live Niri, on a real compositor:

1. Bar trigger opens the panel on the focused output, hugging the bar.
2. `panel.open` with no `section` opens Home; with `section` opens that page;
   with an unknown or disabled section returns an error and changes nothing.
3. Rail navigation moves between all seven functional pages.
4. Media, Network and Bluetooth are visible, inert and not focusable.
5. Volume, brightness, DND and power profile change real system state.
6. Escape closes; Tab walks rail → body; reduced motion settles at once.
7. The shell reports `active (running)` **and** paints — no blank shell.

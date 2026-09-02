# Control Centre Design and Planning Handover

Date: 2026-09-03.

## Assignment

Produce two documents for the next Milestone 7 tranche:

1. `docs/plans/2026-09-03-control-center-design.md`
2. `docs/plans/2026-09-03-control-center.md`

Use `superpowers:brainstorming` for the design and obtain owner approval before
writing the plan. Use `superpowers:writing-plans` for the implementation plan.
The owner has waived TDD for this work: each task implements a coherent slice,
adds one focused runnable check for non-trivial logic, runs the affected
packages, and commits the slice.

Commit each document to `main` as a docs-only commit and register it in
`docs/plans/README.md`. Do not write product code during this assignment. Do
not start, stop, signal, or replace a running `sysc-shell`; other sessions are
using the live compositor.

Track the paperwork in `sysc-158`. `sysc-154` tracks later implementation and
depends on the paperwork, chrome/theme parity, and notification-centre work.
Run `bd` only from `/home/nomadx/sysc-shell`.

## Owner direction

The control centre is the shell's access spine. It should let a user reach
live controls, settings, launcher, weather, monitoring, notifications, power,
and later system integrations from one place.

Use Noctalia v5 for the information architecture and overall composition. Use
DMS for the compact quick-setting tiles, icon wells, and control density. Keep
the native Go retained renderer and extend existing primitives when a shipped
control-centre element proves the need.

Network, Bluetooth, and Media must appear as disabled destinations in the
first release. Give each a clear unavailable treatment and no action. The
tracker owns their later implementation:

- `sysc-157`: NetworkManager-backed Network service and page
- `sysc-155`: BlueZ-backed Bluetooth service and page
- `sysc-156`: MPRIS-backed Media service and page

Do not implement those three backends in the control-centre tranche. Do not
omit their destinations or describe them only in prose.

## Visual references

- DMS control centre image:
  `https://danklinux.com/img/homepage/controlcenter_dark.png`
- Saved Noctalia control centre:
  `docs/plans/assets/2026-08-31-bar-visual-parity/refs/noctalia-control-center.png`
- Existing comparison:
  `docs/plans/2026-08-30-panels-and-controls-prior-art.md`
- Noctalia source snapshot:
  `/home/nomadx/noctalia/src/shell/control_center/`
- DMS installed source:
  `/usr/share/quickshell/dms/Modules/ControlCenter/`

Treat both projects as behavior and visual references. Do not import their C++,
QML, configuration, or plugin APIs.

The saved Noctalia image establishes the preferred shape: a narrow icon rail,
active-section header, layered cards, an asymmetric Home page, and direct
domain navigation. The DMS image contributes a two-column quick-control grid,
large semantic icon wells, short status labels, and a compact user/action
header. The shell theme supplies the colours, type, opacity, radius, elevation,
and motion.

## Read before designing

Read these in order:

1. `AGENTS.md`
2. `docs/roadmap.md` Milestone 7
3. `docs/plans/2026-08-26-sysc-shell-design.md`
4. `docs/plans/2026-08-30-panel-foundation-design.md`
5. `docs/plans/2026-08-30-panels-and-controls-prior-art.md`
6. `docs/plans/2026-09-02-chrome-catalogue-design.md`
7. `docs/plans/2026-09-02-chrome-catalogue.md`
8. `docs/plans/2026-09-02-theme-system-parity-design.md`
9. `docs/plans/2026-09-02-theme-system-parity.md`
10. `docs/plans/2026-09-03-notification-centre-design.md`
11. `docs/plans/2026-09-03-notification-centre.md`
12. `docs/plans/2026-09-01-launcher-design.md`
13. `docs/plans/2026-09-03-wallpaper-design.md`

Inspect these implementation owners:

- `internal/shell/panel.go`
- `internal/shell/panelhost.go`
- `internal/shell/registry.go`
- `internal/shell/popout_monitor.go`
- `internal/shell/popout_session.go`
- `internal/shell/popout_clock.go`
- `internal/shell/popout_notifications.go`
- `internal/shell/popout_settings.go`
- `internal/shell/popout_launcher.go`
- `internal/shell/widget.go`
- `internal/ui/tree.go`
- `internal/ui/layout.go`
- `internal/ui/column.go`
- `internal/ui/focus.go`
- `internal/services/audio.go`
- `internal/services/brightness.go`
- `internal/services/weather.go`
- `internal/ipc/server.go`
- `internal/config/config.go`

The chrome implementation is active in
`/home/nomadx/sysc-shell/.worktrees/feature/chrome-catalogue-implementation`.
Another agent will execute theme parity after chrome lands. Reconcile the
design and plan against their final APIs before naming edits. Do not modify
their worktree.

## Working design defaults

Challenge these during brainstorming, but keep them when no stronger result
emerges:

- Add one first-party `PanelControlCenter`, public name `control-center`.
- Use a bar-hugging panel near Noctalia's 700 by 520 logical size. Preserve the
  project's Exclusive keyboard policy and one interactive-root rule.
- Compose a fixed-width icon rail and one scrollable content body from existing
  rows, columns, buttons, cards, and scroll nodes. Add no general split-pane or
  page framework unless the first real layout cannot work without it.
- Ship Home, Audio, Monitor, Power, Weather, Calendar, and Notifications as
  functional destinations. Show Media, Network, and Bluetooth as disabled.
- Let header actions open the existing Settings, Launcher, and Session panels.
  Opening one replaces the control centre through the current root owner.
- Build Home from current data: user/host/uptime, audio, brightness, DND,
  battery/profile, weather, metrics, and notification state. Do not paint fake
  device names, album metadata, or connectivity state.
- Reuse existing domain content. Extract a pure content builder only when the
  control centre becomes its second consumer. Keep dedicated panels available.
- Support direct section entry through IPC and internal actions. Reject unknown
  sections without changing the open panel.
- Use the surface animator from `sysc-141` for rail selection and page changes.
  Reduced motion settles the page at once. Hidden pages must leave hit testing
  and focus order.
- Extend the pinned Material Symbols subset only for the approved bar, rail,
  header, and disabled-destination icons. Record the exact ligature inventory
  and licence path in the design.

## Decisions the design must lock

Number each decision and obtain owner approval:

1. Exact panel geometry, bar attachment, compact behavior, and output fitting.
2. Rail order, labels/tooltips, active treatment, and disabled treatment.
3. Home card hierarchy and which values update while Home is visible.
4. Functional page boundaries and how each reuses a dedicated panel tree.
5. Bar trigger, default placement, and Material icon.
6. IPC shape for direct section entry and behavior for unknown sections.
7. Focus movement between rail and body, scrolling, Escape, and focus restore.
8. Service leases while Home or a domain page is active.
9. Audio, brightness, DND, and power action ownership. No command or filesystem
   I/O may run while `Registry.mu` or the Wayland owner is blocked.
10. Page transition direction, retargeting, and reduced-motion behavior.
11. Empty, stale, unavailable, and command-failure presentation.
12. Automated and live Niri acceptance gates.

## Project constraints

- Go and the native retained renderer remain mandatory.
- Niri comes first. Other compositors need another approved design.
- Keep Wayland types under `internal/platform/wayland` and Niri wire types under
  `internal/platform/niri`.
- Keep one Wayland dispatch goroutine. Relays and bounded external work stay
  off that owner.
- Draw after invalidation and respect frame callbacks and buffer release.
- Panel `configure`, `render`, and `handle` paths take `Registry.mu`.
  `rebuildPanel` already holds the lock and calls the unlocked form.
- Use the semantic theme and surface animator produced by `sysc-141` and
  `sysc-142`. Do not add colours, timing constants, a second theme engine, or a
  per-control ticker.
- Preserve Settings, Launcher, Monitor, Session, Calendar, and Notifications as
  first-party surfaces. The control centre gives them another entry and may
  reuse their content; it does not delete them.
- Add no C++, Rust, Lua, Luau, Qt, QML, Quickshell, CGO, compositor, lock screen,
  runtime SVG loader, or plugin compatibility layer.
- Add a UI primitive only when an approved control-centre component consumes
  it. Prefer composition and existing dependencies.
- Use beads for status and discovered work. Do not add markdown task lists to
  the design or plan.

## Plan shape

The implementation plan should use implementation-first slices and small
commits. A likely sequence is:

1. Reconcile the landed chrome, theme, and notification APIs.
2. Add the panel identity, public IPC entry, bar trigger, and finite icon set.
3. Build the rail/body composition and section model.
4. Build Home from current immutable snapshots.
5. Wire audio, brightness, DND, and power controls through bounded off-owner
   work.
6. Reuse Monitor, Power, Weather, Calendar, and Notifications content.
7. Complete focus, scrolling, direct-section entry, disabled destinations, and
   transitions.
8. Run focused package checks, full repository gates, and the live Niri matrix.

The final plan must name exact files, commands, expected results, commit
boundaries, dependency gates, and a stop condition. It must not pull
NetworkManager, BlueZ, MPRIS, a settings rewrite, wallpaper hosting, desktop
widgets, or a plugin page API into this tranche.

## Acceptance target

The paperwork finishes when the owner approves a design that resolves the
twelve decisions, the implementation plan can be executed without inventing
contracts, both documents and their register rows are committed on `main`, and
`sysc-158` is closed. Product implementation stays blocked until `bd` shows
`sysc-154` ready.

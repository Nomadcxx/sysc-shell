# sysc-shell

`sysc-shell` is a Go-first native Wayland desktop shell for Niri: one bar per output, panels, OSDs,
notifications, a system tray, a launcher, and a plugin host. It does not depend on Qt, QML,
Quickshell, C++, Rust, Lua, or Luau.

Work is tracked in `bd`. Designs and plans are registered in
[`docs/plans/README.md`](docs/plans/README.md). The milestone sequence is [`docs/roadmap.md`](docs/roadmap.md).

## Consumed modules

Pins are in `go.mod`. This process does not vendor those trees; it imports the tagged modules.

| Module | Pin | Role |
|---|---|---|
| [`sysc-wayland`](https://github.com/Nomadcxx/sysc-wayland) | `v0.2.1` | Pure-Go Wayland client and protocol generator. |
| [`sysc-metrics`](https://github.com/Nomadcxx/sysc-metrics) | `v0.2.0` | Linux telemetry for built-in monitoring widgets. Tag lives on `milestone/power-collectors`, not that repo's `main`. |
| [`sysc-notify`](https://github.com/Nomadcxx/sysc-notify) | `v0.1.0-rc.2` | Freedesktop Notifications daemon. Separate process; this shell dials `$XDG_RUNTIME_DIR/sysc-notify/presenter.v1.sock`. Binary is `cmd/sysc-notify` on the tag (`origin/redesign/v0.1`). That repo's `main` is still docs-only. |
| [`sysc-tray`](https://github.com/Nomadcxx/sysc-tray) | `v0.1.0-rc.1` | StatusNotifierItem and DBusMenu daemon. Separate process; this shell dials `$XDG_RUNTIME_DIR/sysc-tray/presenter.v1.sock`. Binary is `cmd/sysc-tray` on the tag. Same `main` gap as notify. |
| [`sysc-launch`](https://github.com/Nomadcxx/sysc-launch) | `v0.1.0` | Desktop-entry scan, fzf ranking, usage history, and Niri spawn. **Library plus a one-shot CLI** (`query` / `launch`), not a daemon. This shell constructs `launcher.NewService` in-process. Ranking history stays at `$XDG_STATE_HOME/sysc-shell/launcher/history.gob` so it does not merge with the module default. Clone: `/home/nomadx/sysc-launch`. |

`replace` directives are forbidden. `git diff --exit-code -- go.mod go.sum` is part of the commit gate.

Local clones of notify and tray at `/home/nomadx/sysc-notify` and `/home/nomadx/sysc-tray` follow
those repos' default branch. Checking them out on `main` is not what this module compiles.

## Scope

Shipped: a bar on each output; clock, workspace, title, CPU, memory, filesystem, block, network,
battery, and weather widgets; panels (clock, system-monitor, session, settings, launcher); OSD;
notification and tray presentation against the protocol packages; theme generation and a template
catalog.

Excluded: a lock screen, a compositor, Noctalia or DMS configuration/plugin/QML compatibility, and
compositors other than Niri.

## Technology direction

- Go owns shell state, Niri IPC, layout, widgets, services, configuration, and plugin supervision.
- `wl_shm` is the renderer. EGL/OpenGL ES enters only after profiling shows a named failing case.
- [`go-text/typesetting`](https://github.com/go-text/typesetting) is the text stack.

## Layout

```text
cmd/sysc-shell/                executable
internal/platform/wayland/     Wayland connection, protocols, outputs, seats, scaling, surfaces
internal/platform/niri/        Niri socket protocol and state projection
internal/render/               buffers, rasterisation, damage, frame scheduling
internal/ui/                   retained nodes, measurement, layout, hit testing
internal/shell/                output hosts, bars, panels, tray, toasts, launcher projection
internal/services/             clock, weather, metrics
internal/notifyclient/         sysc-notify presenter client
internal/trayclient/           sysc-tray presenter client
internal/icons/                theme-icon resolve and decode
internal/theme/                Material 3 generation (matugen) and fallback
internal/theming/              template catalog and apply/unapply
internal/settings/             settings registry
internal/config/               JSON configuration
internal/ipc/                  local IPC
tests/integration/             Niri and Wayland integration checks
docs/                          architecture, roadmap, designs and plans
```

## Documentation

- [Approved architecture](docs/plans/2026-08-26-sysc-shell-design.md)
- [Roadmap](docs/roadmap.md)
- [Design and plan register](docs/plans/README.md)
- [Niri hotkeys](docs/niri-hotkeys.md)

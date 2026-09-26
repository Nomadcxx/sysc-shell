# sysc-shell

`sysc-shell` is a Go-first native Wayland desktop shell for Niri: one bar per output, panels, OSDs,
notifications, a system tray, a launcher, and a plugin host. It does not depend on Qt, QML,
Quickshell, C++, Rust, Lua, or Luau.

Work is tracked in `bd`. Designs and plans are registered in
[`docs/plans/README.md`](docs/plans/README.md). The milestone sequence is [`docs/roadmap.md`](docs/roadmap.md).
Niri setup for the frosted bar and blurred panels is in [`docs/niri-blur.md`](docs/niri-blur.md).

## Consumed modules

Pins are in `go.mod`. This process does not vendor those trees; it imports the tagged modules.

| Module | Pin | Role |
|---|---|---|
| [`sysc-wayland`](https://github.com/Nomadcxx/sysc-wayland) | `v0.2.2` | Pure-Go Wayland client and protocol generator. |
| [`sysc-metrics`](https://github.com/Nomadcxx/sysc-metrics) | `v0.6.1` | Linux telemetry for built-in monitoring widgets and the on-demand process view, including validated NVIDIA GPU VRAM parsing. |
| [`sysc-notify`](https://github.com/Nomadcxx/sysc-notify) | `v0.1.0-rc.4` | Freedesktop Notifications daemon. Separate process; this shell dials `$XDG_RUNTIME_DIR/sysc-notify/presenter.v1.sock`. Binary is `cmd/sysc-notify` on the tag (`redesign/v0.1`). That repo's `main` is still docs-only. |
| [`sysc-tray`](https://github.com/Nomadcxx/sysc-tray) | `v0.1.0-rc.1` | StatusNotifierItem and DBusMenu daemon. Separate process; this shell dials `$XDG_RUNTIME_DIR/sysc-tray/presenter.v1.sock`. Binary is `cmd/sysc-tray` on the tag. Same `main` gap as notify. |
| [`sysc-launch`](https://github.com/Nomadcxx/sysc-launch) | `v0.1.0` | Desktop-entry scan, fzf ranking, usage history, and Niri spawn. **Library plus a one-shot CLI** (`query` / `launch`), not a daemon. This shell constructs `launcher.NewService` in-process. Ranking history stays at `$XDG_STATE_HOME/sysc-shell/launcher/history.gob` so it does not merge with the module default. Clone: `/home/nomadx/sysc-launch`. |

`replace` directives are forbidden. `git diff --exit-code -- go.mod go.sum` is part of the commit gate.

Local clones of notify and tray at `/home/nomadx/sysc-notify` and `/home/nomadx/sysc-tray` follow
those repos' default branch. Checking them out on `main` is not what this module compiles.

## Intel i915 GPU usage

Intel i915 reports utilization through PMU engine-busy counters. A shell build that displays Intel
usage must pin `sysc-metrics` to `v0.5.1` or newer and let its metrics service own one stateful
`GPUSampler`. `ReadGPU` supplies Intel identity and temperature, but it cannot produce Intel
utilization. The sampler needs two samples: the first establishes a baseline and the second can
report usage. A valid `0%` means the GPU is idle. Without permission, identity and temperature
remain available while usage stays unavailable.

On the tested Intel laptop, opening the system-wide i915 counters requires `CAP_PERFMON` on the
shell process. `CAP_SYS_ADMIN` also satisfies the kernel gate but grants broader privilege. Lowering
`kernel.perf_event_paranoid` alone did not enable these counters. Apply the file capability again
after every replacement of the installed binary:

```sh
sudo setcap cap_perfmon+ep /home/nomadx/.local/bin/sysc-shell
getcap /home/nomadx/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service
systemctl --user is-active sysc-shell.service
```

The expected `getcap` result contains `cap_perfmon=ep`. The implementation covers the Linux
`i915` driver. The newer `xe` driver needs a separate driver-specific qualification.

## Existing bar configurations

The built-in default groups CPU, memory, temperature, and GPU as radial gauges. Existing JSON
configs keep their stored display mode, so an older config can show the legacy text or meter
widgets even while GPU telemetry works. Set `"display": "radial"` on each metric item that should
use the grouped gauges:

```json
[
  {"id":"cpu","display":"radial"},
  {"id":"memory","display":"radial"},
  {"id":"temperature","display":"radial"},
  {"id":"gpu","display":"radial"}
]
```

Back up `~/.config/sysc-shell/config.json` before editing it, then restart the user service.

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
plugins/reference/             in-tree reference plugin (weather)
tests/integration/             Niri and Wayland integration checks
docs/                          architecture, roadmap, designs and plans
```

## Official plugins

The official plugins (screen recorder, notes, timer, world clock, calendar,
GitHub notifications, mini docker, wallpaper depth, cat) live in the companion
repository [`sysc-plugins`](https://github.com/Nomadcxx/sysc-plugins). Build and
install them from there (`make install` symlinks each plugin directory into
`$XDG_CONFIG_HOME/sysc-shell/plugins`); the shell discovers them at startup and
manages enable/disable, settings, and state through the plugin host.

## Documentation

- [Approved architecture](docs/plans/2026-08-26-sysc-shell-design.md)
- [Roadmap](docs/roadmap.md)
- [Design and plan register](docs/plans/README.md)
- [Niri hotkeys](docs/niri-hotkeys.md)

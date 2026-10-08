# Development

Deploying to a machine goes through `scripts/deploy`; see `AGENTS.md`.

## Sibling modules

The shell imports these as tagged Go modules. The pins are in `go.mod`; nothing is vendored.
`replace` directives are forbidden, and `git diff --exit-code -- go.mod go.sum` is part of the
commit gate.

| Module | Role |
|---|---|
| [sysc-wayland](https://github.com/Nomadcxx/sysc-wayland) | Pure-Go Wayland client and protocol generator |
| [sysc-metrics](https://github.com/Nomadcxx/sysc-metrics) | Linux telemetry for the monitoring widgets and the process view |
| [sysc-launch](https://github.com/Nomadcxx/sysc-launch) | Desktop-entry scan, ranking and usage history. A library the shell runs in-process; its ranking history lives at `$XDG_STATE_HOME/sysc-shell/launcher/history.gob` |
| [sysc-notify](https://github.com/Nomadcxx/sysc-notify) | Notification daemon. Separate process; the shell dials `$XDG_RUNTIME_DIR/sysc-notify/presenter.v1.sock` |
| [sysc-tray](https://github.com/Nomadcxx/sysc-tray) | StatusNotifierItem and DBusMenu daemon. Separate process; the shell dials `$XDG_RUNTIME_DIR/sysc-tray/presenter.v1.sock`. The code is on the `redesign/v0.1` branch and its tags; `main` holds only docs |
| [sysc-clipboard](https://github.com/Nomadcxx/sysc-clipboard) | Clipboard history daemon. Separate process; the shell uses its `client` package |
| [oksvg](https://github.com/srwiley/oksvg) + [rasterx](https://github.com/srwiley/rasterx) | Pure-Go SVG rasterisation for theme icons. Upstream tags no releases, so the pins are pseudo-versions |

## Technology direction

- Go owns shell state, Niri IPC, layout, widgets, services, configuration, and plugin supervision.
- `wl_shm` is the renderer. EGL/OpenGL ES enters only after profiling shows a named failing case.
- [`go-text/typesetting`](https://github.com/go-text/typesetting) is the text stack.

## Layout

```text
cmd/sysc-shell/                executable
cmd/sysc-panel-preview/        paints a plugin panel tree (JSON) to a PNG with the host's own layout and painter
internal/platform/wayland/     Wayland connection, protocols, outputs, seats, scaling, surfaces
internal/platform/niri/        Niri socket protocol and state projection
internal/render/               buffers, rasterisation, damage, frame scheduling
internal/ui/                   retained nodes, measurement, layout, hit testing
internal/shell/                output hosts, bars, panels, tray, toasts, launcher projection
internal/services/             clock, weather, metrics
internal/wallpaper/            wallpaper backends (awww, swaybg, gSlapper)
internal/calc/                 launcher calculator
internal/emoji/                launcher emoji table
internal/notifyclient/         sysc-notify presenter client
internal/trayclient/           sysc-tray presenter client
internal/icons/                theme-icon resolve and decode
internal/theme/                Material 3 generation (matugen) and fallback
internal/theming/              template catalog and apply/unapply
internal/settings/             settings registry
internal/config/               JSON configuration
internal/ipc/                  local IPC
internal/plugin/               plugin discovery, supervision and store
plugin/v1/                     public plugin wire protocol
plugins/reference/             in-tree reference plugin (weather)
tests/integration/             Niri and Wayland integration checks
docs/                          architecture, roadmap, designs and plans
```

## Panel previews

`cmd/sysc-panel-preview` renders a plugin panel's wire tree to a PNG through the
host's own converter, layout and painter, for catalog screenshots and
documentation. It exists as a command, not an importable package, because the
default theme lives in `internal/shell` and a public package importing it would
drag the shell's whole dependency graph into every consumer.

```sh
go install ./cmd/sysc-panel-preview
sysc-panel-preview -width 360 -height 480 -o panel.png tree.json
```

`-scale` is in 120ths (120 is 100%; the default 180 is 150%). The output is an
opaque panel with transparent rounded corners on the default dark theme, with
no animation, hover or focus state. A tree the host would refuse fails with the
host's message and writes no file. Image nodes are decoded from their paths as
the host does, and an image that cannot be decoded is an error rather than an
empty box. The painted area is capped at 32 million pixels. Text uses the fonts
installed on the machine, so output can differ between machines.

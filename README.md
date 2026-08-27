# sysc-shell

`sysc-shell` is a Go-first native Wayland desktop shell for Niri. The project starts with a small shell-owned UI runtime, a bar on each output, and a versioned external-process widget protocol. It does not depend on Qt, QML, Quickshell, C++, Rust, Lua, or Luau.

The project is in the design and architectural-proof stage. No production shell exists yet.

The plans were independently audited on 2026-08-27. The verdict is *ready after listed fixes*; the corrections are applied in the owning documents and the remaining owner decisions are listed in the [plan-audit report](docs/plans/2026-08-27-plan-audit-report.md).

## Scope

The first useful release will provide:

- one layer-shell bar on every active Niri output;
- correct output hotplug, scale, configure, input, damage, and buffer handling;
- built-in clock, workspace, weather, CPU, memory, disk, and network widgets;
- host-owned visual components with consistent styling;
- supervised external plugins over a versioned IPC protocol.

The first releases exclude a lock screen, a compositor, Noctalia configuration compatibility, QML compatibility, and support for compositors other than Niri.

## Technology direction

- Go owns shell state, Niri IPC, layout, widgets, services, configuration, and plugin supervision.
- [`sysc-wayland`](https://github.com/Nomadcxx/sysc-wayland) supplies the owned pure-Go Wayland client
  and protocol generator. `sysc-shell` starts implementation only after `sysc-wayland v0.1.0` passes its
  release gate.
- `wl_shm` supplies the first renderer. EGL/OpenGL ES enters the project only after profiling shows a need.
- [`go-text/typesetting`](https://github.com/go-text/typesetting) is the first text-shaping candidate. HarfBuzz and FreeType remain a fallback if the pure-Go stack fails production text tests.
- [`dgop`](https://github.com/AvengeMedia/dgop) supplies system metrics for built-in monitoring widgets.

## Documentation

- [Approved architecture](docs/plans/2026-08-26-sysc-shell-design.md)
- [Roadmap](docs/roadmap.md)
- [Prior-art assessment](docs/prior-art.md)
- [Architectural-proof implementation plan](docs/plans/2026-08-26-architectural-proof.md)
- [Development orchestration](docs/plans/2026-08-26-development-orchestration.md)
- [Independent plan-audit handover](docs/plans/2026-08-27-plan-audit-handover.md)
- [Plan-audit report](docs/plans/2026-08-27-plan-audit-report.md)

## Planned repository layout

```text
cmd/sysc-shell/                executable entry point
internal/platform/wayland/     Wayland connection, protocols, outputs, seats, scaling, surfaces
internal/platform/niri/        Niri socket protocol and state projection
internal/render/               buffers, rasterisation, damage, frame scheduling
internal/ui/                   retained nodes, measurement, layout, hit testing
internal/shell/                output hosts, bars, panels, shell lifecycle
internal/services/             clock, weather, system metrics and later integrations
internal/plugins/              plugin manifests, supervision and IPC
protocols/                     pinned xdg-shell, layer-shell, fractional-scale and viewporter XML
assets/fonts/                  fonts only when system discovery cannot cover a requirement
tests/integration/             Niri and Wayland integration checks
docs/                          architecture, roadmap and implementation plans
```

The implementation plan creates files inside these directories as each owner gains real behavior. Empty package scaffolding will not be committed.

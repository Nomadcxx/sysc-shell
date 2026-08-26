# Prior-art Assessment

Date: 2026-08-26

## Summary

No inspected project supplies a complete Go-native desktop shell runtime. Several projects remove large parts of the work:

- DMS supplies reusable Go system integrations and a working pure-Go Wayland client path.
- `sysc-lock` proves layer-shell discovery and per-output surface creation on Niri.
- gSlapper provides the strongest local example of long-running native Wayland surface and frame lifecycle.
- Noctalia v5 provides the feature, ownership, scene-tree, and service reference.
- Niri replaces the compositor role that Umbriel fills for Noctalia.

`sysc-shell` should combine those lessons in one Go repository. It should not port any project line by line.

## Noctalia v5 beta

Source: <https://github.com/noctalia-dev/noctalia>

Observed architecture:

- one native C++23 process;
- direct Wayland and EGL/OpenGL ES;
- retained scene graph, layout, hit testing, and controls;
- custom poll loop across Wayland, D-Bus, PipeWire, IPC, network, files, and rendering;
- shell-owned bars, panels, launcher, notifications, wallpaper, OSDs, plugins, configuration, and services;
- Luau plugin runtime;
- roughly 286,000 C++ and header lines in the inspected snapshot.

Use:

- feature inventory and interaction behavior;
- output-host and separate-surface ownership;
- startup and shutdown ordering;
- scene invalidation, hit testing, and render scheduling concepts;
- configuration validation and prior-good-state behavior;
- service boundaries and capability degradation.

Do not use:

- C++ implementation;
- Luau runtime;
- lock-screen/session-lock work;
- compositor portability code during the Niri-only stages;
- source or configuration compatibility as a requirement.

## DankMaterialShell

Source: <https://github.com/AvengeMedia/DankMaterialShell>
Inspected commit: `ea0b158eb8c7d368ae7083218dbfc792faf019b2` from 2026-08-25.

Current DMS separates a large Go backend from its Quickshell/QML frontend. The inspected Go backend contains about 157,000 lines. Quickshell still owns the bar, panels, scene composition, rendering, input, animation, and dynamic QML widgets.

Relevant findings:

- DMS creates one `PanelWindow` per selected screen through Quickshell `Variants`.
- `DankBarWindow.qml` assigns the screen, anchors, layer, namespace, size, exclusive zone, and input mask.
- `NiriService.qml` connects to `$NIRI_SOCKET`, requests `EventStream`, and projects workspace, window, output, layout, overview, and cast events.
- DMS imports `dgop` as a Go library inside its IPC backend.
- DMS's screenshot selector and color picker use pure-Go Wayland, layer-shell, seats, `wl_shm`, viewporter, output scaling, buffer release, damage, and frame callbacks.
- Noctalia v5 uses `fractional-scale-v1` with `viewporter` and treats the compositor's preferred scale in 120ths as the surface scale.
- DMS plugins remain dynamic QML. Their useful traits are manifests, source precedence, per-output instances, startup checks, namespaced state, and component injection.
- DMS weather runs Open-Meteo and geocoding calls from QML/curl. The behavior is reusable; the implementation is not.

Use directly or adapt:

- pin `dankgo` and generate local protocol bindings;
- pin `dgop` for monitoring collectors;
- adapt direct Niri event-stream handling;
- adapt output and buffer lifecycle patterns from native screenshot/color-picker code;
- copy behavior and visual requirements from built-in widgets.

Skip:

- Quickshell/QML frontend code;
- dynamic QML plugin loading;
- multi-compositor branches;
- DMS's full backend as one imported service.

## dankgo

Source: <https://github.com/AvengeMedia/dankgo>
Inspected commit: `10434658325c` from 2026-08-23.

`dankgo` provides:

- a pure-Go Wayland wire client;
- generated core Wayland and xdg-shell bindings;
- a Go Wayland protocol scanner;
- Unix-socket IPC, paths, D-Bus helpers, and process-supervision utilities.

The focused `go test ./wayland/...` check passed on 2026-08-26. Those tests do not replace live compositor qualification. DMS's native tools provide the stronger integration evidence.

Decision: import the Wayland packages at a pinned commit. Keep their types inside the platform package. Generate and own the layer-shell binding from pinned protocol XML.

## dgop

Source: <https://github.com/AvengeMedia/dgop>
Inspected commit: `473bc52` from 2026-08-25.

`dgop` exposes Go collectors and JSON models for CPU, memory, disks, rates, network, processes, hardware, and GPUs. Cursor values support interval-correct CPU and process measurements.

Evidence collected on 2026-08-26:

- `go test ./gops/... ./models/...` passed;
- a live `meta --modules cpu,memory --json` call returned valid metrics on the development machine;
- DMS imports `github.com/AvengeMedia/dgop/gops` and calls `GetMeta` inside its Go backend.

Decision: import pinned collector packages for built-in monitoring widgets. Do not launch a CLI process for each update. Wrap collection in one consumer-counted shell service.

## go-text/typesetting

Source: <https://github.com/go-text/typesetting>
Inspected commit: `ddb7ff96ad4d2dc730cbcae9dd5140023f319c3e` from 2026-07-29.

The project provides pure-Go font parsing, HarfBuzz-compatible shaping, bidirectional text, segmentation, font scanning, and glyph output. Fyne, Gio, and Ebitengine use it. The API remains at an unstable version.

Decision: qualify it during the architectural proof with Latin and joined/right-to-left fixtures. Keep a HarfBuzz/FreeType fallback if shaping, fallback, rasterisation, or memory tests fail.

## sysc-lock

Relevant local source: `internal/wayland/wlr_layer_shell.go`.

It proves:

- Go plus generated layer-shell bindings;
- protocol discovery;
- per-output layer-surface creation;
- operation on a multi-monitor Niri session.

It lacks render buffers, complete configure/scale propagation, frame callbacks, damage, full output lifecycle, seats, and input. Use it as a proof history, not as the new platform package.

## sysc-greet

Source assessed from the local `sysc-greet` repository.

The application logic is Go, but Kitty and Niri own its fullscreen Wayland surface and terminal rendering. Its Bubble Tea UI cannot back a native shell surface.

Adapt:

- gSlapper IPC, retry, and scoped shutdown patterns;
- wallpaper startup resolution;
- lifecycle and packaging lessons;
- theme color data when licensing and ownership allow it.

Skip greeter authentication, session discovery, terminal rendering, and greeter-specific Niri configuration.

## sysc-screen

The idle package and Niri adapter contain useful event-loop, cancellation, and systemd patterns. The existing Niri adapter parses CLI text. `sysc-shell` will use Niri's JSON socket protocol instead.

## gSlapper

Source: <https://github.com/Nomadcxx/gslapper>

gSlapper provides the strongest local native-surface reference:

- layer-shell and output discovery;
- per-output EGL surfaces;
- scaling and frame callbacks;
- long-running multi-output lifecycle;
- Unix-socket control.

Keep gSlapper as an external wallpaper/video process. Reuse lifecycle ideas rather than importing its GStreamer renderer into the shell.

## Umbriel

Source: <https://github.com/noctalia-dev/umbriel>

Umbriel is a wlroots/SceneFX compositor. It owns DRM, input, windows, workspaces, layout, XWayland, server-side layer-shell, session lock, and compositor IPC.

Decision: do not build a `sysc-shell` equivalent. Niri already owns those responsibilities. Building another compositor would add the largest risk while contributing nothing to the first shell milestones.

## DankCalendar and danksearch

Sources:

- <https://github.com/AvengeMedia/dankcalendar>
- <https://github.com/AvengeMedia/danksearch>

Both projects are substantial standalone services. DankCalendar contains provider sync, reminders, OAuth, keyring storage, recurrence, and its own database. `danksearch` runs a persistent filesystem index.

Decision: treat them as later external integrations through their CLI or IPC. Do not make either project a foundation dependency.

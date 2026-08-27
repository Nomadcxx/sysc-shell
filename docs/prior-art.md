# Prior-art Assessment

Date: 2026-08-26. Amended 2026-08-27 by [the plan audit](plans/2026-08-27-plan-audit-report.md).

## Summary

No inspected project supplies a complete Go-native desktop shell runtime. Several projects remove large parts of the work:

- DMS supplies reusable Go system integrations and a working pure-Go Wayland client path.
- `sysc-lock` proves layer-shell discovery and per-output surface creation on Niri, through CGO.
- gSlapper provides the strongest local example of long-running native Wayland surface and frame lifecycle.
- Noctalia v5 provides the feature, ownership, scene-tree, and service reference.
- Niri replaces the compositor role that Umbriel fills for Noctalia.

`sysc-shell` should combine those lessons in one Go repository. It should not port any project line by line.

## Noctalia v5 beta

Source: <https://github.com/noctalia-dev/noctalia>
License: MIT.

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
License: MIT.

Current DMS separates a large Go backend from its Quickshell/QML frontend. The inspected Go backend contains about 157,000 lines. Quickshell still owns the bar, panels, scene composition, rendering, input, animation, and dynamic QML widgets.

Relevant findings:

- DMS creates one `PanelWindow` per selected screen through Quickshell `Variants`.
- `DankBarWindow.qml` assigns the screen, anchors, layer, namespace, size, exclusive zone, and input mask.
- `NiriService.qml` connects to `$NIRI_SOCKET`, requests `EventStream`, and projects workspace, window, output, layout, overview, and cast events.
- DMS imports `dgop` as a Go library inside its IPC backend.
- DMS's screenshot selector and color picker use pure-Go Wayland, layer-shell, seats, `wl_shm`, viewporter, output scaling, buffer release, damage, and frame callbacks.
- Noctalia v5 uses `fractional-scale-v1` with `viewporter` and treats the compositor's preferred scale in 120ths as the surface scale. `sysc-shell` adopts the same contract; see the design's rendering section.
- DMS plugins remain dynamic QML. Their useful traits are manifests, source precedence, per-output instances, startup checks, namespaced state, and component injection.
- DMS weather runs Open-Meteo and geocoding calls from QML/curl. The behavior is reusable; the implementation is not.

Use directly or adapt:

- use the focused `sysc-wayland` extraction and generate local protocol bindings;
- pin `dgop` for monitoring collectors;
- adapt direct Niri event-stream handling;
- adapt output and buffer lifecycle patterns from native screenshot/color-picker code;
- copy behavior and visual requirements from built-in widgets.

Skip:

- Quickshell/QML frontend code;
- dynamic QML plugin loading;
- multi-compositor branches;
- DMS's full backend as one imported service.

## dankgo and sysc-wayland

Source: <https://github.com/AvengeMedia/dankgo>
Inspected commit: `10434658325c` from 2026-08-23.

The upstream repository uses MIT at its root. The copied Wayland client and scanner subtrees carry
BSD-3-Clause licence files. The commit is the tip of `main`, not a tagged release.

`dankgo` provides:

- a pure-Go Wayland wire client;
- generated core Wayland bindings covering `wl_output` version 4 (`name` and `description` events),
  `wl_compositor` 6, `wl_seat` 9 and `wl_surface` 6, plus xdg-shell;
- a Go Wayland protocol scanner under `cmd/go-wayland-scanner`, a fork of `rajveermalviya/go-wayland`
  carrying its own BSD license file;
- Unix-socket IPC, paths, D-Bus helpers, and process-supervision utilities.

The focused `go test ./wayland/...` check passed on 2026-08-26. Those tests do not replace live compositor
qualification.

The 2026-08-27 audit ran a probe built against this commit on Niri 26.04: registry, `wl_output` v4 naming,
layer surface, fractional scale, viewporter, `wl_shm` buffers, frame callbacks, buffer release, pointer,
and teardown all worked with no protocol error. Three properties of the client shape the platform package
and are recorded in the proof plan: there is no `prepare_read`/`read_events` split and no write buffer, so
requests flush immediately and a plain `poll()` is correct; `Dispatch()` reads exactly one message and
blocks when none is pending; and `wl_display.error` is **silently discarded** unless a handler is
installed.

Decision: extract the client and scanner into `github.com/Nomadcxx/sysc-wayland`, preserve the subtree
licences and provenance, repair stream and descriptor handling there, and publish `v0.1.0` before shell
implementation. Keep xdg-shell and shell extension XML and bindings in `sysc-shell`; the scanner accepts
the local xdg-shell import path when layer-shell generation needs `xdg_popup`.
Generate and own the layer-shell binding from pinned protocol XML, invoking the scanner with an
`@v0.1.0` suffix so its build dependencies stay out of this repository's `go.sum`.

## dgop

Source: <https://github.com/AvengeMedia/dgop>
Inspected commit: `473bc52` from 2026-08-25.
License: MIT.

`dgop` exposes Go collectors and JSON models for CPU, memory, disks, rates, network, processes, hardware, and GPUs. Cursor values support interval-correct CPU and process measurements.

Evidence collected on 2026-08-26:

- `go test ./gops/... ./models/...` passed;
- a live `meta --modules cpu,memory --json` call returned valid metrics on the development machine;
- DMS imports `github.com/AvengeMedia/dgop/gops` and calls `GetMeta` inside its Go backend.

Decision: import pinned collector packages for built-in monitoring widgets. Do not launch a CLI process for each update. Wrap collection in one consumer-counted shell service.

## go-text/typesetting

Source: <https://github.com/go-text/typesetting>
Inspected commit: `ddb7ff96ad4d2dc730cbcae9dd5140023f319c3e` from 2026-07-29.

License: Unlicense OR BSD-3-Clause.

The project provides pure-Go font parsing, HarfBuzz-compatible shaping, bidirectional text, segmentation,
font scanning, and glyph output. Fyne, Gio, and Ebitengine use it. The API remains at an unstable version.

Verified at this commit on 2026-08-27: `font.ParseTTF`, `shaping.HarfbuzzShaper.Shape` with explicit
direction, script, language and 26.6 size, `Face.GlyphData` returning `font.GlyphOutline`, and segment
operations that map onto `golang.org/x/image/vector.Rasterizer`. A run shaped and rasterised Latin and
Arabic with zero `notdef` and identical output across two calls, and contextual joining was confirmed by
every shaped glyph ID differing from its nominal `cmap` glyph ID.

Two assets in this module matter to the plan:

- `font/testdata/Amiri-Regular.ttf` with `font/testdata/OFL.txt` -- SIL OFL 1.1, the joined-script CI
  fixture. Vendor both into `internal/render/testdata/`; `goregular` has no Arabic coverage.
- `fontscan` -- `NewFontMap`, `UseSystemFonts` with a disk cache, `SetQuery`, and `ResolveFace(rune)` for
  per-rune fallback. This covers the bar milestone's font discovery and fallback needs directly.

Decision: qualify it during the architectural proof with Latin and joined/right-to-left fixtures. Keep a
HarfBuzz/FreeType fallback if shaping, fallback, rasterisation, or memory tests fail.

## sysc-lock

Relevant local source: `sysc-lock/internal/wayland/wlr_layer_shell.go` (checkout under
`~/Documents/sysc-lock`).

It is a **CGO client**, not a pure-Go one. The file opens with `#cgo pkg-config: wayland-client` and
compiles `wayland-scanner`-generated C bindings from `internal/wayland/protocols/`.

It proves:

- protocol discovery;
- per-output layer-surface creation;
- operation on a multi-monitor Niri session.

It proves **nothing about the pure-Go client path**, which is the risk the architectural proof exists to
qualify. It also lacks render buffers, complete configure/scale propagation, frame callbacks, damage, full
output lifecycle, seats, and input. Use it as proof history for layer-shell behavior, not as evidence for
the Go Wayland client and not as the new platform package.

## Licensing summary

Noctalia, DankMaterialShell, dgop and the `dankgo` repository root use MIT. The extracted Wayland client
and scanner use BSD-3-Clause. `go-text/typesetting` is Unlicense or BSD-3-Clause, and
`Amiri-Regular.ttf` is SIL OFL 1.1.

Behavior, layout, interaction and feature inventory are not copyrightable and may be copied freely as
requirements, which covers most of what this assessment proposes to reuse. MIT also permits source
adaptation provided the copyright notice and license text travel with it, so clean-room reimplementation
is not a legal requirement. The binding constraint is the project's own rule against translating their
source.

Two vendored assets do carry notice obligations: the protocol XML files, whose copyright headers must be
preserved verbatim, and `Amiri-Regular.ttf`, which must ship beside `OFL.txt`.

## sysc-greet

Source assessed from the local `sysc-greet` repository (`~/sysc-greet`).

The application logic is Go, but Kitty and Niri own its fullscreen Wayland surface and terminal rendering. Its Bubble Tea UI cannot back a native shell surface.

Adapt:

- gSlapper IPC, retry, and scoped shutdown patterns;
- wallpaper startup resolution;
- lifecycle and packaging lessons;
- theme color data when licensing and ownership allow it.

Skip greeter authentication, session discovery, terminal rendering, and greeter-specific Niri configuration.

## sysc-screen

Checkout under `~/Documents/sysc-screen`. The idle package and Niri adapter contain useful event-loop,
cancellation, and systemd patterns. The existing Niri adapter parses CLI text. `sysc-shell` will use
Niri's JSON socket protocol instead.

## gSlapper

Source: <https://github.com/Nomadcxx/gslapper> (local checkout `~/gSlapper`)

gSlapper provides the strongest local native-surface reference:

- layer-shell and output discovery;
- per-output EGL surfaces;
- scaling and frame callbacks;
- long-running multi-output lifecycle;
- Unix-socket control.

Keep gSlapper as an external wallpaper/video process. Reuse lifecycle ideas rather than importing its
GStreamer renderer into the shell. The shell should not own its lifecycle by default; the user's service
manager does, which keeps the wallpaper slice a client of an existing control socket instead of a process
supervisor.

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

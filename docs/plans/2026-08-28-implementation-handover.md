# sysc-shell Implementation Handover

Date: 2026-08-28

## Purpose

This document hands `sysc-shell` to the agent that will start the architectural proof. It records the
released dependency baseline, settled architecture, execution order, known traps, and the exact first
task. Read the linked plans before writing product code. Do not start the multi-output bar or any later
roadmap work until the proof passes its live Niri gate.

## Current state

`sysc-shell` contains approved and audited plans but no product code, generated bindings, or Go module.
The clean baseline is:

| Item | State |
|---|---|
| Local repository | `/home/nomadx/sysc-shell` |
| Remote | `https://github.com/Nomadcxx/sysc-shell.git` |
| Branch | `main` |
| Baseline commit | `a77552477268dacad3204ed60fc13be04ff4e828` |
| Licence | BSD-3-Clause |
| Product stage | Milestone 0 complete; Milestone 1 not started |
| First executable milestone | Architectural proof |
| Required compositor | Niri |
| Required platform | Linux |

The relevant documents are:

- [agent rules](../../AGENTS.md);
- [approved architecture](2026-08-26-sysc-shell-design.md);
- [architectural-proof plan](2026-08-26-architectural-proof.md);
- [development orchestration](2026-08-26-development-orchestration.md);
- [roadmap](../roadmap.md);
- [prior-art assessment](../prior-art.md);
- [plan-audit report](2026-08-27-plan-audit-report.md).

The audit verdict was `ready after listed fixes`. The owning documents contain those fixes. Owner
decisions D1 and D2 are settled. D3 through D5 remain deferred to their named milestones.

## Released Wayland foundation

`sysc-wayland v0.1.1` is the required foundation for new shell work.

| Item | Value |
|---|---|
| Local repository | `/home/nomadx/sysc-wayland` |
| Remote | `https://github.com/Nomadcxx/sysc-wayland.git` |
| Release commit | `a8a80eefaad41bd47eb05cc27c1777bf1a202245` |
| Remote `main` | same commit |
| Final tag | `v0.1.1`, annotated tag peeling to the same commit |
| Reviewed candidate | `v0.1.1-rc.1`, peeling to the same commit |
| Release branch | `fix/v0.1.1`, retained locally and on the remote |
| Release worktree | `/home/nomadx/.config/superpowers/worktrees/sysc-wayland/fix/v0.1.1` |
| Licence | BSD-3-Clause, with preserved upstream notices |

Release qualification completed on 2026-08-28:

- `go mod tidy` left `go.mod` and `go.sum` unchanged;
- `go generate ./client` reproduced the checked core binding;
- `go test -race ./...`, `go vet ./...`, and `go build ./...` passed;
- two clean modules generated xdg-shell, layer-shell, fractional-scale, and viewporter through the
  remote `@v0.1.1-rc.1` module resolver;
- each generated pair was byte-identical and both modules built;
- a live Niri client completed two roundtrips and received `wl_output.name` values `DP-1` and `DP-3`;
- a clean module named `github.com/Nomadcxx/sysc-shell` resolved
  `github.com/Nomadcxx/sysc-wayland v0.1.1` without a local replacement.

Generated qualification hashes were:

```text
xdg-shell        61f9d1737146909ff4a025312482da6ddec9ccdabb4b9fa2fafe9a6cfd877a98
layer-shell      9c42bafd3ac00101f374c8b68bd86199c2e901ea3fd22b7d69f879630765c0da
fractional-scale 6818f0a02c8f56b66d632a5e960a74b8987a6774628de75d1dcecd1f424fb9f6
viewporter       935ba80b2e63c93676dc7ceb82c3fba347958564706c206efa6a7dbeff6a4673
```

The v0.1.1 correction pins the exact Wayland 1.24 core XML needed to reproduce the public binding and
fixes generated array request framing. An array request now includes the four-byte raw-length field,
four-byte payload padding, and the padded cursor advance. Do not work around transport or scanner
behavior inside `sysc-shell`; fix any new wire defect in `sysc-wayland` and release it there.

## Required preflight correction

Several `sysc-shell` documents predate v0.1.1 and still say `v0.1.0`. Before Task 1, make one focused
documentation commit on the milestone branch:

1. replace the shell's `sysc-wayland v0.1.0` dependency and scanner pins with `v0.1.1` in `AGENTS.md`,
   `README.md`, the architecture, proof plan, orchestration plan, audit gate, roadmap, and prior-art
   references where they describe the active dependency;
2. add `-prefix xdg_` to Task 6's xdg-shell scanner command;
3. keep the explicit local xdg-shell import in the layer-shell scanner command;
4. keep each generated protocol in its own package directory;
5. confirm every active pin with:

   ```bash
   rg -n 'sysc-wayland.*v0\.1\.0|scanner@v0\.1\.0' AGENTS.md README.md docs
   ```

The xdg prefix is mandatory. The original v0.1.0 release-candidate qualification omitted it and
produced a layer-shell package that did not compile against the generated xdg-shell package. Use the
known-working command shape from the `sysc-wayland v0.1.1` README:

```bash
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 \
  -pkg xdgshell -prefix xdg_ \
  -o xdg_shell.go -i ../../../../protocols/xdg-shell.xml
```

For layer-shell, retain:

```text
-xdg-shell-import github.com/Nomadcxx/sysc-shell/internal/platform/wayland/xdgshell
```

This preflight changes documentation and command pins only. It does not reopen the architecture.

## Product boundary

Build a native Go Wayland shell for Niri that grows toward Noctalia and DMS behavior parity. Do not
port their source or preserve their configuration, QML, or plugin APIs.

Fixed constraints:

- Go owns shell state, Niri IPC, layout, rendering, widgets, configuration, and plugin supervision;
- do not add C++, Rust, Lua, Luau, Qt, QML, or Quickshell;
- do not build a compositor, lock screen, session-lock client, or Umbriel equivalent;
- do not add multi-compositor abstractions during the foundation stages;
- use `wl_shm` first and add EGL/OpenGL ES only for a measured failing case;
- build shell-specific components as real consumers require them;
- do not turn `internal/ui` into a general-purpose Go toolkit;
- run third-party plugins as supervised processes, never as Go binary plugins;
- keep shell product code in one repository until a component has a second real consumer and a stable
  API.

The proof is intentionally narrow: one top-anchored 48-logical-pixel layer surface on one selected Niri
output. It contains shaped text, a meter, and a button. A Niri workspace event changes text, and a
pointer click changes model state.

## Repository ownership

Keep ownership sharp so the repositories do not grow overlapping platform layers.

| Repository | Owns | Must not own |
|---|---|---|
| `sysc-wayland` | Pure-Go Wayland transport, core bindings, proxy lifecycle, protocol scanner | Layer policy, shell state, rendering, widgets, Niri integration |
| `sysc-shell` | Niri state, all Wayland surfaces, rendering, UI, configuration, interaction, plugin supervision | D-Bus notification or tray service state |
| `sysc-metrics` | Read-only Linux telemetry and sampling | CLI, daemon, shell UI, portability API |
| `sysc-notify` | `org.freedesktop.Notifications`, bounded state, shell IPC | Wayland surfaces, popup rendering, Niri focus decisions |
| `sysc-tray` | StatusNotifierWatcher/Host, items, DBusMenu, shell IPC | Wayland surfaces, icon presentation, menu rendering |

Current supporting repository state:

| Repository | Main commit | Stage |
|---|---|---|
| `sysc-metrics` | `ea6a54ca20ec55574be183836e019a685c4573a0` | design only |
| `sysc-notify` | `32da2b53d95c7f7d46ca576f3e3eb739f2c0faf4` | design only |
| `sysc-tray` | `04ca0183efc257c69af664c0eaaf635fc401081c` | design only |

`sysc-metrics` may develop beside the proof after its M0 API gate. Notification and tray research may
also proceed, but shell integration waits until panels, images, menus, input, and accessibility have
owners. Do not let a service repository define shell widget trees.

## Architecture invariants

### Concurrency and ownership

One goroutine owns the Wayland connection and every proxy created from it. No renderer, Niri reader,
collector, widget, or plugin goroutine may call a Wayland proxy. Other goroutines submit commands or
immutable state through channels.

The proof uses a concrete `Callbacks` struct, not a single-implementation `App` interface. The caller
owns the invalidation channel. `Run` receives from it and never closes it.

### Output and workspace truth

Wayland owns outputs. Niri owns workspaces.

- `wl_registry.global` and `global_remove` own output existence and hotplug;
- `wl_output.name` version 4 supplies connector identity such as `DP-1`;
- `wl_output.geometry`, `mode`, and `done` supply transform, mode, and physical properties;
- `wp_fractional_scale_v1.preferred_scale` supplies the render scale per surface;
- `zwlr_layer_surface_v1.configure` supplies the usable surface size;
- Niri `WorkspacesChanged` and `WorkspaceActivated` supply workspace state keyed by output name;
- derive focused output from the workspace whose `is_focused` field is true.

Niri has no output event. Never create or destroy an output host from Niri state. Hold unmatched Niri
workspace state by connector name and join it to Wayland hosts when both exist.

Milestone 2 keys each `OutputHost` by the `wl_registry` global name, a `uint32`. A connector string is a
configuration attribute, not host identity. This prevents duplicate hosts after reconnect or rename.

### Configure, scale, and coordinates

Keep four values distinct:

1. connector identity;
2. logical surface coordinates;
3. fractional scale as a numerator over 120;
4. physical buffer pixels.

Never reduce `scale120` to an integer. Compute each physical dimension as:

```text
(logical * scale120 + 60) / 120
```

Validate multiplication and every `int32` conversion before allocating or sending dimensions.

Leave `wl_surface.set_buffer_scale` at 1. Set only the viewport destination to the logical configure
size. Do not set a viewport source rectangle. Submit `wl_surface.damage_buffer` in buffer pixels. Layout
and hit testing use logical units; painting and damage use buffer pixels.

The layer configure size may differ from the output mode and Niri's logical width because other layer
clients reserve space. Never derive a shell surface size from `wl_output` mode or Niri IPC.

The initial surface sequence is fixed: create the surface and layer role, set anchors and properties,
commit once without a buffer, receive configure, acknowledge it, then attach and commit a buffer.
Fractional scale may arrive before or after configure, or by itself after a scale change. A scale-only
event at unchanged logical size still retires the old physical buffer generation and redraws.

### Buffers and frame scheduling

Use two memfd-backed `wl_shm` slots. A buffer generation owns its memfd, mapping, pool, and buffers.
During reconfigure, allocate a new generation and retire the old one only after every buffer reports
`wl_buffer.release`, or after the surface is destroyed.

A frame callback does not release a buffer. Niri was observed delivering `wl_callback.done` while the
submitted slot remained busy. The scheduler must handle frame-first and release-first orderings,
coalesce repeated invalidations, discard a pending render after reconfigure, and stop all work after a
surface close.

Draw only after invalidation. Request one frame callback per submitted frame. An invalidation while a
frame is pending sets one pending flag. No state change means no new commit and no continuous frame
loop.

### Wayland registry and polling

Bind each global at `min(serverVersion, clientMaximum)`. The proof owns this maximum table:

| Interface | Maximum |
|---|---:|
| `wl_compositor` | 6 |
| `wl_shm` | 1 |
| `wl_seat` | 7 |
| `wl_output` | 4 |
| `zwlr_layer_shell_v1` | 4 |
| `wp_fractional_scale_manager_v1` | 1 |
| `wp_viewporter` | 1 |

The proof requires compositor, shm, seat, layer-shell, fractional-scale manager, viewporter, and at
least one output. Name a missing required interface in the startup error. The proof has no integer-scale
fallback because it exists to qualify the fractional path.

Install a display-error handler before any other request. `sysc-wayland` records the protocol error as a
sticky connection error before calling the optional handler.

Use `Context.ControlFD` only inside its callback. Never retain or expose the socket descriptor. Poll the
Wayland descriptor and a cancellation/invalidation wake descriptor on the owner goroutine. Dispatch one
message for each readiness, then poll with zero timeout to drain. Return every control, dispatch, write,
and protocol error.

### Pointer input

Create or release the pointer when `wl_seat.capabilities` changes. Track enter and leave. Ignore motion
without focus. Wayland supplies viewport-destination coordinates, which match logical hit-test bounds.
Record the node on press and activate only when release occurs inside the same node. The proof does not
need keyboard focus or an input serial; add the serial before Milestone 4.

### Niri IPC

Connect directly to `$NIRI_SOCKET`; do not shell out to `niri msg`. Write the JSON string
`"EventStream"` followed by a newline. Read and validate the first reply before reading events:

```json
{"Ok":"Handled"}
```

Treat `{"Err":"..."}` as startup failure. The stream then supplies a complete initial
`WorkspacesChanged` snapshot, so the proof needs no separate initial query. Decode known events through
private wire structs with pointer fields for required-value validation. Keep workspace IDs as `uint64`.
Accept nullable `name` and `output`, reject malformed known events without publishing partial state,
ignore unknown top-level events, limit each line to 1 MiB, and publish only the newest snapshot when the
consumer is slow.

### UI, text, and rendering

The proof tree contains one row with two text nodes, a fixed-width meter, and a button. Measurement flows
from leaves to root. Arrangement flows from root to leaves. Painting follows source order. Hit testing
walks the arranged tree in reverse paint order.

Use `go-text/typesetting` at commit `ddb7ff96ad4d2dc730cbcae9dd5140023f319c3e` and
`golang.org/x/image/vector`. Shape at the physical pixel size. Vendor `Amiri-Regular.ttf` and `OFL.txt`
from the pinned module for the joined-script test. Confirm contextual joining by proving shaped glyph IDs
differ from nominal glyph IDs; merely receiving glyphs does not qualify shaping.

The canvas uses little-endian premultiplied ARGB8888 in B, G, R, A memory order. Select ARGB8888 from
the compositor's advertised `wl_shm.format` events instead of assuming it. Validate stride and buffer
length, premultiply colors, and clip fills and masks.

## Milestone sequence

1. **M0, foundation:** completed after `sysc-wayland v0.1.1` qualification and this handover.
2. **M1, architectural proof:** one interactive layer surface on one output. This is the next milestone.
3. **M2, stable bar:** one host and bar per active output, hotplug, mixed scales, transforms, exclusive
   zones, sections, reload, and a 60-minute idle gate.
4. **M3, built-in widgets:** clock, date, Niri state, weather, CPU, memory, storage, block and network
   rates, battery, and shared service lifetimes.
5. **M4, panels and controls:** popouts, keyboard focus, menus, lists, settings, accessibility behavior.
6. **M5, notifications and tray:** integrate released headless services through bounded versioned IPC;
   keep presentation in the shell.
7. **M6, external widgets and plugins:** version the component vocabulary proved by built-ins and panels.
8. **M7, shell breadth:** launcher, OSDs, clipboard, control center, media, wallpaper, and desktop widgets
   as separately designed vertical slices.
9. **M8, rendering qualification:** retain `wl_shm` unless measurements justify a second renderer.

Each milestone ends at its gate. Do not pull a later component into an earlier branch.

## Architectural-proof execution order

The approved proof plan has ten tasks. Follow it with TDD and one task-aligned commit each.

1. Initialise the module and CLI parser.
2. Build the retained proof tree, row layout, and hit testing.
3. Qualify pure-Go shaping and rasterisation.
4. Paint into ARGB buffers.
5. Implement the pure two-slot scheduler.
6. Pin XML and generate the four shell protocol packages.
7. Implement the Wayland owner and `wl_shm` surface.
8. Implement typed Niri event streaming.
9. Integrate the proof model.
10. Run and document the live Niri gate.

Run Task 1 alone. After its commit, independent lanes may branch from that exact commit:

| Lane | Tasks | Merge dependency |
|---|---|---|
| UI/render | 2 through 5, in order | merge before Task 7 |
| Protocols | 6 | merge before Task 7 |
| Niri | 8 | merge before Task 9 |

The integration owner merges UI/render and protocols, executes Task 7, merges Niri, then owns Tasks 9
and 10. A lane that needs another lane's public contract stops for an integration decision. Do not let
parallel work edit the same files or invent substitute APIs.

## Exact next task

1. Confirm `/home/nomadx/sysc-shell` is clean and `main` is at the baseline named above.
2. Create a dedicated worktree and branch named `milestone/architectural-proof`.
3. Commit the v0.1.1 preflight correction described above.
4. Execute Task 1 from the proof plan with strict red, green, refactor order.

Task 1 creates only:

```text
go.mod
go.sum
cmd/sysc-shell/main.go
cmd/sysc-shell/main_test.go
```

Use module path `github.com/Nomadcxx/sysc-shell`, Go language level 1.26, and these direct pins:

```text
github.com/Nomadcxx/sysc-wayland v0.1.1
github.com/go-text/typesetting ddb7ff96ad4d2dc730cbcae9dd5140023f319c3e
golang.org/x/image v0.44.0
golang.org/x/sys v0.47.0
```

Write the `parseOptions` tests first. Cover `--output DP-1`, an unknown flag, and a missing flag value.
Run the test and confirm it fails because `parseOptions` does not exist. Then add the minimum
`flag.NewFlagSet` parser, signal-aware main path, and `run` stub from the plan. Finish with:

```bash
go test ./cmd/sysc-shell
go vet ./cmd/sysc-shell
git diff --check
```

Commit Task 1 as:

```text
build: initialize sysc-shell module
```

Stop after Task 1 for review before opening parallel lanes.

## Known traps and rejected approaches

- Do not build an Umbriel equivalent. Niri already owns compositor responsibilities.
- Do not import all of DMS, dgop, or dankgo. Use behavior and focused code as references; consume
  `sysc-wayland` and later released `sysc-metrics` APIs.
- Do not add a permanent smoke CLI flag. Task 7's flat-color wiring is temporary and Task 9 replaces it.
- Do not use a connector string as output-host identity.
- Do not derive the layer surface size from output mode or Niri state.
- Do not use integer scale, `wl_surface.damage`, or `wp_viewport.set_source`.
- Do not reuse, unmap, close, or resize storage while any compositor-held buffer remains busy.
- Do not treat frame completion as buffer release.
- Do not invoke Wayland from a second goroutine, even for cancellation or redraw.
- Do not discard the Niri reply envelope as an unknown event.
- Do not design the plugin protocol before built-in widgets prove the node vocabulary.
- Do not claim plugin capabilities provide OS isolation. Version one treats plugins as trusted child
  processes.
- Do not add a renderer interface while `wl_shm` is the only renderer.
- Do not add empty package scaffolding, a DI container, a logging framework, a configuration library, a
  Makefile, or speculative UI primitives.

## Deferred decisions and unqualified hardware behavior

- Choose a pure-Go SVG strategy before Milestone 3. Standard Go and `x/image` do not decode SVG, while
  freedesktop icon themes use it heavily.
- Revisit OS-level plugin isolation before the plugin milestone. The current capability model records
  intent but does not sandbox filesystem, network, process, or D-Bus access.
- Settle AT-SPI scope during Milestone 4 design. Keyboard navigation, visible focus, reduced motion, and
  high contrast remain mandatory at that milestone regardless.
- Multi-output hotplug, physical disconnect, output transform, and layer-shell v5
  `set_exclusive_edge` were not exercised during the audit.
- The audit verified Niri 26.04 behavior from the installed binary and live probes. Revalidate wire
  fixtures when the supported Niri version changes.
- The proof deliberately does not claim paragraph-level bidi, general font fallback, keyboard input, or
  compositor portability.

## Proof acceptance gate

The finished proof must run as:

```bash
go run ./cmd/sysc-shell --output DP-1
```

It must reserve 48 logical pixels, render the fixed tree, update the workspace label, route a pointer
click, render at scale 1 and a non-1 scale, remain idle without redraws, and release all resources on
SIGINT or SIGTERM.

Before acceptance, run:

```bash
go test -race ./...
go vet ./...
go build ./cmd/sysc-shell
```

Record the Niri version, output properties, configure size, `scale120`, physical buffer size and stride,
advertised and selected shm formats, click and workspace behavior, idle frame count, and shutdown result.
Use an already-scaled output when available. If a temporary scale change is required, capture the exact
old value, use a non-focused output, restore it, and verify the restored value.

The milestone stops after Task 10. The next activity is a design review for one bar on every output, not
bar implementation in the proof worktree.

## Resume commands

Start by inspecting rather than mutating:

```bash
cd /home/nomadx/sysc-shell
git status --short
git log -1 --oneline --decorate
git remote -v
rg -n 'sysc-wayland.*v0\.1\.0|scanner@v0\.1\.0' AGENTS.md README.md docs
```

After updating the v0.1.1 pins, prove the dependency from a clean module without a local replacement:

```bash
go mod init github.com/Nomadcxx/sysc-shell
go get github.com/Nomadcxx/sysc-wayland@v0.1.1
go list -m github.com/Nomadcxx/sysc-wayland
```

Do not add a `replace` directive. Do not use a local scanner during release or generation
qualification.

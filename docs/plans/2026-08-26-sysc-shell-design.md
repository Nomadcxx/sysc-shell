# sysc-shell Architecture Design

Date: 2026-08-26
Status: Approved. Amended 2026-08-27 by [the plan audit](2026-08-27-plan-audit-report.md).

## Product definition

`sysc-shell` will provide a native Wayland shell for Niri. Go will own the shell process, system services, compositor state, UI tree, rendering decisions, widgets, configuration, and plugin supervision.

The project aims for growing capability parity with Noctalia and DMS. It will not translate their source or preserve their configuration and plugin APIs.

## Constraints

- No C++, Rust, Lua, Luau, Qt, QML, or Quickshell.
- Niri is the required compositor.
- No lock-screen or session-lock client.
- No new compositor. Niri owns DRM, input devices, workspaces, windows, layout, XWayland, and server-side layer-shell policy.
- Go is the primary language. Mature C libraries may sit behind a narrow boundary when measurements or correctness tests reject the Go path.
- The first renderer uses shared-memory buffers and redraws after invalidation. A continuous frame loop is out of scope.

## Goals

1. Prove the architecture with one interactive native layer surface on Niri.
2. Run one stable bar surface on each active output.
3. Build a shell-specific retained UI runtime with text, layout, input, damage, and standard controls.
4. Ship useful built-in widgets before opening the plugin API.
5. Support external-process plugins through a versioned, host-rendered protocol.
6. Expand toward launcher, notifications, panels, OSDs, tray, clipboard, media, and session controls after the bar foundation holds.

## Non-goals

- General desktop application development.
- A public Go UI toolkit.
- Arbitrary in-process plugin code.
- Quickshell, QML, Noctalia, or DMS compatibility.
- Multi-compositor portability during the foundation stages.
- GPU rendering before a measured software-rendering limit.
- Additional platform repositories without a second consumer. `sysc-wayland` is the owner-approved
  exception because the shell depends on transport fixes and generator changes that need an independent
  release gate.

## System shape

```text
Niri event socket ──> Niri adapter ───────┐
                                          │
Linux and D-Bus services ─> services ─────┼─> immutable shell state
                                          │             │
plugin processes <── versioned IPC ─> plugin host       │
                                                        v
                                                 retained UI tree
                                                        │
                                                measure + layout
                                                        │
                                                damage + hit map
                                                        │
Wayland events ──> output hosts ──> surfaces ──> wl_shm renderer
```

One process owns the shell. It may run collectors and plugin supervision in separate goroutines, but one goroutine owns every Wayland proxy and dispatch operation. Callers send commands to that goroutine instead of invoking Wayland objects themselves.

## Dependency policy

The first proof pins these projects:

- `github.com/Nomadcxx/sysc-wayland v0.1.0` for the Wayland wire client and protocol generator;
- `github.com/go-text/typesetting` for pure-Go shaping and font parsing;
- `golang.org/x/image` and `golang.org/x/sys` for rasterisation and Linux system calls.

The system-monitor milestone pins `github.com/AvengeMedia/dgop` and imports its collector packages. The shell will not execute a new `dgop` process for each sample.

`sysc-wayland` starts as an owned extraction from `dankgo` commit
`10434658325c819efaf063f48eec4ae36555727e`; its repository records provenance and local divergences.
`go-text/typesetting` uses an unstable version, so its types stay behind the text package and the project
pins a known commit. Other dependencies follow the dependency-ownership ladder in the `sysc-wayland`
design; the new repository does not make forks the default.

## Wayland platform

The Wayland package owns:

- display connection and registry lifecycle;
- compositor, shared-memory, output, seat and layer-shell globals;
- generated protocol bindings from pinned XML;
- output add/remove and scale events;
- fractional-scale and viewporter objects for logical-to-buffer mapping;
- layer-surface configure, close and exclusive-zone state;
- pointer and keyboard events;
- shared-memory file, pool and buffer lifetimes;
- frame callbacks, buffer release and damage submission;
- shutdown ordering.

Each registry global binds at `min(server version, client supported version)`, where the client maximum
comes from a table this project owns; the generated client exports no per-interface version constant. Missing required
globals fail startup with a named error. Optional protocols expose an unavailable capability and do not
abort the shell.

Output identity comes from `wl_output.name`, which `wl_output` version 4 provides directly.
`zxdg_output_manager_v1` is not required.

### Source of truth for output and workspace state

Wayland owns outputs. Niri owns workspaces. There is no overlap to reconcile, because Niri's event stream
emits no output events at all.

| Property | Owner |
|---|---|
| Output existence, add and remove | `wl_registry.global` / `global_remove` for `wl_output` |
| Connector identity (`DP-1`) | `wl_output.name` (version 4) |
| Transform, mode, physical size | `wl_output.geometry` / `mode`, committed on `done` |
| Render scale | `wp_fractional_scale_v1.preferred_scale`, per surface |
| Usable surface size | `zwlr_layer_surface_v1.configure` -- **not** the output size |
| Workspaces and active workspace per output | Niri `WorkspacesChanged` / `WorkspaceActivated`, keyed by output name |
| Focused output | derived from the workspace whose `is_focused` is true |

A Niri workspace event may name an output whose `wl_output` has not yet been announced, or has already
been removed. Hold Niri workspace state keyed by output name and join it to hosts lazily. Never create or
destroy an output host from a Niri event.

The layer-surface configure size is the output area minus other clients' exclusive zones, so it can be
smaller than the output. Nothing may derive a surface size from `wl_output` mode or from Niri IPC.

### Output and surface model

An `OutputHost` owns the shell surfaces for one `wl_output`:

```text
OutputHost
├── bar surface                 top layer, exclusive zone
├── panel surfaces              added when popouts exist
└── overlay/OSD surfaces        added by later milestones
```

The bar is a layer surface, not the layer itself. The compositor maintains the layer for each output. The shell creates an independent surface for each output and gives that surface the layer-shell role.

The shell never places all UI on one fullscreen transparent surface. Bars, panels, notifications, OSDs, and backgrounds need different anchors, input regions, keyboard policies, z-order, and lifetimes.

The output host state machine is:

```text
discovered -> role-created -> initial-empty-commit -> configured
configured -> buffer-attached -> mapped
mapped -> reconfigure -> mapped          (layer-surface configure: new logical size)
mapped -> rescale -> mapped              (preferred_scale alone: same logical size, new buffer size)
mapped -> output-removed/closed -> destroyed
```

`wp_fractional_scale_v1.preferred_scale` is independent of layer-shell configure. A scale change at an
unchanged logical size emits only `preferred_scale`, with no configure to acknowledge, and still requires
a new buffer generation. It is also unordered against the first configure, so the host starts at 120.

Hosts are keyed by `wl_registry` global name, never by connector string. Global names are unique for the
connection's lifetime; a connector name can disappear and return as a different monitor.

The shell acknowledges every configure before attaching a matching buffer. It destroys child resources before the parent surface and disconnects only after all hosts stop.

## Rendering

The first renderer writes premultiplied ARGB pixels into `wl_shm` buffers. `ARGB8888` is selected from
the advertised `wl_shm.format` list, not assumed. Each surface owns at least two slots. A slot remains busy
from attach until `wl_buffer.release`.

Frame completion does not release a buffer. `wl_callback.done` for a commit arrives while that commit's
buffer is still held; the release arrives only once the next buffer has been attached and committed. The
renderer skips or coalesces a redraw when no slot is free, and never infers slot availability from a frame
callback.

The scale contract is fixed by `fractional-scale-v1`: `wl_surface.set_buffer_scale` stays at 1, the
`wp_viewport` destination is the logical size, and the buffer is the logical size multiplied by the
preferred scale in 120ths, rounded half away from zero. No viewport source rectangle is set. Layout and
hit testing work in logical units; painting works in buffer pixels.

A state change marks affected nodes dirty. Layout changes expand damage to old and new bounds. The surface requests one frame callback, submits accumulated damage, commits, and waits. A later invalidation sets a pending flag instead of issuing another frame.

Damage is submitted with `wl_surface.damage_buffer` in buffer pixels. `wl_surface.damage` takes surface
units, which are ambiguous under a viewport. The proof starts with full-surface damage. The bar milestone
adds rectangle damage after tests cover old and new bounds. The project will add EGL/OpenGL ES only when profiling shows that shared-memory rendering misses an agreed frame, CPU, or power budget.

No renderer interface will exist while `wl_shm` is the only implementation. The second renderer, if required, will justify the shared contract.

## Text

Text quality is a foundation concern. The proof will shape and rasterise Latin plus one explicitly
directed right-to-left joined-script run with `go-text/typesetting`, using `Amiri-Regular.ttf` (SIL OFL
1.1) from the pinned module's test data. That qualifies shaping, contextual joining and rasterisation; it
does not claim general bidirectional support. Paragraph-level bidi and segmentation become a gate when
real user text such as window titles reaches the bar. It will measure runs before layout. The bar milestone adds a glyph-mask cache after the proof records the uncached cost.

The bar milestone adds system font discovery, fallback, truncation, and scale tests. The pinned
dependency's `fontscan` package already supplies the whole minimum -- system scanning with a disk cache,
family and aspect matching, and per-rune fallback -- so this is component work, not a font-management
project. Color emoji, input methods, and advanced accessibility enter later milestones when a component needs them.

HarfBuzz and FreeType remain the fallback. The team will choose them only if the pure-Go implementation fails shaping, fallback, memory, or rendering benchmarks that matter to shell content.

## UI runtime

The retained tree uses a small fixed vocabulary:

- row, column, stack and spacer;
- text, icon, image, rectangle, meter and graph;
- button, toggle, slider, scroll area and list as milestones require them.

Each node holds style inputs, measured size, arranged bounds, paint data, input state, accessibility metadata, and children when applicable. Measurement flows from leaves toward the root. Arrangement flows from the root toward leaves. Painting follows z-order. Hit testing walks the same arranged tree in reverse paint order.

State mutation occurs in the shell model. Nodes receive immutable view data and emit named actions. This keeps system operations out of rendering code and lets unit tests exercise layout and actions without Wayland.

The runtime stays internal. It will not expose arbitrary shaders, user-defined primitives, general windows, or a scripting language.

## Niri integration

The Niri adapter connects to `$NIRI_SOCKET` and uses newline-delimited JSON requests and responses. It owns one event-stream connection and short-lived or serialized request traffic as required by the protocol.

The adapter connects with a bare JSON request terminated by a newline and reads one reply line, either
`{"Ok":<response>}` or `{"Err":"<message>"}`, before any events follow. The `EventStream` reply is
`{"Ok":"Handled"}`. The event stream then delivers a **complete initial snapshot** -- every workspace on
every output -- so no separate initial query is needed and no reconciliation race exists.

The adapter projects Niri events into typed shell state:

- workspaces and active workspace per output;
- focused output, derived from the focused workspace;
- windows and focused window;
- keyboard layouts;
- overview and cast state when a feature consumes them.

Niri emits **no output event**; there is no `OutputsChanged`. Output identity, scale, mode, transform and
hotplug come from Wayland, as the source table above records. Workspace events carry an output *name*
only.

The architecture proof needs workspace state only. It must parse unknown event types without
disconnecting so a Niri update does not break the shell, and it must not extend that tolerance to the
reply line -- a discarded `Err` reply would hang the client silently.

## Services

Services publish immutable snapshots and accept commands. A service starts when the first widget or shell component needs it and stops after its last consumer leaves, unless the service has a global shell responsibility.

Initial services:

- clock from Go's `time` package;
- Niri workspaces from the event socket;
- CPU, memory, disk and network metrics from `dgop`;
- weather from Open-Meteo through `net/http`, with explicit coordinates first and automatic location later.

The shell keeps gSlapper external for wallpaper and video. Existing sysc-greet lifecycle code provides patterns for retry, scoped shutdown, and startup reconciliation.

## Plugin model

Plugins run as child processes. The host owns their surfaces and drawing.

The initial protocol uses JSON Lines over stdin/stdout or a private Unix socket. A handshake establishes protocol version, plugin identity, requested capabilities, and supported view nodes. Messages have size limits and request IDs.

Plugins may:

- publish data for a bar widget;
- describe a tree of host-owned nodes;
- receive click, scroll, toggle and command events;
- read and write namespaced settings through the host;
- request a standard popout after the host grants the capability.

Plugins may not create Wayland surfaces, access host memory, run inside the shell process, or define
executable rendering code. The host applies restart limits, timeouts, backpressure, and capability checks.

**Scope of the guarantee.** The capability model limits calls into the host. It does not provide
filesystem, network, process or D-Bus isolation: a plugin is an ordinary child process holding the shell's
own privileges. Version one treats plugins as trusted code the user chose to install, and capabilities as
a mechanism for declaring intent and auditing it -- not as a security boundary against hostile code. Any
stronger claim requires OS-level sandboxing and a separate design. State this before the protocol is
designed, because "capabilities" invites the stronger reading.

Message-size limits bound one message, not a sender. A well-formed plugin sending valid updates in a tight
loop, or one deep node tree, can still exhaust layout time and redraw bandwidth. The plugin milestone
therefore needs update-rate limits with coalescing, node-count and depth caps, a bounded inbound queue
that drops to the newest snapshot, and a layout budget that marks a plugin degraded rather than stalling
the shell.

The protocol begins after built-in widgets prove the component vocabulary. The team will not design a hypothetical schema before those widgets exist.

## Configuration

The project will define a fresh configuration format. The bar milestone needs output selection, height, edge, colors, spacing, font, and ordered left/center/right widget lists.

Configuration loading validates the complete candidate before replacing live state. A parse or validation failure keeps the previous configuration and reports a useful path and field error. Durable writes use a temporary file, `fsync`, and rename when the settings UI arrives.

## Error handling and shutdown

- Required protocol absence produces one startup error naming the missing interface.
- Output-specific failures destroy that host and keep other outputs running when protocol integrity permits it.
- Plugin failure removes or marks that widget; it does not terminate the shell.
- Service timeouts retain the last valid snapshot and expose stale/error state to the widget.
- Niri reconnection uses bounded exponential backoff and republishes a complete snapshot after reconnect.
- SIGINT and SIGTERM cancel the root context. The shell stops plugins and collectors, destroys surfaces and buffers, closes Niri connections, then disconnects Wayland.

Protocol errors that invalidate the Wayland connection terminate the process with a non-zero status. A user service manager can restart it.

## Testing and proof

Pure packages use table tests for layout, damage, hit testing, protocol decoding, plugin framing, and state projection. Renderer tests compare pixel buffers or small golden images. Fuzz tests cover external JSON and plugin messages after the schemas settle.

Live Niri gates cover:

- one and multiple outputs;
- output connect, disconnect, rotation, integer scale and fractional scale;
- exclusive-zone reservation;
- pointer input and click routing;
- idle behavior with no continuous redraw;
- clean shutdown and restart.

The architectural proof passes only when one surface combines Wayland, text, layout, input, Niri state, and event-driven rendering. A colored rectangle does not pass the gate.

## Scope estimate

These ranges guide planning rather than delivery commitments:

- architectural proof: 2,000 to 5,000 lines;
- reliable multi-output bar foundation: 10,000 to 25,000 lines;
- useful retained UI runtime with panels: 30,000 to 60,000 lines;
- broad Noctalia or DMS capability parity: at least 100,000 lines and a year-scale effort.

Niri-only support, no lock screen, and reuse of `sysc-wayland` and `dgop` reduce the scope. Text, input,
accessibility, popouts, and plugin UI still require deliberate runtime work.

## Decisions

1. The project name is `sysc-shell`.
2. Go is the primary language.
3. The first Wayland path uses `sysc-wayland`; CGO/libwayland is a fallback.
4. The first renderer uses `wl_shm`; GPU work needs profiling evidence.
5. The shell owns one bar surface per output.
6. Shell product code stays in one repository through the foundation stages; `sysc-wayland` owns the
   separately released transport and scanner.
7. Plugins run out of process and use host-rendered components.
8. Noctalia and DMS provide behavior references, not compatibility contracts.
9. Niri is the sole required compositor.
10. Lockscreen and compositor work stay out of scope.

## Open qualification gates

- Publish `sysc-wayland v0.1.0` only after its socket-pair, generator, and live Niri release gates pass.
- Prove that the pinned `sysc-wayland` release handles the required Niri registry, buffer, input, and
  shutdown paths under this long-running workload.
- Prove that `go-text/typesetting` meets the bar's shaping, font fallback, and memory needs.
- Measure shared-memory rendering before deciding whether to add EGL/OpenGL ES.
- Derive the plugin node vocabulary from built-in widgets before versioning it.
- Choose an SVG strategy for icons. Neither the standard library nor `golang.org/x/image` decodes SVG, and
  freedesktop icon themes are predominantly SVG.
- Decide whether screen-reader export through AT-SPI is in scope, and at which milestone.

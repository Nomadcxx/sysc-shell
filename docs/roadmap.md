# sysc-shell Roadmap

Date: 2026-08-26. Amended 2026-08-27 by [the plan audit](plans/2026-08-27-plan-audit-report.md).

Each milestone ends at a working gate. Later work does not enter the branch until the current gate passes on Niri,
unless the owner defers a hardware-only qualification in `bd`. A deferred gate stays open and keeps its missing
evidence; it does not turn an unrun check into a pass.

Every design, plan and handover produced so far is registered in [`plans/README.md`](plans/README.md),
including which branch each one lives on. Documents are written on milestone branches and only reach
`main` when that milestone merges, so no single branch holds the whole set.

## Milestone 0: Project foundation

Deliverables:

- approved architecture and constraints;
- prior-art assessment;
- architectural-proof implementation plan;
- development orchestration plan;
- approved `sysc-metrics`, `sysc-notify`, and `sysc-tray` boundaries and roadmaps;
- qualified `sysc-wayland v0.1.1` release;
- empty package directories on disk, without placeholder Go packages.

Exit gate:

- documents agree on scope, ownership, dependencies, and the first live proof;
- the repository has no product code or unapproved dependencies.

## Milestone 1: Architectural proof

Build one top-anchored layer surface on one selected Niri output. The surface renders a small retained tree containing shaped text, a meter, and a button. A Niri workspace event changes visible text, and a pointer click changes state.

Required behavior:

- pure-Go Wayland connection through pinned `sysc-wayland v0.1.1`;
- generated layer-shell protocol binding;
- generated fractional-scale and viewporter bindings, both treated as required;
- initial empty commit followed by configure acknowledgement;
- fractional scale carried as a numerator over 120, never as an integer factor;
- `wl_shm` buffer pool with release tracking;
- text measurement and rasterisation through the selected Go text stack;
- row layout, painting, hit testing, and action dispatch;
- frame-callback-driven redraw;
- Niri event-stream decoding;
- SIGINT/SIGTERM cleanup.

Exit gate:

- `go test ./...` and `go vet ./...` pass;
- the live proof works at scale 1 and at one non-1 scale available on the test system;
- pointer clicks route to the expected node;
- a workspace change updates the label;
- no redraw occurs while state remains unchanged;
- repeated start and stop leaves no process or socket behind.

Kill or reconsider gate:

- no `sysc-shell` implementation task starts until `sysc-wayland v0.1.1` passes fragmented-read,
  descriptor, generator, and live Niri qualification;
- stop and compare a Gio-backed renderer or GTK4 if `sysc-wayland` cannot maintain protocol correctness,
  the text stack cannot render the fixtures, or the design requires cross-goroutine Wayland access.

## Milestone 2: Stable bar on every output

Turn the proof surface into one `OutputHost` and bar per active output.

Required behavior:

- output add/remove and layer-surface close handling, driven by `wl_registry` events. Niri emits no
  output event, so Wayland is the only hotplug signal and the only source of connector identity, scale,
  mode and transform;
- output hosts keyed by `wl_registry` global name, never by connector string, so a reconnected or renamed
  connector cannot produce a duplicate host;
- top, bottom, left, and right anchors in the model, with top as the first supported configuration;
- integer and fractional scale handling, including a scale change that arrives with no accompanying
  layer-surface configure;
- output transform handling;
- exclusive-zone reservation;
- left, center, and right bar sections;
- opaque and transparent input-region correctness;
- text truncation and font fallback;
- full redraw first, then tested rectangle damage;
- configuration load and validated reload;
- one process and one Wayland dispatch owner.

Exit gate:

- every connected output receives exactly one configured bar;
- hotplug and unplug reconcile hosts without restarting the shell;
- changing scale or mode does not leave stale buffers or wrong hit regions;
- Niri windows respect the configured exclusive zone;
- a 60-minute idle run shows no continuous frame loop when reduced motion is on or no looping paint is visible;
- a reconnect/restart test restores all bars.

## Milestone 3: Built-in widget foundation

Use first-party widgets to discover the component vocabulary before publishing a plugin protocol.

Initial widgets:

- clock and date;
- Niri workspaces and focused window title;
- CPU and memory;
- filesystem capacity, block-device and network rates;
- battery state and remaining time;
- weather through Open-Meteo.

Work:

- pin and import `sysc-metrics` after its core and power gates pass;
- add consumer-counted service lifetimes;
- add icon, meter, graph, tooltip, stale-data, and error states only as widgets need them;
- share each service snapshot across output-specific widget instances;
- set update intervals by data type and power cost;
- add accessible names and keyboard activation for interactive widgets;
- decide the icon decoding strategy before the first icon ships. PNG, JPEG and GIF come from the standard
  library and WebP, TIFF and BMP from `golang.org/x/image`; **SVG has no decoder in either**, and icon
  themes are predominantly SVG.

Exit gate:

- widgets on multiple outputs share collectors instead of duplicating work;
- removing a widget releases its service reference;
- unavailable sensors or network services degrade without removing the bar;
- visual screenshots pass the agreed theme and spacing review;
- idle CPU, update CPU, RSS, redraw count, and binary size have recorded baselines.

## Milestone 4: Panels and standard controls

Add separate panel surfaces and enough controls for useful shell interaction.

Candidate components enter only with a consumer:

- scroll area and virtual list;
- toggle, slider, menu, text field, tabs, and graphs;
- anchored popout placement with output-edge clamping;
- keyboard focus, escape-to-close, and focus restoration;
- settings panels for bar and widget configuration.

Exit gate:

- clock/calendar or system-monitor popout works on each output;
- only the open panel requests keyboard focus;
- a panel never changes the bar's exclusive zone;
- placement remains within transformed and scaled output bounds;
- keyboard-only interaction covers every shipped control;
- every interactive node carries an accessible name and role;
- reduced-motion and high-contrast preferences change behavior rather than being ignored.

Accessibility becomes an acceptance gate at this milestone, because this is where panels first request
keyboard focus and ship interactive controls. Screen-reader export through AT-SPI is a separate decision
with its own D-Bus subsystem; it is not part of this gate.

## Milestone 5: Notifications and system tray

Integrate the two independent D-Bus services after bars, panel surfaces, menus, image handling, keyboard
focus, and accessibility have owners in the shell runtime.

Notification work:

- qualify and pin `sysc-notify`'s `org.freedesktop.Notifications` implementation;
- connect through bounded, versioned Unix-socket IPC and recover a current snapshot after reconnect;
- render popup surfaces with actions, images, progress, urgency, expiry, and dismiss behavior;
- resolve sender PID lineage against cached Niri windows and focus only one unambiguous match;
- add `xdg_activation_v1` tokens later, then advertise that capability.

Tray work:

- qualify and pin `sysc-tray`'s watcher, host, item, and DBusMenu implementation;
- connect through bounded, versioned Unix-socket IPC and recover item state after reconnect;
- render normal, attention, and overlay icon state in the bar;
- render keyboard-accessible DBusMenu surfaces within the active output;
- survive item, watcher, bus, service, and shell restarts without duplicate registrations.

Exit gate:

- notification replacement, actions, close reasons, expiry, shell restart, and ambiguous focus pass;
- tray registration, property updates, activation, scrolling, menus, owner replacement, and restart pass;
- malformed or oversized D-Bus data can remove only its source item or notification request;
- neither service imports Wayland or owns presentation;
- shell absence does not block `Notify` or discard valid tray registration state within documented bounds.

The repository-specific M0 gates must settle exact protocol and compatibility behavior before product code
starts. `sysc-shell` consumes tagged releases, not unreviewed local replacements.

## Milestone 6: External widget and plugin host

Version the component vocabulary proven by built-in widgets and expose it to supervised processes.

Required behavior:

- manifest validation and protocol handshake;
- version negotiation and capability grants;
- JSON Lines framing with message and tree limits;
- view snapshot, incremental update, and input-event messages;
- namespaced settings and state;
- process restart budget, cancellation, timeout, and stderr capture;
- placeholder/error UI after plugin failure;
- per-output widget instances backed by shared plugin state when requested.

Exit gate:

- an example plugin supplies a clock-like widget and a standard popout without linking to the shell;
- malformed, oversized, slow, and crashed plugins cannot crash or block the shell;
- protocol compatibility tests pin version-one behavior;
- disabling a plugin removes its nodes and child process;
- a plugin sending well-formed updates in a tight loop, or one deep node tree, cannot exhaust CPU,
  memory, layout time or redraw bandwidth. Message-size validation bounds one message, not a sender, so
  this gate needs update-rate limits with coalescing, node-count and depth caps, a bounded inbound queue
  that drops to the newest snapshot, and a layout budget that marks a plugin degraded;
- the design states plainly whether capabilities are a security boundary. As designed they are not: a
  plugin is an ordinary child process with the shell's privileges.

## Milestone 7: Shell breadth

Add features in vertical slices. Each slice owns its service, state projection, components, surfaces, actions, tests, and failure behavior.

Candidate order:

1. launcher and application search;
2. OSDs and richer notification history;
3. clipboard history;
4. control center, network, Bluetooth, audio, brightness, and power actions;
5. media controls;
6. wallpaper control through external gSlapper and static wallpaper providers;
7. desktop widgets and richer plugin surfaces.

No slice may import a full DMS or Noctalia subsystem without a dependency and ownership review.

Notifications and tray have dedicated repositories and Milestone 5 gates. Two later D-Bus subsystems still
need designs before entering a branch: MPRIS player discovery and position handling, and AT-SPI if the
project adopts screen-reader export.

The Wayland protocols these slices need are all present on Niri 26.04 and require no new negotiation
design: `ext_data_control_manager_v1` and `zwlr_data_control_manager_v1` for clipboard history,
`zwp_primary_selection_device_manager_v1`, `zwlr_screencopy_manager_v1`, `xdg_activation_v1`,
`ext_foreign_toplevel_list_v1`, `zwlr_foreign_toplevel_manager_v1`, `ext_idle_notifier_v1` and
`zwp_idle_inhibit_manager_v1`. Two are worth adopting earlier than their slice: `wp_cursor_shape_manager_v1`
sets a pointer cursor without shipping cursor bitmaps, and `zwlr_output_manager_v1` is the path for any
output configuration feature.

The wallpaper slice consumes gSlapper's existing control socket. Decide now that the shell does **not**
own gSlapper's lifecycle by default, so a wallpaper feature stays a socket client instead of growing a
process-supervision subsystem.

## Milestone 8: Rendering qualification

Keep `wl_shm` when it meets the measured budgets. Add EGL/OpenGL ES only for a named failing case such as animation frame time, large blurred panels, image-heavy grids, or unacceptable CPU/power use.

"Large blurred panels" is measured and resolved in favour of `wl_shm`. On this machine's 3440x1440 output the screencopy readback is 5.7 ms and the reduced-resolution blur 5.3 ms for the worst regular panel: about 11 ms for one open, against a 16.67 ms frame, and paid once per open rather than per frame.

"Animation frame time" and "image-heavy grids" are measured too, on 2026-09-13. An animating surface publishes 21 times a second at the 33 ms frame cap against 62 uncapped, while the 16 ms tick still advances the animation from the clock and the settling frame always paints. The wallpaper picker stopped rebuilding its panel tree for every decoded thumbnail; that rebuild measured 0.53 to 0.65 ms over five runs on a five-entry root and grows with the visible tile count. Rectangle damage exists but no surface has adopted it, so every surface still damages its whole buffer.

Only "unacceptable CPU/power use" is still unmeasured. It needs the 60-minute idle observation, so `sysc-202` stays open rather than being retired on four cases of which three are answered.

If GPU work starts:

- retain the UI tree and layout engine;
- add the second renderer beside the working shared-memory renderer;
- compare pixel output and damage behavior;
- keep the software renderer for tests and recovery when practical;
- document driver, scale, and output combinations used for qualification.

The renderer milestone does not expand shell features.

## Milestone 9: User control of the shell

Give the user direct control over what the shell does and how it is arranged. This milestone is a
programme of five sub-projects, each with its own design, plan, and implementation cycle. Sub-projects
do not merge into one another.

| | Sub-project | State |
|---|---|---|
| A | Settings foundation: schema, controls, surface, apply contract | Designed — `plans/2026-09-15-settings-foundation-design.md` |
| B | Bar composition: widget add, remove, reorder, per-widget options | Designed — `plans/2026-09-15-bar-composition-design.md` |
| C | Bar geometry and placement: edge, auto-hide, exclusive zone, per-output overrides | Not designed |
| D | Surfaces and behaviour: notification, OSD, launcher, and panel settings | Not designed |
| E | New subsystems: night light, idle behaviour, screenshot, hooks, keybinds, dock | Not designed |

Clipboard history is tracked separately as `sysc-205` and is not part of sub-project E.

Driver: the shell models twelve configuration domains and exposes five. Bar widget arrangement is
reachable only as a comma-separated string in a single settings field, and `top` is the only
implemented bar edge — `bottom`, `left`, and `right` are rejected at load.

Exit gate:

- every `config.Config` domain is reachable from the settings surface;
- a user can add, remove, and reorder bar widgets without editing a file;
- a change made in the user interface survives a configuration reload that arrives from outside it;
- a setting reset returns the value its own writer rule treats as the default;
- the live Niri check passes for the settings surface and the bar editor.

## Milestone 10: Shell surface polish

Make the existing Niri shell read as one coherent product at bar and control-centre size while keeping
state, I/O, and protocol ownership in the services that already provide them. M10 follows the
surface-stacking integration and is split into seven registered designs with four execution owners:
`sysc-314` for the centre composition, `sysc-315` for notifications and battery, `sysc-316` for
service-owned tray termination and its upstream qualification, and `sysc-317` for integration and the
exit gate. Existing owners remain responsible for metrics, weather, launcher, and Network defects.

Deliverables:

- four truthful Home system gauges and one shared, diagnosable GPU snapshot for the bar and System
  Monitor;
- the weather icon/value row, the supplied nested-gates PNG launcher mark, and number-free Niri
  workspace shapes;
- one time/date composition with optional media at its right edge and a fixed-centre SYSC wordmark;
- theme-derived low, normal, and critical notification treatment plus one hysteretic battery warning;
- service-owned tray termination with current-identity validation and state-arrival acknowledgement;
- the existing NetworkManager-backed page as a functional Control Centre destination;
- affected-package checks, upstream protocol qualification where required, and a bare-metal Niri
  completion snapshot.

Exit gate:

- all seven designs have executable plans and their focused checks pass;
- displayed metrics and weather values come from valid service state, with truthful unavailable and
  failure states and no fabricated zeroes or duplicate collectors;
- the launcher uses `/home/nomadx/Pictures/sysc-aperture-c-nested-gates.png` as the project-owned asset,
  and the workspace tree paints no workspace number;
- the wordmark remains at the content-band centre when clock/date widths, media presence, titles, and
  side-section widths vary;
- battery and tray operations use qualified service releases, and tray Close success follows the
  matching service delta rather than a hidden-only row removal;
- the Network rail routes to the existing service/body without a second connection or credential path;
- `gofmt`, `go vet ./...`, the required race checks, module-diff and whitespace checks are recorded;
- the live Niri result on DP-1 at 3440×1440, scale 1.0, plus every unavailable second-output,
  laptop, hardware, or disposable-fixture check is recorded in the completion handover.

## Deferred work

- compositor support beyond Niri;
- session lock and lock-screen UI;
- arbitrary plugin rendering or scripting;
- mobile/touch-first layouts;
- a standalone public UI toolkit;
- splitting Wayland, rendering, or plugin packages into separate repositories.

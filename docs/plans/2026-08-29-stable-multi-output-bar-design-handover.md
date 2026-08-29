# Stable Multi-Output Bar Design Review Handover

Date: 2026-08-29

## Purpose

This handover starts the next `sysc-shell` activity: the Milestone 2 design review for one stable bar on
every connected Niri output. The receiving agent must settle the contracts, obtain owner approval, and
then write an executable implementation plan. Do not start bar implementation while those contracts
remain open.

The milestone turns the single-output architectural proof into one process with one Wayland dispatch
owner and one `OutputHost` per active `wl_output`. Each host owns an independent layer surface, render
scheduler, fractional-scale object, viewport, and buffer generations.

## Repository state

| Item | State |
|---|---|
| Repository | `/home/nomadx/sysc-shell` |
| Branch | `main` |
| Local HEAD | `bcd59ad02b2b3b200f6fbcdcf33a8e87ebdee57a` |
| Remote | `https://github.com/Nomadcxx/sysc-shell.git` |
| Remote state at handover | local `main` is 25 commits ahead of `origin/main` |
| Working tree at handover | clean |
| Milestone branch/worktree | merged and removed |
| `proof-v0` tag | not created |
| Required Wayland foundation | `github.com/Nomadcxx/sysc-wayland v0.1.1` |

Do not assume the 25 local commits have been pushed. Check the branch and remote state before creating a
worktree or tag. Do not create `proof-v0` until the deferred live proof checks below pass or the owner
explicitly changes the tag gate.

Read these documents before proposing a design:

- `AGENTS.md`;
- `docs/roadmap.md`, especially Milestone 2;
- `docs/plans/2026-08-26-sysc-shell-design.md`;
- `docs/plans/2026-08-26-development-orchestration.md`;
- `docs/plans/2026-08-27-plan-audit-report.md`;
- `docs/plans/2026-08-28-architectural-proof-review-fixes.md`;
- `tests/integration/README.md`.

## Architectural-proof state

The architectural proof is merged. Its automated gate passes on `main`:

```bash
go mod tidy -diff
go generate ./internal/platform/wayland/xdgshell
go generate ./internal/platform/wayland/layershell
go generate ./internal/platform/wayland/fractionalscale
go generate ./internal/platform/wayland/viewporter
git diff --exit-code
go test -race -count=1 ./...
go vet ./...
go build ./cmd/sysc-shell
```

The available live checks covered mapping, idle rendering, repeated restart, SIGINT, and SIGTERM. Two
proof checks remain deferred because the last session ran over SSH while the spare output was disconnected:

1. click the proof button with a physical pointer;
2. run the proof at a non-1 scale on a spare or non-focused output.

The installed `ydotool` device did not reach Niri's seat. Do not count that synthetic attempt as pointer
evidence. Run the deferred checks when local input and a suitable output are available. They do not block
the Milestone 2 design review, but they remain open before final hardware qualification.

## Product constraints

- Linux and Niri only.
- Go owns shell state, Niri IPC, layout, rendering, configuration, and interaction.
- Do not add C++, Rust, Lua, Luau, Qt, QML, Quickshell, a compositor, or a lock screen.
- Keep every Wayland proxy on one owner goroutine.
- Keep `wl_shm` unless measurements prove it misses a named budget.
- Do not add a renderer interface while `wl_shm` is the only renderer.
- Use built-in Go widgets first. Do not design the external plugin protocol in this milestone.
- Keep shell presentation in `sysc-shell`; do not move it to `sysc-wayland` or a new repository.
- Do not add notification, tray, weather, metrics, panels, popouts, OSDs, or GPU work to Milestone 2.
- Add a UI primitive only when the stable bar requires it.
- Preserve the pinned dependency and generated-protocol provenance.

## Owner-supplied visual baseline

Use the following DMS parity values as the design starting point:

- nominal bar height: 48 logical pixels;
- exclusive zone: 44 logical pixels;
- separation or spacing budget: 4 logical pixels;
- left, center, and right sections on every output.

The proof currently requests a 48-pixel surface and a 48-pixel exclusive zone. The design must define how
the 48/44/4 values map to layer size, margins, exclusive zone, input region, and content bounds. Do not
copy DMS geometry without explaining the Wayland requests. Confirm whether the 4 pixels form an outer
gap, an internal content inset, or another measured relationship.

The center section needs an explicit collision rule. State whether it remains geometrically centered when
left and right widths differ, which section truncates first, and what happens on a narrow output. Use
named theme tokens for height, spacing, background, foreground, accent, muted, error, radius, and text
sizes. Do not copy Noctalia or DMS pixels beyond the owner-supplied baseline.

Milestone 2 must prove the three-section layout without pulling Milestone 3 widgets forward. Choose a
small fixture from existing proof content or other fixed first-party nodes. The design must name that
fixture and explain how it exercises alignment, truncation, hit testing, and redraw without becoming a
widget framework.

## Fixed ownership and data flow

Wayland owns outputs. Niri owns workspaces.

| State | Source and ownership |
|---|---|
| Output existence | `wl_registry.global` and `global_remove` for `wl_output` |
| Host identity | registry global name, `uint32` |
| Connector name | `wl_output.name`, used as a configuration and Niri-join attribute |
| Mode, transform, physical data | `wl_output.geometry`, `mode`, and `done` |
| Render scale | `wp_fractional_scale_v1.preferred_scale`, per bar surface |
| Logical bar size | layer-surface `configure` |
| Workspace state | Niri event stream, keyed by connector name |

Never key a host by connector string. A connector can disappear and return under a new registry global.
Create and remove hosts only from Wayland lifecycle events. Hold unmatched Niri workspace state by
connector and join it when a named host exists.

The required top-level shape is:

```text
Wayland owner goroutine
├── shared globals and seat
├── outputs map[uint32]*OutputHost
│   ├── wl_output metadata
│   └── BarSurface
│       ├── wl_surface + layer role
│       ├── fractional-scale + viewport
│       ├── scheduler
│       └── current and retiring buffer generations
└── shared immutable shell/Niri state
```

One process per output and one Wayland goroutine per output are rejected. They duplicate shared state,
break the existing ownership invariant, and make hotplug and shutdown harder.

## Current code that must change

The proof code is deliberately singular:

- `internal/platform/wayland/client.go` stores one selected output, surface, layer role, viewport,
  fractional-scale object, scheduler, pointer-focus flag, and buffer-generation set on `owner`;
- `bindGlobals` binds only the outputs present during startup;
- the registry global handler records later outputs but does not bind them or create hosts;
- `global_remove` closes the whole proof when the selected output disappears;
- pointer routing compares events with one surface;
- `internal/shell/proof.go` owns one fixed one-level render tree;
- `cmd/sysc-shell/main.go` connects one `Proof` to one Wayland surface.

Do not duplicate `owner` once per output. Move per-surface state into the smallest coherent host type and
leave shared display, registry, compositor, shm, seat, layer-shell manager, scale manager, viewporter, and
polling state on the owner.

The design should compare these approaches:

1. **Recommended: one owner plus an `OutputHost` map.** Extract the current surface state into a host,
   route registry and surface events by global/object identity, and keep all proxy calls on the owner.
2. **Independent per-output clients.** Simpler local structs but multiple Wayland connections, duplicate
   Niri state, and harder atomic reload/shutdown. Reject unless evidence overturns the one-owner invariant.
3. **One fullscreen surface spanning outputs.** Fewer surfaces but wrong lifecycle, scale, anchoring,
   exclusive-zone, and input semantics. Reject.

## Design questions the next agent must answer

### Output discovery and host lifecycle

1. When does an advertised `wl_output` become ready for a bar: after `name`, after `done`, or after a
   complete minimum metadata set?
2. How does the owner bind an output announced after initial roundtrips without dispatching from another
   goroutine?
3. What exact state machine covers discovery, name arrival, role creation, initial empty commit,
   configure, mapping, scale-only reconfigure, layer close, global removal, and reconnect?
4. How are late events for a removed output or destroyed surface ignored safely?
5. If a layer surface closes while its `wl_output` remains, does the shell recreate it, leave the host
   degraded, or terminate? Define retry limits and the error boundary.
6. Which host failure is local, and which protocol failure terminates the shared connection?
7. How does shutdown destroy all host children, then shared globals, then flush and close once?

### Scale, transform, and buffers

8. Preserve logical surface size, `scale120`, and physical buffer size as separate values. Show the
   calculation and overflow checks for each host.
9. Confirm how Niri applies `wl_output.transform` to a layer surface and whether the client must set a
   buffer transform. Do not infer this from mode dimensions.
10. Prove that a scale-only event retires only that host's buffer generation and does not redraw other
    outputs.
11. Define generation and frame-callback ownership when an output disappears during a pending frame.
12. Decide whether layer-shell v4 remains sufficient or whether v5 `set_exclusive_edge` is required for
    multi-edge support. If v5 enters, qualify Niri behavior and update the bind cap deliberately.

### Pointer and input regions

13. Replace the single `pointerInside` state with focus that identifies the entered surface/host and keeps
    the latest logical coordinates.
14. Define pointer behavior when the focused surface is destroyed or the seat loses pointer capability.
15. Define opaque and transparent input regions. The bar must not consume clicks in pixels that the
    design declares click-through.
16. Keep hit testing in logical coordinates and use the same arranged bounds that painting uses.

### Bar layout and rendering

17. Specify measurement and arrangement for left, center, and right sections, including true centering,
    overlap, priority, clipping, and truncation.
18. Decide whether a bar-specific three-section layout is enough. Do not build a general constraint
    engine without another consumer.
19. Name the minimal node additions needed by the fixture. Existing proof nodes should remain where they
    fit.
20. Define the immutable render snapshot for each host and the shared model data it references.
21. Start with full redraw. Add rectangle damage only after tests cover old and new bounds, scale
    conversion, clipping, and reconfigure.

### Text

22. Use the pinned `go-text/typesetting/fontscan` support for system scanning, family/aspect matching, and
    per-rune fallback. Define the cache directory, bounded face cache, run splitting, and failure fallback.
23. Define truncation before implementation: end ellipsis, available-width measurement, fallback-face
    boundaries, and behavior when even the ellipsis does not fit.
24. Keep paragraph bidi, color emoji, and input methods out unless the selected Milestone 2 fixture
    requires them.

### Configuration and reload

25. Define a fresh configuration schema for output matching, bar height, exclusive zone, edge, theme
    tokens, font, spacing, and ordered left/center/right items. No Noctalia, DMS, QML, or Quickshell
    compatibility is required.
26. Match output overrides by connector attribute while retaining registry global name as runtime host
    identity.
27. Parse and validate a complete candidate before replacing live state. A bad reload keeps the last valid
    configuration and reports the exact field path.
28. Prefer an explicit reload trigger such as SIGHUP for the first implementation. Add inotify only if the
    owner requires automatic file watching; do not add a watcher dependency for its own sake.
29. Define which changes update a mapped host in place and which require role or surface recreation.
30. Define transaction ordering so a reload cannot leave half the outputs on old policy and half on new
    policy after validation succeeds.

## Required tests and live matrix

The design must map every contract to a check before the implementation plan is approved.

Pure tests must cover:

- host creation exactly once per registry global;
- connector rename or reconnect without duplicate hosts;
- Niri state arriving before host creation and after host removal;
- output removal and layer close during pending configure, frame, and buffer release;
- mixed output scales and scale-only reconfigure;
- transform and logical/physical dimension handling;
- pointer focus switching between surfaces and focused-surface destruction;
- three-section layout on wide, narrow, asymmetric, empty, and overflowing content;
- font fallback and truncation at scale 1 and fractional scale;
- input-region construction;
- valid configuration load, rejected candidate, and successful reload;
- no redraw on unrelated output or unchanged state;
- complete child-to-parent shutdown across several hosts.

The Milestone 2 live matrix is:

- one output;
- two or more outputs;
- physical hotplug and unplug without restart;
- reconnect under a new registry global;
- one transformed output;
- mixed scales, including a non-1 scale;
- scale or mode change while mapped;
- exclusive-zone verification with Niri windows;
- physical pointer routing on at least two bars;
- validated config reload with all bars present;
- restart restoring one bar per output;
- 60-minute idle run with no continuous frame loop.

Keep machine-specific logs outside Git. Store reusable commands and required observations in
`tests/integration/README.md`.

## Design gate and deliverables

The receiving agent must produce these artifacts in order:

1. `docs/plans/2026-08-29-stable-multi-output-bar-design.md` with user-visible behavior, ownership, state
   machines, data flow, protocol versions, error policy, shutdown, configuration schema, tests, hardware
   matrix, non-goals, and stop conditions.
2. An owner-review section listing unresolved decisions and the recommended choice for each.
3. Owner approval of the design.
4. `docs/plans/2026-08-29-stable-multi-output-bar.md`, an executable TDD implementation plan with exact
   files, failing checks, commands, expected results, and commit boundaries.

Do not combine the design and implementation plan. The design settles contracts; the plan sequences
work against them.

The design review should stop and ask the owner before introducing:

- a new dependency or configuration parser;
- layer-shell v5 as a requirement;
- a new goroutine owner;
- a new surface kind beyond the bar;
- an exported package or repository split;
- CGO or a graphics backend;
- an automatic file watcher;
- a generalized UI abstraction whose only consumer is the bar.

## Recommended implementation sequence after design approval

The design may adjust this sequence, but it should preserve dependency order:

1. Extract per-surface state into one host without changing single-output behavior.
2. Add a pure registry-to-host reconciliation model and lifecycle tests.
3. Create one startup bar for every ready output.
4. Add live output bind, hotplug, unplug, and reconnect.
5. Add per-host metadata, transform, mixed-scale, and reconfigure behavior.
6. Route the shared seat and pointer across bar surfaces.
7. Add theme tokens and the three-section bar layout.
8. Add system font selection, fallback, and truncation.
9. Add configuration load and validated reload.
10. Add tested input regions and rectangle damage.
11. Run local review, the full live matrix, performance capture, and milestone handoff.

After the design fixes package contracts, output lifecycle, bar layout/text, and configuration parsing can
use separate worktrees. The integration owner merges them in dependency order. Do not parallelize two
tasks that both need to discover the `OutputHost` API.

## Non-goals for Milestone 2

- notification daemon or popup UI;
- system tray or DBusMenu;
- metrics, weather, battery, audio, or MPRIS services;
- external plugin protocol;
- launcher, control center, panels, wallpaper, OSDs, clipboard, or lock screen;
- SVG icon strategy;
- GPU rendering;
- compositor portability;
- AT-SPI, keyboard navigation, and panel focus behavior, which enter at the approved later milestone.

## Stop or reconsider conditions

Stop and return to owner review if:

- correct multi-output ownership requires Wayland proxy calls from more than one goroutine;
- the one-owner event loop cannot express safe hotplug and teardown without transport changes;
- transform or mixed-scale behavior cannot be stated in logical and physical units with a testable rule;
- rectangle damage expands scope before full redraw meets measured needs;
- configuration reload requires a framework or service not justified by the milestone;
- the pure-Go text stack fails the selected fallback/truncation fixture or a measured resource budget;
- a proposed abstraction has only one use and does not reduce lifecycle complexity.

If a transport or generated-binding defect appears, fix and release it in `sysc-wayland`; do not work
around it inside shell policy code.

## Resume commands

Start read-only:

```bash
cd /home/nomadx/sysc-shell
git status --short --branch
git log -5 --oneline --decorate
git remote -v
git tag --list
go test -race -count=1 ./...
go vet ./...
rg -n "Milestone 2|OutputHost|hotplug|left, center, and right" docs internal
```

Before writing product code, confirm that the owner approved the design and that an implementation plan
exists. Create implementation work only in a dedicated `milestone/stable-bar` worktree. Keep `main` for
accepted gates and documentation.

## Handoff completion condition

The receiving agent completes this handover when the owner has approved the stable-bar design and the
repository contains an audited implementation plan. Implementation is the following activity, not part of
this design-review handover.

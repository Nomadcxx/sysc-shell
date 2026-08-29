# Built-in Widget Foundation Execution Handover

Date: 2026-08-30

## Purpose

This handover starts Milestone 3 without competing with the Milestone 2 correction branch. The receiving
agent owns the Milestone 3 design and executable implementation plan first. Product code starts after the
owner approves those documents and the Milestone 2 state, reload, identity, rendering, and invalidation
corrections have merged.

The first code slice is clock/date plus Niri workspace and focused-window text. It must prove shared
service ownership and output-specific widget instances without creating a general UI toolkit or plugin
protocol.

## Repository state

- Repository: `/home/nomadx/sysc-shell`
- Remote: `https://github.com/Nomadcxx/sysc-shell.git`
- Accepted local `main`: `296b0eb`
- Milestone 2 worktree:
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/stable-bar`
- Milestone 2 branch: `milestone/stable-bar`
- Milestone 2 reviewed baseline: `57b49f0`
- `sysc-metrics` repository: `/home/nomadx/sysc-metrics`
- `sysc-metrics/main`: `d821afe`, clean and pushed to `origin/main`
- `sysc-metrics` release tags: none as of 2026-08-30

Milestone 2 still has review corrections in progress. It also needs the live Niri matrix, which cannot run
in the current SSH session. Do not modify its progress handover. Read the Milestone 2 worktree for current
contracts, but do not edit it or branch product code from its uncorrected commit.

## Safe parallel work now

Create a documentation worktree from `main` and keep it separate from the Milestone 2 worktree:

```bash
cd /home/nomadx/sysc-shell
git worktree add \
  /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation \
  -b milestone/widget-foundation main
```

In that worktree, produce these two documents:

1. `docs/plans/2026-08-30-built-in-widget-foundation-design.md`
2. `docs/plans/2026-08-30-built-in-widget-foundation.md`

Use `superpowers:brainstorming` for the design and obtain owner approval. Then use
`superpowers:writing-plans` for the implementation plan. The implementation plan must begin with the
required `superpowers:executing-plans` header, name exact files and tests, use TDD, and include small
commit boundaries.

After Milestone 2 merges, bring the widget branch onto the new `main` before writing product code. Do not
merge Milestone 3 until Milestone 2 passes its live Niri gate.

## Read these first

Read the files in this order:

1. `docs/roadmap.md`, Milestones 2 and 3.
2. `docs/plans/2026-08-26-sysc-shell-design.md`, especially UI runtime, Niri integration, services,
   project decisions, and open qualification gates.
3. `docs/plans/2026-08-26-development-orchestration.md`, especially design, dependency, review,
   performance, hardware, and handoff gates.
4. `docs/plans/2026-08-27-plan-audit-report.md`, especially font fallback, SVG decision D3, and service
   lifetime findings.
5. `docs/prior-art.md`, the Noctalia v5, DMS, and dgop sections.
6. `docs/plans/2026-08-29-stable-multi-output-bar-design-handover.md` for the bar geometry and ownership
   contracts. Treat its implementation status as historical.
7. Current code under `internal/shell`, `internal/ui`, `internal/render`, `internal/platform/niri`, and
   `internal/config` in the Milestone 2 worktree.
8. `/home/nomadx/sysc-metrics/docs/plans/2026-08-27-sysc-metrics-design.md`,
   `/home/nomadx/sysc-metrics/docs/roadmap.md`, and
   `/home/nomadx/sysc-metrics/docs/plans/2026-08-29-core-counters-merge-handover.md`.
9. The current `sysc-metrics` public API in `/home/nomadx/sysc-metrics/metrics.go` and exported Linux
   collectors in that repository.

The metrics merge handover predates the final audit fixes and push. Use commit `d821afe` as the current
source state, and verify it rather than copying the handover's old merge hash.

## Fixed project decisions

The Milestone 3 design must preserve these decisions:

- Linux and Niri are the only platform contract.
- Go owns the shell. Do not add C++, Rust, Lua, Luau, Qt, QML, or Quickshell.
- `sysc-shell` owns every Wayland surface and all presentation.
- One goroutine owns the Wayland connection and every Wayland proxy.
- Keep the `wl_shm` renderer. A GPU backend requires measured evidence and a design revision.
- Keep one bar instance per Wayland output global. A connector name joins configuration and Niri state;
  it is not host identity.
- Use first-party widgets to discover the retained-node vocabulary before publishing a plugin protocol.
- Keep services process-scoped and share immutable snapshots across output-specific widget instances.
- `sysc-metrics` owns Linux collection, units, timestamps, deltas, and partial source errors.
  `sysc-shell` owns polling, sharing, formatting, stale state, and presentation.
- Use system fonts with per-rune fallback. Focused-window titles are the first unbounded user text and
  must use the corrected Milestone 2 shaping, truncation, and fallback path.
- Add UI primitives only when a shipped widget consumes them.
- Author project-owned SVG source icons and commit runtime PNG or alpha-mask assets at the required
  sizes. Milestone 3 does not need a runtime SVG decoder.
- Preserve the default bar geometry: nominal height 48, gap 4, painted body 40, layer surface and
  exclusive zone 44, radius 12.
- Do not build configuration compatibility with Noctalia, DMS, QML, or Quickshell.

## Recommended milestone shape

Split Milestone 3 into four reviewed tranches. The first tranche is the current assignment.

### Tranche 3A: text widgets and service ownership

Ship:

- clock and date text;
- Niri workspace text per output;
- focused-window title per output;
- shared service lifetime and snapshot delivery needed by those widgets;
- configuration entries for the shipped widget IDs;
- stale and unavailable text states only where a real source can fail;
- automated lifetime, projection, layout, reload, and redraw tests;
- live checks on at least two outputs after Milestone 2 passes.

Keep the first slice read-only. Do not add workspace activation, popouts, keyboard focus, tooltips,
icons, graphs, or animation. Read-only widgets avoid inventing Milestone 4 keyboard-focus behavior.

### Tranche 3B: core metrics

After a reviewed `sysc-metrics` release exists, add CPU, memory, filesystem capacity, block-device rate,
and network-rate consumers. One process-level polling owner must feed every configured output. The shell
must not create one sampler or polling goroutine per bar.

### Tranche 3C: power

Wait for `sysc-metrics` M2 power and thermal gates. Add battery state and remaining time only after
UPower loss, sysfs fallback, source transition, multi-battery, suspend/resume, and missing-service tests
pass in that repository.

### Tranche 3D: weather and visual vocabulary

Use Open-Meteo through `net/http` with explicit coordinates first. Add automatic location only after a
separate privacy and failure-policy decision. Add project-owned icon assets under the policy below. Add
meter, graph, tooltip, stale-data, and error nodes only when a widget in this tranche needs them.

## Design work required for Tranche 3A

The design must answer these questions before the implementation plan names APIs.

### Widget ownership and view construction

Compare these approaches:

1. Concrete widget structs that build existing `ui.Node` values. This is the recommended starting point.
2. A small internal widget interface, but only if clock and Niri widgets need the same lifecycle methods
   and the interface removes real duplication.
3. A declarative or plugin-shaped widget schema. Reject this for Tranche 3A because no external consumer
   exists and the retained-node vocabulary is still changing.

State which object owns each output-specific widget instance, how a configuration reload stages a
replacement set, and how an old output global can disappear without deleting a reconnecting host that
shares its connector name.

### Service lifetime

Define the exact first-consumer and last-consumer behavior. A candidate configuration must acquire its
needed service references before the old widget set releases them, so an accepted reload does not stop
and restart a still-used service. A rejected reload must leave service counts and goroutines unchanged.

Do not add a generic service registry, dependency-injection container, or single-implementation
interface. Start with concrete ownership. Extract a common lease helper only after two services prove the
same start/stop rules.

The existing Niri event stream may remain a global shell responsibility because the shell already owns
one process-wide connection. The design must say whether it runs without Niri widgets and why. Consumer
counting still needs proof through a service that can become unused, such as the clock or later metrics
poller.

### Clock scheduling

Use Go's `time` package. Align updates to the next displayed boundary rather than relying on an
ever-drifting ticker. The service should sleep when no widget consumes it, publish one immutable snapshot
to all clock/date instances, coalesce slow consumers, and stop promptly on context cancellation.

The design must state the configured formats, timezone behavior, and update boundary. Prefer the current
local timezone and Go layout strings unless the owner requests another contract. Avoid locale and
calendar frameworks in this tranche.

### Niri window state

Extend the existing event stream instead of opening another connection or shelling out to `niri msg`.
Research the Niri 26.04 event schema for at least:

- `WindowsChanged` initial state;
- `WindowOpenedOrChanged` if emitted by the supported version;
- `WindowClosed`;
- `WindowFocusChanged`;
- workspace activation or window movement that changes the title shown on an output.

Record exact wire fields and event ordering in the design. Use synthetic fixtures in Git so tests do not
store real window titles or machine-specific data. Preserve unknown-event tolerance and treat malformed
known events as stream errors without publishing partial state.

The output projection must join a window to the workspace's connector name, then update only bars whose
visible workspace or focused-window title changed. Wayland global ID remains the bar identity.

### Configuration

Propose the smallest validated schema for the four first-slice widget IDs. Decide how the Milestone 2
fixture IDs (`shell-name`, `workspace`, `meter`, and `toggle`) leave the defaults. There is no published
configuration compatibility promise, but invalid or misspelled IDs must fail the whole candidate and
leave the live widget and service sets unchanged.

Do not add per-widget free-form maps, reflection-driven decoding, or a configuration library. Extend the
existing standard-library JSON loader and exact-field validation.

### Invalidation and redraw

Trace one clock tick and one Niri title change from source snapshot to affected widget, retained nodes,
connector/global mapping, scheduler invalidation, and frame submission. Multiple source updates may
coalesce, but the latest visible state cannot be dropped. No state change means no submitted frame.

The design must account for output overlap during reconnect and configuration reload while two globals
temporarily share one connector.

## Dependency gates

### `sysc-metrics`

`sysc-metrics` at `d821afe` contains standard-library-only collectors for CPU, memory, swap, uptime,
filesystem capacity, block counters/rates, and network counters/rates. It has no release tag. Do not add a
local `replace`, copy its code into the shell, or import an untagged commit.

Before Tranche 3B:

1. audit the current public API and race gate;
2. qualify it from one proposed shell consumer;
3. tag and push the approved release;
4. pin that tag in `sysc-shell`;
5. record license, version, package boundary, and fork/removal condition in the Milestone 3 design.

The current exported entry points are `NewCPUSampler`, `ReadMemory`, `ReadUptime`, `ReadFilesystems`,
`NewBlockSampler`, and `NewNetworkSampler`, followed by each sampler's `Sample` method. The shell must
own polling intervals and snapshot sharing.

### Icon asset policy

Tranche 3A ships no icons. Later Milestone 3 widgets may use project-owned icons under this policy:

- keep the authored SVG source in the repository;
- commit deterministic PNG or alpha-mask outputs at the physical sizes the shell uses;
- tint symbolic alpha masks with the current theme at paint time;
- do not parse SVG in the running shell;
- do not add a runtime SVG dependency, CGO library, or external conversion process;
- record the source and generated-asset licence beside the assets.

The implementation plan must choose the minimum checked-in size set after measuring the bar's supported
fractional scales. The renderer may select the nearest larger asset and downscale only if a pixel fixture
proves the result acceptable. Do not build a general asset pipeline before the first icon exists.

Milestone 5 tray and notification work will consume arbitrary application and freedesktop-theme icons.
That milestone must decide whether to add bounded runtime SVG decoding or accept a documented fallback.
External icon-theme compatibility does not justify a Milestone 3 decoder.

## Implementation plan boundaries

The executable plan should use this dependency order after owner approval and Milestone 2 integration:

1. Extend Niri typed state and synthetic event fixtures for windows and focused titles.
2. Project workspaces and focused titles by connector while preserving global host identity.
3. Add the concrete clock service and prove first-consumer start, shared snapshots, boundary alignment,
   last-consumer stop, and cancellation.
4. Add the first concrete widget view builders using existing text/layout nodes.
5. Extend validated configuration and stage widget/service changes through reload.
6. Replace the Milestone 2 fixture defaults with the four first-slice widgets.
7. Add two-output sharing, reconnect overlap, missing Niri, reload, redraw-coalescing, and race tests.
8. Run the automated gate, then the live Niri widget matrix and performance baseline.

Each numbered task needs a failing test, focused passing command, full package check, and one commit. Do
not parallelize tasks that edit `internal/shell/registry.go`, `internal/config`, or the reload transaction.

After the design fixes contracts, these lanes may run in separate worktrees:

- Niri lane: event decoding and immutable window snapshots under `internal/platform/niri`.
- Clock lane: concrete timer and lifetime tests under `internal/services`.
- Integration lane: widget composition, configuration, registry ownership, and reload. One agent owns
  this lane and merges the others.

The metrics lane stays in `sysc-metrics` until it produces an approved tag. Weather and SVG research can
remain read-only and must not add dependencies during Tranche 3A.

## Required automated evidence

The plan must leave runnable checks for these behaviors:

- two bars consume one clock service and one clock update;
- removing one bar or widget retains the service for the remaining consumer;
- removing the last consumer stops its timer/goroutine;
- a rejected reload does not alter service references or visible widgets;
- an accepted reload does not restart a service still in use;
- Niri initial window state yields the correct title for each output;
- focus, title, workspace, close, and move events invalidate only affected output bars;
- two Wayland globals sharing a connector retain distinct widget instances;
- unavailable Niri state renders a stable fallback and leaves the bar present;
- long titles truncate through system-font fallback without exceeding bounds;
- no source change produces no redraw;
- all goroutines stop under cancellation and the race detector reports no shared-state defect.

At each integration checkpoint run:

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell
gofmt -l .
git diff --check
```

Do not claim live behavior from these commands.

## Live Niri and performance gate

Run the live gate only after Milestone 2 passes its own matrix and the Tranche 3A diff passes local review.
Record reusable commands in `tests/integration/README.md`; keep connector names, titles, measurements, and
machine-specific output outside Git.

The Tranche 3A matrix must cover:

- one output and at least two outputs;
- one shared clock snapshot rendered on every configured bar;
- independent workspace and focused-window text per output;
- focus and title changes without restarting the shell;
- output reconnect with no duplicate or missing widget instance;
- config reload adding and removing clock/date/Niri widgets;
- Niri socket loss and shell shutdown;
- suspend/resume timing if the clock service keeps a long-lived timer.

Record baselines for idle CPU and wakeups, CPU during clock and Niri updates, RSS, submitted/skipped frame
counts, layout/paint duration, allocations per update, and binary size. Establish measurements before
setting budgets.

## Stop and return to owner review

Stop if any proposed design:

- needs a second goroutine to call Wayland;
- keys a widget instance only by connector name;
- opens one Niri connection, clock timer, or metrics sampler per output;
- requires a general widget schema, renderer interface, plugin protocol, or dependency-injection
  container for the first four text widgets;
- imports untagged `sysc-metrics` code or adds a local module replacement;
- adds a runtime SVG decoder or general asset pipeline for Milestone 3;
- adds panels, keyboard focus, workspace commands, weather, metrics, graphs, or animation to Tranche 3A;
- cannot preserve the last accepted widget/service set after a failed reload;
- cannot stop every new goroutine under cancellation;
- needs a new dependency where the Go standard library or current pinned modules cover the behavior.

## Completion handoff

For the design/plan tranche, report:

- branch and worktree;
- design decisions and rejected alternatives;
- exact implementation-plan path;
- dependency decisions and pins, if any;
- files changed and checks run;
- unresolved owner choices;
- assumptions that need live Niri or hardware evidence.

Stop for owner approval before product implementation. After implementation, write a separate completion
handover with commit hashes, fresh gate output, live observations, measurements, known defects, and the
next tranche. Do not update the Milestone 2 progress handover.

# sysc-shell Development Orchestration Plan

Date: 2026-08-26

## Purpose

This plan controls how the project moves from one architecture gate to the next. It keeps platform, UI, and feature work aligned while the codebase has few established contracts.

## Unit of delivery

One roadmap milestone forms one delivery unit:

```text
approved milestone design
        -> executable implementation plan
        -> isolated worktree
        -> small tested commits
        -> local review
        -> live Niri gate
        -> milestone handoff
```

Do not keep a permanent development branch containing several unfinished milestones. Merge or close one milestone before starting the next design.

## Repository and branch model

- `main` contains completed gates and documentation.
- Create one worktree and branch named `milestone/<short-name>` for implementation.
- Use short task branches only when two tasks can proceed without sharing files or an unsettled API.
- Rebase or merge task branches into the milestone branch before the live gate.
- Tag working milestone baselines after review, for example `proof-v0` and `bar-v0`.

Shell presentation and product coordination stay in this repository. Four supporting repositories have
approved independent ownership:

- `sysc-wayland`: Wayland transport, core bindings, and scanner;
- `sysc-metrics`: read-only Linux telemetry library;
- `sysc-notify`: freedesktop notification service and shell IPC;
- `sysc-tray`: StatusNotifierItem, DBusMenu, and shell IPC.

Further splits require an independently testable Linux service boundary and lower total maintenance.

## Planning gates

### Design gate

The milestone design must name:

- user-visible behavior;
- owners and data flow;
- required and optional protocols;
- error and shutdown behavior;
- tests and live hardware matrix;
- explicit non-goals;
- a stop or reconsider condition.

Implementation does not begin until the owner approves the design.

### Plan gate

The implementation plan must contain exact files, failing checks, minimal implementation steps, commands, expected results, and commit boundaries. A plan cannot use phrases such as "finish the renderer" or "handle edge cases" without listing the behavior.

### Dependency gate

Before adding a dependency, record:

- the behavior it replaces;
- its license and maintenance state;
- the pinned version or commit;
- the package boundary containing it;
- the removal or fork condition.

Standard library and installed platform services take precedence. A new framework requires a design revision.

## Work lanes

`sysc-wayland` runs as a separate foundation project first. Its `v0.1.1` tag must pass the tests,
generator reproduction, shell-generation probe, and live Niri probe in its foundation plan. No
`sysc-shell` implementation lane starts against an untagged commit or a local replacement.

After `sysc-wayland v0.1.1` is published, run `sysc-shell` Task 1 alone. Then use these lanes:

| Lane | Plan tasks | Owns |
|---|---|---|
| UI/render | 2 through 5, in order | retained tree, layout, shaping, rasterisation, pixels, scheduler |
| Protocols | 6 | pinned XML and reproducible generated bindings |
| Niri | 8 | socket framing, event decoding, state projection |

The integration owner reviews and merges all three lanes, then runs Task 7 after UI/render and protocol
work pass. Task 9 waits for Tasks 7 and 8. The same owner runs Tasks 9 and 10 because the command, proof
model, live compositor run, and shutdown cross lane boundaries.

Use one worktree per lane. Each lane branches from the Task 1 commit and touches only its listed files.
Merge UI/render and protocols before Task 7; merge Niri before Task 9. If a lane needs to change another
lane's public contract, stop it and resolve the contract in the integration branch.

The stable-bar milestone may separate output lifecycle, bar layout, and configuration parsing after its design fixes their contracts. Widget services can proceed in parallel only after the bar's state and invalidation model passes its gate.

### Cross-repository lanes

Repository work may proceed in parallel only at settled boundaries:

| Lane | May start | Merge or release gate |
|---|---|---|
| `sysc-wayland` foundation | now | `v0.1.1` before shell Task 1 |
| `sysc-metrics` core | after its M0 API gate | tagged core release before shell metric widgets |
| `sysc-notify` headless service | after its M0 protocol gate | private-bus and IPC tests before shell popup integration |
| `sysc-tray` headless service | after its M0 compatibility gate | watcher/item and IPC tests before shell tray integration |
| `sysc-shell` presentation | after each dependency tag | live Niri gate for the consuming milestone |

Metrics has no dependency on shell rendering and can develop beside the architectural proof. Notification
and tray protocol research can also proceed, but their shell adapters wait for panel, image, menu, input,
and accessibility contracts from Milestone 4. Do not let service repositories define shell widget trees.

Each service uses its own versioned protocol and release tag. Do not create a shared IPC or model
repository until two released services expose stable, materially identical code that costs less to share
than to keep local.

## Agent use

Use agents for bounded tasks with explicit files and proof commands:

- investigation: trace one protocol, dependency, or prior-art behavior and return file-and-line evidence;
- builder: implement one task whose API already appears in the approved plan;
- reviewer: inspect one diff for correctness, scope, and missing tests.

Do not ask several agents to design the same subsystem. The primary thread owns architecture and resolves cross-package choices. Agents working in parallel must use separate worktrees unless every task is read-only.

Each builder handoff includes:

- files changed;
- checks run and their result;
- assumptions;
- unresolved hardware behavior;
- commit hash.

## Commit discipline

- Keep commits aligned with one plan task.
- Pair a behavior with its focused test in the same commit.
- Avoid drive-by formatting and unrelated cleanup.
- Generated protocol code and its pinned XML belong in one commit.
- Commit messages state the subsystem and behavior, for example `feat: add Wayland layer-surface owner`.
- Do not claim a live Niri result from unit tests.

## Review gates

### Local code review

Review each task for:

- ownership at the responsible package;
- input validation at sockets, files, protocol messages, and plugin boundaries;
- cancellation and cleanup;
- cross-goroutine access to Wayland proxies;
- redraw and allocation behavior;
- scope added beyond the milestone;
- a focused test that fails when the behavior breaks.

### Architecture review

Run an architecture review before merging a milestone when the diff adds any of these:

- a new goroutine owner or long-running process;
- a protocol or persisted schema;
- a new surface kind;
- a UI primitive;
- CGO, dynamic libraries, or a graphics backend;
- an exported package or repository split.

The review may approve, narrow, or move the work to a later milestone.

### Live Niri review

Run live checks after unit review. Unit fakes cannot prove configure order, scale, buffer release, input coordinates, exclusive zones, or compositor shutdown.

The operator records the tested Niri version and output properties. Store reusable commands and acceptance observations in `tests/integration/README.md`; keep machine-specific logs outside Git.

## Visual consistency

The first theme defines named tokens for background, foreground, accent, muted, error, spacing, radius, bar height, and text sizes. Components consume tokens instead of independent constants.

For each visual milestone:

1. capture the same fixture tree at scale 1 and the test machine's fractional scale;
2. compare bounds, baseline alignment, spacing, clipping, and input regions;
3. keep small software-renderer golden images for deterministic primitives;
4. review live screenshots for font fallback and compositor scaling that golden tests cannot reproduce.

Do not copy Noctalia or DMS pixels blindly. Use them to identify behavior and hierarchy, then define one `sysc-shell` visual system.

## Performance evidence

Record measurements before setting hard budgets. Each milestone captures:

- idle CPU and wakeups;
- CPU during representative updates;
- resident memory;
- submitted and skipped frame counts;
- layout and paint duration;
- allocations per update for hot paths;
- binary size.

The first invariant is behavioral: no invalidation means no submitted frame. The widget milestone establishes baseline numbers on the reference machine. A later design sets thresholds from those measurements and comparable Noctalia/DMS runs.

GPU work requires a reproducible failing case. A subjective desire for smoother rendering does not pass the dependency gate.

## Hardware matrix

The minimum Niri matrix grows by milestone:

| Milestone | Required matrix |
|---|---|
| Architectural proof | one output at scale 1; one output at a non-1 scale |
| Stable bar | one output; two or more outputs; hotplug; transform; mixed scales |
| Widgets | shared services across outputs; missing sensor/service; suspend/resume |
| Panels | each output edge; small output; keyboard-only input; pointer input |
| Plugins | healthy, slow, malformed, oversized, crashed, restarted, disabled |

Add a hardware case after a reproduced defect. Do not build a theoretical matrix that no operator can run.

## Failure and rollback

- Keep the last passing milestone tag available.
- A failed task reverts through a normal commit; do not rewrite shared history.
- A failed live gate keeps the milestone branch open and `main` unchanged.
- A dependency qualification failure removes the dependency before merge or triggers a design revision.
- A protocol/schema change before its first release can replace the draft. After release, use version negotiation or migration.

## Milestone handoff

The handoff contains:

- accepted behavior and live matrix;
- automated commands and results;
- measurements without unsupported comparisons;
- known defects and deferred items;
- dependency pins;
- next design question.

Update `docs/roadmap.md` only when evidence changes a gate or scope. Do not turn it into a task log.

## First execution sequence

1. Commit the documentation baseline on `main`.
2. Execute the `sysc-wayland` foundation plan in its repository and obtain owner approval for
   `v0.1.1` publication.
3. Run the M0 design gates for `sysc-metrics`, `sysc-notify`, and `sysc-tray`; create exact implementation
   plans only after each contract passes review.
4. Create `milestone/architectural-proof` in a dedicated `sysc-shell` worktree and execute Task 1.
5. Start `sysc-metrics` core work in its own repository once its M0 gate settles public types.
6. Branch the shell UI/render, protocols, and Niri lanes from the Task 1 commit.
7. Review and merge the lanes in dependency order, then execute Tasks 7, 9, and 10 under one integration
   owner.
8. Tag the accepted baseline `proof-v0`.
9. Return to design mode for the stable multi-output bar; keep headless notification and tray work behind
   their repository gates until the shell has the presentation primitives they require.

The proof plan supplies the stop condition. Do not start the bar milestone in the proof worktree.

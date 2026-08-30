# Tranche 3A Immediate Execution Handover

Date: 2026-08-30

## Assignment

Start Milestone 3 Tranche 3A now. Update the existing Milestone 3 worktree from `main`, run Task 0, then
execute the approved 16-task plan with `superpowers:executing-plans`.

`sysc-5` remains open as deferred Milestone 2 hardware qualification. Its remaining checks do not block
Milestone 3 development, commits, or iteration. Do not claim that Milestone 2 has passed its full hardware
gate until `sysc-5` closes.

## Repository and worktree

- Primary checkout: `/home/nomadx/sysc-shell`
- Remote: `https://github.com/Nomadcxx/sysc-shell.git`
- Integration branch: `main`
- Milestone 3 worktree:
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation`
- Milestone 3 branch: `milestone/widget-foundation`

Run `bd` only from the primary checkout. Each worktree can create a separate ignored SQLite database and
report the wrong graph. Do not maintain a Markdown task list beside `bd`.

## Start here

Inspect the Milestone 3 worktree and preserve any unexpected local changes:

```bash
cd /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation
git status --short
git merge --ff-only main
```

Do not reset or overwrite a dirty worktree. Resolve its ownership before merging.

Confirm the issue state from the primary checkout:

```bash
cd /home/nomadx/sysc-shell
bd show sysc-6
bd ready
bd blocked
```

Claim `sysc-6` when implementation starts. Flush `.beads/issues.jsonl` from the primary checkout whenever
the issue state changes, and commit that file with the work it describes.

## Authoritative Tranche 3A documents

Read these files in order:

1. Design:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation-design.md`
2. Audit report:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation-audit-report.md`
3. Executable plan:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation.md`
4. Original commission:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation-execution-handover.md`

The executable plan contains Tasks 0 through 15. Task 0 remains a real implementation gate. Run its
checks against the merged Milestone 2 correction code before changing product code. The open `sysc-5`
hardware qualification does not form part of Task 0.

Use the `superpowers:executing-plans` workflow, follow the plan's TDD sequence and review checkpoints, and
commit at each boundary named by the plan.

## Current baseline

Milestone 2 implementation, review corrections, and the DMS content-rhythm correction are on `main`.
Relevant commits are:

- `72cb086`: Milestone 2 review corrections;
- `309f9d2`: Milestone 2 integration;
- `3f69801`: DMS-aligned content dimensions;
- `caaaabe`: prior handover commit, superseded by this document's execution order.

The automated gate passed after integration and visual correction:

```bash
go mod tidy -diff
go vet ./...
go build ./cmd/sysc-shell
go test -race -count=1 ./...
gofmt -l internal cmd
git diff --check
```

Task 0 must produce fresh evidence. Use isolated `/tmp` Go and XDG caches if default cache locks interfere.
The race suite needs an environment that permits Unix sockets.

## Tranche 3A scope

Implement only:

- clock and date text;
- Niri workspace text per output;
- focused-window title per output;
- process-scoped service lifetime and immutable snapshot delivery;
- output-specific widget instances;
- validated configuration, transactional reload, invalidation, and tests for those widgets.

Keep this slice read-only. Do not add metrics, battery, weather, panels, popouts, icons, graphs, animation,
workspace commands, a plugin protocol, a generic service registry, a renderer interface, or a second Niri
connection.

Preserve these invariants:

- one goroutine owns the Wayland connection and every proxy;
- a registry global identifies a bar host;
- a connector joins configuration and Niri state but never identifies a host;
- services live at process scope and feed output-specific widget instances;
- accepted reloads acquire replacement service references before releasing the old set;
- rejected reloads preserve accepted widgets, service counts, and visible state;
- `wl_shm` remains the only renderer;
- system-font fallback and cluster-safe truncation handle unbounded window titles;
- no source change means no submitted frame.

## Geometry and visual baseline

Do not change the layer-shell geometry to imitate the apparent DMS wallpaper gap. Measured DMS and
sysc-shell geometry matches:

- nominal token: 48 logical pixels;
- top and side gap: 4 logical pixels;
- painted body: 40 logical pixels;
- layer surface and exclusive zone: 44 logical pixels;
- corner radius: 12 logical pixels.

DMS's wallpaper layer caused the apparent extra separation below its bar. The current content defaults
from `3f69801` are a 6px inset, 4px item spacing, and 4px proof-button padding. Preserve them.

## Deferred Milestone 2 qualification

Another session can finish `sysc-5` on suitable hardware. The outstanding checks are:

- simultaneous bars on two or more outputs;
- independent workspace text on each output;
- physical hotplug, unplug, and reconnect;
- mixed simultaneous scales;
- pointer routing across two bars;
- the full 60-minute idle run.

Follow `/home/nomadx/sysc-shell/tests/integration/README.md`. Keep connector names, window titles,
screenshots, and host measurements outside Git. Close `sysc-5` only after every remaining row has direct
evidence. Findings from that run may create defects, but the qualification activity itself does not stop
Tranche 3A.

## Stop conditions

Stop and report before widening scope if implementation requires:

- a second Wayland goroutine;
- connector-only widget identity;
- one Niri connection or timer per output;
- an untagged `sysc-metrics` import or local module replacement;
- a runtime SVG decoder;
- a generic plugin, widget, service, or dependency-injection framework;
- a new dependency that the standard library or pinned modules cover;
- changes to panel, notification, tray, weather, metrics, power, or wallpaper ownership.

## Completion gate

Before merging Tranche 3A, request a focused code review, run the full automated gate, and complete the
Tranche 3A live matrix from the plan. Write a completion handover with commit hashes, fresh command output,
live observations, measurements, defects, and the next unblocked `bd` issue.

Do not edit the historical Milestone 2 progress handover.

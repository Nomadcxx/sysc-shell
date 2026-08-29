# Live Gate to Tranche 3A Execution Handover

Date: 2026-08-30

## Assignment

Complete the remaining Milestone 2 live Niri matrix. Close `sysc-5` only when every remaining check has
direct evidence. Then update the Milestone 3 worktree from `main` and execute Tranche 3A from its approved
16-task plan.

Do not start Tranche 3A while `bd show sysc-6` lists `sysc-5` as an open dependency.

## Repository and worktrees

- Primary checkout: `/home/nomadx/sysc-shell`
- Remote: `https://github.com/Nomadcxx/sysc-shell.git`
- Integration branch: `main`
- Milestone 3 worktree:
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation`
- Milestone 3 branch: `milestone/widget-foundation`
- Milestone 2 correction commit: `72cb086`
- Milestone 2 integration commit: `309f9d2`
- DMS content-dimension correction: `3f69801`

Run `bd` only from the primary checkout. A worktree has a separate ignored SQLite database and can report
the wrong dependency graph.

## Authoritative issue order

The issue graph enforces this order:

1. `sysc-5`: complete the Milestone 2 live Niri gate.
2. `sysc-6`: execute Tranche 3A.
3. `sysc-17`: execute Tranche 4A after Tranche 3A.

`sysc-4` and `sysc-13` are closed. `sysc-6` now depends on `sysc-5` as well as the closed correction
gate.

Start with:

```bash
cd /home/nomadx/sysc-shell
git status --short
bd show sysc-5
bd show sysc-6
bd blocked
```

Do not keep a Markdown task list beside `bd`.

## Milestone 2 state

Milestone 2 code and its review corrections are on `main`. The correction set covers output-global
identity, reconnect overlap, configured reload geometry, region refresh, system-font selection and
fallback, cluster-safe truncation, rounded-corner clipping, layer-close recovery, strict JSON parsing,
and lossless invalidation overflow.

The complete automated gate passed after integration and after the visual-token correction:

```bash
go mod tidy -diff
go vet ./...
go build -o /tmp/sysc-shell-final ./cmd/sysc-shell
go test -race -count=1 ./...
gofmt -l internal cmd
git diff --check
```

Use isolated `/tmp` Go and XDG caches if the default cache locks. The race suite needs an environment
that permits Unix sockets.

## Live evidence already obtained

A live Niri run covered these checks without changing the user's persistent shell configuration:

- one bar appeared on the available output;
- the default surface and exclusive zone were 44 logical pixels;
- a transformed output kept a 44px height, used post-transform logical width, and rendered upright;
- a live fractional-scale change replaced the buffer without dropping the bar;
- a valid reload changed geometry and theme;
- an invalid reload stayed mapped, retained the accepted state, and named `bar.height` in the error;
- a service restart restored the bar;
- a short idle sample showed no continuous frame loop.

Screenshots and machine-specific observations remained outside Git, as required by
`tests/integration/README.md`.

## Live evidence still required

Follow `/home/nomadx/sysc-shell/tests/integration/README.md`. Concentrate on the checks that need hardware
which was absent from the first run:

- two or more connected outputs with one bar on each;
- independent workspace text on each output;
- physical hotplug, unplug, and reconnect without duplicate or missing bars;
- mixed scales across simultaneous outputs;
- physical pointer routing on at least two bars;
- the complete 60-minute idle run.

Repeat the already-passed single-output checks only if the code changed or the multi-output run exposes a
regression. Keep connector names, window titles, screenshots, and host measurements outside Git.

When every matrix row passes:

```bash
cd /home/nomadx/sysc-shell
bd close sysc-5 --reason "Full live Niri matrix passed."
bd sync --flush-only
git add .beads/issues.jsonl
git commit -m "chore: close M2 live gate"
```

Do not close `sysc-5` for partial evidence.

## Visual finding that must not be reversed

The DMS comparison separated two different concerns.

The layer-shell geometry already matches DMS:

- nominal token: 48 logical pixels;
- top and side gap: 4 logical pixels;
- painted body: 40 logical pixels;
- layer surface and exclusive zone: 44 logical pixels;
- corner radius: 12 logical pixels.

Pixel measurements placed the DMS and sysc-shell body edge, transparent rows, and Niri window border at
the same coordinates. The apparent extra clearance under DMS came from its wallpaper layer. Stopping the
single DMS process also removed that layer and exposed Niri's dark background, which made the transparent
gap disappear visually. Running sysc-shell over the same wallpaper through an independent background
layer reproduced the DMS separation without moving the bar or window.

Do not increase the exclusive zone or paint the transparent gap to compensate. A standalone background
or wallpaper owner is a separate shell capability. Define that ownership in its own design when the
roadmap reaches it.

The content band did differ from DMS. Commit `3f69801` corrected the defaults to:

- content inset: 6px, producing a 28px item band inside the 40px body;
- item spacing: 4px;
- proof-button padding: 4px.

The Milestone 3 design inherits those values. Do not restore the audited historical 8px/6px defaults.

## Tranche 3A documents

Read these in order:

1. Design:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation-design.md`
2. Audit report:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation-audit-report.md`
3. Executable plan:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation.md`
4. Original commission:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-built-in-widget-foundation-execution-handover.md`

The executable plan has 16 tasks, numbered 0 through 15. Task 0 is a real gate. Run it after `sysc-5`
closes; do not infer that the merged code satisfies it.

## Starting Tranche 3A

After the live gate closes, update the existing clean worktree:

```bash
cd /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation
git status --short
git merge --ff-only main
```

If the worktree is not clean, inspect and preserve the existing changes. Do not reset or overwrite them.

Use the `superpowers:executing-plans` workflow. Follow the plan's TDD sequence and review checkpoints.
Commit each plan task at its named boundary. Run `bd` from the primary checkout and flush
`.beads/issues.jsonl` with the work it describes.

## Tranche 3A scope

Ship only:

- clock text;
- date text;
- Niri workspace text per output;
- focused-window title per output;
- process-scoped service lifetime and immutable snapshot delivery;
- output-specific widget instances;
- validated configuration, transactional reload, invalidation, and tests for those widgets.

Keep the first slice read-only. Do not add metrics, battery, weather, panels, popouts, icons, graphs,
animation, workspace commands, a plugin protocol, a generic service registry, a renderer interface, or a
second Niri connection.

Preserve these invariants:

- one goroutine owns the Wayland connection and every proxy;
- a registry global identifies a bar host;
- a connector joins configuration and Niri state but never identifies a host;
- services live at process scope and feed output-specific widget instances;
- accepted reloads acquire replacement service references before releasing the old set;
- rejected reloads preserve the accepted widgets, service counts, and visible state;
- `wl_shm` remains the only renderer;
- system-font fallback and cluster-safe truncation handle unbounded window titles;
- no source change means no submitted frame.

## Stop conditions

Stop and report rather than widening scope if implementation needs:

- a second Wayland goroutine;
- connector-only widget identity;
- one Niri connection or timer per output;
- an untagged `sysc-metrics` import or local module replacement;
- a runtime SVG decoder;
- a generic plugin, widget, service, or dependency-injection framework;
- a new dependency that the standard library or current pinned modules already cover;
- changes to panel, notification, tray, weather, metrics, power, or wallpaper ownership.

## Handoff after Tranche 3A

Before merge, request a focused code review and run the complete automated gate. Then run the Tranche 3A
live matrix from the plan. Write a completion handover with commit hashes, fresh command output, live
observations, measurements, known defects, and the next unblocked `bd` issue.

Do not edit the old Milestone 2 progress handover.

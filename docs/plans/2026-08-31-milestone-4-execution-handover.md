# Milestone 4 Execution Handover

Date: 2026-08-31
Scope: implement all corrected Milestone 4 work in the existing `milestone/panels-controls` worktree.

## Assignment

Implement Tranche 4A, then Tranche 4B, from the corrected plans on `main`. Keep implementation on
`milestone/panels-controls`; do not edit product code in the primary checkout. Commit each plan task
before starting the next one. Stop at the review boundaries named below.

Another session is auditing M3 on `main`. Preserve its files and beads rows. Do not merge the M4
branch into `main`; report the completed branch and evidence for review.

## Read first

1. `AGENTS.md`
2. `docs/roadmap.md`, Milestone 4 gate
3. `docs/plans/2026-08-31-milestone-4-post-m3-audit-report.md`
4. `docs/plans/2026-08-30-panel-foundation-design.md`
5. `docs/plans/2026-08-30-panel-foundation.md`
6. `docs/plans/2026-08-30-settings-osd-theme-catalog-design.md`
7. `docs/plans/2026-08-30-settings-osd-theme-catalog.md`

The post-M3 audit and corrected plans supersede conflicting instructions in the historical M4 review
and parallel-execution handover.

## Repository and tracker

- Primary checkout: `/home/nomadx/sysc-shell`, branch `main`. Run `bd` here, or set
  `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` from the worktree.
- Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls`
- Branch: `milestone/panels-controls`
- Claim 4A with `bd update sysc-17 --status in_progress`.
- Commit `.beads/issues.jsonl` with the code or documents whose state it records.
- Do not create a second beads database in the worktree.

Before implementation, rebase the worktree branch onto `main` containing this handover. Resolve the
beads JSONL by preserving every issue ID from `main`; use the primary database for the final export.

## Existing 4A prework

The branch contains four commits based on the earlier plan:

| Commit | Work |
|---|---|
| `8fdbe6d` | Task 1 matugen invocation verification |
| `949eb71` | Task 5 panel placement and single-instance model |
| `8926c8e` | Task 8 rounded masks and shadows |
| `d01267c` | Prework close and mask comments |

Keep the implementations that still match the corrected plan. Amend Task 5 during integration:

- public panel name is `system-monitor`, not `monitor`;
- `PanelSet.Output(PanelID) (uint32, bool)` replaces the ambiguous `Open` return;
- hotkey and IPC placement centers off the focused output's bar;
- fitted sizes and margins must remain non-negative on tiny outputs.

Run the focused Task 5 and Task 8 tests after the rebase before relying on those commits.

## Tranche 4A order

Execute the 13-task 4A plan in dependency order. Tasks 1, 5, and 8 have prework; verify and correct
them instead of reimplementing them. Start new product work at Task 2.

The load-bearing post-M3 rules are:

- System-monitor reuses `internal/services.Metrics`, its selector leases, snapshots, and histories.
  It imports no `sysc-metrics` package directly and creates no second sampler owner.
- CPU and memory tabs always ship. Add at most the first configured filesystem, block, and network
  selector on the focused bar, one per source. Reuse M3 `KindGraph`.
- Keep `wayland.Invalidation.Global`; add `SurfaceID` without rekeying a bar to connector.
- Deliver `wl_keyboard.key` unchanged. Do not subtract 8.
- Configuration reload keeps auxiliary surfaces mapped and rebuilds their content and theme in place.
- Generated palette opacity belongs to `shell.Theme`; pass the resolved boolean through
  `HostCallbacks` without importing shell types into the Wayland package.
- 4A accepts wallpaper and hex theme sources. Task 12 of 4B adds named stock sources.
- Panels open through IPC and Niri hotkeys. Do not add a pointer-only bar launcher.
- Animation goroutines stop on close, move, output loss, and registry shutdown.
- 4A adds no module dependency or local `replace` directive.

After each task, run the focused package tests and `go test ./...`. Before the 4A gate, run:

```bash
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
```

Record live Niri results that were run. Keep unrun hardware checks open; do not convert missing
hardware into a pass. `sysc-5` remains owner-deferred evidence and does not block development.

Stop after Task 13 and report 4A commits, automated output, live observations, and unresolved risks.
Do not begin 4B until 4A has been reviewed and merged into the M4 branch baseline.

## Tranche 4B review boundaries

Execute 4B as three reviewed slices, all on `milestone/panels-controls`:

1. Tasks 1-8: sysc-wayland bindings, controls, settings, and atomic persistence.
2. Tasks 9-11: audio, brightness, OSD, and IPC.
3. Tasks 12-14: stock themes, template catalog, and acceptance.

At each boundary, run the focused checks plus `go test ./...`, commit all work, and report the slice.
Do not carry changes from the next slice into the review.

Task 1 is cross-repository work. Implement and test text-input-v3 and cursor-shape-v1 in
`/home/nomadx/sysc-wayland`, pass its release gate, tag a release, then pin that tag in sysc-shell.
Do not use a local `replace`.

Runtime commands use `exec.LookPath`, argv slices, bounded contexts, and no shell. Missing optional
tools hide their capability or select the documented fallback. Config writes use a unique temp file
in the destination directory, mode 0600, file and directory sync, rename, and cleanup on error.

## Stop conditions

Stop and report before continuing if any of these occurs:

- a corrected plan conflicts with a landed M3 API;
- a change would put Wayland proxies on another goroutine;
- 4A requires a new dependency, direct metrics sampler, or local module replacement;
- reload would tear down the settings panel or another open aux surface;
- the sysc-wayland release gate fails;
- focused tests or the full race suite regress and the cause is not contained in the current task;
- the live Niri result contradicts a source-verified focus, stacking, or placement assumption.

## Handoff evidence

For each review boundary, return:

- commit hashes mapped to plan tasks;
- `git status --short --branch`;
- focused test commands and results;
- full test, race, vet, and formatting results where required;
- live checks as pass, fail, or unrun;
- measurements and hardware-specific behavior;
- discovered work recorded in `bd` with a `discovered-from` dependency.

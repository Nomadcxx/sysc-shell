# Milestone 4 Tranche 4A Parallel Execution Handover

Date: 2026-08-30

## Assignment

Start the Milestone 4 parallel-safe work while Tranche 3A continues. The approved design and executable
implementation plan already exist. Do not redesign Tranche 4A or write another plan.

Use `superpowers:executing-plans`. Execute the safe tasks named below, preserve their existing TDD and
commit boundaries, then join the rest of the plan to `main` after Tranche 3A merges.

The deferred Milestone 2 hardware qualification in `sysc-5` does not block this work.

## Repository and worktree

- Primary checkout: `/home/nomadx/sysc-shell`
- Remote: `https://github.com/Nomadcxx/sysc-shell.git`
- Integration branch: `main`
- Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls`
- Branch: `milestone/panels-controls`
- Parallel-prework issue: `sysc-18`
- Full Tranche 4A issue: `sysc-17`

The M4 worktree was clean and its branch was an ancestor of `main` when this handover was written. Start
with:

```bash
cd /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls
git status --short
git merge --ff-only main
```

Preserve unexpected changes. Do not reset or overwrite a dirty worktree.

Run `bd` from `/home/nomadx/sysc-shell`, never from a worktree. This checkout uses `bd 0.17.7`, which has
produced partial JSONL exports during concurrent work. Do not commit `.beads/issues.jsonl` unless
`jq -s 'length' .beads/issues.jsonl` matches
`sqlite3 .beads/beads.db 'select count(*) from issues;'` in the primary checkout.

## Existing design and plan

Read these documents in order:

1. Design:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-panel-foundation-design.md`
2. Review and applied corrections:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-milestone-4-review.md`
3. Niri research:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-panels-and-controls-research.md`
4. Executable 13-task plan:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-panel-foundation.md`

The plan already defines files, tests, commands, and commit boundaries. This handover changes execution
order to permit work with no M3 file overlap. It does not change the approved feature scope.

## Start now: parallel-safe tasks

Execute these plan tasks:

1. Task 1: verify the matugen `color` command and record the exact invocation.
2. Task 5: implement pure panel placement and single-instance state in new `panel.go` files.
3. Task 8: implement cached rounded masks and pre-blurred shadows.

Task 1 changes documentation at most. Task 5 creates new shell files and imports stable `ui.Rect` data.
Task 8 adds the renderer mask implementation and changes `canvas.go`; Tranche 3A does not modify that
file.

Run each task's failing test before implementation, then its named passing test. Commit at the boundary
already specified by the plan.

Task 3, the isolated theme-generation package, has no M3 file overlap. Start it after `sysc-16` records
the owner decision permitting matugen as an optional runtime binary. The fallback palette remains
mandatory if that decision permits matugen.

## Wait for Tranche 3A before these tasks

Do not start the following work until `sysc-6` merges to `main`:

| Plan task | Shared ownership |
|---|---|
| 2 | M3 changes `internal/config/config.go`, `load.go`, and their tests. |
| 4 | M3 creates or changes `bar.go` and `registry.go`. |
| 6 | M3 changes the Wayland client and its invalidation path. |
| 7 | M3 changes the UI tree and layout vocabulary. |
| 9 and 10 | Panel hosting and popouts consume M3 registry, widgets, clock service, and Niri projection. |
| 11 and 12 | IPC effects and process wiring consume the finished panel host and M3 `main.go` wiring. |
| 13 | The gate tests require the integrated M3 and M4 surface set. |

This boundary avoids parallel edits to config, registry, bar, Wayland client, UI tree, and process
wiring. Do not copy unfinished M3 files into the M4 branch.

## Join after Tranche 3A merges

After `sysc-6` closes and its branch merges to `main`:

```bash
cd /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/panels-controls
git status --short
git merge main
```

Resolve conflicts by preserving both the completed M3 contracts and the isolated M4 commits. Stop if a
conflict changes ownership rather than text.

Verify the M3 prerequisites listed at the start of the 4A plan, including:

- process-scoped `Registry` host ownership;
- `Bar` event and render callbacks;
- the leased clock service;
- transactional config preparation and invalidation delivery;
- one Wayland goroutine and one Niri connection.

Run the full automated gate after the merge. Continue the remaining plan in this order:

```text
Task 2 -> Task 3 if pending -> Task 4 -> Task 6 -> Task 7 ->
Task 9 -> Task 10 -> Task 11 -> Task 12 -> Task 13
```

Tasks 5 and 8 must not be repeated after their commits merge cleanly.

## Tranche 4A scope

Tranche 4A ships:

- auxiliary panel and dismiss-shield surfaces;
- placement, clamping, rounded corners, and shadows;
- button, label, separator, and roving keyboard focus;
- matugen-backed tokens with a compiled fallback, subject to `sysc-16`;
- clock/calendar and session/power panels;
- the versioned shell IPC socket and Niri hotkey documentation.

The system-monitor panel, metrics service, tabs, and graphs remain deferred until Tranche 3B qualifies a
tagged `sysc-metrics` release. Tranche 4A adds no Go module dependency.

## Invariants

- One goroutine owns the Wayland connection and every proxy.
- Panel state lives at process scope in `shell.Registry`.
- One panel ID has one process-wide instance.
- Opening or closing a panel never changes the bar exclusive zone.
- Reload keeps open auxiliary surfaces mapped and refreshes their content.
- Keyboard interaction reaches each shipped control and Escape closes the panel.
- Interactive nodes carry an accessible name and role.
- `wl_shm` remains the renderer.

## Stop conditions

Stop and report if implementation requires:

- editing an unfinished M3-owned file before `sysc-6` merges;
- a second Wayland goroutine or Niri connection;
- a local `replace` or untagged `sysc-metrics` import;
- metrics, notifications, tray, settings, OSD, or template-catalog work;
- a generic UI, service, dependency-injection, or plugin framework;
- an external binary before `sysc-16` records its policy.

## Handover after execution

For the parallel slice, report task commits, fresh test output, and any M3 contract assumption that did
not hold. Keep `sysc-18` scoped to Tasks 1, 5, and 8 plus conditional Task 3.

Before merging full Tranche 4A, request focused review, run the plan's automated gate, and complete its
live Niri matrix. Write a completion handover with commit hashes, observations, defects, and the next
unblocked `bd` issue.

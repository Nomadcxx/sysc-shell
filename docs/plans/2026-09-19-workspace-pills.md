> **For implementation:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

# Workspace Pills: Shape-Only With Urgency

Date: 2026-09-19
Design: docs/plans/2026-09-16-bar-weather-launcher-workspaces-design.md (slice D3)
Issue: sysc-410
Worktree: .worktrees/feature/sysc-410, branch feature/sysc-410

## Goal

The bar's workspace row stops painting numbers and paints state instead. Niri's
`is_urgent` flag travels from the event wire through the projection into the
pill, the focused workspace becomes the larger flexible rounded element, and
every other workspace becomes a smaller shape whose fill says occupied, urgent,
or empty. The row keeps Niri index ordering, focus dominance, the pre-snapshot
fallback, and the existing hit and accessibility identity.

## Scope

In: `internal/platform/niri` wire decoding of `is_urgent`, `workspacePill`
urgency in `internal/shell/projection.go`, shape-only pill painting in
`internal/shell/widget.go`, focused checks in the three existing test files.

Out: workspace switching actions, pill labels, new theme tokens, new config,
the weather (D1) slice, and any change outside the three files above plus their
tests.

## Invariants

- The projected workspace tree contains no painted number: every pill is a
  childless capsule. No hidden text node carries an index.
- Focus dominates: a focused workspace keeps `FillAccent` even when urgent.
- On inactive pills urgent outranks occupied: `FillError` before
  `FillContainer`.
- Empty inactive pills stay present as hit targets with `FillNone`, so empty
  and occupied remain distinguishable while the row keeps its width.
- Pills keep their existing identity: no new Action, Name, or Tooltip appears.
- Niri index ordering and the pre-snapshot fallback capsule are unchanged.
- Urgency is optional on the wire: a payload without `is_urgent` decodes false.

## Task 1 — carry urgency on the wire (internal/platform/niri)

Failing check first: `internal/platform/niri/events_test.go` gains a case
where the workspace payload carries `"is_urgent":true` and the decoded
`Workspace.Urgent` is true; a payload without the field decodes false.

Implement: `Workspace` gains `Urgent bool`; `wireWorkspace` decodes an optional
`*bool` for `is_urgent` (absent stays false, the field is not required);
`project()` copies it into the projected workspace.

Commit: `feat(niri): carry workspace urgency from the wire`

## Task 2 — project urgency into pills (internal/shell)

Failing check first: `internal/shell/projection_test.go` — `projectOutputs`
sets `Urgent` on the pill of an urgent workspace, and a focused urgent
workspace still reports `Focused` with `Urgent` set.

Implement: `workspacePill` gains `Urgent bool`; `projectOutputs` fills it from
the projected workspace.

Commit: `feat(shell): project workspace urgency into pills`

## Task 3 — paint shapes, not numbers (internal/shell)

Failing check first: `internal/shell/widget_test.go` —
`refreshWorkspacePills` builds childless capsules: the focused pill is
`FillAccent` with `Height` = `metrics.IconLarge` and `Width` = twice that;
an occupied inactive pill is a square `FillContainer` capsule of side
`metrics.IconLarge`; an urgent inactive pill is `FillError`; an empty inactive
pill is `FillNone`; no pill has children. The pre-snapshot fallback capsule is
unchanged. `workspacePillsMatch` asserts the new shape-only structure.

Implement: rewrite `refreshWorkspacePills` and `workspacePillsMatch` in
`internal/shell/widget.go`. Fill precedence per pill: focused → `FillAccent`;
urgent → `FillError`; occupied → `FillContainer`; empty → `FillNone`. Sizes
come from `theme.Metrics` already carried on `barView`: inactive side =
`IconLarge`, focused height = `IconLarge`, focused width = `2 × IconLarge`.
The row gap stays `workspacePillGap`.

Commit: `feat(shell): paint workspace pills as shapes`

## Task 4 — gates

```
timeout 240s env GOMAXPROCS=2 go test -p 2 -count=1 \
  ./internal/platform/niri ./internal/shell ./internal/ui ./internal/config
go build ./cmd/sysc-shell
go vet -p 2 ./...
gofmt -l <changed files>   # no output
git diff --check
git diff --exit-code -- go.mod go.sum
```

The repository-wide race gate stays machine-blocked on this workstation; the
affected-package commands above are the available proof, recorded as such.

## Task 5 — live gate and landing

Laptop eDP-1 (ssh -p 7777 nomadx@192.168.0.64, Niri pid socket
/run/user/1000/niri.wayland-*.sock): stop `sysc-shell.service`, run the built
binary with the session environment, assert `niri msg -j layers` maps
`sysc-shell:bar`, and capture with `grim`. The capture shows shape-only pills:
the focused pill wider than the dots, occupied dots filled, empty slots
unfilled, ordering intact. Urgency is proven by the unit checks; a live urgent
window is best-effort only. Restore `sysc-shell.service` afterwards.

Then merge the branch to main, close sysc-410 citing the asset of evidence
(wire, projection, paint checks plus the live capture), and commit
`.beads/issues.jsonl` in the same commit series. Do not push unless asked.

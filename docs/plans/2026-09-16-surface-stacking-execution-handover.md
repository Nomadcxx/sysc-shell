# Surface stacking execution handover

Date: 2026-09-16.

This is the receiving handover for `docs/plans/2026-09-11-surface-stacking.md`.
Start that plan from the published `main`; do not redesign the slice here.

## Receiving state

`main`, `origin/main`, and the server-side `main` ref are all at
`d1f82c5` (`chore: close media selection task`). The media prerequisite is
landed and live-tested:

- `internal/services/media.go` owns the MPRIS service.
- `internal/shell/mediabody.go` owns the shared control-centre Media body.
- The page has functional transport, position, player selection, bounded local
  and remote art, and a Home summary.
- The bar and page share the art worker/cache. The laptop gate showed the same
  Spotify cover art in the bar and Media card, with the transport controls and
  player selector mapped.
- `sysc-156`, `sysc-283`, and `sysc-284` are closed. `sysc-205` is unrelated
  clipboard-history work and remains in progress.

The current Media card is still the bounded rounded-art composition in
`mediaNowPlaying`. It is not a `KindStack` consumer yet. There is currently no
`KindStack` production code; the next worker must execute the six tasks in the
stacking plan rather than assume the primitive already exists.

No Beads issue is created for stacking at this handoff. That is deliberate: the
plan creates its consumer issue only in Task 6, after the media-card decision
and implementation. Do not create a placeholder issue now.

## Dependencies and important reconciliation

The blur dependency is satisfied on `main` by `127f7aa` (`Merge branch
'feature/panel-backdrop-blur'`). Its renderer contains
`internal/render/blendMaskImage` in `image.go`, with bounded bilinear sampling
through an alpha mask. Do not reopen the blur slice or add another image
decoder.

The stacking plan calls the future image route `paintImageSmooth`, but that
symbol is not currently present. Treat the dependency as satisfied at the
renderer level: trace and reuse `blendMaskImage` (or add the smallest
background-only routing helper needed by Task 4), while preserving the current
nearest-neighbour `paintImage` path for worker-sized icon/art rasters. Do not
turn every `KindImage` into a bilinear image or duplicate the blur compositor.

Read these before editing:

- `docs/plans/2026-09-11-surface-stacking-design.md`
- `docs/plans/2026-09-11-surface-stacking.md`
- `internal/ui/layout.go`, `internal/ui/column.go`, and `internal/ui/tree.go`
- `internal/render/image.go` and `internal/render/paint.go`
- `internal/shell/mediabody.go` and its tests

## Start procedure

Use `superpowers:executing-plans` or
`superpowers:subagent-driven-development` as required by the plan. Create a
dedicated worktree from the published `main`; do not implement in the primary
checkout or in the old media worktree:

```bash
cd /home/nomadx/sysc-shell
git worktree add .worktrees/feature/surface-stacking -b feature/surface-stacking main
```

Run Beads only from `/home/nomadx/sysc-shell`. The primary checkout contains
the canonical `.beads/beads.db`; do not initialise a second database in the
new worktree. Leave the pre-existing untracked `.commandcode/` and `.cursor/`
directories alone.

## Execution order

Follow the plan's task boundaries and commit messages. The required proof is:

1. Add `KindStack` end to end: maximum measurement, shared-box placement,
   column support, paint coverage, and `kindcoverage`/stack tests.
2. Pin reverse hit traversal so the last, topmost child wins.
3. Pin a `FillScrim` child changing the pixels beneath it.
4. Add the explicit background-image distinction and route only that path
   through the existing bilinear compositor. Keep icon sampling unchanged.
5. Make the existing Media card the real consumer. Its child order must be
   background image, scrim, then foreground content. Preserve the current
   late-art/no-art fallback, bounded art worker, controls, state updates, and
   hit behaviour. Add the plan's `TestMediaCardStacksContentOverItsBackground`
   coverage rather than accepting a stack with no production consumer. Record
   the consumer decision in the commit body.
6. Only after the consumer is real, create the plan's
   `Surface stacking: media card consumer` issue with
   `discovered-from:sysc-156`, export the canonical JSONL carefully, and commit
   the tracker change. Do not create that issue before Task 5.

Do not pull the template panel, rectangle damage, a general UI toolkit, or
another media tree into this slice. The template panel is the later approved
second consumer, not a reason to widen the current implementation.

## Verification gate

The stacking plan forbids `go test ./...` and every `-race` build. Use its
named checks instead:

```bash
go test ./internal/ui -run 'TestStack|TestKind' -v
go test ./internal/render -run 'TestStackScrim|TestBackgroundImage|TestPaintImage' -v
go test ./internal/shell -run TestMediaCard -v
go test ./internal/shell
go vet ./internal/ui ./internal/render ./internal/shell
go build ./...
gofmt -w .
test -z "$(gofmt -l .)"
git diff --exit-code -- go.mod go.sum
```

The first failing checks are intentional. Keep the nil-child and explicit
height cases from the plan; they protect the layout invariant that makes a
stack different from a flowing column. The media test must prove a real
stacked card, not only that `KindStack` can be constructed in isolation.

For the consumer gate, build a fresh binary and use the existing isolated
laptop test process only. The last isolated process was
`/tmp/sysc-shell-media-merged`; its PID was `1866511` when this handover was
written, but discover the process by its exact path before replacing it. The
laptop is reached with `ssh -p 7777 nomadx@192.168.0.64`; discover its current
Niri socket and output rather than assuming an old suffix. Do not overwrite
`~/.local/bin/sysc-shell`, do not restart the installed service, and do not use
`pkill -f`.

After Task 5, open the Media section, capture real pixels, and confirm the
full-bleed art, scrim, foreground content, bar art, controls, and player list
remain readable and interactive. Assert mapped surfaces with
`niri msg -j layers`. Record any scale-specific or no-art behaviour in the
completion record; do not silently change the plan to make an unrunnable gate
look passed.

Stop at the stacking plan's exit gate. Leave the template consumer and later
surface-stacking extensions for their own plan work.

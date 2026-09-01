# Milestone 5 Tranche 5A Execution Handover

Status: Tasks 1–4 landed on `milestone/m5a-notifications`; Tasks 5–10 remain
Plan: [notifications foundation](2026-08-30-notifications-foundation.md)
Decision: [icon raster decision](2026-09-01-m5-icon-raster-decision.md)

## What is done

The M5 entrance gates are all closed. Both services are tagged and published, and the shell resolves the
notify pin through the public module proxy — no `replace` directive anywhere.

| Task | State | Commit |
|---|---|---|
| 1 Pin and fixture the notify protocol | done | `ee451d2` |
| 2 Generation-safe notify client | done | `5977f71` |
| 3 Shared raster node and bounded icon path | done | `a354912`, `28af57a` |
| 4 Body markup | markup done; card tree not started | `275af66` |
| 5–10 | not started | — |

`a50c964` additionally fixes a pre-existing intermittent race in this package: two `t.Parallel()` tests
shared the package-level `runArgv` hook. It is now a `Registry` field.

## What Task 4 still needs

`internal/shell/notifytext.go` parses the freedesktop subset into bounded runs and is tested. The card
tree half — `internal/shell/notifycard.go` and its test — is not written. It must build retained nodes
from the imported `protocol` records, omit actions and reply for history entries, and carry urgency,
countdown, and an independent value bar. Link nodes exist only where the opener capability passed
qualification; `ParseBody` already takes that as its `allowLinks` argument.

## What changed against the plan

**Icons are raster-only.** Recorded in full in the icon raster decision document. No SVG rasterizer is
pinned, so an SVG-only theme yields a placeholder. This deviates from one line of the `sysc-tray` v0.1
qualification matrix and the M5 qualification record must say so.

**`KindImage` was added without a `kindCount` sentinel.** The plan said to extend the exhaustive
coverage table if it had landed. It has not — that table is on the unmerged bar-parity branch. When
those meet, `KindImage` needs adding to it; do not weaken the sentinel to accommodate it.

## Where the pieces live

- `internal/notifyclient` — socket transport and the generation rule. Publishes immutable
  `Message{Generation, Kind, …}`. A structural error ends the generation rather than skipping a message.
- `internal/icons` — the one shared resolver and bounded decode worker. The tray uses this too in 5B;
  it must not grow a second icon path.
- `internal/ui` `KindImage` plus `internal/render/image.go` — measure in both row and column paths, and
  a painter that drops a raster whose pixels do not match its declared geometry.
- `internal/shell/notifytext.go` — body markup.

## Gate

From the worktree:

```bash
gofmt -l .
go vet ./...
go test -race -count=1 ./...
```

All clean at `a50c964`. Run `-count=8` on `./internal/shell/` when touching that package: its races
surface roughly one run in eight.

## Remaining M5

5A Tasks 4 (card tree) through 10, then Tranche 5B against `sysc-tray v0.1.0-rc.1`, then `sysc-65`
qualification on the two-output Niri matrix. Both service tags are `rc.1`; a wire change means `rc.2`,
and the first tag never moves.

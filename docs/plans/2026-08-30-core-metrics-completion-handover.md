# Core Metrics (Milestone 3, Tranche 3B) Completion Handover

Date: 2026-08-31
Branch: `milestone/core-metrics`, unmerged
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/core-metrics`
Design: `2026-08-30-core-metrics-design.md`
Plan: `2026-08-30-core-metrics.md`
Issue: `sysc-8`

All thirteen tasks are implemented. The tranche has **not** been audited and the live matrix has **not**
been run.

## Commits

| Commit | Task |
|---|---|
| `1e61236` | 1 — `leaseSet` extracted from `clock.go` |
| `ad4f07c` | 2 — metrics service lifetime, sources and leases |
| `893d0f2` | 3 — the sampling loop |
| `c270f7d` | 4 — the history ring |
| `1ad5f9a` | 5 — `Node.MinWidth`, a width floor for text |
| `628bacd` | 6 — `KindGraph` and its painter |
| `7dc7b72` | 7 — the five metric ids and their validation |
| `8a0d3e2` | 8 — metric widgets and projection |
| `2954993` | 9 — registry leasing and `UpdateMetrics` |
| `b5fe59a` | 10 — the sampling pump in `main.go` |
| `82d2e46` | 11 — cross-cutting evidence |
| `1f3a298` | 12 — the live matrix |

Twelve commits, 26 files, +2146 / -327 against `main`.

## Automated gate

Run on 2026-08-31 at `1f3a298`:

```
go mod tidy -diff      no difference
go test -race -count=1 ./...   ok, every package, no race report
go vet ./...           silent
go build -o /tmp/sysc-shell-tranche3b ./cmd/sysc-shell   succeeded
gofmt -l .             no output
git diff --check       no output
```

283 test functions repository-wide, 25 in `internal/services` and 66 in `internal/shell`.

`go.mod` carries `github.com/Nomadcxx/sysc-metrics v0.1.0` and no `replace` directive.

**These prove no live behaviour.** No claim about a real Niri session is made anywhere in this document.

## Design decisions as built

Each of D1–D8 is implemented as designed. Worth restating, because they are the parts a reader is most
likely to want to change back:

- **Five item ids, not one parameterised `metric`.** `path` on a CPU widget fails at load naming
  `bar.items.<section>[N].path`, rather than being silently ignored.
- **A meter on a rate source is rejected at load.** Bytes per second has no full scale. A graph stays
  legal on every source because it normalises against its own window maximum.
- **The shell never computes a rate.** `formatMetric(item, snapshot)` takes no interval, so there is no
  elapsed time in scope to divide by. The prior art's defect — DMS's `timeDiff = updateInterval / 1000`
  — is unreachable here rather than merely avoided.
- **Only leased sources are sampled.** `collect` guards each source on `Leased`, so a CPU-only
  configuration never opens `/proc/diskstats`.
- **One goroutine owns all three stateful samplers** for the process lifetime; they are constructed
  inside `run` and no reference escapes it.
- **Failure logging is edge-triggered.** One line when a source begins failing, one when it recovers.

## Deviations from the plan

Three, all small, all deliberate. Nothing in the design changed.

**1. Task 6's paint fixture was rewritten.** The plan's `newPaintFixture` helper could not have worked:
it set no `style.Body`, and `Paint` rejects a body with a non-positive dimension, so every call would
have returned an error before painting. Its `filledPixels` helper counted any pixel whose RGB was
non-zero, but the fixture background is `#101418`, so the unfilled column would have counted as full and
the assertion `right <= left` could never have passed.

The test now reuses the existing `newTestCanvas`, `testStyle` and `mustTestFace` helpers, sets
`style.Body` to the canvas, and counts pixels equal to the accent colour through a new `accentPixels`
helper. The assertion is stricter than the plan's: the zero column must paint exactly no accent pixels
and the full column must paint exactly its whole area, rather than merely more than the other.

**2. `Registry.Close` also closes the metrics service.** Task 9's diff did not add it, though Task 10's
prose asserts it happens. Without it the sampling goroutine outlives the registry and Task 11's
goroutine-count test fails. Added in `2954993`.

**3. One error message reworded.** The plan's rate-meter rejection read "which the rate source %q has
no". It now reads "has none". No behaviour change; the test asserts only that the error names `display`.

## What is not done

1. **The live matrix has not been run.** It is recorded in `tests/integration/README.md` as ten numbered
   items plus six baselines. The owner has deferred live testing for the milestone; this is not a defect
   to chase, and no live behaviour may be claimed until it runs.
2. **No independent audit.** 3A's audit found seven findings, two of which would have left the tree red
   at a task boundary. 3B has had self-review only. An audit before merge is the pattern that worked.
3. **Not merged.** `milestone/core-metrics` is twelve commits ahead of `main`.
4. **`sysc-8` is still open.** The tracker was not touched; closing it is a merge-time action, as
   `sysc-6` was for 3A.

## Known characteristics worth a reviewer's attention

Not defects, but the places where a reviewer's judgement is most useful:

- **`historyLocked` copies every leased ring on every view assembly**, which happens once per bar per
  update. At 120 samples and five sources that is bounded and small, but it is the tranche's only
  per-update allocation of consequence. `normalise` then allocates once more per graph.
- **`hasPlottedWidget` forces a repaint every sample for any bar carrying a meter or a graph.** This is
  correct — neither carries text, so text comparison cannot see their change — but it means the "no
  source change, no submitted frame" property holds only for text metrics. The design anticipated this
  for graphs; a meter whose fraction is unchanged also repaints, which is a slightly wider net than
  strictly needed.
- **`run` recomputes the full interval on each loop**, so a burst of `rearm` signals restarts the sleep
  rather than sampling. Acquire only signals `rearm` when the finest interval strictly shortens, so a
  starving sequence would need repeated shortening acquisitions; it mirrors `Clock.run` exactly.
- **`record` uses `Filesystems[0]`** for the filesystem history ring, not the widget's configured mount.
  A graph on a filesystem therefore plots the first mount in the snapshot rather than the one the widget
  names. The design's D6 puts one ring per source rather than per subject, so this follows from it, but
  it is the one place where a graph and its text sibling can disagree about what they describe.

## Next

`sysc-10` (Tranche 3D, weather and visual vocabulary) is unblocked by this work once `sysc-8` closes.
`sysc-19` (power and thermal collectors in `sysc-metrics`) is independent and ready now.

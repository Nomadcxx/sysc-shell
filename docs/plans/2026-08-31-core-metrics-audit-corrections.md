# Core Metrics Audit Corrections

Date: 2026-08-31
Branch: `milestone/core-metrics`, unmerged
Answers: `2026-08-31-core-metrics-audit-report.md`

All six findings are closed. The two majors were owner decisions; both were taken as the audit
recommended, and the design carries the amendments inline.

## Commits

| Commit | Finding | Change |
|---|---|---|
| `07fc08a` | 6 | A whitespace metric selector is rejected at load |
| `2ab96ed` | 4 | The percentage width floor is measured, not guessed |
| `4a43c7c` | 1, 5 | History is keyed by selector, and discarded with its last lease |
| `84b45fb` | 2, 3 | Absence and change detection cover every display mode |
| `136cc7c` | — | D3, D6, D7 and D8 amended in the design |

## Finding 1 — history keyed by subject (major)

**Owner decision: amend D6 to per-subject rings.**

`Selector{Source, Subject, Direction}` now keys both the leases and the history rings. A network graph
of `eth9` rx plots that interface in that direction; a filesystem graph of `/fixture` plots that mount.
The rejected alternative is unchanged: rings stay owned by the service, so two bars graphing the same
subject share one.

`Snapshot` gained `Fraction`, `Rate` and `Value`. Selecting a subject out of a pass moved to the type
that holds the pass, so `internal/shell` no longer carries a second walk of the same structures — that
duplication is what let the graph and its text sibling diverge in the first place.

Evidence: `TestEachSubjectKeepsItsOwnHistory`, `TestEachDirectionKeepsItsOwnHistory`,
`TestAFilesystemHistoryFollowsItsOwnMount` in `internal/services`, and `TestAGraphPlotsItsOwnSubject`
in `internal/shell` for the lookup half. The audit's missing evidence row is filled.

## Finding 2 — absence in every display mode (major)

**Owner decision: an absent meter paints nothing.**

`ui.Node.Absent` marks a node with no reading. It still measures and reserves its space, so nothing
reflows when a source drops, but it paints no track and no fill. A genuine zero still paints its track,
which is what keeps the two distinguishable — the defect was that an unavailable meter and an idle
machine were pixel-identical.

A graph plots only while the current pass carries a reading. Its ring keeps the last good window, and
drawing that window for a source which stopped reporting was the stale node D7 rejected, reached by
accident.

`Absent` carries no colour and no message, so it does not anticipate 3D's error tone; `Node.Tone` can
refine it there without rework.

Evidence: `TestAMeterWithNoReadingIsAbsentRatherThanZero`, `TestAGraphStopsPlottingWhenItsSourceFails`,
`TestAnAbsentMeterPaintsNothing` (which also asserts a zero still paints its track), and
`TestAnAbsentGraphPaintsNothing`.

## Finding 3 — change detection covers every display mode

`Bar.applyLocked` captures each node before formatting and compares `Value`, `Values` and `Absent`
alongside `Text`. `hasPlottedWidget` is deleted: a bar no longer repaints because of what kind of widget
it holds, only because something it shows actually changed.

Task 9's `TestABarWithAGraphRepaintsOnEverySample` asserted the behaviour this finding calls a defect. It
is replaced by `TestAGraphRepaintsWhenItsWindowChanges`, which asserts the invariant instead: a changed
window repaints, an identical one does not.

Evidence: that test plus `TestAnUnchangedMeterChangesNothing` and `TestAnUnchangedGraphChangesNothing`.
"No source change, no submitted frame" now holds in all three display modes rather than one.

## Finding 4 — the floor is measured

`"100%"` measures **36** logical pixels on the resolved sans-serif face at size 14. The floor was 34, so
D8 did not hold: a value crossing 99 to 100 still widened its node.

`Node.MinWidth int` became `Node.MinWidthText string`, a sample string shaped through the same path as
the node's own text. A pixel constant cannot be right across faces it was never measured on.

Evidence: `TestAPercentageKeepsOneWidthFromNineToOneHundred` lays out a real bar at 9% and at 100% and
requires one width. Removing the floor makes it fail 20 against 36, so it tests the defect and not just
the mechanism.

## Finding 5 — the ring goes with its last lease

A selector's ring is created with its first lease and deleted with its last, in the same critical
section. A widget removed at midday and restored in the evening starts a fresh window instead of drawing
one line across a gap of hours.

A reading absent this pass is skipped rather than pushed as zero: a failed collector is not a
measurement of nothing, and a pushed zero would draw a trough that never happened.

Evidence: `TestReleasingTheLastLeaseDiscardsTheHistory`, `TestASecondConsumerSharesTheHistory` (the
sharing this must not break), `TestAnAbsentReadingIsNotRecordedAsZero`.

## Finding 6 — whitespace selectors

`selector` trims before its emptiness check and stores the trimmed value, so `" "` fails at load naming
its field path rather than loading a widget that waits forever for a mount that cannot exist.

Evidence: `TestAWhitespaceSelectorIsRejected`.

## Also addressed

`copyNode` shallow-copied, so a painted node shared the live node's `Values` backing array. The audit
called this "not a current defect" because `format` assigns a fresh slice. `copyNode` promises that no
pointer into live model state reaches the painter, and a slice header is one, so it now clones `Values`
and the promise is true rather than accidentally true.

## Gate

Re-run at `136cc7c`:

```
go mod tidy -diff                 no difference
go test -race -count=1 ./...      ok, every package, no race report
go vet ./...                      silent
go build -o /tmp/sysc-shell-3b-final ./cmd/sysc-shell   succeeded
gofmt -l .                        no output
git diff --check                  no output
```

299 test functions, up from 282 at audit time. `go.mod` carries `sysc-metrics v0.1.0` and no `replace`.

These prove no live behaviour. The live matrix remains unrun and owner-deferred.

## State

Nineteen commits ahead of `main`, unmerged. `sysc-8` is still open.

The audit's recommended action 4 said not to merge until findings 1 and 2 were decided. They are decided
and applied, so that gate is clear. Whether a re-audit of the corrections is wanted before merge is the
owner's call; the changed surface is `Selector` and its keying, `Node.Absent`, `Node.MinWidthText`, and
`applyLocked`.

# Core Metrics Audit Report

Date: 2026-08-31
Auditor: independent audit agent (standard technical audit of the Tranche 3B implementation)
Commissioned by: `docs/plans/2026-08-31-tranche-3b-audit-brief.md`

## Scope and state

- Branch: `milestone/core-metrics`
- Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/core-metrics`
- Branch head at audit time: `b9c12c6` (the audit brief). Implementation head: `28aa607` /
  `1f3a298` (live-matrix record). Fourteen commits ahead of `main`; unmerged.
- Documents read, in the brief's order: the design (`2026-08-30-core-metrics-design.md`), the
  completion handover, the 13-task plan where a task's intent was needed, and the Milestone 3
  charter (`2026-08-30-built-in-widget-foundation-execution-handover.md`).
- Diff against `main`: 28 files, +2382 / −327. In-scope product code is
  `internal/services/{leases,metrics}.go`, `internal/shell/{metricwidget,widget,registry,bar}.go`,
  `internal/config/{config,load}.go`, `internal/ui/{tree,layout}.go`, `internal/render/paint.go`,
  `cmd/sysc-shell/main.go`, and their tests.
- No product file was edited. The live matrix was not run and is not a finding.

### Automated gate, re-run this session at `b9c12c6`

```
go mod tidy -diff                 no difference
go test -race -count=1 ./...      ok, every package, no race report
go vet ./...                      silent
go build -o /tmp/sysc-shell-tranche3b-audit ./cmd/sysc-shell   succeeded
gofmt -l .                        no output
git diff --check                  no output
```

282 `Test*` functions. `go.mod` carries `github.com/Nomadcxx/sysc-metrics v0.1.0` and no `replace`.

These prove no live behaviour.

## Already declared — confirmed, not re-reported

| Claim | Result |
|---|---|
| Task 6's paint fixture was rewritten | **Held.** `TestGraphPaintsTallerColumnsForLargerValues` reuses `newTestCanvas` / `testStyle` / `mustTestFace`, sets `style.Body`, and counts accent pixels. The zero column must paint exactly none; the full column must paint exactly its area (`2*h`). That is the right assertion: the plan's `filledPixels` would have counted the `#101418` background as filled. |
| `Registry.Close` closes the metrics service | **Held.** `registry.go:198` calls `r.metrics.Close()`. Without it the sampling goroutine outlives the registry. |
| One error string reworded, "has no" → "has none" | **Held.** `load.go:352`. Behaviour unchanged; the test still asserts the error names `display`. |

## Ranked sites

| Area | Verdict |
|---|---|
| Filesystem / selector history | **D6 is wrong for selector sources.** See finding 1. |
| Lock discipline | **Holds.** Every nested path is `r.mu` then `m.mu` (`viewLocked` → `historyLocked` → `Leased`/`History`). `services` never imports `shell`. The sampling goroutine takes `m.mu` and never `r.mu`. `publish` is called after `r.mu` is released. |
| Sampler ownership | **Holds.** `CPUSampler`, `BlockSampler`, `NetworkSampler` are locals of `run` (`metrics.go:222–226`). They are not fields of `Metrics`. `Sample()` appears only in `collect`, which only `run` calls. At most one `run` is live: `startLocked` fires only when `m.stop == nil`. |
| Repaint breadth | **Confirmed hole** for meters. See finding 3. Graphs are the case the design anticipated. |
| Newest-wins publish | **Holds for a single sender.** `sendSnapshot` is called only from `run`. On stop/restart, `stopIfUnusedLocked` nils `m.stop` before `<-done`, so a concurrent `Acquire` can start a second `run` whose `sendSnapshot` overlaps the exiting one — the same Clock shape, and `PrepareConfig` avoids the lease-count hitting zero. Invalidations block (`registry.go:79–86`). |
| Early return in `run` | **Holds.** `finestLocked()==0` is only reachable after `releaseMetric`/`Close` has already nilled `m.stop` in the same critical section that emptied the leases. `Running()` cannot be true with no goroutine. The opposite window exists (`Running()` false while the old goroutine unwinds) and matches Clock. |
| Graph painting | **Arithmetic is safe.** `len(values) > box.W` drops the oldest; last column right edge is `X+W`; remainder leaves a left gap, never overflow. `width < 1` and `x < box.X` are unreachable after the slice. Paint tests cover only two samples in width 4. |
| Validation completeness | **Holds**, with one empty-string hole (finding 6). Unknown ids fail at `load.go:274` before option checks, naming the item path. Meter-on-rate names `.display`. Direction vocabularies are separate (`rx` on block fails). |
| Allocation per update | **As disclosed.** `historyLocked` copies every leased ring once per bar per sample; `normalise` allocates again per graph. Bounded (120 × sources). Not a defect. |

## Findings

Severity key: **verified** = reproduced against code; **suspected** = reasoned, not executed.

| # | Sev | Where | Defect | Consequence | Fix |
|---|---|---|---|---|---|
| 1 | Major (verified) | Design D6; `internal/services/metrics.go:396–416`; `internal/shell/metricwidget.go:173–180` | One history ring per *source type*. `record` pushes `Filesystems[0]` (lexicographic first successful mount from `sysc-metrics`), the sum of every block device's read+write, and the sum of every interface's rx+tx. A graph widget plots `History[src]` and never consults `item.Path` / `Device` / `Interface` / `Direction`. Text and meter siblings *do* select. D6's prior-art citation is Noctalia's `diskHistory(path, windowSize)` — per path, not per source type. The rejected alternative was per-*widget* rings, which is still right to reject. | `{"id":"network","interface":"wlan0","direction":"rx","display":"graph"}` plots whole-machine bidirectional traffic. A filesystem graph of `/home` plots `/` (or whichever mount sorts first). The graph and its text sibling disagree about the subject. | Key rings by `(source, subject[, direction])`, still owned by the service and still shared across bars. That keeps D6's anti-duplication rationale and matches the prior art D6 claimed to follow. |
| 2 | Major (verified) | D7; `metricwidget.go:165–169`, `:178–180`; `metrics.go:390–416` | Unavailable handling splits by display mode. Text uses `formatMetric` → `"-"`. A meter writes `node.Value = 0`. A graph assigns `normalise(v.History[src])` and does not look at the current snapshot; `record` skips a failed source, so the ring keeps last-good samples. D7 rejected a stale node; those belong to 3D. `formatMetric`'s comment says no widget invents a zero. | A failed CPU meter looks like idle 0%. A graph whose collector has been failing for minutes still plots the last good window. Three display modes, three unavailable behaviours, one of them the rejected alternative. | Meter: keep the last layout but do not invent 0 — treat absent as the text placeholder (or skip fill, leaving only the track). Graph: do not plot when the current snapshot has that source nil/invalid; an empty `Values` already paints nothing. |
| 3 | Minor (verified) | `registry.go:227–231`; `bar.go:103–111`, `135–145` | `apply` compares only text. Meter/graph `format` always returns `""` and writes `Value`/`Values` as a side effect, so `apply` never sees them. `UpdateMetrics` ORs `hasPlottedWidget()`, which is true for *any* meter or graph on the bar, every sample. The design anticipated this for graphs; a meter whose fraction is unchanged also repaints. Disclosed in the completion handover. | A meter-only bar submits a frame every interval even when the fraction is identical. "No source change, no submitted frame" holds only for text metrics. | Compare `Value` (and `Values` by content) inside `apply`. Then `hasPlottedWidget` is unnecessary. A graph whose normalised window is unchanged would also stop repainting, which is the invariant. |
| 4 | Minor (verified) | `metricwidget.go:148–150`, `:185` | `metricTextFloor = 34` is an unmeasured pixel constant. D8 exists so `"9%"` and `"100%"` do not reflow; the layout primitive is tested (`TestTextIsFlooredAtItsMinWidth`) but nothing asserts that 34 is ≥ the shaped width of `"100%"` at size 14 tabular on the shipped face. Same class as 3A's `defaultTitleMaxWidth = 260`. | If 34 is short, D8 does not hold and a percentage still shoves its neighbours as it crosses 99. If it is long, the field is padded for no reason. | Measure `"100%"` once with the same path `measureNode` uses, or lift the default into the theme token set beside the other geometry. |
| 5 | Minor (verified) | `metrics.go:72–83`, `386–417` | History rings are process-lifetime. They are never reset on last-lease stop. A later re-acquire appends into the old 120 samples. | A CPU graph removed at 14:00 and restored at 18:00 plots the afternoon samples as if they were contiguous with the evening ones. The visual gap is a multi-hour hole. | Clear the ring (or start a new one) when that source's lease count hits zero. |
| 6 | Minor (verified) | `config/load.go:421–422` | `selector` rejects `nil` and `""`, not whitespace. `" "` is a well-formed path/device/interface. | A typo of spaces loads, then renders `"-"`. until a mount named `" "` appears, which it will not. | `strings.TrimSpace` before the empty check, or reject anything that is not a real path/device/interface token. |

## Concurrency — verified, no defect

- Lock order is one-way: registry → metrics → (bar, in `apply`). Bar never re-enters registry or Metrics. Wayland `Render` takes only `Bar.mu`.
- Samplers do not escape `run`. The pump in `main.go:84–92` receives `Snapshot` values, not sampler pointers.
- `sendSnapshot` is newest-wins, capacity 1, never closes, single sender in the steady state.
- `publish` blocks; `Close` unblocks via `r.closed`.
- `copyNode` shares the `Values` backing array with the live node. Today `format` assigns a new slice rather than mutating in place, so Paint races nothing. An in-place write later would race; not a current defect.

## Coverage against the design's evidence table

| Required behaviour | Check | Result |
|---|---|---|
| Two bars share one service and one sample | `TestTwoBarsShareOneMetricsServiceAndOneSample` | Held |
| Only leased sources are sampled | `TestOnlyLeasedSourcesArePopulated`, `TestAMixedBarLeasesEverySourceItUses`, `TestAConfigWithNoMetricLeavesTheServiceStopped` | Held |
| Last consumer stops the goroutine | `TestDroppingTheLastMetricBarStopsTheService`, `TestTheFirstMetricLeaseStartsAndTheLastStops` | Held |
| Accepted reload does not restart a live service | `TestAnAcceptedReloadDoesNotRestartTheSamplingService` (`Starts()==1`) | Held |
| Rejected reload alters nothing | `TestARejectedReloadLeavesTheSamplingServiceUnchanged` | Held |
| Samplers stay sequentially owned | Construction in `run`; `-race` clean; one goroutine | Held by structure |
| Rates use measured elapsed time | `formatMetric` takes no interval; shell never divides | Held by construction. No delayed-tick fixture in this repo — that evidence lives in `sysc-metrics` |
| First sample has no rate | `TestTheFirstRateSampleRendersThePlaceholder` | Held (projection of `Valid: false`) |
| Partial failure isolates | `TestOneFailingSourceDoesNotSuppressAnother` | Held for *text*. Finding 2 is the meter/graph hole |
| Meter on a rate source is rejected | `TestAMeterOnARateSourceIsRejected` | Held |
| Graph auto-scales to its window | `TestAGraphNormalisesAgainstItsWindow`, `TestAnAllZeroWindowNormalisesFlat` | Held |
| Percentage width is stable | `TestTextIsFlooredAtItsMinWidth` | Held for the primitive. Finding 4 is the missing widget-level measurement |
| No source change produces no redraw | `TestAnUnchangedSampleChangesNothing` | Held for text (`metricConfig` is a CPU text widget). Finding 3 is the meter/graph hole |
| Cancellation stops every goroutine | `TestClosingTheRegistryStopsTheSamplingGoroutine` | Held |

No test asserts that a filesystem/block/network *graph* plots the configured subject. That is the missing row for finding 1.

## Stop conditions

Checked individually. None applies.

- One goroutine owns the Wayland connection; this tranche does not touch a proxy.
- One goroutine owns all three `sysc-metrics` samplers. No sampler, timer, or polling goroutine per output. The metrics pump in `main.go` is a Clock-shaped consumer, not a sampler.
- Widget instances remain keyed by `wl_registry` global.
- `Clock` and `Metrics` are concrete. There is no `interface {` in this tranche's new code, no service registry, no injection container. `leaseSet` is a shared struct.
- `go.mod` reads `v0.1.0`. No `replace`. No new dependency beyond `sysc-metrics`.
- The shell never recomputes a rate. `formatMetric(item, snapshot)` has no interval in scope.
- A metrics failure degrades; it does not cancel the process context (unlike a Niri stream failure).
- Invalidations publish with a blocking send.
- No thermal, battery, GPU, or process metrics.

Charter deviations left in place: graph node in 3B (D5); five item ids (D1); meter rejected on a rate source; unavailable text is `"-"`.

## Decisions judged wrong

**D6, as written, for selector sources.** One ring per source type is the right grain for CPU and memory, and sharing that ring across bars is the point. It is the wrong grain for filesystem, block, and network, which have a subject (and, for rates, a direction). The design's own prior-art paragraph names `diskHistory(path, windowSize)`. The implementation is a faithful reading of the D6 table and a contradiction of that paragraph.

The alternative is still service-owned, still not per widget: one ring per `(source, subject[, direction])`. Two bars graphing `wlan0` rx share a ring; a bar graphing `wlan0` tx does not. Cost: a map instead of a `[sourceCount]*ring`, still capped at the number of live metric widgets.

D1–D5, D7's *intent*, and D8 survive. D7's *implementation* for meter and graph does not (finding 2).

## Claims that could not be verified, and what would settle them

| Claim | Settling evidence |
|---|---|
| A delayed tick yields the library's rate, not `delta/interval` | A `sysc-metrics` fixture already covers this; the shell cannot undo it without an interval in `formatMetric`. No additional shell test is required unless someone later adds one. |
| `metricTextFloor = 34` is ≥ `"100%"` at size 14 tabular | One `Measure` call against the shipped face (finding 4). |
| Graph columns at 120 samples in 48 logical pixels, and at `box.W ∈ {0,1}` | Extending `TestGraphPaintsTallerColumnsForLargerValues`. Arithmetic was checked by hand and holds. |
| `Filesystems[0]` on this machine | Live `/proc/self/mountinfo` order after `sysc-metrics`'s lexicographic sort. Not run; the code path does not depend on which mount is first, only that it is not `item.Path`. |
| Live matrix (ten items plus six baselines) | Owner-deferred. Not a finding. |

## Live Niri and hardware

Not run. No live claim is made from the automated gate, the completion handover, or this report.

## Recommended action for the owner

1. **Amend D6** to per-subject (and per-direction, for rates) rings, still service-owned. Then change `record` / `History` and add one test that a filesystem graph of `/fixture` does not plot a different mount.
2. **Close finding 2** so meter and graph obey D7: no invented zero, no frozen last-good plot.
3. Findings 3–6 are cheap and can land with (1)–(2) or wait.
4. Do not close `sysc-8` and do not merge until (1) and (2) are decided. `sysc-10` stays gated on that close. `sysc-19` remains independent.

### Verdict

**Proceed with named corrections.** The lifetime, lock, sampler, validation, pin, and rate-enforcement work is sound. The two majors are a design-grain miss that the graphs faithfully implemented, and a D7 split that makes non-text unavailable states lie. Neither is a stop-condition breach. Both are user-visible on a bar that ships a graph.

# Tranche 3A Implementation Audit and Q/A Report

Date: 2026-08-31
Auditor: independent audit agent (implementation vs spec, plus Q/A of the shipped tests)
Commissioned after: `docs/plans/2026-08-31-core-metrics-audit-report.md`
Subject: Tranche 3A as merged to `main` at `f245eba`

This is not the 2026-08-30 design/plan audit. That one ran before any product code existed. This
one checks the shipped implementation against
`2026-08-30-built-in-widget-foundation-design.md` and the charter, and whether the automated
evidence can actually catch a miss.

The live matrix is owner-deferred. It is not a finding. No live behaviour is claimed.

## Scope and state

- Branch: `main` at `f245eba` (3A merged at `39a7760`; later commits exist on `main`)
- Worktree: `/home/nomadx/sysc-shell`
- Spec: `2026-08-30-built-in-widget-foundation-design.md` (D1–D8, evidence table, stop conditions)
- Charter: `2026-08-30-built-in-widget-foundation-execution-handover.md`
- Completion handover: `2026-08-30-built-in-widget-foundation-completion-handover.md` (five declared
  plan deviations — confirmed, not re-reported)
- In: `internal/services/clock.go`, `internal/shell/{widget,registry,bar,projection}.go`,
  `internal/platform/niri/{events,client}.go`, `internal/config/{config,load}.go`,
  `internal/ui/{tree,layout}.go`, `internal/render/{paint,text,truncate}.go`,
  `cmd/sysc-shell/main.go`, and their tests
- Out: Tranche 3B code on `milestone/core-metrics`; 3C/3D plans; the live matrix

### Automated gate, re-run this session at `f245eba`

```
go mod tidy -diff                 no difference
go test -race -count=1 ./...      ok, every package, no race report
go vet ./...                      silent
go build -o /tmp/sysc-shell-tranche3a-qa ./cmd/sysc-shell   succeeded
gofmt -l .                        no output
git diff --check                  no output
```

`go.mod` has no `sysc-metrics`. These prove no live behaviour.

## Already declared — confirmed, not re-reported

The completion handover's five plan deviations all hold in the tree: `Invalidation` rekeyed to
global (`9acf2b5`); `WindowsChanged` removed from the ignored-event fixture; pointer tests rebuilt
on a synthetic action node; `PrepareConfig` keeps `[]wayland.HostIdentity`; socket-level window
test uses `nextSnapshot`. Tabular figures shipped (`Node.Tabular` → `tnum`). Lossless invalidation
ships (`publish` blocks; `TestEveryChangedBarIsReportedWhenManyChangeAtOnce` proves twelve bars
against an eight-deep channel).

## Findings

Severity key: **verified** = reproduced against code; **suspected** = reasoned, not executed.

| # | Sev | Where | Defect | Consequence | Fix |
|---|---|---|---|---|---|
| 1 | Major (verified) | Design §Invalidation step 4 and `Bar.apply` (`design.md:169–170`, `:441`); `internal/shell/bar.go:121–131`; `wayland/client.go:670–681`; `render/paint.go:108–110` | `apply` writes `node.Text` and returns whether it changed. It does not re-layout. Invalidation only calls `sched.Invalidate()`. `Configure`/`layoutLocked` run only on a layer-shell configure. `paintText` returns immediately when `box.W <= 0`. | Production order is Configure (often with empty clock text and empty title, so `Bounds.W == 0`) then later `UpdateClock`/`UpdateNiri`. The first clock tick and the first focused-window title therefore paint into a zero-width box and **do not appear** until a later output configure (scale or size change). A title that grows is clipped to the empty-state width; the centre clock is not re-centred when the left section grows. Tests do not catch this: `TestALongTitleStaysWithinItsCapAndKeepsItsNeighbour` calls `UpdateNiri` *then* `Configure`, which is the inverse of the live sequence. | Store last configure size on `Bar`. On `apply` true, call `layoutLocked`. Add a test that Configures with empty text, then applies a non-empty view, and asserts `Bounds.W > 0` **without** a second Configure. |
| 2 | Minor (verified) | `wayland/client.go:316–324` | `hostBecameReady` calls `NewHost` (which acquires leases) then `validate`. A validate — or a later `createBar` — failure returns without `DropHost`. | Leases and the clock goroutine live until process `registry.Close()`. Rare (a host that became ready then failed validation), but it is a leak on an error path the design said must not leak. | `DropHost(h.global)` on every failure after a successful `NewHost`. |
| 3 | Minor (verified) | Evidence table row 5; `registry_test.go:204–234` | “An accepted reload does not restart a live service, **including a boundary change**.” The registry test reloads only `Accent`. Boundary re-arm is proven at the service (`TestAShorterBoundaryDoesNotRestartTheClock`), not through `PrepareConfig`. | A regression that restarted the clock on a format change from `15:04` to `15:04:05` would not fail the integration test the design named. | One `PrepareConfig` whose candidate changes a clock format's derived boundary, then `Starts()==1`. |
| 4 | Minor (verified) | Evidence table row 7 | Focus, title, and close have decoder/projection tests but no `UpdateNiri` → changed-globals test. Move and workspace-switch do (`tranche3a_test.go:109`, `registry_test.go:85`). The design-audit move gap was closed; the other three events in that sentence were not lifted to the same level. | A projection bug that updated the node text but reported the wrong globals would only be caught for workspace and move. | One table test: focus-within-workspace, title change, close → expected globals. |

Not defects:

- `clockBoundaries` acquires one lease per clock instance, including two one-minute clocks on the default bar. The comment says “distinct”; D7 wants consumer counting. Two leases for two clocks is the right grain. The comment is wrong, the code is not.
- Design prose mentions `ctx.Done()` for clock cancellation; the implementation uses a `stop` channel via `Close`. Equivalent. The published API never took a context.

## Decisions vs implementation

| # | Decision | Result |
|---|---|---|
| D1 | Concrete `textWidget`, no widget interface | **Held.** |
| D2 | Instances keyed by `wl_registry` global | **Held.** `TestTwoGlobalsSharingAConnectorKeepDistinctInstances`. |
| D3 | Title is that output's active workspace's active window | **Held.** `projectOutputs` never reads global focus. `WindowFocusChanged` is ignored (`events.go:280`). |
| D4 | One `clock` id, parameterised by layout | **Held.** Defaults: `15:04` centre, `Mon 2 Jan` right. |
| D5 | Options per instance, string-or-object union | **Held.** Wrong-id options fail naming the field path. |
| D6 | Boundary derived from layout; cap 1 min; invariant layouts rejected | **Held.** `"HH:MM"` rejected; `"January 2006"` accepted as 1 min. |
| D7 | Clock consumer-counted; Niri stream unconditional | **Held.** `TestAConfigWithNoClockLeavesTheServiceStopped`; `main.go` always opens the stream. |
| D8 | `MaxWidth` on text, consumed by `window-title` | **Held** at the primitive. Finding 1 means a title that appears after first configure never receives a new measured width, so D8 is moot until layout runs again. |

Tabular figures (design-audit major, accepted): **Held.** Only clock sets `Tabular`; measure, truncate, and raster all take the flag (`TestOnlyClockWidgetsRequestTabularFigures`).

## Q/A of the automated evidence

| Required behaviour | Named check | Q/A |
|---|---|---|
| Two bars, one clock, one update | `TestTwoBarsShareOneClockServiceAndOneUpdate` | Passes. Default bar takes two leases (two clocks); `Starts()==1` is the right assertion. |
| Drop one bar, service remains | `TestRemovingOneBarRetainsTheServiceForTheOther` | Passes. |
| Drop last, goroutine stops | same + `TestClosingTheRegistryLeavesNoGoroutine` | Passes. |
| Rejected reload alters nothing | `TestARejectedReloadLeavesServicesAndWidgetsUnchanged` | Passes for bar identity, text, `Starts()`, `Running()`. Does not assert lease count. |
| Accepted reload does not restart | `TestAnAcceptedReloadDoesNotRestartAServiceStillInUse` | Passes. Does not change a boundary (finding 3). |
| Initial titles per output | `TestInitialWindowStateTitlesEachOutput` | Passes. |
| Focus/title/workspace/close/move invalidate only affected bars | workspace + **move** named; focus/title/close decoder-only | Finding 4. Move (design-audit finding 6) **is closed**. |
| Dangling `active_window_id` | `TestADanglingActiveWindowIDProjectsToNoTitle` | Passes. |
| Two globals, one connector | `TestTwoGlobalsSharingAConnectorKeepDistinctInstances` | Passes. |
| Unavailable Niri → `"-"` / empty title | `TestUnavailableNiriStateRendersAStableFallback` | Passes. |
| Long multi-script title within cap | `TestALongTitleStaysWithinItsCapAndKeepsItsNeighbour` | Passes **only because Configure follows apply**. Inverts live order (finding 1). |
| No source change, no redraw | `TestATickInsideTheSameBoundaryChangesNothing` | Passes. |
| Cancellation, no race | `TestClosingTheRegistryLeavesNoGoroutine`; `go test -race` | Passes this session. |
| Lossless invalidation | `TestEveryChangedBarIsReportedWhenManyChangeAtOnce` | Passes. |

**Q/A verdict on the suite:** it is strong on lifetime, projection, and invalidation *identity*. It is weak on the layout/paint pipeline: no test Configures, then changes text, then Renders and inspects pixels or `Bounds`. That is why finding 1 shipped with a green gate.

## Concurrency — verified, no defect

- Clock `Release`/`Close` wait on `done` outside the lock.
- `publish` does not hold `r.mu`.
- `Bar.Render` takes only `b.mu`; Wayland never takes the registry lock.
- `send` is newest-wins, single sender.
- `PrepareConfig` acquire-before-release: `Starts()` stays 1.

## Stop conditions

None triggered. No second Wayland goroutine; no connector-keyed identity; one Niri connection and one clock timer; no widget interface or registry; no `sysc-metrics`; no SVG; no panels/focus/metrics/graphs in this tranche; `Rollback` exists; goroutines stop under `Close`; no new dependency.

## Claims that could not be verified

| Claim | Settling evidence |
|---|---|
| Finding 1 on a live output (clock invisible until next configure) | One Niri session: start the shell, wait for the first minute tick, inspect whether the centre clock is visible before any output scale change. Owner-deferred with the matrix. The code path is verified; the pixel result is not. |
| Suspend/resume timer lateness (design: ≤ one boundary of stale text) | Live matrix item. |
| `WorkspaceActiveWindowChanged` on this build of niri | Verified live in the 2026-08-30 design audit on `26.04 (8ed0da4)`. Not re-run here. |

## Decisions judged wrong

None of D1–D8. Finding 1 is an implementation miss of a stated step, not a bad decision.

## Recommended action

1. **Fix finding 1 before any live claim, and before treating 3A as a stable base for 3B.** 3B's `apply` is the same function; a meter/graph/text metric that appears after first configure has the same zero-width hole.
2. Invert the long-title test so Configure happens first. That is the regression lock.
3. Findings 2–4 are cheap.
4. Do not close `sysc-6` on the automated gate. The matrix is still the close condition the completion handover named.

### Verdict

**Proceed with named corrections.** Lifetime, D3 projection, failure policy, tabular figures, lossless invalidation, and reload acquire-before-release all match the spec. The suite is green and cannot see the one user-visible hole: **text that arrives after the first layer-shell configure never gets a layout**, so a clock and a window title can paint at width zero until the output is reconfigured.

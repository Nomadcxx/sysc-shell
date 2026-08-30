# M3 Code Quality Sweep

Date: 2026-08-31
Issue: `sysc-25`
Branch: `milestone/power` at `67dcd5b` (includes 3A–3D)
Scope: landed Milestone 3 widget code only. Correctness, security, performance, and the live Niri
matrix are out of scope. Recorded charter deviations are not candidates for reversal.

This is a ponytail pass: what to delete, unexport, or inline. Nothing was applied.

## Already declared — not findings

| Claim | Result |
|---|---|
| Icons as a font face, not rasters | Charter deviation (3D D4). Leave. |
| No extra node kinds; failure uses `Node.Tone` | Charter deviation (3D D5). Leave. |
| Graph node shipped in 3B | Charter deviation (3B D5). Leave. |
| Battery is a `Source` on the 3B sampler, not a fourth service | 3C D1. Leave. |
| `Acquire` allocates a battery history ring that `record` skips | 3C plan finding 4. Leave. |
| `Weather.RequestURL` is exported for shell reload tests | Recorded in the 3D handover. Cross-package; keep. |
| `Starts` / `Running` on Clock, Metrics, Weather | 3A design: test observability. Production `main.go` also uses `Updates` through `Registry.Clock` / `Metrics` / `Weather`. Keep. |

## Ranked cuts

`delete: Metrics.Leased is exported and has zero callers. SourceLeased is the collect path; nothing asks about a selector. Nothing replaces it. [internal/services/metrics.go:247]`

`yagni: Metrics.History is used only by metrics_test.go. Histories is the production path. Unexport; same-package tests still compile. [internal/services/metrics.go:533]`

`shrink: Registry.historyLocked is a one-line wrapper. Call r.metrics.Histories() from viewLocked. [internal/shell/registry.go:320]`

`shrink: metricFraction and metricValue re-derive a selector then call snap.Fraction / snap.Value. Inline at the two format closures. Tests that call them directly move onto formatMetric / buildMetricWidget. [internal/shell/metricwidget.go:37,47]`

## Inspected, not cut

- `leaseSet` is a struct shared by three services, not an interface. That is the 3B/3D stop condition held.
- Three `Update*` methods and three pumps in `main.go` are the same shape on different types. A generic would be a one-use abstraction.
- `formatRate`, `humaniseAge`, and `batteryDuration` look similar and are not: rates scale by 1000, ages pick one unit, remaining time is `h`+`m`.
- `KindButton` / `Node.Action` predate M3 and are what Tranche 4A consumes. Out of this sweep.
- `textWidget` wrapping meters and graphs is a name, not a layer.

## Test-only exports

| Symbol | Can die? |
|---|---|
| `Metrics.Leased` | Yes. Zero callers. |
| `Metrics.History` | Unexport, do not delete. |
| `Weather.RequestURL` | No. `package shell` tests need it. |
| `Registry.Clock` / `Metrics` / `Weather` | No. `cmd/sysc-shell` reads `Updates()` through them. |
| `Clock.Starts` / `Running` and the Metrics/Weather twins | No. Named in the 3A design. |

## Cheap deletions, if approved

1. Delete `Metrics.Leased` (~8 lines).
2. Unexport `Metrics.History`.
3. Inline `historyLocked` (~5 lines).
4. Optional: inline `metricFraction` / `metricValue` (~20 lines, test edits).

`net: about -15 lines from 1–3, about -35 if 4 is included. 0 deps.`

Do not apply until named.

# Power Plan Audit Report

Date: 2026-08-31
Auditor: independent audit agent (design + plan vs current `main`, not an implementation)
Subject: Tranche 3C — `2026-08-31-power-design.md` and `2026-08-31-power.md`

This is a plan/design audit. No 3C product code was written. The live matrix was not run and is not
a finding. The tranche remains blocked on `sysc-19`.

## Verdict

**Rebase the plan onto post-D6 `Metrics`, then wait for Task 0.** Decisions D1–D6 hold. Battery as a
`Source` on the existing sampling loop, empty-when-absent, warning as low-and-not-charging, and
provisional signatures behind a stop gate are the right shape.

The plan cannot be pasted onto current `main`. Tasks 2 and 5 still call `Acquire(Source, duration)`,
`Leased(Source)`, and `metricSource`. 3B's audit corrections changed those to `Acquire(Selector, …)`,
`SourceLeased`, and `metricSelector`. Execution would fail at the first RED step, and a "fix it until
it compiles" pass could reintroduce per-source leasing.

Do not treat this audit as permission to start 3C. `sysc-metrics v0.1.0` still has no battery API.
Task 0 is the real gate; this report only says the plan will also be wrong *after* that gate unless
it is rewritten against the selector API.

## Scope and state

- Branch: `main` at the working tree (3B merged, including D6 selector history)
- Spec: `2026-08-31-power-design.md` (D1–D6, provisional `BatterySnapshot`, Task 0)
- Plan: `2026-08-31-power.md` (7 tasks, Task 0 is the reconciliation gate)
- Against: current `internal/services/metrics.go`, `internal/shell/{metricwidget,registry,bar}.go`,
  3D design (`Tone`, icon face), `go.mod` (`sysc-metrics v0.1.0`, no `replace`)
- Blockers named by the design: `sysc-19` / `v0.2.0`, and Tranche 3D (`sysc-10`)
- Out: implementing battery; raising `sysc-metrics`; closing `sysc-9` or `sysc-19`

## Decisions — confirmed, not re-opened

| # | Decision | Result |
|---|---|---|
| D1 | Battery is a `Source` on 3B's `Metrics`, not a fourth service | **Hold.** Same library, same polling owner. |
| D2 | Fifteen glyphs on 3D's icon font | **Hold.** Depends on 3D D4. |
| D3 | Empty when absent, re-evaluated every snapshot | **Hold.** Desktop and laptop share one config. |
| D4 | Warning is low **and** not charging | **Hold.** Matches DMS. Scope sentence disagrees — finding 3. |
| D5 | Per-instance label: percent / time / rate / none | **Hold.** |
| D6 | Signatures provisional; Task 0 refuses a mismatch | **Hold.** The table's Stop rows (no `Present`, no aggregate) are the right stops. |

## Ranked sites

| Area | Verdict |
|---|---|
| Sampling ownership | **Holds.** One `run`, battery joins `collect`. No fourth goroutine is specified. |
| Lease / selector API | **Plan is pre-D6.** Finding 1. |
| History rings | **Intent holds** (battery is not graphed). Acquire still allocates a ring. Finding 4. |
| Absence | **Holds.** `formatBattery` returns `""`; empty text measures zero-wide. Task 6 tests present→absent→present with no reload. |
| Warning conjunction | **Implementation matches D4.** `!charging && charge <= WarnBelow`. `BatteryFull` counts as charging, so a full battery never warns. |
| Tone dirty | **Depends on 3D finding 4.** Glyph-in-text covers most 3C transitions; a threshold-only reload might not. |
| Task 0 | **Holds as written.** Do not weaken it. |

## Findings

Severity key: **verified** = read against current `main`; **specified** = contradiction inside the
documents; **declared** = already in the design, confirmed.

| # | Sev | Where | Defect | Consequence | Fix |
|---|---|---|---|---|---|
| 1 | Major (verified) | Plan Tasks 2 and 5 vs `internal/services/metrics.go` and `internal/shell/metricwidget.go` | Snippets use the pre-D6 API: `m.Acquire(SourceBattery, d)`, `m.Leased(SourceBattery)`, `m.Leased(SourceCPU)`, `collect`'s `m.Leased(SourceBattery)`, and `metricSource` / `"battery" → SourceBattery`. Current code: `Acquire(sel Selector, interval)`, `Leased(sel Selector)`, `SourceLeased(src Source)`, `metricSelector` returning `(Selector, bool)`. Registry tests already call `SourceLeased`. | Task 2 Step 2's expected fail (`SourceBattery` undefined) might still happen, but the tests will not compile even after the const is added. Task 5's "add a case to `metricSource`" has no function to edit. A mechanical compile fix that restored `Leased(Source)` would drop 3B's selector grain. | Rewrite every snippet: `Acquire(Selector{Source: SourceBattery}, d)`, `SourceLeased(SourceBattery)` in tests and in `collect`, `metricSelector` case `"battery": sel.Source = SourceBattery`. Keep `collect` using `SourceLeased` like the five existing branches. |
| 2 | Blocker (declared) | Design assumptions; plan Task 0; `go.mod` | `sysc-metrics v0.1.0` has no battery types. Task 0 already stops on missing `Present` or missing aggregate. | The tranche cannot execute. This audit does not move that. | Leave Task 0 as the first step after `sysc-19` tags. Re-read this report's finding 1 in the same sitting — the library bump and the selector rebase are independent. |
| 3 | Minor (specified) | Design scope vs D4 vs plan Goal | Scope: "warning tone when the charge is low and **falling**." D4 and the plan Goal: "low and **not charging**." `formatBattery` implements D4. "Falling" would require a previous sample and would miss a machine that boots already low. | An executor who implements the scope sentence writes a different widget than D4's evidence table (`low and discharging is error; low and charging is normal`). | Change the design scope sentence to D4's wording. Do not add a derivative. |
| 4 | Minor (verified) | Plan Task 2 "not recorded into a history ring"; `Metrics.Acquire` | Every successful `Acquire` creates `history[sel] = newRing(historySize)`. `record` pushes only when `snap.Value(sel)` is ok; `Value` has no `SourceBattery` arm, so the ring stays empty. | 120 unused floats per battery lease for the process lifetime. Not a behaviour defect. | Either skip ring allocation when `sel.Source == SourceBattery`, or leave it and say so. Do not invent a `Value` that would start plotting a flat line — the plan already rejected that. |
| 5 | Minor (specified) | Plan "Lanes" | "Tasks 1–2 (icons) and Task 3 (configuration) are disjoint." Task 2 is the battery *source*, not icons. | Parallelism is still real (1, 2, 3 are independent). The parenthetical misleads a dispatcher. | "Tasks 1 (icons), 2 (source), and 3 (configuration) are disjoint." |
| 6 | Minor (verified) | 3C Task 4 `formatBattery` + 3D `applyLocked` | Same hole as 3D audit finding 4: tone is a side effect. 3C's usual transitions also change the glyph rune, so text comparison often saves it. Reloading `warn-below` across the current charge can change tone (and possibly not the percent text). | A threshold-only reload might not repaint until the next percent tick. | 3D should compare `Tone` in `apply`. 3C needs no second patch if that lands first. |

## Q/A of the plan's own tests

| Required behaviour | Named check | Q/A |
|---|---|---|
| Battery lease independent of CPU/block | `TestABatteryLeaseIsIndependentOfOtherSources` | Right idea. Rewrite against `SourceLeased` (finding 1) or it never compiles. |
| Battery-only collect | `TestOnlyTheBatterySourceIsPopulated` | Holds once `collect` uses `SourceLeased(SourceBattery)`. The 3s wait is generous for a 50ms interval. |
| Widget takes the battery lease | `TestABatteryWidgetLeasesTheBatterySource` | Right. Implementation is a `metricSelector` case, not `metricSource`. |
| Absence re-evaluated | `TestBatteryAbsenceIsReEvaluatedEverySnapshot` | Holds. Present / absent / present with `UpdateMetrics` is the right order. |
| Warning conjunction | Task 4 table | Matches D4, not the scope sentence (finding 3). |
| No charge change, no redraw | evidence table | Holds for identical snapshots. Threshold-only config reload is untested (finding 6). |
| Task 0 API table | Stop on no `Present` / no aggregate | **Do not skip.** A desktop `Present: false` is success, as the plan already says. |

**Q/A verdict:** the behavioural tests are the right tests. They are written in a dialect `main` no
longer speaks. Rebasing them is cheaper than discovering it at Task 2 RED.

## Concurrency — specified, no defect in the plan

- Battery is sampled on the existing `run` goroutine. `ReadBattery` is a function, not a sampler with
  retained counters, so it does not join the `samplers` struct. That is correct if the library's
  function is stateless; Task 0 must confirm.
- `collect` currently calls `SourceLeased`, which takes `m.mu`, after `run` has released the lock.
  Adding a battery branch the same way does not introduce a new lock inversion.
- No per-frame charging animation (anti-pattern, recorded).

## Stop conditions

Task 0's Stop rows remain the stops. This audit does not add one. A device selector is still a
reconciliation outcome, not a 3C invention.

## Claims that could not be verified

| Claim | Settling evidence |
|---|---|
| Library `Charge` is a fraction 0–1 | Task 0. Plan already has a row if it is 0–100. |
| `Present` exists | Task 0 Stop if not. |
| `ReadBattery` is safe on the sampling goroutine and on a desktop | Task 0 plus the existing "desktop returns `Present` false" note. |
| 3D icon range has room for fifteen battery runes | 3D Task 3 has not run. 3C Task 1 extends `iconFaceFor`; if 3D used a closed range, Task 1 already shows the or-battery check. |

## Recommended action

1. **Do not execute 3C** until `sysc-19` tags a release and 3D has shipped tone + the icon face.
2. **Rewrite Tasks 2 and 5 against `Selector` / `SourceLeased` / `metricSelector` now**, while the
   diff is still a plan edit. Waiting until Task 0 means the reconciliation pass also has to fight
   stale snippets.
3. **Align the design scope sentence with D4** (finding 3). Findings 4–5 are one-line plan notes.
4. Rely on 3D to compare `Tone` in `apply` (finding 6). If 3D is cut down to skip that, add it in 3C
   Task 4.

Do not implement battery in the same change as the plan rebase.

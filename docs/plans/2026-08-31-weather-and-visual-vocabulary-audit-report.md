# Weather and Visual Vocabulary Plan Audit Report

Date: 2026-08-31
Auditor: independent audit agent (design + plan vs current `main`, not an implementation)
Subject: Tranche 3D — `2026-08-30-weather-and-visual-vocabulary-design.md` and
`2026-08-30-weather-and-visual-vocabulary.md`

This is a plan/design audit. No 3D product code was written. The live matrix was not run and is not
a finding.

## Verdict

**Amend before executing.** Decisions D1–D9 hold. The fetch bounds, `leaseSet` reuse, icon-font
deviation, tone-not-kind, and cuttable tooltip are the right shape.

One gap is unimplementable as written: coordinates and unit are baked into `NewWeather` at
`NewRegistry`, while the live gate requires a reload of those fields without restarting the service.
The evidence table only claims `Starts() == 1` across an *interval* change. That contradiction has to
be resolved in the design and Task 7 before anyone pastes the snippets.

Two cheaper plan-vs-`main` misses would fail Task 1 and Task 4 on contact: the `Lease` snippet
reverts 3B's `selector` field, and Task 4 expects `FontMap.Face` to be undefined.

## Scope and state

- Branch: `main` at the working tree (3A and 3B merged; 3A findings 2–4 present uncommitted)
- Spec: `2026-08-30-weather-and-visual-vocabulary-design.md` (D1–D9, evidence table, live gate)
- Plan: `2026-08-30-weather-and-visual-vocabulary.md` (11 tasks; 9–11 cuttable)
- Against: current `internal/services/{clock,metrics,leases}.go`, `internal/shell/{registry,bar}.go`,
  `internal/render/fontmap.go`, `internal/ui/tree.go`, Tranche 4A surface notes
- `sysc-8` is closed. 3B's `leaseSet` is on `main`. Assumption 1 holds.
- Out: implementing weather, icons, tone, or tooltip; the live matrix; closing `sysc-10`

## Decisions — confirmed, not re-opened

| # | Decision | Result |
|---|---|---|
| D1 | Open-Meteo, configured coordinates, no geocoding | **Hold.** Charter privacy gate. |
| D2 | Concrete `services.Weather` on shared `leaseSet` | **Hold.** Do not introduce an interface. |
| D3 | Bounded fetch / retry / backoff copied from DMS | **Hold.** Numbers are in the plan. Connect-timeout split is finding 5. |
| D4 | Icons as an embedded font face | **Hold.** Recorded charter deviation; 3C depends on it. Do not revert to raster. |
| D5 | `Node.Tone`, no new kinds; staleness in the text | **Hold.** |
| D6 | Tooltip is OSD-shaped, not 4A's panel+shield | **Hold.** |
| D7 | Placement is 4A's D5 rule | **Hold.** |
| D8 | Dwell timer signals the owner; never a proxy | **Hold.** |
| D9 | Tooltip is last and cuttable | **Hold.** |

## Ranked sites

| Area | Verdict |
|---|---|
| Service lifetime | **Holds** for interval: `Acquire` / last-`Release` / rearm match Clock. Coordinates and unit do not. Finding 1. |
| Lease bookkeeping | **Snippet is stale.** Finding 2. |
| Fetch discipline | **Holds** as specified: 6s client timeout, per-request context, body cap, min interval, backoff, last-good retained, edge-triggered stderr. |
| Icon resolution | **Plan Task 4 is a replace, not an add.** Finding 3. |
| Tone / dirty | **Painter path is specified.** `apply` does not compare `Tone`. Finding 4. |
| Tooltip vs 4A | **Not a defect.** 4A ships panels, not OSD. If 4A Overlay helpers exist when Task 9 starts, reuse construction, not the panel shape (D6). Assumption 2 already says this. |
| Validation | **Holds.** Cross-section `weather` item without a `weather` block names `weather.latitude`. |

## Findings

Severity key: **verified** = read against current `main`; **specified** = contradiction inside the
documents.

| # | Sev | Where | Defect | Consequence | Fix |
|---|---|---|---|---|---|
| 1 | Major (specified + verified) | Design live gate; evidence table; plan Task 7; `NewRegistry` snippet | `NewWeather(lat, lon, unit)` is called once from `NewRegistry`. `PrepareConfig` rebuilds bars and re-acquires leases at the new interval; it never mutates the service. The evidence table only requires `Starts() == 1` across an *interval* change. The live gate and the design's hardware list require reload of coordinates, unit **and** interval without restart. Clock can ignore this because it has no identity parameters. Weather's identity *is* the request. | A reload that changes city or `fahrenheit` keeps fetching the old URL forever. A process that started with no `weather` block constructed `NewWeather(0, 0, …)` and cannot pick up a later block. Live gate item 7 is unimplementable. | Add `Weather.Reconfigure(lat, lon, unit)` under the existing mutex: no-op when unchanged, otherwise write the fields and rearm a fetch. Call it from `PrepareConfig`/`Commit` with the candidate config. Do **not** replace the `*Weather` pointer — live leases hold it. Interval stays a lease concern. Amend the evidence table to name coordinates and unit, not only interval. |
| 2 | Major (verified) | Plan Task 1 `Lease` snippet vs `internal/services/clock.go` | The snippet replaces the whole struct and names `source Source`. Current `Lease` has `selector Selector` (3B D6). `Release` already switches on `clock` / `metrics`. | Pasting the snippet reverts selector history. Task 1 would not compile as a field add (`weather` is fine; `source` is not). | Show only the added field `weather *Weather` and the `Release` arm. Leave `selector` untouched. |
| 3 | Minor (verified) | Plan Task 4 Step 2; `internal/render/fontmap.go:71` | Expected fail is "`Face` undefined". `FontMap.Face` already exists and caches per rune through `outlineFaceForRune`. | The RED step is wrong. The test should fail because an icon rune resolves to a system face (or notdef), not because of a missing method. | Rewrite Step 2 as FAIL on resolution. Step 3 inserts `iconFaceFor` *ahead of* the system query inside the existing `Face`. |
| 4 | Minor (verified) | Plan Task 5 / 7; `internal/shell/bar.go` `applyLocked` | `apply` compares `Text`, `Value`, `Absent`, `Values`. Task 7 writes `node.Tone` as a side effect, the same pattern 3B's meters used for `Value` before that comparison existed. Weather's three states mostly change text as well (`-` / `weather unavailable` / temperature / age). | A later Tone-only change (3C threshold reload with an unchanged glyph, or a future widget) submits no frame. Cheap to close here. | Compare `Tone` beside `Value` in `applyLocked` in the same task that introduces the field. |
| 5 | Minor (specified) | D3 "3s connect and 6s total"; plan `http.Client{Timeout: connectAndReadBudget}` | The client timeout and the request context are both 6s. There is no `DialContext` deadline of 3s. DMS's `--connect-timeout 3 --max-time 6` is not reproduced. | A stalled connect can occupy the full 6s. The shell still paints; the bound is weaker than D3's wording, not unbounded. | Either add a 3s `DialContext` on the client's transport, or amend D3 to "6s total, which is also the connect budget in Go". Do not invent a second timeout that the tests do not assert. |
| 6 | Minor (verified) | Plan Task 5 paint fixture | Snippet uses `newPaintFixture` and `canvas.pixels`. 3B shipped `newTestCanvas` / `testStyle`; `Canvas.Pix` is the field. The plan already says to use the real names. | A literal paste fails to compile. | Write the test against `newTestCanvas` and `Pix`. |

## Q/A of the plan's own tests

| Required behaviour | Named check | Q/A |
|---|---|---|
| Two bars, one weather goroutine | `TestTwoBarsShareOneWeatherServiceAndOneReading` | Right assertion (`Starts() == 1`, both bars dirty). Does not prove one *fetch*; that is Task 2. |
| Identical reading, no dirty | `TestAnUnchangedReadingChangesNothing` | Holds **if** finding 4 is applied or the fixture only changes text. |
| No widget, service stopped | `TestAConfigWithNoWeatherWidgetLeavesTheServiceStopped` | Holds. Matches Clock. |
| Reload does not restart | evidence table, live gate 7 | Interval is tested by Clock's existing pattern. Coordinates/unit have **no** test and no API (finding 1). Add `TestAnAcceptedReloadPicksUpNewCoordinatesWithoutRestarting`. |
| Fetch timeout / cap / backoff | Task 2 httptest cases | Shape is right. Assert the delay cap and the min-fetch floor explicitly; do not only comment them. |
| Error tone paints error colour | `TestErrorToneTextPaintsInTheErrorColour` | Right idea. Use `Pix` (finding 6). `ProofStyle.Error` must not reuse `AccentOn` — the plan already says this; `bar.go` currently maps `theme.Error` onto `AccentOn` for toggled chrome, so the new field is necessary. |
| Icon rune uses project face | Task 4 | Holds once Step 2 is a resolution failure, not a missing `Face`. |

**Q/A verdict:** lifetime and fetch tests will catch the defects they name. Nothing in Tasks 1–8
would catch a coordinate reload leaving the old URL. That is the same class of hole as 3A's inverted
Configure/apply tests: the suite can be green while the live gate item is false.

## Concurrency — specified, no defect in the plan

- One fetch goroutine; `Updates()` newest-wins, capacity 1, never closed.
- `Release` / `Close` wait on `done` outside the lock, Clock-shaped.
- Dwell timer sends; owner goroutine creates and destroys the Overlay surface.
- `UpdateWeather` publishes after releasing `r.mu`.

## Stop conditions

None triggered by this audit. No second Wayland goroutine is specified; no geocoding; no widget
interface; no new module; tooltip is Overlay with keyboard none.

## Claims that could not be verified

| Claim | Settling evidence |
|---|---|
| Open-Meteo still answers the planned URL with no key | One `curl` at execution, not at plan audit. |
| Authored icon font coverage of the eight WMO buckets | Font does not exist yet (plan Task 3). |
| 4A Overlay helpers to reuse | 4A code is not a 3D prerequisite; assumption 2 covers the branch. |

## Recommended action

1. **Amend the design and Task 7 for `Weather.Reconfigure`** (finding 1) and add the reload test.
   This is the only change that is expensive after Task 1 lands.
2. **Fix the Task 1 `Lease` snippet** so it cannot revert `selector` (finding 2).
3. **Retarget Task 4** at replacing `Face`'s body (finding 3).
4. **Compare `Tone` in `applyLocked`** when the field is added (finding 4).
5. Pick one resolution for the 3s connect bound (finding 5). Finding 6 is a paste note.

Do not start Task 1 until 1 and 2 are in the plan. Findings 3–6 can land in the same docs commit.
Do not implement 3D in the same change as the amendment.

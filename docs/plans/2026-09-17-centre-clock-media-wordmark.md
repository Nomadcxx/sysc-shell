# Centre clock, media, and wordmark implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the default bar centre one time/date composition followed by a fixed-centre SYSC wordmark and optional media, without changing the existing media, rendering, or interaction contracts.

**Architecture:** Change only the default `config.Bar.Center` shape, carry the existing clock and media widgets through their current builders, and filter absent media at the bar's retained-section seam. Add one narrow `ui.ArrangeBar` branch for a centre section with exactly one top-level `KindWordmark`; all other centre sections keep the current absolute-centre and collision path.

**Tech Stack:** Go standard library, existing retained `internal/ui` tree, existing `internal/shell` media/art services, and table/focused tests.

---

Date: 2026-09-17. Issue: `sysc-314`. Executes [`2026-09-16-centre-clock-media-wordmark-design.md`](2026-09-16-centre-clock-media-wordmark-design.md).

## Invariants and non-goals

- The default centre is exactly `[group(clock time, clock date)] [wordmark] [media]`.
- The group is one capsule with two tabular clock values in time-before-date order.
- The media node is present in configuration but has zero layout, paint, hit, and tooltip surface while unavailable.
- The wordmark's measured centre stays at the content-band centre for fitting combinations of side widths, clock/date widths, media presence, and media title length.
- Existing media art, status glyphs, title marquee/max-width, `panel:media`, and wordmark `panel:control-center` behavior remain intact.
- The existing `ArrangeBar` path remains unchanged for centre sections without exactly one top-level `KindWordmark`.
- No generic flex/alignment API, new media protocol, player discovery, seeking, volume control, or overflow behavior from `sysc-313` is added here.

## Verified code anchors

Re-check names rather than relying on line numbers if the base moves:

| Responsibility | File | Current seam |
|---|---|---|
| Default bar shape | `internal/config/config.go` | `Default`, `Bar.Center`, `knownItems` |
| Item decoding/validation | `internal/config/load.go` | `wireItem`, `resolveItem`, existing one-level groups |
| Clock construction | `internal/shell/widget.go` | `buildWidgets`, `clockBoundaries`, `clockWidthFloor` |
| Media construction/state | `internal/shell/mediawidget.go` | `buildMediaWidget`, `refreshMediaWidget`, `hideWhenAbsent` |
| Visibility before layout | `internal/shell/bar.go` | `sections`, called by layout, render, action bounds, and hit testing |
| Owner-side re-layout | `internal/shell/bar.go` | `applyLocked`, `relayoutLocked`, `layoutLocked` |
| Anchor/collision | `internal/ui/bar.go` | `ArrangeBar`, `sectionWidth`, `placeSection` |
| Wordmark size/semantics | `internal/ui/layout.go`, `internal/ui/tree.go` | `KindWordmark`, `ImageW/ImageH`, `Action`, `Name`, `Role` |

## Task order

Every production change follows a red-green cycle: add one focused assertion, run it and observe the expected pre-change failure, implement the smallest responsible change, then rerun the focused package check.

### Task 1: Specify and build the default centre shape

**Files:**

- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

Update the existing default-vocabulary test to assert three centre items: a `group` with two nested clocks (`15:04` then `Mon 2 Jan`, each with a positive minute boundary), one `wordmark`, and one trailing `media`. Keep the existing left/right default assertions and prove the media item is accepted by the vocabulary.

Run before the implementation:

```bash
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/config -run 'TestDefaultVocabularyShipsBothClocksAndBothNiriWidgets'
```

Expected: FAIL because the current default has two top-level clocks and no media item.

Change only `Default().Bar.Center` to the approved group/wordmark/media shape. Do not change the wire schema: groups and media already decode and validate. Rerun the focused test; expect PASS.

### Task 2: Carry the clock floor through the group and initialize media absent

**Files:**

- Modify: `internal/shell/widget_test.go`
- Modify: `internal/shell/widget.go`
- Modify: `internal/shell/mediawidget.go`
- Test: `internal/shell/widget_test.go`

Add a builder test over `config.Default().Bar.Center` that proves the first widget is one capsule containing one row with two clock members, the second is the bare wordmark, the third is a capsuled media widget, and the nested clocks retain the existing `clockWidthFloor` while requesting tabular figures. Assert the media root is marked absent before its first state refresh, so initial configure cannot reserve a phantom media icon.

Run before the implementation:

```bash
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/shell -run 'TestDefaultCentreBuildsTimeDateGroupAndMedia'
```

Expected: FAIL because the current default tree has separate clocks, nested clocks do not inherit the floor, and a newly built media widget is not initially absent.

Implement the smallest builder change: propagate the already-existing floor through the one recursive group build used by a wordmark-bearing centre, and set the media row's initial `Absent` state. Preserve `buildWidgets`'s public test seam and all existing item wrapping rules. Rerun the focused test and the existing media widget tests.

### Task 3: Collapse absent widgets before every bar consumer

**Files:**

- Modify: `internal/shell/mediawidget_test.go`
- Modify: `internal/shell/bar_test.go`
- Modify: `internal/shell/bar.go`
- Test: `internal/shell/mediawidget_test.go`, `internal/shell/bar_test.go`

Add a regression test that applies an unavailable media state after a previously-laid-out present state and proves the media node is omitted from `sections`, `renderViewLocked`, `actionBounds`, and `hitLocked`; the neighbouring wordmark has no gap from the absent node. Add the reverse transition to prove the media node returns with its existing action and bounds after a present state.

Run before the implementation:

```bash
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/shell -run 'TestAbsentMedia|TestMediaWidgetIsAbsentWithNoPlayer'
```

Expected: FAIL because `sections` currently returns every widget even when its root is marked absent, leaving stale layout and hit surfaces.

Filter only `textWidget`s marked `hideWhenAbsent` and `node.Absent` in the retained section list. Apply the same visibility rule to grouped members without altering `widgets()`, because `applyLocked` must still refresh an absent media widget. Ensure tooltip lookup also skips absent roots/members. Keep the operation under the bar's existing lock ownership and do not add a second media-state cache.

Rerun the focused shell tests. Expect an absent media item to consume no width and a present item to use the existing `buildMediaWidget` tree.

### Task 4: Add and implement the anchored-centre layout path

**Files:**

- Modify: `internal/ui/bar_test.go`
- Modify: `internal/ui/bar.go`
- Test: `internal/ui/bar_test.go`

Add a table test with a fixed text measurer and a `KindWordmark` carrying an explicit image box. Vary the preceding time/date width, following media width (including zero), left/right section widths, and a bounded long-title width. For every fitting case assert:

- the wordmark centre is the content-band centre;
- changing any surrounding width does not move the wordmark;
- preceding nodes end before the mark and following nodes start after it;
- surrounding budgets never produce negative bounds and side sections truncate before the mark.

Run before the implementation:

```bash
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/ui -run 'TestAnchoredWordmark'
```

Expected: FAIL because the current path centres the whole variable-width centre section.

Add a narrow helper/path in `ArrangeBar` selected only when the centre contains exactly one top-level `KindWordmark`. Measure the mark and each preceding/following subsection. Place the mark at the content-band centre; place preceding nodes right-aligned into the space before it and following nodes left-aligned into the space after it. Give the centre composition priority over left/right section width, cap every side with the existing `placeSection` truncation semantics, and retain the current oversized-centre behavior for a mark or composition that cannot fit. Do not add fields to `Node` or a generic alignment mode.

Rerun the new table test and all existing `internal/ui` bar tests. The non-wordmark tests must remain unchanged and passing.

### Task 5: Prove the end-to-end default composition and stable actions

**Files:**

- Modify: `internal/shell/bar_test.go`
- Modify: `internal/shell/mediawidget_test.go`
- Test: `internal/shell/bar_test.go`, `internal/shell/mediawidget_test.go`

Add one shell integration test using `config.Default()` and a configured bar. Apply a clock snapshot with no player, render, then apply a present player with art and a long title, render again, and finally cross a minute boundary. Assert the wordmark centre is unchanged in all states, the group remains one capsule, the media title stays within its configured max width and retains marquee intent, and the absent state leaves no media section node.

Add/retain focused assertions for:

- wordmark `Action == panelControlCenterAction`, `Name == "Control centre"`, `Role == "button"`;
- media `Action == panelMediaAction`, `Name == "Media"`, `Role == "button"`;
- playing/paused/stopped fallback glyphs and resolved art;
- media absent/present transitions and no picker/list subtree.

Run before any integration-only production adjustment:

```bash
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/shell -run 'TestDefaultCentre|TestMediaWidget|TestWordmarkRightClickOpensControlCentre'
```

Expected: the new default-centre test fails until Tasks 1–4 are present; existing action/media tests identify any accidental regression in the retained seams. No media protocol or Registry change is expected.

Implement only fixes exposed by these assertions at the owning builder/visibility/layout seam, then rerun the focused shell package tests.

### Task 6: Repository and live proof

**Files:**

- No production files beyond Tasks 1–5.
- Create: `docs/plans/YYYY-MM-DD-<topic>-completion-handover.md` only after implementation and gates.

Run the smallest sufficient affected-package proof with capped parallelism:

```bash
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/config
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/ui
timeout 90s env GOMAXPROCS=2 go test -p 2 -count=1 ./internal/shell
gofmt -w internal/config/config.go internal/config/config_test.go internal/shell/bar.go internal/shell/bar_test.go internal/shell/mediawidget.go internal/shell/mediawidget_test.go internal/shell/widget.go internal/shell/widget_test.go internal/ui/bar.go internal/ui/bar_test.go
test -z "$(gofmt -l internal/config/config.go internal/config/config_test.go internal/shell/bar.go internal/shell/bar_test.go internal/shell/mediawidget.go internal/shell/mediawidget_test.go internal/shell/widget.go internal/shell/widget_test.go internal/ui/bar.go internal/ui/bar_test.go)"
go vet -p 2 ./...
git diff --check
git diff --exit-code -- go.mod go.sum
```

The repository's full `go test -race ./...` form is machine-refused and the unrestricted `go test ./...` form is also blocked; use the affected-package commands above and record that limitation rather than claiming the blocked gate.

For the bare-metal gate, use the existing Niri environment on `DP-1` (3440×1440, scale 1.0): capture the default bar with no player, with a player, and with a long title; cross a clock minute and vary surrounding state; inspect `niri msg -j layers` before and after so no bar surface leaks. Record the exact binary, surface state, and any unexercised second-output behavior in the completion handover. Do not claim `sysc-313` overflow or a second output from this ticket.

Commit the completion handover and register row only with its fresh evidence, then update/close `sysc-314` from `/home/nomadx/sysc-shell` with the focused checks and live result.

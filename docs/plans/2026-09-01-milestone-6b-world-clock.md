# Milestone 6B Incremental Views and World Clock Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add drop-safe incremental views, bounded scheduling, lists, and drag/reorder, then prove them with a feature-rich World Clock plugin.

**Architecture:** Keep one runtime and one immutable current tree per view. Apply keyed subtree patches only at a matching base revision; request a snapshot after loss. Extend the retained UI only with the list and drag behavior World Clock consumes.

**Tech Stack:** Go standard library, M6A `plugin/v1` and `internal/plugin`, existing retained UI, Niri-first shell surfaces.

**Design:** `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`

---

### Task 1: Apply keyed subtree patches by revision

**Files:**
- Modify: `plugin/v1/message.go`
- Modify: `plugin/v1/node.go`
- Modify: `internal/plugin/view.go`
- Test: `internal/plugin/view_test.go`

**Step 1:** Add failing tests for a valid replacement, multiple independent replacements, duplicate target, missing target, duplicate key in replacement, wrong base revision, stale revision, and a resync request emitted once until a snapshot arrives.

**Step 2:** Implement copy-on-write keyed replacement. Keep the previous immutable tree when any operation fails; do not partially apply a patch.

**Step 3:** Run `go test -race -count=1 ./plugin/v1 ./internal/plugin -run 'Patch|Revision|Resync' -v`.

**Step 4:** Commit `feat(plugin): apply revisioned view patches`.

### Task 2: Bound inbound traffic and redraw publication

**Files:**
- Create: `internal/plugin/inbound.go`
- Create: `internal/plugin/schedule.go`
- Test: `internal/plugin/inbound_test.go`
- Test: `internal/plugin/schedule_test.go`

**Step 1:** Write deterministic clock tests for a 120-update burst, 60/s sustained allowance, discard before JSON decode after exhaustion, one overwrite slot per view, ordered control messages capped at 32, 30 Hz publication, and plugin degradation after repeated violations.

**Step 2:** Implement token accounting and newest-wins view slots with injected time. Keep control replies, shutdown, and resync separate from disposable view work.

**Step 3:** Run `go test -race -count=1 ./internal/plugin -run 'Inbound|Schedule|Rate|Coalesce' -v`.

**Step 4:** Commit `feat(plugin): bound update traffic`.

### Task 3: Measure and enforce layout budgets

**Files:**
- Modify: `internal/plugin/prepare.go`
- Test: `internal/plugin/prepare_test.go`

**Step 1:** Add an injected-clock test for one clean layout, an 8 ms overrun, three overruns within ten seconds, degradation, suppression of queued work, and recovery after a clean snapshot or restart.

**Step 2:** Add a fairness test: one flooding plugin cannot keep a second plugin's only pending job out of the fixed worker queue.

**Step 3:** Implement per-plugin pending ownership and overrun accounting. Do not try to interrupt layout; node/depth limits bound one call.

**Step 4:** Run `go test -race -count=1 ./internal/plugin -run 'Prepare|Budget|Fair' -v`.

**Step 5:** Commit `feat(plugin): enforce layout budgets`.

### Task 4: Add bounded lists and drag/drop primitives

**Files:**
- Modify: `plugin/v1/node.go`
- Modify: `internal/ui/tree.go`
- Modify: `internal/ui/column.go`
- Modify: `internal/ui/scroll.go`
- Create: `internal/ui/drag.go`
- Modify: `internal/shell/panelhost.go`
- Test: `internal/ui/drag_test.go`
- Test: `internal/ui/scroll_test.go`
- Test: `internal/shell/panelhost_test.go`

**Step 1:** Write table tests for a fixed-height list, viewport clipping, keyboard scrolling, drag start after threshold, insertion-zone hit slop, accepted and rejected drag types, cancel, drop payload, and hot-close during a drag.

**Step 2:** Add `KindDragSource` and `KindDropZone` only if the existing hit/focus model cannot represent those two consumers. Keep drag state in `PanelHost`, not in wire nodes.

**Step 3:** Reject drag and list controls in bar and tooltip views. Require an accessible name for a drag handle.

**Step 4:** Run `go test -race -count=1 ./internal/ui ./internal/shell -run 'Drag|Drop|Scroll|List|Panel' -v`.

**Step 5:** Commit `feat(ui): add list reordering controls`.

### Task 5: Complete per-output view lifecycle

**Files:**
- Modify: `internal/plugin/runtime.go`
- Modify: `internal/shell/pluginhost.go`
- Modify: `internal/shell/pluginwidget.go`
- Test: `internal/shell/pluginhost_test.go`

**Step 1:** Add tests for two outputs with separate view IDs and instance settings over one PID, focused-output context on events, connector rename, hot-unplug close, replug open, and stale events from a closed generation.

**Step 2:** Implement host-issued view generations. Copy output context into messages and ignore late work after close.

**Step 3:** Run `go test -race -count=1 ./internal/plugin ./internal/shell -run 'Output|View|Generation|Hotplug' -v`.

**Step 4:** Commit `feat(plugin): scope views to outputs`.

### Task 6: Ship the World Clock reference plugin

**Files:**
- Create: `plugins/reference/worldclock/manifest.json`
- Create: `plugins/reference/worldclock/clock.go`
- Create: `plugins/reference/worldclock/view.go`
- Test: `plugins/reference/worldclock/clock_test.go`
- Test: `plugins/reference/worldclock/view_test.go`
- Create: `cmd/sysc-plugin-world-clock/main.go`

**Step 1:** Write tests for IANA validation with `time.LoadLocation`, duplicate rejection, add/remove confirmation state, ordered persistence, drag reorder at every insertion index, UTC offset formatting, local 12/24-hour format, and once-per-second patches that touch time nodes only.

**Step 2:** Implement one service state, bar opener, and attached panel with input, scroll, live rows, confirmation controls, and drag handles. Store the ordered zone list through the host state API.

**Step 3:** Run `go test -race -count=1 ./plugins/reference/worldclock ./cmd/sysc-plugin-world-clock` and `go build ./cmd/sysc-plugin-world-clock`.

**Step 4:** Commit `feat(plugin): add World Clock reference plugin`.

### Task 7: Prove overload and World Clock behavior

**Files:**
- Create: `tests/integration/plugin_update_gate_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Add fake-plugin cases for a tight valid update loop, patch loss, continuous snapshots, a depth-16 maximum tree, depth 17 rejection, 1,024 nodes, 1,025 nodes, and a measured layout overrun.

**Step 2:** Add a two-output World Clock case that reorders zones in one panel and observes both bar views without a second process.

**Step 3:** Run `go test -race -count=1 ./...` and `go vet ./...`.

**Step 4:** Close `sysc-70` with the gate evidence, flush beads, and commit `test(plugin): prove bounded incremental views` with `.beads/issues.jsonl`.

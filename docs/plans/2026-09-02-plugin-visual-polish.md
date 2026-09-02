# Plugin Visual Polish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Plugin bar widgets, attached panels, and the Settings Plugins list use the same chrome language as Noctalia's official plugins and DMS pills, then a second pass gives Timer and World Clock distinct Hallmark voices.

**Architecture:** Host maps existing v1 kinds and `Tone` onto fill roles, padded-row cards, and drag-only drop gaps. Reference plugin trees stop putting controls on the bar. No `fontSize` or colour on the wire.

**Tech Stack:** Go, `plugin/v1`, `internal/ui`, `internal/render`, existing theme tokens. No new module, no CGO, no icon-font expansion in T1.

**Design:** [2026-09-02-plugin-visual-polish-design.md](2026-09-02-plugin-visual-polish-design.md)

**Research:** [2026-09-02-plugin-visual-polish-research.md](2026-09-02-plugin-visual-polish-research.md)

**Do not implement until `sysc-66` is closed.** Work in a dedicated worktree, not on `milestone/plugin-host` while M6C–F are open. T2 does not start until T1's Timer and World Clock view tests pass.

---

## Tranche 1 — prior art

### Task 1: Button and drag-handle fills from Tone and kind

**Files:**
- Modify: `internal/render/paint.go`
- Test: `internal/render/paint_test.go`

**Step 1: Write the failing test**

```go
func TestPaintButtonFillFollowsToneAndKind(t *testing.T) {
	cases := []struct {
		name string
		node ui.Node
		want func(ProofStyle) Color
	}{
		{"primary", ui.Node{Kind: ui.KindButton, Text: "Start"}, func(s ProofStyle) Color { return s.Accent }},
		{"destructive", ui.Node{Kind: ui.KindButton, Text: "Reset", Tone: ui.ToneError}, func(s ProofStyle) Color { return s.Error }},
		{"grip", ui.Node{Kind: ui.KindDragSource, Text: ""}, func(s ProofStyle) Color { return Color{} }},
	}
	// rasterise each node; assert the fill of the first interior pixel matches want(style)
}
```

**Step 2:** Run `go test -race -count=1 ./internal/render -run PaintButtonFill -v`. Expected: FAIL because `KindButton` and `KindDragSource` share `style.accent()`.

**Step 3:** Split the paint case. Primary button keeps accent fill + `buttonText`. Destructive uses `style.Error` + on-error text. Drag source paints no fill (or a hairline `Track`) and foreground marks.

**Step 4:** Re-run the test. Expected: PASS.

**Step 5:** Commit `fix(render): map plugin controls to primary, destructive, and ghost fills`

---

### Task 2: Padded panel rows paint as cards; drop zones keep Height

**Files:**
- Modify: `internal/plugin/view.go` (`convertNode` copies `Height` for every node, not only lists)
- Modify: `internal/render/paint.go` (padded `KindRow` in a panel)
- Modify: `internal/ui/drag.go` / `internal/shell/panelhost.go` as needed so an active matching drag is visible to paint
- Test: `internal/plugin/view_test.go`
- Test: `internal/render/paint_test.go`
- Test: `internal/ui/drag_test.go`

**Step 1:** Failing tests: convert preserves `Height: 3` on a drop zone; a row with `Padding: 6` fills with `Capsule`; a drop zone with no active drag paints nothing; with an accepted drag it paints a `Track` bar of that height.

**Step 2:** Run `go test -race -count=1 ./internal/plugin ./internal/render ./internal/ui -run 'DropZone|PaddedRow|ConvertHeight' -v`. Expected: FAIL.

**Step 3:** Minimal convert + paint. Do not add `KindCard`. Ceiling: toolbar rows that set `Padding >= 6` also card (design D6).

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(ui): paint padded plugin rows and drag insertion gaps`

---

### Task 3: Timer trees — readout on the bar, instrument on the panel

**Files:**
- Modify: `plugins/reference/timer/view.go`
- Test: `plugins/reference/timer/view_test.go` (create if missing)

**Step 1:** Failing tests: `BarTree` has no child with text `Start`, `Pause`, or `Reset`; it does contain the remaining time with `Tabular`; `PanelTree` has a padded inner row, duration field, Reset at `ToneError`, and Start/Pause as a normal button.

**Step 2:** Run `go test -race -count=1 ./plugins/reference/timer -run Tree -v`. Expected: FAIL on extra bar buttons.

**Step 3:** Rewrite `BarTree` / `PanelTree` only. Do not change countdown logic.

**Step 4:** Re-run plus `go test -race -count=1 ./plugins/reference/timer ./cmd/sysc-plugin-timer`. Expected: PASS.

**Step 5:** Commit `fix(plugin): move Timer controls off the bar`

---

### Task 4: World Clock trees — filled rows, inter-row gaps, inline confirm

**Files:**
- Modify: `plugins/reference/worldclock/view.go`
- Test: `plugins/reference/worldclock/view_test.go`

**Step 1:** Failing tests: bar has no `KindButton` labelled with the zone as a second control if a single time readout suffices; panel list children alternate drop zone / padded row; drop zones are not inside the zone row; pending remove replaces the row's trailing control with confirm/cancel, not a footer appended after the list; drag source has no `Text: "="`.

**Step 2:** Run `go test -race -count=1 ./plugins/reference/worldclock -run Tree -v`. Expected: FAIL.

**Step 3:** Rewrite `PanelTree` / `BarTree`. Keep `TimePatch` keys (`time`, `time:Zone`).

**Step 4:** Re-run plus existing World Clock tests. Expected: PASS.

**Step 5:** Commit `fix(plugin): present World Clock rows as a reorderable list`

---

### Task 5: Manager rows and failure chrome

**Files:**
- Modify: `internal/shell/popout_plugins.go`
- Modify: `internal/shell/pluginwidget.go`
- Modify: `internal/shell/pluginhost.go` (`pluginPanelError`)
- Test: `internal/shell/popout_plugins_test.go`
- Test: `internal/shell/pluginwidget_test.go`

**Step 1:** Failing tests: manager title line is the plugin name, not a capabilities dump; placeholder text is the plugin name, not `"!"`; failed panel Retry is normal, Disable is `ToneError`.

**Step 2:** Run `go test -race -count=1 ./internal/shell -run 'PluginCard|Placeholder|PanelError' -v`. Expected: FAIL.

**Step 3:** Minimal tree changes. Keep enable/retry/rescan actions.

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `fix(shell): present plugin manager and failure slots as rows`

---

### Task 6: T1 gate

**Files:** none new.

**Step 1:** `gofmt -w . && test -z "$(gofmt -l .)"`

**Step 2:** `go vet ./... && go test -race -count=1 ./... && git diff --exit-code -- go.mod go.sum`

**Step 3:** Close the T1 bead with that evidence. Flush beads.

**Step 4:** Commit `test(plugin): prove prior-art plugin chrome` including `.beads/issues.jsonl` if the close landed there.

---

## Tranche 2 — design principles

Do not start until Task 6 is closed.

### Task 7: Distinct panel voices

**Files:**
- Modify: `plugins/reference/timer/view.go`
- Modify: `plugins/reference/worldclock/view.go`
- Modify: `plugins/reference/notes/view.go` if Notes exists and still shares the Gap-8 list
- Test: matching `view_test.go` files

**Step 1:** Failing tests: Timer panel root gap/padding differ from World Clock; Timer has one card child; World Clock has a list of padded rows and no instrument card around the whole list.

**Step 2:** Run the view tests. Expected: FAIL if T1 left them isomorphic.

**Step 3:** Smallest tree delta that satisfies D12. No new tokens.

**Step 4:** Commit `fix(plugin): separate Timer and World Clock panel voices`

---

### Task 8: Undo or keep inline confirm

**Files:**
- Modify: `plugins/reference/worldclock` (and Notes if it confirms delete)
- Test: corresponding tests

**Step 1:** If notification actions can carry Undo, failing test: remove is immediate and an undo action restores the zone; no confirm buttons. If not, skip this task and keep D8 inline confirm.

**Step 2:** Implement only if Step 1's probe is true. Do not add a modal.

**Step 3:** Commit `fix(plugin): make zone removal reversible` or skip with a bead comment naming the missing notify action.

---

### Task 9: T2 gate

Same commands as Task 6. Close the T2 bead. Commit `test(plugin): prove plugin chrome voices` with beads.

---

## Non-goals in this plan

Icon-font PUA expansion, v1.1 tones, plugin store, Control Center tiles, live grim (owner after M6 live gate).

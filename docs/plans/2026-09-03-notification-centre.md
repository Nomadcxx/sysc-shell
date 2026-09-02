# Notification Centre Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a first-party DMS-parity notification centre (bar bell + `PanelNotifications`) and unclip live toasts so cards measure their content and sit clear of the bar.

**Architecture:** `sysc-notify` still owns D-Bus, expiry, and history. The shell projects that snapshot onto `PanelNotifications` and the existing per-output Overlay toast host. No new node kinds: `KindCapsule` stroke, optional `KindMeter` height, catalogue glyphs. Relays stay off the Wayland owner.

**Tech Stack:** Go, existing panel host / IPC / `icons.Worker` / icon font authoring (`internal/render/icons/build.py`). No new module pin until `history.remove` tags.

**Design:** `docs/plans/2026-09-03-notification-centre-design.md` (D1–D17). bd: design `sysc-151`, execution `sysc-152`. Closes `sysc-150` when header commands reach the client.

## Global Constraints

- Run `bd` from `/home/nomadx/sysc-shell`. Never `bd export` (it clobbers JSONL).
- Do not run `go test -race` on this machine (OOM). Use `timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <name>`.
- `gofmt -w` touched files; `go vet` the touched package; screen commit messages for `agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark`.
- Do not implement a control centre, a notify daemon, or a tight toast Wayland surface.
- Commit `.beads/issues.jsonl` with the code it describes.
- First toast paint may load fonts; tests that measure must install a `TextRenderer` or a stub `MeasureText`.

---

### Task 0: Reconcile with landed main

**Files:**
- Read: `docs/plans/2026-09-03-notification-centre-design.md`
- Inspect: `internal/shell/panel.go`, `internal/shell/panelhost.go`, `internal/shell/toasthost.go`, `internal/shell/toastlayout.go`, `internal/shell/popout_notifications.go`, `internal/shell/notifycard.go`, `internal/ipc/server.go`, `internal/config/config.go`, `internal/ui/column.go`

**Step 1:** Confirm `sysc-152` is in progress (`bd show sysc-152`).

**Step 2:** Confirm APIs this plan names still exist: `PanelID` / `parsePanelName` / `panelTargetSize` / `panelTree`, `ui.ContentHeight`, `monitorSurfaceHeight`, `toastHost.cardHeight`, `centerTree`, `sendNotify`, `knownItems`, `knownPanels`.

**Step 3:** Confirm live outputs with `niri msg -j outputs` still include DP-1 and DP-3.

**Step 4:** Commit only a plan correction if reconciliation changed this document.

---

### Task 1: Unclip toasts (measure height, clear the bar)

The live toast is clipped. `cardHeight` is 96 for every record, and layout starts 12 px from the output edge under a 48 px top bar.

**Files:**
- Modify: `internal/shell/toastlayout.go`
- Modify: `internal/shell/toasthost.go`
- Modify: `internal/shell/toastlayout_test.go`
- Modify: `internal/shell/toasthost_test.go`

**Step 1: Write the failing tests**

```go
func TestToastLayoutClearsTheBar(t *testing.T) {
	geom := toastGeometry{OutputW: 1920, OutputH: 1080, Corner: toastTopRight, BarZone: 48}
	rects, _ := toastLayout(geom, []int{80})
	if rects[0].Y < 48+toastMargin {
		t.Fatalf("card Y = %d, want >= %d (bar + margin)", rects[0].Y, 48+toastMargin)
	}
}

func TestToastCardHeightFollowsContent(t *testing.T) {
	root := &ui.Node{Kind: ui.KindColumn, Padding: 12, Gap: 6, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "summary"},
		{Kind: ui.KindText, Text: "a longer body line that must not be cropped"},
		{Kind: ui.KindButton, Text: "Open", Padding: 4},
	}}
	measure := func(text string, _ bool) (int, int) { return len(text) * 8, 16 }
	h, err := ui.ContentHeight(root, toastCardWidth, measure)
	if err != nil {
		t.Fatal(err)
	}
	if h <= 96 {
		t.Fatalf("content height %d still fits the 96 guess; use a taller tree", h)
	}
	if got := toastCardHeight(root, toastCardWidth, measure, 12); got < h {
		t.Fatalf("placed height %d < content %d", got, h)
	}
}
```

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'TestToastLayoutClearsTheBar|TestToastCardHeightFollowsContent'`. Expected: FAIL (`BarZone` unknown, `toastCardHeight` unknown).

**Step 3:** Add `BarZone` to `toastGeometry`. On a top corner, first card Y is `BarZone+toastMargin`; on a bottom corner, inset from `OutputH-BarZone`. `limit` subtracts `BarZone` as well as the two margins.

Add `toastCardHeight(root, width, measure, radius)` = `ContentHeight` plus `2*radius` (same ceiling as `monitorSurfaceHeight`). `cardHeight` / `rebuild` / `cardRects` measure the real tree; fallback 96 only when the tree or measure is missing.

`recompute` must pass the output's bar exclusive zone into geometry (from that output's bar policy / `exclusiveBarZone`).

**Step 4:** Re-run the tests. Expected: PASS. Also run `TestToastLayoutStacksFromTheConfiguredCorner` and `TestToastHostPublishesTheCardUnionAsInputRegion` so placement still hugs the trailing edge.

**Step 5:** Commit.

```bash
git commit -m "$(cat <<'EOF'
fix(shell): size toast cards from their tree

The 96 px guess cropped body and actions, and a 12 px origin sat under
the top bar. Measure ContentHeight and inset by the bar zone.

EOF
)"
```

---

### Task 2: Relative time and history chips

**Files:**
- Create: `internal/shell/notifytime.go`
- Test: `internal/shell/notifytime_test.go`

**Step 1: Write the failing table test**

```go
func TestFormatNotifyTime(t *testing.T) {
	now := time.Date(2026, 9, 3, 15, 4, 0, 0, time.Local)
	cases := []struct {
		ts   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-90 * time.Minute), "13:34"},
		{now.Add(-26 * time.Hour), "Wednesday, 13:04"},
	}
	// ...
}

func TestHistoryFilter(t *testing.T) {
	now := time.Date(2026, 9, 3, 15, 0, 0, 0, time.Local)
	// all / 1h / today / yesterday / 7d / older with 7-day retention
}
```

Copy strings from the design (D12, D7). Weekday is English `time.Weekday.String()`.

**Step 2:** Run the test. Expected: compile failure.

**Step 3:** Implement `formatNotifyTime` and `historyFilter` as pure functions. No I/O.

**Step 4:** Tests PASS.

**Step 5:** Commit `test(shell): pin DMS notify time and history chips`.

---

### Task 3: Group active records like DMS Current

**Files:**
- Modify: `internal/shell/popout_notifications.go` (`groups` currently history-only)
- Modify: `internal/shell/popout_notifications_test.go`

**Step 1:** Extend tests: two mail actives + one chat active → two groups, mail count 2, critical group sorts first, key is desktop-entry lowercased else app name.

Keep `TestCenterGroupsHistoryNewestFirst` for the old history helper until Task 9 deletes history grouping.

**Step 2:** FAIL on missing `activeGroups`.

**Step 3:** `activeGroups(active []protocol.Notification) []notifyGroup` using D6. Do not reuse history grouping for the Current tab.

**Step 4:** PASS.

**Step 5:** Commit.

---

### Task 4: Catalogue glyphs

**Files:**
- Create: `internal/render/icons/svg/notifications.svg`
- Create: `internal/render/icons/svg/notifications-off.svg`
- Create: `internal/render/icons/svg/close.svg`
- Create: `internal/render/icons/svg/schedule.svg`
- Modify: `internal/render/icons/build.py` (append after `replay`)
- Modify: `internal/render/iconfont.go`
- Modify: `internal/render/iconfont_test.go`

**Step 1:** `TestNotifyCatalogueNamesResolve` for `notifications`, `notifications-off`, `close`, `schedule`.

**Step 2:** FAIL.

**Step 3:** Original 24×24 filled paths, same language as `cpu.svg`. Run `python internal/render/icons/build.py` to refresh `sysc-icons.ttf`. Consecutive PUA after `iconReplay`. Add `iconNames` entries.

**Step 4:** PASS.

**Step 5:** Commit.

---

### Task 5: Bar bell

**Files:**
- Modify: `internal/config/config.go` (`knownItems`, `Default` right section)
- Modify: `internal/config/load.go` if the item needs no extra fields
- Modify: `internal/shell/widget.go`
- Create: `internal/shell/notifywidget.go`
- Test: `internal/shell/notifywidget_test.go`
- Modify: `internal/shell/registry.go` (`bindBarPanelActionsLocked`)

**Step 1:** Tests: default bar contains `notifications`; widget text is the notifications rune; unread paints a 6 px Error child; DND swaps to `notifications-off`; left action `panel:notifications`; middle `notify:dnd`; right `notify:dnd-menu`.

**Step 2:** FAIL on unknown item.

**Step 3:** `buildNotifyWidget` like battery. Do not hide when unread is zero. Wire `bindBarPanelActionsLocked`: left → `TogglePanel(PanelNotifications)` (panel id lands in Task 6; until then the action string is enough and the toggle can wait one commit — if `PanelNotifications` does not exist yet, land this task after Task 6's enum, or add the enum in this task's Step 3 first).

Land `PanelNotifications` constant in this task if Task 6 would otherwise be blocked. `parsePanelName("notifications")` can wait for Task 6 if tests only check the action string.

**Step 4:** PASS. `go test -count=1 ./internal/config -run Default` still accepts the new item.

**Step 5:** Commit.

---

### Task 6: Panel host, size, IPC, centre-open

**Files:**
- Modify: `internal/shell/panel.go`
- Modify: `internal/shell/panelhost.go` (`parsePanelName`, `panelTargetSize`, `panelTree`, open/close hooks)
- Modify: `internal/ipc/server.go` (`knownPanels`)
- Modify: `internal/ipc/server_test.go`
- Modify: `internal/shell/panelhost_test.go`
- Modify: `internal/shell/presentation.go` (set `centerOpen` from real panel lifetime, not only tests)

**Step 1:** Tests: `parsePanelName("notifications")`; `panelTargetSize` W=416; open sets `centerOpen` and sends `history.mark-seen`; close clears `centerOpen`; IPC `panel.toggle {"panel":"notifications"}`.

**Step 2:** FAIL.

**Step 3:** `Align: "right"`, hug the bar (not `CenterY`). Height: `ContentHeight` clamped to design D3 (min 300, max min(80% output, header+600)), same intrinsic path as sysmon. Opening acquires the interactive root. `markCenterSeen` ids go through `sendNotify(CommandHistoryMarkSeen)`.

Placeholder tree is enough if Task 7 owns chrome; this task must map a surface and flip `centerOpen`.

**Step 4:** PASS.

**Step 5:** Commit.

---

### Task 7: Header and Current / History tabs

**Files:**
- Modify: `internal/shell/popout_notifications.go`
- Modify: `internal/shell/popout_notifications_test.go`
- Modify: `internal/shell/panelhost.go` (`activate` / pointer for tab and Clear)

**Step 1:** `centerTree` contains title `Notifications`, DND and schedule buttons, Clear, and tabs `Current (n)` / `History (n)`. Empty Current shows `Nothing to see here`. Clear action is `notify:center:dismiss-all` on tab 0 and `notify:center:clear-history` on tab 1. DND action `notify:center:dnd`. Filter index lives on `PanelHost` like `launcherSel`.

**Step 2:** FAIL (stub still has four text buttons).

**Step 3:** Replace the stub header. Tab buttons `Role: "tab"`. Do not add settings or keyboard-hint buttons.

**Step 4:** PASS.

**Step 5:** Commit.

---

### Task 8: Current-tab grouped cards

**Files:**
- Modify: `internal/shell/notifycard.go`
- Modify: `internal/shell/popout_notifications.go`
- Test: `internal/shell/notifycard_test.go` (create if missing)

**Step 1:** A two-mail group shows app name, relative time, summary, body, Primary count badge, Dismiss, and expand when count > 1. Critical group has 2 px stroke (or left chip). Actions from the latest member. History rows are not on this tab.

**Step 2:** FAIL.

**Step 3:** Build cards with `KindCapsule`, 56 px `KindImage` (or letter fallback), D11 chrome. Expand lists up to 10 members. Per-id dismiss still `notify:<id>:dismiss`. Group dismiss dismisses each member.

**Step 4:** PASS.

**Step 5:** Commit.

---

### Task 9: History tab

**Files:**
- Modify: `internal/shell/popout_notifications.go`
- Modify: `internal/shell/popout_notifications_test.go`

**Step 1:** History tab is a flat list, newest first, with chips from Task 2. Cards have no action row. Close control is omitted until `protocol.CommandHistoryRemove` exists on the pin; add a `historyRemoveSupported()` that is false on rc.2. Empty filtered list still uses `Nothing to see here`.

**Step 2:** FAIL (`TestCenterGroupsHistoryNewestFirst` must be replaced: history is no longer grouped).

**Step 3:** Delete history grouping from the Current/History trees. Keep `unread` / `markSeen`.

**Step 4:** PASS.

**Step 5:** Commit. If the notify pin later grows `history.remove`, a follow-on paints the close button; record that as `bd create` discovered-from `sysc-152`.

---

### Task 10: Header actions reach sysc-notify (`sysc-150`)

**Files:**
- Modify: `internal/shell/panelhost.go` (or `notifyactions.go` if centre actions share the resolver)
- Modify: `internal/shell/popout_notifications.go`
- Test: `internal/shell/popout_notifications_test.go` / a sender fake like `toasthost_test.go`

**Step 1:** Click Clear on Current → `CommandDismissAll`. Clear on History → `CommandHistoryClear`. DND toggles local state only. Opening already sent mark-seen in Task 6.

**Step 2:** FAIL (buttons still have actions and no sender).

**Step 3:** One switch on `notify:center:*` next to session actions. `busy` does not retry.

**Step 4:** PASS. `bd close sysc-150 --reason "centre header sends dismiss-all, history.clear, and mark-seen"`.

**Step 5:** Commit.

---

### Task 11: Toast DMS chrome and icons

**Files:**
- Modify: `internal/shell/notifycard.go`
- Modify: `internal/ui/column.go` (`KindMeter` honours `Height` when > 0)
- Modify: `internal/render/paint.go` (capsule stroke; meter tone)
- Modify: `internal/shell/toasthost.go` (fill `KindImage` from `image-data` / `icons.Worker`)
- Tests: `internal/shell/notifycard_test.go`, `internal/ui/column_test.go`

**Step 1:** Toast tree: no `"!"`; bottom `KindMeter` Height 3, Value = remaining/duration; critical has left 8 px Primary chip and 2 px stroke; 56 px icon slot always reserved; letter fallback when no raster. Persistent (`DurationMS==0`) omits the draining meter.

**Step 2:** FAIL.

**Step 3:** Minimal paint: `KindCapsule` optional `Stroke` int + `StrokeFill`. Meter fill uses `ToneError` when set, else accent. Convert `protocol.Image` to `ui.Image` on the owner only when dimensions already match; named `AppIcon` goes through `icons.Worker` like tray.

**Step 4:** PASS. Width constant becomes 380 (`toastCardWidth`), tests that hard-code 360 updated.

**Step 5:** Commit.

---

### Task 12: DND gestures and duration menu

**Files:**
- Modify: `internal/shell/notifywidget.go`
- Modify: `internal/shell/popout_notifications.go` (schedule menu = D9 presets)
- Modify: `internal/shell/registry.go`
- Test: `internal/shell/notifywidget_test.go`, `internal/shell/popout_notifications_test.go`

**Step 1:** Middle-click toggles DND. Right-click / schedule glyph shows presets 15m, 30m, 1h, 3h, 8h, tomorrow 08:00, until off. Selecting 1h matches existing `setDNDPresetAt`. "Until off" is `setDND(true)` with zero end.

**Step 2:** FAIL.

**Step 3:** Reuse `KindMenu` if the bar already pops one for tray; otherwise a compact column of `KindButton` in the panel header (DMS `DndDurationMenu`) for the schedule glyph, and the same tree as a bar-adjacent menu for right-click. Do not invent a new surface class.

**Step 4:** PASS.

**Step 5:** Commit.

---

### Task 13: Owner-deferred live Niri list

Do not block merge on this task. Record the checklist in the completion handover when someone runs it.

On the live session (`NIRI_SOCKET`, `WAYLAND_DISPLAY=wayland-1`):

1. Stop `dms` by pid, start `sysc-notify` + `sysc-shell`.
2. Bell visible on both outputs; unread Error dot after `notify-send`.
3. Toast fully below the bar; body and actions unclipped; timeout bar drains.
4. Left-click bell → 416-wide Exclusive panel; `niri msg -j layers` shows it.
5. Current groups two notifies from one app; History lists them after expiry/dismiss.
6. Clear on Current vs History hits different commands (watch notify logs).
7. DND middle-click suppresses the next toast; history still accepts.
8. Escape and shield dismiss the panel; toasts resume.

---

After Task 12 the slice is executable without Task 13. Offer the usual execution choice (subagent-driven vs a second session with executing-plans).

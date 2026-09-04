# Running-apps Bar Pill Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a default-right bar capsule of compact per-application icons for every Niri UI window, with focus-or-cycle on left click and desktop `Actions=` plus Close all on right click.

**Architecture:** Niri event stream gains `Focused` / `FocusTimestamp` on `Window`. A pure grouper in `internal/shell` turns the session window list into slots. Focus/close are short-lived JSON `Action` requests on `$NIRI_SOCKET`. Desktop identity and `Actions=` spawn live in the shell (go-freedesktop scan + `niri msg action spawn`); they do not go through `launcher.Service`. The bar item `running-apps` is a `KindCapsule` of 24 px tiles; the context menu is an Overlay popup in the interactive-root chain.

**Tech Stack:** Go, existing `internal/platform/niri`, `internal/icons`, `github.com/go-freedesktop/desktopentry` (already indirect; promote to a direct require), M4 `KindMenu` / tray Overlay popup family. Do not add `sysc-launch` as a runtime dependency of this widget.

**Design:** `docs/plans/2026-09-05-running-apps-pill-design.md` (D1–D15). Audit `docs/plans/2026-09-05-running-apps-pill-audit-report.md` findings 1, 3, 4, 6–8 folded in. Findings 2 and 5 (launcher catalogue / `Service.Activate`) are superseded by the owner D8/D11 amendment: the pill must work with the launcher widget unused.

**Test runner:** never `go test ./...` or `-race`. Always:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

**bd:** claim `sysc-175` with `bd update sysc-175 --status in_progress` before Task 1. Commit `.beads/issues.jsonl` in the same commit as the code it describes. Close with `bd close sysc-175 --reason "..."` after the live gate (Task 9).

---

### Task 1: Project window focus from the Niri stream

**Files:**
- Modify: `internal/platform/niri/events.go` (`Window`, `wireWindow`, `project`, `apply`)
- Test: `internal/platform/niri/events_test.go`

**Step 0:** `bd update sysc-175 --status in_progress` from `/home/nomadx/sysc-shell`. Include `.beads/issues.jsonl` in this task's commit.

**Step 1:** Extend `TestWindowsChangedReplacesTheWholeSet` so window 80 is focused and window 81 is not. Add a fixture with a **non-null** timestamp object, not an int:

```json
"focus_timestamp": {"secs": 166673, "nanos": 194678785}
```

Assert `FocusTimestamp == 166673*1e9 + 194678785`. A bare `int64` unmarshal of that object must not be how `project()` works — that path errors and aborts the whole `WindowsChanged` batch.

Also apply `{"WindowFocusChanged":{"id":81}}` after the two-window snapshot and assert window 81 is focused and 80 is not. Niri sends that event for focus moves; ignoring it leaves `Focused` stuck after subscribe.

**Step 2:** Run:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/platform/niri -run 'TestWindowsChangedReplacesTheWholeSet|TestWindowFocusChanged'
```

Expected: FAIL — `Window` has no `Focused` field / `WindowFocusChanged` ignored.

**Step 3:** Add comparable fields (keep `slices.Equal` working):

```go
type Window struct {
	ID             uint64
	Title          string
	AppID          string
	WorkspaceID    uint64
	HasWorkspace   bool
	Focused        bool
	FocusTimestamp int64 // monotonic ns; 0 when null or omitted
}
```

`wireWindow.focus_timestamp` is `*struct{ Secs uint64 `json:"secs"`; Nanos uint64 `json:"nanos"` }`. Null / missing → 0. Convert with `int64(secs)*1e9 + int64(nanos)`. Decode `is_focused` as bool, default false.

On `WindowFocusChanged`, set `Focused` true only on that id and false on every other window; publish if that changed.

**Step 4:** Re-run the named tests. Also run `TestWindowOpenedOrChangedInsertsThenReplacesByID` and `TestWindowClosedRemovesByID`.

**Step 5:** Commit `feat(niri): project window focus from the event stream` (include `.beads/issues.jsonl`).

---

### Task 2: Short-lived Niri Action requests

**Files:**
- Create: `internal/platform/niri/action.go`
- Test: `internal/platform/niri/action_test.go`

**Step 1:** Write a table test that listens on a unix socket, accepts one connection, and checks the payload.

```go
func TestActionWritesFocusAndClose(t *testing.T) {
	// Serve one line, reply {"Ok":"Handled"}\n
	// Action(ctx, socket, FocusWindow{ID: 80}) writes {"Action":{"FocusWindow":{"id":80}}}\n
	// Action(ctx, socket, CloseWindow{ID: 80}) writes {"Action":{"CloseWindow":{"id":80}}}\n
	// {"Err":"..."} returns an error; the caller logs it (D15).
}
```

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/platform/niri -run TestActionWritesFocusAndClose`

Expected: FAIL — `Action` undefined.

**Step 3:** Implement `Action(ctx, socketPath string, body any) error`: dial, write JSON + newline, read one reply line, require `Ok`. Do not use the EventStream connection. Types:

```go
type FocusWindow struct{ ID uint64 `json:"id"` }
type CloseWindow struct{ ID uint64 `json:"id"` }
```

Marshal as `{"Action":{...}}` with the struct name as the variant key (`FocusWindow` / `CloseWindow`).

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(niri): send FocusWindow and CloseWindow on the IPC socket`

---

### Task 3: Group windows into application slots

**Files:**
- Create: `internal/shell/runningapps.go`
- Test: `internal/shell/runningapps_test.go`

**Step 1:** Table test `groupRunningApps(windows, lookup)`:

| Case | Windows | Want |
|---|---|---|
| two firefox | app_id `firefox` twice | one slot, two IDs |
| firefox + brave | distinct ids | two slots, first-seen order |
| `steam_app_123` + `steam` | lookup misses the game, hits `steam` | one Steam slot |
| `steam_app_123` with its own desktop file | lookup hits `steam_app_123` | its own slot, not folded |
| unknown | `app_id` `xyz` | one slot keyed by `xyz`, no actions |
| empty | none | nil / empty |

Lookup in the test is a map of app_id → `{id, icon, actions}`. Steam maps `steam` → id `steam`.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestGroupRunningApps`

Expected: FAIL — `groupRunningApps` undefined.

**Step 3:** Implement. Slot key: lowercase desktop-entry id if lookup hits, else lowercase `app_id`. Try the window's own `app_id` first. Only if that misses and `strings.HasPrefix(lower, "steam_app")`, look up `"steam"` (D2). Preserve Niri list order for members and for first-seen slots. Record `Focused` if any member is focused; MRU member is max `FocusTimestamp`, else members[0] (D6: no sticky last-focused memory).

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(shell): group Niri windows into running-app slots`

---

### Task 3b: Production identity lookup (D11)

**Files:**
- Modify: `internal/shell/runningapps.go` (or a tiny helper next to it)
- Test: `internal/shell/runningapps_test.go`

`groupRunningApps` takes a lookup. This task is the production function that builds it from a shell-owned desktop index. Do not import `sysc-launch`. Do not call `launcher.Service`. The index type is a small shell struct (`ID`, `Icon`, `StartupWMClass`, `Actions`); tests inject a slice.

**Step 1:** Table test `lookupRunningApp(appID, entries)`:

| app_id | catalogue | want |
|---|---|---|
| `firefox` | `{ID:"firefox"}` | that entry |
| `Firefox` | `{ID:"firefox"}` | that entry (fold) |
| `org.mozilla.firefox` | `{ID:"firefox"}` | that entry (last `.`/`/` segment) |
| `Foo` | `{ID:"org.foo.Bar", StartupWMClass:"Foo"}` | that entry |
| `steam` | `{ID:"steam"}` | that entry |
| `xyz` | firefox only | miss |

Empty catalogue → miss.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestLookupRunningApp`

Expected: FAIL.

**Step 3:** Scan the entry slice. Match equal-fold `ID`, then equal-fold `StartupWMClass`, then equal-fold last `.`/`/` segment of `app_id` against `ID`. Production load uses `desktopentry.ScanDirs` (or a walk that keeps `NoDisplay` and drops `Hidden`); tests never touch the live XDG tree.

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(shell): match running-app ids to desktop entries`

---

### Task 4: Focus-or-cycle

**Files:**
- Modify: `internal/shell/runningapps.go`
- Test: `internal/shell/runningapps_test.go`

**Step 1:** Table test `nextFocusID(slot)`:

- No member focused → MRU id
- Member[0] focused, three members → member[1]
- Last member focused → member[0] (wrap)

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestNextFocusID`

Expected: FAIL.

**Step 3:** Implement from D6. Do not send IPC here; return the id.

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(shell): pick the next window for a running-app slot`

---

### Task 5: Menu rows

**Files:**
- Modify: `internal/shell/runningapps.go`
- Test: `internal/shell/runningapps_test.go`

**Step 1:** Table test `runningAppMenu(slot)` returns `[]runningAppMenuRow`, not labels alone:

```go
type runningAppMenuRow struct {
	Label    string
	ActionID string // desktop action ID; empty when CloseAll
	CloseAll bool
}
```

- Steam-like actions Store/Library/Friends → three rows with those names and IDs, then `{Label:"Close all", CloseAll:true}`
- Empty actions → one Close all row

Task 8 indexes this slice: non-CloseAll rows expand that action's `Exec` and spawn; CloseAll sends `CloseWindow` per member.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestRunningAppMenu`

Expected: FAIL.

**Step 3:** Append the Close all row after the `Action` rows. No pin, no window-title list.

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(shell): running-app menu is desktop actions plus Close all`

---

### Task 6: Config item and default placement

**Files:**
- Modify: `internal/config/config.go` (`knownItems`, `Default().Bar.Right`)
- Test: `internal/config/config_test.go` (`TestDefaultVocabularyShipsBothClocksAndBothNiriWidgets`)

**Step 1:** Assert `knownItems` accepts `running-apps`. Assert `Default().Bar.Right[0].ID == "running-apps"` and left is still workspace + window-title.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config -run TestDefaultVocabularyShipsBothClocksAndBothNiriWidgets`

Expected: FAIL — right[0] is the cpu/memory group.

**Step 3:** Add `"running-apps": {}` to `knownItems`. Prepend `{ID: "running-apps"}` to `Default().Bar.Right`. No new options.

**Step 4:** Re-run. Expected: PASS. Also run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config -run TestParse`

**Step 5:** Commit `feat(config): running-apps is a bar item, first on the default right`

---

### Task 7: Bar capsule of icon tiles

**Files:**
- Modify: `internal/shell/widget.go` (`barView`, `buildWidgets` case)
- Modify: `internal/shell/registry.go` (`UpdateNiri`, `viewLocked`)
- Test: `internal/shell/runningapps_widget_test.go`

Session-global slots live on `barView.Running`, not on `outputState`. `UpdateNiri` groups the snapshot once, stores the slice on the Registry, and `viewLocked` copies that same slice onto every bar's view.

**Step 1:** A bar with `running-apps` and two grouped firefox slots paints one capsule containing two 24 px tiles; the focused tile is `FillAccent`. Empty slots → capsule has no children / the widget measures empty (D14). Copy `refreshWorkspacePills` for rebuild-vs-no-op.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestRunningAppsCapsule`

Expected: FAIL.

**Step 3:** `buildWidgets` case `"running-apps"`: a `KindCapsule` wrapping a `KindRow` of tiles. Tile: `KindImage` when a raster exists, else `KindText` of the first letter. Icon size 18, tile 24, row gap 4. `Action` per tile `running-app:<slotKey>`. Refresh from `barView.Running`. Hide: zero children, no placeholder (D14). Reuse `internal/icons` the way launcher rows do; missing raster is the letter. Catalogue for `lookupRunningApp` is the shell desktop index (D11), not launcher `Results()`. Do not call `launcherServiceLocked` from this widget.

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(shell): paint the running-apps capsule from grouped slots`

---

### Task 8: Clicks, Niri actions, desktop actions, Overlay menu

**Files:**
- Modify: `internal/shell/registry.go` (bar `onAction` / pointer)
- Modify: `internal/shell/runningapps.go` or a small host next to `traymenuhost.go` for the Overlay `KindMenu`
- Test: `internal/shell/runningapps_click_test.go`

**Step 1:**
- Left click on a slot calls `nextFocusID` then a recorded `FocusWindow` (fake Action in the test).
- Right click opens a menu whose labels match Task 5.
- Choosing a non-CloseAll row expands that action's `Exec` (`desktopentry.ExpandExec`) and records a spawn of `niri msg action spawn --` plus the argv. Do not call `launcher.Service.Activate` or `launcherSpawn`; those require the launcher service and write usage history.
- Choosing Close all records `CloseWindow` for each member.
- Slot gone while menu open → menu closes.

Do not parent `KindMenu` in the bar tree. Overlay auxiliary surface, same family as `trayMenuHost` (D13). Reuse `NewMenu` from `internal/shell/menu.go`.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run TestRunningAppsClick`

Expected: FAIL.

**Step 3:** Minimal wiring. Focus/close JSON and niri spawn argv run off the Wayland owner. Failures log, no panic (D15).

**Step 4:** Re-run. Expected: PASS.

**Step 5:** Commit `feat(shell): running-apps focus, cycle, and desktop-action menu` (include `.beads/issues.jsonl` if claim state changed).

---

### Task 9: Live Niri gate (owner machine)

**Files:** none required.

On the live compositor (`NIRI_SOCKET`, `WAYLAND_DISPLAY=wayland-1`):

1. Add nothing if Default() already has the item; restart the scratchpad binary.
2. Open Steam and Firefox. Both icons appear on the right, before cpu/memory.
3. Left-click Firefox with two windows: first focuses, second cycles.
4. Right-click Steam: Store/Library/Friends/… then Close all. Right-click an app with no `Actions=`: Close all only.
5. Close the last Firefox window: its icon disappears; the capsule remains if Steam is still open.
6. `niri msg -j layers` still shows the bar; the menu Overlay maps on right-click and unmaps on close.

If a pointer is not available, stop and leave the gate open in bd. Do not fake it.

After the gate: `bd close sysc-175 --reason "live Niri: Steam/Firefox icons, cycle, Actions= menu"` and commit `.beads/issues.jsonl`.

---

## Notes for the implementer

- `@ponytail`: no dock, no pins, no `/proc`, no foreign-toplevel, no `launcher.Service`. Duplicate XDG walk vs an open launcher is a known ceiling; do not refactor the launcher to share the index in this tranche.
- `@superpowers:executing-plans` for task-by-task execution.
- Workspace pills in `widget.go` are the rebuild pattern. Tray Overlay is the menu-host pattern. Spawn is `niri msg action spawn --` argv, same compositor seam as sysc-launch, without going through it.
- Existing configs without `running-apps` do not gain it until they reset to Default or add the item; only `Default()` changes.

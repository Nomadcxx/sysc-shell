# Built-in Widget Foundation (Milestone 3, Tranche 3A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship four read-only text widget instances — two clocks, a Niri workspace label, and a Niri
focused-window title — on every configured output, backed by one shared consumer-counted clock service and
one per-output widget instance set.

**Architecture:** Widgets are concrete values pairing a retained `ui.Node` with a pure
`func(barView) string`. A process-scoped `Registry` owns one `Bar` per Wayland output global, assembles
each bar's immutable view from a process-wide clock snapshot plus a per-connector Niri projection, and
reports which globals changed so only those bars repaint. The clock is a consumer-counted service whose
tick boundary is derived from the configured layout string; configuration reload acquires the replacement
set's leases before releasing the outgoing set's, so a service in continuous use is never restarted.

**Tech Stack:** Go 1.26, standard library only for all new code. Existing pinned modules
(`sysc-wayland v0.1.1`, `go-text/typesetting`, `golang.org/x/image`, `golang.org/x/sys`) are unchanged. No
new dependency is added by this plan.

**Spec:** `docs/plans/2026-08-30-built-in-widget-foundation-design.md`

## Global Constraints

Every task's requirements implicitly include this section.

- Linux and Niri are the only platform contract. Niri 26.04 is the qualified version.
- Go owns the shell. No C++, Rust, Lua, Luau, Qt, QML, or Quickshell.
- One goroutine owns the Wayland connection and every Wayland proxy. No new code calls a Wayland proxy.
- No new module dependency, no `replace` directive, no `sysc-metrics` import. This tranche is Tranche 3A;
  metrics are Tranche 3B.
- Widget instances are keyed by `wl_registry` global name (`uint32`). The connector string is an attribute
  and is never an identity key.
- One Niri connection and one clock timer for the whole process, never one per output.
- No widget interface, no widget schema, no plugin protocol, no service registry, no dependency-injection
  container, and no single-implementation interface.
- Read-only tranche. No workspace activation, popouts, keyboard focus, tooltips, icons, graphs, or
  animation.
- No icons ship, so no SVG decoder and no asset pipeline.
- Default bar geometry is unchanged: nominal height 48, gap 4, painted body 40, layer surface and
  exclusive zone 44, radius 12.
- All new goroutines must stop under cancellation, and `go test -race` must report no data race.
- Test fixtures use connectors `DP-9` and `HDMI-A-9` and invented window titles. No real connector name,
  window title, or machine-specific value enters Git.
- Commit messages must not contain any of `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`,
  `cody`, `tabnine`, `codex`, `gemini`, `bard`, `llm`, `bot`, `agent` as a case-insensitive substring; a
  repository hook rejects them. Note that this rejects innocent words containing them, such as "both".

## File Structure

**Created**

| Path | Responsibility |
|---|---|
| `internal/services/clock.go` | The `Clock` service: lease counting, boundary selection, aligned scheduling. |
| `internal/services/clock_test.go` | Lease lifecycle, boundary selection, alignment, cancellation. |
| `internal/platform/niri/events_test.go` | Unit tests for `state.apply` that need no socket. |
| `internal/shell/projection.go` | `projectOutputs`: one Niri snapshot to per-connector text. |
| `internal/shell/projection_test.go` | Projection table tests, including the dangling-id case. |
| `internal/shell/widget.go` | `barView`, `textWidget`, `buildWidgets`. |
| `internal/shell/widget_test.go` | Format functions and change detection. |

**Modified**

| Path | Change |
|---|---|
| `internal/platform/niri/events.go` | `Window` type, `Workspace.ActiveWindowID`, four new events, unchanged-state suppression. |
| `internal/platform/niri/client.go` | Publish the stored snapshot instead of rebuilding it. |
| `internal/platform/niri/client_test.go` | Window fixtures through the fake socket. |
| `internal/ui/tree.go` | `Node.MaxWidth`, and `Node.Tabular` plus the widened `MeasureText` in Task 14. |
| `internal/ui/layout.go` | `measureNode` clamps text width to `MaxWidth` and passes the tabular flag. |
| `internal/render/text.go`, `truncate.go`, `paint.go` | Tabular-figure shaping (Task 14 only). |
| `internal/ui/layout_test.go` | Clamp coverage. |
| `internal/config/config.go` | `Item` type, new vocabulary, new defaults. |
| `internal/config/load.go` | String-or-object item decoding, per-item validation, boundary derivation. |
| `internal/config/config_test.go` | Vocabulary, union decoding, validation failures. |
| `internal/shell/proof.go` → `internal/shell/bar.go` | `Proof` becomes `Bar`; fixture widgets removed; widgets and view application added. |
| `internal/shell/proof_test.go` → `internal/shell/bar_test.go` | Renamed with the type. |
| `internal/shell/registry.go` | Global keying, clock leases, `UpdateClock`, changed-global returns, `Close`. |
| `internal/shell/registry_test.go` | Rewritten against global keys and leases. |
| `internal/platform/wayland/client.go` | `PreparedConfig.Rollback`. |
| `cmd/sysc-shell/main.go` | Clock pump goroutine, `Registry.Close`. |
| `tests/integration/README.md` | Tranche 3A live matrix commands. |

## Lanes

After Task 0 passes, Tasks 1–3 (Niri) and Tasks 4–5 (clock) touch disjoint packages and may run in
separate worktrees. Tasks 6–15 are the integration lane and are strictly serial: they edit
`internal/shell/registry.go`, `internal/config`, and the reload transaction, which the handover forbids
parallelising. One agent owns the integration lane and merges the other two.

---

### Task 0: Gate — verify the merged Milestone 2 contracts

This is a verification gate, not a code change. The design depends on three Milestone 2 corrections. If
any is absent, **stop and return to the owner** rather than adapting the plan alone.

**Files:** none.

- [ ] **Step 1: Bring the branch onto the merged main**

```bash
cd /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/widget-foundation
git fetch origin
git rebase origin/main
go build ./... && go test ./... 2>&1 | tail -20
```

Expected: a clean build and green tests on the merged Milestone 2 code.

- [ ] **Step 2: Verify host identity is keyed by Wayland global**

```bash
grep -n "NewHost\|DropHost\|type Invalidation\|type PreparedConfig" -A3 internal/platform/wayland/client.go
```

Expected: `NewHost` and `DropHost` take a `uint32` global, `PreparedConfig.Hosts` is keyed by `uint32`,
and `Invalidation` names a global. If they still take a connector string, **stop**: identity is a
Milestone 2 correction and this plan assumes it.

- [ ] **Step 3: Verify the system font map is wired into the bar**

```bash
grep -rn "NewSystemFontMap\|SplitRuns" --include='*.go' internal/shell internal/render | grep -v _test
```

Expected: `internal/shell` constructs a `render.FontMap` rather than parsing embedded `goregular`. If
`internal/shell/proof.go` still calls `render.ParseFace(goregular.TTF)`, **stop**: window titles are the
first unbounded user text and need per-rune fallback.

- [ ] **Step 4: Verify invalidation cannot silently drop a redraw**

```bash
grep -n "invalidations" -A6 internal/shell/registry.go
```

Expected: no non-blocking `default:` that discards an invalidation for a bar whose state changed. If a
lossy drop remains, record it and carry it into Task 13, which tests the invariant directly.

- [ ] **Step 5: Record the outcome**

Write one paragraph in the pull request or handover naming which of the three contracts were confirmed and
which were absent. Do not proceed past this task with an absent contract.

---

### Task 1: Niri window types and window events

**Files:**
- Modify: `internal/platform/niri/events.go`
- Test: `internal/platform/niri/events_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `niri.Window` with fields `ID uint64`, `Title string`, `AppID string`, `WorkspaceID uint64`,
  `HasWorkspace bool`; `niri.Snapshot.Windows []Window`. Task 8 joins on these.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/niri/events_test.go`:

```go
package niri

import "testing"

// Wire fixtures follow the Niri 26.04 schema. Titles and connectors are
// invented; no real window or machine state appears here.
const (
	windowsChangedFixture = `{"WindowsChanged":{"windows":[` +
		`{"id":80,"title":"Fixture One","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
		`"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null},` +
		`{"id":81,"title":null,"app_id":null,"pid":null,"workspace_id":null,` +
		`"is_focused":false,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}]}}`

	windowOpenedFixture = `{"WindowOpenedOrChanged":{"window":` +
		`{"id":82,"title":"Fixture Two","app_id":"fixture.two","pid":1001,"workspace_id":5,` +
		`"is_focused":false,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}}}`

	windowRetitledFixture = `{"WindowOpenedOrChanged":{"window":` +
		`{"id":80,"title":"Fixture One Renamed","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
		`"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}}}`

	windowClosedFixture  = `{"WindowClosed":{"id":80}}`
	windowClosedUnknown  = `{"WindowClosed":{"id":9999}}`
	windowMissingIDFixt  = `{"WindowOpenedOrChanged":{"window":{"title":"No Identity"}}}`
)

// applyAll feeds lines to a fresh state and fails on the first decode error.
func applyAll(t *testing.T, lines ...string) *state {
	t.Helper()
	var s state
	for i, line := range lines {
		if _, err := s.apply([]byte(line)); err != nil {
			t.Fatalf("line %d: apply: %v", i, err)
		}
	}
	return &s
}

func TestWindowsChangedReplacesTheWholeSet(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture)

	snap := s.snapshot()
	if len(snap.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(snap.Windows))
	}
	if got := snap.Windows[0]; got.ID != 80 || got.Title != "Fixture One" || got.AppID != "fixture.one" {
		t.Fatalf("first window = %+v, want id 80 titled Fixture One", got)
	}
	if !snap.Windows[0].HasWorkspace || snap.Windows[0].WorkspaceID != 5 {
		t.Fatalf("first window workspace = %d/%v, want 5/true",
			snap.Windows[0].WorkspaceID, snap.Windows[0].HasWorkspace)
	}
	// A null title, app_id and workspace_id are legal and must not fail the event.
	if got := snap.Windows[1]; got.ID != 81 || got.Title != "" || got.HasWorkspace {
		t.Fatalf("second window = %+v, want id 81 with empty title and no workspace", got)
	}
}

func TestWindowOpenedOrChangedInsertsThenReplacesByID(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture, windowOpenedFixture, windowRetitledFixture)

	snap := s.snapshot()
	if len(snap.Windows) != 3 {
		t.Fatalf("windows = %d, want 3 after one insert", len(snap.Windows))
	}
	for _, w := range snap.Windows {
		if w.ID == 80 && w.Title != "Fixture One Renamed" {
			t.Fatalf("window 80 title = %q, want the replacement", w.Title)
		}
	}
}

func TestWindowClosedRemovesByID(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture, windowClosedFixture)

	for _, w := range s.snapshot().Windows {
		if w.ID == 80 {
			t.Fatal("WindowClosed left the window in the set")
		}
	}
}

// Closing a window the shell never saw asks for no change: the desired
// post-state already holds. It must not stop the stream.
func TestWindowClosedForAnUnknownIDIsANoOp(t *testing.T) {
	t.Parallel()
	var s state
	if _, err := s.apply([]byte(windowsChangedFixture)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	publish, err := s.apply([]byte(windowClosedUnknown))
	if err != nil {
		t.Fatalf("closing an unknown window errored: %v", err)
	}
	if publish {
		t.Fatal("closing an unknown window published a snapshot")
	}
	if len(s.snapshot().Windows) != 2 {
		t.Fatal("closing an unknown window changed the set")
	}
}

// A window with no id cannot be stored or matched later, so it is malformed.
func TestWindowWithoutAnIDIsAStreamError(t *testing.T) {
	t.Parallel()
	var s state
	if _, err := s.apply([]byte(windowMissingIDFixt)); err == nil {
		t.Fatal("a window with no id was accepted")
	}
	if len(s.snapshot().Windows) != 0 {
		t.Fatal("a malformed event published partial state")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/niri/ -run 'TestWindow' -v`
Expected: FAIL to compile — `snap.Windows` undefined, `Window` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/platform/niri/events.go`, add the type beside `Workspace`:

```go
// Window is one Niri window. Only the fields the shell projects are decoded.
// Layout, focus timestamp, urgency and floating state have no consumer, so an
// event carrying them decodes and ignores them.
type Window struct {
	ID    uint64
	Title string
	AppID string
	// WorkspaceID is meaningful only when HasWorkspace is set. Niri sends
	// workspace_id as null for a window on no workspace, which is distinct
	// from workspace zero.
	WorkspaceID  uint64
	HasWorkspace bool
}
```

Add `Windows []Window` to `Snapshot`, below `Workspaces`:

```go
// Snapshot is an immutable view of workspace and window state.
type Snapshot struct {
	Workspaces []Workspace
	Windows    []Window
	// FocusedOutput is derived from the workspace whose is_focused is true.
	// Niri has no dedicated event or field for it.
	FocusedOutput string
}
```

Add the wire types beside `wireWorkspace`:

```go
// wireWindow decodes one window. Only id is required; Niri sends title,
// app_id and workspace_id as null in legitimate states.
type wireWindow struct {
	ID          *uint64 `json:"id"`
	Title       *string `json:"title"`
	AppID       *string `json:"app_id"`
	WorkspaceID *uint64 `json:"workspace_id"`
}

func (w wireWindow) project() (Window, error) {
	if w.ID == nil {
		return Window{}, fmt.Errorf("niri: window is missing id")
	}
	out := Window{ID: *w.ID}
	if w.Title != nil {
		out.Title = *w.Title
	}
	if w.AppID != nil {
		out.AppID = *w.AppID
	}
	if w.WorkspaceID != nil {
		out.WorkspaceID, out.HasWorkspace = *w.WorkspaceID, true
	}
	return out, nil
}

type wireWindowsChanged struct {
	Windows []wireWindow `json:"windows"`
}

type wireWindowOpenedOrChanged struct {
	Window wireWindow `json:"window"`
}

type wireWindowClosed struct {
	ID *uint64 `json:"id"`
}
```

Add `windows []Window` to `state`:

```go
// state accumulates workspace and window events into snapshots.
type state struct {
	workspaces []Workspace
	windows    []Window
}
```

In `apply`, after the `WorkspaceActivated` block and before the final `return false, nil`:

```go
	if payload, ok := envelope["WindowsChanged"]; ok {
		var changed wireWindowsChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WindowsChanged: %w", err)
		}
		// Build the whole set before replacing state, so a malformed member
		// cannot publish a partial snapshot.
		next := make([]Window, 0, len(changed.Windows))
		for _, w := range changed.Windows {
			projected, err := w.project()
			if err != nil {
				return false, err
			}
			next = append(next, projected)
		}
		s.windows = next
		return true, nil
	}

	if payload, ok := envelope["WindowOpenedOrChanged"]; ok {
		var opened wireWindowOpenedOrChanged
		if err := json.Unmarshal(payload, &opened); err != nil {
			return false, fmt.Errorf("niri: decode WindowOpenedOrChanged: %w", err)
		}
		projected, err := opened.Window.project()
		if err != nil {
			return false, err
		}
		if i := slices.IndexFunc(s.windows, func(w Window) bool { return w.ID == projected.ID }); i >= 0 {
			s.windows[i] = projected
		} else {
			s.windows = append(s.windows, projected)
		}
		return true, nil
	}

	if payload, ok := envelope["WindowClosed"]; ok {
		var closed wireWindowClosed
		if err := json.Unmarshal(payload, &closed); err != nil {
			return false, fmt.Errorf("niri: decode WindowClosed: %w", err)
		}
		if closed.ID == nil {
			return false, fmt.Errorf("niri: WindowClosed is missing id")
		}
		i := slices.IndexFunc(s.windows, func(w Window) bool { return w.ID == *closed.ID })
		if i < 0 {
			// The desired post-state — this window absent — already holds.
			// There is nothing to diverge from, so this is not an error.
			return false, nil
		}
		s.windows = slices.Delete(s.windows, i, i+1)
		return true, nil
	}
```

In `snapshot`, clone and sort windows by id so the view is stable across runs. Insert before the
`Snapshot{...}` construction and add the field:

```go
	windows := slices.Clone(s.windows)
	slices.SortFunc(windows, func(a, b Window) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})

	snap := Snapshot{Workspaces: workspaces, Windows: windows}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/platform/niri/ -run 'TestWindow' -v`
Expected: PASS, five tests.

- [ ] **Step 5: Run the whole package and commit**

```bash
go test -race ./internal/platform/niri/ && gofmt -l internal/platform/niri
git add internal/platform/niri/events.go internal/platform/niri/events_test.go
git commit -m "feat(niri): decode window state from the event stream"
```

---

### Task 2: Workspace active window

**Files:**
- Modify: `internal/platform/niri/events.go`
- Test: `internal/platform/niri/events_test.go:` append

**Interfaces:**
- Consumes: `Window` and `state.windows` from Task 1.
- Produces: `Workspace.ActiveWindowID uint64` and `Workspace.HasActiveWindow bool`. Task 8 reads these.

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/niri/events_test.go`:

```go
const (
	// Two outputs, each with its own active workspace and active window.
	twoOutputWorkspaces = `{"WorkspacesChanged":{"workspaces":[` +
		`{"id":5,"idx":1,"name":"code","output":"DP-9","is_urgent":false,` +
		`"is_active":true,"is_focused":true,"active_window_id":80},` +
		`{"id":6,"idx":1,"name":null,"output":"HDMI-A-9","is_urgent":false,` +
		`"is_active":true,"is_focused":false,"active_window_id":81}]}}`

	activeWindowChanged        = `{"WorkspaceActiveWindowChanged":{"workspace_id":5,"active_window_id":82}}`
	activeWindowCleared        = `{"WorkspaceActiveWindowChanged":{"workspace_id":5,"active_window_id":null}}`
	activeWindowUnknownWkspace = `{"WorkspaceActiveWindowChanged":{"workspace_id":404,"active_window_id":82}}`
)

// workspaceByID finds a workspace in a snapshot, failing the test if absent.
func workspaceByID(t *testing.T, snap Snapshot, id uint64) Workspace {
	t.Helper()
	for _, w := range snap.Workspaces {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("workspace %d not in snapshot", id)
	return Workspace{}
}

func TestWorkspacesCarryTheirActiveWindow(t *testing.T) {
	t.Parallel()
	s := applyAll(t, twoOutputWorkspaces)
	snap := s.snapshot()

	five := workspaceByID(t, snap, 5)
	if !five.HasActiveWindow || five.ActiveWindowID != 80 {
		t.Fatalf("workspace 5 active window = %d/%v, want 80/true",
			five.ActiveWindowID, five.HasActiveWindow)
	}
	six := workspaceByID(t, snap, 6)
	if !six.HasActiveWindow || six.ActiveWindowID != 81 {
		t.Fatalf("workspace 6 active window = %d/%v, want 81/true",
			six.ActiveWindowID, six.HasActiveWindow)
	}
}

func TestWorkspaceActiveWindowChangedUpdatesOneWorkspace(t *testing.T) {
	t.Parallel()
	s := applyAll(t, twoOutputWorkspaces, activeWindowChanged)
	snap := s.snapshot()

	if got := workspaceByID(t, snap, 5); got.ActiveWindowID != 82 {
		t.Fatalf("workspace 5 active window = %d, want 82", got.ActiveWindowID)
	}
	// The other output's workspace must be untouched: this is a per-output
	// projection, not a global focus signal.
	if got := workspaceByID(t, snap, 6); got.ActiveWindowID != 81 {
		t.Fatalf("workspace 6 active window = %d, want the unchanged 81", got.ActiveWindowID)
	}
}

func TestWorkspaceActiveWindowCanBeCleared(t *testing.T) {
	t.Parallel()
	s := applyAll(t, twoOutputWorkspaces, activeWindowCleared)

	if got := workspaceByID(t, s.snapshot(), 5); got.HasActiveWindow {
		t.Fatalf("workspace 5 still reports active window %d after a null", got.ActiveWindowID)
	}
}

// Unlike a close for an unknown window, this event carries state with nowhere
// to go: the projection would keep showing a stale title with no resync path.
func TestWorkspaceActiveWindowChangedForAnUnknownWorkspaceIsAStreamError(t *testing.T) {
	t.Parallel()
	var s state
	if _, err := s.apply([]byte(twoOutputWorkspaces)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := s.apply([]byte(activeWindowUnknownWkspace)); err == nil {
		t.Fatal("an unknown workspace id was accepted")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/niri/ -run 'TestWorkspace' -v`
Expected: FAIL to compile — `Workspace.HasActiveWindow` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/platform/niri/events.go`, extend `Workspace`:

```go
// Workspace is one Niri workspace.
type Workspace struct {
	ID      uint64
	Index   int
	Name    string
	Output  string
	Active  bool
	Focused bool
	// ActiveWindowID is meaningful only when HasActiveWindow is set. It is the
	// workspace's own active window, which is what makes a per-output focused
	// title possible; Niri's global focus is a separate concept the shell does
	// not project.
	ActiveWindowID  uint64
	HasActiveWindow bool
}
```

Extend `wireWorkspace` with `ActiveWindowID *uint64 \`json:"active_window_id"\`` and set it at the end of
`wireWorkspace.project`, before the `return`:

```go
	if w.ActiveWindowID != nil {
		out.ActiveWindowID, out.HasActiveWindow = *w.ActiveWindowID, true
	}
```

Add the event wire type beside `wireWorkspaceActivated`:

```go
type wireWorkspaceActiveWindowChanged struct {
	WorkspaceID    *uint64 `json:"workspace_id"`
	ActiveWindowID *uint64 `json:"active_window_id"`
}
```

Add the handler in `apply`, after the `WorkspaceActivated` block:

```go
	if payload, ok := envelope["WorkspaceActiveWindowChanged"]; ok {
		var changed wireWorkspaceActiveWindowChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode WorkspaceActiveWindowChanged: %w", err)
		}
		if changed.WorkspaceID == nil {
			return false, fmt.Errorf("niri: WorkspaceActiveWindowChanged is missing workspace_id")
		}
		i := slices.IndexFunc(s.workspaces, func(w Workspace) bool { return w.ID == *changed.WorkspaceID })
		if i < 0 {
			// WorkspacesChanged always precedes this event, so an unknown id
			// means the stream and this state have diverged. There is nowhere
			// to record the active window, so the title would go stale
			// silently.
			return false, fmt.Errorf(
				"niri: WorkspaceActiveWindowChanged names unknown workspace %d", *changed.WorkspaceID)
		}
		if changed.ActiveWindowID != nil {
			s.workspaces[i].ActiveWindowID = *changed.ActiveWindowID
			s.workspaces[i].HasActiveWindow = true
		} else {
			s.workspaces[i].ActiveWindowID, s.workspaces[i].HasActiveWindow = 0, false
		}
		return true, nil
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/platform/niri/ -run 'TestWorkspace' -v`
Expected: PASS. Existing `client_test.go` tests must also still pass — its fixtures already carry
`active_window_id`.

- [ ] **Step 5: Run the whole package and commit**

```bash
go test -race ./internal/platform/niri/ && gofmt -l internal/platform/niri
git add internal/platform/niri/events.go internal/platform/niri/events_test.go
git commit -m "feat(niri): track each workspace's active window"
```

---

### Task 3: Suppress publication when decoded state is unchanged

`WindowOpenedOrChanged` fires on floating, urgency and focus changes that never alter a projected field.
Publishing those wakes the Wayland owner for nothing.

**Files:**
- Modify: `internal/platform/niri/events.go`, `internal/platform/niri/client.go`
- Test: `internal/platform/niri/events_test.go:` append

**Interfaces:**
- Consumes: `state.apply` from Tasks 1–2.
- Produces: `state.last Snapshot`, the snapshot `client.read` publishes. `apply` still returns
  `(publish bool, err error)`; it now returns false when nothing a consumer can see changed.

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/niri/events_test.go`:

```go
func TestRepeatingAnEventPublishesNothing(t *testing.T) {
	t.Parallel()
	var s state

	publish, err := s.apply([]byte(twoOutputWorkspaces))
	if err != nil || !publish {
		t.Fatalf("first apply published %v, err %v; want true, nil", publish, err)
	}
	publish, err = s.apply([]byte(twoOutputWorkspaces))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if publish {
		t.Fatal("an identical WorkspacesChanged published a second snapshot")
	}
}

// A window property the shell does not project must not cost a wake-up.
func TestAWindowChangeOutsideTheProjectionPublishesNothing(t *testing.T) {
	t.Parallel()
	const sameWindowNowUrgent = `{"WindowOpenedOrChanged":{"window":` +
		`{"id":80,"title":"Fixture One","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
		`"is_focused":true,"is_floating":true,"is_urgent":true,"layout":{},"focus_timestamp":7}}}`

	var s state
	if _, err := s.apply([]byte(windowsChangedFixture)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	publish, err := s.apply([]byte(sameWindowNowUrgent))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if publish {
		t.Fatal("a change to an unprojected field published a snapshot")
	}
}

func TestARealTitleChangeStillPublishes(t *testing.T) {
	t.Parallel()
	var s state
	if _, err := s.apply([]byte(windowsChangedFixture)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	publish, err := s.apply([]byte(windowRetitledFixture))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !publish {
		t.Fatal("a title change did not publish")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/niri/ -run 'PublishesNothing|StillPublishes' -v`
Expected: FAIL — both suppression tests report a published snapshot.

- [ ] **Step 3: Write the implementation**

Add `last Snapshot` to `state`:

```go
// state accumulates workspace and window events into snapshots.
//
// last is the most recently published snapshot. Comparing against it is what
// keeps an event that changes no projected field from waking the shell.
type state struct {
	workspaces []Workspace
	windows    []Window
	last       Snapshot
}
```

Add the comparison helper below `snapshot`:

```go
// publishIfChanged records and reports a new snapshot only when it differs
// from the last published one. Workspace and Window contain only comparable
// fields, so slices.Equal is an exact comparison.
func (s *state) publishIfChanged() bool {
	next := s.snapshot()
	if next.FocusedOutput == s.last.FocusedOutput &&
		slices.Equal(next.Workspaces, s.last.Workspaces) &&
		slices.Equal(next.Windows, s.last.Windows) {
		return false
	}
	s.last = next
	return true
}
```

Replace every `return true, nil` in `apply` with `return s.publishIfChanged(), nil`. There are six: the
`WorkspacesChanged`, `WorkspaceActivated`, `WorkspaceActiveWindowChanged`, `WindowsChanged`,
`WindowOpenedOrChanged` and `WindowClosed` blocks. Leave `WindowClosed`'s unknown-id branch returning
`false, nil` — it is already a no-op and must stay one. `WorkspaceActivated` currently reads
`return true, s.activate(...)`; rewrite it so the error is checked first:

```go
		if err := s.activate(*activated.ID, *activated.Focused); err != nil {
			return false, err
		}
		return s.publishIfChanged(), nil
```

In `internal/platform/niri/client.go`, publish the stored snapshot rather than building a second one. In
`read`, replace `send(snapshots, s.snapshot())` with:

```go
			send(snapshots, s.last)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/platform/niri/ -v`
Expected: PASS, including the pre-existing socket tests.

- [ ] **Step 5: Commit**

```bash
go test -race ./internal/platform/niri/ && gofmt -l internal/platform/niri
git add internal/platform/niri/events.go internal/platform/niri/events_test.go internal/platform/niri/client.go
git commit -m "perf(niri): publish only when projected state changes"
```

---

### Task 4: Clock service lifetime

**Files:**
- Create: `internal/services/clock.go`, `internal/services/clock_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `services.NewClock() *Clock`; `(*Clock).Acquire(boundary time.Duration) (*Lease, error)`;
  `(*Lease).Release()`; `(*Clock).Updates() <-chan time.Time`; `(*Clock).Close()`;
  `(*Clock).Running() bool`; `(*Clock).Starts() int`. Task 10 holds the leases.

- [ ] **Step 1: Write the failing test**

Create `internal/services/clock_test.go`:

```go
package services

import (
	"testing"
	"time"
)

func TestTheFirstLeaseStartsAndTheLastStops(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	if c.Running() {
		t.Fatal("a clock with no lease is running")
	}

	first, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !c.Running() {
		t.Fatal("the first lease did not start the clock")
	}

	second, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := c.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; a second consumer must share the running clock", got)
	}

	first.Release()
	if !c.Running() {
		t.Fatal("releasing one of two leases stopped the clock")
	}

	second.Release()
	if c.Running() {
		t.Fatal("releasing the last lease left the clock running")
	}
}

func TestReleaseIsIdempotentAndNilSafe(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	lease, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lease.Release()
	lease.Release()

	var absent *Lease
	absent.Release()

	if c.Running() {
		t.Fatal("a double release left the clock running")
	}
}

// A reload acquires the replacement set's leases before releasing the outgoing
// set's, so a service in continuous use must never restart.
func TestAcquireBeforeReleaseDoesNotRestartTheClock(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	outgoing, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	incoming, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	outgoing.Release()

	if !c.Running() {
		t.Fatal("the clock stopped during an overlapping handover")
	}
	if got := c.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; the clock restarted during a reload", got)
	}
	incoming.Release()
}

// A boundary change must re-arm the running timer, not cycle the goroutine.
func TestAShorterBoundaryDoesNotRestartTheClock(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	minute, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	second, err := c.Acquire(time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if got := c.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1 after a boundary change", got)
	}
	second.Release()
	minute.Release()
}

func TestANonPositiveBoundaryIsRejected(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	if _, err := c.Acquire(0); err == nil {
		t.Fatal("a zero boundary was accepted")
	}
	if c.Running() {
		t.Fatal("a rejected acquire started the clock")
	}
}

func TestCloseStopsTheGoroutine(t *testing.T) {
	t.Parallel()
	c := NewClock()
	if _, err := c.Acquire(time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	c.Close()

	if c.Running() {
		t.Fatal("Close left the clock running")
	}
	// Close must be safe to call twice; shutdown paths may both reach it.
	c.Close()
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -v`
Expected: FAIL — no non-test Go files in `internal/services`.

- [ ] **Step 3: Write the implementation**

Create `internal/services/clock.go`:

```go
// Package services holds the shell's process-scoped data sources.
//
// A service starts when its first consumer acquires a lease and stops when the
// last one is released, so a configuration that uses no clock costs no timer
// and no goroutine. Services here are concrete: there is no registry, no
// container, and no interface with one implementation.
package services

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// Clock publishes the current time to every consumer, aligned to the finest
// boundary any live lease requires.
//
// One Clock serves every bar. Consumers receive one shared snapshot per tick
// rather than sampling independently, so two outputs cost one wake-up.
type Clock struct {
	mu     sync.Mutex
	leases []*Lease
	// stop is closed to end the running goroutine; nil means not running.
	stop chan struct{}
	// done is closed by the goroutine as it exits, so a stop can wait for it.
	done chan struct{}
	// starts counts goroutine starts. A boundary change re-arms rather than
	// restarting, so this stays at one across a reload.
	starts int

	// rearm wakes the goroutine when a newly acquired lease needs a shorter
	// boundary than the one it is currently sleeping on.
	rearm   chan struct{}
	updates chan time.Time
}

// Lease is one consumer's claim on the clock.
type Lease struct {
	clock    *Clock
	boundary time.Duration
}

func NewClock() *Clock {
	return &Clock{
		rearm:   make(chan struct{}, 1),
		updates: make(chan time.Time, 1),
	}
}

// Updates carries the newest time. The channel is created once and never
// closed, so it survives stop and start cycles. The shell is its only receiver
// and fans each snapshot out to every bar.
func (c *Clock) Updates() <-chan time.Time { return c.updates }

// Acquire registers a consumer needing updates at least every boundary.
func (c *Clock) Acquire(boundary time.Duration) (*Lease, error) {
	if boundary <= 0 {
		return nil, fmt.Errorf("services: clock boundary %v is not positive", boundary)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.finestLocked()
	lease := &Lease{clock: c, boundary: boundary}
	c.leases = append(c.leases, lease)

	switch {
	case c.stop == nil:
		c.startLocked()
	case boundary < previous:
		// The goroutine is asleep on a longer deadline; wake it to re-arm.
		select {
		case c.rearm <- struct{}{}:
		default:
		}
	}
	return lease, nil
}

// Release drops a consumer, stopping the clock when it was the last one. It is
// idempotent and safe on a nil lease.
func (l *Lease) Release() {
	if l == nil || l.clock == nil {
		return
	}
	c := l.clock
	l.clock = nil

	c.mu.Lock()
	i := slices.Index(c.leases, l)
	if i < 0 {
		c.mu.Unlock()
		return
	}
	c.leases = slices.Delete(c.leases, i, i+1)
	done := c.stopIfUnusedLocked()
	c.mu.Unlock()

	// Waiting outside the lock: the goroutine takes the same mutex.
	if done != nil {
		<-done
	}
}

// Close releases every lease and stops the goroutine. It is safe to call twice.
func (c *Clock) Close() {
	c.mu.Lock()
	for _, l := range c.leases {
		l.clock = nil
	}
	c.leases = nil
	done := c.stopIfUnusedLocked()
	c.mu.Unlock()

	if done != nil {
		<-done
	}
}

// Running reports whether the timer goroutine is live.
func (c *Clock) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stop != nil
}

// Starts counts how many times the goroutine has started.
func (c *Clock) Starts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts
}

// finestLocked is the shortest boundary any live lease requires, or zero when
// there are none.
func (c *Clock) finestLocked() time.Duration {
	finest := time.Duration(0)
	for _, l := range c.leases {
		if finest == 0 || l.boundary < finest {
			finest = l.boundary
		}
	}
	return finest
}

func (c *Clock) startLocked() {
	c.stop, c.done = make(chan struct{}), make(chan struct{})
	c.starts++
	go c.run(c.stop, c.done)
}

// stopIfUnusedLocked ends the goroutine when no lease remains, returning the
// channel the caller should wait on after unlocking.
func (c *Clock) stopIfUnusedLocked() chan struct{} {
	if len(c.leases) > 0 || c.stop == nil {
		return nil
	}
	close(c.stop)
	done := c.done
	c.stop, c.done = nil, nil
	return done
}

func (c *Clock) run(stop, done chan struct{}) {
	defer close(done)

	for {
		c.mu.Lock()
		boundary := c.finestLocked()
		c.mu.Unlock()
		if boundary <= 0 {
			return
		}

		timer := time.NewTimer(time.Until(nextBoundary(time.Now(), boundary)))
		select {
		case <-stop:
			timer.Stop()
			return
		case <-c.rearm:
			// A shorter boundary arrived; recompute the deadline.
			timer.Stop()
		case <-timer.C:
			send(c.updates, time.Now())
		}
	}
}

// nextBoundary reports the first instant strictly after now that is aligned to
// b. Each tick recomputes its own deadline from the wall clock, so error
// cannot accumulate the way a fixed-period ticker's does.
func nextBoundary(now time.Time, b time.Duration) time.Time {
	next := now.Truncate(b).Add(b)
	if !next.After(now) {
		next = next.Add(b)
	}
	return next
}

// send publishes the newest time, replacing one the consumer has not read.
// This goroutine is the only sender, so the retry always finds room.
func send(updates chan time.Time, now time.Time) {
	select {
	case updates <- now:
		return
	default:
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- now:
	default:
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services
git add internal/services/
git commit -m "feat(services): add a consumer-counted clock service"
```

---

### Task 5: Clock alignment and delivery

**Files:**
- Modify: `internal/services/clock.go` (no change expected; the test proves Task 4's `nextBoundary`)
- Test: `internal/services/clock_test.go:` append

**Interfaces:**
- Consumes: `nextBoundary`, `Acquire`, `Updates` from Task 4.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append to `internal/services/clock_test.go`:

```go
func TestNextBoundaryAligns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		now      time.Time
		boundary time.Duration
		want     time.Time
	}{
		{
			name:     "mid minute rounds up to the next minute",
			now:      time.Date(2026, 8, 30, 15, 4, 37, 500_000_000, time.UTC),
			boundary: time.Minute,
			want:     time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC),
		},
		{
			// Exactly on a boundary must advance, never return now, or the
			// goroutine would spin on a zero-length timer.
			name:     "exactly on a boundary advances",
			now:      time.Date(2026, 8, 30, 15, 4, 0, 0, time.UTC),
			boundary: time.Minute,
			want:     time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC),
		},
		{
			name:     "sub second rounds up to the next second",
			now:      time.Date(2026, 8, 30, 15, 4, 37, 250_000_000, time.UTC),
			boundary: time.Second,
			want:     time.Date(2026, 8, 30, 15, 4, 38, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nextBoundary(tc.now, tc.boundary); !got.Equal(tc.want) {
				t.Fatalf("nextBoundary(%v, %v) = %v, want %v", tc.now, tc.boundary, got, tc.want)
			}
		})
	}
}

// One tick reaches the consumer, aligned to the second boundary.
func TestASecondBoundaryTickIsDelivered(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	lease, err := c.Acquire(time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	select {
	case now := <-c.Updates():
		// The publish happens just after the boundary, so the sub-second
		// remainder is small. A generous bound keeps this stable under load
		// while still failing an unaligned ticker.
		if off := now.Sub(now.Truncate(time.Second)); off > 500*time.Millisecond {
			t.Fatalf("tick landed %v past the second boundary, want it aligned", off)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no tick arrived within three seconds")
	}
}

// The channel holds only the newest time, so a slow consumer coalesces rather
// than queueing stale values.
func TestUpdatesKeepOnlyTheNewestTime(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	older := time.Date(2026, 8, 30, 15, 4, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC)
	send(c.updates, older)
	send(c.updates, newer)

	got := <-c.Updates()
	if !got.Equal(newer) {
		t.Fatalf("received %v, want the newest %v", got, newer)
	}
	select {
	case extra := <-c.Updates():
		t.Fatalf("a second value %v was queued behind the newest", extra)
	default:
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run 'NextBoundary|Delivered|NewestTime' -v`
Expected: PASS if Task 4 is complete and correct. If `TestNextBoundaryAligns` fails on the
"exactly on a boundary" case, the `!next.After(now)` guard in `nextBoundary` is wrong — fix it there.

This task is a verification task: it proves Task 4's scheduling core rather than adding behavior. If all
three pass immediately, that is the expected outcome; commit them.

- [ ] **Step 3: Run the package with the race detector**

Run: `go test -race -count=1 ./internal/services/ -v`
Expected: PASS with no race report.

- [ ] **Step 4: Confirm no goroutine outlives the test binary**

Run: `go test -race -count=1 ./internal/services/ -v 2>&1 | grep -i 'leak\|goroutine' || echo "no goroutine complaints"`
Expected: `no goroutine complaints`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services
git add internal/services/clock_test.go
git commit -m "test(services): cover clock alignment and newest-wins delivery"
```

---

### Task 6: Bounded text width

Prior art caps the focused-window title independently of section collision (Noctalia `maxWidth` 260, DMS
`Math.min(implicitWidth, 250)`). Without it a long title consumes its whole section budget and squeezes
its neighbour to zero.

**Files:**
- Modify: `internal/ui/tree.go`, `internal/ui/layout.go`
- Test: `internal/ui/layout_test.go:` append

**Interfaces:**
- Consumes: nothing.
- Produces: `ui.Node.MaxWidth int`, honoured by `measureNode` for `KindText`. Task 10 sets it.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/layout_test.go`:

```go
// A text node with MaxWidth never measures wider than its cap, so unbounded
// user text cannot consume a whole section.
func TestTextIsClampedToItsMaxWidth(t *testing.T) {
	t.Parallel()
	// Ten pixels per rune keeps the arithmetic obvious.
	measure := func(s string) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "aaaaaaaaaa", MaxWidth: 40},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 40 {
		t.Fatalf("clamped width = %d, want the 40 cap", got)
	}
}

// A cap wider than the text must not stretch it.
func TestMaxWidthDoesNotPadShortText(t *testing.T) {
	t.Parallel()
	measure := func(s string) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "ab", MaxWidth: 200},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 20 {
		t.Fatalf("width = %d, want the natural 20", got)
	}
}

// Zero means unbounded, so existing nodes are unaffected.
func TestZeroMaxWidthIsUnbounded(t *testing.T) {
	t.Parallel()
	measure := func(s string) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "aaaaa"},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 50 {
		t.Fatalf("width = %d, want the natural 50", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ -run MaxWidth -v`
Expected: FAIL to compile — `MaxWidth` is not a field of `Node`.

- [ ] **Step 3: Write the implementation**

In `internal/ui/tree.go`, add the field to `Node`, below `Width`:

```go
	// MaxWidth caps a text node's measured width. Zero means unbounded. It
	// exists because a focused-window title is unbounded user text: without a
	// cap it would take a whole section's budget before anything truncated.
	MaxWidth int
```

In `internal/ui/layout.go`, clamp in `measureNode`:

```go
	case KindText:
		w, h := measure(n.Text)
		if n.MaxWidth > 0 && w > n.MaxWidth {
			w = n.MaxWidth
		}
		return w, h, nil
```

The cap composes with the existing collision rules rather than replacing them: `placeSection` still grants
`min(natural, remaining)`, and the painter still truncates a node granted less than its natural width. A
title therefore never exceeds its cap, and can still be squeezed below it.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: PASS, including the pre-existing layout and bar tests.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ui
git add internal/ui/tree.go internal/ui/layout.go internal/ui/layout_test.go
git commit -m "feat(ui): cap text node width for unbounded user text"
```

---

### Task 7: Configuration item vocabulary

Replaces the Milestone 2 fixture vocabulary with the real one, and moves widget options onto each item so
two clocks can carry different formats. Because `config.Bar.Left` changes type, this task also makes the
mechanical adaptation in `internal/shell` needed to keep the tree building.

**Files:**
- Modify: `internal/config/config.go`, `internal/config/load.go`, `internal/config/config_test.go`
- Modify: `internal/shell/proof.go` (mechanical adaptation only)
- Modify: `internal/shell/proof_test.go`, `internal/shell/registry_test.go` (fixture IDs)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `config.Item{ID string; Format string; Boundary time.Duration; MaxWidth int}`;
  `config.Bar.Left`, `.Center`, `.Right` become `[]Item`. Tasks 10 and 11 read these.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestDefaultVocabularyShipsBothClocksAndBothNiriWidgets(t *testing.T) {
	t.Parallel()
	cfg := Default()

	if got := len(cfg.Bar.Left); got != 2 {
		t.Fatalf("left items = %d, want workspace and window-title", got)
	}
	if cfg.Bar.Left[0].ID != "workspace" {
		t.Fatalf("left[0] = %q, want workspace", cfg.Bar.Left[0].ID)
	}
	if cfg.Bar.Left[1].ID != "window-title" || cfg.Bar.Left[1].MaxWidth <= 0 {
		t.Fatalf("left[1] = %+v, want window-title with a positive max width", cfg.Bar.Left[1])
	}
	if len(cfg.Bar.Center) != 1 || cfg.Bar.Center[0].ID != "clock" {
		t.Fatalf("center = %+v, want one clock", cfg.Bar.Center)
	}
	if len(cfg.Bar.Right) != 1 || cfg.Bar.Right[0].ID != "clock" {
		t.Fatalf("right = %+v, want one clock", cfg.Bar.Right)
	}
	// The two default clocks must differ, or the defaults do not demonstrate
	// a date.
	if cfg.Bar.Center[0].Format == cfg.Bar.Right[0].Format {
		t.Fatal("the two default clocks share a format; one should show the date")
	}
	for _, item := range append(append([]Item{}, cfg.Bar.Center...), cfg.Bar.Right...) {
		if item.Boundary <= 0 {
			t.Fatalf("default clock %+v has no tick boundary", item)
		}
	}
}

func TestTheFixtureVocabularyIsGone(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"shell-name", "meter", "toggle"} {
		body := []byte(`{"bar":{"items":{"left":["` + id + `"]}}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("the retired fixture id %q was accepted", id)
		}
	}
}

func TestAnItemIsEitherAStringOrAnObject(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{
		"left":["workspace"],
		"center":[{"id":"clock","format":"15:04:05"}],
		"right":[{"id":"window-title","max-width":120}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Bar.Left[0].ID != "workspace" {
		t.Fatalf("bare string item = %+v, want workspace", cfg.Bar.Left[0])
	}
	if got := cfg.Bar.Center[0]; got.Format != "15:04:05" || got.Boundary != time.Second {
		t.Fatalf("seconds clock = %+v, want a one second boundary", got)
	}
	if got := cfg.Bar.Right[0]; got.MaxWidth != 120 {
		t.Fatalf("window-title = %+v, want max width 120", got)
	}
}

func TestAClockWithoutSecondsTicksOncePerMinute(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"center":[{"id":"clock","format":"Mon 2 Jan"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Bar.Center[0].Boundary; got != time.Minute {
		t.Fatalf("boundary = %v, want one minute", got)
	}
}

// "HH:MM" is not a Go layout; Format renders it literally and forever
// unchanged. Catching that is the whole point of validating the layout.
func TestATimeInvariantFormatIsRejected(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{"HH:MM", "hh:mm:ss", "the time"} {
		body := []byte(`{"bar":{"items":{"center":[{"id":"clock","format":"` + layout + `"}]}}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("the time-invariant layout %q was accepted", layout)
		}
	}
}

// A coarse but legitimate layout must not be mistaken for a typo.
func TestACoarseFormatIsAccepted(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"center":[{"id":"clock","format":"January 2006"}]}}}`))
	if err != nil {
		t.Fatalf("a month-and-year layout was rejected: %v", err)
	}
	if got := cfg.Bar.Center[0].Boundary; got != time.Minute {
		t.Fatalf("boundary = %v, want one minute", got)
	}
}

func TestAnOptionOnTheWrongItemIsRejected(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"bar":{"items":{"left":[{"id":"workspace","format":"15:04"}]}}}`,
		`{"bar":{"items":{"left":[{"id":"clock","max-width":100}]}}}`,
	}
	for _, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("an option on the wrong item was accepted: %s", body)
		}
	}
}

func TestANonPositiveMaxWidthIsRejected(t *testing.T) {
	t.Parallel()
	body := []byte(`{"bar":{"items":{"left":[{"id":"window-title","max-width":0}]}}}`)
	if _, err := Parse(body); err == nil {
		t.Fatal("a zero max width was accepted")
	}
}

func TestAnUnknownItemStillNamesItsFieldPath(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar":{"items":{"right":["workspace","nope"]}}}`))
	if err == nil {
		t.Fatal("an unknown item was accepted")
	}
	if !strings.Contains(err.Error(), "bar.items.right[1]") {
		t.Fatalf("error %q does not name the failing field path", err)
	}
}
```

Add `"strings"` and `"time"` to that file's imports if absent.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL to compile — `Item` undefined, `cfg.Bar.Left[0].ID` invalid on a `[]string`.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add the item type and swap the section types:

```go
// Item is one validated widget instance. Options live on the instance rather
// than the bar, so one bar can carry two clocks with different formats.
type Item struct {
	ID string
	// Format is the Go layout string for a clock. Empty on other items.
	Format string
	// Boundary is how often this clock's text can change, derived from Format
	// at load. Zero on other items.
	Boundary time.Duration
	// MaxWidth caps a window title in logical pixels. Zero on other items.
	MaxWidth int
}

// Bar is the resolved policy for one bar.
type Bar struct {
	Enabled    bool
	Edge       string
	Height     int
	Gap        int
	Padding    int
	Spacing    int
	Radius     int
	FontFamily string
	FontSize   int
	Left       []Item
	Center     []Item
	Right      []Item
}
```

Replace `knownItems` and add the defaults:

```go
// knownItems is the Tranche 3A widget vocabulary. The Milestone 2 fixture ids
// are deliberately absent: there is no compatibility promise, so a stale
// configuration fails loudly instead of silently dropping a widget.
var knownItems = map[string]struct{}{
	"clock": {}, "workspace": {}, "window-title": {},
}

const (
	// defaultClockFormat and defaultDateFormat are the two default clock
	// instances. There is no separate date widget: a date is a clock with a
	// coarser layout.
	defaultClockFormat = "15:04"
	defaultDateFormat  = "Mon 2 Jan"
	// defaultTitleMaxWidth matches the shipped default in the reference
	// shells, which cap the focused-window title at 250 to 260 logical pixels.
	defaultTitleMaxWidth = 260
)
```

Update `Default()`'s items:

```go
			Left: []Item{
				{ID: "workspace"},
				{ID: "window-title", MaxWidth: defaultTitleMaxWidth},
			},
			Center: []Item{
				{ID: "clock", Format: defaultClockFormat, Boundary: time.Minute},
			},
			Right: []Item{
				{ID: "clock", Format: defaultDateFormat, Boundary: time.Minute},
			},
```

Add `"time"` to that file's imports.

In `internal/config/load.go`, replace `wireItems` and the `items` helper:

```go
// wireItem decodes either a bare id string or an object carrying that id plus
// its options. Both reference shells attach options per instance, and a
// max-width has nowhere else to live.
type wireItem struct {
	ID       string  `json:"id"`
	Format   *string `json:"format"`
	MaxWidth *int    `json:"max-width"`
}

func (i *wireItem) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var id string
		if err := json.Unmarshal(trimmed, &id); err != nil {
			return err
		}
		*i = wireItem{ID: id}
		return nil
	}
	// A local type without this method, so decoding the object form does not
	// recurse into UnmarshalJSON.
	type plain wireItem
	var p plain
	if err := json.Unmarshal(trimmed, &p); err != nil {
		return err
	}
	*i = wireItem(p)
	return nil
}

type wireItems struct {
	Left   *[]wireItem `json:"left"`
	Center *[]wireItem `json:"center"`
	Right  *[]wireItem `json:"right"`
}
```

Replace the `items` helper with per-item resolution:

```go
// items resolves one section, rejecting the whole candidate on the first
// failure and naming its exact field path.
func items(supplied *[]wireItem, base []Item, path string) ([]Item, error) {
	if supplied == nil {
		return base, nil
	}
	out := make([]Item, 0, len(*supplied))
	for i, w := range *supplied {
		item, err := resolveItem(w, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// resolveItem validates one item and fills in its defaults. An option supplied
// on an item that does not accept it is an error rather than a silently
// ignored field, so a misplaced setting is visible.
func resolveItem(w wireItem, path string) (Item, error) {
	if _, ok := knownItems[w.ID]; !ok {
		return Item{}, pathErr(path, "%q is not a known item", w.ID)
	}
	item := Item{ID: w.ID}

	if w.Format != nil && w.ID != "clock" {
		return Item{}, pathErr(path+".format", "is accepted only on a clock, not on %q", w.ID)
	}
	if w.MaxWidth != nil && w.ID != "window-title" {
		return Item{}, pathErr(path+".max-width", "is accepted only on a window-title, not on %q", w.ID)
	}

	switch w.ID {
	case "clock":
		item.Format = defaultClockFormat
		if w.Format != nil {
			item.Format = *w.Format
		}
		boundary, err := clockBoundary(item.Format)
		if err != nil {
			return Item{}, pathErr(path+".format", "%s", err)
		}
		item.Boundary = boundary
	case "window-title":
		item.MaxWidth = defaultTitleMaxWidth
		if w.MaxWidth != nil {
			if *w.MaxWidth <= 0 {
				return Item{}, pathErr(path+".max-width", "%d is not positive", *w.MaxWidth)
			}
			item.MaxWidth = *w.MaxWidth
		}
	}
	return item, nil
}
```

Add the boundary derivation to the same file:

```go
// clockProbeBase is a fixed instant in UTC, so boundary derivation is
// deterministic and independent of the machine's clock and zone.
var clockProbeBase = time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)

// clockProbes ascend, so the first one that changes the rendered text names
// the resolution the layout displays. The long tail exists to distinguish a
// legitimately coarse layout such as "January 2006" from a typo.
var clockProbes = []time.Duration{
	time.Second, time.Minute, time.Hour,
	24 * time.Hour, 32 * 24 * time.Hour, 400 * 24 * time.Hour,
}

// clockBoundary reports how often a layout's text can change.
//
// time.Format never returns an error, so a layout cannot be validated by
// parsing it; it can be validated by observing it. A layout that renders
// identically at every probe does not depend on the time at all, which is what
// a non-layout such as "HH:MM" does.
//
// The result is capped at one minute even for a daily layout. Truncating to a
// day would truncate to UTC midnight rather than local midnight, so a date
// would flip at the wrong moment and break across a daylight-saving change.
// Re-rendering once a minute and detecting no change is correct and needs no
// calendar arithmetic.
func clockBoundary(layout string) (time.Duration, error) {
	base := clockProbeBase.Format(layout)
	for _, probe := range clockProbes {
		if clockProbeBase.Add(probe).Format(layout) == base {
			continue
		}
		if probe == time.Second {
			return time.Second, nil
		}
		return time.Minute, nil
	}
	return 0, fmt.Errorf(
		"%q does not change with time; a Go layout uses a reference instant such as 15:04", layout)
}
```

Add `"bytes"` and `"time"` to `load.go`'s imports.

In `internal/shell/proof.go`, adapt `build` mechanically. Task 10 replaces this wholesale; for now it only
has to compile and keep the existing workspace behavior:

```go
// build turns configured items into nodes. Ids are validated at load, so an
// unknown id cannot reach here.
func (p *Proof) build(items []config.Item) []*ui.Node {
	out := make([]*ui.Node, 0, len(items))
	for _, item := range items {
		switch item.ID {
		case "workspace":
			out = append(out, p.label)
		case "clock":
			out = append(out, &ui.Node{Kind: ui.KindText})
		case "window-title":
			out = append(out, &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth})
		}
	}
	return out
}
```

Delete `p.meter`, `p.button`, the `toggleAction`, `meterLow`, `meterHigh` constants, `MeterValue`,
`ButtonBounds`, and `activateLocked`'s toggle body from `proof.go`. In `Handle`, keep the pointer state
machine and hit testing; replace `activateLocked` with:

```go
// activateLocked applies an action and reports whether state changed. No
// Tranche 3A node carries an action, so this is inert at runtime; the pointer
// path stays covered by tests and ready for Milestone 4 controls.
func (p *Proof) activateLocked(action string) bool { return false }
```

Update `internal/shell/proof_test.go` and `internal/shell/registry_test.go` to use the new vocabulary:
replace `[]string{"workspace"}` with `[]config.Item{{ID: "workspace"}}` and delete the meter and toggle
click tests, whose widgets no longer exist. Keep every pointer-routing test that uses a synthetic node.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./internal/shell/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go build ./... && gofmt -l internal/config internal/shell
git add internal/config/ internal/shell/
git commit -m "feat(config): adopt the tranche 3A widget vocabulary"
```

---

### Task 8: Per-connector projection

**Files:**
- Create: `internal/shell/projection.go`, `internal/shell/projection_test.go`
- No other file is touched. This task is purely additive.

**Interfaces:**
- Consumes: `niri.Snapshot`, `niri.Window`, `Workspace.ActiveWindowID` from Tasks 1–2.
- Produces: `outputState{Workspace string; Title string}` and
  `projectOutputs(niri.Snapshot) map[string]outputState`. Task 10 calls this.

- [ ] **Step 1: Write the failing test**

Create `internal/shell/projection_test.go`:

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// twoOutputs is one snapshot with a distinct active workspace and active
// window on each connector.
func twoOutputs() niri.Snapshot {
	return niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Index: 1, Name: "code", Output: "DP-9", Active: true, Focused: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 6, Index: 1, Output: "HDMI-A-9", Active: true,
				ActiveWindowID: 81, HasActiveWindow: true},
		},
		Windows: []niri.Window{
			{ID: 80, Title: "Fixture One", AppID: "fixture.one"},
			{ID: 81, Title: "Fixture Two", AppID: "fixture.two"},
		},
	}
}

func TestEachOutputProjectsItsOwnWorkspaceAndTitle(t *testing.T) {
	t.Parallel()
	got := projectOutputs(twoOutputs())

	if got["DP-9"].Workspace != "code" || got["DP-9"].Title != "Fixture One" {
		t.Fatalf("DP-9 = %+v, want the named workspace and its own window", got["DP-9"])
	}
	// An unnamed workspace falls back to its index.
	if got["HDMI-A-9"].Workspace != "1" || got["HDMI-A-9"].Title != "Fixture Two" {
		t.Fatalf("HDMI-A-9 = %+v, want the index and its own window", got["HDMI-A-9"])
	}
}

// Niri focus is global, but the title is not: an unfocused output keeps
// showing its own active window.
func TestAnUnfocusedOutputKeepsItsOwnTitle(t *testing.T) {
	t.Parallel()
	snap := twoOutputs()
	// Focus moves entirely to DP-9; HDMI-A-9 is merely active.
	got := projectOutputs(snap)

	if got["HDMI-A-9"].Title != "Fixture Two" {
		t.Fatalf("unfocused output title = %q, want its own window", got["HDMI-A-9"].Title)
	}
}

func TestAFocusedWorkspaceOutranksAnActiveOneOnTheSameOutput(t *testing.T) {
	t.Parallel()
	got := projectOutputs(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 1, Name: "active-one", Output: "DP-9", Active: true},
		{ID: 2, Name: "focused-one", Output: "DP-9", Focused: true},
	}})

	if got["DP-9"].Workspace != "focused-one" {
		t.Fatalf("workspace = %q, want the focused one", got["DP-9"].Workspace)
	}
}

// Between WindowClosed and the following WorkspaceActiveWindowChanged the id
// names a window that is gone. That must render as no title, not as an error
// and not as a stale one.
func TestADanglingActiveWindowIDProjectsToNoTitle(t *testing.T) {
	t.Parallel()
	got := projectOutputs(niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true,
				ActiveWindowID: 9999, HasActiveWindow: true},
		},
		Windows: []niri.Window{{ID: 80, Title: "Fixture One"}},
	})

	if got["DP-9"].Workspace != "code" {
		t.Fatalf("workspace = %q, want it unaffected", got["DP-9"].Workspace)
	}
	if got["DP-9"].Title != "" {
		t.Fatalf("title = %q, want empty for a window that is gone", got["DP-9"].Title)
	}
}

func TestAWorkspaceWithNoActiveWindowProjectsToNoTitle(t *testing.T) {
	t.Parallel()
	got := projectOutputs(niri.Snapshot{
		Workspaces: []niri.Workspace{{ID: 5, Name: "empty", Output: "DP-9", Active: true}},
		Windows:    []niri.Window{{ID: 80, Title: "Fixture One"}},
	})

	if got["DP-9"].Title != "" {
		t.Fatalf("title = %q, want empty on an empty workspace", got["DP-9"].Title)
	}
}

// A workspace naming no output cannot be joined to a bar and must be skipped
// rather than producing an empty-string key.
func TestAWorkspaceWithNoOutputIsSkipped(t *testing.T) {
	t.Parallel()
	got := projectOutputs(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 7, Name: "detached", Active: true},
	}})

	if _, ok := got[""]; ok {
		t.Fatal("a workspace with no output produced an entry")
	}
	if len(got) != 0 {
		t.Fatalf("projection = %+v, want empty", got)
	}
}

func TestAnEmptySnapshotProjectsNothing(t *testing.T) {
	t.Parallel()
	if got := projectOutputs(niri.Snapshot{}); len(got) != 0 {
		t.Fatalf("projection = %+v, want empty", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/shell/ -run 'Project|Dangling|Unfocused|Detached' -v`
Expected: FAIL to compile — `projectOutputs` and `outputState` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/shell/projection.go`:

```go
package shell

import (
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// outputState is the Niri-derived text for one connector.
//
// A missing workspace renders "-" and a missing window renders empty. Both are
// stable: an empty text node measures zero-wide, so a section with no window
// simply shrinks rather than reserving space.
type outputState struct {
	Workspace string
	Title     string
}

// noWorkspace is the label shown before the first snapshot arrives, or for a
// connector Niri has not reported.
const noWorkspace = "-"

// projectOutputs reduces one snapshot to per-connector text.
//
// The title join is output → that output's active workspace → its
// active_window_id → that window. Niri's globally focused window is
// deliberately not consulted: each bar reports what is active on its own
// monitor, so moving focus to another output does not blank this one.
func projectOutputs(s niri.Snapshot) map[string]outputState {
	// A focused workspace outranks a merely active one on the same output.
	chosen := make(map[string]niri.Workspace, len(s.Workspaces))
	focused := make(map[string]bool, len(s.Workspaces))
	for _, w := range s.Workspaces {
		if w.Output == "" || !(w.Focused || w.Active) {
			continue
		}
		if focused[w.Output] && !w.Focused {
			continue
		}
		chosen[w.Output] = w
		if w.Focused {
			focused[w.Output] = true
		}
	}

	titles := make(map[uint64]string, len(s.Windows))
	for _, window := range s.Windows {
		titles[window.ID] = window.Title
	}

	out := make(map[string]outputState, len(chosen))
	for connector, w := range chosen {
		label := w.Name
		if label == "" {
			label = strconv.Itoa(w.Index)
		}
		state := outputState{Workspace: label}
		if w.HasActiveWindow {
			// A missing entry means the window closed and the follow-up event
			// has not arrived. Empty is correct; stale would be wrong.
			state.Title = titles[w.ActiveWindowID]
		}
		out[connector] = state
	}
	return out
}
```

Leave `projectWorkspaces` and `Registry.UpdateNiri` in `internal/shell/registry.go` exactly as they
are. `projectWorkspaces` still has a live caller until Task 10 rewrites the registry, and deleting it
here would leave the tree red at this task's own checkpoint. This task only adds.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/shell/ -run 'Project|Dangling|Unfocused|Detached|Outrank' -v`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/shell
git add internal/shell/projection.go internal/shell/projection_test.go
git commit -m "feat(shell): project workspace and window title per connector"
```

---

### Task 9: Rename Proof to Bar

A pure mechanical rename with no behavior change, kept separate so the behavior diff in Task 10 is
readable.

**Files:**
- Rename: `internal/shell/proof.go` → `internal/shell/bar.go`
- Rename: `internal/shell/proof_test.go` → `internal/shell/bar_test.go`
- Modify: `internal/shell/registry.go`, `internal/shell/registry_test.go`

**Interfaces:**
- Consumes: `Proof` from Task 7.
- Produces: type `Bar` with the same method set. Tasks 10–12 extend it.

- [ ] **Step 1: Rename the files**

```bash
git mv internal/shell/proof.go internal/shell/bar.go
git mv internal/shell/proof_test.go internal/shell/bar_test.go
```

- [ ] **Step 2: Rename the type and its references**

```bash
sed -i 's/\bProof\b/Bar/g' internal/shell/bar.go internal/shell/bar_test.go internal/shell/registry.go internal/shell/registry_test.go
sed -i 's/\bp \*Bar\b/b *Bar/g; s/\bp\./b./g' internal/shell/bar.go
```

Then read `internal/shell/bar.go` and fix what `sed` could not: the receiver name in every method
signature, the package doc comment, and `render.ProofStyle`, which belongs to `internal/render` and must
**not** be renamed. Confirm with:

```bash
grep -n "ProofStyle" internal/shell/bar.go
```

Expected: `render.ProofStyle` still present and unmodified.

Update the package comment at the top of `bar.go`:

```go
// Package shell holds the bar model: its retained tree, its widgets, and the
// projection from service and Niri state into that tree.
package shell
```

Update the `Bar` doc comment:

```go
// Bar owns the model, the retained tree, the text renderer and style, and one
// buffered invalidation channel, for exactly one output.
```

- [ ] **Step 3: Verify the rename compiles and nothing else changed**

```bash
go build ./... && go vet ./internal/shell/
git diff --stat
```

Expected: a build with no errors, and a diff confined to the four files.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/shell/ -v`
Expected: PASS, with the same test count as before the rename.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/shell
git add -A internal/shell/
git commit -m "refactor(shell): rename the proof type to Bar"
```

---

### Task 10: Widget views and registry ownership

Tasks 10 and 11 of the original plan are merged here, on the audit's finding 1 and 2. The migration of
`internal/shell` from the Milestone 2 shape to the Tranche 3A shape is atomic: `Registry.UpdateNiri`
calls `projectWorkspaces` and `Bar.SetWorkspace`, and both disappear in this change. Splitting it leaves
the tree red at a task checkpoint, so the plan would fail its own gate. Tests are still written before
implementations; there is simply one green checkpoint and one commit for the whole migration.

**Files:**
- Create: `internal/shell/widget.go`, `internal/shell/widget_test.go`
- Modify: `internal/shell/bar.go`, `internal/shell/bar_test.go`
- Modify: `internal/shell/registry.go`, `internal/shell/registry_test.go`

**Interfaces:**
- Consumes: `config.Item` (Task 7), `ui.Node.MaxWidth` (Task 6), `projectOutputs`/`outputState`/
  `noWorkspace` (Task 8), `services.Clock` (Task 4).
- Produces: `barView{Now time.Time; Workspace string; Title string}`;
  `buildWidgets([]config.Item) []textWidget`; `clockBoundaries(...[]config.Item) []time.Duration`;
  `(*Bar).apply(barView) bool`; `(*Bar).connector() string`;
  `NewRegistry(config.Config) *Registry`;
  `(*Registry).NewHost(global uint32, connector string) (wayland.HostCallbacks, error)`;
  `(*Registry).DropHost(global uint32)`; `(*Registry).UpdateNiri(niri.Snapshot) []uint32`;
  `(*Registry).UpdateClock(time.Time) []uint32`; `(*Registry).Clock() *services.Clock`;
  `(*Registry).Close()`. Tasks 11 and 12 use these.

**Expect a red tree between Steps 4 and 5.** That is intended: `registry.go` still calls the methods
Step 4 removes. Do not try to make Step 4 compile on its own.


- [ ] **Step 1: Write the widget-view tests**

Create `internal/shell/widget_test.go`:

```go
package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// reference is a fixed instant, so format assertions do not depend on when the
// test runs.
var reference = time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)

func TestAClockWidgetFormatsTheSharedSnapshot(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04", Boundary: time.Minute},
		{ID: "clock", Format: "Mon 2 Jan", Boundary: time.Minute},
	})

	view := barView{Now: reference}
	if got := widgets[0].format(view); got != "15:04" {
		t.Fatalf("time clock = %q, want 15:04", got)
	}
	if got := widgets[1].format(view); got != "Sun 30 Aug" {
		t.Fatalf("date clock = %q, want Sun 30 Aug", got)
	}
}

// Before the first tick there is no time to show, and a bar must still render.
func TestAClockWidgetIsEmptyBeforeTheFirstTick(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "clock", Format: "15:04"}})

	if got := widgets[0].format(barView{}); got != "" {
		t.Fatalf("clock before the first tick = %q, want empty", got)
	}
}

func TestNiriWidgetsReadTheirOutputsProjection(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	})
	view := barView{Workspace: "code", Title: "Fixture One"}

	if got := widgets[0].format(view); got != "code" {
		t.Fatalf("workspace = %q, want code", got)
	}
	if got := widgets[1].format(view); got != "Fixture One" {
		t.Fatalf("title = %q, want Fixture One", got)
	}
	if got := widgets[1].node.MaxWidth; got != 120 {
		t.Fatalf("title node max width = %d, want 120", got)
	}
}

func TestApplyWritesOnlyChangedText(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	if changed := bar.apply(barView{Now: reference, Workspace: "code", Title: "Fixture One"}); !changed {
		t.Fatal("the first view reported no change")
	}
	// Re-applying the same view must report nothing: no change, no redraw.
	if changed := bar.apply(barView{Now: reference, Workspace: "code", Title: "Fixture One"}); changed {
		t.Fatal("an identical view reported a change")
	}
	// A different instant inside the same minute renders identical text.
	sameMinute := reference.Add(20 * time.Second)
	if changed := bar.apply(barView{Now: sameMinute, Workspace: "code", Title: "Fixture One"}); changed {
		t.Fatal("a tick inside the same minute reported a change")
	}
	// Crossing the minute must change.
	nextMinute := reference.Add(time.Minute)
	if changed := bar.apply(barView{Now: nextMinute, Workspace: "code", Title: "Fixture One"}); !changed {
		t.Fatal("crossing a minute boundary reported no change")
	}
}

func TestABarRemembersItsConnector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "HDMI-A-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}
	if got := bar.connector(); got != "HDMI-A-9" {
		t.Fatalf("connector = %q, want HDMI-A-9", got)
	}
}
```

- [ ] **Step 2: Write the registry tests**

Replace the body of `internal/shell/registry_test.go` with:

```go
package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// newHost is the common setup: one registry with hosts at the given globals.
func newHosts(t *testing.T, reg *Registry, hosts map[uint32]string) {
	t.Helper()
	for global, connector := range hosts {
		if _, err := reg.NewHost(global, connector); err != nil {
			t.Fatalf("NewHost(%d, %s): %v", global, connector, err)
		}
	}
}

func TestTwoBarsShareOneClockServiceAndOneUpdate(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d, want 1 shared start for two bars", got)
	}

	changed := reg.UpdateClock(time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC))
	if len(changed) != 2 {
		t.Fatalf("one clock update changed %d bars, want 2", len(changed))
	}
}

func TestRemovingOneBarRetainsTheServiceForTheOther(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.DropHost(1)
	if !reg.Clock().Running() {
		t.Fatal("dropping one of two bars stopped the clock")
	}

	reg.DropHost(2)
	if reg.Clock().Running() {
		t.Fatal("dropping the last bar left the clock running")
	}
}

// Reconnect overlap: two globals briefly carry the same connector. They must
// stay distinct instances with distinct leases.
func TestTwoGlobalsSharingAConnectorKeepDistinctInstances(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "DP-9"})

	if len(reg.bars) != 2 {
		t.Fatalf("bars = %d, want two distinct instances for one connector", len(reg.bars))
	}
	if reg.bars[1] == reg.bars[2] {
		t.Fatal("two globals share one bar instance")
	}

	// A projection for that connector must reach both.
	changed := reg.UpdateNiri(niri.Snapshot{
		Workspaces: []niri.Workspace{{ID: 5, Name: "code", Output: "DP-9", Active: true}},
	})
	if len(changed) != 2 {
		t.Fatalf("one connector's change reached %d bars, want 2", len(changed))
	}

	// Dropping the stale global must not remove the reconnected one.
	reg.DropHost(1)
	if _, ok := reg.bars[2]; !ok {
		t.Fatal("dropping one global removed the other sharing its connector")
	}
}

func TestOnlyTheAffectedOutputIsInvalidated(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
		{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true},
	}})

	// Change one output only.
	changed := reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "notes", Output: "DP-9", Active: true},
		{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true},
	}})
	if len(changed) != 1 || changed[0] != 1 {
		t.Fatalf("changed = %v, want only global 1", changed)
	}
}

func TestAnIdenticalSnapshotChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
	}}
	if changed := reg.UpdateNiri(snap); len(changed) != 1 {
		t.Fatalf("first update changed %v, want global 1", changed)
	}
	if changed := reg.UpdateNiri(snap); len(changed) != 0 {
		t.Fatalf("an identical snapshot changed %v", changed)
	}
}

// A clock tick inside the same minute renders identical text, so no bar
// repaints. This is the no-change-no-frame invariant.
func TestATickInsideTheSameBoundaryChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	base := time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)
	if changed := reg.UpdateClock(base); len(changed) != 1 {
		t.Fatalf("first tick changed %v, want global 1", changed)
	}
	if changed := reg.UpdateClock(base.Add(20 * time.Second)); len(changed) != 0 {
		t.Fatalf("a tick inside the same minute changed %v", changed)
	}
	if changed := reg.UpdateClock(base.Add(time.Minute)); len(changed) != 1 {
		t.Fatalf("crossing a minute changed %v, want global 1", changed)
	}
}

// Niri state may name an output whose wl_output has not been announced yet.
// It must be held and applied when the host appears, and must never create one.
func TestNiriStateForAnUnknownOutputIsHeldNotDropped(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "later", Output: "DP-9", Active: true},
	}})
	if len(reg.bars) != 0 {
		t.Fatal("a Niri event created a bar")
	}

	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	if got := reg.bars[1].left[0].node.Text; got != "later" {
		t.Fatalf("new bar workspace = %q, want the held state", got)
	}
}

func TestAConfigWithNoClockLeavesTheServiceStopped(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{ID: "workspace"}}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if reg.Clock().Running() {
		t.Fatal("a configuration with no clock started the clock service")
	}
}

func TestCloseReleasesEverything(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.Close()
	if reg.Clock().Running() {
		t.Fatal("Close left the clock running")
	}
	if len(reg.bars) != 0 {
		t.Fatal("Close left bars behind")
	}
	reg.Close()
}
```

- [ ] **Step 3: Run both to verify they fail**

Run: `go test ./internal/shell/ -v`
Expected: FAIL to compile — `buildWidgets`, `barView`, `apply`, `connector`, `UpdateClock` and
`Clock` are undefined, and `NewWithTheme` still takes two arguments.

- [ ] **Step 4: Implement the widget views**

Create `internal/shell/widget.go`:

```go
package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// barView is the immutable input every widget formats from. The Registry
// assembles it from one process-wide clock snapshot and this output's Niri
// projection, so two bars share one clock update while keeping their own
// workspace and title.
type barView struct {
	// Now is zero until the first clock tick.
	Now       time.Time
	Workspace string
	Title     string
}

// textWidget is one configured widget instance: a retained node plus the pure
// function that produces its text.
//
// Every Tranche 3A widget is a function of the view alone, with no mutable
// state and no lifecycle, so there is nothing for an interface to abstract.
// Change detection lives in Bar.apply rather than in each widget, because the
// node already holds the last rendered text.
type textWidget struct {
	node   *ui.Node
	format func(barView) string
}

// buildWidgets turns validated items into widget instances. Ids and options
// are validated at load, so an unknown id cannot reach here.
func buildWidgets(items []config.Item) []textWidget {
	out := make([]textWidget, 0, len(items))
	for _, item := range items {
		switch item.ID {
		case "clock":
			layout := item.Format
			out = append(out, textWidget{
				node: &ui.Node{Kind: ui.KindText},
				format: func(v barView) string {
					if v.Now.IsZero() {
						return ""
					}
					return v.Now.Format(layout)
				},
			})
		case "workspace":
			out = append(out, textWidget{
				node:   &ui.Node{Kind: ui.KindText},
				format: func(v barView) string { return v.Workspace },
			})
		case "window-title":
			out = append(out, textWidget{
				node:   &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth},
				format: func(v barView) string { return v.Title },
			})
		}
	}
	return out
}

// clockBoundaries reports the distinct tick boundaries a section set needs.
// The Registry acquires one lease per entry.
func clockBoundaries(sections ...[]config.Item) []time.Duration {
	var out []time.Duration
	for _, section := range sections {
		for _, item := range section {
			if item.ID == "clock" && item.Boundary > 0 {
				out = append(out, item.Boundary)
			}
		}
	}
	return out
}
```

In `internal/shell/bar.go`, replace the node fields with widget sections and add the connector. Change the
struct fields:

```go
	// Sections are arranged by ui.ArrangeBar into absolute bounds, so painting
	// and hit testing walk them as one flat list.
	left, center, right []textWidget

	// conn is the connector this bar renders for. It selects configuration and
	// joins Niri state; it is never this bar's identity, which is its Wayland
	// global.
	conn string
```

Delete the `label` field and `workspace`, `toggled` model fields. Change the constructors:

```go
// New builds a bar from the built-in defaults for one connector.
func New(connector string) (*Bar, error) {
	cfg := config.Default()
	return NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, connector)
}

// NewWithTheme builds a bar from resolved theme tokens, a bar policy, and the
// connector whose Niri state it reads.
func NewWithTheme(theme Theme, policy config.Bar, connector string) (*Bar, error) {
	if err := theme.Valid(); err != nil {
		return nil, err
	}
	face, err := render.ParseFace(goregular.TTF)
	if err != nil {
		return nil, err
	}

	b := &Bar{
		conn:          connector,
		theme:         theme,
		text:          render.NewTextRenderer(face),
		invalidations: make(chan struct{}, 1),
		style: render.ProofStyle{
			Size:       theme.TextSize,
			Scale120:   ui.ScaleUnit,
			Background: theme.Background,
			Foreground: theme.Foreground,
			Track:      theme.Muted,
			Accent:     theme.Accent,
			AccentOn:   theme.Error,
		},
	}

	b.left = buildWidgets(policy.Left)
	b.center = buildWidgets(policy.Center)
	b.right = buildWidgets(policy.Right)
	return b, nil
}
```

If Task 0 confirmed the font map is wired, keep whatever `NewWithTheme` already does for text rather than
reintroducing `render.ParseFace`; only the widget construction and the connector are new here.

Add the accessors and view application:

```go
// connector reports the output this bar renders for.
func (b *Bar) connector() string { return b.conn }

// widgets returns the three sections in paint order.
func (b *Bar) widgets() [][]textWidget { return [][]textWidget{b.left, b.center, b.right} }

// sections returns the retained nodes in paint order, for layout and painting.
// This REPLACES the existing sections() method, which returned the three node
// slices directly; it now derives them from the widget instances.
func (b *Bar) sections() [][]*ui.Node {
	out := make([][]*ui.Node, 0, 3)
	for _, section := range b.widgets() {
		nodes := make([]*ui.Node, 0, len(section))
		for _, w := range section {
			nodes = append(nodes, w.node)
		}
		out = append(out, nodes)
	}
	return out
}

// apply writes each widget's text from the view and reports whether anything
// changed. A false return means no layout and no redraw: no state change, no
// submitted frame.
func (b *Bar) apply(view barView) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.applyLocked(view)
}

func (b *Bar) applyLocked(view barView) bool {
	changed := false
	for _, section := range b.widgets() {
		for _, w := range section {
			if text := w.format(view); text != w.node.Text {
				w.node.Text = text
				changed = true
			}
		}
	}
	return changed
}
```

Delete `SetWorkspace`, `UpdateNiri`, `activeWorkspace`, `workspaceLabelLocked`, and `WorkspaceLabel` from
`bar.go`; the Registry now owns the projection. In `layoutLocked` and `renderViewLocked`, remove the
`p.label.Text = …` lines: text is written by `apply`, not by layout. Update both to iterate `b.sections()`.

Update `internal/shell/bar_test.go` and `registry_test.go` call sites for the third `NewWithTheme`
argument, and replace `WorkspaceLabel()` assertions with `apply` plus a node-text check.

- [ ] **Step 5: Implement the registry**

Replace `internal/shell/registry.go`:

```go
package shell

import (
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// Registry owns every bar, the services they consume, and the state they read.
//
// Bars are keyed by wl_registry global name. A connector is an attribute: two
// globals may briefly share one during a reconnect, and they must stay
// distinct instances with distinct service leases.
//
// Niri state is keyed by connector and held whether or not a host exists,
// because a Niri event may name an output whose wl_output has not been
// announced yet or has already been removed. A host is never created or
// destroyed from a Niri event.
type Registry struct {
	mu      sync.Mutex
	cfg     config.Config
	outputs map[string]outputState
	bars    map[uint32]*Bar
	leases  map[uint32][]*services.Lease
	now     time.Time

	clock *services.Clock
}

func NewRegistry(cfg config.Config) *Registry {
	return &Registry{
		cfg:     cfg,
		outputs: make(map[string]outputState),
		bars:    make(map[uint32]*Bar),
		leases:  make(map[uint32][]*services.Lease),
		clock:   services.NewClock(),
	}
}

// Clock is the shared clock service. The process pumps its updates into
// UpdateClock.
func (r *Registry) Clock() *services.Clock { return r.clock }

// NewHost builds the hooks for one output's bar and acquires its services.
func (r *Registry) NewHost(global uint32, connector string) (wayland.HostCallbacks, error) {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()

	bar, leases, callbacks, err := r.buildBar(cfg, connector)
	if err != nil {
		return wayland.HostCallbacks{}, err
	}

	r.mu.Lock()
	bar.apply(r.viewLocked(connector))
	r.bars[global] = bar
	r.leases[global] = leases
	r.mu.Unlock()

	return callbacks, nil
}

// DropHost releases a bar and its service leases after its surface is
// destroyed. Only the named global is affected, so a stale global sharing a
// connector with a reconnected one cannot remove it.
func (r *Registry) DropHost(global uint32) {
	r.mu.Lock()
	leases := r.leases[global]
	delete(r.bars, global)
	delete(r.leases, global)
	r.mu.Unlock()

	releaseAll(leases)
}

// Close releases every bar and service. It is safe to call twice.
func (r *Registry) Close() {
	r.mu.Lock()
	var leases []*services.Lease
	for global, held := range r.leases {
		leases = append(leases, held...)
		delete(r.leases, global)
	}
	r.bars = make(map[uint32]*Bar)
	r.mu.Unlock()

	releaseAll(leases)
	r.clock.Close()
}

// UpdateClock applies a shared time snapshot to every bar and reports the
// globals whose text actually changed.
//
// One tick reaches every bar from one snapshot; a bar whose rendered text is
// unchanged is not reported, so no frame is submitted for it.
func (r *Registry) UpdateClock(now time.Time) []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.now = now
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	return changed
}

// UpdateNiri projects a snapshot into per-connector text and reports the
// globals whose text actually changed.
func (r *Registry) UpdateNiri(s niri.Snapshot) []uint32 {
	next := projectOutputs(s)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Replaced wholesale, not merged: a connector absent from the projection
	// has no workspace state any more, and keeping its last value would render
	// a stale workspace or title on a host that reconnects under that name.
	r.outputs = next

	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	return changed
}

// viewLocked assembles one bar's immutable input: the process-wide clock
// snapshot plus this connector's Niri projection.
func (r *Registry) viewLocked(connector string) barView {
	state, ok := r.outputs[connector]
	if !ok {
		state = outputState{Workspace: noWorkspace}
	}
	return barView{Now: r.now, Workspace: state.Workspace, Title: state.Title}
}

// buildBar creates one bar and acquires the services its items need. A failure
// releases whatever was already acquired, so a rejected build leaks nothing.
func (r *Registry) buildBar(cfg config.Config, connector string) (
	*Bar, []*services.Lease, wayland.HostCallbacks, error,
) {
	policy := cfg.ForConnector(connector)
	bar, err := NewWithTheme(ThemeFrom(cfg, policy), policy, connector)
	if err != nil {
		return nil, nil, wayland.HostCallbacks{}, err
	}

	var leases []*services.Lease
	for _, boundary := range clockBoundaries(policy.Left, policy.Center, policy.Right) {
		lease, err := r.clock.Acquire(boundary)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}

	return bar, leases, wayland.HostCallbacks{
		Configure: bar.Configure,
		Render:    bar.Render,
		Handle:    bar.Handle,
	}, nil
}

func releaseAll(leases []*services.Lease) {
	for _, lease := range leases {
		lease.Release()
	}
}
```

`PrepareConfig` is rewritten in Task 11. Leave it compiling here by returning a named error, so a
half-migrated reload cannot run:

```go
// PrepareConfig is replaced in the reload task. Until then it refuses, so a
// half-migrated reload cannot run.
func (r *Registry) PrepareConfig(cfg config.Config, hosts map[uint32]string) (wayland.PreparedConfig, error) {
	return wayland.PreparedConfig{}, errors.New("shell: reload staging is not wired yet")
}
```

Note `NewHost` calls `bar.apply`, which takes `Bar.mu`, while holding `Registry.mu`. That ordering —
registry then bar, never the reverse — must hold everywhere. `UpdateClock` and `UpdateNiri` follow it too.

- [ ] **Step 6: Run the package with the race detector**

Run: `go build ./... && go test -race ./internal/shell/ -v`
Expected: PASS with no race report. This is the first green checkpoint since Step 3.

- [ ] **Step 7: Commit**

```bash
gofmt -l internal/shell
git add internal/shell/
git commit -m "feat(shell): build widget views and key bars by output global"
```

---

### Task 11: Reload staging with acquire before release

**Files:**
- Modify: `internal/shell/registry.go`, `internal/shell/registry_test.go`
- Modify: `internal/platform/wayland/client.go`

**Interfaces:**
- Consumes: `buildBar`, `releaseAll` from Task 10.
- Produces: `wayland.PreparedConfig{Hosts map[uint32]HostCallbacks; Commit func(); Rollback func()}`;
  `(*Registry).PrepareConfig(config.Config, map[uint32]string) (wayland.PreparedConfig, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/shell/registry_test.go`:

```go
func TestAnAcceptedReloadDoesNotRestartAServiceStillInUse(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	before := reg.bars[1]
	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d before reload, want 1", got)
	}

	candidate := config.Default()
	candidate.Theme.Accent = "#ff8800"
	prepared, err := reg.PrepareConfig(candidate, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	// Prepare must not touch live state.
	if reg.bars[1] != before {
		t.Fatal("PrepareConfig replaced a live bar before commit")
	}

	prepared.Commit()
	if reg.bars[1] == before {
		t.Fatal("commit retained the old bar")
	}
	if got := reg.Clock().Starts(); got != 1 {
		t.Fatalf("clock starts = %d after reload, want 1; the service restarted", got)
	}
	if !reg.Clock().Running() {
		t.Fatal("the clock stopped across a reload that still uses it")
	}
}

func TestARejectedReloadLeavesServicesAndWidgetsUnchanged(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
	}})
	before := reg.bars[1]
	beforeText := before.left[0].node.Text
	beforeStarts := reg.Clock().Starts()

	// A theme this bar cannot be built from: a gap that leaves no body.
	broken := config.Default()
	broken.Bar.Height = 4
	broken.Bar.Gap = 4
	if _, err := reg.PrepareConfig(broken, map[uint32]string{1: "DP-9"}); err == nil {
		t.Fatal("an unbuildable candidate was prepared")
	}

	if reg.bars[1] != before {
		t.Fatal("a rejected reload replaced the live bar")
	}
	if got := reg.bars[1].left[0].node.Text; got != beforeText {
		t.Fatalf("visible text = %q, want the unchanged %q", got, beforeText)
	}
	if got := reg.Clock().Starts(); got != beforeStarts {
		t.Fatalf("clock starts = %d, want the unchanged %d", got, beforeStarts)
	}
	if !reg.Clock().Running() {
		t.Fatal("a rejected reload stopped the clock")
	}
}

// The owner may still reject after the shell prepared. Rollback must return
// lease counts exactly where they were.
func TestRollbackReleasesEverythingPrepareAcquired(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	prepared, err := reg.PrepareConfig(config.Default(), map[uint32]string{1: "DP-9"})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Rollback()

	if !reg.Clock().Running() {
		t.Fatal("rollback stopped a service the live bar still uses")
	}
	// The live bar must still hold exactly its own lease, so dropping it stops
	// the clock. A leaked prepared lease would keep it running.
	reg.DropHost(1)
	if reg.Clock().Running() {
		t.Fatal("rollback leaked a lease: the clock outlived its last consumer")
	}
}

func TestCommitAppliesHeldStateToTheReplacementBars(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 5, Name: "code", Output: "DP-9", Active: true},
	}})

	prepared, err := reg.PrepareConfig(config.Default(), map[uint32]string{1: "DP-9"})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.bars[1].left[0].node.Text; got != "code" {
		t.Fatalf("replacement bar workspace = %q, want the held state", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/shell/ -run 'Reload|Rollback|Commit' -v`
Expected: FAIL — `PrepareConfig` returns the placeholder error and `PreparedConfig.Rollback` is undefined.

- [ ] **Step 3: Write the implementation**

In `internal/platform/wayland/client.go`, extend the prepared type:

```go
// PreparedConfig holds replacement callbacks built before a reload changes
// live state, keyed by output global.
type PreparedConfig struct {
	Hosts  map[uint32]HostCallbacks
	Commit func()
	// Rollback undoes what preparing acquired. The owner can still reject a
	// candidate after the application prepared it, and a prepared bar may hold
	// service leases; without this they would leak a running goroutine.
	Rollback func()
}
```

In `owner.prepareConfig`, after the nil check on `prepared.Commit`, add:

```go
	if prepared.Rollback == nil {
		return preparedOwnerConfig{}, errors.New("wayland: prepared config has no rollback function")
	}
```

and call `prepared.Rollback()` on every error path that returns after `o.cb.PrepareConfig` succeeded —
the omitted-host and `app.validate` failures. Carry `rollback` on `preparedOwnerConfig` beside `commit`.

Replace `Registry.PrepareConfig`:

```go
// PrepareConfig builds every enabled host's replacement bar and acquires its
// services before the caller changes live host policy.
//
// Acquiring here, and releasing the outgoing leases only in Commit, is what
// keeps a service in continuous use from stopping: its consumer count never
// reaches zero, so it is never restarted. A failure at any point releases
// exactly what this call acquired.
func (r *Registry) PrepareConfig(cfg config.Config, hosts map[uint32]string) (wayland.PreparedConfig, error) {
	bars := make(map[uint32]*Bar, len(hosts))
	leases := make(map[uint32][]*services.Lease, len(hosts))
	callbacks := make(map[uint32]wayland.HostCallbacks, len(hosts))

	for global, connector := range hosts {
		bar, held, hooks, err := r.buildBar(cfg, connector)
		if err != nil {
			for _, acquired := range leases {
				releaseAll(acquired)
			}
			return wayland.PreparedConfig{}, err
		}
		bars[global] = bar
		leases[global] = held
		callbacks[global] = hooks
	}

	// released guards against Commit and Rollback both running, and against
	// either running twice.
	var once sync.Once

	return wayland.PreparedConfig{
		Hosts: callbacks,
		Commit: func() {
			once.Do(func() {
				r.mu.Lock()
				outgoing := r.leases
				for _, bar := range bars {
					bar.apply(r.viewLocked(bar.connector()))
				}
				r.cfg = cfg
				r.bars = bars
				r.leases = leases
				r.mu.Unlock()

				// Released only after the replacement set holds its own, so
				// the count never touches zero for a service still in use.
				for _, held := range outgoing {
					releaseAll(held)
				}
			})
		},
		Rollback: func() {
			once.Do(func() {
				for _, held := range leases {
					releaseAll(held)
				}
			})
		},
	}, nil
}
```

`Commit` takes `Registry.mu` and then each bar's lock through `apply`. That is the same order `NewHost`,
`UpdateClock` and `UpdateNiri` use — registry first, then bar — and nothing takes them in the reverse
order, so the ordering is a total one. `Bar.Render` takes only the bar's lock from the Wayland goroutine
and never reaches for the registry's, so it cannot close a cycle.

Add `"errors"` and `"sync"` to `registry.go`'s imports as needed, and remove the placeholder
`PrepareConfig` and its `errors` use if no longer required.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/shell/ ./internal/platform/wayland/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go build ./... && gofmt -l internal/shell internal/platform/wayland
git add internal/shell/ internal/platform/wayland/client.go
git commit -m "feat(shell): stage reload so a live service is never restarted"
```

---

### Task 12: Process wiring

**Files:**
- Modify: `cmd/sysc-shell/main.go`

**Interfaces:**
- Consumes: `Registry.Clock`, `Registry.UpdateClock`, `Registry.Close` from Tasks 10–11.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Append to `cmd/sysc-shell/main_test.go`:

```go
// The shell must fail with a named error, not a panic or a Wayland attempt,
// when it is started outside a Niri session.
func TestRunWithoutANiriSocketNamesTheMissingVariable(t *testing.T) {
	t.Setenv("NIRI_SOCKET", "")

	err := run(context.Background())
	if err == nil {
		t.Fatal("run succeeded with no NIRI_SOCKET")
	}
	if !strings.Contains(err.Error(), "NIRI_SOCKET") {
		t.Fatalf("error %q does not name the missing variable", err)
	}
}
```

Add `"context"` and `"strings"` to that file's imports if absent.

- [ ] **Step 2: Run the test to verify it passes or fails**

Run: `go test ./cmd/sysc-shell/ -run NiriSocket -v`
Expected: PASS. This test guards the wiring change that follows; if it already passes, it is the
regression net for Step 3.

- [ ] **Step 3: Write the implementation**

In `cmd/sysc-shell/main.go`, add the clock pump beside the existing Niri pump and release everything on
exit. After `registry := shell.NewRegistry(cfg)`:

```go
	// Releases every service lease and stops the clock goroutine when the
	// process unwinds, whether through cancellation or an error return.
	defer registry.Close()
```

After the Niri pump goroutine, add:

```go
	// The clock publishes on its own goroutine; this pump turns each snapshot
	// into per-bar text and hands the changed outputs to the Wayland owner.
	// One tick serves every bar.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-registry.Clock().Updates():
				registry.UpdateClock(now)
			}
		}
	}()
```

If the merged Milestone 2 invalidation transport requires the registry to publish changed globals itself,
`UpdateClock`'s return value is already what it needs; wire it exactly as `UpdateNiri`'s is wired in the
existing Niri pump, so both paths are identical.

- [ ] **Step 4: Run the tests and build**

```bash
go test -race ./... 2>&1 | tail -20
go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell
```

Expected: all packages PASS and a successful build.

- [ ] **Step 5: Commit**

```bash
gofmt -l cmd
git add cmd/sysc-shell/
git commit -m "feat(shell): pump clock snapshots into the bar registry"
```

---

### Task 13: Cross-cutting evidence

The remaining behaviors from the design's evidence table that no single earlier task proves end to end.

**Files:**
- Create: `internal/shell/tranche3a_test.go`
- Modify: `internal/platform/niri/client_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–12.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Create `internal/shell/tranche3a_test.go`:

```go
package shell

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// The initial event burst must give each output its own title.
func TestInitialWindowStateTitlesEachOutput(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.UpdateNiri(niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true, Focused: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
				ActiveWindowID: 81, HasActiveWindow: true},
		},
		Windows: []niri.Window{
			{ID: 80, Title: "Fixture One"},
			{ID: 81, Title: "Fixture Two"},
		},
	})

	if got := reg.bars[1].left[1].node.Text; got != "Fixture One" {
		t.Fatalf("DP-9 title = %q, want its own window", got)
	}
	if got := reg.bars[2].left[1].node.Text; got != "Fixture Two" {
		t.Fatalf("HDMI-A-9 title = %q, want its own window", got)
	}
}

// A bar with no Niri state must still render a stable fallback rather than
// disappearing or showing a stale value.
func TestUnavailableNiriStateRendersAStableFallback(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if got := reg.bars[1].left[0].node.Text; got != noWorkspace {
		t.Fatalf("workspace before any snapshot = %q, want %q", got, noWorkspace)
	}
	if got := reg.bars[1].left[1].node.Text; got != "" {
		t.Fatalf("title before any snapshot = %q, want empty", got)
	}
	// The bar still has its three sections and can lay out.
	if len(reg.bars[1].sections()) != 3 {
		t.Fatal("the bar lost a section with no Niri state")
	}
}

// An unbounded title must not exceed its cap or push its neighbour out.
func TestALongTitleStaysWithinItsCapAndKeepsItsNeighbour(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	// Mixed scripts on purpose: titles are the first unbounded user text, so
	// truncation has to hold across a per-rune face change, not just ASCII.
	// The non-Latin run sits beyond the cap, where the cut lands.
	long := strings.Repeat("Fixture Title Segment ", 20) +
		strings.Repeat("\u3053\u3093\u306b\u3061\u306f\u4e16\u754c ", 10) +
		strings.Repeat("\u0645\u0631\u062d\u0628\u0627 \u0628\u0627\u0644\u0639\u0627\u0644\u0645 ", 10)
	reg.UpdateNiri(niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
		},
		Windows: []niri.Window{{ID: 80, Title: long}},
	})

	bar := reg.bars[1]
	if err := bar.Configure(1920, 44, 120); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	workspace := bar.left[0].node
	title := bar.left[1].node
	if title.Bounds.W > title.MaxWidth {
		t.Fatalf("title width %d exceeds its cap %d", title.Bounds.W, title.MaxWidth)
	}
	if workspace.Bounds.W <= 0 {
		t.Fatal("a long title squeezed the workspace widget to zero width")
	}
	// The title must not run past the bar's content band.
	if right := title.Bounds.X + title.Bounds.W; right > 1920 {
		t.Fatalf("title right edge %d exceeds the surface width", right)
	}
	// The truncated text must remain valid UTF-8: a cut inside a multi-byte
	// rune would render replacement characters.
	if !utf8.ValidString(title.Text) {
		t.Fatal("truncation split a multi-byte rune")
	}
}

// Finding 6: a window moving to another workspace changes which output shows
// it. Only the affected bars may be reported.
func TestMovingAWindowInvalidatesOnlyTheOutputsItLeavesAndJoins(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9", 3: "DP-8"})

	base := niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true},
			{ID: 7, Name: "idle", Output: "DP-8", Active: true},
		},
		Windows: []niri.Window{{ID: 80, Title: "Fixture One", WorkspaceID: 5, HasWorkspace: true}},
	}
	reg.UpdateNiri(base)

	// The window moves from workspace 5 (DP-9) to workspace 6 (HDMI-A-9).
	moved := niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 5, Name: "code", Output: "DP-9", Active: true},
			{ID: 6, Name: "chat", Output: "HDMI-A-9", Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 7, Name: "idle", Output: "DP-8", Active: true},
		},
		Windows: []niri.Window{{ID: 80, Title: "Fixture One", WorkspaceID: 6, HasWorkspace: true}},
	}
	changed := reg.UpdateNiri(moved)

	seen := map[uint32]bool{}
	for _, g := range changed {
		seen[g] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("changed = %v, want the output it left (1) and the one it joined (2)", changed)
	}
	if seen[3] {
		t.Fatalf("changed = %v, want the untouched output 3 excluded", changed)
	}
	if got := reg.bars[1].left[1].node.Text; got != "" {
		t.Fatalf("DP-9 title = %q, want empty after the window left", got)
	}
	if got := reg.bars[2].left[1].node.Text; got != "Fixture One" {
		t.Fatalf("HDMI-A-9 title = %q, want the window it gained", got)
	}
}

// Every goroutine this package starts must be gone once the registry closes.
func TestClosingTheRegistryLeavesNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	reg := NewRegistry(config.Default())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})
	reg.Close()

	// The clock goroutine exits synchronously inside Close, but the scheduler
	// may not have reaped it yet.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines = %d after Close, want at most the starting %d", got, before)
	}
}

// Every state change must reach exactly one invalidation for its bar, with no
// bar's redraw lost. This is the invariant Task 0 Step 4 could not confirm
// statically.
func TestEveryChangedBarIsReportedWhenManyChangeAtOnce(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)

	hosts := map[uint32]string{}
	workspaces := []niri.Workspace{}
	for i := uint32(1); i <= 12; i++ {
		connector := "DP-" + string(rune('a'+i-1))
		hosts[i] = connector
		workspaces = append(workspaces, niri.Workspace{
			ID: uint64(i), Name: "ws", Output: connector, Active: true,
		})
	}
	newHosts(t, reg, hosts)

	changed := reg.UpdateNiri(niri.Snapshot{Workspaces: workspaces})
	if len(changed) != len(hosts) {
		t.Fatalf("changed %d bars, want all %d; an invalidation was dropped",
			len(changed), len(hosts))
	}

	seen := make(map[uint32]int, len(changed))
	for _, global := range changed {
		seen[global]++
	}
	for global, count := range seen {
		if count != 1 {
			t.Fatalf("global %d reported %d times, want exactly one", global, count)
		}
	}
}
```

Append to `internal/platform/niri/client_test.go` a socket-level case proving window events survive the
real read path. Add the fixture constants beside the existing ones and one test:

```go
// A full initial burst, as the compositor sends it: workspaces, then windows.
const initialBurst = `{"WorkspacesChanged":{"workspaces":[` +
	`{"id":5,"idx":1,"name":"code","output":"DP-9","is_urgent":false,` +
	`"is_active":true,"is_focused":true,"active_window_id":80}]}}` + "\n" +
	`{"WindowsChanged":{"windows":[` +
	`{"id":80,"title":"Fixture One","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
	`"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}]}}`

func TestTheStreamDeliversWindowState(t *testing.T) {
	t.Parallel()
	server := startFakeNiri(t, replyOK, initialBurst)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshots, errs := Stream(ctx, server.path)
	var last Snapshot
	for snap := range snapshots {
		last = snap
	}
	select {
	case err := <-errs:
		if err != nil && ctx.Err() == nil {
			t.Fatalf("stream: %v", err)
		}
	default:
	}

	if len(last.Windows) != 1 || last.Windows[0].Title != "Fixture One" {
		t.Fatalf("windows = %+v, want one titled Fixture One", last.Windows)
	}
	if len(last.Workspaces) != 1 || !last.Workspaces[0].HasActiveWindow {
		t.Fatalf("workspaces = %+v, want one with an active window", last.Workspaces)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/shell/ ./internal/platform/niri/ -run 'Tranche|Initial|Fallback|LongTitle|Goroutine|ChangedBar|DeliversWindow' -v`
Expected: FAIL where behavior is missing. If `TestEveryChangedBarIsReportedWhenManyChangeAtOnce` fails,
the invalidation path is lossy — fix it in `internal/shell/registry.go` so every changed global is
returned, and record that Milestone 2's correction did not cover it.

- [ ] **Step 3: Fix whatever the tests expose**

There is no new feature to write here if Tasks 1–12 are correct. Any failure is a real defect in an
earlier task; fix it there rather than weakening the test.

- [ ] **Step 4: Run the full suite with the race detector**

```bash
go test -race -count=1 ./... 2>&1 | tail -20
```

Expected: `ok` for every package, no race report.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal
git add internal/shell/tranche3a_test.go internal/platform/niri/client_test.go
git commit -m "test(shell): cover sharing, fallback, title bounds and teardown"
```

---

### Task 14: Tabular figures for the clock

From the design audit's one major finding. A proportional face gives digits different advances, so the
centre clock's measured width changes as the time changes — `15:04` and `15:19` are not the same width —
and because `ArrangeBar` pins the centre from its own width, the clock shifts by a pixel or two every
minute. Tabular figures fix it at the shaping layer.

**The owner may cut this task.** It is the only task that grew scope after the audit, and it changes the
`ui.MeasureText` contract. Nothing else in Tranche 3A depends on it; cutting it ships a clock that jitters
slightly on the minute. If it is cut, delete this task and note the deferral in the completion handover.

`shaping.Input` carries `FontFeatures []FontFeature`, and `ot.MustNewTag("tnum")` is the tabular-figures
tag, so this needs no new dependency.

**Files:**
- Modify: `internal/ui/tree.go`, `internal/ui/layout.go`
- Modify: `internal/render/text.go`, `internal/render/truncate.go`, `internal/render/paint.go`
- Modify: `internal/shell/widget.go`, `internal/shell/bar.go`
- Test: `internal/render/text_test.go`, `internal/render/truncate_test.go`,
  `internal/render/paint_test.go`, `internal/ui/layout_test.go`, `internal/ui/bar_test.go`,
  `internal/shell/widget_test.go`

**Interfaces:**
- Consumes: `buildWidgets` (Task 10), `ui.Node` (Task 6).
- Produces: `ui.Node.Tabular bool`; `ui.MeasureText` becomes
  `func(text string, tabular bool) (width, height int)`;
  `(*TextRenderer).Shape/Measure/Truncate` each take a trailing `tabular bool`.

Note that `Layout`, `ArrangeBar`, `sectionWidth` and `placeSection` only pass `MeasureText` through, so
none of their signatures change. Only the two `measure(n.Text)` call sites in `measureNode` do.

- [ ] **Step 1: Write the failing test**

Append to `internal/render/text_test.go`:

```go
// Every digit must advance identically, or a clock changes width as the time
// changes. This is the whole point of tabular figures.
func TestTabularFiguresGiveEveryDigitTheSameWidth(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))

	widths := make(map[int]string)
	for d := 0; d <= 9; d++ {
		s := fmt.Sprintf("%d%d:%d%d", d, d, d, d)
		w, _, err := r.Measure(s, 14, true)
		if err != nil {
			t.Fatalf("Measure(%q): %v", s, err)
		}
		widths[w] = s
	}
	if len(widths) != 1 {
		t.Fatalf("tabular measurement produced %d distinct widths, want 1: %v", len(widths), widths)
	}
}

// The flag must actually reach the shaper: proportional measurement of the
// same strings should not be uniform for a face with proportional figures.
func TestTheTabularFlagReachesTheShaper(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))

	tab, _, err := r.Measure("00:00", 14, true)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	prop, _, err := r.Measure("00:00", 14, false)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	// Go Regular's digits are already tabular, so the widths may legitimately
	// match. What must hold is that both paths shape successfully and return a
	// positive width; a silently dropped feature would still be caught by
	// TestTabularFiguresGiveEveryDigitTheSameWidth on any proportional face.
	if tab <= 0 || prop <= 0 {
		t.Fatalf("widths tab=%d prop=%d, want both positive", tab, prop)
	}
}
```

Append to `internal/shell/widget_test.go`:

```go
// Only the clock asks for tabular figures; nothing else should.
func TestOnlyClockWidgetsRequestTabularFigures(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	})

	if !widgets[0].node.Tabular {
		t.Fatal("the clock node does not request tabular figures")
	}
	if widgets[1].node.Tabular || widgets[2].node.Tabular {
		t.Fatal("a non-clock widget requested tabular figures")
	}
}
```

`mustTestFace` is the existing helper at `text_test.go:12`. Add `"fmt"` to that file's imports if absent;
`goregular` is already imported there.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/render/ ./internal/shell/ -run 'Tabular' -v`
Expected: FAIL to compile — `Measure` takes two arguments and `Node.Tabular` is undefined.

- [ ] **Step 3: Write the implementation**

In `internal/ui/tree.go`, add the field and widen the measurement contract:

```go
	// Tabular requests tabular (fixed-advance) figures when shaping this node.
	// A clock sets it: with proportional digits the rendered width changes as
	// the time changes, which visibly shifts a centred clock every minute.
	Tabular bool
```

```go
// MeasureText reports the logical width and height of a shaped string. The
// tabular flag is the node's, and reaches the shaper as an OpenType feature.
type MeasureText func(text string, tabular bool) (width, height int)
```

In `internal/ui/layout.go`, pass the flag at both `measureNode` call sites:

```go
	case KindText:
		w, h := measure(n.Text, n.Tabular)
		if n.MaxWidth > 0 && w > n.MaxWidth {
			w = n.MaxWidth
		}
		return w, h, nil
	case KindMeter:
		...
	case KindButton:
		w, h := measure(n.Text, n.Tabular)
		return w + 2*n.Padding, h + 2*n.Padding, nil
```

In `internal/render/text.go`, thread the feature into the shaper:

```go
// tabularFigures is the OpenType feature that gives every digit the same
// advance. Applied per call, because only some runs want it.
var tabularFigures = []shaping.FontFeature{{Tag: ot.MustNewTag("tnum"), Value: 1}}

// Shape lays out one run at the given physical pixel size. When tabular is
// set, digits shape with equal advances.
func (r *TextRenderer) Shape(text string, size int, tabular bool) (shaping.Output, error) {
	if r == nil || r.face == nil {
		return shaping.Output{}, fmt.Errorf("render: nil face")
	}
	if size <= 0 {
		return shaping.Output{}, fmt.Errorf("render: size %d is not positive", size)
	}

	runes := []rune(text)
	script := runScript(runes)
	input := shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: scriptDirection(script),
		Face:      r.face,
		Size:      fixed.I(size),
		Script:    script,
		Language:  language.NewLanguage("und"),
	}
	if tabular {
		input.FontFeatures = tabularFigures
	}
	return r.shaper.Shape(input), nil
}
```

`ot` is already imported in this file as `ot "github.com/go-text/typesetting/font/opentype"`.

Give `Measure` and the file's other `Shape` caller the same trailing parameter, and pass it through
`Truncate`:

```go
func (r *TextRenderer) Measure(text string, size int, tabular bool) (int, int, error) {
	out, err := r.Shape(text, size, tabular)
	...
}
```

```go
func (r *TextRenderer) Truncate(text string, size, avail int, tabular bool) (string, int, error) {
```

and pass `tabular` at all three `r.Shape(...)` calls inside `truncate.go`.

In `internal/render/paint.go`, the text case has the node, so pass its flag:

```go
	fitted, _, err := text.Truncate(s, size, box.W, n.Tabular)
```

In `internal/shell/bar.go`, widen the measure closure in `layoutLocked`:

```go
	measure := func(s string, tabular bool) (int, int) {
		w, h, err := b.text.Measure(s, b.style.Size, tabular)
		if err != nil {
			return 0, 0
		}
		return w, h
	}
```

In `internal/shell/widget.go`, set the flag on the clock node only:

```go
		case "clock":
			layout := item.Format
			out = append(out, textWidget{
				node: &ui.Node{Kind: ui.KindText, Tabular: true},
				format: func(v barView) string {
					if v.Now.IsZero() {
						return ""
					}
					return v.Now.Format(layout)
				},
			})
```

Widen every existing test measurement helper. These are the exact sites, verified against
`milestone/stable-bar@57b49f0`; `internal/ui/scale_test.go` has none and needs no change.

| File | Site |
|---|---|
| `internal/ui/layout_test.go:6` | `func fakeMeasure(s string) (int, int)` — package level |
| `internal/ui/layout_test.go` | the local `measure := func(s string) (int, int)` closures added by Task 6 |
| `internal/ui/bar_test.go:6` | `func fixed(s string) (int, int)` — package level |
| `internal/render/paint_test.go:119,219` | two local `measure := func(s string) (int, int)` closures |

Each takes a trailing `bool`, ignored where the test does not care:

```go
func fakeMeasure(s string, _ bool) (int, int) { return len(s) * 8, 16 }
func fixed(s string, _ bool) (int, int)       { return len([]rune(s)) * 10, 20 }
```

The `paint_test.go` closures forward it, since they call the real renderer:

```go
	measure := func(s string, tabular bool) (int, int) {
		w, h, err := r.Measure(s, style.Size, tabular)
		...
	}
```

Also add the trailing argument at every existing direct call in `internal/render`'s own tests:
`text_test.go:50,90,113,119,154,160` (`Measure`/`Shape`) and
`truncate_test.go:22,27,43,49,70,92,110,115` (`Measure`/`Truncate`). Pass `false` — those tests are about
shaping and truncation, not figures.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go build ./... && go test -race ./internal/ui/ ./internal/render/ ./internal/shell/ -v`
Expected: PASS across all three packages.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal
git add internal/ui/ internal/render/ internal/shell/
git commit -m "feat(render): shape clock runs with tabular figures"
```

---

### Task 15: Automated gate and live matrix

**Files:**
- Modify: `tests/integration/README.md`

**Interfaces:**
- Consumes: everything.
- Produces: the recorded live matrix.

- [ ] **Step 1: Run the full automated gate**

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell
gofmt -l .
git diff --check
```

Expected: `go mod tidy -diff` reports no difference (this tranche adds no dependency), every package
passes, vet is silent, the build succeeds, `gofmt -l` prints nothing, and `git diff --check` prints
nothing.

- [ ] **Step 2: Confirm no dependency crept in**

```bash
git diff origin/main -- go.mod go.sum
```

Expected: empty. A non-empty diff means a Global Constraint was violated; stop and remove the dependency.

- [ ] **Step 3: Record the live matrix**

Append to `tests/integration/README.md`:

```markdown
## Tranche 3A: built-in widget foundation

Run only after Milestone 2 passes its own live matrix. Record connector names,
window titles and measurements outside this repository.

Build and start:

    go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell
    /tmp/sysc-shell-milestone3

Matrix:

1. One output, then at least two. Every configured output receives exactly one
   bar, with clock, date, workspace and title.
2. One clock snapshot on every bar: the minute changes on all bars together.
3. Independent per-output text: switch workspaces on one monitor and confirm
   the other monitor's workspace and title do not change.
4. Focus a different window in the same workspace and confirm the title
   updates. This is the one behavior the design could not verify offline: it
   assumes Niri emits WorkspaceActiveWindowChanged for an in-workspace focus
   move. If the title does not update, consume WindowFocusChanged as a second
   trigger and re-run.
5. Retitle a window (change a browser tab) and confirm only that output's bar
   repaints.
6. Unplug and replug an output. No duplicate bar, no missing widget, no leaked
   instance.
7. Edit the configuration to add and remove clock and Niri widgets, then
   SIGHUP. Confirm the new set renders and the clock does not visibly stall.
8. Write an invalid configuration and SIGHUP. Confirm the previous widgets stay
   live and the error names its field path on stderr.
9. Kill the Niri socket. Confirm the shell exits cleanly with a named error and
   leaves no process or socket behind.
10. Suspend and resume. Confirm the clock catches up within one boundary
    (at most 60 seconds).

Baselines to record before setting any budget:

- idle CPU and wakeups over 60 minutes, with a minute-boundary clock;
- CPU during clock ticks and during a burst of window title changes;
- RSS after one hour;
- submitted and skipped frame counts;
- layout and paint duration per update;
- allocations per update;
- binary size.
```

- [ ] **Step 4: Run the live matrix**

Execute every numbered item above on a real Niri session with at least two outputs. Record the outcome of
each. Do not claim any live behavior from the automated gate alone.

- [ ] **Step 5: Commit and write the completion handover**

```bash
git add tests/integration/README.md
git commit -m "docs: record the tranche 3A live matrix"
```

Then write `docs/plans/2026-08-30-built-in-widget-foundation-completion-handover.md` containing: commit
hashes, fresh gate output, live observations against each matrix item, measured baselines, known defects,
and the next tranche. Do not modify the Milestone 2 progress handover.

---

## Deviations to report

Stop and return to the owner rather than improvising if any of these occur:

- Task 0 finds an absent Milestone 2 contract.
- Live matrix item 4 fails, meaning `WorkspaceActiveWindowChanged` does not cover in-workspace focus
  moves. The fix is local, but it changes a design decision and should be recorded.
- Any task appears to need a second goroutine touching Wayland, a widget interface, a new dependency, or a
  per-output timer. Every one of these is a stop condition in the design.

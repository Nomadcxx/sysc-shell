package niri

import "testing"

// Wire fixtures follow the Niri 26.04 schema. Titles and connectors are
// invented; no real window or machine state appears here.
const (
	windowsChangedFixture = `{"WindowsChanged":{"windows":[` +
		`{"id":80,"title":"Fixture One","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
		`"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{},` +
		`"focus_timestamp":{"secs":166673,"nanos":194678785}},` +
		`{"id":81,"title":null,"app_id":null,"pid":null,"workspace_id":null,` +
		`"is_focused":false,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}]}}`

	windowFocusChangedFixture = `{"WindowFocusChanged":{"id":81}}`
	windowFocusChangedNull    = `{"WindowFocusChanged":{"id":null}}`

	windowOpenedFixture = `{"WindowOpenedOrChanged":{"window":` +
		`{"id":82,"title":"Fixture Two","app_id":"fixture.two","pid":1001,"workspace_id":5,` +
		`"is_focused":false,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}}}`

	windowRetitledFixture = `{"WindowOpenedOrChanged":{"window":` +
		`{"id":80,"title":"Fixture One Renamed","app_id":"fixture.one","pid":1000,"workspace_id":5,` +
		`"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{},"focus_timestamp":null}}}`

	windowClosedFixture = `{"WindowClosed":{"id":80}}`
	windowClosedUnknown = `{"WindowClosed":{"id":9999}}`
	windowMissingIDFixt = `{"WindowOpenedOrChanged":{"window":{"title":"No Identity"}}}`
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
	if !snap.Windows[0].Focused {
		t.Fatalf("window 80 focused = false, want true")
	}
	wantTS := int64(166673)*1e9 + 194678785
	if snap.Windows[0].FocusTimestamp != wantTS {
		t.Fatalf("window 80 FocusTimestamp = %d, want %d", snap.Windows[0].FocusTimestamp, wantTS)
	}
	// A null title, app_id and workspace_id are legal and must not fail the event.
	if got := snap.Windows[1]; got.ID != 81 || got.Title != "" || got.HasWorkspace {
		t.Fatalf("second window = %+v, want id 81 with empty title and no workspace", got)
	}
	if snap.Windows[1].Focused || snap.Windows[1].FocusTimestamp != 0 {
		t.Fatalf("window 81 focus = %+v, want unfocused with null timestamp", snap.Windows[1])
	}
}

func TestWindowFocusChanged(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture, windowFocusChangedFixture)
	snap := s.snapshot()
	var w80, w81 Window
	for _, w := range snap.Windows {
		switch w.ID {
		case 80:
			w80 = w
		case 81:
			w81 = w
		}
	}
	if w80.Focused || !w81.Focused {
		t.Fatalf("after WindowFocusChanged: 80 focused=%v, 81 focused=%v; want 80 false, 81 true",
			w80.Focused, w81.Focused)
	}
}

func TestWindowFocusChangedNullClearsFocus(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture, windowFocusChangedNull)
	for _, w := range s.snapshot().Windows {
		if w.Focused {
			t.Fatalf("window %d still focused after id:null", w.ID)
		}
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
		`"is_focused":true,"is_floating":true,"is_urgent":true,"layout":{},` +
		`"focus_timestamp":{"secs":166673,"nanos":194678785}}}}`

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

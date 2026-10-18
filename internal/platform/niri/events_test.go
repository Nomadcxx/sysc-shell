package niri

import (
	"encoding/json"
	"testing"
)

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

	windowOpenedFloatingFixture = `{"WindowOpenedOrChanged":{"window":` +
		`{"id":82,"title":"Fixture Two","app_id":"fixture.two","pid":1001,"workspace_id":5,` +
		`"is_focused":false,"is_floating":true,"is_urgent":false,` +
		`"layout":{"tile_size":[1200.5,800.25],"tile_pos_in_workspace_view":[40.5,60.25]},` +
		`"focus_timestamp":null}}}`

	windowLayoutsFixture = `{"WindowLayoutsChanged":{"changes":[` +
		`[80,{"tile_size":[1200.5,800.25],"tile_pos_in_workspace_view":[40.5,60.25]}],` +
		`[9999,{"tile_size":[1,1]}]]}}`
	windowLayoutCleared   = `{"WindowLayoutsChanged":{"changes":[[80,null]]}}`
	windowLayoutMalformed = `{"WindowLayoutsChanged":{"changes":[[80]]}}`
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

func castSnapshotJSON(t *testing.T, s *state) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(s.snapshot())
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return snapshot
}

func TestCastsChangedPublishesKnownBaseline(t *testing.T) {
	t.Parallel()
	var s state
	changed, err := s.apply([]byte(`{"CastsChanged":{"casts":[]}}`))
	if err != nil || !changed {
		t.Fatalf("apply empty baseline = (%v, %v), want (true, nil)", changed, err)
	}
	snapshot := castSnapshotJSON(t, &s)
	var known bool
	if err := json.Unmarshal(snapshot["CastsKnown"], &known); err != nil || !known {
		t.Fatalf("CastsKnown = %s, want true", snapshot["CastsKnown"])
	}
	var casts []json.RawMessage
	if err := json.Unmarshal(snapshot["Casts"], &casts); err != nil || len(casts) != 0 {
		t.Fatalf("Casts = %s, want an empty list", snapshot["Casts"])
	}
}

func TestCastsChangedSortsSnapshot(t *testing.T) {
	t.Parallel()
	line := `{"CastsChanged":{"casts":[` +
		`{"stream_id":29,"is_active":false,"target":{"Nothing":{}}},` +
		`{"stream_id":21,"is_active":false,"target":{"Nothing":{}}}]}}`
	s := applyAll(t, line)
	var casts []struct {
		StreamID uint64 `json:"StreamID"`
	}
	if err := json.Unmarshal(castSnapshotJSON(t, s)["Casts"], &casts); err != nil {
		t.Fatalf("decode casts: %v", err)
	}
	if len(casts) != 2 || casts[0].StreamID != 21 || casts[1].StreamID != 29 {
		t.Fatalf("cast order = %+v, want stream IDs 21, 29", casts)
	}
}

func TestCastStartedOrChangedUpsertsAndCastStoppedRemoves(t *testing.T) {
	t.Parallel()
	s := applyAll(t, `{"CastsChanged":{"casts":[]}}`)
	started := `{"CastStartedOrChanged":{"cast":{"stream_id":22,"is_active":true,` +
		`"target":{"Output":{"name":"DP-9"}},"pw_node_id":44}}}`
	changed, err := s.apply([]byte(started))
	if err != nil || !changed {
		t.Fatalf("apply cast start = (%v, %v), want (true, nil)", changed, err)
	}
	snapshot := castSnapshotJSON(t, s)
	var casts []struct {
		StreamID    uint64 `json:"StreamID"`
		Active      bool   `json:"Active"`
		TargetSet   bool   `json:"TargetSet"`
		TargetKnown bool   `json:"TargetKnown"`
		PWNodeID    uint32 `json:"PWNodeID"`
		HasPWNodeID bool   `json:"HasPWNodeID"`
	}
	if err := json.Unmarshal(snapshot["Casts"], &casts); err != nil {
		t.Fatalf("decode casts: %v", err)
	}
	if len(casts) != 1 || casts[0].StreamID != 22 || !casts[0].Active ||
		!casts[0].TargetSet || !casts[0].TargetKnown || casts[0].PWNodeID != 44 || !casts[0].HasPWNodeID {
		t.Fatalf("cast snapshot = %+v, want active output cast 22 with PipeWire node 44", casts)
	}
	changed, err = s.apply([]byte(`{"CastStopped":{"stream_id":22}}`))
	if err != nil || !changed {
		t.Fatalf("apply cast stop = (%v, %v), want (true, nil)", changed, err)
	}
	if got := castSnapshotJSON(t, s)["Casts"]; string(got) != "[]" {
		t.Fatalf("Casts after stop = %s, want []", got)
	}
}

func TestUnknownCastTargetDoesNotBecomeInactive(t *testing.T) {
	t.Parallel()
	s := applyAll(t, `{"CastsChanged":{"casts":[]}}`)
	event := `{"CastStartedOrChanged":{"cast":{"stream_id":23,"is_active":true,` +
		`"target":{"FutureTarget":{}},"pw_node_id":null}}}`
	if changed, err := s.apply([]byte(event)); err != nil || !changed {
		t.Fatalf("apply cast with unknown target = (%v, %v), want (true, nil)", changed, err)
	}
	snapshot := castSnapshotJSON(t, s)
	var casts []struct {
		Active      bool `json:"Active"`
		TargetSet   bool `json:"TargetSet"`
		TargetKnown bool `json:"TargetKnown"`
	}
	if err := json.Unmarshal(snapshot["Casts"], &casts); err != nil {
		t.Fatalf("decode casts: %v", err)
	}
	if len(casts) != 1 || !casts[0].Active || casts[0].TargetKnown || casts[0].TargetSet {
		t.Fatalf("cast state = %+v, want active with an unknown target", casts)
	}
}

func TestCastWindowTargetRequiresANonzeroWindowID(t *testing.T) {
	t.Parallel()
	s := applyAll(t, `{"CastsChanged":{"casts":[{"stream_id":25,"is_active":true,"target":{"Window":{"id":0}}}]}}`)
	cast := s.snapshot().Casts[0]
	if cast.TargetKnown || cast.TargetSet {
		t.Fatalf("zero-ID target = %+v, want unknown and unset", cast)
	}
}

func TestMalformedCastEventLeavesBaselineUnchanged(t *testing.T) {
	t.Parallel()
	s := applyAll(t, `{"CastsChanged":{"casts":[]}}`)
	if _, err := s.apply([]byte(`{"CastStartedOrChanged":{"cast":{"stream_id":24}}}`)); err == nil {
		t.Fatal("cast without is_active was accepted")
	}
	snapshot := castSnapshotJSON(t, s)
	var casts []json.RawMessage
	if err := json.Unmarshal(snapshot["Casts"], &casts); err != nil || len(casts) != 0 {
		t.Fatalf("malformed cast changed state: %s", snapshot["Casts"])
	}
}

func TestWindowsChangedReplacesTheWholeSet(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture)

	snap := s.snapshot()
	if len(snap.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(snap.Windows))
	}
	if got := snap.Windows[0]; got.ID != 80 || got.Title != "Fixture One" || got.AppID != "fixture.one" || got.Pid != 1000 {
		t.Fatalf("first window = %+v, want id 80 pid 1000 titled Fixture One", got)
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
	if got := snap.Windows[1]; got.ID != 81 || got.Title != "" || got.HasWorkspace || got.Pid != 0 {
		t.Fatalf("second window = %+v, want id 81 with empty title, no workspace, and no pid", got)
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

	// One urgent workspace beside one that omits is_urgent entirely: urgency
	// is optional on the wire and false when the field is missing.
	urgentWorkspacesFixture = `{"WorkspacesChanged":{"workspaces":[` +
		`{"id":7,"idx":2,"name":null,"output":"DP-9","is_urgent":true,` +
		`"is_active":true,"is_focused":false,"active_window_id":null},` +
		`{"id":8,"idx":3,"name":null,"output":"DP-9",` +
		`"is_active":false,"is_focused":false,"active_window_id":null}]}}`
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

func TestWorkspacesCarryUrgency(t *testing.T) {
	t.Parallel()
	s := applyAll(t, urgentWorkspacesFixture)
	snap := s.snapshot()

	if got := workspaceByID(t, snap, 7); !got.Urgent {
		t.Fatalf("workspace 7 urgent = false, want true")
	}
	if got := workspaceByID(t, snap, 8); got.Urgent {
		t.Fatalf("workspace 8 urgent = true, want false when the field is absent")
	}
	if got := workspaceByID(t, applyAll(t, twoOutputWorkspaces).snapshot(), 5); got.Urgent {
		t.Fatalf("workspace 5 urgent = true, want false")
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
		`"is_focused":true,"is_floating":false,"is_urgent":true,"layout":{},` +
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

func TestKeyboardLayoutEventsReachTheSnapshot(t *testing.T) {
	var s state
	changed, err := s.apply([]byte(`{"KeyboardLayoutsChanged":{"keyboard_layouts":{"names":["English (US)","German"],"current_idx":0}}}`))
	if err != nil || !changed {
		t.Fatalf("layouts changed: %v %v", changed, err)
	}
	if got := s.last.Layouts; len(got.Names) != 2 || got.Current != 0 {
		t.Fatalf("layouts %+v", got)
	}
	changed, err = s.apply([]byte(`{"KeyboardLayoutSwitched":{"idx":1}}`))
	if err != nil || !changed || s.last.Layouts.Current != 1 {
		t.Fatalf("switch: %v %v %+v", changed, err, s.last.Layouts)
	}
	if changed, _ := s.apply([]byte(`{"KeyboardLayoutSwitched":{"idx":1}}`)); changed {
		t.Fatal("a switch to the current layout published")
	}
	if _, err := s.apply([]byte(`{"KeyboardLayoutSwitched":{}}`)); err == nil {
		t.Fatal("a switch without idx was accepted")
	}
}

func TestOverviewEventsUpdatePersistentSnapshot(t *testing.T) {
	t.Parallel()
	var s state
	for _, tc := range []struct {
		line string
		want bool
	}{
		{`{"OverviewOpenedOrClosed":{"is_open":true}}`, true},
		{`{"OverviewOpenedOrClosed":{"is_open":false}}`, false},
	} {
		changed, err := s.apply([]byte(tc.line))
		if err != nil || !changed {
			t.Fatalf("apply %s = changed %v, err %v; want true, nil", tc.line, changed, err)
		}
		if got := s.snapshot().OverviewOpen; got != tc.want {
			t.Fatalf("OverviewOpen = %v after %s, want %v", got, tc.line, tc.want)
		}
		changed, err = s.apply([]byte(tc.line))
		if err != nil || changed {
			t.Fatalf("repeated apply %s = changed %v, err %v; want false, nil", tc.line, changed, err)
		}
	}
}

func TestMalformedOverviewEventDoesNotChangeState(t *testing.T) {
	t.Parallel()
	s := applyAll(t, `{"OverviewOpenedOrClosed":{"is_open":true}}`)
	if _, err := s.apply([]byte(`{"OverviewOpenedOrClosed":{}}`)); err == nil {
		t.Fatal("overview event without is_open was accepted")
	}
	if !s.snapshot().OverviewOpen {
		t.Fatal("malformed overview event changed the open state")
	}
}

func TestWindowOpenedOrChangedProjectsFloatingAndLayout(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture, windowOpenedFloatingFixture)

	var got *Window
	for i := range s.windows {
		if s.windows[i].ID == 82 {
			got = &s.windows[i]
		}
	}
	if got == nil {
		t.Fatal("window 82 missing after open")
	}
	if !got.Floating {
		t.Fatal("is_floating true did not project")
	}
	if !got.HasLayout || !got.TileInView {
		t.Fatalf("layout missing: %+v", *got)
	}
	if got.TileW != 1200.5 || got.TileH != 800.25 || got.TileX != 40.5 || got.TileY != 60.25 {
		t.Fatalf("tile rect = (%v,%v %vx%v), want (40.5,60.25 1200.5x800.25)", got.TileX, got.TileY, got.TileW, got.TileH)
	}
}

func TestWindowLayoutsChangedUpdatesKnownWindowsOnly(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture)
	publish, err := s.apply([]byte(windowLayoutsFixture))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !publish {
		t.Fatal("a tile layout change published nothing")
	}
	if len(s.windows) != 2 {
		t.Fatalf("windows = %d, want 2; an unknown id must not insert", len(s.windows))
	}
	for _, w := range s.windows {
		if w.ID != 80 {
			continue
		}
		if !w.HasLayout || !w.TileInView {
			t.Fatalf("window 80 layout missing: %+v", w)
		}
		if w.TileW != 1200.5 || w.TileH != 800.25 || w.TileX != 40.5 || w.TileY != 60.25 {
			t.Fatalf("window 80 tile rect = (%v,%v %vx%v)", w.TileX, w.TileY, w.TileW, w.TileH)
		}
	}
}

func TestWindowLayoutsChangedForUnknownIDsPublishesNothing(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture)
	publish, err := s.apply([]byte(`{"WindowLayoutsChanged":{"changes":[[9999,{"tile_size":[1,1]}]]}}`))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if publish {
		t.Fatal("a layout change for an unknown window published")
	}
}

func TestWindowLayoutsChangedNullClearsTheTileRectangle(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture, windowLayoutsFixture, windowLayoutCleared)
	for _, w := range s.windows {
		if w.ID != 80 {
			continue
		}
		if w.HasLayout || w.TileInView || w.TileX != 0 || w.TileY != 0 || w.TileW != 0 || w.TileH != 0 {
			t.Fatalf("null layout left the rectangle behind: %+v", w)
		}
	}
}

func TestMalformedWindowLayoutChangeIsAStreamError(t *testing.T) {
	t.Parallel()
	s := applyAll(t, windowsChangedFixture)
	before := len(s.snapshot().Windows)
	if _, err := s.apply([]byte(windowLayoutMalformed)); err == nil {
		t.Fatal("a one-entry change decoded without error")
	}
	if after := len(s.snapshot().Windows); after != before {
		t.Fatalf("windows changed after a decode error: %d -> %d", before, after)
	}
}

const castsChangedFixture = `{"CastsChanged":{"casts":[` +
	`{"stream_id":9,"session_id":4,"kind":"PipeWire","target":{"Output":{"name":"DP-1"}},` +
	`"is_dynamic_target":false,"is_active":true,"pid":null,"pw_node_id":12},` +
	`{"stream_id":11,"session_id":5,"kind":"WlrScreencopy","target":{"Window":{"id":80}},` +
	`"is_dynamic_target":false,"is_active":false,"pid":1001,"pw_node_id":null}]}}`

func TestCastsChangedReplacesWholeSetAndMarksKnown(t *testing.T) {
	t.Parallel()
	s := applyAll(t, castsChangedFixture)

	snap := s.snapshot()
	if !snap.CastsKnown || len(snap.Casts) != 2 {
		t.Fatalf("casts known=%v set=%+v, want two known casts", snap.CastsKnown, snap.Casts)
	}
	want := []Cast{
		{StreamID: 9, Active: true, Target: CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true, PWNodeID: 12, HasPWNodeID: true},
		{StreamID: 11, Target: CastTarget{Kind: "window", WindowID: 80}, TargetSet: true, TargetKnown: true},
	}
	for i := range want {
		if snap.Casts[i] != want[i] {
			t.Fatalf("casts = %+v, want %+v", snap.Casts, want)
		}
	}

	publish, err := s.apply([]byte(`{"CastsChanged":{"casts":[]}}`))
	if err != nil {
		t.Fatalf("apply empty replacement: %v", err)
	}
	if !publish || !s.snapshot().CastsKnown || len(s.snapshot().Casts) != 0 {
		t.Fatalf("empty replacement publish=%v snapshot=%+v, want known inactive", publish, s.snapshot().Casts)
	}
}

func TestCastStartedOrChangedUpsertsByStreamID(t *testing.T) {
	t.Parallel()
	s := applyAll(t, castsChangedFixture)
	lines := []string{
		`{"CastStartedOrChanged":{"cast":{"stream_id":9,"target":{"Window":{"id":81}},"is_active":false}}}`,
		`{"CastStartedOrChanged":{"cast":{"stream_id":12,"target":{"Nothing":{}},"is_active":true}}}`,
	}
	for _, line := range lines {
		if _, err := s.apply([]byte(line)); err != nil {
			t.Fatalf("apply %s: %v", line, err)
		}
	}

	snap := s.snapshot()
	if !snap.CastsKnown || len(snap.Casts) != 3 {
		t.Fatalf("snapshot = %+v, want three known casts", snap.Casts)
	}
	if got := snap.Casts[0]; got != (Cast{StreamID: 9, Target: CastTarget{Kind: "window", WindowID: 81}, TargetSet: true, TargetKnown: true}) {
		t.Errorf("updated cast = %+v", got)
	}
	if got := snap.Casts[2]; got != (Cast{StreamID: 12, Active: true, Target: CastTarget{Kind: "nothing"}, TargetKnown: true}) {
		t.Errorf("new cast = %+v", got)
	}
}

func TestCastStoppedRemovesOnlyTheNamedStream(t *testing.T) {
	t.Parallel()
	s := applyAll(t, castsChangedFixture)
	if _, err := s.apply([]byte(`{"CastStopped":{"stream_id":9}}`)); err != nil {
		t.Fatalf("apply stop: %v", err)
	}
	if casts := s.snapshot().Casts; len(casts) != 1 || casts[0].StreamID != 11 {
		t.Fatalf("casts after stop = %+v, want only stream 11", casts)
	}
	if publish, err := s.apply([]byte(`{"CastStopped":{"stream_id":999}}`)); err != nil || publish {
		t.Fatalf("unknown stop publish=%v err=%v, want no change", publish, err)
	}
}

func TestMalformedCastEventDoesNotChangeState(t *testing.T) {
	t.Parallel()
	s := applyAll(t, castsChangedFixture)
	before := s.snapshot()
	for _, line := range []string{
		`{"CastsChanged":{}}`,
		`{"CastStartedOrChanged":{"cast":{"target":{"Nothing":{}},"is_active":true}}}`,
		`{"CastStopped":{}}`,
	} {
		if _, err := s.apply([]byte(line)); err == nil {
			t.Errorf("apply %s succeeded, want an error", line)
		}
		after := s.snapshot()
		if !after.CastsKnown || len(after.Casts) != len(before.Casts) || after.Casts[0] != before.Casts[0] {
			t.Fatalf("state changed after %s: %+v", line, after.Casts)
		}
	}
}

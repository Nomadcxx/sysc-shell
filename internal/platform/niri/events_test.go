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

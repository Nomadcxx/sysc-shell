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

func TestProjectionEmitsWorkspacePillsPerOutput(t *testing.T) {
	t.Parallel()
	got := projectOutputs(niri.Snapshot{
		Workspaces: []niri.Workspace{
			{ID: 1, Index: 1, Output: "DP-9", Focused: true, Active: true,
				ActiveWindowID: 80, HasActiveWindow: true},
			{ID: 2, Index: 2, Output: "DP-9"},
			{ID: 3, Index: 1, Output: "HDMI-A-9", Active: true},
		},
		Windows: []niri.Window{{ID: 80, Title: "One", WorkspaceID: 1, HasWorkspace: true}},
	})

	dp := got["DP-9"].Pills
	if len(dp) != 2 {
		t.Fatalf("DP-9 pills = %d, want 2", len(dp))
	}
	if dp[0].Index != 1 || !dp[0].Focused || !dp[0].Occupied {
		t.Errorf("DP-9 pill 0 = %+v, want focused and occupied index 1", dp[0])
	}
	if dp[1].Index != 2 || dp[1].Focused || dp[1].Occupied {
		t.Errorf("DP-9 pill 1 = %+v, want an unfocused empty index 2", dp[1])
	}
	if n := len(got["HDMI-A-9"].Pills); n != 1 {
		t.Errorf("HDMI-A-9 pills = %d, want its own 1", n)
	}
}

// Niri sends workspaces in no guaranteed order; the bar must not reshuffle.
func TestWorkspacePillsAreOrderedByIndex(t *testing.T) {
	t.Parallel()
	got := projectOutputs(niri.Snapshot{Workspaces: []niri.Workspace{
		{ID: 3, Index: 3, Output: "DP-9"},
		{ID: 1, Index: 1, Output: "DP-9", Focused: true, Active: true},
		{ID: 2, Index: 2, Output: "DP-9"},
	}})
	pills := got["DP-9"].Pills
	if len(pills) != 3 {
		t.Fatalf("pills = %d, want 3", len(pills))
	}
	for i, p := range pills {
		if p.Index != i+1 {
			t.Fatalf("pill %d has index %d; order is not by index", i, p.Index)
		}
	}
}

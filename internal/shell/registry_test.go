package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

func TestBarShowsOnlyItsOwnOutputsWorkspace(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())

	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{Output: "DP-1", Name: "web", Focused: true, Active: true},
		{Output: "DP-3", Index: 4, Active: true},
	}})

	if got := reg.WorkspaceFor("DP-1"); got != "web" {
		t.Fatalf("DP-1 workspace = %q, want web", got)
	}
	if got := reg.WorkspaceFor("DP-3"); got != "4" {
		t.Fatalf("DP-3 workspace = %q, want the index 4", got)
	}
}

// Niri may name an output whose wl_output has not been announced yet. The state
// must survive until a host appears, and must never create one.
func TestNiriStateForAnUnknownOutputIsHeldNotDropped(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{Output: "DP-9", Name: "later", Active: true},
	}})

	if got := reg.WorkspaceFor("DP-9"); got != "later" {
		t.Fatalf("held workspace = %q, want later", got)
	}
	if len(reg.bars) != 0 {
		t.Fatal("a Niri event created a bar")
	}
}

func TestChangedConnectorsReportsOnlyRealChanges(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	first := niri.Snapshot{Workspaces: []niri.Workspace{
		{Output: "DP-1", Name: "a", Active: true},
	}}

	if changed := reg.UpdateNiri(first); len(changed) != 1 || changed[0] != "DP-1" {
		t.Fatalf("first update changed %v, want [DP-1]", changed)
	}
	// An identical snapshot must not invalidate: no change, no redraw.
	if changed := reg.UpdateNiri(first); len(changed) != 0 {
		t.Fatalf("an identical snapshot reported %v as changed", changed)
	}
}

func TestFocusedWorkspaceOutranksAnActiveOneOnTheSameOutput(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{Output: "DP-1", Name: "active-one", Active: true},
		{Output: "DP-1", Name: "focused-one", Focused: true},
	}})

	if got := reg.WorkspaceFor("DP-1"); got != "focused-one" {
		t.Fatalf("workspace = %q, want the focused one", got)
	}
}

func TestNewHostAppliesTheHeldWorkspaceAndPerOutputPolicy(t *testing.T) {
	t.Parallel()
	cfg, err := config.Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"height":44}}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reg := NewRegistry(cfg)
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{Output: "DP-1", Name: "web", Focused: true},
	}})

	if _, err := reg.NewHost("DP-1"); err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	bar, ok := reg.bars["DP-1"]
	if !ok {
		t.Fatal("NewHost did not record the bar")
	}
	if got := bar.WorkspaceLabel(); got != "Workspace: web" {
		t.Fatalf("label = %q, want the workspace held before the host existed", got)
	}
	if bar.theme.BarHeight != 44 {
		t.Fatalf("bar height = %d, want the DP-1 override 44", bar.theme.BarHeight)
	}
}

func TestDropHostReleasesTheBar(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	if _, err := reg.NewHost("DP-1"); err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	reg.DropHost("DP-1")
	if len(reg.bars) != 0 {
		t.Fatal("DropHost left the bar behind")
	}
}

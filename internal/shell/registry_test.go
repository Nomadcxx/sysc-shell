package shell

import (
	"fmt"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
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

func TestInvalidationOverflowCoalescesToAllBars(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(config.Default())
	workspaces := make([]niri.Workspace, 0, cap(reg.invalidations)+1)
	for i := 0; i < cap(reg.invalidations)+1; i++ {
		workspaces = append(workspaces, niri.Workspace{
			ID: uint64(i + 1), Index: i + 1, Output: fmt.Sprintf("DP-%d", i+1), Active: true,
		})
	}
	reg.UpdateNiri(niri.Snapshot{Workspaces: workspaces})

	global := false
	seen := make(map[string]bool)
	for {
		select {
		case inv := <-reg.Invalidations():
			if inv.Connector == "" {
				global = true
			}
			seen[inv.Connector] = true
		default:
			if !global && len(seen) != len(workspaces) {
				t.Fatalf("overflow retained %d connector invalidations without a global one", len(seen))
			}
			return
		}
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

	if _, err := reg.NewHost(7, "DP-1"); err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	bar, ok := reg.bars[7]
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
	if _, err := reg.NewHost(7, "DP-1"); err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	reg.DropHost(7)
	if len(reg.bars) != 0 {
		t.Fatal("DropHost left the bar behind")
	}
}

func TestReconnectOverlapKeepsBarsDistinctByGlobal(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(config.Default())
	if _, err := reg.NewHost(7, "DP-1"); err != nil {
		t.Fatalf("NewHost(7): %v", err)
	}
	if _, err := reg.NewHost(9, "DP-1"); err != nil {
		t.Fatalf("NewHost(9): %v", err)
	}
	if len(reg.bars) != 2 || reg.bars[7] == reg.bars[9] {
		t.Fatalf("bars = %+v, want distinct globals 7 and 9", reg.bars)
	}

	reg.DropHost(7)
	if len(reg.bars) != 1 || reg.bars[9] == nil {
		t.Fatalf("dropping old global removed reconnecting bar: %+v", reg.bars)
	}
}

func TestPrepareConfigReplacesAllBarsOnCommit(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(config.Default())
	reg.UpdateNiri(niri.Snapshot{Workspaces: []niri.Workspace{
		{Output: "DP-1", Name: "web", Focused: true},
		{Output: "HDMI-A-1", Name: "chat", Active: true},
	}})
	identities := []wayland.HostIdentity{
		{Global: 7, Connector: "DP-1"},
		{Global: 8, Connector: "HDMI-A-1"},
	}
	for _, identity := range identities {
		if _, err := reg.NewHost(identity.Global, identity.Connector); err != nil {
			t.Fatalf("NewHost(%s): %v", identity.Connector, err)
		}
	}
	beforeDP := reg.bars[7]
	beforeHDMI := reg.bars[8]

	candidate := config.Default()
	candidate.Theme.Accent = "#ff8800"
	candidate.Bar.Left = []string{"workspace"}
	candidate.Bar.Center = nil
	prepared, err := reg.PrepareConfig(candidate, identities)
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}

	if reg.bars[7] != beforeDP || reg.bars[8] != beforeHDMI {
		t.Fatal("PrepareConfig changed live bars before commit")
	}
	if len(prepared.Hosts) != 2 {
		t.Fatalf("prepared hosts = %d, want 2", len(prepared.Hosts))
	}

	prepared.Commit()
	if reg.bars[7] == beforeDP || reg.bars[8] == beforeHDMI {
		t.Fatal("commit retained an old bar")
	}
	if got := reg.bars[7].WorkspaceLabel(); got != "Workspace: web" {
		t.Fatalf("DP-1 label = %q, want held workspace", got)
	}
	if got := reg.bars[8].WorkspaceLabel(); got != "Workspace: chat" {
		t.Fatalf("HDMI-A-1 label = %q, want held workspace", got)
	}
	wantAccent := (Color{R: 0xff, G: 0x88, B: 0x00, A: 0xff})
	if got := reg.bars[7].theme.Accent; got != wantAccent {
		t.Fatalf("DP-1 accent = %+v, want %+v", got, wantAccent)
	}
}

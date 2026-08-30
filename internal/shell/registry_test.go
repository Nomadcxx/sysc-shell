package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

// newHosts is the common setup: one registry with hosts at the given globals.
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

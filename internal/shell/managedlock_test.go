package shell

import (
	"testing"
	"time"

	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
)

// A snapshot that changes nothing must not rebuild or republish the panels.
// The managed lock client already suppresses unchanged states upstream; this
// is the second gate, because a panel rebuild costs a render and a compositor
// round trip (#140).
func TestApplyManagedSnapshotIgnoresIdenticalState(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	if got := countSurfaceInvalidations(reg, reducedPanelCap+50*time.Millisecond); got == 0 {
		t.Fatal("settings panel produced no invalidations; it never appeared")
	}

	idle := locksession.State{
		Snapshot: locksession.Snapshot{Sequence: 1, Generation: 1, Phase: "idle"},
		Known:    true,
		Owner:    ":1.9",
	}
	reg.applyManagedSnapshot(idle)
	if got := countSurfaceInvalidations(reg, 100*time.Millisecond); got == 0 {
		t.Fatal("a new snapshot did not rebuild or republish the settings panel")
	}

	reg.applyManagedSnapshot(idle)
	if got := countSurfaceInvalidations(reg, 100*time.Millisecond); got != 0 {
		t.Fatalf("an identical snapshot republished the panel %d times", got)
	}

	sealed := idle
	sealed.Phase = "sealed"
	reg.applyManagedSnapshot(sealed)
	if got := countSurfaceInvalidations(reg, 100*time.Millisecond); got == 0 {
		t.Fatal("a changed phase did not rebuild the panel")
	}
}

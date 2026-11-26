package shell

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
)

// A snapshot that changes nothing must not rebuild or republish the panels.
// The managed lock client already suppresses unchanged states upstream; this
// is the second gate, because a panel rebuild costs a render and a compositor
// round trip (#140).
func TestApplyManagedSnapshotIgnoresIdenticalState(t *testing.T) {
	reg := newPanelRegistryWithoutBridge(t)
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

// sealedBackground attaches service to a registry whose lock session is
// sealed and runs its coordination worker, counting passes.
func sealedBackground(t *testing.T, service *fakeWallsService) *atomic.Int64 {
	t.Helper()
	r := newPanelRegistry(t)
	r.backgroundRequests = make(chan struct{}, 1)
	r.backgroundLeasePath = filepath.Join(t.TempDir(), "sysc-shell", "lock-background.json")
	r.managedState = locksession.State{Known: true, Snapshot: locksession.Snapshot{
		Phase: "sealed", Session: "14", Compositor: "/run/user/1000/niri.sock:1:2", Generation: 1,
	}}
	passes := new(atomic.Int64)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case <-r.backgroundRequests:
				r.coordinateBackground()
				passes.Add(1)
			}
		}
	}()
	service.stopAnswers = true
	r.attachWallsService(service)
	waitFor(t, func() bool { return passes.Load() > 0 })
	time.Sleep(200 * time.Millisecond)
	return passes
}

// While sealed, every Screensaver snapshot queues a coordination pass. The
// service answers a stop for a preview that is not running with a fresh
// snapshot, so coordination that asks for that stop queues its own next pass
// and the shell spins for as long as the session stays locked (sysc-2418).
func TestSealedBackgroundSettlesWithoutAPreview(t *testing.T) {
	service := newFakeWallsService(readyWallsSnapshot())
	passes := sealedBackground(t, service)
	_, _, stops, _ := service.counts()
	if got := passes.Load(); got > 3 {
		t.Fatalf("sealed coordination ran %d passes and stopped an idle preview %d times; want it to settle", got, stops)
	}
}

// A preview still running when the session seals is stopped, once.
func TestSealStopsARunningPreview(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.Previewing, snapshot.PreviewReady = true, true
	service := newFakeWallsService(snapshot)
	passes := sealedBackground(t, service)
	if _, _, stops, _ := service.counts(); stops != 1 || service.Snapshot().Previewing {
		t.Fatalf("seal stopped the running preview %d times over %d passes, previewing=%t; want once", stops, passes.Load(), service.Snapshot().Previewing)
	}
}

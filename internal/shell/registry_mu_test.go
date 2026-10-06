package shell

import (
	"errors"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// fillInvalidations leaves the cap-8 invalidation channel full and undrained,
// which is what an owner whose bridge has stopped looks like to a caller
// holding Registry.mu. The next publishSurface then parks (issue #119).
func fillInvalidations(r *Registry) {
	for i := 0; i < cap(r.invalidations); i++ {
		r.invalidations <- wayland.Invalidation{}
	}
}

// scheduleControl runs its work off-owner and then re-takes Registry.mu to
// apply the result and publish. Publishing is a blocking send, so the lock has
// to be free before it: otherwise one stalled invalidation freezes every
// goroutine of the Registry behind this one mutex.
func TestScheduleControlLeavesRegistryMuFreeWhileItsPublishBlocks(t *testing.T) {
	r := newPanelRegistry(t)
	withTestBar(t, r, 7, r.cfg)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()

	drainInvalidations(r)
	fillInvalidations(r)

	ran := make(chan struct{})
	r.scheduleControl(h, func() error { close(ran); return errors.New("boom") })
	<-ran
	// The goroutine has finished its work and is at the publish by now; the
	// publish cannot complete, so whatever it holds, it holds from here on.
	time.Sleep(50 * time.Millisecond)
	if got := len(r.invalidations); got != cap(r.invalidations) {
		t.Fatalf("invalidations = %d, want the cap %d: the publish drained, so this proved nothing", got, cap(r.invalidations))
	}

	acquired := make(chan struct{})
	go func() {
		r.mu.Lock()
		close(acquired)
		r.mu.Unlock()
	}()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("Registry.mu stayed locked while a blocked surface publish held it")
	}
}

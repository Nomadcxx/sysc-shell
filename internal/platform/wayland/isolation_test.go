package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
)

// A host-scoped failure must destroy one host and leave the others running.
// That is the error boundary the design draws: only a failure that invalidates
// the shared connection may terminate the process.
func TestHostScopedTeardownLeavesOtherHostsMapped(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	bad := mappedHost(s, 1, "DP-1")
	good := mappedHost(s, 2, "DP-3")

	o := &owner{hosts: s}
	if err := o.teardownHost(bad); err != nil {
		t.Fatalf("teardownHost: %v", err)
	}
	if bad.alive {
		t.Fatal("the torn-down host is still alive")
	}
	if !good.alive || good.state != hostMapped {
		t.Fatalf("neighbour = alive %v state %d, want alive and mapped",
			good.alive, good.state)
	}
	if _, ok := s.get(2); !ok {
		t.Fatal("tearing one host down removed its neighbour from the set")
	}
}

// A generation whose buffers will never be released must still free its storage
// when the host goes away, or an unplug would leak the mapping.
func TestTeardownFreesStorageWithAnOutstandingBuffer(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 1, "DP-1")

	// A real generation needs a live wl_shm. The contract under test is the
	// retirement bookkeeping, so the generation is built directly with no
	// pool, mapping or buffers, and fd -1 so teardown closes nothing.
	gen := &generation{id: 1, fd: -1, width: 64, height: 44}
	gen.retire.attached() // outstanding; its release will never arrive
	h.bar.current = gen

	o := &owner{hosts: s}
	if err := o.teardownHost(h); err != nil {
		t.Fatalf("teardownHost with an outstanding buffer: %v", err)
	}
	if h.bar.current != nil || len(h.bar.retiring) != 0 {
		t.Fatal("generations survived teardown")
	}
	if !gen.retire.freeable() {
		t.Fatal("the generation was not marked freeable, so its storage leaked")
	}
}

// Teardown must stop the scheduler, or a dead host could still offer work.
func TestTeardownStopsTheScheduler(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 1, "DP-1")
	h.bar.sched.Invalidate()

	o := &owner{hosts: s}
	if err := o.teardownHost(h); err != nil {
		t.Fatalf("teardownHost: %v", err)
	}
	if d, _ := h.bar.sched.Next(); d == render.DecisionRender {
		t.Fatal("a torn-down host still offers render work")
	}
}

// Removing a global must not disturb the arrival order of the survivors, which
// is what keeps render and shutdown order deterministic.
func TestRemovingAHostPreservesTheOrderOfTheRest(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	mappedHost(s, 1, "DP-1")
	mappedHost(s, 2, "DP-2")
	mappedHost(s, 3, "DP-3")

	s.remove(2)

	var got []string
	for _, h := range s.each() {
		got = append(got, h.connector)
	}
	if len(got) != 2 || got[0] != "DP-1" || got[1] != "DP-3" {
		t.Fatalf("order = %v, want [DP-1 DP-3]", got)
	}
}

package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
)

// mappedHost builds a host that has already allocated a generation and drained
// the dirty flag Configure sets, so a test observes only what it triggers.
func mappedHost(s *hostSet, global uint32, connector string) *OutputHost {
	h, _ := s.add(global, nil)
	h.connector = connector
	h.doneSeen = true
	h.state = hostMapped
	h.sched.Configure(10, 10)
	_ = h.sched.Submitted(0)
	_ = h.sched.Frame()
	_ = h.sched.Release(0)
	return h
}

func TestInvalidationRoutesToOneGlobal(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	a := mappedHost(s, 1, "DP-1")
	b := mappedHost(s, 2, "DP-3")

	o := &owner{hosts: s}
	o.invalidate(Invalidation{Global: 2})

	if d, _ := a.sched.Next(); d == render.DecisionRender {
		t.Fatal("global 1 was invalidated by an event addressed to global 2")
	}
	if d, _ := b.sched.Next(); d != render.DecisionRender {
		t.Fatal("global 2 was not invalidated")
	}
}

// Reconnect overlap puts two bars on one connector. Each must stay separately
// addressable, which connector-keyed routing could not express.
func TestTwoGlobalsSharingAConnectorAreAddressedSeparately(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	old := mappedHost(s, 1, "DP-1")
	fresh := mappedHost(s, 2, "DP-1")

	o := &owner{hosts: s}
	o.invalidate(Invalidation{Global: 2})

	if d, _ := old.sched.Next(); d == render.DecisionRender {
		t.Fatal("the outgoing bar was invalidated by its replacement's global")
	}
	if d, _ := fresh.sched.Next(); d != render.DecisionRender {
		t.Fatal("the replacement bar was not invalidated")
	}
}

func TestAZeroGlobalInvalidatesEveryBar(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	a := mappedHost(s, 1, "DP-1")
	b := mappedHost(s, 2, "DP-3")

	o := &owner{hosts: s}
	o.invalidate(Invalidation{})

	for name, h := range map[string]*OutputHost{"DP-1": a, "DP-3": b} {
		if d, _ := h.sched.Next(); d != render.DecisionRender {
			t.Fatalf("%s was not invalidated by a broadcast", name)
		}
	}
}

func TestInvalidationSkipsDeadHosts(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 1, "DP-1")
	h.alive = false

	o := &owner{hosts: s}
	o.invalidate(Invalidation{})

	if d, _ := h.sched.Next(); d == render.DecisionRender {
		t.Fatal("a dead host was invalidated")
	}
}

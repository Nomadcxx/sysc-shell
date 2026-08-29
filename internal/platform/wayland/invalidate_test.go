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

func TestInvalidationRoutesToOneConnector(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	a := mappedHost(s, 1, "DP-1")
	b := mappedHost(s, 2, "DP-3")

	o := &owner{hosts: s}
	o.invalidate(Invalidation{Connector: "DP-3"})

	if d, _ := a.sched.Next(); d == render.DecisionRender {
		t.Fatal("DP-1 was invalidated by an event addressed to DP-3")
	}
	if d, _ := b.sched.Next(); d != render.DecisionRender {
		t.Fatal("DP-3 was not invalidated")
	}
}

func TestEmptyConnectorInvalidatesEveryBar(t *testing.T) {
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

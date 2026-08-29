package wayland

import (
	"slices"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// hostSet holds every output host, keyed by wl_registry global name. Creation
// happens only on wl_registry.global and removal only on global_remove, so a
// connector that disappears and returns cannot produce a duplicate host.
type hostSet struct {
	hosts   map[uint32]*OutputHost
	arrival []uint32
}

func newHostSet() *hostSet {
	return &hostSet{hosts: make(map[uint32]*OutputHost)}
}

// add creates a host for a global, reporting false when one already exists.
func (s *hostSet) add(global uint32, proxy *client.Output) (*OutputHost, bool) {
	if existing, ok := s.hosts[global]; ok {
		return existing, false
	}
	h := newHost(global, proxy)
	s.hosts[global] = h
	s.arrival = append(s.arrival, global)
	return h, true
}

// remove forgets a global and reports the host that owned it.
func (s *hostSet) remove(global uint32) (*OutputHost, bool) {
	h, ok := s.hosts[global]
	if !ok {
		return nil, false
	}
	delete(s.hosts, global)
	s.arrival = slices.DeleteFunc(s.arrival, func(g uint32) bool { return g == global })
	return h, true
}

func (s *hostSet) get(global uint32) (*OutputHost, bool) {
	h, ok := s.hosts[global]
	return h, ok
}

// byConnector finds a host by its wl_output.name. Used to join Niri workspace
// state, which is keyed by connector, to a host. Never used as identity.
func (s *hostSet) byConnector(name string) (*OutputHost, bool) {
	if name == "" {
		return nil, false
	}
	for _, global := range s.arrival {
		if h := s.hosts[global]; h.connector == name {
			return h, true
		}
	}
	return nil, false
}

// each returns hosts in arrival order, which keeps render and shutdown order
// deterministic across runs.
func (s *hostSet) each() []*OutputHost {
	out := make([]*OutputHost, 0, len(s.arrival))
	for _, global := range s.arrival {
		out = append(out, s.hosts[global])
	}
	return out
}

func (s *hostSet) len() int { return len(s.hosts) }

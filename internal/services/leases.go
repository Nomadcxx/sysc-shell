package services

import (
	"slices"
	"time"
)

// leaseSet is the lease bookkeeping shared by Clock and Metrics: which
// consumers are live, and the finest interval they collectively require.
//
// It is a struct rather than an interface. Two services share this code; they
// do not share a contract, and nothing dispatches over them.
//
// leaseSet performs no locking. Its owner holds a mutex across every call.
type leaseSet struct {
	leases []*Lease
}

// add registers a lease and reports the finest interval before and after, so a
// caller can tell whether a running timer needs re-arming without recomputing.
func (s *leaseSet) add(l *Lease) (previous, current time.Duration) {
	previous = s.finest()
	s.leases = append(s.leases, l)
	return previous, s.finest()
}

// remove drops a lease and reports whether it was present.
func (s *leaseSet) remove(l *Lease) bool {
	i := slices.Index(s.leases, l)
	if i < 0 {
		return false
	}
	s.leases = slices.Delete(s.leases, i, i+1)
	return true
}

// finest is the shortest interval any live lease requires, or zero when there
// are none.
func (s *leaseSet) finest() time.Duration {
	out := time.Duration(0)
	for _, l := range s.leases {
		if out == 0 || l.boundary < out {
			out = l.boundary
		}
	}
	return out
}

func (s *leaseSet) len() int { return len(s.leases) }

// clear empties the set and returns what it held, for a caller releasing all.
func (s *leaseSet) clear() []*Lease {
	held := s.leases
	s.leases = nil
	return held
}

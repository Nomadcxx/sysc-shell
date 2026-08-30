package services

import (
	"testing"
	"time"
)

func TestLeaseSetReportsTheFinestInterval(t *testing.T) {
	t.Parallel()
	var s leaseSet

	minute := &Lease{boundary: time.Minute}
	second := &Lease{boundary: time.Second}

	if got := s.finest(); got != 0 {
		t.Fatalf("empty set finest = %v, want 0", got)
	}
	s.add(minute)
	if got := s.finest(); got != time.Minute {
		t.Fatalf("finest = %v, want one minute", got)
	}
	s.add(second)
	if got := s.finest(); got != time.Second {
		t.Fatalf("finest = %v, want the shorter second", got)
	}
	s.remove(second)
	if got := s.finest(); got != time.Minute {
		t.Fatalf("finest after release = %v, want one minute again", got)
	}
}

// add reports the interval before and after, so a caller can tell whether the
// running timer needs re-arming without recomputing.
func TestLeaseSetAddReportsTheTransition(t *testing.T) {
	t.Parallel()
	var s leaseSet

	previous, current := s.add(&Lease{boundary: time.Minute})
	if previous != 0 || current != time.Minute {
		t.Fatalf("first add = (%v, %v), want (0, 1m)", previous, current)
	}
	previous, current = s.add(&Lease{boundary: time.Second})
	if previous != time.Minute || current != time.Second {
		t.Fatalf("shortening add = (%v, %v), want (1m, 1s)", previous, current)
	}
}

func TestLeaseSetRemoveIsIdempotent(t *testing.T) {
	t.Parallel()
	var s leaseSet
	lease := &Lease{boundary: time.Second}
	s.add(lease)

	if !s.remove(lease) {
		t.Fatal("first remove reported the lease absent")
	}
	if s.remove(lease) {
		t.Fatal("second remove reported the lease present")
	}
	if s.len() != 0 {
		t.Fatalf("len = %d, want 0", s.len())
	}
}

func TestLeaseSetClearReturnsEverything(t *testing.T) {
	t.Parallel()
	var s leaseSet
	s.add(&Lease{boundary: time.Second})
	s.add(&Lease{boundary: time.Minute})

	released := s.clear()
	if len(released) != 2 {
		t.Fatalf("clear returned %d leases, want 2", len(released))
	}
	if s.len() != 0 {
		t.Fatal("clear left leases behind")
	}
}

package services

import (
	"testing"
	"time"
)

func TestTheFirstLeaseStartsAndTheLastStops(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	if c.Running() {
		t.Fatal("a clock with no lease is running")
	}

	first, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !c.Running() {
		t.Fatal("the first lease did not start the clock")
	}

	second, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := c.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; a second consumer must share the running clock", got)
	}

	first.Release()
	if !c.Running() {
		t.Fatal("releasing one of two leases stopped the clock")
	}

	second.Release()
	if c.Running() {
		t.Fatal("releasing the last lease left the clock running")
	}
}

func TestReleaseIsIdempotentAndNilSafe(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	lease, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lease.Release()
	lease.Release()

	var absent *Lease
	absent.Release()

	if c.Running() {
		t.Fatal("a double release left the clock running")
	}
}

// A reload acquires the replacement set's leases before releasing the outgoing
// set's, so a service in continuous use must never restart.
func TestAcquireBeforeReleaseDoesNotRestartTheClock(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	outgoing, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	incoming, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	outgoing.Release()

	if !c.Running() {
		t.Fatal("the clock stopped during an overlapping handover")
	}
	if got := c.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; the clock restarted during a reload", got)
	}
	incoming.Release()
}

// A boundary change must re-arm the running timer, not cycle the goroutine.
func TestAShorterBoundaryDoesNotRestartTheClock(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	minute, err := c.Acquire(time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	second, err := c.Acquire(time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if got := c.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1 after a boundary change", got)
	}
	second.Release()
	minute.Release()
}

func TestANonPositiveBoundaryIsRejected(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	if _, err := c.Acquire(0); err == nil {
		t.Fatal("a zero boundary was accepted")
	}
	if c.Running() {
		t.Fatal("a rejected acquire started the clock")
	}
}

func TestCloseStopsTheGoroutine(t *testing.T) {
	t.Parallel()
	c := NewClock()
	if _, err := c.Acquire(time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	c.Close()

	if c.Running() {
		t.Fatal("Close left the clock running")
	}
	// Close must be safe to call twice; shutdown paths may both reach it.
	c.Close()
}

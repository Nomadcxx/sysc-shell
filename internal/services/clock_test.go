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

func TestNextBoundaryAligns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		now      time.Time
		boundary time.Duration
		want     time.Time
	}{
		{
			name:     "mid minute rounds up to the next minute",
			now:      time.Date(2026, 8, 30, 15, 4, 37, 500_000_000, time.UTC),
			boundary: time.Minute,
			want:     time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC),
		},
		{
			// Exactly on a boundary must advance, never return now, or the
			// goroutine would spin on a zero-length timer.
			name:     "exactly on a boundary advances",
			now:      time.Date(2026, 8, 30, 15, 4, 0, 0, time.UTC),
			boundary: time.Minute,
			want:     time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC),
		},
		{
			name:     "sub second rounds up to the next second",
			now:      time.Date(2026, 8, 30, 15, 4, 37, 250_000_000, time.UTC),
			boundary: time.Second,
			want:     time.Date(2026, 8, 30, 15, 4, 38, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nextBoundary(tc.now, tc.boundary); !got.Equal(tc.want) {
				t.Fatalf("nextBoundary(%v, %v) = %v, want %v", tc.now, tc.boundary, got, tc.want)
			}
		})
	}
}

// One tick reaches the consumer, aligned to the second boundary.
func TestASecondBoundaryTickIsDelivered(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	lease, err := c.Acquire(time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	select {
	case now := <-c.Updates():
		// The publish happens just after the boundary, so the sub-second
		// remainder is small. A generous bound keeps this stable under load
		// while still failing an unaligned ticker.
		if off := now.Sub(now.Truncate(time.Second)); off > 500*time.Millisecond {
			t.Fatalf("tick landed %v past the second boundary, want it aligned", off)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no tick arrived within three seconds")
	}
}

// The channel holds only the newest time, so a slow consumer coalesces rather
// than queueing stale values.
func TestUpdatesKeepOnlyTheNewestTime(t *testing.T) {
	t.Parallel()
	c := NewClock()
	t.Cleanup(c.Close)

	older := time.Date(2026, 8, 30, 15, 4, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC)
	send(c.updates, older)
	send(c.updates, newer)

	got := <-c.Updates()
	if !got.Equal(newer) {
		t.Fatalf("received %v, want the newest %v", got, newer)
	}
	select {
	case extra := <-c.Updates():
		t.Fatalf("a second value %v was queued behind the newest", extra)
	default:
	}
}

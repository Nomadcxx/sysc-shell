package services

import (
	"testing"
	"time"
)

func TestTheFirstMetricLeaseStartsAndTheLastStops(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	if m.Running() {
		t.Fatal("a service with no lease is running")
	}

	cpu, err := m.Acquire(SourceCPU, 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !m.Running() {
		t.Fatal("the first lease did not start the service")
	}

	memory, err := m.Acquire(SourceMemory, 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := m.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; a second consumer must share the goroutine", got)
	}

	cpu.Release()
	if !m.Running() {
		t.Fatal("releasing one of two leases stopped the service")
	}
	memory.Release()
	if m.Running() {
		t.Fatal("releasing the last lease left the service running")
	}
}

// Only leased sources may be sampled. This is what makes per-source leasing
// worth its complexity: a CPU-only bar never reads /proc/diskstats.
func TestOnlyLeasedSourcesAreReported(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	lease, err := m.Acquire(SourceCPU, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	if !m.Leased(SourceCPU) {
		t.Fatal("the leased source reports unleased")
	}
	for _, src := range []Source{SourceMemory, SourceFilesystem, SourceBlock, SourceNetwork} {
		if m.Leased(src) {
			t.Fatalf("source %v reports leased with no consumer", src)
		}
	}
}

// A reload acquires the replacement set's leases before releasing the outgoing
// set's, so a service in continuous use must never restart.
func TestAcquireBeforeReleaseDoesNotRestartTheMetricsService(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	outgoing, err := m.Acquire(SourceCPU, 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	incoming, err := m.Acquire(SourceCPU, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	outgoing.Release()

	if !m.Running() {
		t.Fatal("the service stopped during an overlapping handover")
	}
	if got := m.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; the service restarted across a reload", got)
	}
	incoming.Release()
}

func TestANonPositiveMetricIntervalIsRejected(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	if _, err := m.Acquire(SourceCPU, 0); err == nil {
		t.Fatal("a zero interval was accepted")
	}
	if m.Running() {
		t.Fatal("a rejected acquire started the service")
	}
}

func TestAnUnknownSourceIsRejected(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	if _, err := m.Acquire(Source(99), time.Second); err == nil {
		t.Fatal("an unknown source was accepted")
	}
}

func TestClosingTheMetricsServiceStopsTheGoroutine(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	if _, err := m.Acquire(SourceCPU, time.Second); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	m.Close()

	if m.Running() {
		t.Fatal("Close left the service running")
	}
	// Close must be safe to call twice; shutdown paths may each reach it.
	m.Close()
}

// A leased source must appear in the snapshot; an unleased one must not, so an
// unused collector is never called.
func TestOnlyLeasedSourcesArePopulated(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	lease, err := m.Acquire(SourceMemory, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	select {
	case snap := <-m.Updates():
		if snap.Memory == nil {
			t.Fatal("the leased memory source was not populated")
		}
		if snap.CPU != nil || snap.Block != nil || snap.Network != nil || snap.Filesystem != nil {
			t.Fatalf("an unleased source was collected: %+v", snap)
		}
		if snap.CollectedAt.IsZero() {
			t.Fatal("snapshot carries no collection time")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no snapshot arrived within three seconds")
	}
}

// The channel holds only the newest snapshot, so a slow consumer coalesces
// rather than queueing stale values.
func TestMetricUpdatesKeepOnlyTheNewest(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	older := Snapshot{CollectedAt: time.Date(2026, 8, 30, 15, 4, 0, 0, time.UTC)}
	newer := Snapshot{CollectedAt: time.Date(2026, 8, 30, 15, 5, 0, 0, time.UTC)}
	sendSnapshot(m.updates, older)
	sendSnapshot(m.updates, newer)

	got := <-m.Updates()
	if !got.CollectedAt.Equal(newer.CollectedAt) {
		t.Fatalf("received %v, want the newest %v", got.CollectedAt, newer.CollectedAt)
	}
	select {
	case extra := <-m.Updates():
		t.Fatalf("a second value %v was queued behind the newest", extra.CollectedAt)
	default:
	}
}

func TestARingKeepsTheNewestSamplesOldestFirst(t *testing.T) {
	t.Parallel()
	r := newRing(3)

	for _, v := range []float64{0.1, 0.2, 0.3, 0.4} {
		r.push(v)
	}
	got := r.values()
	want := []float64{0.2, 0.3, 0.4}
	if len(got) != len(want) {
		t.Fatalf("ring holds %d values, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ring = %v, want %v (oldest first, newest last)", got, want)
		}
	}
}

func TestAPartialRingReportsOnlyWhatItHolds(t *testing.T) {
	t.Parallel()
	r := newRing(5)
	r.push(0.5)

	if got := r.values(); len(got) != 1 || got[0] != 0.5 {
		t.Fatalf("partial ring = %v, want one sample", got)
	}
}

// A returned history must not alias the ring, or a later sample would mutate
// a slice a widget is already plotting.
func TestHistoryDoesNotAliasTheRing(t *testing.T) {
	t.Parallel()
	r := newRing(3)
	r.push(0.1)

	got := r.values()
	got[0] = 99
	if again := r.values(); again[0] != 0.1 {
		t.Fatal("mutating a returned history changed the ring")
	}
}

func TestHistoryRecordsOnlyLeasedSources(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	m.record(Snapshot{CollectedAt: time.Now()})
	if got := m.History(SourceCPU); len(got) != 0 {
		t.Fatalf("an empty snapshot recorded %v", got)
	}
}

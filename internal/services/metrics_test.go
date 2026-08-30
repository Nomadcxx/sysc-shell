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

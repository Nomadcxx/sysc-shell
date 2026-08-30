package services

import (
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
)

func TestTheFirstMetricLeaseStartsAndTheLastStops(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	if m.Running() {
		t.Fatal("a service with no lease is running")
	}

	cpu, err := m.Acquire(Selector{Source: SourceCPU}, 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !m.Running() {
		t.Fatal("the first lease did not start the service")
	}

	memory, err := m.Acquire(Selector{Source: SourceMemory}, 2*time.Second)
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

	lease, err := m.Acquire(Selector{Source: SourceCPU}, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	if !m.SourceLeased(SourceCPU) {
		t.Fatal("the leased source reports unleased")
	}
	for _, src := range []Source{SourceMemory, SourceFilesystem, SourceBlock, SourceNetwork} {
		if m.SourceLeased(src) {
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

	outgoing, err := m.Acquire(Selector{Source: SourceCPU}, 2*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	incoming, err := m.Acquire(Selector{Source: SourceCPU}, time.Second)
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

	if _, err := m.Acquire(Selector{Source: SourceCPU}, 0); err == nil {
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

	if _, err := m.Acquire(Selector{Source: Source(99)}, time.Second); err == nil {
		t.Fatal("an unknown source was accepted")
	}
}

func TestClosingTheMetricsServiceStopsTheGoroutine(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	if _, err := m.Acquire(Selector{Source: SourceCPU}, time.Second); err != nil {
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

	lease, err := m.Acquire(Selector{Source: SourceMemory}, 50*time.Millisecond)
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
	if got := m.History(Selector{Source: SourceCPU}); len(got) != 0 {
		t.Fatalf("an empty snapshot recorded %v", got)
	}
}

// Two widgets watching different subjects of one source must keep separate
// histories, or a graph of one interface plots the whole machine.
func TestEachSubjectKeepsItsOwnHistory(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	fast := Selector{Source: SourceNetwork, Subject: "eth9", Direction: "rx"}
	slow := Selector{Source: SourceNetwork, Subject: "eth8", Direction: "rx"}
	for _, sel := range []Selector{fast, slow} {
		lease, err := m.Acquire(sel, time.Hour)
		if err != nil {
			t.Fatalf("Acquire(%v): %v", sel, err)
		}
		defer lease.Release()
	}

	m.record(Snapshot{Network: &metrics.NetworkSnapshot{
		Interfaces: []metrics.NetworkInterface{
			{Name: "eth9", Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 900, Valid: true}},
			{Name: "eth8", Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 100, Valid: true}},
		},
	}})

	if got := m.History(fast); len(got) != 1 || got[0] != 900 {
		t.Fatalf("eth9 history = %v, want its own 900", got)
	}
	if got := m.History(slow); len(got) != 1 || got[0] != 100 {
		t.Fatalf("eth8 history = %v, want its own 100", got)
	}
}

// The two directions of one interface are distinct subjects.
func TestEachDirectionKeepsItsOwnHistory(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	rx := Selector{Source: SourceNetwork, Subject: "eth9", Direction: "rx"}
	tx := Selector{Source: SourceNetwork, Subject: "eth9", Direction: "tx"}
	for _, sel := range []Selector{rx, tx} {
		lease, err := m.Acquire(sel, time.Hour)
		if err != nil {
			t.Fatalf("Acquire(%v): %v", sel, err)
		}
		defer lease.Release()
	}

	m.record(Snapshot{Network: &metrics.NetworkSnapshot{
		Interfaces: []metrics.NetworkInterface{{
			Name: "eth9",
			Rates: metrics.NetworkRates{
				ReceiveBytesPerSecond:  10,
				TransmitBytesPerSecond: 20,
				Valid:                  true,
			},
		}},
	}})

	if got := m.History(rx); len(got) != 1 || got[0] != 10 {
		t.Fatalf("rx history = %v, want 10", got)
	}
	if got := m.History(tx); len(got) != 1 || got[0] != 20 {
		t.Fatalf("tx history = %v, want 20", got)
	}
}

// A filesystem graph must plot the mount its widget names, not whichever mount
// the collector happens to report first.
func TestAFilesystemHistoryFollowsItsOwnMount(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	sel := Selector{Source: SourceFilesystem, Subject: "/fixture"}
	lease, err := m.Acquire(sel, time.Hour)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	m.record(Snapshot{Filesystem: &metrics.FilesystemSnapshot{
		Filesystems: []metrics.Filesystem{
			{MountPoint: "/", Capacity: metrics.Capacity{TotalBytes: 100, UsedBytes: 90}},
			{MountPoint: "/fixture", Capacity: metrics.Capacity{TotalBytes: 100, UsedBytes: 10}},
		},
	}})

	if got := m.History(sel); len(got) != 1 || got[0] != 0.1 {
		t.Fatalf("history = %v, want the named mount's 0.1, not the first mount's 0.9", got)
	}
}

// An absent reading is skipped, not recorded as zero: a failed collector is
// not a measurement of nothing, and a zero would draw a trough that never
// happened.
func TestAnAbsentReadingIsNotRecordedAsZero(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	sel := Selector{Source: SourceCPU}
	lease, err := m.Acquire(sel, time.Hour)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	m.record(Snapshot{CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Valid: false}}})
	if got := m.History(sel); len(got) != 0 {
		t.Fatalf("an invalid reading recorded %v, want nothing", got)
	}
}

// The history goes with the last lease. Keeping it would let a widget removed
// at midday and restored in the evening plot both windows as one line.
func TestReleasingTheLastLeaseDiscardsTheHistory(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	sel := Selector{Source: SourceCPU}
	lease, err := m.Acquire(sel, time.Hour)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	m.record(Snapshot{CPU: &metrics.CPUSnapshot{
		Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true},
	}})
	if got := m.History(sel); len(got) != 1 {
		t.Fatalf("history = %v, want one sample before release", got)
	}

	lease.Release()

	again, err := m.Acquire(sel, time.Hour)
	if err != nil {
		t.Fatalf("re-Acquire: %v", err)
	}
	defer again.Release()
	if got := m.History(sel); len(got) != 0 {
		t.Fatalf("a re-acquired selector inherited %v, want a fresh window", got)
	}
}

// A second consumer of the same selector shares the ring rather than resetting
// it, which is the anti-duplication rationale per-subject keying preserves.
func TestASecondConsumerSharesTheHistory(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	sel := Selector{Source: SourceCPU}
	first, err := m.Acquire(sel, time.Hour)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer first.Release()
	m.record(Snapshot{CPU: &metrics.CPUSnapshot{
		Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true},
	}})

	second, err := m.Acquire(sel, time.Hour)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	defer second.Release()

	if got := m.History(sel); len(got) != 1 || got[0] != 0.42 {
		t.Fatalf("history = %v, want the shared 0.42 kept across a second lease", got)
	}
}

func TestABatteryLeaseIsIndependentOfOtherSources(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	lease, err := m.Acquire(Selector{Source: SourceBattery}, 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	if !m.SourceLeased(SourceBattery) {
		t.Fatal("the battery source reports unleased")
	}
	for _, src := range []Source{SourceCPU, SourceMemory, SourceFilesystem, SourceBlock, SourceNetwork} {
		if m.SourceLeased(src) {
			t.Fatalf("source %v reports leased with only a battery consumer", src)
		}
	}
}

// A battery-only configuration must never read CPU or block counters.
func TestOnlyTheBatterySourceIsPopulated(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	t.Cleanup(m.Close)

	lease, err := m.Acquire(Selector{Source: SourceBattery}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	select {
	case snap := <-m.Updates():
		if snap.CPU != nil || snap.Block != nil || snap.Network != nil ||
			snap.Filesystem != nil || snap.Memory != nil {
			t.Fatalf("an unleased source was collected: %+v", snap)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no snapshot arrived within three seconds")
	}
}

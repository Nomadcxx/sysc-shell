package services

import (
	"fmt"
	"os"
	"sync"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
)

// Source names one telemetry source. A lease names the source it needs, so a
// configuration using only a clock and a CPU widget never opens
// /proc/diskstats.
type Source uint8

const (
	SourceCPU Source = iota
	SourceMemory
	SourceFilesystem
	SourceBlock
	SourceNetwork
	sourceCount
)

// String names a source for error messages and tests.
func (s Source) String() string {
	switch s {
	case SourceCPU:
		return "cpu"
	case SourceMemory:
		return "memory"
	case SourceFilesystem:
		return "filesystem"
	case SourceBlock:
		return "block"
	case SourceNetwork:
		return "network"
	}
	return fmt.Sprintf("source(%d)", uint8(s))
}

// Snapshot aggregates one collection pass. This is the shell's own type: the
// library exposes CPUSnapshot, MemorySnapshot and the rest, but no aggregate.
//
// A nil field means that source is unleased, or failed this pass. Either way
// its widgets render the unavailable placeholder; a consumer never has to
// distinguish the two.
type Snapshot struct {
	CollectedAt time.Time
	CPU         *metrics.CPUSnapshot
	Memory      *metrics.MemorySnapshot
	Filesystem  *metrics.FilesystemSnapshot
	Block       *metrics.BlockSnapshot
	Network     *metrics.NetworkSnapshot
}

// Metrics samples the leased telemetry sources on one goroutine.
//
// That goroutine solely owns the three stateful samplers. The library
// documents them as owned by one sequential caller: each retains previous
// counters, so a concurrent Sample would corrupt the rate derivation.
type Metrics struct {
	mu     sync.Mutex
	leases [sourceCount]leaseSet
	stop   chan struct{}
	done   chan struct{}
	starts int

	rearm   chan struct{}
	updates chan Snapshot

	history [sourceCount]*ring
}

func NewMetrics() *Metrics {
	m := &Metrics{
		rearm:   make(chan struct{}, 1),
		updates: make(chan Snapshot, 1),
	}
	for i := range m.history {
		m.history[i] = newRing(historySize)
	}
	return m
}

// Updates carries the newest snapshot. The channel is created once and never
// closed, so it survives stop and start cycles.
func (m *Metrics) Updates() <-chan Snapshot { return m.updates }

// Acquire registers a consumer of one source at the interval it needs.
func (m *Metrics) Acquire(src Source, interval time.Duration) (*Lease, error) {
	if src >= sourceCount {
		return nil, fmt.Errorf("services: unknown metrics source %d", uint8(src))
	}
	if interval <= 0 {
		return nil, fmt.Errorf("services: metrics interval %v is not positive", interval)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	before := m.finestLocked()
	lease := &Lease{metrics: m, source: src, boundary: interval}
	m.leases[src].add(lease)
	after := m.finestLocked()

	switch {
	case m.stop == nil:
		m.startLocked()
	case before != 0 && after < before:
		select {
		case m.rearm <- struct{}{}:
		default:
		}
	}
	return lease, nil
}

// Leased reports whether any consumer needs this source.
func (m *Metrics) Leased(src Source) bool {
	if src >= sourceCount {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leases[src].len() > 0
}

// Close releases every lease and stops the goroutine. It is safe to call twice.
func (m *Metrics) Close() {
	m.mu.Lock()
	for i := range m.leases {
		for _, l := range m.leases[i].clear() {
			l.metrics = nil
		}
	}
	done := m.stopIfUnusedLocked()
	m.mu.Unlock()

	if done != nil {
		<-done
	}
}

// Running reports whether the sampling goroutine is live.
func (m *Metrics) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop != nil
}

// Starts counts goroutine starts. An interval change re-arms rather than
// restarting, so this stays at one across a reload.
func (m *Metrics) Starts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.starts
}

// finestLocked is the shortest interval any live lease requires, across every
// source, or zero when there are none.
func (m *Metrics) finestLocked() time.Duration {
	out := time.Duration(0)
	for i := range m.leases {
		if f := m.leases[i].finest(); f != 0 && (out == 0 || f < out) {
			out = f
		}
	}
	return out
}

func (m *Metrics) startLocked() {
	m.stop, m.done = make(chan struct{}), make(chan struct{})
	m.starts++
	go m.run(m.stop, m.done)
}

func (m *Metrics) stopIfUnusedLocked() chan struct{} {
	for i := range m.leases {
		if m.leases[i].len() > 0 {
			return nil
		}
	}
	if m.stop == nil {
		return nil
	}
	close(m.stop)
	done := m.done
	m.stop, m.done = nil, nil
	return done
}

// releaseMetric drops one lease, stopping the goroutine when it was the last.
func (m *Metrics) releaseMetric(l *Lease) {
	m.mu.Lock()
	if !m.leases[l.source].remove(l) {
		m.mu.Unlock()
		return
	}
	done := m.stopIfUnusedLocked()
	m.mu.Unlock()

	if done != nil {
		<-done
	}
}

// samplers holds the stateful collectors. They are constructed once and used
// only by the sampling goroutine, because the library documents them as owned
// by one sequential caller: each retains previous counters, so a concurrent
// Sample would corrupt its rate derivation.
type samplers struct {
	cpu     *metrics.CPUSampler
	block   *metrics.BlockSampler
	network *metrics.NetworkSampler
}

func (m *Metrics) run(stop, done chan struct{}) {
	defer close(done)

	s := samplers{
		cpu:     metrics.NewCPUSampler(),
		block:   metrics.NewBlockSampler(),
		network: metrics.NewNetworkSampler(),
	}
	// failing tracks which sources are currently reporting errors, so the log
	// is edge-triggered. A per-tick log at the default interval would emit
	// roughly 1,800 lines an hour for one permanently absent device.
	var failing [sourceCount]bool

	for {
		m.mu.Lock()
		interval := m.finestLocked()
		m.mu.Unlock()
		if interval <= 0 {
			return
		}

		timer := time.NewTimer(interval)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-m.rearm:
			// A shorter interval arrived; recompute the deadline.
			timer.Stop()
		case <-timer.C:
			snap := m.collect(&s, &failing)
			m.record(snap)
			sendSnapshot(m.updates, snap)
		}
	}
}

// collect samples every leased source. A source that fails is left nil, which
// renders as unavailable; one failing source never suppresses another.
func (m *Metrics) collect(s *samplers, failing *[sourceCount]bool) Snapshot {
	snap := Snapshot{CollectedAt: time.Now()}

	if m.Leased(SourceCPU) {
		if v, err := s.cpu.Sample(); err != nil {
			noteFailure(failing, SourceCPU, err)
		} else {
			noteRecovery(failing, SourceCPU)
			snap.CPU = &v
		}
	}
	if m.Leased(SourceMemory) {
		if v, err := metrics.ReadMemory(); err != nil {
			noteFailure(failing, SourceMemory, err)
		} else {
			noteRecovery(failing, SourceMemory)
			snap.Memory = &v
		}
	}
	if m.Leased(SourceFilesystem) {
		if v, err := metrics.ReadFilesystems(); err != nil {
			noteFailure(failing, SourceFilesystem, err)
		} else {
			noteRecovery(failing, SourceFilesystem)
			snap.Filesystem = &v
		}
	}
	if m.Leased(SourceBlock) {
		if v, err := s.block.Sample(); err != nil {
			noteFailure(failing, SourceBlock, err)
		} else {
			noteRecovery(failing, SourceBlock)
			snap.Block = &v
		}
	}
	if m.Leased(SourceNetwork) {
		if v, err := s.network.Sample(); err != nil {
			noteFailure(failing, SourceNetwork, err)
		} else {
			noteRecovery(failing, SourceNetwork)
			snap.Network = &v
		}
	}
	return snap
}

func noteFailure(failing *[sourceCount]bool, src Source, err error) {
	if failing[src] {
		return
	}
	failing[src] = true
	fmt.Fprintf(os.Stderr, "sysc-shell: metrics source %s unavailable: %v\n", src, err)
}

func noteRecovery(failing *[sourceCount]bool, src Source) {
	if !failing[src] {
		return
	}
	failing[src] = false
	fmt.Fprintf(os.Stderr, "sysc-shell: metrics source %s recovered\n", src)
}

// sendSnapshot publishes the newest snapshot, replacing one the consumer has
// not read. This goroutine is the only sender, so the retry always finds room.
func sendSnapshot(updates chan Snapshot, snap Snapshot) {
	select {
	case updates <- snap:
		return
	default:
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- snap:
	default:
	}
}

// TEMPORARY: replaced in Task 4.
const historySize = 120

type ring struct{}

func newRing(int) *ring { return &ring{} }

func (m *Metrics) record(Snapshot) {}

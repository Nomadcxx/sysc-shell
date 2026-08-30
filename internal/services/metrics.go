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

// Selector names one metric subject. CPU and memory have exactly one subject
// each; a filesystem, block device or interface has many, and a rate has a
// direction as well.
//
// It is the history ring's key as well as the lease's, so two bars graphing
// the same interface share one ring while a graph of the opposite direction
// keeps its own.
type Selector struct {
	Source Source
	// Subject is the mount point, block device or interface name. Empty on
	// cpu and memory.
	Subject string
	// Direction is "write" or "tx" to select the outbound counter. Empty, or
	// any other value, selects the inbound one; the loader has already
	// restricted it to that source's vocabulary.
	Direction string
}

// String names a selector for error messages and tests.
func (s Selector) String() string {
	switch {
	case s.Subject == "":
		return s.Source.String()
	case s.Direction == "":
		return s.Source.String() + ":" + s.Subject
	}
	return s.Source.String() + ":" + s.Subject + ":" + s.Direction
}

// Fraction reports a value between zero and one for the sources that have a
// full scale. A rate source reports absent: bytes per second has no full
// scale, which is why the loader rejects a meter on one.
func (s Snapshot) Fraction(sel Selector) (float64, bool) {
	switch sel.Source {
	case SourceCPU:
		if s.CPU == nil || !s.CPU.Usage.Valid {
			return 0, false
		}
		return s.CPU.Usage.Fraction, true
	case SourceMemory:
		if s.Memory == nil {
			return 0, false
		}
		return capacityFraction(s.Memory.Memory)
	case SourceFilesystem:
		if s.Filesystem == nil {
			return 0, false
		}
		for _, fs := range s.Filesystem.Filesystems {
			if fs.MountPoint == sel.Subject {
				return capacityFraction(fs.Capacity)
			}
		}
	}
	return 0, false
}

// Rate reports bytes per second for the rate sources, for the one subject and
// direction the selector names.
func (s Snapshot) Rate(sel Selector) (float64, bool) {
	switch sel.Source {
	case SourceBlock:
		if s.Block == nil {
			return 0, false
		}
		for _, d := range s.Block.Devices {
			if d.Name != sel.Subject {
				continue
			}
			if !d.Rates.Valid {
				return 0, false
			}
			if sel.Direction == "write" {
				return d.Rates.WriteBytesPerSecond, true
			}
			return d.Rates.ReadBytesPerSecond, true
		}
	case SourceNetwork:
		if s.Network == nil {
			return 0, false
		}
		for _, i := range s.Network.Interfaces {
			if i.Name != sel.Subject {
				continue
			}
			if !i.Rates.Valid {
				return 0, false
			}
			if sel.Direction == "tx" {
				return i.Rates.TransmitBytesPerSecond, true
			}
			return i.Rates.ReceiveBytesPerSecond, true
		}
	}
	return 0, false
}

// Value is the number one selector describes, whichever kind its source
// yields. Absent means the source is unleased, failed this pass, or does not
// carry the subject the selector names.
func (s Snapshot) Value(sel Selector) (float64, bool) {
	if v, ok := s.Fraction(sel); ok {
		return v, true
	}
	return s.Rate(sel)
}

// capacityFraction is used over total. A zero total means the capacity was not
// read, which is absent rather than zero per cent.
func capacityFraction(c metrics.Capacity) (float64, bool) {
	if c.TotalBytes == 0 {
		return 0, false
	}
	return float64(c.UsedBytes) / float64(c.TotalBytes), true
}

// Metrics samples the leased telemetry sources on one goroutine.
//
// That goroutine solely owns the three stateful samplers. The library
// documents them as owned by one sequential caller: each retains previous
// counters, so a concurrent Sample would corrupt the rate derivation.
type Metrics struct {
	mu sync.Mutex
	// leases and history are keyed by selector rather than by source, so a
	// graph of one interface plots that interface. Both entries appear on the
	// first lease of a selector and are deleted with its last, which is what
	// stops a re-acquired widget from plotting samples from hours ago as
	// though they were contiguous.
	leases  map[Selector]*leaseSet
	history map[Selector]*ring
	stop    chan struct{}
	done    chan struct{}
	starts  int

	rearm   chan struct{}
	updates chan Snapshot
}

func NewMetrics() *Metrics {
	return &Metrics{
		leases:  make(map[Selector]*leaseSet),
		history: make(map[Selector]*ring),
		rearm:   make(chan struct{}, 1),
		updates: make(chan Snapshot, 1),
	}
}

// Updates carries the newest snapshot. The channel is created once and never
// closed, so it survives stop and start cycles.
func (m *Metrics) Updates() <-chan Snapshot { return m.updates }

// Acquire registers a consumer of one selector at the interval it needs.
func (m *Metrics) Acquire(sel Selector, interval time.Duration) (*Lease, error) {
	if sel.Source >= sourceCount {
		return nil, fmt.Errorf("services: unknown metrics source %d", uint8(sel.Source))
	}
	if interval <= 0 {
		return nil, fmt.Errorf("services: metrics interval %v is not positive", interval)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	before := m.finestLocked()
	lease := &Lease{metrics: m, selector: sel, boundary: interval}
	if m.leases[sel] == nil {
		m.leases[sel] = &leaseSet{}
		m.history[sel] = newRing(historySize)
	}
	m.leases[sel].add(lease)
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

// Leased reports whether any consumer needs this exact selector.
func (m *Metrics) Leased(sel Selector) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leases[sel].len() > 0
}

// SourceLeased reports whether any consumer needs this source, whatever its
// subject. The sampling loop asks this: one collector serves every subject it
// reports, so a second interface costs no second read.
func (m *Metrics) SourceLeased(src Source) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sourceLeasedLocked(src)
}

func (m *Metrics) sourceLeasedLocked(src Source) bool {
	for sel, set := range m.leases {
		if sel.Source == src && set.len() > 0 {
			return true
		}
	}
	return false
}

// Close releases every lease and stops the goroutine. It is safe to call twice.
func (m *Metrics) Close() {
	m.mu.Lock()
	for sel, set := range m.leases {
		for _, l := range set.clear() {
			l.metrics = nil
		}
		delete(m.leases, sel)
		delete(m.history, sel)
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
	for _, set := range m.leases {
		if f := set.finest(); f != 0 && (out == 0 || f < out) {
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
	for _, set := range m.leases {
		if set.len() > 0 {
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
	set := m.leases[l.selector]
	if set == nil || !set.remove(l) {
		m.mu.Unlock()
		return
	}
	// The ring goes with the last lease. Keeping it would let a widget removed
	// at midday and restored in the evening plot the two windows as one
	// continuous line across a gap of hours.
	if set.len() == 0 {
		delete(m.leases, l.selector)
		delete(m.history, l.selector)
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

	if m.SourceLeased(SourceCPU) {
		if v, err := s.cpu.Sample(); err != nil {
			noteFailure(failing, SourceCPU, err)
		} else {
			noteRecovery(failing, SourceCPU)
			snap.CPU = &v
		}
	}
	if m.SourceLeased(SourceMemory) {
		if v, err := metrics.ReadMemory(); err != nil {
			noteFailure(failing, SourceMemory, err)
		} else {
			noteRecovery(failing, SourceMemory)
			snap.Memory = &v
		}
	}
	if m.SourceLeased(SourceFilesystem) {
		if v, err := metrics.ReadFilesystems(); err != nil {
			noteFailure(failing, SourceFilesystem, err)
		} else {
			noteRecovery(failing, SourceFilesystem)
			snap.Filesystem = &v
		}
	}
	if m.SourceLeased(SourceBlock) {
		if v, err := s.block.Sample(); err != nil {
			noteFailure(failing, SourceBlock, err)
		} else {
			noteRecovery(failing, SourceBlock)
			snap.Block = &v
		}
	}
	if m.SourceLeased(SourceNetwork) {
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

// historySize is the number of samples a graph plots. It matches the reference
// shell's window; at the default two-second interval it is four minutes.
const historySize = 120

// ring is a fixed-capacity sample buffer, newest last. It never allocates
// after construction, because a graph pushes one value per source per tick for
// the process lifetime.
type ring struct {
	values0 []float64
	next    int
	filled  bool
}

func newRing(size int) *ring { return &ring{values0: make([]float64, size)} }

func (r *ring) push(v float64) {
	r.values0[r.next] = v
	r.next++
	if r.next == len(r.values0) {
		r.next, r.filled = 0, true
	}
}

// values returns the samples oldest first, in a slice the caller owns.
func (r *ring) values() []float64 {
	if !r.filled {
		return append([]float64(nil), r.values0[:r.next]...)
	}
	out := make([]float64, 0, len(r.values0))
	out = append(out, r.values0[r.next:]...)
	return append(out, r.values0[:r.next]...)
}

// History returns one selector's samples, oldest first. An unleased or
// never-sampled selector returns nothing, which a graph renders as an empty
// plot rather than an error.
func (m *Metrics) History(sel Selector) []float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.history[sel]; r != nil {
		return r.values()
	}
	return nil
}

// Histories returns every leased selector's samples. The registry assembles
// one of these per update rather than asking selector by selector.
func (m *Metrics) Histories() map[Selector][]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[Selector][]float64, len(m.history))
	for sel, r := range m.history {
		out[sel] = r.values()
	}
	return out
}

// record appends one sample per leased selector, so a graph plots the subject
// its widget names rather than an aggregate of every subject the source
// reports.
//
// A selector with no reading this pass is skipped rather than pushed as zero:
// a failed collector is not a measurement of zero, and inventing one would
// draw a trough that never happened.
func (m *Metrics) record(snap Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sel, r := range m.history {
		if v, ok := snap.Value(sel); ok {
			r.push(v)
		}
	}
}

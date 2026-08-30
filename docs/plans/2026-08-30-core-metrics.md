# Core Metrics (Milestone 3, Tranche 3B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship five metric widget types — CPU, memory, filesystem, block and network — on every
configured output, fed by one process-level sampling owner with per-source consumer counting.

**Architecture:** A second concrete service, `services.Metrics`, mirrors `services.Clock`: leases start
and stop one sampling goroutine, which samples at the finest interval any live lease requires and only
for sources something references. That goroutine solely owns the three stateful `sysc-metrics` samplers,
which the library documents as sequentially owned. Widgets stay pure functions from an immutable view to
a string, as in Tranche 3A; the view gains a metrics snapshot and a history ring.

**Tech Stack:** Go 1.26, standard library, plus one new dependency: `github.com/Nomadcxx/sysc-metrics`
at the tag `sysc-7` produces. Existing pinned modules unchanged.

**Spec:** `docs/plans/2026-08-30-core-metrics-design.md`

## Global Constraints

Every task's requirements implicitly include this section.

- Linux and Niri are the only platform contract.
- Go owns the shell. No C++, Rust, Lua, Luau, Qt, QML, or Quickshell.
- One goroutine owns the Wayland connection and every Wayland proxy. No new code calls a Wayland proxy.
- **One goroutine owns all three `sysc-metrics` samplers.** `CPUSampler`, `BlockSampler` and
  `NetworkSampler` retain previous counters and are documented as owned by one sequential caller.
- **No local `replace` directive and no untagged `sysc-metrics` import.** Both are recorded stop
  conditions. Task 0 gates on the tag existing.
- Rates come from the library's monotonic comparison. The shell never divides a counter delta by a
  nominal interval.
- Widget instances are keyed by `wl_registry` global name (`uint32`). The connector is an attribute.
- One sampling timer for the whole process, never one per output.
- No widget interface, no widget schema, no plugin protocol, no service registry, no
  dependency-injection container, and no single-implementation interface.
- No thermal, battery, GPU or process metrics. Those are Tranche 3C and `sysc-metrics` M2.
- No tooltip, no icon, no popout, no stale or error node. Those are Tranche 3D.
- Default bar geometry is unchanged: nominal height 48, gap 4, painted body 40, exclusive zone 44,
  radius 12.
- All new goroutines must stop under cancellation, and `go test -race` must report no data race.
- Test fixtures use connectors `DP-9` and `HDMI-A-9`, devices `nvme9n1`, interfaces `eth9`, and mount
  point `/fixture`. No real connector, device, interface, mount or machine value enters Git.
- Commit messages must not contain any of `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`,
  `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `llm`, `bot`, `agent` as a case-insensitive
  substring; a repository hook rejects them. Note that this rejects innocent words containing them, such
  as "both" and "robot".
- Commits from a worktree need `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit …`, or the
  bd pre-commit hook aborts and blocks every commit on the branch.

## File Structure

**Created**

| Path | Responsibility |
|---|---|
| `internal/services/leases.go` | `leaseSet`: lease bookkeeping shared by `Clock` and `Metrics`. |
| `internal/services/leases_test.go` | Lease counting, finest-interval selection, start/stop transitions. |
| `internal/services/metrics.go` | The `Metrics` service: sources, leases, sampling loop, history. |
| `internal/services/metrics_test.go` | Lifetime, per-source sampling, history, cancellation. |
| `internal/shell/metricwidget.go` | Metric widget construction and formatting. |
| `internal/shell/metricwidget_test.go` | Formatting, fraction/rate rules, unavailable rendering. |

**Modified**

| Path | Change |
|---|---|
| `internal/services/clock.go` | Adopt `leaseSet`; behaviour unchanged. |
| `internal/config/config.go` | Five item ids; `Display`, `Interval`, `Path`, `Device`, `Interface`, `Direction`. |
| `internal/config/load.go` | Per-id option validation, including the fraction/rate display rule. |
| `internal/config/config_test.go` | Vocabulary and validation cases. |
| `internal/ui/tree.go` | `Node.MinWidth`, `KindGraph`, `Node.Values`. |
| `internal/ui/layout.go` | `measureNode` floors text at `MinWidth`; measures `KindGraph`. |
| `internal/ui/layout_test.go` | Floor and graph measurement coverage. |
| `internal/render/paint.go` | Paint `KindGraph`. |
| `internal/render/paint_test.go` | Graph paint coverage. |
| `internal/shell/widget.go` | `barView` carries the snapshot; `buildWidgets` builds metric widgets. |
| `internal/shell/registry.go` | Metric leases, `UpdateMetrics`, `Metrics()` accessor. |
| `internal/shell/registry_test.go` | Metric lease and update coverage. |
| `cmd/sysc-shell/main.go` | Metrics pump goroutine. |
| `go.mod`, `go.sum` | Add `sysc-metrics` at its release tag. |
| `tests/integration/README.md` | Tranche 3B live matrix. |

## Lanes

Tasks 1–4 (services) and Tasks 5–6 (UI primitives) touch disjoint packages and may run in separate
worktrees after Task 0. Tasks 7–12 are the integration lane and are strictly serial.

---

### Task 0: Gate — the sysc-metrics release tag

This is a verification gate, not a code change. The tranche cannot be implemented without a tagged
dependency. If the tag is absent, **stop and return to the owner**; do not add a `replace` directive.

**Files:** none.

- [ ] **Step 1: Confirm a tag exists and is pushed**

```bash
git -C /home/nomadx/sysc-metrics tag --list
git -C /home/nomadx/sysc-metrics ls-remote --tags origin
```

Expected: at least one semantic version tag, present on the remote. If the list is empty, **stop**:
`sysc-7` has not closed and this plan cannot execute.

- [ ] **Step 2: Confirm the public API matches the design**

```bash
cd /home/nomadx/sysc-metrics
grep -nE '^func (New(CPU|Block|Network)Sampler|Read(Memory|Filesystems|Uptime))' *.go
grep -nE '^func \(s \*(CPU|Block|Network)Sampler\) Sample' *.go
```

Expected: `NewCPUSampler`, `NewBlockSampler`, `NewNetworkSampler`, `ReadMemory`, `ReadFilesystems`,
`ReadUptime`, and three `Sample() (…Snapshot, error)` methods. If any signature differs, record the
difference and adapt only `internal/services/metrics.go` and `internal/shell/metricwidget.go`; nothing
else in this plan depends on the library surface.

- [ ] **Step 3: Add the dependency at its tag**

```bash
cd /home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/core-metrics
go get github.com/Nomadcxx/sysc-metrics@<tag>
go mod tidy
grep -n "replace" go.mod || echo "no replace directive, correct"
go build ./...
```

Expected: `go.mod` names the tagged version, no `replace` directive appears, and the tree builds.

- [ ] **Step 4: Commit the dependency**

```bash
git add go.mod go.sum
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "build: depend on the tagged metrics library"
```

---

### Task 1: Extract the shared lease set

A pure refactor of shipped code. `Clock`'s behaviour must not change; its existing tests are the
regression net and must pass untouched.

**Files:**
- Create: `internal/services/leases.go`, `internal/services/leases_test.go`
- Modify: `internal/services/clock.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `leaseSet` with `add(*Lease) (previous, current time.Duration)`, `remove(*Lease) bool`,
  `finest() time.Duration`, `len() int`, `clear() []*Lease`. Task 2 uses it.

- [ ] **Step 1: Write the failing test**

Create `internal/services/leases_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run LeaseSet -v`
Expected: FAIL to compile — `leaseSet` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/services/leases.go`:

```go
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
```

Now rewrite `internal/services/clock.go` to hold a `leaseSet` instead of a bare slice. Replace the
`leases []*Lease` field with `leases leaseSet`, and replace the bodies that touched it:

```go
// Acquire registers a consumer needing updates at least every boundary.
func (c *Clock) Acquire(boundary time.Duration) (*Lease, error) {
	if boundary <= 0 {
		return nil, fmt.Errorf("services: clock boundary %v is not positive", boundary)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lease := &Lease{clock: c, boundary: boundary}
	previous, current := c.leases.add(lease)

	switch {
	case c.stop == nil:
		c.startLocked()
	case previous != 0 && current < previous:
		// The goroutine is asleep on a longer deadline; wake it to re-arm.
		select {
		case c.rearm <- struct{}{}:
		default:
		}
	}
	return lease, nil
}
```

```go
func (l *Lease) Release() {
	if l == nil || l.clock == nil {
		return
	}
	c := l.clock
	l.clock = nil

	c.mu.Lock()
	if !c.leases.remove(l) {
		c.mu.Unlock()
		return
	}
	done := c.stopIfUnusedLocked()
	c.mu.Unlock()

	// Waiting outside the lock: the goroutine takes the same mutex.
	if done != nil {
		<-done
	}
}
```

```go
// Close releases every lease and stops the goroutine. It is safe to call twice.
func (c *Clock) Close() {
	c.mu.Lock()
	for _, l := range c.leases.clear() {
		l.clock = nil
	}
	done := c.stopIfUnusedLocked()
	c.mu.Unlock()

	if done != nil {
		<-done
	}
}
```

Delete `Clock.finestLocked` and replace its two call sites — in `run` and previously in `Acquire` —
with `c.leases.finest()`. In `stopIfUnusedLocked`, replace `len(c.leases) > 0` with
`c.leases.len() > 0`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS, including every pre-existing clock test unchanged. If a clock test needed editing, the
refactor changed behaviour — revert and try again.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services && go vet ./internal/services/
git add internal/services/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "refactor(services): share lease bookkeeping between services"
```

---

### Task 2: The metrics service — sources, leases and lifetime

**Files:**
- Create: `internal/services/metrics.go`, `internal/services/metrics_test.go`

**Interfaces:**
- Consumes: `leaseSet`, `Lease` from Task 1.
- Produces: `Source` with constants `SourceCPU`, `SourceMemory`, `SourceFilesystem`, `SourceBlock`,
  `SourceNetwork`; `NewMetrics() *Metrics`; `(*Metrics).Acquire(Source, time.Duration) (*Lease, error)`;
  `(*Metrics).Updates() <-chan Snapshot`; `(*Metrics).Close()`; `(*Metrics).Running() bool`;
  `(*Metrics).Starts() int`; `(*Metrics).Leased(Source) bool`; the `Snapshot` type. Tasks 3, 4 and 9 use
  these.

- [ ] **Step 1: Write the failing test**

Create `internal/services/metrics_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run Metric -v`
Expected: FAIL to compile — `NewMetrics`, `Source` and `SourceCPU` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/services/metrics.go`:

```go
package services

import (
	"fmt"
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
```

`Lease` gains the two fields the metrics path needs, and `Release` dispatches on which service owns it.
In `internal/services/clock.go`, replace the `Lease` type and its `Release`:

```go
// Lease is one consumer's claim on a service. Exactly one of clock or metrics
// is set; the zero value is an already-released lease.
type Lease struct {
	clock    *Clock
	metrics  *Metrics
	source   Source
	boundary time.Duration
}

// Release drops a consumer, stopping its service when it was the last one. It
// is idempotent and safe on a nil lease.
func (l *Lease) Release() {
	switch {
	case l == nil:
		return
	case l.clock != nil:
		c := l.clock
		l.clock = nil
		c.releaseClock(l)
	case l.metrics != nil:
		m := l.metrics
		l.metrics = nil
		m.releaseMetric(l)
	}
}
```

Rename the body of the old `Release` to `func (c *Clock) releaseClock(l *Lease)`, taking the lease as a
parameter and no longer clearing `l.clock` itself.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS. `run`, `ring`, `newRing` and `historySize` do not exist yet, so add temporary stubs at
the bottom of `metrics.go` to compile, and delete them in Tasks 3 and 4:

```go
const historySize = 120

type ring struct{}

func newRing(int) *ring { return &ring{} }

func (m *Metrics) run(stop, done chan struct{}) { <-stop; close(done) }
```

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services && go vet ./internal/services/
git add internal/services/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(services): add the leased metrics service lifetime"
```

---

### Task 3: The sampling loop

Replaces Task 2's `run` stub with real collection. The three stateful samplers are constructed once and
owned by this goroutine alone.

**Files:**
- Modify: `internal/services/metrics.go`, `internal/services/metrics_test.go`

**Interfaces:**
- Consumes: `Snapshot`, `Source`, `Metrics` from Task 2.
- Produces: a populated `Snapshot` on `Updates()`. Task 9 consumes it.

- [ ] **Step 1: Write the failing test**

Append to `internal/services/metrics_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run 'Populated|Newest' -v`
Expected: FAIL — the stub `run` never publishes, so `TestOnlyLeasedSourcesArePopulated` times out, and
`sendSnapshot` is undefined.

- [ ] **Step 3: Write the implementation**

In `internal/services/metrics.go`, delete the `run` stub and add:

```go
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
```

Add `"os"` to the file's imports. `m.record` is added in Task 4; until then, add a temporary stub and
delete it there:

```go
func (m *Metrics) record(Snapshot) {}
```

Note the deliberate difference from the clock: there is no boundary alignment. Metrics have no
wall-clock boundary to land on, so the deadline is simply `now + interval`. Rates still come from the
library's monotonic comparison of two samples, never from this interval.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS. The memory test reads real `/proc/meminfo`, which is available on any Linux host.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services && go vet ./internal/services/
git add internal/services/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(services): sample every leased telemetry source"
```

---

### Task 4: The history ring

Replaces Task 3's `record` stub and Task 2's `ring` stub. A graph plots this.

**Files:**
- Modify: `internal/services/metrics.go`, `internal/services/metrics_test.go`

**Interfaces:**
- Consumes: `Snapshot`, `Source` from Tasks 2–3.
- Produces: `(*Metrics).History(Source) []float64`, oldest first. Task 8 plots it.

- [ ] **Step 1: Write the failing test**

Append to `internal/services/metrics_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run 'Ring|History' -v`
Expected: FAIL to compile — `push`, `values` and `History` undefined; `newRing` returns the empty stub.

- [ ] **Step 3: Write the implementation**

In `internal/services/metrics.go`, replace the `ring` and `record` stubs:

```go
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

// History returns one source's samples, oldest first. An unleased or
// never-sampled source returns an empty slice, which a graph renders as an
// empty plot rather than an error.
func (m *Metrics) History(src Source) []float64 {
	if src >= sourceCount {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.history[src].values()
}

// record appends each present source's headline fraction. A rate source has no
// natural full scale, so it records the raw rate and the graph normalises
// against its own window maximum.
func (m *Metrics) record(snap Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if snap.CPU != nil && snap.CPU.Usage.Valid {
		m.history[SourceCPU].push(snap.CPU.Usage.Fraction)
	}
	if snap.Memory != nil {
		m.history[SourceMemory].push(usedFraction(snap.Memory.Memory))
	}
	if snap.Filesystem != nil && len(snap.Filesystem.Filesystems) > 0 {
		m.history[SourceFilesystem].push(usedFraction(snap.Filesystem.Filesystems[0].Capacity))
	}
	if snap.Block != nil {
		var total float64
		for _, d := range snap.Block.Devices {
			if d.Rates.Valid {
				total += d.Rates.ReadBytesPerSecond + d.Rates.WriteBytesPerSecond
			}
		}
		m.history[SourceBlock].push(total)
	}
	if snap.Network != nil {
		var total float64
		for _, i := range snap.Network.Interfaces {
			if i.Rates.Valid {
				total += i.Rates.ReceiveBytesPerSecond + i.Rates.TransmitBytesPerSecond
			}
		}
		m.history[SourceNetwork].push(total)
	}
}

// usedFraction is used over total, or zero when the total is unknown.
func usedFraction(c metrics.Capacity) float64 {
	if c.TotalBytes == 0 {
		return 0
	}
	return float64(c.UsedBytes) / float64(c.TotalBytes)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services && go vet ./internal/services/
git add internal/services/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(services): keep a bounded sample history per source"
```

---

### Task 5: A minimum width for text nodes

Prior art requires this: a percentage crossing from one digit to three reflows its section every sample.
Tabular figures align digits but cannot fix a changing digit count.

**Files:**
- Modify: `internal/ui/tree.go`, `internal/ui/layout.go`
- Test: `internal/ui/layout_test.go:` append

**Interfaces:**
- Consumes: nothing.
- Produces: `ui.Node.MinWidth int`, honoured by `measureNode` for `KindText`. Task 8 sets it.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/layout_test.go`:

```go
// A short string in a floored node still occupies the floor, so a percentage
// does not reflow its section as it crosses from one digit to three.
func TestTextIsFlooredAtItsMinWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "9%", MinWidth: 40},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 40 {
		t.Fatalf("floored width = %d, want the 40 floor", got)
	}
}

// Text wider than the floor keeps its natural width.
func TestMinWidthDoesNotShrinkWideText(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "100%", MinWidth: 20},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 40 {
		t.Fatalf("width = %d, want the natural 40", got)
	}
}

// The floor and the cap compose: the cap still wins over a wider floor.
func TestMaxWidthStillCapsAFlooredNode(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "aaaaaaaa", MinWidth: 70, MaxWidth: 50},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 50 {
		t.Fatalf("width = %d, want the 50 cap to win over the 70 floor", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ -run MinWidth -v`
Expected: FAIL to compile — `MinWidth` is not a field of `Node`.

- [ ] **Step 3: Write the implementation**

In `internal/ui/tree.go`, add the field below `MaxWidth`:

```go
	// MinWidth reserves a floor for a text node's measured width. Zero means
	// natural. A percentage sets it so its section does not reflow as the
	// value crosses from one digit to three; tabular figures align digits but
	// cannot fix a changing digit count.
	MinWidth int
```

In `internal/ui/layout.go`, apply the floor before the cap in `measureNode`:

```go
	case KindText:
		w, h := measure(n.Text, n.Tabular)
		if n.MinWidth > w {
			w = n.MinWidth
		}
		if n.MaxWidth > 0 && w > n.MaxWidth {
			w = n.MaxWidth
		}
		return w, h, nil
```

The cap is applied last so it always wins: a node cannot be forced wider than its cap by a floor.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/ -v`
Expected: PASS, including every pre-existing layout and bar test.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ui && go vet ./internal/ui/
git add internal/ui/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(ui): reserve a width floor for text nodes"
```

---

### Task 6: The graph node

**Files:**
- Modify: `internal/ui/tree.go`, `internal/ui/layout.go`, `internal/render/paint.go`
- Test: `internal/ui/layout_test.go:` append, `internal/render/paint_test.go:` append

**Interfaces:**
- Consumes: nothing.
- Produces: `ui.KindGraph`, `ui.Node.Values []float64`. Task 8 populates them.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/layout_test.go`:

```go
// A graph occupies its configured width and the full content height, so it
// reserves space the way a meter does rather than measuring its data.
func TestAGraphMeasuresItsConfiguredWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindGraph, Width: 60, Values: []float64{0.1, 0.9}},
	}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 60 {
		t.Fatalf("graph width = %d, want the configured 60", got)
	}
}

// A graph with no samples still reserves its width, so a bar does not reflow
// when the first sample arrives.
func TestAnEmptyGraphStillReservesItsWidth(t *testing.T) {
	t.Parallel()
	measure := func(s string, _ bool) (int, int) { return 10 * len([]rune(s)), 10 }

	root := &Node{Kind: KindRow, Children: []*Node{{Kind: KindGraph, Width: 60}}}
	if err := Layout(root, Rect{X: 0, Y: 0, W: 200, H: 20}, measure); err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if got := root.Children[0].Bounds.W; got != 60 {
		t.Fatalf("empty graph width = %d, want 60", got)
	}
}
```

Append to `internal/render/paint_test.go`:

```go
// A graph paints a column per sample, taller for larger values, and leaves the
// unfilled part of the box alone.
func TestGraphPaintsTallerColumnsForLargerValues(t *testing.T) {
	t.Parallel()

	canvas, style, r := newPaintFixture(t, 40, 20)
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind:   ui.KindGraph,
		Width:  4,
		Values: []float64{0, 1},
		Bounds: ui.Rect{X: 0, Y: 0, W: 4, H: 20},
	}}}

	if err := Paint(canvas, root, r, style); err != nil {
		t.Fatalf("Paint: %v", err)
	}

	// The full-height column must paint more pixels than the zero column.
	left := filledPixels(canvas, ui.Rect{X: 0, Y: 0, W: 2, H: 20})
	right := filledPixels(canvas, ui.Rect{X: 2, Y: 0, W: 2, H: 20})
	if right <= left {
		t.Fatalf("full column painted %d pixels, zero column %d; want the full one taller",
			right, left)
	}
}
```

Add the two helpers at the bottom of `internal/render/paint_test.go` if they are absent:

```go
// newPaintFixture builds a canvas, style and renderer for one paint assertion.
func newPaintFixture(t *testing.T, w, h int) (*Canvas, ProofStyle, *TextRenderer) {
	t.Helper()
	pixels := make([]byte, w*h*4)
	canvas, err := NewCanvas(pixels, w, h, w*4)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	style := ProofStyle{
		Size:       14,
		Scale120:   ui.ScaleUnit,
		Background: Color{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
		Foreground: Color{R: 0xe8, G: 0xec, B: 0xf0, A: 0xff},
		Accent:     Color{R: 0x00, G: 0x80, B: 0xff, A: 0xff},
		Track:      Color{R: 0x30, G: 0x34, B: 0x38, A: 0xff},
	}
	return canvas, style, NewTextRenderer(mustTestFace(t))
}

// filledPixels counts pixels in the box that differ from the background.
func filledPixels(c *Canvas, box ui.Rect) int {
	count := 0
	for y := box.Y; y < box.Y+box.H; y++ {
		for x := box.X; x < box.X+box.W; x++ {
			i := y*c.Stride() + x*4
			if c.Pixels()[i] != 0 || c.Pixels()[i+1] != 0 || c.Pixels()[i+2] != 0 {
				count++
			}
		}
	}
	return count
}
```

If `Canvas` exposes no `Stride()` or `Pixels()` accessor, read the fields directly — the test is in
package `render`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ ./internal/render/ -run Graph -v`
Expected: FAIL to compile — `KindGraph` and `Node.Values` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/ui/tree.go`, add the kind and the field:

```go
const (
	KindRow Kind = iota
	KindText
	KindMeter
	KindButton
	KindGraph
)
```

```go
	// Values are the graph's samples, oldest first, each already normalised to
	// zero through one by the widget. The node carries no scale of its own.
	Values []float64
```

In `internal/ui/layout.go`, add the measurement case to `measureNode`:

```go
	case KindGraph:
		// A graph reserves its configured width and the full content height,
		// the way a meter does. It does not measure its data, so a bar does
		// not reflow as samples arrive.
		return n.Width, contentHeight, nil
```

In `internal/render/paint.go`, add the paint case beside `KindMeter`:

```go
	case ui.KindGraph:
		return paintGraph(c, n, style.Scale120.PhysicalRect(n.Bounds), style)
```

and the painter:

```go
// paintGraph fills one column per sample, newest at the right, using the same
// rectangle fill the meter uses. There is no path rasteriser and no
// anti-aliasing: a bar-height sparkline needs neither.
//
// Values are already normalised to zero through one by the widget, so this
// applies no scale of its own.
func paintGraph(c *Canvas, n *ui.Node, box ui.Rect, style ProofStyle) error {
	if box.W <= 0 || box.H <= 0 || len(n.Values) == 0 {
		return nil
	}

	// Columns are laid out newest-last. When there are more samples than
	// pixels, the oldest are dropped rather than averaged: the recent shape is
	// what a glanceable bar graph is for.
	values := n.Values
	if len(values) > box.W {
		values = values[len(values)-box.W:]
	}
	width := box.W / len(values)
	if width < 1 {
		width = 1
	}

	for i, v := range values {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		height := int(float64(box.H) * v)
		if height <= 0 {
			continue
		}
		x := box.X + box.W - (len(values)-i)*width
		if x < box.X {
			continue
		}
		fillRect(c, ui.Rect{X: x, Y: box.Y + box.H - height, W: width, H: height}, style.Accent)
	}
	return nil
}
```

If the meter's painter uses a helper other than `fillRect`, use that one instead — read the `KindMeter`
case and reuse exactly what it calls, so the graph and the meter fill pixels identically.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/ui/ ./internal/render/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ui internal/render && go vet ./internal/ui/ ./internal/render/
git add internal/ui/ internal/render/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(ui): add a sparkline graph node"
```

---

### Task 7: Configuration vocabulary

Adds the five metric ids and their options. The fraction-versus-rate display rule lives here, because it
is a property of the configured value and must fail at load rather than render nonsense.

**Files:**
- Modify: `internal/config/config.go`, `internal/config/load.go`
- Test: `internal/config/config_test.go:` append

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `config.Item` gains `Display string`, `Interval time.Duration`, `Path string`,
  `Device string`, `Interface string`, `Direction string`. Tasks 8 and 9 read them.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestMetricItemsCarryTheirSelectorsAndDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"items":{"right":[
		{"id":"cpu","display":"meter"},
		{"id":"memory"},
		{"id":"filesystem","path":"/fixture"},
		{"id":"block","device":"nvme9n1","direction":"read"},
		{"id":"network","interface":"eth9","direction":"rx","display":"graph"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	items := cfg.Bar.Right
	if len(items) != 5 {
		t.Fatalf("items = %d, want 5", len(items))
	}
	if items[0].Display != "meter" {
		t.Fatalf("cpu display = %q, want meter", items[0].Display)
	}
	if items[1].Display != "text" {
		t.Fatalf("memory display = %q, want the text default", items[1].Display)
	}
	if items[2].Path != "/fixture" {
		t.Fatalf("filesystem path = %q, want /fixture", items[2].Path)
	}
	if items[3].Device != "nvme9n1" || items[3].Direction != "read" {
		t.Fatalf("block = %+v, want device nvme9n1 reading", items[3])
	}
	if items[4].Interface != "eth9" || items[4].Direction != "rx" {
		t.Fatalf("network = %+v, want eth9 receiving", items[4])
	}
	for _, item := range items {
		if item.Interval <= 0 {
			t.Fatalf("item %+v has no sampling interval", item)
		}
	}
}

// A rate has no full scale, so a meter of "3.2 MB/s" would be meaningless.
func TestAMeterOnARateSourceIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"block","device":"nvme9n1","display":"meter"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":"eth9","display":"meter"}]}}}`,
	} {
		_, err := Parse([]byte(body))
		if err == nil {
			t.Fatalf("a meter on a rate source was accepted: %s", body)
		}
		if !strings.Contains(err.Error(), "display") {
			t.Fatalf("error %q does not name the display field", err)
		}
	}
}

func TestAGraphIsAcceptedOnEverySource(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"cpu","display":"graph"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":"eth9","display":"graph"}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err != nil {
			t.Fatalf("a graph was rejected on %s: %v", body, err)
		}
	}
}

func TestAMissingSelectorIsRejected(t *testing.T) {
	t.Parallel()
	cases := []struct{ body, want string }{
		{`{"bar":{"items":{"right":[{"id":"filesystem"}]}}}`, "path"},
		{`{"bar":{"items":{"right":[{"id":"block"}]}}}`, "device"},
		{`{"bar":{"items":{"right":[{"id":"network"}]}}}`, "interface"},
	}
	for _, c := range cases {
		err := errFromParse(t, c.body)
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("error %q does not name the missing %q", err, c.want)
		}
	}
}

func TestASelectorOnTheWrongItemIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"cpu","path":"/fixture"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"memory","device":"nvme9n1"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"clock","interval":"2s"}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("an option on the wrong item was accepted: %s", body)
		}
	}
}

func TestAnInvalidDirectionIsRejected(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"bar":{"items":{"right":[{"id":"block","device":"nvme9n1","direction":"rx"}]}}}`,
		`{"bar":{"items":{"right":[{"id":"network","interface":"eth9","direction":"read"}]}}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("a direction from the wrong vocabulary was accepted: %s", body)
		}
	}
}

func TestANonPositiveIntervalIsRejected(t *testing.T) {
	t.Parallel()
	body := `{"bar":{"items":{"right":[{"id":"cpu","interval":"0s"}]}}}`
	if _, err := Parse([]byte(body)); err == nil {
		t.Fatal("a zero interval was accepted")
	}
}

// errFromParse fails the test unless parsing returns an error.
func errFromParse(t *testing.T, body string) error {
	t.Helper()
	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatalf("Parse(%s) succeeded, want a validation error", body)
	}
	return err
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run 'Metric|Meter|Graph|Selector|Direction|Interval' -v`
Expected: FAIL — the five ids are not in `knownItems`, so every case is rejected as an unknown item and
the assertions about specific fields never run.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, extend `Item` and the vocabulary:

```go
// Item is one validated widget instance. Options live on the instance rather
// than the bar, so one bar can carry two clocks with different formats and two
// filesystem widgets watching different mounts.
type Item struct {
	ID string
	// Format is the Go layout string for a clock. Empty on other items.
	Format string
	// Boundary is how often this clock's text can change, derived from Format
	// at load. Zero on other items.
	Boundary time.Duration
	// MaxWidth caps a window title in logical pixels. Zero on other items.
	MaxWidth int

	// Display is "text", "meter" or "graph" on a metric item. Empty elsewhere.
	Display string
	// Interval is the sampling interval for a metric item. Zero elsewhere.
	Interval time.Duration
	// Path names the mount a filesystem item watches. Empty on other items.
	Path string
	// Device names the block device a block item watches.
	Device string
	// Interface names the network interface a network item watches.
	Interface string
	// Direction is "read"/"write" on block and "rx"/"tx" on network.
	Direction string
}
```

```go
// knownItems is the Milestone 3 widget vocabulary through Tranche 3B.
var knownItems = map[string]struct{}{
	"clock": {}, "workspace": {}, "window-title": {},
	"cpu": {}, "memory": {}, "filesystem": {}, "block": {}, "network": {},
}

// fractionSources yield a value between zero and one, which a meter can fill.
// Rate sources yield bytes per second and have no full scale, so a meter is
// meaningless on them and rejected at load.
var fractionSources = map[string]bool{"cpu": true, "memory": true, "filesystem": true}

// rateSources yield bytes per second.
var rateSources = map[string]bool{"block": true, "network": true}

const (
	// defaultMetricInterval is the sampling period a metric item uses unless
	// it names its own.
	defaultMetricInterval = 2 * time.Second
	// defaultMetricDisplay renders a value as text.
	defaultMetricDisplay = "text"
)

// blockDirections and networkDirections are deliberately separate vocabularies
// so "rx" on a block device fails rather than silently meaning "read".
var blockDirections = map[string]bool{"read": true, "write": true}
var networkDirections = map[string]bool{"rx": true, "tx": true}

// isMetric reports whether an id names a metric widget.
func isMetric(id string) bool { return fractionSources[id] || rateSources[id] }
```

In `internal/config/load.go`, extend `wireItem` and `resolveItem`:

```go
type wireItem struct {
	ID       string  `json:"id"`
	Format   *string `json:"format"`
	MaxWidth *int    `json:"max-width"`

	Display   *string `json:"display"`
	Interval  *string `json:"interval"`
	Path      *string `json:"path"`
	Device    *string `json:"device"`
	Interface *string `json:"interface"`
	Direction *string `json:"direction"`
}
```

Add these branches to `resolveItem`, after the existing `format` and `max-width` rejections and before
the `switch w.ID`:

```go
	if !isMetric(w.ID) {
		for _, unwanted := range []struct {
			name string
			set  bool
		}{
			{"display", w.Display != nil},
			{"interval", w.Interval != nil},
			{"path", w.Path != nil},
			{"device", w.Device != nil},
			{"interface", w.Interface != nil},
			{"direction", w.Direction != nil},
		} {
			if unwanted.set {
				return Item{}, pathErr(path+"."+unwanted.name,
					"is accepted only on a metric item, not on %q", w.ID)
			}
		}
	}
```

and this case to the `switch w.ID`, listing every metric id:

```go
	case "cpu", "memory", "filesystem", "block", "network":
		resolved, err := resolveMetric(w, path)
		if err != nil {
			return Item{}, err
		}
		item = resolved
```

Add the metric resolver to the same file:

```go
// resolveMetric validates one metric item and fills in its defaults.
//
// A selector naming an absent mount, device or interface is deliberately not
// an error here: devices are hot-plugged and interfaces come and go, so the
// widget validates as well-formed and renders the unavailable placeholder
// until its subject appears.
func resolveMetric(w wireItem, path string) (Item, error) {
	item := Item{
		ID:       w.ID,
		Display:  defaultMetricDisplay,
		Interval: defaultMetricInterval,
	}

	if w.Display != nil {
		switch *w.Display {
		case "text", "graph":
		case "meter":
			if rateSources[w.ID] {
				return Item{}, pathErr(path+".display",
					"a meter needs a full scale, which the rate source %q has no", w.ID)
			}
		default:
			return Item{}, pathErr(path+".display",
				"%q is not one of text, meter, graph", *w.Display)
		}
		item.Display = *w.Display
	}

	if w.Interval != nil {
		interval, err := time.ParseDuration(*w.Interval)
		if err != nil {
			return Item{}, pathErr(path+".interval", "%q is not a duration such as 2s", *w.Interval)
		}
		if interval <= 0 {
			return Item{}, pathErr(path+".interval", "%v is not positive", interval)
		}
		item.Interval = interval
	}

	// Each selector is required on exactly one id and rejected on the others,
	// so a path on a CPU widget cannot be silently ignored.
	if err := selector(w.Path, w.ID, "filesystem", path+".path", &item.Path); err != nil {
		return Item{}, err
	}
	if err := selector(w.Device, w.ID, "block", path+".device", &item.Device); err != nil {
		return Item{}, err
	}
	if err := selector(w.Interface, w.ID, "network", path+".interface", &item.Interface); err != nil {
		return Item{}, err
	}

	switch w.ID {
	case "block":
		item.Direction = "read"
		if w.Direction != nil {
			if !blockDirections[*w.Direction] {
				return Item{}, pathErr(path+".direction",
					"%q is not one of read, write", *w.Direction)
			}
			item.Direction = *w.Direction
		}
	case "network":
		item.Direction = "rx"
		if w.Direction != nil {
			if !networkDirections[*w.Direction] {
				return Item{}, pathErr(path+".direction", "%q is not one of rx, tx", *w.Direction)
			}
			item.Direction = *w.Direction
		}
	default:
		if w.Direction != nil {
			return Item{}, pathErr(path+".direction",
				"is accepted only on block and network, not on %q", w.ID)
		}
	}
	return item, nil
}

// selector requires an option on the one id that needs it and rejects it
// elsewhere. A machine has many mounts, devices and interfaces, so there is no
// defensible default and the option cannot be optional.
func selector(supplied *string, id, wantID, path string, dest *string) error {
	if id != wantID {
		if supplied != nil {
			return pathErr(path, "is accepted only on %q, not on %q", wantID, id)
		}
		return nil
	}
	if supplied == nil || *supplied == "" {
		return pathErr(path, "is required on %q", wantID)
	}
	*dest = *supplied
	return nil
}
```

The existing `resolveItem` assigns `item := Item{ID: w.ID}` before the switch; change that so the metric
case can replace the whole value, and keep the `clock` and `window-title` cases writing into it as they
do now.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS, including every pre-existing configuration test.

- [ ] **Step 5: Commit**

```bash
go build ./... && gofmt -l internal/config && go vet ./internal/config/
git add internal/config/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(config): add the metric widget vocabulary"
```

---

### Task 8: Metric widgets

**Files:**
- Create: `internal/shell/metricwidget.go`, `internal/shell/metricwidget_test.go`
- Modify: `internal/shell/widget.go`

**Interfaces:**
- Consumes: `config.Item` (Task 7), `services.Snapshot` and `Source` (Tasks 2–4), `ui.MinWidth` and
  `ui.KindGraph` (Tasks 5–6).
- Produces: `metricSource(config.Item) (services.Source, bool)`; `formatMetric(config.Item,
  services.Snapshot) string`; `metricFraction(config.Item, services.Snapshot) (float64, bool)`;
  `barView` gains `Metrics services.Snapshot` and `History map[services.Source][]float64`. Task 9 fills
  them.

- [ ] **Step 1: Write the failing test**

Create `internal/shell/metricwidget_test.go`:

```go
package shell

import (
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// fixtureSnapshot carries one of every source with invented values.
func fixtureSnapshot() services.Snapshot {
	return services.Snapshot{
		CollectedAt: time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC),
		CPU:         &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 250},
		},
		Filesystem: &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{{
			MountPoint: "/fixture",
			Capacity:   metrics.Capacity{TotalBytes: 200, UsedBytes: 100},
		}}},
		Block: &metrics.BlockSnapshot{Devices: []metrics.BlockDevice{{
			Name:  "nvme9n1",
			Rates: metrics.BlockRates{ReadBytesPerSecond: 3_200_000, Valid: true},
		}}},
		Network: &metrics.NetworkSnapshot{Interfaces: []metrics.NetworkInterface{{
			Name:  "eth9",
			Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 1_500_000, Valid: true},
		}}},
	}
}

func TestFractionSourcesFormatAsPercentages(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()
	cases := []struct {
		item config.Item
		want string
	}{
		{config.Item{ID: "cpu"}, "42%"},
		{config.Item{ID: "memory"}, "25%"},
		{config.Item{ID: "filesystem", Path: "/fixture"}, "50%"},
	}
	for _, c := range cases {
		if got := formatMetric(c.item, snap); got != c.want {
			t.Fatalf("%s formatted %q, want %q", c.item.ID, got, c.want)
		}
	}
}

func TestRateSourcesFormatInDecimalUnits(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()

	block := config.Item{ID: "block", Device: "nvme9n1", Direction: "read"}
	if got := formatMetric(block, snap); got != "3.2 MB/s" {
		t.Fatalf("block rate = %q, want 3.2 MB/s", got)
	}
	network := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	if got := formatMetric(network, snap); got != "1.5 MB/s" {
		t.Fatalf("network rate = %q, want 1.5 MB/s", got)
	}
}

// An absent source, an absent subject and an invalid value all render the same
// placeholder, so a consumer never distinguishes them.
func TestUnavailableMetricsRenderThePlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		item config.Item
		snap services.Snapshot
	}{
		{"source absent", config.Item{ID: "cpu"}, services.Snapshot{}},
		{
			"mount absent",
			config.Item{ID: "filesystem", Path: "/nowhere"},
			fixtureSnapshot(),
		},
		{
			"device absent",
			config.Item{ID: "block", Device: "absent0", Direction: "read"},
			fixtureSnapshot(),
		},
		{
			"value invalid",
			config.Item{ID: "cpu"},
			services.Snapshot{CPU: &metrics.CPUSnapshot{
				Usage: metrics.CPUUsage{Fraction: 0.9, Valid: false},
			}},
		},
	}
	for _, c := range cases {
		if got := formatMetric(c.item, c.snap); got != noWorkspace {
			t.Fatalf("%s rendered %q, want %q", c.name, got, noWorkspace)
		}
	}
}

// The first rate sample is always invalid, because there is no previous
// counter to compare against.
func TestTheFirstRateSampleRendersThePlaceholder(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{Network: &metrics.NetworkSnapshot{
		Interfaces: []metrics.NetworkInterface{{
			Name:  "eth9",
			Rates: metrics.NetworkRates{Valid: false},
		}},
	}}
	item := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	if got := formatMetric(item, snap); got != noWorkspace {
		t.Fatalf("first sample rendered %q, want %q", got, noWorkspace)
	}
}

func TestDirectionSelectsTheCounter(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{Network: &metrics.NetworkSnapshot{
		Interfaces: []metrics.NetworkInterface{{
			Name: "eth9",
			Rates: metrics.NetworkRates{
				ReceiveBytesPerSecond:  1_000_000,
				TransmitBytesPerSecond: 2_000_000,
				Valid:                  true,
			},
		}},
	}}
	rx := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	tx := config.Item{ID: "network", Interface: "eth9", Direction: "tx"}
	if got := formatMetric(rx, snap); got != "1.0 MB/s" {
		t.Fatalf("rx = %q, want 1.0 MB/s", got)
	}
	if got := formatMetric(tx, snap); got != "2.0 MB/s" {
		t.Fatalf("tx = %q, want 2.0 MB/s", got)
	}
}

// A meter needs a fraction. Rate sources have none, and the loader already
// rejects a meter on them, so this reports absent rather than guessing.
func TestOnlyFractionSourcesReportAFraction(t *testing.T) {
	t.Parallel()
	snap := fixtureSnapshot()

	if got, ok := metricFraction(config.Item{ID: "cpu"}, snap); !ok || got != 0.42 {
		t.Fatalf("cpu fraction = %v/%v, want 0.42/true", got, ok)
	}
	item := config.Item{ID: "network", Interface: "eth9", Direction: "rx"}
	if _, ok := metricFraction(item, snap); ok {
		t.Fatal("a rate source reported a fraction")
	}
}

func TestEveryMetricIDMapsToASource(t *testing.T) {
	t.Parallel()
	want := map[string]services.Source{
		"cpu":        services.SourceCPU,
		"memory":     services.SourceMemory,
		"filesystem": services.SourceFilesystem,
		"block":      services.SourceBlock,
		"network":    services.SourceNetwork,
	}
	for id, src := range want {
		got, ok := metricSource(config.Item{ID: id})
		if !ok || got != src {
			t.Fatalf("%s mapped to %v/%v, want %v", id, got, ok, src)
		}
	}
	if _, ok := metricSource(config.Item{ID: "clock"}); ok {
		t.Fatal("a non-metric id mapped to a source")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/shell/ -run Metric -v`
Expected: FAIL to compile — `formatMetric`, `metricFraction` and `metricSource` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/shell/metricwidget.go`:

```go
package shell

import (
	"fmt"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// metricSource maps a widget id to the telemetry source it leases.
func metricSource(item config.Item) (services.Source, bool) {
	switch item.ID {
	case "cpu":
		return services.SourceCPU, true
	case "memory":
		return services.SourceMemory, true
	case "filesystem":
		return services.SourceFilesystem, true
	case "block":
		return services.SourceBlock, true
	case "network":
		return services.SourceNetwork, true
	}
	return 0, false
}

// metricFraction reports a fraction between zero and one for the sources that
// have one. Rate sources have no full scale and report absent; the loader
// already rejects a meter on them, so nothing asks twice.
func metricFraction(item config.Item, snap services.Snapshot) (float64, bool) {
	switch item.ID {
	case "cpu":
		if snap.CPU == nil || !snap.CPU.Usage.Valid {
			return 0, false
		}
		return snap.CPU.Usage.Fraction, true
	case "memory":
		if snap.Memory == nil {
			return 0, false
		}
		return capacityFraction(snap.Memory.Memory)
	case "filesystem":
		if snap.Filesystem == nil {
			return 0, false
		}
		for _, fs := range snap.Filesystem.Filesystems {
			if fs.MountPoint == item.Path {
				return capacityFraction(fs.Capacity)
			}
		}
	}
	return 0, false
}

// capacityFraction is used over total. A zero total means the capacity was not
// read, which is absent rather than zero per cent.
func capacityFraction(c metrics.Capacity) (float64, bool) {
	if c.TotalBytes == 0 {
		return 0, false
	}
	return float64(c.UsedBytes) / float64(c.TotalBytes), true
}

// metricRate reports bytes per second for the rate sources.
func metricRate(item config.Item, snap services.Snapshot) (float64, bool) {
	switch item.ID {
	case "block":
		if snap.Block == nil {
			return 0, false
		}
		for _, d := range snap.Block.Devices {
			if d.Name != item.Device {
				continue
			}
			if !d.Rates.Valid {
				return 0, false
			}
			if item.Direction == "write" {
				return d.Rates.WriteBytesPerSecond, true
			}
			return d.Rates.ReadBytesPerSecond, true
		}
	case "network":
		if snap.Network == nil {
			return 0, false
		}
		for _, i := range snap.Network.Interfaces {
			if i.Name != item.Interface {
				continue
			}
			if !i.Rates.Valid {
				return 0, false
			}
			if item.Direction == "tx" {
				return i.Rates.TransmitBytesPerSecond, true
			}
			return i.Rates.ReceiveBytesPerSecond, true
		}
	}
	return 0, false
}

// formatMetric renders one metric. An absent source, an absent subject and an
// invalid value all render the same placeholder, so a reader never has to tell
// them apart and no widget invents a zero.
func formatMetric(item config.Item, snap services.Snapshot) string {
	if fraction, ok := metricFraction(item, snap); ok {
		return fmt.Sprintf("%.0f%%", fraction*100)
	}
	if rate, ok := metricRate(item, snap); ok {
		return formatRate(rate)
	}
	return noWorkspace
}

// rateUnits are decimal, following the reference shell: a network rate is
// conventionally quoted in megabytes, not mebibytes.
var rateUnits = []struct {
	suffix string
	scale  float64
}{
	{"GB/s", 1e9},
	{"MB/s", 1e6},
	{"kB/s", 1e3},
}

func formatRate(bytesPerSecond float64) string {
	for _, u := range rateUnits {
		if bytesPerSecond >= u.scale {
			return fmt.Sprintf("%.1f %s", bytesPerSecond/u.scale, u.suffix)
		}
	}
	return fmt.Sprintf("%.0f B/s", bytesPerSecond)
}
```

In `internal/shell/widget.go`, extend the view and build metric widgets. `barView` gains two fields:

```go
type barView struct {
	// Now is zero until the first clock tick.
	Now       time.Time
	Workspace string
	Title     string
	// Metrics is the newest sampling pass. Its nil fields mean unleased or
	// failed; either renders the placeholder.
	Metrics services.Snapshot
	// History carries each leased source's samples for a graph to plot.
	History map[services.Source][]float64
}
```

Add the metric branch to `buildWidgets`, after the existing cases:

```go
		case "cpu", "memory", "filesystem", "block", "network":
			out = append(out, buildMetricWidget(item))
```

and the builder, in `internal/shell/metricwidget.go`:

```go
// buildMetricWidget makes one metric instance. Display mode is fixed at build
// time, so the format function never branches on configuration at paint time.
func buildMetricWidget(item config.Item) textWidget {
	switch item.Display {
	case "meter":
		node := &ui.Node{Kind: ui.KindMeter, Width: metricMeterWidth}
		return textWidget{
			node: node,
			format: func(v barView) string {
				// A meter carries its value on the node, not as text. The
				// fraction is written here because apply is the one pass that
				// sees each view.
				if fraction, ok := metricFraction(item, v.Metrics); ok {
					node.Value = fraction
				} else {
					node.Value = 0
				}
				return ""
			},
		}
	case "graph":
		node := &ui.Node{Kind: ui.KindGraph, Width: metricGraphWidth}
		src, _ := metricSource(item)
		return textWidget{
			node: node,
			format: func(v barView) string {
				node.Values = normalise(v.History[src])
				return ""
			},
		}
	default:
		return textWidget{
			node:   &ui.Node{Kind: ui.KindText, Tabular: true, MinWidth: metricTextFloor},
			format: func(v barView) string { return formatMetric(item, v.Metrics) },
		}
	}
}

const (
	// metricMeterWidth and metricGraphWidth are the reserved widths for the
	// two non-text display modes, in logical pixels.
	metricMeterWidth = 48
	metricGraphWidth = 48
	// metricTextFloor reserves the width of a three-digit percentage so a bar
	// does not reflow as a value crosses from 9% to 100%.
	metricTextFloor = 34
)

// normalise scales samples against the window maximum, which is what lets a
// rate with no natural full scale drive a graph. An all-zero window plots flat
// rather than dividing by zero.
func normalise(values []float64) []float64 {
	if len(values) == 0 {
		return nil
	}
	maximum := 0.0
	for _, v := range values {
		if v > maximum {
			maximum = v
		}
	}
	out := make([]float64, len(values))
	if maximum <= 0 {
		return out
	}
	for i, v := range values {
		out[i] = v / maximum
	}
	return out
}
```

Add `"github.com/Nomadcxx/sysc-shell/internal/ui"` and `"github.com/Nomadcxx/sysc-shell/internal/services"`
to `metricwidget.go`'s imports, and `"github.com/Nomadcxx/sysc-shell/internal/services"` to
`widget.go`'s.

A meter and a graph return empty text, so `Bar.apply`'s change detection sees no change from them. Task 9
handles that: a bar carrying either is marked dirty whenever its snapshot changes.

**Note what `formatMetric` cannot do.** It takes an item and a snapshot, and no interval. The shell has
no elapsed time to divide by even accidentally, so the design's requirement that rates come from the
library's monotonic comparison is enforced by the signature rather than by a test asserting an absence.
`TestRateSourcesFormatInDecimalUnits` reads `Rates.ReadBytesPerSecond` straight through, which is the
positive half of the same evidence. This is the defect DMS carries — `timeDiff = updateInterval / 1000`
— made unreachable here rather than merely avoided.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/shell/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go build ./... && gofmt -l internal/shell && go vet ./internal/shell/
git add internal/shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(shell): render metric widgets from a sampling pass"
```

---

### Task 9: Registry ownership

**Files:**
- Modify: `internal/shell/registry.go`
- Test: `internal/shell/registry_test.go:` append

**Interfaces:**
- Consumes: `services.Metrics` (Tasks 2–4), `metricSource` and `buildMetricWidget` (Task 8).
- Produces: `(*Registry).Metrics() *services.Metrics`; `(*Registry).UpdateMetrics(services.Snapshot)
  []uint32`. Task 10 wires them.

- [ ] **Step 1: Write the failing test**

Append to `internal/shell/registry_test.go`:

```go
// metricConfig is a bar carrying one CPU text widget.
func metricConfig() config.Config {
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{
		ID: "cpu", Display: "text", Interval: 2 * time.Second,
	}}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil
	return cfg
}

func TestAMetricWidgetLeasesItsSource(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if !reg.Metrics().Leased(services.SourceCPU) {
		t.Fatal("a CPU widget did not lease the CPU source")
	}
	for _, src := range []services.Source{
		services.SourceMemory, services.SourceFilesystem,
		services.SourceBlock, services.SourceNetwork,
	} {
		if reg.Metrics().Leased(src) {
			t.Fatalf("source %v leased with no widget", src)
		}
	}
}

func TestTwoBarsShareOneMetricsServiceAndOneSample(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("metrics starts = %d, want 1 shared start for two bars", got)
	}

	changed := reg.UpdateMetrics(services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	})
	if len(changed) != 2 {
		t.Fatalf("one sample changed %d bars, want 2", len(changed))
	}
}

func TestDroppingTheLastMetricBarStopsTheService(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	reg.DropHost(1)
	if !reg.Metrics().Running() {
		t.Fatal("dropping one of two bars stopped the metrics service")
	}
	reg.DropHost(2)
	if reg.Metrics().Running() {
		t.Fatal("dropping the last bar left the metrics service running")
	}
}

// An unchanged sample must not repaint: no source change, no submitted frame.
func TestAnUnchangedSampleChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 1 {
		t.Fatalf("first sample changed %v, want global 1", changed)
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 0 {
		t.Fatalf("an identical sample changed %v", changed)
	}
}

// A configuration naming no metric leaves the service stopped, so a clock-only
// bar costs no sampling goroutine.
func TestAConfigWithNoMetricLeavesTheServiceStopped(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if reg.Metrics().Running() {
		t.Fatal("a configuration with no metric started the sampling service")
	}
}

// A graph and a meter carry no text, so text comparison cannot detect their
// change. A bar carrying one must repaint whenever its snapshot changes.
func TestABarWithAGraphRepaintsOnEverySample(t *testing.T) {
	t.Parallel()
	cfg := metricConfig()
	cfg.Bar.Left = []config.Item{{
		ID: "cpu", Display: "graph", Interval: 2 * time.Second,
	}}

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	snap := services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 1 {
		t.Fatalf("first sample changed %v, want global 1", changed)
	}
	if changed := reg.UpdateMetrics(snap); len(changed) != 1 {
		t.Fatalf("a graph bar changed %v, want it repainted on every sample", changed)
	}
}
```

Add `metrics "github.com/Nomadcxx/sysc-metrics"` and
`"github.com/Nomadcxx/sysc-shell/internal/services"` to that file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/shell/ -run 'Metric|Sample|Graph' -v`
Expected: FAIL to compile — `Registry.Metrics` and `Registry.UpdateMetrics` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/shell/registry.go`, add the service to the struct and constructor:

```go
	clock   *services.Clock
	metrics *services.Metrics
```

```go
		clock:         services.NewClock(),
		metrics:       services.NewMetrics(),
```

Add the accessor and the update path:

```go
// Metrics is the shared sampling service. The process pumps its updates into
// UpdateMetrics.
func (r *Registry) Metrics() *services.Metrics { return r.metrics }

// UpdateMetrics applies a sampling pass to every bar and reports the globals
// whose rendering actually changed.
func (r *Registry) UpdateMetrics(snap services.Snapshot) []uint32 {
	r.mu.Lock()
	r.sample = snap
	var changed []uint32
	for global, bar := range r.bars {
		// A graph and a meter carry no text, so text comparison cannot see
		// their change. A bar holding either repaints on every sample.
		if bar.apply(r.viewLocked(bar.connector())) || bar.hasPlottedWidget() {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()

	r.publish(changed)
	return changed
}
```

Add `sample services.Snapshot` to the struct, and extend `viewLocked` to carry it plus the history each
leased source holds:

```go
func (r *Registry) viewLocked(connector string) barView {
	state, ok := r.outputs[connector]
	if !ok {
		state = outputState{Workspace: noWorkspace}
	}
	return barView{
		Now:       r.now,
		Workspace: state.Workspace,
		Title:     state.Title,
		Metrics:   r.sample,
		History:   r.historyLocked(),
	}
}

// historyLocked collects the samples every leased source holds. Only leased
// sources are asked, so an unused ring is never copied.
func (r *Registry) historyLocked() map[services.Source][]float64 {
	out := make(map[services.Source][]float64, 5)
	for _, src := range []services.Source{
		services.SourceCPU, services.SourceMemory, services.SourceFilesystem,
		services.SourceBlock, services.SourceNetwork,
	} {
		if r.metrics.Leased(src) {
			out[src] = r.metrics.History(src)
		}
	}
	return out
}
```

In `buildBar`, acquire a metric lease per metric item beside the clock leases:

```go
	for _, item := range allItems(policy) {
		src, ok := metricSource(item)
		if !ok {
			continue
		}
		lease, err := r.metrics.Acquire(src, item.Interval)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}
```

and add the helper beside it:

```go
// allItems is every configured item across the three sections.
func allItems(policy config.Bar) []config.Item {
	out := make([]config.Item, 0, len(policy.Left)+len(policy.Center)+len(policy.Right))
	out = append(out, policy.Left...)
	out = append(out, policy.Center...)
	return append(out, policy.Right...)
}
```

Because a metric lease is a `*services.Lease` exactly as a clock lease is, `DropHost`, `Close`,
`PrepareConfig` and `Rollback` need no change: they already release every lease a bar holds, and
`Lease.Release` dispatches to the owning service.

In `internal/shell/bar.go`, add the predicate the update path needs:

```go
// hasPlottedWidget reports whether any widget renders through the node rather
// than through text. A meter and a graph carry their value on the node, so
// text comparison cannot detect their change.
func (b *Bar) hasPlottedWidget() bool {
	for _, section := range b.widgets() {
		for _, w := range section {
			if w.node.Kind == ui.KindMeter || w.node.Kind == ui.KindGraph {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go build ./... && go test -race ./internal/shell/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/shell && go vet ./internal/shell/
git add internal/shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(shell): lease telemetry sources per bar"
```

---

### Task 10: Process wiring

**Files:**
- Modify: `cmd/sysc-shell/main.go`

**Interfaces:**
- Consumes: `Registry.Metrics`, `Registry.UpdateMetrics` from Task 9.
- Produces: nothing.

- [ ] **Step 1: Write the implementation**

There is no new observable behaviour to test at this layer: `run` opens Wayland, which no unit test can
do. The regression net is the existing `TestRunRequiresNiriSocket`, which proves the environment is
still validated before anything else happens.

In `cmd/sysc-shell/main.go`, add a second pump beside the clock pump:

```go
	// The sampling service publishes on its own goroutine; this pump turns
	// each pass into per-bar text and hands the changed outputs to the Wayland
	// owner. One pass serves every bar.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case snapshot := <-registry.Metrics().Updates():
				registry.UpdateMetrics(snapshot)
			}
		}
	}()
```

`registry.Close()` already runs on the deferred path from Tranche 3A, and it now closes the metrics
service too, so no further shutdown wiring is needed.

- [ ] **Step 2: Run the regression test**

Run: `go test ./cmd/sysc-shell/ -v`
Expected: PASS.

- [ ] **Step 3: Build and run the full suite**

```bash
go build -o /tmp/sysc-shell-tranche3b ./cmd/sysc-shell
go test -race -count=1 ./... 2>&1 | tail -20
```

Expected: a successful build and every package passing.

- [ ] **Step 4: Commit**

```bash
gofmt -l cmd
git add cmd/sysc-shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(shell): pump sampling passes into the bar registry"
```

---

### Task 11: Cross-cutting evidence

The behaviours from the design's evidence table that no single earlier task proves end to end.

**Files:**
- Create: `internal/shell/tranche3b_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–10.
- Produces: nothing.

- [ ] **Step 1: Write the test**

Create `internal/shell/tranche3b_test.go`:

```go
package shell

import (
	"runtime"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// mixedConfig carries a text metric, a meter and a graph on one bar, which is
// the arrangement most likely to expose a change-detection defect.
func mixedConfig() config.Config {
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{
		{ID: "cpu", Display: "text", Interval: time.Second},
		{ID: "memory", Display: "meter", Interval: time.Second},
		{ID: "network", Display: "graph", Interval: time.Second,
			Interface: "eth9", Direction: "rx"},
	}
	cfg.Bar.Center = nil
	cfg.Bar.Right = nil
	return cfg
}

func TestAMixedBarLeasesEverySourceItUses(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(mixedConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	for _, src := range []services.Source{
		services.SourceCPU, services.SourceMemory, services.SourceNetwork,
	} {
		if !reg.Metrics().Leased(src) {
			t.Fatalf("source %v is used by a widget but not leased", src)
		}
	}
	for _, src := range []services.Source{services.SourceFilesystem, services.SourceBlock} {
		if reg.Metrics().Leased(src) {
			t.Fatalf("source %v leased with no widget", src)
		}
	}
}

// A partial failure must isolate: one unreadable source renders the
// placeholder while its neighbours keep rendering.
func TestOneFailingSourceDoesNotSuppressAnother(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{
		{ID: "cpu", Display: "text", Interval: time.Second},
		{ID: "memory", Display: "text", Interval: time.Second},
	}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil

	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	// CPU present, memory absent — as when one collector fails.
	reg.UpdateMetrics(services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.42, Valid: true}},
	})

	bar := reg.bars[1]
	if got := bar.left[0].node.Text; got != "42%" {
		t.Fatalf("healthy source rendered %q, want 42%%", got)
	}
	if got := bar.left[1].node.Text; got != noWorkspace {
		t.Fatalf("failed source rendered %q, want the placeholder", got)
	}
}

// A meter's fraction reaches its node, which is how a meter renders at all.
func TestAMeterCarriesItsFractionOnTheNode(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(mixedConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reg.UpdateMetrics(services.Snapshot{
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 1000, UsedBytes: 250},
		},
	})

	if got := reg.bars[1].left[1].node.Value; got != 0.25 {
		t.Fatalf("meter node value = %v, want 0.25", got)
	}
}

// A graph normalises against its own window, which is what lets a rate with no
// natural full scale be plotted at all.
func TestAGraphNormalisesAgainstItsWindow(t *testing.T) {
	t.Parallel()
	got := normalise([]float64{1_000_000, 2_000_000, 4_000_000})

	if len(got) != 3 {
		t.Fatalf("normalised %d values, want 3", len(got))
	}
	if got[2] != 1 {
		t.Fatalf("window maximum normalised to %v, want 1", got[2])
	}
	if got[0] != 0.25 || got[1] != 0.5 {
		t.Fatalf("normalised = %v, want [0.25 0.5 1]", got)
	}
}

// An all-zero window must plot flat rather than divide by zero.
func TestAnAllZeroWindowNormalisesFlat(t *testing.T) {
	t.Parallel()
	for _, v := range normalise([]float64{0, 0, 0}) {
		if v != 0 {
			t.Fatalf("all-zero window normalised to %v, want flat zero", v)
		}
	}
}

// An accepted reload must not restart a sampling service still in use, and a
// changed interval must re-arm rather than cycle the goroutine.
func TestAnAcceptedReloadDoesNotRestartTheSamplingService(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("starts = %d before reload, want 1", got)
	}

	candidate := metricConfig()
	candidate.Bar.Left[0].Interval = time.Second
	prepared, err := reg.PrepareConfig(candidate, identities(map[uint32]string{1: "DP-9"}))
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	if got := reg.Metrics().Starts(); got != 1 {
		t.Fatalf("starts = %d after reload, want 1; the service restarted", got)
	}
	if !reg.Metrics().Running() {
		t.Fatal("the sampling service stopped across a reload that still uses it")
	}
}

// A rejected reload must leave the sampling service exactly as it was.
func TestARejectedReloadLeavesTheSamplingServiceUnchanged(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(metricConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})
	before := reg.Metrics().Starts()

	broken := metricConfig()
	broken.Bar.Height = 4
	broken.Bar.Gap = 4
	if _, err := reg.PrepareConfig(broken, identities(map[uint32]string{1: "DP-9"})); err == nil {
		t.Fatal("an unbuildable candidate was prepared")
	}

	if got := reg.Metrics().Starts(); got != before {
		t.Fatalf("starts = %d, want the unchanged %d", got, before)
	}
	if !reg.Metrics().Running() {
		t.Fatal("a rejected reload stopped the sampling service")
	}
}

// Every goroutine the tranche starts must be gone once the registry closes.
func TestClosingTheRegistryStopsTheSamplingGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	reg := NewRegistry(mixedConfig())
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})
	reg.Close()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutines = %d after Close, want at most the starting %d", got, before)
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test -race ./internal/shell/ -v`
Expected: PASS. Any failure is a real defect in an earlier task; fix it there rather than weakening the
test.

- [ ] **Step 3: Run the full suite with the race detector**

```bash
go test -race -count=1 ./... 2>&1 | tail -20
```

Expected: `ok` for every package, no race report.

- [ ] **Step 4: Commit**

```bash
gofmt -l internal && go vet ./...
git add internal/shell/tranche3b_test.go
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "test(shell): cover leasing, isolation and reload for metrics"
```

---

### Task 12: Automated gate and live matrix

**Files:**
- Modify: `tests/integration/README.md`

- [ ] **Step 1: Run the full automated gate**

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-tranche3b ./cmd/sysc-shell
gofmt -l .
git diff --check
```

Expected: `go mod tidy -diff` reports no difference, every package passes, vet is silent, the build
succeeds, and the last two print nothing.

- [ ] **Step 2: Confirm the dependency is tagged and unreplaced**

```bash
grep -n "sysc-metrics" go.mod
grep -n "replace" go.mod || echo "no replace directive, correct"
```

Expected: `sysc-metrics` at a semantic version tag, and no `replace` directive. A `replace` here is a
recorded stop condition — remove it and return to the owner.

- [ ] **Step 3: Record the live matrix**

Append to `tests/integration/README.md`:

```markdown
## Tranche 3B: core metrics

Run after Tranche 3A's matrix. Record connector, device, interface and mount
names and all measurements outside this repository.

Build and start:

    go build -o /tmp/sysc-shell-tranche3b ./cmd/sysc-shell
    /tmp/sysc-shell-tranche3b

Matrix:

1. One output, then at least two. Each output renders the same metric
   independently and updates together.
2. A text metric, a meter and a graph on one bar simultaneously, all updating.
3. Load the machine and confirm the CPU value and graph track the load, and
   settle when it stops.
4. Fill and empty a filesystem and confirm the meter follows.
5. Down an interface, or unplug a device, and confirm its widget renders "-"
   while every other widget keeps updating and the shell keeps running.
6. Bring it back and confirm the widget recovers without a restart.
7. Confirm the first sample after start renders "-" for rate widgets for one
   interval only.
8. Reload adding and removing metric widgets, and changing an interval, and
   confirm the service does not restart and no widget stalls.
9. Write an invalid metric configuration and SIGHUP; confirm the previous
   widgets stay live and the error names its field path on stderr.
10. Confirm stderr carries exactly one line when a source starts failing and
    one when it recovers, not one per sample.

Baselines to record before setting any budget:

- idle CPU and wakeups over 60 minutes with a 2-second interval, against the
  Tranche 3A clock-only baseline;
- CPU during sampling and during a graph repaint;
- RSS after one hour with a graph running;
- submitted and skipped frame counts;
- allocations per sampling pass;
- binary size against the Tranche 3A binary.
```

- [ ] **Step 4: Run the live matrix**

Execute every numbered item on a real Niri session with at least two outputs. Record each outcome. Do
not claim any live behaviour from the automated gate alone.

- [ ] **Step 5: Commit and write the completion handover**

```bash
git add tests/integration/README.md
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "docs: record the tranche 3B live matrix"
```

Then write `docs/plans/2026-08-30-core-metrics-completion-handover.md` with commit hashes, fresh gate
output, live observations per matrix item, measured baselines, defects, and the next unblocked issue.

---

## Deviations to report

Stop and return to the owner rather than improvising if any of these occur:

- Task 0 finds no `sysc-metrics` tag, or a public API that differs from the design.
- Any task appears to need a second goroutine calling a sampler, one timer per output, an interface over
  `Clock` and `Metrics`, a `replace` directive, or a new dependency.
- A rate cannot be derived without recomputing it in the shell from a nominal interval.
- A metric failure appears to require stopping the shell.

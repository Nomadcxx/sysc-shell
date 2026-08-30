# Core Metrics Design — Milestone 3, Tranche 3B

Date: 2026-08-30
Status: Owner-approved in session. Not yet audited.
Branch: `milestone/core-metrics`
Worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/core-metrics`

Implements the tranche described in
[the Milestone 3 charter](2026-08-30-built-in-widget-foundation-execution-handover.md), which fixes
3B's scope. Builds directly on the widget, service and reload machinery
[Tranche 3A](2026-08-30-built-in-widget-foundation-design.md) shipped.

## Scope

Tranche 3B ships five metric widget types on every configured output, fed by one process-level sampling
owner:

- `cpu` — aggregate utilisation;
- `memory` — used fraction of RAM;
- `filesystem` — used fraction of one mount;
- `block` — read or write rate for one device;
- `network` — receive or transmit rate for one interface.

It ships the sampling service, per-source consumer counting, two UI primitives, and the configuration
and tests those widgets need. It ships no thermal, battery, GPU or process metrics, no daemon, no CLI,
no popout, no tooltip, and no icons.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | Separate item ids: `cpu`, `memory`, `filesystem`, `block`, `network`. | One `metric` id parameterised by a stat enum. Noctalia parameterises; DMS splits. Splitting lets each id validate only the options it accepts, so `path` on a CPU widget is impossible by construction. |
| D2 | `services.Metrics` is a second concrete service beside `Clock`. | Generalising `Clock` into a shared ticker. That is a single-implementation abstraction, a recorded stop condition, and it couples wall-clock boundary alignment to elapsed-time sampling. |
| D3 | Per-source leases; the service samples at the finest interval any live lease requires. | A fixed global interval, or one timer per widget instance. |
| D4 | Display mode per instance: `text`, `meter`, `graph`. | Text only. |
| D5 | The graph node ships in 3B. | The charter places it in 3D. Metrics are its only plausible consumer; designing it in 3D and retrofitting 3B would define it twice. **Recorded as a deliberate charter deviation.** |
| D6 | The service owns one history ring per source. | Per-widget history. Two bars showing the same source would keep duplicate rings that must not diverge. |
| D7 | An unavailable value renders `"-"`. | A stale or error node. Those belong to 3D; `"-"` matches 3A's fallback for an unreported connector. |
| D8 | `ui.Node.MinWidth` reserves a stable field for percentages. | Relying on tabular figures alone. Those align digits but do not fix `9%` and `100%` differing in digit count. |

## Prior art review

Reviewed on 2026-08-30 against local sources:

- Noctalia v5, `/home/nomadx/noctalia`, C++23, `src/shell/bar/widgets/sysmon_widget.h` and
  `src/system/system_monitor_service.h`.
- DankMaterialShell at `892b8ae`, `Modules/DankBar/Widgets/{CpuMonitor,RamMonitor,DiskUsage,
  NetworkMonitor}.qml` and `Services/DgopService.qml`.

### What confirmed this design

**Consumer-counted, per-source acquisition.** Both shells reached the shape 3A's clock already uses,
independently. DMS's `CpuMonitor.qml` calls `DgopService.addRef(["cpu"])` on creation and
`removeRef(["cpu"])` on destruction, against `moduleRefCounts[module]`; a module is polled only while
something references it. Noctalia's `SystemMonitorService` exposes `retainCpuTemp`/`releaseCpuTemp`,
`retainCpuCores`, `retainGpuVram` and `retainDiskPath(path)`/`releaseDiskPath(path)` against `int refs`,
driving one `samplingLoop()`. Two independent designs converging on `services.Clock`'s pattern is the
strongest evidence available that D2 and D3 are right.

**Demand-scaled cadence.** DMS sets `updateInterval: refCount > 0 ? 3000 : 30000`. The principle —
sampling rate follows live demand rather than a constant — is what D3 generalises to a finest-required
interval.

**A fixed history ring for graphs.** Noctalia's `kHistorySize = 120`, with `history(windowSize)` and
`diskHistory(path, windowSize)` on the service, not on the widget. D6 follows it, including the size.

**Display mode as a per-instance option.** Noctalia's `SysmonDisplayMode { Text, Graph, Gauge, None }`
is per widget instance and defaults to `Gauge`. D4 follows, minus `None`, which has no consumer.

### What changed this design

**A minimum width is required, not cosmetic.** DMS reserves the painted width of the literal `"100%"`
via `StyledTextMetrics` and takes `Math.max(cpuBaseline.width, paintedWidth)`, exposed as a
`minimumWidth` setting. Noctalia exposes `labelMinWidth`. Both solve the same defect: a percentage
crossing from one digit to three reflows its section every sample. 3A's `Tabular` flag aligns digits but
cannot fix a changing digit *count*, so D8 adds `MinWidth`. Without it, a CPU meter beside a clock would
shift the clock every few seconds — the exact defect 3A's tabular-figures work existed to prevent.

**Rate sources cannot drive a meter.** `cpu`, `memory` and `filesystem` yield a fraction that fills a
bar. `block` and `network` yield bytes per second with no full scale. A meter of "3.2 MB/s" has no
meaning, so `display: "meter"` is rejected on a rate source at load, naming its field path. A graph
remains valid because it normalises against the maximum in its own window.

### Anti-patterns observed

- **DMS derives rates from the nominal interval.** `DgopService.qml` computes
  `const timeDiff = updateInterval / 1000` and divides counter deltas by it. When a poll runs late —
  load, scheduling, resume from suspend — the reported rate is wrong in proportion to the lateness.
  `sysc-metrics` already compares two monotonic samples and treats counter resets, device replacement
  and suspend as discontinuities. The upstream library is more correct than the prior art here, and this
  design must not undo that by recomputing rates in the shell.
- **DMS polls by subprocess.** `dgopProcess.running = true` shells out to the `dgop` binary. The charter
  forbids an external conversion or collection process; `sysc-metrics` is a library call in-process.

### Deferred, with prior art noted

| Feature | Prior art |
|---|---|
| Per-core CPU display | Noctalia `retainCpuCores`; `CPUSnapshot.Cores` already carries it |
| CPU and GPU temperature | Noctalia `SysmonStat::CpuTemp`, `cpu_temp_sensor.h`; needs `sysc-metrics` M2 |
| Swap | `MemorySnapshot.Swap` exists; no charter mandate in 3B |
| Load average | `CPUSnapshot.Load1/5/15` exists; no charter mandate |
| Process list popout | DMS `toggleProcessList`; Milestone 4 |
| Tooltips on metric widgets | Both shells; Tranche 3D |

## Service lifetime

`internal/services.Metrics` is concrete. There is no service registry and no interface.

```go
type Source uint8

const (
	SourceCPU Source = iota
	SourceMemory
	SourceFilesystem
	SourceBlock
	SourceNetwork
)

func NewMetrics() *Metrics
func (m *Metrics) Acquire(src Source, interval time.Duration) (*Lease, error)
func (m *Metrics) Updates() <-chan Snapshot   // newest-wins, capacity 1; Snapshot is shell-side
func (m *Metrics) History(src Source) []float64
func (m *Metrics) Close()
func (m *Metrics) Running() bool
func (m *Metrics) Starts() int
```

`Snapshot` is this package's own aggregate, not a `sysc-metrics` type — the library has no bare
`Snapshot`, only `CPUSnapshot`, `MemorySnapshot` and the rest. One aggregate keeps every source in a
tick sharing a single collection instant:

```go
// Snapshot aggregates one collection pass. A nil field means that source is
// unleased, or failed this tick; either way its widgets render "-".
type Snapshot struct {
	CollectedAt time.Time
	CPU         *metrics.CPUSnapshot
	Memory      *metrics.MemorySnapshot
	Filesystem  *metrics.FilesystemSnapshot
	Block       *metrics.BlockSnapshot
	Network     *metrics.NetworkSnapshot
}
```

The lifetime rules are `Clock`'s, unchanged:

- the first lease starts the goroutine, the last stops it and waits for exit;
- the service samples at the finest interval any live lease requires, recomputed on acquire and release;
- a shorter interval signals a capacity-1 `rearm` channel so the running goroutine re-arms without
  restarting, so `Starts()` stays at 1 across a reload;
- cancellation stops the goroutine through its `select` on `ctx.Done()`.

The lease bookkeeping common to `Clock` and `Metrics` — the slice of leases, the finest-boundary scan,
start and stop transitions — factors into an unexported `leaseSet` struct in the same package. It is a
shared struct with methods, not an interface, so it introduces no abstraction over the two services.

**Only leased sources are sampled.** A configuration whose only metric widget is `cpu` never opens
`/proc/diskstats`. This is what makes per-source leasing worth its complexity rather than sampling
everything on one timer.

### Sampler ownership

`sysc-metrics` documents `CPUSampler`, `BlockSampler` and `NetworkSampler` as owned by one sequential
polling caller: each retains previous counters, and concurrent `Sample()` calls would corrupt that
state. The service's single goroutine owns all three for the process lifetime. No other goroutine calls
them, and none is constructed per output.

`ReadMemory`, `ReadFilesystems` and `ReadUptime` are stateless and could be called anywhere; they are
called from the same goroutine anyway, so every source in one snapshot shares one collection instant.

### Cadence

Each `Item` carries an `interval`, defaulting to 2 seconds and validated positive at load. The service
sleeps on a `time.Timer` to the next deadline, recomputed each iteration, matching `Clock.nextBoundary`
in structure but not in alignment: metrics have no wall-clock boundary to align to, so the deadline is
simply `now + finest`.

Rates are the library's, computed from measured elapsed time between two monotonic samples. The shell
never divides a counter delta by a nominal interval.

## Widgets and configuration

```json
{
  "bar": {
    "items": {
      "right": [
        {"id": "cpu", "display": "meter"},
        {"id": "memory"},
        {"id": "filesystem", "path": "/", "display": "text"},
        {"id": "network", "interface": "wlan0", "direction": "rx", "display": "graph"},
        {"id": "block", "device": "nvme0n1", "direction": "read"}
      ]
    }
  }
}
```

The vocabulary gains exactly these five ids. `clock`, `workspace` and `window-title` are unchanged.

```go
// Item gains, alongside 3A's Format, Boundary and MaxWidth:
Display   string        // "text", "meter" or "graph"
Interval  time.Duration // sampling interval; zero on non-metric items
Path      string        // filesystem only
Device    string        // block only
Interface string        // network only
Direction string        // block and network only
```

Validation, each failure naming its exact field path:

- `path` is required on `filesystem` and rejected elsewhere;
- `device` is required on `block`, `interface` required on `network`, each rejected elsewhere;
- `direction` accepts `read`/`write` on `block` and `rx`/`tx` on `network`, and is rejected elsewhere;
- `display` accepts `text`, `meter`, `graph` on a fraction source, and only `text` and `graph` on a rate
  source;
- `interval` must be positive.

A selector naming an absent mount, device or interface is **not** a load error. Devices are hot-plugged
and interfaces come and go, so the widget validates as well-formed and renders `"-"` until its subject
appears — the same treatment 3A gives a connector Niri has not reported.

### Projection

Each widget is a pure function from the snapshot to a string and a fraction, as 3A's widgets are pure
functions from `barView` to a string. `barView` gains the metrics snapshot and the history ring;
`Bar.apply` continues to write only changed text and report whether anything changed.

Formatting: fractions render as `"42%"`; rates render in SI decimal units, `"3.2 MB/s"`, following
Noctalia's `DecimalByteRateUnit`. Byte capacities are not rendered as absolute values in this tranche —
every fraction source shows a percentage — so no binary-versus-decimal capacity decision is needed yet.

## UI primitives

Two additions, each with a shipped consumer.

```go
// Node gains, for KindText:
MinWidth int // 0 means natural width

// and a new kind:
KindGraph // Values are pre-normalised to 0..1; painted right-aligned
```

`measureNode` returns `max(natural, MinWidth)` for a text node, so a percentage reserves a stable field.
`MinWidth` and `MaxWidth` compose: a node floors at `MinWidth` and clamps at `MaxWidth`. They cannot
conflict in this tranche, because `MinWidth` is derived by the widget rather than configured, and the
only node carrying `MaxWidth` is `window-title`, which sets no floor. 3B adds no `min-width` option;
should a later tranche add one, that is where a floor-above-cap check belongs.

`KindGraph` paints one filled column per sample into `wl_shm`, right-aligned so the newest sample sits at
the leading edge. It reuses the existing rectangle fill; there is no path rasteriser, no new dependency,
and no anti-aliasing. A graph with fewer samples than its width leaves the unfilled columns background.

`KindMeter` is unchanged and gains its first consumer since the Milestone 2 fixture was removed.

## Failure and unavailability

| Case | Behaviour |
|---|---|
| A collector returns an error | That source is unavailable for the tick; its widgets render `"-"`. Other sources are unaffected. |
| A snapshot carries partial `Issues` | Entities that read correctly still render; only the failing mount or device renders `"-"`. |
| A validity flag is false | Renders `"-"`. |
| The first rate sample | Always invalid — there is no previous counter — so a rate widget renders `"-"` for exactly one interval, as a clock renders `""` before its first tick. |
| A selector names something absent | Renders `"-"` until it appears. |

**A metrics failure never stops the shell.** A Niri stream failure cancels the process context because
workspace state is load-bearing; an unreadable `/proc/diskstats` is not a reason to tear down every bar.
The tranche degrades to `"-"` and keeps running.

Logging is edge-triggered: one line when a source begins failing, one when it recovers. A per-tick log
at the default interval would emit roughly 1,800 lines an hour for one permanently absent device.

## Invalidation and redraw

Unchanged from 3A. A sample publishes to the newest-wins channel; a pump goroutine in `main.go` calls
`Registry.UpdateMetrics`, which applies the view to every bar and returns the globals whose text
actually changed, publishing exactly those on the invalidation transport with a blocking send.

No source change means no redraw: two identical snapshots mark nothing dirty on the second. A graph is
the one widget whose value changes almost every tick by nature, so a bar carrying one will legitimately
repaint at its interval.

## Files

New:

- `internal/services/metrics.go`, `internal/services/metrics_test.go`
- `internal/services/leases.go` — the `leaseSet` helper shared with `clock.go`
- `internal/shell/metricwidget.go`, `internal/shell/metricwidget_test.go`

Changed:

- `internal/services/clock.go` — adopt `leaseSet`
- `internal/config/config.go`, `load.go` — five ids, their options, validation
- `internal/shell/widget.go` — metric widgets in `buildWidgets`; `barView` carries the snapshot
- `internal/shell/registry.go` — metric leases, `UpdateMetrics`
- `internal/ui/tree.go`, `layout.go` — `MinWidth`, `KindGraph`
- `internal/render/paint.go` — paint `KindGraph`
- `cmd/sysc-shell/main.go` — metrics pump goroutine
- `go.mod`, `go.sum` — `sysc-metrics` at the tag `sysc-7` produces

## Automated evidence

| Required behaviour | Check |
|---|---|
| Two bars share one service and one sample | Two leases yield one goroutine; one update changes both bars |
| Only leased sources are sampled | A CPU-only configuration never calls the block sampler |
| The last consumer stops the goroutine | `DropHost` leaves `Running()` false; goroutine count returns to baseline |
| An accepted reload does not restart a live service | `Starts()` is still 1, including across an interval change |
| A rejected reload alters nothing | Lease count, bar set and visible text unchanged |
| Samplers stay sequentially owned | `-race` with concurrent bars; one goroutine holds all three samplers |
| Rates use measured elapsed time | A delayed tick yields the correct rate, not `delta/interval` |
| The first sample has no rate | Rate widgets render `"-"` for exactly one interval |
| A partial failure isolates | One bad mount renders `"-"`; its neighbour still renders |
| A meter on a rate source is rejected | Load fails naming `bar.items.<section>[N].display` |
| A graph auto-scales to its window | Values normalise against the ring maximum |
| Percentage width is stable | `MinWidth` holds the field across 9% to 100% |
| No source change produces no redraw | Two identical snapshots mark nothing dirty on the second |
| Cancellation stops every goroutine | `go test -race`, goroutine count before and after |

At each integration checkpoint:

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build -o /tmp/sysc-shell-tranche3b ./cmd/sysc-shell
gofmt -l .
git diff --check
```

These do not demonstrate live behaviour and no live claim is made from them.

## Live gate

Recorded in `tests/integration/README.md` when the plan lands. Connector names, device names, interface
names and measurements stay out of Git.

- one output, then at least two, each rendering the same metric independently;
- a meter, a graph and a text metric on one bar simultaneously;
- unplugging a device or downing an interface renders `"-"` without stopping the shell;
- a filesystem that fills and empties moves the meter;
- reload adding and removing metric widgets, and changing an interval, without restarting the service;
- idle CPU and wakeups over 60 minutes with a 2-second metric interval, against 3A's clock-only baseline.

## Dependencies and assumptions

1. **`sysc-metrics` carries a release tag.** `sysc-7` must close before this plan can execute. The
   charter forbids an untagged module and a local `replace` directive; 3A recorded both as stop
   conditions. The design is written against the merged API at `d821afe`, whose public surface is
   `CPUSampler`, `BlockSampler`, `NetworkSampler`, `ReadMemory`, `ReadFilesystems`, `ReadUptime` and the
   value types in `metrics.go`. A tag that changes that surface invalidates the widget projections here
   and nothing else.
2. **3A's service, widget and reload machinery is on `main`.** Merged at `39a7760`.
3. **No thermal, battery, GPU or process collection.** Those are `sysc-metrics` M2 and Tranche 3C.

## Stop conditions

Return to the owner rather than improvising if implementation requires:

- a second goroutine calling a `sysc-metrics` sampler;
- one sampler, timer or polling goroutine per output;
- an interface over `Clock` and `Metrics`, a service registry, or an injection container;
- a local `replace` directive or an untagged `sysc-metrics` import;
- recomputing rates in the shell from a nominal interval;
- an external process or CGO for collection;
- a new dependency beyond `sysc-metrics`;
- thermal, battery, GPU or process metrics, which belong to 3C and to `sysc-metrics` M2.

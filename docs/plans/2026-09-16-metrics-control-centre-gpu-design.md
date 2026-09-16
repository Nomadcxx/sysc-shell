# Metrics, Control Centre, and GPU design

Date: 2026-09-16. Parent commission: `sysc-309`.

## Goal

Put four truthful radial gauges in the Control Centre Home System section and
make the bar, standalone System Monitor, and Control Centre consume one GPU
snapshot from the existing metrics service.

## Existing seams

- `internal/services/metrics.go` owns selector leases, one sampling goroutine,
  `services.Snapshot`, `Snapshot.Fraction`, thermal data, and `SourceGPU`.
- `internal/shell/panelhost.go` leases CPU, memory, battery, and clock for the
  Control Centre. `monitorSelectors` already includes CPU, memory, and GPU for
  the standalone monitor.
- `internal/shell/controlcenter_pages.go:ccHome` currently constructs only CPU
  and memory gauges. `internal/shell/popout_monitor.go` already projects GPU
  cards and GPU identity from the same snapshot.
- `internal/render/iconfont.go` owns the compact CPU, memory, and GPU gauge
  glyphs. `ui.KindRadialGauge` owns the ring, clamping, unavailable state, and
  centred icon/value paint.

## Decisions

### D1. The Home gauge set has four fixed selectors

The Home System section uses this order:

1. CPU usage: `{Source: SourceCPU}`
2. Memory usage: `{Source: SourceMemory}`
3. CPU temperature: `{Source: SourceCPU, Subject: "temperature"}`
4. GPU usage: `{Source: SourceGPU}` or the selected GPU subject

The CPU temperature fraction remains the existing `Celsius/100` projection;
the visible value carries degrees Celsius. CPU usage, memory usage, and GPU
usage carry percentages. The ring and its value stay in `KindRadialGauge`.

### D2. One lease set serves every consumer

The Control Centre acquires the four selectors above alongside its existing
battery and clock leases. The CPU temperature selector does not start a
second CPU read: `SourceLeased` causes one collection pass to read CPU and
thermal data. The GPU selector uses the same `services.Metrics` instance as
the bar and standalone monitor. Panel close releases all four leases with the
existing host lifecycle.

### D3. GPU choice is deterministic and explicit

The shared projection chooses one GPU before asking `Snapshot.Fraction` for a
value. With multiple devices it sorts by non-empty PCI ID, then by name, and
selects the first. A single device may use the empty subject. The helper keeps
the selected subject in the monitor and Home projections, so both consumers
show the same device. An invalid or missing usage remains unavailable. The
helper never substitutes zero.

If a multi-GPU snapshot cannot distinguish devices, the shell reports the GPU
slot unavailable and records the upstream fixture needed to repair the
identity contract. It does not choose a random list position.

### D4. Home keeps its existing geometry and semantics

`ccResourceGroup` becomes the one construction site for all four slots. It
uses the current gauge size, density-derived card layout, `GaugeIconName`, and
the temperature value-text path already used by the metric widget. Labels and
tooltips identify the exact source. A valid zero remains a visible zero; nil,
invalid, unleased, and failed readings use the existing unavailable state.

### D5. Upstream defects stay upstream

The shell qualifies the pinned `github.com/Nomadcxx/sysc-metrics@v0.4.0`
reader before changing the module pin. If the reader cannot provide a valid
GPU value on hardware that has one, `sysc-metrics` owns the fix and release.
Shell work covers only leasing, deterministic selection, projection, and
rendering defects.

## Data flow

```text
sysc-metrics ReadGPU/ReadThermal
        -> services.Metrics one Snapshot
        -> Registry.sample
        -> bar metric widgets
        -> System Monitor GPU card
        -> Control Centre four gauge slots
```

`UpdateMetrics` stores the immutable snapshot before rebuilding any retained
tree. No collector, filesystem read, or GPU command runs while `Registry.mu`
is held.

## Focused proof

The executable plan will add or amend the smallest tests at these seams:

- `internal/shell/controlcenter_test.go`: four Home gauges, exact labels and
  values, valid zeros, and four unavailable states.
- `internal/shell/popout_monitor_test.go` and a pure selector test: one
  selected GPU reaches both projections and multi-GPU order is stable.
- `internal/shell/registry_test.go`: Control Centre acquisition includes CPU,
  memory, temperature, and GPU selectors and releases them on close.
- `internal/services/metrics_test.go`: a GPU selector preserves valid versus
  unavailable usage and does not turn a failed read into zero.

## Live gate

On the receiving Niri session, use only `DP-1` at 3440×1440 and scale 1.0.
Open the Control Centre Home and System Monitor, capture both surfaces, and
record the exact `sysc-metrics` reader and result. If the machine has no GPU,
record the unavailable slot. If it has one, record the real value and device
identity. No laptop or second-output result belongs in this gate.

## Boundary

This design does not add a metrics service, GPU-specific shell reader, graph
to Home, or a new gauge primitive. It does not change the standalone monitor's
card roster beyond making its already-leased GPU path visible and diagnosable.

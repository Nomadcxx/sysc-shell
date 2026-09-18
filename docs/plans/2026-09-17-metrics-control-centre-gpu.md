> **For implementation:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

# Metrics, Control Centre, and GPU execution plan

**Goal:** Ship four truthful Home System gauges and make the bar, standalone
System Monitor, and Control Centre consume one deterministic GPU snapshot and
one exact-device history from the existing metrics service.

**Scope:** This plan executes the approved
`docs/plans/2026-09-16-metrics-control-centre-gpu-design.md` and the owner-approved
radial package in
`docs/plans/assets/2026-09-17-metrics-control-centre-radial-design/`. It
reconciles the provisional shell diff already present in the handover; it does
not start a second shell-polish pass, add a renderer, or close the M10 parent.

## Qualification result that governs the implementation

The lease/history seam was traced before this plan was written:

- `internal/services/metrics.go:Metrics.Acquire` keys leases and rings by the
  complete `services.Selector`.
- `Metrics.collect` calls one service-owned
  `github.com/Nomadcxx/sysc-metrics@v0.5.1` `GPUSampler.Sample` per collection
  tick whenever any `SourceGPU` selector is leased. The sampler is constructed
  and closed by the single metrics goroutine; the upstream package owns GPU
  counter collection, not shell history or selector leases.
- `Metrics.record` currently records a wildcard GPU lease by asking
  `Snapshot.Value({Source: SourceGPU})`. That is the first list entry, not a
  stable selected device. A PCI-qualified shell projection therefore cannot
  safely use that ring.
- An exact PCI selector cannot be acquired reliably before the first GPU
  snapshot. Replacing the discovery lease on every selection change would also
  make device disappearance stop discovery unless another wildcard lease were
  kept alive.

The selected implementation contract is therefore option 3 from the handover:
keep the existing source-level GPU discovery lease and add a service-owned,
immutable per-device history projection. One `GPUSampler` snapshot feeds all
qualified PCI rings; the shell chooses the device and looks up the exact
`services.Selector{Source: SourceGPU, Subject: pciID}`. The source lease remains
owned by the existing bar and `PanelHost.leases` lifecycles.

The shell pins the audited `github.com/Nomadcxx/sysc-metrics@v0.5.1` release.
`v0.5.0` is explicitly excluded: its audit found that the PMU close-on-exec
flag was passed in the wrong ABI field. Do not add a `replace`, moving branch,
shell-side `nvidia-smi`, or wildcard graph fallback.

## Invariants

- Home has exactly four fixed gauges, in this order: CPU usage, memory usage,
  CPU temperature, GPU usage.
- CPU temperature uses `{Source: SourceCPU, Subject: "temperature"}` and the
  existing `Celsius/100` ring contract. Its visible value is Celsius.
- CPU, memory, and GPU usage are percentages. A valid zero is rendered as a
  visible zero. Nil, invalid, unleased, failed, or ambiguous state is absent,
  never an invented zero.
- GPU selection sorts a copy by non-empty PCI ID and then name. A single GPU
  may have an empty subject. Multiple devices without a distinguishing
  identity are unavailable.
- Home, System Monitor, and the bar use the same selected selector and the
  same immutable `services.Snapshot`. No shell code calls a GPU reader.
- The GPU history contains only valid samples for the qualified device. An
  invalid sample is skipped, not recorded as zero. A valid device list that
  drops a device discards that device's old ring; a collector failure with no
  device list remains unavailable and does not fabricate a sample.
- There is one `services.Metrics` sampler. All filesystem, process, and
  collector work stays outside `Registry.mu`; snapshot publication and surface
  invalidation happen after unlocking it. The Wayland dispatch owner remains on
  one goroutine.
- Home uses four 40px rings in one row inside the unchanged 356×88 System card;
  each ring carries a centred value and a caption below it. The card keeps an
  accessible group name while dropping the visible `System` heading. The
  visible temperature caption is `Temp`; its accessible name remains `CPU
  temperature`.
- `paintRadialGauge` derives its centred icon/value sizes from the physical box.
  The bar's 22px radial remains a regression surface.
- Intel i915 usage is truthful only when the process can open the system-wide
  PMU counters (`CAP_PERFMON` or `CAP_SYS_ADMIN`). Without that capability the
  GPU identity remains available but usage is absent with an issue; the shell
  must not grant capabilities implicitly or display a fabricated zero.
- Live qualification is restricted to Niri `DP-1`, `3440×1440`, scale `1.0`.
  Do not claim a second output, laptop, absent hardware, or another machine.

## Task 1: Specify the exact GPU history seam and watch it fail

**Files:**

- Test: `internal/services/metrics_test.go`, beside the existing lease/history
  tests.
- No production files in this task.

### Step 1: Add the smallest failing regression

Use a plain `metrics.GPUSnapshot` fixture and the package-private `record`
seam. Acquire only the existing wildcard source lease:

```go
services.Selector{Source: services.SourceGPU}
```

Add focused tests that prove:

1. Two PCI-qualified GPUs recorded in one snapshot produce separate exact
   histories, even when the next snapshot reverses their list order.
2. A valid `0` usage is present in the selected ring, while an invalid usage
   produces no history sample.
3. A valid snapshot in which a device disappears drops that device's old ring;
   a later reappearance starts a fresh ring.
4. `Histories` returns copies and the last `SourceGPU` lease release clears the
   qualified rings, just as the existing generic history contract clears rings.
5. The wildcard source still supplies discovery and does not require a second
   sampler start.

The assertions must use exact selectors and values, not painted nodes. The
tests must not call `metrics.ReadGPU`; the fixture is proving the shell service
contract above the pinned reader.

### Step 2: Run the red check

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/services \
  -run 'TestGPU(History|.*Lease|.*Device)' -count=1
```

Expected result: `FAIL` because the current `Metrics.record` only iterates the
selector-keyed rings created by `Acquire`; it has no exact per-device GPU ring.
If the new test passes, it is testing the wrong contract and must be corrected
before production code is written.

### Step 3: Commit the red specification

Review `git diff --cached` and ensure the unrelated `.beads/issues.jsonl` change
is not staged. Commit only the focused test:

```bash
git add internal/services/metrics_test.go
git commit -m "test: specify exact GPU history"
```

This is the first implementation commit boundary. Do not combine service code,
shell code, or a module-pin change with it.

## Task 2: Implement the service-owned exact history projection

**Files:**

- Modify: `internal/services/metrics.go` in `Metrics`, `Acquire`,
  `releaseMetric`, `Close`, `record`, `History`, and `Histories`.
- Test: the red tests in `internal/services/metrics_test.go`.

### Step 1: Implement the smallest safe state

Keep `history map[Selector]*ring` for ordinary selectors. Add the narrowest
GPU-specific state needed by the proven contract, owned by `Metrics` and
protected by its existing mutex. The state must:

- exist only while at least one `SourceGPU` lease is live;
- key non-empty PCI IDs to independent rings;
- record only `GPU.Usage.Valid` values, including a real zero;
- discard qualified rings when a successful GPU snapshot no longer lists that
  PCI ID, so a disconnected device cannot resume with an old timeline;
- avoid creating an exact identity for multiple GPUs with empty or duplicate
  PCI identity;
- support the one-device empty-subject case without aliasing a later distinct
  device. If the fixture cannot prove a stable empty-subject boundary, render
  that history unavailable and record the limitation rather than guessing;
- return copied slices from `History` and copied map values from `Histories`;
- clear all GPU-specific rings when the last GPU source lease is released.

The existing `collect` path remains one `GPUSampler.Sample` call per sampling
pass. `run` closes that sampler on every stop path. The service history
projection must not start a goroutine, read sysfs, invoke a process, or call
upstream code.

`History`/`Histories` should expose qualified GPU selectors only while
`SourceGPU` is leased. The shell must still require a currently valid selected
snapshot before plotting a returned ring. Do not expose the wildcard ring as a
qualified device's history.

### Step 2: Run the focused green check and service regressions

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/services \
  -run 'TestGPU(History|.*Lease|.*Device)' -count=1
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/services -count=1
```

Expected result: both commands exit `0`; valid zero, invalid readings,
disappearance, release, copied history, and one-sampler behavior are covered.

### Step 3: Commit the service implementation

```bash
git add internal/services/metrics.go internal/services/metrics_test.go
git commit -m "feat: retain qualified GPU history"
```

No `go.mod` or `go.sum` change belongs in this commit. If the service contract
cannot be implemented without changing the pinned upstream reader, stop here
and follow the upstream boundary in Task 6 instead of weakening the test.

## Task 3: Reconcile deterministic selection and truthful shell projection

Start from the preserved local shell diff named by the handover. Reuse fitting
helpers and tests; do not duplicate a second GPU selector or a second metrics
service.

**Files:**

- Modify: `internal/shell/popout_monitor.go` at `selectGPU`, monitor history,
  card, legend, and system-fact projection.
- Modify: `internal/shell/metricwidget.go` at
  `metricSelectorForSnapshot`, current-value formatting, and graph projection.
- Modify: `internal/shell/controlcenter_pages.go` at `ccHome` and
  `ccResourceGroup`.
- Tests: `internal/shell/popout_monitor_test.go`,
  `internal/shell/metricwidget_test.go`, and
  `internal/shell/controlcenter_test.go`.

### Step 1: Add or amend the failing shell assertions

Use table tests over `services.Snapshot` fixtures. Cover:

- one GPU with a PCI ID;
- multiple GPUs sorted by non-empty PCI ID, then name, without mutating the
  snapshot;
- duplicate or otherwise ambiguous identity unavailable;
- one GPU with an empty subject accepted for current projection;
- valid selected usage, including `0`, kept distinct from absent/invalid usage;
- the exact selected selector reaching the bar graph, System Monitor graph,
  and Home gauge from the same snapshot/history map;
- exact GPU history values remain tied to PCI identity when GPU list order
  changes;
- GPU disappearance removes the graph/history presentation instead of
  plotting a stale ring or a wildcard ring;
- Home has exactly four radial gauges in the required order and labels;
- CPU temperature displays Celsius text while its ring uses `Celsius/100`;
- nil, invalid, unleased, failed, and ambiguous values set `Absent=true` with
  `Value=0`, while valid zero sets `Absent=false` with `Value=0`.

Run the focused shell tests before changing production code:

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/shell \
  -run 'Test(Select|GPU|ControlCentreHome|Home.*Gauge|Metric.*Graph)' -count=1
```

Expected result: `FAIL` for any remaining wildcard-history lookup, missing
fourth gauge, inconsistent selector, or stale-value path. A test that passes
before the service/shell change must be tightened until it exercises the exact
history contract.

### Step 2: Implement the shell projection

Keep `selectGPU` as one pure shell helper. It sorts an index copy, never the
snapshot slice, and returns the selected `services.Selector` plus a validity
flag. Use that result in the bar, monitor, and Home paths.

The bar and monitor graphs must look up the selected exact selector in the
immutable history map returned by `Metrics.Histories`. A missing exact ring or
invalid current value makes the graph unavailable. They must not fall back to
`{Source: SourceGPU}`.

`ccResourceGroup` remains the single Home gauge construction site. Keep the
existing `ui.KindRadialGauge`, icon vocabulary, labels, tooltip semantics, and
validity boundary. Temperature gets its Celsius value text; usage gauges get
percentages. Clear retained `Value`/text state on an unavailable rebuild.

### Step 3: Run focused and package checks

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/services ./internal/shell \
  -count=1
```

Expected result: exit `0`; all service history, selector, Home, monitor, bar,
weather, and panel regressions remain green.

### Step 4: Commit shell projection changes

```bash
git add internal/shell/controlcenter_pages.go \
  internal/shell/controlcenter_test.go internal/shell/metricwidget.go \
  internal/shell/metricwidget_test.go internal/shell/popout_monitor.go \
  internal/shell/popout_monitor_test.go
git commit -m "feat: share truthful GPU projections"
```

Keep `.beads/issues.jsonl` out of this commit unless the owner has separately
requested a tracker update for this exact slice.

## Task 4: Wire leases, release, and lock ownership

**Files:**

- Modify: `internal/shell/panelhost.go` in `acquirePanelLeases`,
  `monitorSelectors`, and the existing close/release path only as needed.
- Modify: `internal/shell/registry.go` in `UpdateMetrics`, `viewLocked`, and
  history snapshot assembly only as needed.
- Tests: `internal/shell/registry_test.go`,
  `internal/shell/controlcenter_test.go`, and focused lock-boundary tests.

### Step 1: Prove the lifecycle failure before wiring changes

Add or amend tests that:

1. Open a Control Centre and find leases for CPU, memory, CPU temperature, GPU,
   battery, clock, and any already-configured weather lease.
2. Close/drop the host and prove every lease is released. The last GPU source
   release clears exact GPU histories.
3. Open System Monitor and Home against one `Registry.metrics` instance and
   project one two-GPU snapshot through both. They must select the same PCI ID
   and fraction.
4. Deliver a snapshot through `Registry.UpdateMetrics` and prove an open Home
   or System Monitor rebuilds from it without creating another `Metrics` value
   or sampler start.
5. Prove machine-fact/filesystem reads happen before taking `Registry.mu` and
   surface invalidations are published after unlocking it. Keep the existing
   AST/try-lock style check if it is the narrowest proof.

Run before implementation:

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/shell \
  -run 'Test(ControlCentre|.*GPU|.*Metrics|.*Lease|.*RegistryLock)' -count=1
```

Expected result: `FAIL` if the current local diff still has a wildcard-only
history, missing lease, stale release, second service, or lock-held I/O.

### Step 2: Implement the narrow lifecycle wiring

Keep GPU collection leased through the existing wildcard `SourceGPU` source
lease. The exact selector is selected from each immutable snapshot and is a
history/projection key, not a new collector. Preserve rollback on partial
lease acquisition and `PanelHost.leases` release on every close path.

Keep `UpdateMetrics`'s order explicit: perform machine facts outside
`Registry.mu`, store the immutable snapshot, rebuild retained trees while the
registry state is protected, unlock, then publish bar and surface
invalidations. No collector, process, filesystem, or GPU command may run in a
lock-held configure/render/handle path.

### Step 3: Run green lifecycle and race checks

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/shell \
  -run 'Test(ControlCentre|.*GPU|.*Metrics|.*Lease|.*RegistryLock)' -count=1
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false -race ./internal/services ./internal/shell \
  -count=1
```

If the workstation hook or font environment rejects the race form, preserve the
exact command and output, compare it with the baseline, and do not call the
race gate passed. The pre-existing `sysc-310` media relay race is reported
separately if it reproduces in the baseline.

### Step 4: Commit lifecycle changes separately

```bash
git add internal/shell/panelhost.go internal/shell/registry.go \
  internal/shell/registry_test.go internal/shell/controlcenter_test.go
git commit -m "feat: release shared metrics leases"
```

## Task 5: Implement the approved four-gauge geometry

The owner-approved radial package resolves the provisional 22px two-row layout:
four 40px rings occupy one row of four 77px slots with 9px gaps. The fixed
356×88 System card, 9px card inset, 480px Home page, and display-only
interaction model remain unchanged.

**Files:**

- Test: `internal/shell/controlcenter_test.go` for fixed-body bounds and gauge
  order/size.
- Modify: `internal/shell/controlcenter_pages.go` for the one-row composition,
  accessible group names, and `Temp` caption.
- Modify: `internal/render/radial.go` for box-derived centre type sizes.

### Step 1: Prove layout safety

The focused layout test must demonstrate that all four gauge slots, captions,
values, and the System card fit within the existing fixed body at font scales
75%, 100%, 125%, and 150%. It must use role-specific text heights rather than
the old constant-height stub, assert 40px gauges and 77px slots, and prove the
visual rings are display-only rather than interactive hit targets.

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ./internal/shell \
  -run 'TestControlCentreHome.*(Gauge|System|Bounds|Fit)' -count=1
```

### Step 2: Qualify the approved geometry on the restricted live target

Build/run the exact shell on Niri `DP-1` at `3440×1440`, scale `1.0`, and
capture the Home System card with valid CPU, memory, temperature, and GPU
fixtures from the real metrics service. Record the four captions, centred
values, unavailable state, and display-only interaction model. The approved
design package is the geometry decision; this gate verifies it in the real
surface.

## Task 6: Run the slice gates and handle the upstream boundary

### Step 1: Run affected package and repository checks

```bash
gofmt -w internal/services internal/shell
test -z "$(gofmt -l internal/services internal/shell)"
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go vet -buildvcs=false ./internal/services ./internal/shell
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false -race -count=1 \
  ./internal/services ./internal/shell
git diff --exit-code -- go.mod go.sum
git diff --check
```

Also run the focused Home, selector/history, lease, and lock tests separately
so a broad environment failure cannot hide a slice regression. Record exact
failures; do not attribute an existing failure to this slice without a clean
baseline comparison.

### Step 2: Verify the audited upstream pin

The expected module change is `v0.4.0` → `v0.5.1`. The latter is the published
release containing the corrected close-on-exec PMU syscall flag. If the module
cannot be downloaded or its checksum does not match the published tag, stop at
that boundary and record the release gate; do not use `v0.5.0`, a moving branch,
or a local replacement:

```bash
git add go.mod go.sum
git commit -m "build: pin metrics history release"
```

Do not close `sysc-121`, `sysc-317`, or `sysc-309` from this slice plan.

### Step 3: Run the restricted DP-1 live gate

Use exactly:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Open and close Control Centre Home and standalone System Monitor. Capture both
surfaces and record:

- `niri msg -j layers` before, while open, and after close;
- the selected GPU PCI ID/name, whether the current reading is valid, and the
  displayed value;
- the exact Home gauge order and the matching System Monitor/bar selector;
- the unavailable presentation for any invalid or absent state;
- the one-output limitation and any hardware-only checks that cannot run.

Do not claim a second output, laptop, absent GPU, forced battery, or another
machine. The M10 completion handover, not this plan, records the final
milestone result and remains owned by `sysc-317`.

## Commit boundaries and completion condition

The intended implementation history is:

1. red exact-history tests;
2. service-owned qualified history;
3. shell selector/Home/monitor/bar projection;
4. lease and lock lifecycle;
5. the audited `v0.5.1` upstream pin;
6. the live and package gates.

Each boundary must pass its preceding focused check. The metrics slice is not
complete until exact GPU history is observed in a real projection, the geometry
decision is recorded, package/race/vet/module-diff evidence is captured, and
the restricted DP-1 live result is written into the M10 completion handover.

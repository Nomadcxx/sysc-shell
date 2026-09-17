> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

# Metrics, Control Centre, and GPU Implementation Plan

**Goal:** Add four truthful Home system gauges and make the bar, standalone System Monitor, and Control Centre use one deterministic GPU snapshot from the existing metrics service.

**Architecture:** Extend the existing `services.Snapshot` projection and `services.Metrics` selector leases. Keep collection in `github.com/Nomadcxx/sysc-metrics@v0.4.0`; add only shell-side deterministic device selection, Home projection, and lifecycle wiring. The Wayland owner continues to consume immutable snapshots, and no reader or process call runs while `Registry.mu` is held.

**Tech Stack:** Go, existing `internal/services`, retained `internal/ui` trees, `internal/shell` panel hosts, and the pinned `sysc-metrics@v0.4.0` module.

---

## Constraints and invariants

- Read `docs/plans/2026-09-16-metrics-control-centre-gpu-design.md` before editing. It is the governing design for this slice.
- Keep `github.com/Nomadcxx/sysc-metrics` at `v0.4.0` unless a real hardware qualification proves the reader is defective. If upstream work is needed, stop shell work at that gate, file it in `sysc-metrics`, publish a tag, and update `go.mod` only to that tag. Do not add a replacement directive or import a moving branch.
- `services.Metrics` remains the only collector. CPU usage, memory usage, CPU temperature (`SourceCPU` with subject `temperature`), and GPU usage are leases on the existing service.
- Home order is CPU usage, memory usage, CPU temperature, GPU usage. Valid zero is visible `0%` or `0°C`; nil, invalid, unleased, failed, or ambiguous readings are unavailable and never fabricated as zero.
- A multi-GPU selection sorts by non-empty PCI ID, then by name, and selects the first deterministic device. If identity is ambiguous, return unavailable. Keep the selected `services.Selector` identical for Home and System Monitor.
- Panel leases must be released through the existing `PanelHost.leases` lifecycle. Do not create a second sampling loop.
- Bare-metal qualification is limited to Niri `DP-1`, `3440×1440`, scale `1.0`. No second-output, laptop, or absent-hardware claim may be made.

## Task 1: Define and prove deterministic GPU selection

**Files:**

- Modify: `internal/shell/popout_monitor.go` near `monitorSelectors`, `formatMonitorMetric`, and GPU legend projection.
- Modify: `internal/shell/controlcenter_pages.go` near `ccResourceGroup`.
- Test: `internal/shell/popout_monitor_test.go`.
- Test: `internal/services/metrics_test.go` only if the current `Snapshot.Fraction` contract needs a focused regression.

### Step 1: Write the failing test

Add table-driven tests for the pure selection/projection seam. Cover:

1. one GPU with a PCI ID selects that ID;
2. multiple GPUs select the lexicographically first non-empty PCI ID, with name as the tie-breaker;
3. a valid selected GPU reports its real fraction, including `0`;
4. missing GPU, invalid usage, and ambiguous identity report unavailable rather than zero;
5. the selected subject makes both the monitor and Home request the same device.

Use `metrics.GPUSnapshot` fixtures and assert selector/value pairs, not painted pixels. Do not mock the upstream reader when a plain snapshot fixture proves the behavior.

### Step 2: Run the focused test and verify it fails for the missing behavior

Run:

```bash
go test ./internal/shell -run 'Test(Select|GPU)' -count=1
```

Expected: `FAIL`, with the new selection helper or deterministic multi-GPU behavior absent. If the test passes before production changes, replace it with a case that exercises the existing defect rather than weakening the assertion.

### Step 3: Implement the smallest selection helper

Add one shell-owned pure helper at the existing monitor projection seam. It must return either the selected `services.Selector{Source: services.SourceGPU, Subject: pciID}` or an unavailable result. Sort a copy of the GPU list; never mutate the service snapshot. An empty PCI ID is allowed only for an unambiguous single device. Keep `Snapshot.Fraction` as the validity boundary and do not convert invalid usage to zero.

Use the helper from both monitor and Home projection. Do not add a GPU reader, a new service, a generic selector registry, or a new UI primitive.

### Step 4: Run the focused test and the package regression set

Run:

```bash
go test ./internal/shell -run 'Test(Select|GPU)' -count=1
go test ./internal/services ./internal/shell -count=1
```

Expected: both commands exit `0`; the existing metric and monitor tests remain green.

### Step 5: Commit the task

```bash
git add internal/shell/popout_monitor.go internal/shell/controlcenter_pages.go internal/shell/popout_monitor_test.go internal/services/metrics_test.go
git commit -m "feat: select GPU metrics deterministically"
```

Commit only files changed for this task. Do not stage `.beads/issues.jsonl` unless the tracker hook explicitly requires a corresponding issue update; if it appears staged, stop and report it before committing.

## Task 2: Add the four Home gauges and preserve truthful values

**Files:**

- Modify: `internal/shell/controlcenter_pages.go` in `ccHome` and `ccResourceGroup`.
- Test: `internal/shell/controlcenter_test.go` near `TestControlCentreHomeRadialResourcesPreserveSampleState`.
- Test: `internal/shell/metricwidget_test.go` only if the shared temperature/GPU selector vocabulary lacks coverage.

### Step 1: Write the failing test

Change the Home regression to require exactly four `ui.KindRadialGauge` nodes in this order: `sysmon-cpu`, `sysmon-memory`, the temperature slot with its Celsius value text, and `sysmon-gpu`. Assert labels identify CPU, Memory, CPU temperature, and GPU. Add a table for a valid fixture, valid zero values, and each unavailable state (nil CPU, nil memory, invalid thermal, nil GPU, invalid GPU usage). Assert unavailable gauges have `Absent=true` and `Value=0`, while valid zero gauges have `Absent=false` and `Value=0`.

### Step 2: Run the focused test and verify it fails

Run:

```bash
go test ./internal/shell -run 'TestControlCentreHome|TestHome.*Gauge|TestHome.*Resource' -count=1
```

Expected: `FAIL` because the current Home tree has only CPU and memory gauges and `ccResourceGroup` has no temperature/GPU value path.

### Step 3: Implement the minimal Home projection

Make `ccResourceGroup` accept the existing selector and a source-specific display mode only if the current code cannot express the temperature label/value. Reuse the existing `Snapshot.Fraction`, `render.GaugeIconName`, `ccPercent`, `ccGaugeSize`, and `ui.KindRadialGauge`. For temperature, display rounded Celsius with the existing degree formatting and use `Celsius/100` as the ring fraction. For GPU, use the deterministic selector from Task 1. Preserve `ccResourceRowH`, current layout, theme metrics, and accessible labels.

Keep invalid and absent readings unavailable. Do not let a failed `Snapshot.Fraction` call leave a stale prior `Value` in a retained node.

### Step 4: Run the focused test and package regressions

Run:

```bash
go test ./internal/shell -run 'TestControlCentreHome|TestHome.*Gauge|TestHome.*Resource' -count=1
go test ./internal/shell -count=1
```

Expected: both commands exit `0`; Home still fits its fixed `480`-pixel body and existing identity, weather, quick-access, and slider tests remain green.

### Step 5: Commit the task

```bash
git add internal/shell/controlcenter_pages.go internal/shell/controlcenter_test.go internal/shell/metricwidget_test.go
git commit -m "feat: complete control centre system gauges"
```

## Task 3: Acquire the shared selector set and rebuild the monitor consistently

**Files:**

- Modify: `internal/shell/panelhost.go` in `acquirePanelLeases` for `PanelControlCenter` and `monitorSelectors` only if required by the selector helper.
- Modify: `internal/shell/popout_monitor.go` where GPU cards and legends derive the selector.
- Modify: `internal/shell/registry.go` only if the existing `UpdateMetrics` rebuild list omits an open Control Centre path; preserve its off-lock publish sequence.
- Test: `internal/shell/registry_test.go` near the existing lease lifecycle tests.
- Test: `internal/shell/popout_monitor_test.go` for the same selected GPU in monitor card and legend.

### Step 1: Write the failing lifecycle tests

Add tests that open a Control Centre host and assert its leases include CPU, memory, CPU temperature, GPU, battery, and clock. Assert closing/dropping the host releases those selectors. Add a shared-snapshot test that projects a two-GPU fixture through the monitor and Home and observes the same selected PCI ID and fraction. Add an update test showing an open Control Centre rebuilds from `Registry.UpdateMetrics` without creating another metrics service or sampling goroutine.

### Step 2: Run the focused tests and verify the missing lease behavior

Run:

```bash
go test ./internal/shell -run 'Test(ControlCentre|.*GPU|.*Metrics|.*Lease)' -count=1
```

Expected: `FAIL` where the current Control Centre lease count lacks temperature and GPU, or where the two consumers choose different GPU subjects.

### Step 3: Implement the lifecycle wiring

Extend the existing Control Centre selector list with `{Source: SourceCPU, Subject: "temperature"}` and the selected GPU selector. Keep battery and clock acquisition and error rollback intact. If the selector must be derived from a snapshot, use a stable selector contract that does not require a collector read during `acquirePanelLeases`; an empty/ambiguous selection leases `SourceGPU` with no fabricated value and remains unavailable. Ensure `UpdateMetrics` rebuilds the already-open Control Centre while still publishing after unlocking `Registry.mu`.

Do not acquire the GPU independently for the monitor and Home, do not call `metrics.ReadGPU` in shell code, and do not run any service work under the Registry lock.

### Step 4: Run the focused and race checks

Run:

```bash
go test ./internal/shell -run 'Test(ControlCentre|.*GPU|.*Metrics|.*Lease)' -count=1
go test ./internal/services ./internal/shell -race -count=1
```

Expected: both commands exit `0`. If the workstation hook refuses the race form, record the exact refusal and run the affected package without `-race`; do not claim race qualification.

### Step 5: Commit the task

```bash
git add internal/shell/panelhost.go internal/shell/popout_monitor.go internal/shell/registry.go internal/shell/registry_test.go internal/shell/popout_monitor_test.go
git commit -m "feat: share GPU state across system surfaces"
```

## Task 4: Qualify the pinned reader and run the M10 slice gate

**Files:**

- Modify: none unless Tasks 1–3 identify a shell-owned defect.
- Record: the completion handover for M10, not this plan, after the whole milestone exits.
- Tracker: update the relevant Beads issue only with the exact reader, selector, and live result; do not use this plan as status storage.

### Step 1: Run the full affected checks

Run:

```bash
gofmt -w internal/services internal/shell
test -z "$(gofmt -l internal/services internal/shell)"
go vet ./internal/services ./internal/shell
go test -race -count=1 ./internal/services ./internal/shell
git diff --exit-code -- go.mod go.sum
git diff --check
```

Expected: all commands exit `0`. Report any hook refusal or pre-existing failure with its exact command and output.

### Step 2: Run the bare-metal Niri check

Use:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Run the exact built shell on `DP-1` (`3440×1440`, scale `1.0`). Open Control Centre Home and System Monitor, capture both, and record whether the machine supplies a GPU, the reader used by `sysc-metrics`, the selected PCI ID/name, and the visible value. Confirm a missing or invalid GPU shows unavailable rather than `0%`. Compare `niri msg -j layers` before and after opening/closing both surfaces.

Do not claim a second output, laptop, forced battery, or hardware absent from this machine. Do not change `go.mod` merely because the live machine lacks a GPU.

### Step 3: Commit only slice-specific follow-up corrections

If the gate finds a shell defect, write its failing regression first, run it red, apply the narrow fix, rerun the affected checks, and commit it separately. If the reader is defective, stop and record the upstream release gate rather than adding a shell fallback.


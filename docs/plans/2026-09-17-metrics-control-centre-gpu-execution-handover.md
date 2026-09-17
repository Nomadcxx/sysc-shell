# M10 metrics, Control Centre, and GPU planning session handover

Date: 2026-09-17. M10 parent: `sysc-309`. Existing metrics/System Monitor
owner: `sysc-121`.

This is a focused session handover for the M10 metrics/GPU slice. The next
agent's immediate job is to write or correct the executable implementation
plan from the approved design, then commit that plan as a docs-only change.
Do not start another broad shell-polish pass and do not begin product-code
implementation from this document.

## Required reading and ownership

Work from `/home/nomadx/sysc-shell` for Beads commands. Read, in this order:

1. `AGENTS.md`.
2. `docs/plans/README.md` and the M10 section of `docs/roadmap.md`.
3. `docs/plans/2026-09-16-metrics-control-centre-gpu-design.md`.
4. The current source seams listed below.
5. `bd show sysc-121`, `bd show sysc-317`, `bd ready`, and `bd blocked`.

The design is the authority. Existing issue descriptions may contain stale
`sysc-metrics@v0.2.0` wording; the checked-in `go.mod`, the pinned
`sysc-metrics@v0.4.0` source, and the design take precedence.

Do not create a second metrics owner or a second GPU reader. `sysc-121` owns
the existing metrics/System Monitor work. `sysc-317` owns cross-slice M10
integration and the final gate. `sysc-309` remains open until the whole M10
acceptance boundary passes.

## Current repository state

- Branch: `main`.
- `HEAD`: `2dc3278` (`docs: plan metrics control centre gpu`).
- The approved GPU design is committed and registered.
- An executable plan already exists at
  `docs/plans/2026-09-17-metrics-control-centre-gpu.md`, committed at the
  current `HEAD`. Treat it as provisional input: audit it against the design,
  especially the lease/history contract below. Do not assume that its passing
  projection tests mean the GPU graphs work.
- The current metrics/GPU implementation is uncommitted in these shell files:

  - `internal/shell/controlcenter_pages.go`
  - `internal/shell/controlcenter_test.go`
  - `internal/shell/metricwidget.go`
  - `internal/shell/metricwidget_test.go`
  - `internal/shell/panelhost.go`
  - `internal/shell/popout_monitor.go`
  - `internal/shell/popout_monitor_test.go`
  - `internal/shell/registry.go`
  - `internal/shell/registry_test.go`

- `.beads/issues.jsonl` has a large unrelated/pre-existing working-tree
  change. Preserve it. Do not reset, clean, stash, or overwrite it.
- No implementation commit, M10 completion gate, live surface capture, or
  completion handover exists for this slice.

The plan commit and this handover are documentation work. Do not stage the
unrelated Beads change with the plan. Register any replacement plan in
`docs/plans/README.md` in the same docs-only commit.

## Governing design decisions

The plan must implement these decisions from
`2026-09-16-metrics-control-centre-gpu-design.md`:

- Control Centre Home has exactly four fixed System gauges, in this order:
  CPU usage, memory usage, CPU temperature, GPU usage.
- CPU temperature uses `{Source: SourceCPU, Subject: "temperature"}` and the
  existing `Celsius/100` ring contract. The visible value is Celsius.
- CPU, memory, and GPU usage display percentages. A valid zero remains a
  visible zero. Nil, invalid, unleased, failed, or ambiguous state is visibly
  unavailable, never an invented zero.
- One `services.Metrics` sampler serves the bar, System Monitor, and Control
  Centre. The Control Centre acquires CPU, memory, temperature, GPU, battery,
  and clock selectors through the existing `PanelHost.leases` lifecycle.
- Multi-GPU choice is deterministic: sort a copy by non-empty PCI ID, then
  name, and select the first. A single device may have an empty subject. An
  identity that cannot distinguish devices is unavailable.
- Home and System Monitor must use the same selected `services.Selector` and
  immutable snapshot. The shell must not call a GPU reader or `nvidia-smi`.
- Filesystem, process, and collector work remains outside `Registry.mu`; the
  Wayland dispatch loop stays on one goroutine.
- The live qualification is only Niri `DP-1`, `3440×1440`, scale `1.0`.
  Do not claim a second output, laptop, absent hardware, or another machine.

The design's D4 says to preserve the existing gauge geometry and semantics.
The local implementation reduced the gauge size from the prior 40 pixels to
22 pixels to fit four slots in the fixed 480-pixel body. The next plan must
make this an explicit geometry decision: prove 22 pixels meets the design's
legibility and hit-target requirements, or amend the geometry/design before
implementation is considered complete. Do not silently ship the reduction.

## Work already performed in the local tree

These are useful starting points, not completed or reviewed commits.

### Projection and selection

`internal/shell/popout_monitor.go` contains a shell-side `selectGPU` helper.
It sorts a copied GPU list by PCI identity and name, returns a selected
`services.Selector`, and preserves the snapshot validity boundary. The bar
metric, System Monitor, and Home GPU projections use the selected snapshot
device. Invalid usage is not converted to zero.

### Home gauges

`internal/shell/controlcenter_pages.go` projects four radial gauge slots and
uses the existing gauge, icon, label, tooltip, and unavailable-state paths.
Temperature uses the CPU thermal subject; GPU uses the selected device.

### Lifecycle and locking

`internal/shell/panelhost.go` and `internal/shell/registry.go` add the
Control Centre metric lease wiring and preserve the existing update/rebuild
sequence. `internal/shell/popout_monitor.go` caches monitor machine facts so
filesystem reads do not occur while `Registry.mu` is held.

### Focused tests

The local test edits cover deterministic selection, valid zero versus
unavailable state, Home gauge order and labels, leases and release, the same
selected GPU reaching Home and System Monitor, history projection, and the
lock/I/O boundary. Review these tests rather than duplicating them. They do
not yet prove that exact GPU history is collected and delivered.

## Research and hardware evidence

The pinned reader already owns GPU discovery. Its source is:

`/home/nomadx/go/pkg/mod/github.com/!nomadcxx/sysc-metrics@v0.4.0/gpu_linux.go`

The reader:

- enumerates `/sys/class/drm/card*`;
- ignores connector entries and `simpledrm`;
- reads PCI vendor/device identity;
- reads AMD usage from `gpu_busy_percent`;
- calls `nvidia-smi` only when a `10de:` device exists;
- matches NVIDIA results by PCI BDF;
- looks up names through `pci.ids`; and
- propagates missing and invalid readings through validity flags.

The receiving machine currently reports:

- DRM device: `card1`;
- GPU: NVIDIA RTX 4060;
- PCI BDF: `00000000:08:00.0`;
- `nvidia-smi`: `0%`, `56°C`.

The pinned module's GPU and thermal tests passed. Prior-art review did not
identify a dependency worth adding:

- Noctalia's system-monitor plugin consumes a host-provided `stats.gpu`;
- DMS's NVIDIA monitor polls `nvidia-smi` and keys variants by PCI address;
- `dgpu-sleep-monitor` watches power state and requires a configured PCI
  address;
- `gpu-monitoring-tools` is old C++/CGO NVIDIA tooling; and
- `amdgpu_top` is Rust and AMD-specialised.

The research conclusion is clear: investigate the shell/service projection
and lease lifecycle first. Do not restart reader research unless a focused
qualification proves the pinned reader cannot produce the required snapshot.

## Confirmed blocker: current GPU graphs do not work

The current local implementation has a selector mismatch:

```go
// Current Control Centre lease shape
services.Selector{Source: services.SourceGPU}
```

The bar and System Monitor projection now ask for history using an exact
PCI-qualified selector chosen from the snapshot. The wildcard GPU lease does
not produce that exact history. The wildcard history path is deliberately not
plotted because it cannot identify which GPU produced each point.

Therefore the current tree can project a valid selected current GPU value,
but the GPU graph remains unavailable. This is the first implementation
problem the plan must address. Re-enabling wildcard history is not an
acceptable fix because it can plot an unidentified device.

The plan must trace the actual `services.Metrics` lease and history
implementation before choosing a fix. Determine whether the existing API can:

1. acquire an exact PCI selector before history collection;
2. publish exact history for a stable selected device; or
3. expose a safe immutable selected-device history projection while retaining
   one sampling pass.

If `sysc-metrics@v0.4.0` cannot express the required contract, stop shell
implementation at that boundary. Record the API gap in the cross-repository
issue, qualify an upstream release, and only then update the shell pin to a
published tag. Do not add a local `replace`, moving branch, shell-side
`nvidia-smi`, or wildcard graph fallback.

## What the next agent must produce

Write or replace the metrics/GPU executable plan from the governing design.
Use the `superpowers:executing-plans` header. The plan must:

- start with a smallest failing test at the lease/history seam, not only a
  selector or painted-tree test;
- name exact files, symbols, commands, and expected red/green results;
- separate service/API qualification from shell projection changes;
- cover exact selector acquisition, exact history, device disappearance,
  invalid readings, release, and shared Home/System Monitor identity;
- include the 22-pixel versus 40-pixel geometry decision as a gate;
- preserve one sampler and the `Registry.mu`/Wayland ownership boundaries;
- define separate commit boundaries for tests, implementation, and any
  upstream pin change; and
- end with the affected package gates and the restricted DP-1 live gate.

Recommended plan sequence:

1. Inspect `internal/services/metrics.go` and the pinned module's lease/history
   API; add the failing exact-history regression.
2. Prove the API boundary and choose an exact-history implementation or an
   upstream release gate.
3. Reconcile the existing local shell diff with the chosen contract, keeping
   the deterministic selection and truthful unavailable behavior.
4. Resolve and document the four-gauge geometry.
5. Run package, race, vet, module-diff, and live Niri evidence gates.

Do not implement the next plan in this planning handover. Commit the corrected
plan and its `docs/plans/README.md` row as a docs-only commit before code work.

## Existing verification evidence

Previously observed passing checks:

```text
sysc-metrics v0.4.0 GPU/thermal tests: PASS
sysc-shell internal/services focused GPU/history/lease/fraction tests: PASS
```

The broad shell run was not valid evidence because the environment could not
resolve `Inter Variable sans-serif` during font setup. The default Go cache is
also read-only. Use the writable cache and disable VCS stamping when needed:

```bash
GOCACHE=/tmp/sysc-shell-go-cache GIT_CONFIG_NOSYSTEM=1 \
GIT_CONFIG_GLOBAL=/dev/null go test -buildvcs=false ...
```

The pre-existing `sysc-310` media relay race may still affect a full shell
race run. Report it separately and do not attribute it to the GPU slice
without a baseline comparison.

## Stop condition

The planning handoff ends when the corrected, registered implementation plan
is committed and visible on `main`. The implementation handoff ends only when
the exact GPU history path is proven, the four-gauge geometry is approved,
affected tests and gates pass, and the DP-1 live result reaches both Home and
System Monitor. M10 remains open until `sysc-317` completes the cross-slice
integration and writes the immutable M10 completion handover.

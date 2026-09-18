# sysc-metrics Intel GPU usage execution handover

Date: 2026-09-17

Receiving repository: `/home/nomadx/sysc-metrics`

Consumer: `/home/nomadx/sysc-shell`, currently pinned to
`github.com/Nomadcxx/sysc-metrics@v0.4.0`

## Outcome

Add truthful Intel GPU utilization to `sysc-metrics` for Intel GPUs managed by
the Linux `i915` driver. Preserve the existing `GPU`, `GPUUsage`,
`GPUSnapshot`, and `ReadGPU` compatibility contract, add the smallest stateful
sampler needed for counter-based utilization, qualify it on the Intel laptop,
and publish the next tagged release for the shell to consume.

The shell must remain on `v0.4.0` until the upstream release exists. Do not
solve this in `sysc-shell` with a second reader, a shell-side `perf` call, or a
`replace` directive.

## Starting point

Work in `/home/nomadx/sysc-metrics`, starting from `main` at the `v0.4.0`
implementation (`232529e` in the handover's source tree). Read:

1. `AGENTS.md`.
2. `docs/plans/2026-09-02-thermal-and-gpu-design.md`.
3. `docs/plans/2026-09-02-thermal-and-gpu.md`.
4. `metrics.go`, `gpu_linux.go`, `gpu_linux_test.go`, and
   `integration_linux_test.go`.
5. The shell consumer in `internal/services/metrics.go`, especially its one
   sequential sampler owner and the current `metrics.ReadGPU()` call.

The existing thermal/GPU design explicitly left Intel usage out of scope. The
new work must amend that design and add an executable upstream plan before the
implementation lands. Keep the amendment and plan in the upstream repository;
do not edit the shell's product code from this commission.

## Evidence and current defect

The current reader already does the following:

- enumerates `/sys/class/drm/card*` devices;
- skips connectors and `simpledrm`;
- reports Intel PCI identity, name, and available hwmon temperature;
- reads AMD usage from `gpu_busy_percent`; and
- reads NVIDIA usage through `nvidia-smi`.

For Intel, `GPU.Usage.Valid` remains false because i915 exposes utilization as
PMU counters rather than a percentage file. The current `ReadGPU` API is
stateless, while a counter percentage needs two readings and a monotonic
interval.

The target laptop is reachable at:

```text
ssh -p 7777 nomadx@192.168.0.64
```

The observed hardware is Intel i915, PCI `0000:00:02.0`, device `0x3ea0`.
The PMU currently exposes:

```text
/sys/bus/event_source/devices/i915/type = 13
format/config:0-20
events: rcs0-busy, bcs0-busy, vcs0-busy, vecs0-busy
unit: ns
```

Kernel versions and GPU generations can expose different engine sets. Discover
the PMU and its `*-busy` event files at runtime; do not hard-code this laptop's
four names or event numbers.

There is no `intel_gpu_top` or `perf` binary on the laptop.
`gt_act_freq_mhz` is a frequency reading, not utilization. Neither is an
acceptable substitute.

## Design boundary

### Recommended approach

Use the i915 PMU engine-busy counters through `perf_event_open`.

The concrete public shape should be a stateful sampler in the style of the
existing CPU, block, and network samplers:

```go
type GPUSampler struct { /* owned by one sequential caller */ }

func NewGPUSampler() *GPUSampler
func (s *GPUSampler) Sample() (GPUSnapshot, error)
func (s *GPUSampler) Close() error // or an equally clear idempotent close API
```

The exact close signature may follow the repository's conventions, but open
PMU file descriptors must have an explicit owner and lifetime. `Sample` must
remain synchronous and must not start a goroutine.

`ReadGPU()` stays source-compatible for callers that need a one-shot identity,
temperature, AMD, or NVIDIA snapshot. It must not hide a process-global sampler
or pretend that one counter read is a valid Intel percentage. A consumer that
needs Intel usage must use `GPUSampler`; the completion handover must state this
migration clearly. The shell's single metrics goroutine will own one sampler
and close it when its metrics run stops.

The sampler should:

1. enumerate the normal DRM GPU snapshot;
2. identify Intel `i915` devices and their PMU event source;
3. discover all available `*-busy` engine events and parse their event config;
4. keep the opened counters and prior values across `Sample` calls;
5. calculate busy deltas over the monotonic sample interval; and
6. write the resulting fraction into the matching Intel `GPU` entry.

Map a PMU to a GPU through its sysfs device/PCI identity when the kernel
exposes that link. If more than one Intel GPU cannot be distinguished, leave
usage unavailable rather than assigning one PMU to an arbitrary device.

The single `GPUUsage.Fraction` needs an explicit multi-engine policy. The
default recommendation is the maximum of the measured engine busy fractions:
it stays within the existing 0..1 contract and does not double-count parallel
engines. If kernel evidence supports a device-total event or justifies another
aggregation, record that decision in the upstream design and test it. Do not
silently sum engine percentages or expose a new UI-specific field.

Do not add `intel_gpu_top`, `perf`, a daemon, C/CGO, a shell-side reader, or a
frequency/max-frequency heuristic. Use the Go standard library and a narrow
Linux syscall shim first. If a new module or a wider native boundary is
needed, stop and record the measured reason before adding it.

## Required semantics

- The first Intel sample has `Usage.Valid == false` because no delta exists.
- A second sample with a readable counter and a positive interval has
  `Usage.Valid == true`.
- A measured zero is valid zero, not unavailable.
- Counter decrease, counter reset, short/invalid reads, non-positive elapsed
  time, and an impossible delta rebaseline the engine and leave that sample
  invalid.
- A missing PMU, missing `*-busy` events, or `EPERM`/`EACCES` leaves the
  Intel GPU in the snapshot with invalid usage and a useful `Issue`; it does
  not turn the reading into zero and does not discard identity or temperature.
- A failed engine read must not produce a fabricated device-wide percentage.
  The implementation must define whether partial engine coverage is invalid or
  is a valid partial snapshot with an `Issue`; choose the conservative rule and
  document it.
- GPU removal closes and drops its PMU state. Reappearance starts with a fresh
  baseline; old history must not resume across the gap.
- Existing AMD and NVIDIA behavior, valid-zero handling, deterministic GPU
  ordering, and `nvidia-smi` injection remain green.
- `GPUUsage.Fraction` is finite and within 0..1 whenever `Valid` is true.
- The API remains safe under the repository's documented ownership rule: one
  sequential sampler owner, no concurrent `Sample` calls.

## Work packages

### 1. Establish the upstream design and probe the kernel interface

Before production code, amend the thermal/GPU design with the sampler
contract, i915 PMU discovery, engine aggregation, permission behavior, and
the boundary for unsupported driver variants such as `xe`. Add an executable
plan with exact files and test commands.

Use read-only probes on the laptop to record the PMU type, event names,
`format`, event definitions, PMU-to-PCI link, `perf_event_paranoid`, and the
first two counter reads. Do not change sysctls or install a CLI as part of the
probe.

If `xe` uses a different public PMU contract, document it as a separate
driver-gated limitation rather than claiming that i915 support covers it.

### 2. Specify pure parsing and delta behavior with failing tests first

Expected files:

- Create or modify: `gpu_pmu_linux.go`.
- Create: `gpu_pmu_linux_test.go`.
- Modify: `metrics_test.go` for public sampler compile assertions and zero
  value semantics.

Inject the narrow PMU opener/counter reader needed by fixture tests. Do not
make tests invoke real `perf_event_open`.

The red tests must cover:

- parsing the PMU `type`, `format`, and `events/*-busy` definitions;
- engine names that differ from the laptop's four names;
- first sample invalid, second sample valid;
- valid zero busy delta;
- counter reset/decrease and impossible delta rebaseline;
- missing event source, malformed event definition, permission failure, and
  short read;
- stable aggregation across engine order changes; and
- bounded finite output.

Run the focused package tests before implementation and show the expected
failure. Then implement the smallest pure calculation that makes them pass.

### 3. Add the stateful sampler and integrate it with GPU discovery

Expected files:

- Modify: `metrics.go`.
- Modify: `gpu_linux.go`.
- Modify: `gpu_linux_test.go`.
- Modify: `integration_linux_test.go`.

Keep static DRM discovery, PCI naming, temperature, AMD usage, and NVIDIA
fallback in their current ownership layer. Layer the i915 sampler onto that
snapshot rather than duplicating GPU enumeration.

The fixture tests must prove:

- an Intel GPU retains identity/name/temp while usage becomes valid after two
  sampler calls;
- no `nvidia-smi` invocation occurs for an Intel-only fixture;
- a valid zero remains valid;
- device disappearance clears the sampler state; and
- `Close` is idempotent and closes every opened event.

Do not make `ReadGPU` sleep to manufacture an interval. Do not use package
globals to retain counter state between callers.

### 4. Update upstream documentation and release artifacts

Update the amended design, executable plan, and `README.md` so they describe:

- i915 PMU as the Intel utilization source;
- the stateful sampler requirement;
- first-sample and valid-zero semantics;
- permission/unavailable behavior; and
- the supported driver/kernel boundary.

Run the full upstream gates:

```bash
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
```

The module-diff check may be omitted only if a dependency change is explicitly
designed, pinned, and recorded. Do not tag a tree with failing fixture tests.

### 5. Qualify the real Intel laptop and publish the release

From the laptop checkout, run an opt-in live check that requires an Intel i915
GPU. It must take at least two samples far enough apart to establish a delta,
print the PCI identity and PMU event set, and assert `Usage.Valid` on the
second sample. An idle valid zero is a successful measurement; if available,
record a short workload that produces a non-zero sample as additional evidence.

The live result must record:

- kernel version and driver;
- Intel PCI BDF and device id;
- PMU type and discovered busy events;
- first-sample invalid state;
- second-sample valid state and fraction, including a valid-zero result; and
- any permission or device-removal behavior observed.

The upstream completion handover must include commit hashes, gate output, the
published release tag (next additive release after `v0.4.0`, expected
`v0.5.0`), and the exact shell migration contract. Do not ask the shell to
consume a moving branch or an untagged pseudo-version.

## Acceptance boundary

This commission is complete when the upstream package has a tagged release
that reports real i915 usage through its stateful sampler, all existing GPU
paths remain green, the laptop's second sample is valid, and the completion
handover records the evidence.

The shell integration follows that release: its one metrics goroutine creates
and owns `GPUSampler`, calls `Sample`, publishes the immutable snapshot, and
closes the sampler with the rest of the metrics lifetime. It then updates its
module pin. No shell implementation should begin from this document before
the upstream tag exists.


# Metrics widgets

Notes for the CPU, memory, temperature and GPU widgets in the bar.

## Older bar configurations

The built-in default groups CPU, memory, temperature, and GPU as radial gauges. Existing JSON
configs keep their stored display mode, so an older config can show the legacy text or meter
widgets even while GPU telemetry works. Set `"display": "radial"` on each metric item that should
use the grouped gauges:

```json
[
  {"id":"cpu","display":"radial"},
  {"id":"memory","display":"radial"},
  {"id":"temperature","display":"radial"},
  {"id":"gpu","display":"radial"}
]
```

Back up `~/.config/sysc-shell/config.json` before editing it, then restart the user service.

## Intel i915 GPU usage

Intel i915 reports utilization through PMU engine-busy counters. A shell build that displays Intel
usage must pin `sysc-metrics` to `v0.5.1` or newer and let its metrics service own one stateful
`GPUSampler`. `ReadGPU` supplies Intel identity and temperature, but it cannot produce Intel
utilization. The sampler needs two samples: the first establishes a baseline and the second can
report usage. A valid `0%` means the GPU is idle. Without permission, identity and temperature
remain available while usage stays unavailable.

On the tested Intel laptop, opening the system-wide i915 counters requires `CAP_PERFMON` on the
shell process. `CAP_SYS_ADMIN` also satisfies the kernel gate but grants broader privilege. Lowering
`kernel.perf_event_paranoid` alone did not enable these counters. Apply the file capability again
after every replacement of the installed binary:

```sh
sudo setcap cap_perfmon+ep ~/.local/bin/sysc-shell
getcap ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service
systemctl --user is-active sysc-shell.service
```

The expected `getcap` result contains `cap_perfmon=ep`. The implementation covers the Linux
`i915` driver. The newer `xe` driver needs a separate driver-specific qualification.

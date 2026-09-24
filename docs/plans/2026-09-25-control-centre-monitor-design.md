# Control Centre Monitor Page and Sparkline Design

Tracked as `sysc-517`. Supersedes `sysc-336` (ccMonitor omits GPU and CPU temperature).

## Why

The Control Centre's Monitor section is five stacked 154px graph cards in a 480px viewport. Three
things are wrong with it, all confirmed live on 2026-09-25:

1. **Network never draws.** Two causes. The Control Centre leases CPU, memory, temperature, GPU and
   battery, but not network, filesystem or block (`acquirePanelLeases`, `panelhost.go`), so a network
   ring exists only when a bar widget happens to lease one. And `ccNetworkSelector` picks the
   alphabetically first interface, which on the desktop is the idle Docker bridge `br-07deea98ce79`.
2. **Temperature and GPU are below the fold.** Only CPU, Memory and Network fit the viewport.
3. **Storage, swap and disk I/O are absent**, although `services.Snapshot` already carries
   `Filesystem`, `Block` and `Memory.Swap`.

Separately, the graph itself reads poorly. `paintGraph` fills one hard-edged column per sample with no
anti-aliasing: a steady low CPU load renders as a comb of 1px spikes and memory as a flat slab.

## Prior art

- **Noctalia `system-monitor` community plugin** (the closest reference): icon plus value per metric,
  grouped CPU / GPU / Memory / Storage / Network; activity and critical thresholds per metric
  (CPU 50/90, CPU temperature 60/85, RAM 60/90, disk 80/95); an em dash for any sensor the hardware
  lacks; a detail view with CPU load averages, GPU usage and temperature, RAM and swap with totals,
  disk used/total for one path, network session totals, and top processes.
- **Noctalia system monitor service**: per-group sampling, with GPU sampled only while displayed. The
  Control Centre "System" tab aggregates the statistics.
- **`chr314/dms-system-monitor`**: live line charts. 1.5px stroke, quadratic smoothing through
  midpoints, an area fill at 18% of the series colour, network RX/TX as two series on one chart,
  auto-scaled rates with 10% headroom, a 60-second window, 64px charts in the popout.

## Decisions

### D1. One sparkline style everywhere

`KindGraph` keeps its name and contract. Its painter changes for every consumer: the Control Centre,
the standalone system monitor, bar `graph` widgets, and plugin views (`v1.KindGraph` maps onto it).
The owner chose this over a style flag on 2026-09-25: the column graph was an alpha design, and the bar
and panel should read as one system.

The painter draws, in order:

1. A 1px baseline across the lower edge in the `Track` colour, so a flat zero reads as data and not as
   an empty box.
2. An area under the primary series at 18% of its line colour.
3. The primary series as a smoothed polyline: quadratic curves through the midpoints of adjacent
   samples, stroked at 1.5 logical pixels and scaled by the surface's render scale.
4. The optional second series as a 1px stroke with no area fill.
5. A 3px dot on the newest primary sample, in the line colour, marking "now".

Anti-aliasing comes from `golang.org/x/image/vector`, already a direct dependency used by the text
path. No new dependency. The stroke is built as a filled outline (the polyline offset by half the stroke
width each side, joined with round caps) because `vector.Rasterizer` fills paths and has no stroker.

Samples map to x newest-at-right across the full box width: n samples span `W-1` pixels evenly, so a
short history is drawn narrower from the right edge rather than stretched. More samples than pixels
keeps the newest `W` samples, as today.

### D2. Colour

The owner overruled the design audit's one-accent budget on 2026-09-25.

| Role | Colour |
|---|---|
| Primary series, below the activity threshold | `Accent` |
| Second series (network upload, disk writes) | `Secondary` |
| Primary series at or above the activity threshold | `Tertiary`, through a new `ui.ToneActivity` |
| Primary series at or above the critical threshold | `Error`, through the existing `ui.ToneError` |

The node's `Tone` selects the primary line colour, and the shell sets it. The threshold tone also
colours that metric's value text, so the number and its line always agree. `ToneActivity` is a new
tone and not a new theme token. The theme has no warning role, `Tertiary` exists on every theme, and
adding a token would widen this change into the theme system.

Thresholds, activity / critical, from Noctalia: CPU 50/90 %, CPU temperature 60/85 °C,
GPU 50/90 %, GPU temperature 60/85 °C, memory 60/90 %, storage 80/95 %. Rates have no threshold.
Thresholds are fixed values in code, not configuration.

### D3. Scale

The shell still normalises before it builds the node, as `monitorGraphValues` does today.

- Fraction sources (CPU, memory, GPU) keep a fixed 0–1 scale.
- Temperature uses a fixed 20–100 °C scale, so a change in the room does not rescale the line.
- Rates (network, disk I/O) scale to the larger of the window's peak × 1.1 and a floor: 64 KiB/s for
  network, 1 MiB/s for disk. The two series of one chart share one scale. The floor keeps idle noise
  flat instead of drawing a 2 KB/s blip as a saturated link.
- Every rate chart carries its scale as text: a `peak 12 MB/s` caption in its row. An auto-scaled
  chart without a reference misstates magnitude.

A missing sample is not a zero. The shell passes only real samples, and an absent source keeps
`Absent: true`, which paints nothing.

### D4. Page layout

Everything fits the 596 × 480 body without scrolling. The page keeps the Control Centre scroll
viewport for consistency, and its content height equals `ccPageH`.

```
┌──────────────────────── 355 ───────────────────────┐ 13 ┌──────── 228 ────────┐
│ ▣ CPU                                   12%   [↗]  │    │ ▣ Memory        39% │
│ ╭╮╭─╮  ╭╮     ╭──╮            64px line + area    •│    │ ████████░░░░░░░░░░░ │  168
│─╯╰╯ ╰──╯╰─────╯  ╰─────────────────────────────────│    │ 12.2 / 31.3 GiB     │
│ 63°C · 4.21 GHz · load 0.82 0.74 0.66              │    │ ██░░░░ swap 0.4/8 G │
└────────────────────────────────────────────────────┘    │ ~~~~~~ 28px line    │
                                                           └─────────────────────┘
                                   13
┌───────────────────────────────────── 596 ──────────────────────────────────────┐
│ ▣ Temperature   CPU 63°C · GPU 51°C              ~~~~~~~~~~~~~~~~~~~~~~~~~~~~  │
│ ▣ GPU           18%                              ~~~~~~~~~~~~~~~~~~~~~~~~~~~~  │
│                 AMD Radeon RX 7900 XTX                                         │  299
│ ▣ Storage       412 / 931 GiB                    ████████████░░░░░░░░░░░░░░░░  │
│                 / · 44%                                                        │
│ ▣ Network       ↓ 1.2 MB/s  ↑ 84 KB/s            ~~~~~~~~~~~~~~~~~~~~~~~~~~~~  │
│                 enp7s0 · peak 12 MB/s            (two series)                  │
│ ▣ Disk I/O      R 3.1 MB/s  W 0.4 MB/s           ~~~~~~~~~~~~~~~~~~~~~~~~~~~~  │
│                 dm-0 · peak 48 MB/s              (two series)                  │
└────────────────────────────────────────────────────────────────────────────────┘
```

- **Heroes, 168px tall.** The columns reuse Home's `ccLeftColumnW` (355) and `ccRightColumnW` (228)
  with `MarginL` between them, so the two pages share one grid. CPU, the metric that moves, takes the
  wide card and a 64px sparkline. Memory takes the narrow card: its value, a `KindMeter` capacity bar
  with the used and total bytes, a thinner swap meter, and a 28px sparkline.
- **One card of five rows below**, `MarginL` under the heroes, filling the remaining 299px. The rows
  are not separate cards; there is one containment layer. Each row is about 50px with `MarginS` between
  rows: an icon, a fixed-width label, a value line with a caption line under it, and the row's mark on
  the right at 28px tall.
- **The mark fits the data.** Temperature, GPU, network and disk I/O are sparklines. Storage is a
  `KindMeter`, because capacity has no interesting history. Network and disk I/O draw two series.
- **Values are tabular** (`Tabular: true`) and sit in a fixed-width column, so 1-second updates do not
  shift the row.
- **Absent is an em dash.** A row whose source is unavailable keeps its place, shows `—`, and paints
  no mark. No row is hidden, so the page does not reflow when a sensor appears.
- **The standalone monitor is one control away.** The CPU hero's title row carries an icon button named
  "Open system monitor", which opens `PanelMonitor` through the existing action. The page adds no
  section label under the header: the header already says "Monitor".

Row content, where the sysc-metrics v0.5.1 snapshot supports it:

| Row | Value | Caption | Mark |
|---|---|---|---|
| CPU hero | usage % | CPU °C · mean core GHz · load 1 / 5 / 15 | usage, 64px |
| Memory hero | used % | used / total, swap used / total | used-fraction meter, swap meter, usage 28px |
| Temperature | CPU °C · GPU °C (GPU part omitted if it reports none) | the CPU sensor source | CPU °C |
| GPU | usage % | GPU name · VRAM used / total (after the v0.6.0 pin) | usage |
| Storage | used / total for `/` | `/` · percent | meter |
| Network | ↓ rx · ↑ tx | interface · peak | rx and tx |
| Disk I/O | R read · W write | device · peak | read and write |

CPU frequency is already in v0.5.1: `CPUSnapshot.Cores[i].FrequencyHz` with `FrequencyValid`. The caption
shows the mean of the valid cores in GHz to two decimals, and drops the part when no core is valid.

GPU VRAM is not in v0.5.1, and dgop, the behavioural reference, has no VRAM collector either. It is
planned in `sysc-metrics` `docs/plans/2026-09-25-gpu-vram.md` for `v0.6.0` and gated by `sysc-521`. Until
the shell pins that release, the GPU caption carries the name only; the pin bump and the VRAM caption are
the last task of this work and wait on the gate.

### D5. Leases and subject selection

The Control Centre additionally leases, at the same one-second interval and for the same lifetime as
its existing leases: `Filesystem` for `/`, subject-less `Network`, and subject-less `Block`. This fixes
the missing network ring, and history accumulates from the moment the Control Centre opens.

The per-subject rate rings need a named subject. Once a snapshot carries interfaces and devices, the
Control Centre resolves one primary interface and one primary device and acquires their two direction
leases (`rx`/`tx`, read/`write`). It re-resolves only when the chosen subject disappears, so the chart
does not jump between devices while you watch it.

- **Primary interface:** the non-loopback interface with the most cumulative receive plus transmit
  bytes. On the desktop that is `enp7s0`; idle bridges and `veth` pairs carry little and lose. This
  replaces `ccNetworkSelector`'s alphabetical pick.
- **Primary device:** the block device backing `/`, found by resolving the root filesystem's `Source`
  through `filepath.EvalSymlinks` to its `/dev/<name>` (`/dev/mapper/ArchinstallVg-root` resolves to
  `dm-0`). If that fails, the device with the most cumulative bytes, skipping `loop`, `ram` and `zram`
  devices.

Both pickers are pure functions over the snapshot and are table-tested.

### D6. Motion

The chart redraws only when a new sample invalidates the panel. It does not animate between samples,
and it does not pulse on a threshold crossing. This follows the repository rule to draw only after
invalidation, and it needs no reduced-motion variant.

## Out of scope

- Per-core CPU, a process list on this page (the standalone monitor and process panel own those),
  and network session totals.
- Configurable thresholds, history windows or chart colours.
- The Home weather scene inset. It is its own issue.

## Verification

- `internal/render`: table tests for sample-to-point mapping, midpoint smoothing, and the short-history
  width rule. A raster test that the stroke has partial-coverage edge pixels (anti-aliased) and stays
  inside its box. A test that `Absent` and empty series paint nothing. A test that `ToneActivity` and
  `ToneError` select `Tertiary` and `Error`.
- `internal/shell`: table tests for the primary-interface and primary-device pickers, including the
  `br-*` / `veth*` / `lo` case from this machine. Tests for the threshold tone per metric, the rate
  scale floor and headroom, and the peak caption. A layout test that the Monitor page lays out at
  596 × 480 with no child past the viewport, the same contract `TestControlCentreHomeChildrenFitItsViewport`
  holds for Home. A test that an absent source renders `—` and keeps its row.
- Existing bar `graph` widget and plugin view tests stay green against the new painter.
- Live Niri: open the Control Centre Monitor section by IPC and capture it. Every row has a value or
  `—`, the network row names `enp7s0` and draws, and nothing is clipped. Capture a bar `graph` widget
  in the same run to confirm the new line style there.

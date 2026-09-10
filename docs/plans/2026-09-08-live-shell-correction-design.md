# Live Shell Correction — Design

Date: 2026-09-08. Owner-approved scope and direction.

This pass corrects the shell that was exercised on the two-output Niri desktop. It reuses the shipped panel, service, plugin, theme, and bar seams. It adds one renderer primitive for compact gauges and one upstream process collector because those behaviours do not exist.

## Decisions

| # | Decision |
|---|---|
| D1 | The SYSC mark uses a periodic theme ramp: Primary → Secondary → Tertiary → Primary. Sampling wraps across the ramp and the animator loops continuously, so there is no pinned colour block or reversal. Every output consumes the same resolved palette. Reduced motion parks at a stable phase. |
| D2 | `GradientPaint` remains host-owned on `ui.Node`; plugin JSON stays unchanged. Rects and alpha masks continue to share the sampler. |
| D3 | The audio failure is fixed at its source. `pw-dump` metadata values are decoded selectively from `json.RawMessage`; unrelated numeric or boolean settings cannot reject the snapshot. Poll failures become visible panel state instead of disappearing. `wpctl` remains the write path. |
| D4 | `PanelAudio` keeps the approved Volumes/Devices information architecture and uses existing controls. It becomes responsive: roughly one-third of output width and two-thirds of output height, capped near 1120×992, with a practical minimum. Cards use the available width, the close action stays at the far right, and the body alone scrolls. |
| D5 | Notification body height is derived from measured header, filter, padding, and gaps. Rebuilds keep descendants inside the mapped surface. The bar indicator is one fixed-size Material bell; unread state is a 6 px Error dot painted inside the icon bounds and never changes pill width. |
| D6 | Add `ui.KindRadialGauge` as the one missing visual primitive. The default sysmon capsule shows four compact 22 px gauges: CPU, memory, CPU temperature, and GPU usage. Original project-owned CPU, memory, and GPU vector glyphs sit inside their rings; temperature alone shows a measured, centred `NN°` value. Rings use analytic pixel coverage, a light stroke, and spatial theme gradients so they remain smooth at fractional output scale. Exact names and values live in tooltips. An unavailable source renders an honest unavailable state without changing geometry. |
| D7 | Right-click anywhere in the sysmon capsule opens `PanelMonitor`. Its initial page is **System Processes**; **System Monitor** is the second top-level page and reuses the existing cards and telemetry. |
| D8 | Process sampling belongs in `sysc-metrics`, activated only while its consumer is open. It scans `/proc` sequentially, keys samples by PID plus start time, derives CPU from successive samples, and reports RSS. Before TERM or KILL, the shell re-reads start time and refuses a recycled PID. No new dependency or daemon is introduced. |
| D9 | The process page provides All/User/System filters, search, sortable Name/CPU/Memory/PID columns, selectable table rows, and a virtual list. One shared table surface replaces per-process capsules. Resting rows stay transparent; hover, press, and selection use a flat edge-to-edge state wash, with no individual rounded outline. One visible `Kill` action sends `SIGTERM`; the panel does not expose a disabled second-stage `SIGKILL` control. It reports permission and vanished-process failures inline. |
| D10 | Weather uses the shipped `org.sysc.weather` reference plugin. Install and enable it with Melbourne coordinates from the existing user configuration, then correct only defects demonstrated live. The control-centre weather consumer stays separate. |
| D11 | Add a `launcher` built-in item at the first position of the default left section. It opens `PanelLauncher`. Use the supplied ghost SVG through the existing icon asset path because the Cowboy Bebop raster loses its shape at bar size. |
| D12 | Existing tracker records remain authoritative (`sysc-249`, `sysc-151`, `sysc-103`, `sysc-82`, `sysc-54`, `sysc-121`, `sysc-72`). This correction does not create a parallel task tree. |

## Geometry and interaction contract

- Audio content uses the configured output logical size, clamped to `min(max(outputW/3, 720), 1120)` by `min(max(2*outputH/3, 640), 992)`. It retains the attached top edge and clamps within output padding.
- Audio header and tab strip remain fixed; Volumes and Devices own the only scroll viewport. Volume rows are `[identity] [role/name + full-width slider] [value] [mute]` and device choices are full-width selected wells.
- Notification layout computes body height after laying out its fixed chrome. No child may extend below the panel content box.
- Gauges are 22 px circular tracks with a light, analytically antialiased progress arc. CPU, memory, and GPU arcs interpolate spatially from theme Accent to Secondary; their centres contain distinct project-owned monochrome vector glyphs sized for the ring. Temperature alone contains one compact measured `NN°` run. Its arc starts at Accent and ends at a heat colour that remains Accent below 60°C, transitions smoothly to a contrast-aware renderer-local amber at 75°C, then to theme Error at 85°C and above. The track keeps the theme Track colour. Exact names and values appear in tooltips. Pointer action belongs to the shared capsule; gradients are driven by position and value and do not animate.
- The monitor page and search/filter controls are 28 px high. Column headers are 22 px high and read as table labels rather than filled pills. The virtual list paints one rounded `FillContainerHigh` table surface. Each process uses a transparent 26 px row on a 32 px pitch, so typography and column alignment create the rhythm instead of repeated chrome. The full row owns selection and focus; hover and press add a quiet flat state layer, and selection adds one rectangular `FillSoft` wash. The independently clickable outlined `Kill` action is 22 px high and no wider than its measured label needs.
- The launcher is the first bar child at the extreme left. Keyboard launch behaviour and the launcher panel itself are unchanged.

## Verification

Focused unit checks cover heterogeneous PipeWire metadata, audio snapshot error state, audio responsive geometry, notification containment, fixed notification indicator width, periodic gradient continuity, radial gauge painting/layout, default bar ordering, process delta/identity validation, and weather plugin configuration. The live Niri gate runs on DP-1 and DP-3: inspect the animated mark, operate real audio devices and streams, open every corrected panel, exercise process TERM on a harmless test process, verify weather, and launch from the far-left icon.

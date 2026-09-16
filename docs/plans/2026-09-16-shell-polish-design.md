# Shell surface polish (M10) design

Date: 2026-09-16. Owner-approved in the preceding design discussion.

This is the umbrella design for the shell-polish follow-up commissioned by
`sysc-309`. It turns the execution handover into seven small design/plan
boundaries. The handover remains the historical receiving-state record; this
document and its children are the decisions to implement.

## Goal

Make the existing Niri shell read as one coherent product at bar and control
centre size while preserving truthful service ownership, keyboard access,
failure states, and the current Go/Wayland architecture.

M10 is a polish and integration milestone. It does not pull forward later
roadmap features, add compositor support, or replace an existing service with a
second implementation.

## Design set

| Design | Scope | Existing owner or gate |
|---|---|---|
| [`2026-09-16-metrics-control-centre-gpu-design.md`](2026-09-16-metrics-control-centre-gpu-design.md) | Four truthful Home gauges and one GPU path shared with System Monitor | `sysc-121`; pinned `sysc-metrics@v0.4.0` |
| [`2026-09-16-bar-weather-launcher-workspaces-design.md`](2026-09-16-bar-weather-launcher-workspaces-design.md) | Weather pill, production launcher artwork, and Noctalia-style workspace shapes | `sysc-277`/`sysc-293`; `sysc-82`; Niri projection owner |
| [`2026-09-16-centre-clock-media-wordmark-design.md`](2026-09-16-centre-clock-media-wordmark-design.md) | One visible time/date/media composition with a fixed SYSC anchor | M10 centre-composition child |
| [`2026-09-16-notifications-battery-urgency-design.md`](2026-09-16-notifications-battery-urgency-design.md) | Urgency styling and a deduplicated battery-warning producer | M10 notification child; qualified `sysc-notify` change |
| [`2026-09-16-owned-tray-termination-design.md`](2026-09-16-owned-tray-termination-design.md) | Service-owned tray termination and shell acknowledgement | M10 tray child; qualified `sysc-tray` change |
| [`2026-09-16-control-centre-network-design.md`](2026-09-16-control-centre-network-design.md) | Existing Network page as a functional control-centre route | `sysc-157`, `sysc-254`, `sysc-268` |
| [`2026-09-16-shell-polish-design.md`](2026-09-16-shell-polish-design.md) | Integration, ownership, sequencing, and the M10 exit gate | `sysc-309` |

The three bar concerns are grouped because they share the widget/projection,
icon, density, and native-bar gate. Metrics and GPU are grouped because the
same `services.Snapshot` and selector leases must serve both consumers.
Clock/media/wordmark, notification/battery, tray termination, and Network stay
separate where their ownership or external protocol risk differs.

## Scope boundary

In scope:

- the four Home System gauge slots: CPU usage, memory usage, CPU temperature,
  and GPU usage;
- the shared GPU path through `sysc-metrics`, `services.Metrics`, System
  Monitor, and the control centre;
- the weather bar's visible icon/value row;
- the supplied launcher PNG as the production launcher mark;
- number-free workspace shapes with preserved Niri focus, occupancy, urgency,
  hit, and accessibility state;
- one visible time/date composition, media at its right edge, and a fixed
  wordmark centre;
- low, normal, and critical notification styling;
- battery warning state transitions through a qualified notification producer;
- service-owned tray termination through a qualified `sysc-tray` release;
- the existing NetworkManager-backed page as a control-centre destination.

Out of scope:

- a second compositor, a lock screen, or support for a compositor other than
  Niri;
- a second metrics, weather, notification, tray, or NetworkManager service;
- arbitrary process signalling from the shell;
- a second output qualification, laptop deployment, or a claim about hardware
  that is not present on the receiving machine;
- new launcher search behaviour, new media protocol behaviour, NetworkManager
  forget-gesture policy, notification-centre layout redesign, or later M9
  sub-projects;
- copying Noctalia, DMS, or their C++/QML implementation or configuration.

## Approaches considered

### One large implementation plan

Putting all nine handover workstreams into one plan would make the default bar,
Registry wiring, and external service gates one review unit. It would also make
an unrelated upstream protocol delay hold the visual slices and make ownership
unclear when a focused check fails. It is rejected.

### Seven focused designs with one integration boundary

The approved approach keeps each service or visual contract small, groups only
the consumers that share a real owner, and gives the centre composition,
external protocol gates, and final merge one explicit integration child. Each
design receives its own executable plan before code starts. This is the M10
shape.

### A new shell-wide presentation layer

A general layout or notification abstraction could make the seven areas look
uniform, but it would move rules out of `internal/ui`, `internal/shell`, and the
services that already own them. It is rejected. Existing retained nodes,
`ArrangeBar`, selector leases, cached service state, and panel hosts are enough.

## Decisions

### D1 — M10 is a dependency-aware set of vertical slices

The seven documents are independently reviewable, but they converge through
one Registry/bar integration owner. A slice may not silently absorb a defect
already owned by `sysc-121`, `sysc-82`, the weather issues, or the network
issues. New tracker children are limited to the centre composition,
notification/battery work, tray termination, and the integration/exit gate.

### D2 — Existing owners stay responsible for state and I/O

`sysc-metrics` owns collection, `services.Metrics` owns leases and the shared
snapshot, `services.Network` owns NetworkManager state, `sysc-notify` owns
notification records and publication, and `sysc-tray` owns tray item identity
and process operations. `Registry.mu` protects projections only. D-Bus,
filesystem, process, HTTP, and external socket work stays off that lock and off
the Wayland dispatch goroutine.

### D3 — Truthful unavailable states are part of the visual contract

Every metric, weather reading, battery state, network value, notification
operation, and tray operation has an explicit unavailable or failed path. A
missing GPU is an unavailable gauge, not `0%`; a failed weather fetch keeps an
aged observation or reports an error; a failed tray termination leaves the item
visible; a missing notification producer does not create a shell-only fake
card.

### D4 — Visual parity uses behaviour and measured geometry, not imported code

Noctalia and DMS remain references for the workspace shape language, urgency
hierarchy, and useful content. The implementation uses the existing semantic
theme, density, shape, renderer, and retained-tree primitives. A new primitive
is justified only where one of these approved consumers needs it and the
existing tree cannot express the invariant.

### D5 — External protocol changes land before shell integration

The battery producer and tray termination designs each have an upstream
qualification gate. The shell does not import a local replacement, spoof a
presenter command, inspect an untrusted PID, or pin a moving branch. The
compatible tagged release, protocol tests, daemon tests, and shell-side fake
seam must exist before the corresponding integration task is executable.

### D6 — The fixed centre anchor is an invariant, not a visual preference

The centre wordmark's logical centre is the centre of the bar content band for
all fitting combinations of side widths, clock text, and media presence. The
implementation must prove this with a layout test that varies those inputs;
visual similarity alone is not evidence.

## Data flow

```text
Niri JSON ──> internal/platform/niri ──> Registry projection ──> workspace bar tree

sysc-metrics ──> services.Metrics leases ──> one Snapshot ──> bar / System Monitor / CC

NetworkManager ──> services.Network cache ──> PanelNetwork body ──> CC Network route

sysc-notify presenter ──> notifyclient ──> notifyState ──> cards / toasts / bar
Battery Snapshot ──> warning reducer ──> qualified notify producer ──┘

sysc-tray ──> trayclient/trayState ──> menu Close command ──> service-owned lifecycle delta
```

The Wayland owner consumes immutable tree/state copies. It never calls a
service or a process operation while holding `Registry.mu`.

## Verification and live gate

Each child design names its smallest focused checks. The merged M10 gate then
runs the affected package checks, `gofmt`, `go vet`, the relevant race checks,
and the repository's module-diff check. The full-tree race command remains
subject to the existing machine limitation and must be reported rather than
silently replaced with a claim.

The live target is the receiving bare-metal Niri session only: `DP-1`,
3440×1440, scale 1.0. The gate opens and closes the affected bar/panel
surfaces, checks `niri msg -j layers` before and after, and records the exact
binary and service releases. It may record the real NVIDIA RTX 4060 GPU result
when `sysc-metrics` supplies it. It cannot claim two-output, laptop, or
credential/tray qualification that this machine cannot run.

M10 is complete only when:

- all seven designs have executable plans and their focused checks pass;
- the four Home gauges and standalone monitor use one truthful GPU snapshot;
- the launcher uses the supplied PNG bytes, not `ghost`, the wordmark, or a
  visual substitute;
- the workspace tree contains no painted workspace number;
- the centre wordmark anchor survives width and media changes;
- battery and tray operations have their required qualified service releases;
- the Network rail routes to the existing service/body without a second model;
- the live Niri result and every hardware-limited omission are recorded in the
  M10 completion handover and the corresponding Beads issues.

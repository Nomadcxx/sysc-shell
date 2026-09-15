# Shell surface polish execution handover

Date: 2026-09-16. Commission: sysc-309.

This handover commissions the next implementation tranche after surface
stacking landed on main. It covers the remaining bar, control-centre,
notification, tray, and system-monitor polish requested by the owner. Status
belongs in bd; this document records the receiving state, contracts, and
completion gates.

## Receiving state

- main and origin/main are at 83e54ab. Surface stacking is merge commit
  07c8544; its completion snapshot is
  [2026-09-16-surface-stacking-completion-handover.md](2026-09-16-surface-stacking-completion-handover.md).
- The blur persistence correction is already included. Blur is the default in
  every preset; explicit opt-outs and radius values survive config writes and
  preset rebases. Do not turn it back into an opt-in feature.
- The receiving checkout is bare-metal Niri only today: output DP-1,
  3440x1440, scale 1.0. The laptop is unavailable and must not be deployed to
  or tested in this tranche. There is no second output here, so do not claim a
  two-output qualification.
- Use a dedicated worktree from main. The primary checkout has unrelated
  local changes; do not clean, stash, or fold them into this work.

Read AGENTS.md, docs/plans/README.md, and bd show sysc-309 before claiming
work. Preserve existing issues that already own a defect; create a new child
or discovered-from:sysc-309 issue only when the current graph has no owner.
Do not create a parallel tracker or a second service.

## Invariants

- Keep Niri and Go as the supported product boundary. Use existing shell
  services, UI nodes, renderer paths, theme roles, and panel ownership.
- Every displayed metric is sourced from an actual snapshot and selector.
  Missing hardware or a failed read is an unavailable state, never a guessed
  number, random SVG, or stale success disguised as zero.
- Keep the Wayland dispatch loop on one goroutine. D-Bus, process, filesystem,
  network, and notification operations remain outside Registry.mu.
- Start each non-trivial owner change with one focused failing check. Keep
  tests at the smallest responsible package, then run the affected-package
  gate. Do not broaden this into unrelated roadmap work.
- If sysc-metrics or sysc-tray needs a public change, record and qualify that
  change in its own repository before pinning it here. No shell-side protocol
  spoofing or arbitrary process signalling.

## Current code map

The following observations are the starting point, not permission to duplicate
an owner:

- internal/shell/controlcenter_pages.go:ccHome currently builds only CPU and
  Memory radial gauges. ccMonitor still projects CPU, Memory, and Network
  graph cards.
- internal/services/metrics.go already carries Snapshot.GPU, SourceGPU,
  Snapshot.Fraction, and thermal data through the pinned sysc-metrics module.
  internal/shell/popout_monitor.go already has GPU projection helpers, but the
  GPU selector must be present in the panel's leased set and visible in the
  final composition.
- internal/shell/popout_controlcenter.go lists Network as disabled and ccPage
  has no functional Network route. PanelNetwork and its shared service/body
  already exist.
- internal/shell/widget.go still has the numbered workspace-capsule
  projection. internal/ui/bar.go centers the whole center section; changes to
  asymmetric center content can move the wordmark unless the layout contract
  is corrected.
- internal/shell/weatherwidget.go already has an icon/text row on the current
  branch, but the visible live result and pill alignment still need to be
  checked. Keep the accessible name while removing redundant visible wording.
- internal/shell/notifycard.go gives special tone treatment to critical
  urgency only. internal/shell/notifywidget.go changes the notification icon
  state but has no separate DMS-style urgency treatment or battery warning
  producer.
- internal/shell/traydrawer.go, internal/shell/traymenuhost.go, and
  internal/shell/trayactions.go consume sysc-tray v0.1.0-rc.1. That protocol
  has activate, scroll, menu, and preference commands, but no terminate
  command.
- The supplied launcher reference is
  /home/nomadx/Pictures/sysc-aperture-c-nested-gates.png. The Noctalia
  workspace reference is
  /home/nomadx/noctalia/src/shell/bar/widgets/workspaces_widget.cpp.

## Workstreams

### 1. Make the control-centre system gauges truthful and complete

Trace the existing metric lease and snapshot path before editing ccHome,
ccMonitor, or the monitor service. The Home System section must contain four
real radial gauge items corresponding to the supported system monitor
selectors. The shipped default metric group is the reference set: CPU usage,
memory usage, CPU temperature, and GPU usage. Confirm the exact selector
vocabulary against the current services.Snapshot and standalone monitor panel
before fixing the projection.

Use the existing KindRadialGauge and theme-derived rendering. Keep the gauge
size, unavailable state, clamping, icon centring, and semantic tooltip
contract. Acquire GPU through the shared metrics service; do not add a second
GPU reader in the control centre. A GPU or thermal source that is absent
remains visibly unavailable while retaining its labelled slot.

Focused proof must cover four Home gauges, selector leases, real CPU/memory
values, and unavailable states. The shared GPU investigation and its
standalone-panel consumer are a separate workstream below. Include the
bare-metal read result in the completion record.

### 2. Correct the weather bar pill

Keep the weather service and shared weather vocabulary. The bar should show
only the condition SVG/icon and temperature in its visible pill; the word
Weather is not content. It may remain the accessible node name and a useful
tooltip title.

Measure the icon and temperature as one row using the existing density
metrics. Centre the icon in its actual painted box and give the pill the same
intentional inset on every side. Fix the shared construction site rather than
adding a weather-only pixel offset. Preserve placeholder, error, stale-age,
unit, and click-to-weather-panel behaviour.

Add a focused tree/layout or raster check for the absence of visible label
text and the icon/text insets. Verify the result in the bare-metal bar.

### 3. Give notifications urgency colour and add battery warnings

Trace the existing sysc-notify presenter path and notification card
composition before touching it. Low, normal, and critical notifications must
have distinct, theme-derived visual roles in the card's meaningful elements,
with critical retaining an accessible error treatment. Match the useful DMS
idea without importing its implementation or adding fixed RGB values. Unread
state must remain legible and must not change the bar item's measured width.

Add battery warning notifications through the owner that can actually publish
notifications. The warning must be based on the service-owned battery
snapshot, deduplicated across sampling ticks, hysteretic around the threshold,
and resolved or superseded when charging/full state returns. Do not make the
shell repeatedly emit a new notification on every sample. If the current
protocol lacks the required sender or replacement semantics, record that as a
sysc-notify change and qualify it there before shell integration.

Focused proof must cover all urgency tones, read/unread geometry, low-battery
enter/hold/recovery transitions, reconnects, and notification service failure.
Keep the existing notification centre and toast behaviour intact.

### 4. Align and redraw the far-left launcher mark

Use the supplied nested-gates image as the visual reference at actual bar
pixel size. Trace the existing ghost asset and icon/font path first. Fix the
mark's intrinsic bounds and its pill alignment at the responsible
renderer/widget boundary; do not compensate with a one-off bar translation.
If the current catalogue cannot express the reference, add one project-owned
vector/icon asset through the established asset pipeline.

The launcher remains the far-left accessible button and opens PanelLauncher.
Its visual centre must remain stable at fractional and native scale. Add a
focused bound/centre check and one bare-metal capture.

### 5. Replace numbered workspace capsules with Noctalia-style shapes

Use the Noctalia source above as behaviour and geometry reference, not as a
code or compatibility target. Replace the prototype's visible workspace
numbers with the requested shape language: the open/focused workspace has a
larger flexible element, other available workspaces have smaller elements, and
no workspace number is painted. Preserve Niri ordering, click targets,
keyboard accessibility, urgent state, and the current workspace transition.

Do not solve this by hiding the digits inside a smaller node. The projected
tree and painter must contain no workspace numbering. Add a focused
projection/layout test that asserts the active-to-inactive size relationship,
no numeric text, stable hit targets, and correct updates as occupancy/focus
changes. Check the native bar on DP-1.

### 6. Make the centre composition one coherent clock/media/wordmark unit

The current default centre contains separate time and date clock items. Make
the visible time/date presentation one widget and connect it visually to the
fixed SYSC wordmark. Put the media widget in the bar's centre section at the
far right of that centre composition; when no player exists it collapses
without leaving a misleading empty slot.

Before coding this slice, run a short design brainstorm using the existing bar
chrome and renderer. Compare at least the plausible connected
time/date-to-wordmark arrangements at bar pixel size, choose the smallest one
that preserves the product request, and record the choice and rejected
alternative in bd or a registered design document. Do not introduce a
general layout toolkit for this one composition.

The wordmark is an invariant anchor, not a flexible child. Changing the width
of time/date, media metadata, left widgets, or right widgets must not change
its centre x-coordinate. A simple MinWidth or symmetric-padding assumption is
insufficient: add a ui.ArrangeBar/shell layout test that varies those widths
and asserts the wordmark remains fixed. Keep the wordmark accessible,
theme-driven, and unaffected by media absence or long titles.

Focused proof must cover one constructed time/date widget, media placement and
collapse, collision/truncation behaviour, and the fixed-wordmark invariant.
Verify the final composition on DP-1.

### 7. Terminate tray applications only through an owned protocol

Right-clicking a tray item and choosing Close must not merely hide the item.
The item should disappear as the result of its process/service terminating,
and the shell must not claim success before that state arrives.

The current pinned sysc-tray protocol has no safe terminate command. Do not
add os.Kill to the shell, signal an arbitrary PID, or infer ownership from an
untrusted tray field. First define the service-owned operation in
/home/nomadx/sysc-tray: validate same-UID ownership and PID/start-time
identity, use the service's real lifecycle authority, reject stale or
recycled identities, and provide a bounded graceful termination path with a
clear unsupported result where the item cannot be owned. Add protocol and
daemon tests there, publish/pin a compatible release, then add the shell menu
action and acknowledgement/error projection.

Track the cross-repository gate in bd before shell integration. Shell tests
must prove stale identities are refused, a hidden-only removal is not reported
as Close, and a successful Close is followed by the tray delta or process
exit. Do not disturb unrelated tray applications during the bare-metal gate.

### 8. Enable the existing Wi-Fi panel in the control centre

Make the Network rail destination functional and route it to the existing
NetworkManager-backed service/body used by PanelNetwork. Do not embed a
standalone panel inside the control centre or create a second connection,
secret store, signal loop, or device model.

Preserve the status-first layout, credential lifetime rules, off-owner
mutations, and current bar wifi action. Update the rail enabled state, ccPage
dispatch, lifecycle handoff, focus, and error paths. Keep existing sysc-157,
sysc-254, and sysc-268 ownership rather than duplicating their issues.

Focused proof must cover rail enablement, page dispatch, shared snapshots,
safe reconnect, no leaked panel surface, and no credential retention. The
bare-metal gate may inspect status and open/close behaviour; do not toggle or
forget a real network merely to create evidence.

### 9. Make the shared GPU path visible and diagnosable

Qualify the complete path from the pinned sysc-metrics GPU reader through
services.Metrics and its SourceGPU lease to the standalone System Monitor and
the control-centre gauges. Use the actual hardware on DP-1 as evidence,
including whether DRM/sysfs or an NVIDIA-specific reader supplies usage and
whether a GPU is present at all.

Add focused fixtures or injected snapshots at the layer where a defect is
found. The shell must select a deterministic GPU when more than one exists,
preserve Valid versus unavailable, and never display a fabricated zero or a
random icon. If the defect is upstream, file and qualify the sysc-metrics
change before updating the shell pin; if it is in shell leasing, projection,
or rendering, fix that owner and keep one shared snapshot for both consumers.

The acceptance check is one real GPU value reaching both the System Monitor
and CC when hardware provides it, plus a truthful unavailable state when it
does not. Record the reader, selector, and live result on sysc-309.

## Integration and completion gate

Workstreams may be split across disjoint worktrees, but Registry wiring, bar
composition, the icon inventory, and control-centre dispatch need one
integration owner. Review the merged diff for duplicated data paths, wordmark
drift, fake metrics, hidden-only tray removal, and lock/Wayland violations
before declaring the epic complete.

Run the smallest affected-package checks during each slice. Before the final
merge, run the repository gates required by AGENTS.md:

    gofmt -w .
    test -z "$(gofmt -l .)"
    go vet ./...
    go test -race -count=1 ./...
    git diff --exit-code -- go.mod go.sum
    git diff --check

For the bare-metal Niri evidence, use the environment from AGENTS.md, capture
the bar and each affected panel at native scale, and compare niri msg -j
layers before and after each open/close path. Record hardware limitations
honestly: this tranche has no laptop and no second output.

When the requested work is complete, write a new completion snapshot, register
it, record the exact gate output and live observations on sysc-309, close the
issue only when its acceptance criteria are met, and leave this execution
handover unchanged.

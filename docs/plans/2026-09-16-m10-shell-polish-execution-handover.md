# M10 shell surface polish execution handover

Date: 2026-09-16. Commission: `sysc-309`.

This is the current receiving handover for Milestone 10. It commissions the
implementation of the approved seven-design set and gives the next owner the
source seams, ownership boundaries, release gates, and live evidence limits.
Beads owns status. This document records the work boundary and the facts that
should survive a change of session.

The older
[`2026-09-16-shell-polish-execution-handover.md`](2026-09-16-shell-polish-execution-handover.md)
is historical. Leave it unchanged. Do not use its receiving commit or its
working-tree observations as the current base without checking `main`.

## Start here

Work from `/home/nomadx/sysc-shell` for Beads commands. Read these in order:

1. `AGENTS.md`.
2. [`docs/plans/README.md`](README.md), then the M10 section of
   [`docs/roadmap.md`](../roadmap.md).
3. The umbrella design and each focused design listed below.
4. The historical handover named above, for the reasons behind the current
   boundaries only.
5. `bd show sysc-309`, `bd ready`, and `bd blocked` from the primary checkout.

The design set landed in commit `6f04574`. Later commits have moved `main`, so
verify the checkout with `git status -sb` and `git rev-parse HEAD`. Preserve
local work that is not part of M10. Do not reset, clean, stash, or stage files
belonging to another slice.

The current checked-in module and README pins are the authority:

| Module | Pin | Role in this tranche |
|---|---|---|
| `github.com/Nomadcxx/sysc-wayland` | `v0.2.2` | Pure-Go Wayland client. |
| `github.com/Nomadcxx/sysc-metrics` | `v0.4.0` | Shared telemetry and GPU reader. |
| `github.com/Nomadcxx/sysc-notify` | `v0.1.0-rc.3` | Notification daemon and presenter protocol. |
| `github.com/Nomadcxx/sysc-tray` | `v0.1.0-rc.1` | Tray daemon and presenter protocol. |

Re-check `go.mod`, `README.md`, and `AGENTS.md` before changing a pin. Do not
copy an older version from a handover or add a local `replace` directive.

## Design and plan boundary

The seven registered designs are:

- [`2026-09-16-shell-polish-design.md`](2026-09-16-shell-polish-design.md),
  the umbrella design for ownership, sequencing, data flow, and the exit
  gate;
- [`2026-09-16-metrics-control-centre-gpu-design.md`](2026-09-16-metrics-control-centre-gpu-design.md),
  four truthful Home gauges and one shared GPU snapshot;
- [`2026-09-16-bar-weather-launcher-workspaces-design.md`](2026-09-16-bar-weather-launcher-workspaces-design.md),
  weather bar chrome, the supplied launcher artwork, and number-free
  workspace shapes;
- [`2026-09-16-centre-clock-media-wordmark-design.md`](2026-09-16-centre-clock-media-wordmark-design.md),
  one clock/date composition, optional media, and the fixed wordmark anchor;
- [`2026-09-16-notifications-battery-urgency-design.md`](2026-09-16-notifications-battery-urgency-design.md),
  urgency roles and the service-owned battery warning;
- [`2026-09-16-owned-tray-termination-design.md`](2026-09-16-owned-tray-termination-design.md),
  owned tray termination and state-arrival acknowledgement; and
- [`2026-09-16-control-centre-network-design.md`](2026-09-16-control-centre-network-design.md),
  the existing Network page as a Control Centre destination.

No M10 executable implementation plan exists yet. Before product code changes,
write and commit seven executable plans, one for each design above. The plans
must carry the `superpowers:executing-plans` header, name exact files and
tests, start with the smallest failing checks, and define commit boundaries.
Register every plan in `docs/plans/README.md` in the same docs-only commit.
Land the design and plan documents on `main`; execute code from dedicated
worktrees after the plan commit is visible there.

The umbrella plan should own the integration review and exit gate. It should
not reimplement the six focused designs. The existing-owner work below stays
with its current Beads issue and contributes evidence to `sysc-317` and
`sysc-309`.

## Beads ownership

Claim the execution issue before changing its scope. Keep status in Beads and
record newly discovered work with `discovered-from:sysc-309` or the most
specific current owner.

Use the repository CLI from the primary checkout, for example:

```bash
bd update sysc-314 --status in_progress
bd close sysc-314 --reason "...evidence..."
```

The close reason should point to the focused checks and the relevant live or
upstream evidence. Do not copy that status into this document.

| Issue | Execution responsibility | Main acceptance boundary |
|---|---|---|
| `sysc-314` | Centre clock/date, media edge, and fixed wordmark | One time/date group, bounded media, stable accessible actions, and a wordmark centre invariant. |
| `sysc-315` | Notification urgency and battery warning | Theme roles, hysteresis, deduplication, reconnect handling, and a qualified producer. |
| `sysc-316` | `sysc-tray` termination qualification and shell acknowledgement | Service-owned identity checks, graceful bounded termination, and success only after the matching delta. |
| `sysc-317` | Cross-slice Registry/bar integration, review, final gates, and completion handover | All seven plans, all nine workstream proofs, live evidence, and truthful omissions. |

The following issues already own adjacent work. Consume their services and
projections instead of creating a second owner or a duplicate service:

- `sysc-121` owns metrics and System Monitor work. Its description contains
  older `sysc-metrics` v0.2.0 wording; the current `v0.4.0` API, source, and
  M10 metrics design take precedence.
- `sysc-82` owns the launcher bar follow-up.
- `sysc-277` and `sysc-293` own the weather parity and live-gate work. M10 bar
  work does not pull in hourly weather, a second weather service, or the
  separate weather-effects and weather-hero work (`sysc-312` and `sysc-318`).
- `sysc-157` owns the landed Network service and panel. `sysc-254` owns the
  unresolved forget-affordance decision, and `sysc-268` owns the deferred live
  credential gate. The M10 Network design only adds the Control Centre route.

Two unrelated open issues can affect the gate and must remain visible:

- `sysc-310` records a pre-existing media relay race in `internal/shell`.
  Establish the baseline before M10 changes and do not attribute that failure
  to a new centre composition without evidence.
- `sysc-313` records silent clipping when a right bar section exceeds the
  output width. The centre anchor must respect its collision contract, but
  this separate overflow defect does not close as an incidental M10 claim.

## Product and architecture invariants

The following rules apply to every slice:

- Go and Niri remain the supported product boundary. Do not add C++, Rust,
  Lua, Luau, Qt, QML, Quickshell, a compositor, or a lock screen.
- Keep Wayland types in `internal/platform/wayland` and Niri wire types in
  `internal/platform/niri`.
- Keep the Wayland dispatch loop on one goroutine. Relays publish immutable
  state through channels; the Wayland owner consumes a copied state snapshot.
- `Registry.mu` protects projections. D-Bus, process, filesystem, HTTP,
  notification, tray, and NetworkManager work runs outside that lock and
  outside the Wayland dispatch goroutine. Panel, tray, toast, and drawer
  `configure`, `render`, and `handle` methods already take the registry lock;
  preserve that convention.
- Draw only after invalidation and keep frame callbacks and buffer-release
  lifetimes intact.
- Use the existing services, leases, retained nodes, `ArrangeBar`, theme
  roles, density metrics, panel hosts, notification client, and tray client.
  Add a UI primitive only for an approved M10 consumer when the current tree
  cannot express its invariant.
- A missing, unleased, invalid, or failed reading is visibly unavailable. A
  valid zero remains a visible zero. The shell never turns missing GPU,
  thermal, battery, weather, network, notification, or tray state into a
  fabricated success.
- Preserve accessible names, roles, focus, hit targets, keyboard paths, and
  error signals while changing visible chrome.
- The shell never owns notification records, tray process identity, or tray
  lifecycle operations. It never parses or signals a tray PID and never adds
  `os.Kill` or an equivalent arbitrary process path.
- No M10 slice adds a general layout toolkit, a second service, a new media
  protocol, launcher search behavior, or Noctalia/DMS configuration
  compatibility.

## Workstream map

The nine areas below are the proof units behind the seven designs. The design
documents fix the decisions; use this map to find the existing behavior before
writing each plan.

### Metrics, Control Centre, and GPU

The shared source path is:

```text
sysc-metrics -> services.Metrics leases -> services.Snapshot
             -> bar metrics / System Monitor / Control Centre Home
```

Read first:

- `internal/services/metrics.go`: `Source`, `Selector`, `Snapshot`,
  `Snapshot.Fraction`, lease ownership, `SourceGPU`, and thermal state;
- `internal/shell/panelhost.go`: `monitorSelectors` and the Control Centre
  lease set;
- `internal/shell/controlcenter_pages.go`: `ccHome` and `ccResourceGroup`;
- `internal/shell/popout_monitor.go`: existing GPU card projection and
  selected-device display; and
- `internal/shell/metricwidget.go`: bar fraction and unavailable rendering.

The Home System section must expose four fixed slots in this order: CPU usage,
memory usage, CPU temperature, and GPU usage. CPU temperature uses the
existing `SourceCPU` plus subject `temperature` projection, with the visible
value in degrees Celsius and the ring fraction derived from the current
thermal contract. GPU usage uses the shared GPU source and a deterministic
device subject. More than one GPU requires stable ordering by non-empty PCI ID
and then name. An ambiguous identity stays unavailable.

The Control Centre must lease the four selectors alongside its existing
battery and clock leases. One `services.Metrics` sampling pass must serve all
consumers. Do not add a Control Centre GPU reader or a second collector.

Focused proof belongs at the smallest failing seam: four Home gauges, valid
zero versus unavailable, lease acquisition and release, deterministic
multi-GPU selection, and one injected GPU snapshot reaching both System
Monitor and Home. Qualify the pinned metrics reader against the receiving
machine before changing the dependency.

### Weather bar

`internal/shell/weatherwidget.go` already has a real icon/value row,
structured tooltip, `panel:weather` action, and the three-state placeholder,
error, and stale-reading behavior. Preserve those paths. M10's bar correction
keeps only the condition icon and temperature visible by default. The
accessible name `Weather`, tooltip facts, optional `ShowCondition`, unit, stale
age, and click behavior remain available.

Use the existing density row and shared weather vocabulary for the icon box,
gap, and capsule inset. Fix alignment at the row or widget construction site,
not with a weather-only translation. Do not reimplement weather decoding or
pull in the standalone weather panel, effects, hourly view, or hero redesign.

The focused check should inspect the retained tree for the visible label,
icon/text row, and stable insets across placeholder, error, stale, and normal
readings. The live gate records the actual bar result.

### Launcher asset

The launcher artwork is the production asset, not a reference image:

```text
/home/nomadx/Pictures/sysc-aperture-c-nested-gates.png
SHA-256: 02f3a6246c193b06701e8d99d7cfbcb5b57136db943d2a67aca8740891827d3f
PNG: 1024x1024, 8-bit RGBA, non-interlaced
Visible alpha bounds: 768x768 at offset (128,128)
```

Copy those exact bytes into a project-owned embedded asset and verify the
source hash, dimensions, alpha bounds, and non-empty visible pixels in a
focused test. Decode it through the existing bounded PNG path into immutable
`ui.Image` data off `Registry.mu`. Use `ui.KindImage` at the requested bar
size; the image box owns its intrinsic bounds and centre.

`internal/shell/widget.go` currently constructs the far-left launcher from
the `ghost` glyph. Replace that visual node while retaining action
`panel:launcher`, accessible name `Open launcher`, button semantics, far-left
placement, and existing hit testing. Do not use `ghost`, the SYSC wordmark,
generated vectors, or a screenshot-like substitute. Do not confuse the
launcher with `internal/render/icons/wordmark/sysc-mark.png`, which belongs to
the wordmark renderer.

The focused shell/render proof should cover the `KindImage` node, the action
and name, native and fractional target sizes, and exact asset metadata. The
bar live gate exercises the button without changing launcher search behavior.

### Workspaces

The Niri wire and projection path is:

```text
internal/platform/niri/events.go -> projectOutputs -> workspacePill
                                  -> refreshWorkspacePills -> bar tree
```

`niri.Workspace` and `wireWorkspace` currently omit `is_urgent`. The
projection's `workspacePill` currently carries index, occupancy, and focus,
and `refreshWorkspacePills` paints the numeric index as text. Extend the
existing wire state only far enough to carry urgency.

Paint shapes rather than numbers. The focused workspace gets the larger
flexible rounded element. Other available workspaces get smaller elements.
Use existing theme and density roles for fill, urgent treatment, dimensions,
and gap. Preserve Niri index ordering, occupied versus empty state, focus
transitions, urgent state, stable accessible identity, pointer hit targets,
keyboard access, and the pre-snapshot fallback. The projected tree must not
contain a painted workspace number, including a hidden number inside a small
node.

Use `internal/platform/niri/events_test.go`,
`internal/shell/projection_test.go`, and `internal/shell/widget_test.go` for
wire, projection, and retained-tree checks. The Noctalia source at
`/home/nomadx/noctalia/src/shell/bar/widgets/workspaces_widget.cpp` supplies
behavior and geometry reference only. Do not copy its code or compatibility
surface.

### Centre clock, media, and wordmark

Read `internal/config` defaults, `internal/shell/widget.go`,
`internal/shell/mediawidget.go`, `internal/render/wordmark.go`, and
`internal/ui/bar.go` before editing. The current default centre has separate
clock items around a `KindWordmark`; `buildWidgets` gives the clocks a floor
when two clocks and a wordmark coexist. `buildMediaWidget` already consumes
the shared media state and has `hideWhenAbsent`, but the centre arrangement
still centres the whole variable-width section.

The target logical composition is:

```text
[ one time/date group ] [ SYSC wordmark ] [ media when available ]
```

Keep the existing tabular clock values, shared capsule, wordmark accessibility
and gradient, media art/title/transport action, max-title behavior, and
absence collapse. Media absence reserves no empty slot. Long titles truncate
within their granted side budget.

Add the narrow anchored-centre path described by the design to
`ui.ArrangeBar`. The wordmark's measured centre must equal the content-band
centre for varied left/right widths, clock/date widths, media presence, and
title lengths. Surrounding nodes yield to the anchor and use existing
truncation and collision rules. A larger clock floor or symmetric padding
alone cannot prove this invariant.

Focused checks cover one constructed time/date group, present and absent
media, art and transport states, bounded long titles, accessible actions, and
the wordmark centre matrix in `internal/ui/bar_test.go`. The live gate checks
no player, a player, a long title, and a minute change.

### Notification urgency and battery warning

Read `internal/shell/notifycard.go`, `notifywidget.go`, `notifications.go`,
the notification presenter client, and the battery snapshot path in
`internal/services/metrics.go`. `notifycard.go` currently gives special tone
treatment to critical urgency. `notifyState`, the centre, and toast hosts
already consume the service snapshot. The bar notification item has a fixed
icon box that must keep its measured width across unread changes.

One semantic helper should map low, normal, and critical urgency to existing
theme roles. Critical retains an accessible error role and edge/container
signal. Apply the role to meaningful card identity elements and leave card
padding, action geometry, timeout meters, grouping, toast containment, and
notification-centre layout intact.

The current `sysc-notify` presenter protocol is read/state-control oriented.
It has no shell publish operation for battery warnings. Qualify that capability
upstream before shell integration. The shell consumes the shared battery
snapshot and uses one stable key such as `sysc-shell:battery-low`. A pure
reducer enters warning at the configured threshold while discharging, holds
through repeated samples, recovers when charging/full returns or when charge
rises above threshold plus five percentage points, and reasserts once after a
producer reconnect. Invalid or absent input clears pending state without
inventing a reading. Send failure stays retryable and never creates a
shell-only notification.

The reducer must not run a producer call under `Registry.mu`. Focused proof
covers all three urgency roles, critical accessibility, stable unread width,
enter/hold/recovery, charging/full, absent/invalid input, reconnect, and
producer failure. The live desktop must not be forced below its battery
threshold or receive a stream of test notifications.

### Owned tray termination

The current shell seams are `internal/shell/tray.go`, `traymenuhost.go`,
`trayactions.go`, `traymenu.go`, and `internal/trayclient`. The current pinned
protocol supports activation, scrolling, menus, and preferences. It has no
safe termination command. Full generation-bearing `tray.ItemKey` values and
the existing `KindItemRemoved` delta already protect stale menu clicks.

Implement the operation in `/home/nomadx/sysc-tray` first. The service owns
the current D-Bus owner, same-UID check, PID and `/proc` start-time identity,
generation, and lifecycle authority. At request time it rejects unsupported,
stale, recycled, cross-UID, ambiguous, or busy identities. It requests
bounded graceful termination through its own authority and publishes the
matching item delta after the owner exits. It does not accept a shell-supplied
PID or fall back to hidden preference removal.

The additive shell protocol should carry only the full `ItemKey`, advertise a
close capability when the service can own the item, and return a clear
unsupported result otherwise. The shell adds one reserved Close row, queues
the command through the existing bounded client, and keeps the item visible
while pending. A reply means accepted work. Shell success means the matching
generation's removal delta arrived. Rejection, timeout, disconnect, or a
generation change restores or retains the visible item.

Focused upstream tests cover authorization, stale/recycled identity, current
same-UID ownership, graceful termination, busy/timeout behavior, and delta
publication. Shell tests cover the exact key, pending state, rejection,
timeout, reconnect, generation reuse, and the hidden-only removal failure.
Use one disposable same-UID fixture for live qualification if available.

### Control Centre Network

`internal/shell/popout_controlcenter.go` currently lists the Network rail as
disabled and `ccPage` has no Network case. `internal/shell/popout_network.go`
already builds `networkTree` with the status-first header, Wi-Fi/Ethernet
tabs, access-point rows, password card, and error states. `services.Network`
and `Registry.setNetwork` already own the one NetworkManager-backed cache and
relay. `panelhost.go` already routes Network actions through the off-owner
`scheduleControl` path.

Enable the rail and route `case "network"` to `networkTree`. Reuse the same
cached service and body for `PanelNetwork` and `PanelControlCenter`. A page
change clears the host password buffer and cancels pending secrets; it does
not create another connection, secret store, device model, signal loop, or
surface. A missing service renders an enabled but truthful unavailable page.

Focused proof covers rail enablement, page dispatch, shared snapshots, Wi-Fi
and Ethernet tab coherence, correct close ownership, reconnect, credential
clearing, no leaked surface, and off-owner scan/radio/activation/secret
actions. The live gate only reads status and opens/closes the page. It does
not toggle Wi-Fi, forget a network, or retain a real credential.

## External release gates

The local clones at `/home/nomadx/sysc-notify` and `/home/nomadx/sysc-tray`
have docs-only `main` branches. Their implementation history lives behind
the candidate tags. Inspect the tag and branch before creating an upstream
worktree; do not compile a shell release from a local `replace` path.

### sysc-notify

The shell currently handshakes for `notification-state` and
`presentation-lifetime` against
`$XDG_RUNTIME_DIR/sysc-notify/presenter.v1.sock`. The `v0.1.0-rc.3` code line
contains history removal and lifetime behavior but no shell producer for a
battery record.

The upstream change needs bounded producer input, a shell-owned replacement
key, urgency, expiry, service-assigned ID, close/replace semantics, same-UID
peer validation, and an explicit unsupported result. Add protocol, presenter,
daemon, authorization, replacement, and close tests upstream. Publish a
tagged compatible release. Update `go.mod` only after those tests pass, then
run the shell client and reducer checks against the released protocol.

### sysc-tray

The shell currently handshakes for the tray capability against
`$XDG_RUNTIME_DIR/sysc-tray/presenter.v1.sock`. The `v0.1.0-rc.1` protocol
has activation, scroll, menu, and preference commands, but no owned
termination operation.

Add and qualify the service-owned capability, full-key terminate command,
typed errors, ownership validation, graceful bounded lifecycle request, and
item-delta acknowledgement upstream. Publish a tagged compatible release.
Only then update the shell pin and integrate the Close row. Keep the shell
free of PID parsing and process signalling.

Model each cross-repository release or missing tag as a Beads issue in this
repository when the graph lacks one. Make shell integration depend on that
issue. A moving branch is not a qualification result.

## Focused proof and repository gates

Write one small runnable check for each non-trivial behavior. Use table tests
for pure layout, reducers, selectors, protocol validation, and identity
rules. The expected focused seams are:

| Area | Checks |
|---|---|
| Metrics/GPU | `internal/services/metrics_test.go`, `internal/shell/controlcenter_test.go`, `popout_monitor_test.go`, `registry_test.go`, and `metricwidget_test.go`. |
| Weather/launcher/workspaces | `internal/shell/weatherwidget_test.go`, `internal/render` asset tests, `internal/shell/widget_test.go`, `projection_test.go`, and `internal/platform/niri/events_test.go`. |
| Centre | `internal/config` default/build tests, `internal/shell/widget_test.go`, `mediawidget_test.go`, and `internal/ui/bar_test.go`. |
| Notifications/battery | `notifycard_test.go`, `notifywidget_test.go`, `notifications_test.go`, and a pure reducer test beside the reducer. Upstream `sysc-notify` protocol and daemon tests are required. |
| Tray | `internal/shell/trayactions_test.go`, `traywiring_test.go`, reconnect/generation tests, and upstream `sysc-tray` protocol/daemon tests. |
| Network | `internal/shell/controlcenter_test.go`, `popout_network_test.go`, `registry_test.go`, and credential/focus lifecycle tests. |

Run the affected package checks after each slice. Before the final merge, the
code-touching tree must provide the evidence required by `AGENTS.md`:

```bash
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
git diff --exit-code -- go.mod go.sum
git diff --check
```

Run these from a dedicated worktree. Establish a baseline before interpreting
new failures. `sysc-310` is a known pre-existing `internal/shell` race and
must be named in the evidence if it remains. A full-tree race result cannot be
silently replaced with a narrower green command. A dependency change is
allowed only for a qualified upstream release and must include its reason in
the plan.

## Bare-metal Niri gate

The receiving machine has one supported live target:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Hardware and display facts:

- output `DP-1`;
- 3440×1440;
- scale 1.0;
- no laptop deployment;
- no runtime virtual output and no second-output qualification.

Use `niri msg -j layers` before and after each affected surface operation.
Record the exact binary, source commit, module pins, surface IDs, and layer
state. Exercise only safe operations:

- bar weather, supplied launcher artwork, workspace focus/occupancy, and an
  urgent workspace fixture where Niri can produce one;
- Control Centre Home and System Monitor, including the shared GPU result or
  a truthful unavailable slot;
- centre with no player, with a player, with a long title, and after a clock
  minute change;
- Network status and open/close behavior without radio, forget, or credential
  actions;
- existing normal and critical notifications, plus the fixed-width unread
  check; record battery presence without forcing a warning; and
- tray menu behavior with one disposable same-UID owned fixture only.

Do not terminate unrelated tray applications. If no safe fixture exists,
record the upstream and shell fixture evidence and leave the real-process
gate open. Do not claim tray termination from a hidden row. When stopping a
scratch fixture, kill the PID obtained for that fixture; do not use a broad
`pkill -f` pattern that can match the shell running the command.

## M10 completion gate

The completion handover can close `sysc-309` only when it records:

- seven executable plans, their focused checks, and the affected-package
  results;
- four truthful Home gauges and one shared, diagnosable GPU snapshot reaching
  the bar, System Monitor, and Control Centre;
- the supplied launcher PNG verified by hash and dimensions, with no `ghost`
  substitute, and a shape-only workspace tree with preserved focus,
  occupancy, urgency, hit targets, and accessibility;
- one clock/date composition, bounded optional media, and a wordmark whose
  centre remains fixed across the layout test matrix and live variations;
- theme-derived low, normal, and critical notification treatment and a
  qualified hysteretic battery warning producer;
- a qualified service-owned tray termination path whose shell success follows
  the matching state delta;
- a functional Network rail using the existing NetworkManager service/body;
- formatting, vet, race, module-diff, and whitespace evidence, including
  named pre-existing failures and hardware limits; and
- a new registered M10 completion handover with exact commit hashes, live
  observations, measurements, and unrun second-output/laptop/fixture gates.

The completion snapshot is immutable. Leave this receiving handover unchanged,
keep `sysc-309` open until its acceptance criteria are met, and close the
execution issues only with evidence attached to their actual boundaries.

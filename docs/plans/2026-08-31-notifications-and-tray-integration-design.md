# Notifications and Tray Integration Design

Date: 2026-08-31

This design corrects the Milestone 5 notification and tray documents after the cross-repository audit.
It also amends the Tranche 3D tooltip and Tranche 4A auxiliary-surface seams that Milestone 5 consumes.
The audit report remains the defect record. This document owns the corrected destination.

Sources:

- [prior art](2026-08-30-notifications-and-tray-prior-art.md)
- [locked research decisions](2026-08-30-notifications-and-tray-research.md)
- [audit report](2026-08-30-notifications-and-tray-audit-report.md)
- [Tranche 3D design](2026-08-30-weather-and-visual-vocabulary-design.md)
- [Tranche 4A design](2026-08-30-panel-foundation-design.md)

## Scope

Milestone 5 delivers the D1 workflows at reference quality: notification popups, center, DND, disk
history, inline reply, swipe, bounded body markup, and a system tray with menus and overflow. It combines
Noctalia and DMS behavior where the existing decisions selected both.

The shell stays a presentation client. `sysc-notify` and `sysc-tray` own D-Bus, durable service state,
and their Unix protocols. Each service publishes a standard-library-only `protocol` package from a
tagged module. The shell imports those packages instead of copying wire structs.

## Fixed decisions

| Topic | Decision |
|---|---|
| Notification lifetime | `sysc-notify` alone owns active state, expiry, close reasons, and history insertion. |
| Presentation feedback | The shell aggregates all outputs to `hovered`, `visible`, `queued`, or `suppressed`. |
| History clearing | `history.clear` removes closed history. `active.dismiss-all` dismisses active notifications. |
| Release order | Each service tags `v0.1.0-rc.1` before shell integration and `v0.1.0` after qualification. |
| Persistence privacy | Skip `transient=true` and `x-sysc-private=true`. Do not treat Canonical or GNOME synchronization hints as private. |
| Protocol ownership | Each service exports its own `protocol` package. There is no shared protocol repository. |
| Icons | M5 renders SVG theme icons through one bounded pure-Go raster path. PNG-only lookup is insufficient. |
| Interactive roots | One process-wide root chain. A panel or drawer may own its attached tray popup. Opening an unrelated root closes the old chain. |
| IPC | Four-byte length-prefixed JSON, handshake, capabilities, snapshot sequence, ordered deltas, request IDs, and resnapshot on gaps. |
| Concurrency | IPC and image workers publish immutable messages. Only the shell owner mutates presentation state or touches Wayland. |
| Tray preferences | 5B includes show/hide, pin/unpin, and order. This revises D12's deferral. |

## Common protocol

Both services use the same transport rules without sharing Go types.

1. Create a private runtime directory with mode `0700` and socket mode `0600`. Reject symlinks and a
   directory not owned by the effective UID. Check `SO_PEERCRED` before the handshake.
2. Prefix each JSON frame with an unsigned 32-bit big-endian length. Reject zero and lengths above
   16 MiB before allocating.
3. Exchange protocol major, minor, role, and capabilities. Major versions must match. Peers ignore
   unknown fields and capabilities but reject unknown message kinds, missing required fields, duplicate
   JSON keys, and invalid enum values.
4. Send one validated snapshot carrying sequence `N`, followed by deltas `N+1`, `N+2`, and so on.
   A gap, duplicate, structural error, queue overflow, or generation change discards the connection and
   requests a new snapshot.
5. Scope request IDs to a connection generation. Every command receives one success or typed error
   reply. Never retry an action, menu selection, or inline reply after an unknown outcome.
6. Permit one presentation client. A new valid presenter replaces the old generation. Services may
   keep small diagnostic clients later, but v1 does not add that role.
7. Bound each connection to 256 queued messages and 32 MiB of decoded data. A slow client loses the
   connection; D-Bus processing continues.

The shell has one reader and one serialized writer per service. The writer queue holds 64 commands.
A full queue returns `busy` to the UI. It does not block the Wayland owner.

## Notifications

### Service state and lifetime

`sysc-notify` validates `Notify`, assigns or replaces the ID, captures at most 16 PID lineage entries
with process start times, and commits the new active record atomically. A malformed optional image
becomes text-only. A structurally invalid replacement leaves the old notification intact.

The shell reports one aggregate state per active notification with this precedence:

```text
hovered > visible > queued > suppressed
```

- Any hovered visible copy pauses the remaining timeout.
- Any visible copy makes the notification visible even if another output queues it.
- All configured outputs must queue it before `queued` applies.
- DND, an open center, zero outputs, shell loss, or no projected card means `suppressed`.
- A notification queued before its first display pauses. Its first display starts a full timeout.
- Suppressing a never-displayed queued notification starts its full timeout so it cannot survive forever.
- Suppressing a displayed notification clears hover and resumes its remaining timeout.
- Replacement updates cards in place and applies the replacement timeout under the current aggregate
  presentation state.

Presentation state is leased. The shell renews it every two seconds; the service clears holds after six
seconds without renewal. Socket close and presenter replacement clear them at once.

The service alone emits close deltas and D-Bus `NotificationClosed`. Actions, dismiss, reply,
`history.clear`, `history.mark-seen`, and `active.dismiss-all` use request/reply commands. Action success
does not depend on Niri focus. The shell may focus one still-live, unambiguous process instance after the
service accepts the action.

### History, center, and DND

Closed eligible notifications enter history. Active notifications never appear in the history array.
History holds 100 entries for seven days. It stores no actions. `seen` persists with each entry; opening
the center marks the entries it presents and drives the bar's unread badge.

The center groups by desktop entry, then application name, with newest entries first. It uses M4's
virtual list. Its header owns DND, temporary DND presets, dismiss-active, and clear-history controls.
DND stays shell-owned and produces the existing OSD feedback. It suppresses presentation, not `Notify`
or history.

`sysc-notify` writes `$XDG_STATE_HOME/sysc-notify/history.json` and PNG sidecars. It creates files as
`0600`, writes and syncs a temporary file, renames, then syncs the directory. It quarantines corrupt or
unknown future schemas instead of overwriting them. Startup and eviction remove sidecars only after a
valid committed history object supplies the reference set.

### Content and interaction

The shell renders the freedesktop body subset as bounded styled runs: bold, italic, underline, line
breaks, and links. It decodes entities and strips unsupported tags and attributes. Invalid markup falls
back to plain text. The service advertises only capabilities that pass end to end. Hyperlink capability
requires a qualified opener; styling alone does not justify the claim.

Cards use one surface per output, a 360 logical-pixel width, geometry-based overflow, six action pairs,
default-action body click, inline reply, a 35 percent swipe threshold, countdown, and an independent
`value` bar. History cards have no actions. Reduced motion removes spatial animation.

## Tray

`sysc-tray` owns watcher acquisition or attachment, hosts, item proxies, properties, method calls,
DBusMenu layout, and menu events. Item identity is unique bus owner, object path, and service generation.
The shell projects the same item state independently on each output.

Commands carry request ID, item generation, and compositor-logical coordinates. A menu request also
carries the saved Wayland serial. A reply must match request ID, item generation, menu revision, output,
and the current root chain. Owner loss, a newer request, output loss, service loss, or root replacement
cancels it.

The shell validates menu trees iteratively. Submenus replace the visible list and use a back stack.
Structural updates wait while pointer or keyboard interaction is active. A selection against an old
revision returns `stale_revision`, invokes nothing, and refreshes the menu. Property-only changes may
update in place if the focused entry still exists.

Attention replaces the normal icon. Overlay renders last at half size. Named SVG or raster icons and
bounded pixmaps share one scale-aware cache. One bounded worker decodes and rasterizes outside the
Wayland owner.

The drawer reuses the bar's tray item nodes. It contains overflow and a recoverable hidden section.
Show/hide, pin/unpin, and move-earlier/move-later controls persist by a stable item token. Prefer a
non-generic SNI `Id`, then a non-generic title. Do not apply a preference when two live items resolve to
the same token.

Tray tooltips reuse Tranche 3D. The service sends bounded SNI title and description fields; the shell
flattens them to one `ui.Node.Tooltip` string. 5B adds no tooltip surface.

## Shared shell primitives

### Tranche 3D tooltip correction

Tranche 3D becomes the first consumer of the auxiliary-surface seam planned for M4. Its tooltip tasks:

- extract the current bar surface lifecycle into `surfaceUnit`;
- add the basic `AuxSpec` and `AuxRequest` open/close path with application render callbacks;
- put tooltip data on `ui.Node`, with generic reverse-paint-order hit testing;
- use one process-wide dwell controller with a generation token;
- place from top, bottom, left, or right bar edges in logical coordinates;
- set an empty input region and close on leave, press, root opening, reload, output loss, or shutdown;
- replace visible content when its source changes;
- bound text and test actual surface creation and teardown.

Weather uses existing condition, temperature, and staleness data. The plan does not enlarge the
Open-Meteo request merely to fill a tooltip.

### Tranche 4A correction

M4 landed the 3D surface extraction, basic auxiliary open/close path, keyboard binding, and per-surface
input routing. It did not land `AuxUpdate`. The M5 prerequisite plan adds that operation for keyboard
interactivity and input regions. Reload keeps panels and OSDs mapped but closes transient tooltips.

M5 adds no second auxiliary host. 5A uses `AuxUpdate` when inline reply changes keyboard from None to
OnDemand and when card input regions change.

### Rendering and roots

5A adds the first raster consumer to the retained tree: one `KindImage` and one image paint path. Both
notifications and tray use it. Existing meter, button, text field, virtual list, and roving-focus nodes
remain the control vocabulary.

The M5 prerequisite plan grows M4's panel set into a process-wide root coordinator. Panels,
notification center, tray drawer, tray menu, and inline reply participate. A drawer or panel may own
an attached tray popup. Tooltips,
OSDs, and ordinary toasts remain noninteractive and sit outside the chain. Opening a root closes any
tooltip and releases the old root's keyboard, text-input, serials, and service leases.

## Bounds and failure behavior

The service protocol packages publish the numeric constants. The shell enforces equal or lower values.

| Resource | Limit |
|---|---|
| Active notifications | 128 |
| Notify body / actions / hints | 16 KiB body; six pairs; 64 hints |
| Styled body | depth 16; 256 runs; 2 KiB link target |
| Source image | 4096x4096; 16 MiB decoded |
| Wire image | 512 px long edge; 1 MiB |
| Tray items | 128 |
| Tray pixmaps | 512x512; 2 MiB per item; 8 MiB aggregate |
| Menu | depth 8; 512 nodes; 2 MiB icon data |
| Tooltip | 2 KiB flattened text |
| Icon cache | 256 entries; 32 MiB |
| SVG | 1 MiB; depth 32; 4096 elements; 65536 path commands; 512x512 destination |

At the notification active cap, close the oldest non-critical record with a finite timeout first, then
the oldest non-critical record, then the oldest record, with close reason `Undefined`. Coalesce tray
property changes to 30 Hz per item and 120 Hz across the service. Bound icon work to one process-wide
worker queue of 32 jobs and collapse duplicate cache keys.

A malformed notification, tray item, pixmap, menu, or icon cannot remove a healthy sibling. Structural
protocol errors discard the connection generation. Notify service loss closes toasts and releases
inline-reply keyboard. Tray service loss closes its tooltip, menu, and drawer and removes tray
projections. Popup failure leaves the icon and non-menu actions usable.

The currently pinned `sysc-wayland v0.2.0` still treats an unknown object-typed event argument as a
`new_id`; a queued pointer or keyboard enter for a destroyed client surface therefore panics. M5 does
not work around this in the shell. Fix the generator upstream, tag a later release, repin, and retain a
dispatch regression test before transient M5 surfaces enter live qualification.

## Release and dependency order

1. Merge the M3 tooltip fixes, execute the M5 shell-prerequisite plan, and repin a `sysc-wayland`
   release that safely ignores unknown object references.
2. Execute the committed `sysc-notify` and `sysc-tray` v0.1 plans. Each freezes and tests its protocol before
   shell code imports it.
3. Tag both modules `v0.1.0-rc.1`.
4. Implement shell 5A against the notify tag and shared M3/M4 primitives.
5. Implement shell 5B against the tray tag and 5A image support.
6. Run cross-process and live Niri qualification, then tag both services `v0.1.0`.

Local module replacements are forbidden. A tagged protocol may change before stable only by publishing
a new release candidate and updating the shell pin.

## Verification

- Protocol fixtures run in each service and the shell. Fuzz framing, JSON, markup, images, pixmaps, and
  menu trees.
- Pure tests cover lifetime aggregation, presentation leases, reconnect generations, root ownership,
  preference collisions, menu correlation, and deferred menu updates.
- Fake-compositor tests cover auxiliary surfaces, empty and union input regions, keyboard transitions,
  popup cleanup, hotplug, transforms, and mixed scale.
- `dbus-run-session` tests exercise the real notification and tray interfaces, malformed clients, owner
  replacement, and service restart.
- The live Niri gate uses two outputs and covers reply, swipe, DND, history, tooltips, attention,
  overlays, scroll, menu traversal, preferences, every restart direction, and 60 minutes idle.

## Deliberate limits

M5 does not add notification sound, per-app rule editors, remote body images, exotic raster codecs,
XEmbed, recursive popup submenus, or AT-SPI export. It does not claim unsupported notification
capabilities. These omissions do not weaken the D1 workflows or the approved tray parity boundary.

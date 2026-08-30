# Tray Foundation Design: Milestone 5, Tranche 5B

Date: 2026-08-31

This document applies the approved
[notifications and tray integration design](2026-08-31-notifications-and-tray-integration-design.md)
to the shell tray tranche.

## Ordering constraints

5B starts after 5A supplies the tagged-protocol client pattern, `ui.KindImage`, raster cache, and
process-wide image worker. It pins `sysc-tray v0.1.0-rc.1` without a local replacement. M3 supplies the
shared tooltip; M4 supplies auxiliary updates, keyboard routing, menus, virtual lists, and root chains.

## Scope

5B ships:

- a generation-safe tray service client;
- bar projections on every output;
- normal, attention, overlay, theme SVG/raster, and pixmap rendering;
- activate, secondary activate, and both scroll orientations;
- keyboard and pointer DBusMenu traversal with an in-menu submenu stack;
- overflow drawer with recoverable hidden items;
- show/hide, pin/unpin, and order preferences;
- shared tooltips, restart recovery, hotplug, and malformed-sibling isolation.

It does not implement watcher or item D-Bus, XEmbed, recursive popup surfaces, new tooltip surfaces, or a
second image pipeline.

## Ownership decisions

| Topic | Owner and behavior |
|---|---|
| Watcher, host, items, menus | `sysc-tray` |
| Pixels and surfaces | Shell |
| Tooltip surface | Reuse Tranche 3D |
| Image paint/cache | Reuse 5A `KindImage` and worker |
| Menu state | Service owns revision; shell owns visible back stack and deferred application |
| Roots | Menu attaches to its bar/drawer root chain; unrelated roots replace the chain |
| Preferences | Shell persists hidden, pinned, and order state |
| Protocol | Import the tagged service package; do not copy structs |

## Protocol client

The client follows 5A's secure framed transport, handshake, sequence baseline, immutable messages,
64-command writer queue, connection generations, and resnapshot-on-gap rules.

Commands include request ID, item generation, compositor-logical coordinates, and saved Wayland serial.
Menu commands also include the expected revision. Replies must match request ID, generation, revision,
output, and current root generation. Owner loss, a newer request, output loss, root replacement, or
service loss cancels the pending result. Unknown outcomes are not retried.

## Item projection and images

The service identifies an item by unique bus owner, object path, and service generation. The shell
projects the same immutable item independently on each output.

Status controls visibility. Attention replaces the normal icon. Overlay composites last at half size.
Named theme SVG or raster icons and bounded pixmaps use 5A's scale-aware cache and one worker queue.
Cache keys include item generation, icon source, destination scale, theme generation, and overlay.
Malformed candidates fall back from named icon to pixmap to placeholder without removing siblings.

SNI title and description flatten to one bounded `ui.Node.Tooltip`. Dynamic property changes update the
node; the shared M3 controller updates or closes its existing tooltip. 5B creates no tooltip host.

Pointer input invokes activate, secondary activate, vertical scroll, or horizontal scroll with
compositor-logical coordinates. The shell waits for service replies/deltas and reports typed errors
without guessing success.

## Menu surface and revision discipline

A tray menu uses D5's xdg_popup path. The Wayland owner creates a popup `surfaceUnit`, assigns it to the
triggering bar or drawer layer surface with `zwlr_layer_surface_v1.get_popup`, and grabs with the saved
input serial. This reuses the shared surface/buffer lifecycle; 5B adds no second host. The first task
live-tests Niri parenting. If Niri rejects the protocol-valid path, the named fallback is one M4 Overlay
auxiliary menu surface with the same root and revision rules.

Opening saves the serial, output, item generation, menu revision, and root generation. The popup uses
OnDemand keyboard and a bounded input region. Close restores keyboard, drops the serial, and releases
the service request.

The shell validates menu trees iteratively at depth 8 and 512 nodes. Submenus replace the visible list
and push the parent on a back stack; Escape or Back returns before closing the root.

While pointer or keyboard interaction is active, structural revisions wait. Property-only changes apply
when the focused entry still exists. On idle, the newest structural revision replaces the tree and
restores focus by entry ID when possible. Selection against an old revision sends nothing, reports
`stale_revision`, refreshes, and keeps the menu usable.

Popup failure leaves icon activation, secondary activation, and scroll usable.

## Overflow, hidden, pinned, and order

Geometry determines how many items fit. Pinned visible items take bar slots first, then ordinary visible
items in saved order. Remaining visible items appear in the drawer. Hidden items remain recoverable in a
separate drawer section and never consume bar slots.

The drawer reuses the same tray item nodes and one M4 root. Per-item controls show/hide, pin/unpin, and
move earlier/later. Preferences persist atomically in shell configuration.

A stable preference token prefers a non-generic SNI `Id`, then a non-generic title. The shell does not
apply a preference when two live items resolve to the same token. It shows both with default ordering and
marks the collision in diagnostics. Service generations do not enter the persisted token.

## Root behavior

A popup opened from a bar icon may be the root or an attached child of its drawer. Opening an unrelated
panel, center, drawer, menu, or inline reply closes the old chain and releases keyboard, text-input,
serials, and pending service requests. Tooltips close before any root opens. Ordinary tray icons remain
noninteractive surfaces until pointer events invoke an action.

## Failure behavior

Service loss closes tooltip, menu, and drawer and removes projections. Item loss closes only roots owned
by that item. Output loss closes roots on that output and retains service state for other projections.
Reconnect replaces the whole generation from a snapshot. Stale menu or command replies have no effect.

## Gate

Automated proof covers framing, reconnect, item generations, icon fallback and overlay order, tooltip
updates, scroll axes, root correlation, menu bounds, submenu back stack, deferred revisions, stale
selection, preference collisions, ordering, hidden recovery, hotplug, and cleanup.

The two-output Niri gate covers representative items, attention, overlays, theme SVG, pixmaps, tooltip,
activate, secondary activate, scroll, full keyboard menu use, pointer revision changes, overflow drawer,
show/hide, pin/order, every restart direction, malformed siblings, and 60 minutes idle.

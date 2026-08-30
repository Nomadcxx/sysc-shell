# Notifications Foundation Design: Milestone 5, Tranche 5A

Date: 2026-08-31

This document applies the approved
[notifications and tray integration design](2026-08-31-notifications-and-tray-integration-design.md)
to the shell notification tranche.

## Ordering constraints

5A starts after:

1. Tranche 3D supplies node-owned tooltips and the basic auxiliary host.
2. Tranche 4A supplies keyboard/event routing, `AuxUpdate`, and the process-wide root coordinator.
3. Tranche 4B supplies text fields, virtual lists, and text-input-v3.
4. `sysc-notify v0.1.0-rc.1` publishes its tested standard-library `protocol` package.

The shell pins the tag. Local module replacements are forbidden.

## Scope

5A ships:

- a generation-safe client for the notify service's framed protocol;
- one toast surface per configured output;
- actions, default action, dismiss, swipe, inline reply, countdown, and value bar;
- bounded styled body runs and one shared raster image node;
- notification center, closed history, persisted seen state, unread badge, DND, and DND presets;
- conservative PID-lineage focus after accepted actions;
- reconnect, hotplug, service-loss, and presenter-lease recovery.

It does not implement D-Bus, notification expiry, active storage, close reasons, disk files, sound,
remote body images, rules, or activation tokens.

## Ownership decisions

| Topic | Owner and behavior |
|---|---|
| Active state and expiry | `sysc-notify` alone |
| Presentation | Shell reports one aggregate state per notification |
| History | Service persists closed eligible records; shell renders it |
| Clear actions | `history.clear` clears history; `active.dismiss-all` dismisses active |
| DND | Shell state suppresses toast presentation, not Notify or history |
| Images | 5A adds `ui.KindImage` and one bounded paint path reused by 5B |
| Roots | Center and inline reply join M4's process-wide root coordinator |
| Protocol | Import tagged service types; do not copy wire structs |

## Protocol client

The client uses the service's four-byte big-endian, 16 MiB bounded JSON framing. It validates the
private socket path, `SO_PEERCRED`, major/minor handshake, capabilities, snapshot baseline, ordered
deltas, and reply IDs.

One reader publishes immutable connection-generation messages. One serialized writer has a 64-command
queue; a full queue returns `busy` to the UI. A sequence gap, duplicate, malformed frame, queue
overflow, or generation change discards projected state and reconnects for a fresh snapshot. Commands
with unknown outcomes are not replayed.

The Wayland owner applies snapshots and deltas. No client goroutine mutates cards, roots, or surfaces.

## Presentation state and lifetime

The shell projects each active record independently on each output, then reports one aggregate:

```text
hovered > visible > queued > suppressed
```

- Any hovered visible copy reports `hovered`.
- Any visible copy reports `visible`.
- `queued` applies only when every configured output queues the record.
- DND, open center, zero outputs, service loss, or no projected card reports `suppressed`.

The service interprets these states. Queued before first display pauses; first display starts the full
duration. Hover pauses remaining duration. Suppression starts a never-displayed timeout or resumes the
remaining displayed timeout. The shell renews presentation state every two seconds; six seconds without
renewal releases holds.

## Toast surfaces

Each configured output owns one Overlay auxiliary surface with `exclusive_zone -1`. Geometry places a
360 logical-pixel card stack from the configured corner. Cards stack away from the edge. Geometry, not a
fixed count, decides overflow.

The surface starts keyboard None. Its input region is the union of visible card rectangles. Entering
inline reply joins the root coordinator, closes any unrelated root and tooltip, then uses `AuxUpdate`
to change keyboard to OnDemand and the input region to include the field. Submit, cancel, root loss,
output loss, service loss, or record close restores None and releases text-input state.

Ordinary toasts stay outside the interactive root chain. Inline reply is the temporary interactive
root. Reduced motion removes swipe and enter translation while preserving state changes.

## Card content

Cards show app identity, summary, body, image/icon, timestamp, urgency, countdown, independent `value`
bar, close, up to six action pairs, and inline reply when advertised.

5A adds `ui.KindImage`, scale-aware measurement, and one renderer path for premultiplied raster data.
Notification and tray consumers share it. Named theme icons prefer SVG, then raster. A bounded pure-Go
SVG subset enforces the integration-design limits before rasterizing off the Wayland owner; unsupported
or malformed icons fall back without removing the card.

Body parsing accepts bold, italic, underline, line breaks, links, and entities. It strips unsupported
tags and attributes, caps depth at 16 and runs at 256, and falls back to plain text on invalid markup.
Links appear only when a qualified opener exists and the capability is advertised.

Pointer behavior uses retained press/release matching. Body click invokes the default action when
present. Swipe commits at 35 percent; otherwise it returns. The shell sends intent and waits for the
service delta before removing a record.

## Center, history, and DND

The notification center is an M4 root with a virtual list. It groups closed history by desktop entry,
then app name, newest first. History rows have no actions. Active records never appear in the history
array.

Opening the center suppresses toasts and sends `history.mark-seen` for the entries shown. Persisted
`seen` drives the bar unread badge. The header provides DND, temporary DND presets, dismiss-active,
and clear-history. DND presets store an end time and clear through one timer; permanent DND has no end.

`history.clear` and `active.dismiss-all` remain separate controls and commands.

## Focus behavior

Active records may include at most 16 `{pid,start_time}` lineage entries. The shell matches them against
the current Niri projection. After the service accepts an action, the shell may focus one still-live,
unambiguous process instance. No match or several matches means no focus. Focus failure does not change
action success.

## Failure behavior

Service loss closes toast surfaces, center, inline reply, and releases keyboard/text-input. Output loss
removes that projection and recomputes aggregate state. Reconnect replaces the whole service generation
from a snapshot. A malformed image, body, or record drops that field or record only.

## Gate

Automated proof covers framing, reconnect generations, sequence gaps, aggregation, lease renewals,
four-edge toast placement, overflow, input-region unions, keyboard transitions, markup bounds, SVG/image
bounds, root ownership, seen state, DND presets, PID ambiguity, and cleanup.

The two-output Niri gate covers actions, reply, swipe, hover, queued first display, DND, center,
history clear versus dismiss-active, unread badge, markup, SVG icons, hotplug, every restart direction,
presenter lease expiry, and 60 minutes idle.

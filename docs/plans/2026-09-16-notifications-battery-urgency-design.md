# Notifications, battery, and urgency design

Date: 2026-09-16. Parent commission: `sysc-309`.

## Goal

Give low, normal, and critical notification cards distinct theme-derived
urgency treatment and publish one deduplicated battery warning through the
notification owner.

## Existing seams

- `internal/shell/notifycard.go` maps only critical notifications to
  `ToneError` and adds a critical accent stripe and outline.
- `internal/shell/notifywidget.go` owns the fixed-width bar icon and unread
  state. `notifyState`, the centre, and toast hosts already consume the
  presenter snapshot from `sysc-notify@v0.1.0-rc.3`.
- The pinned presenter protocol carries the three urgency values and history,
  but its command socket has no publish operation for the shell to create a
  battery notification.
- `services.Snapshot.Battery` contains presence, valid charge, charging state,
  and the service-owned battery reading. The default battery widget uses a
  20 percent warning threshold.

## Decisions

### D1. Urgency maps to semantic theme roles

One helper resolves urgency for every card construction path:

- low uses the theme's subtle foreground role for secondary meaning;
- normal uses the normal foreground role;
- critical uses the error foreground and error-derived edge or container
  treatment, retaining an accessible error signal.

The helper applies the role to the summary and other meaningful identity
elements. It does not add fixed RGB values. Existing card padding, action
geometry, timeout meters, grouping, and toast containment remain unchanged.

Unread state remains a separate state. The bar unread icon keeps its existing
fixed icon box and fill treatment, so unread transitions do not change the
bar item's measured width.

### D2. The notification service owns battery publication

Before shell integration, qualify a small producer capability in
`sysc-notify`. The current presenter role is read/state-control only. The
producer path must accept a bounded shell-owned key, notification content,
urgency, expiry, and replacement identity; return the service-assigned ID;
and close or replace the record by that key. The daemon remains the only
owner of notification records and FDO publication.

The producer transport validates its same-UID peer, bounds every field, and
returns an unsupported result when the service release lacks the capability.
The shell does not inject a local `protocol.Notification`, call the presenter
state reducer directly, or pretend a failed send succeeded. The compatible
tag and protocol/daemon tests land in `/home/nomadx/sysc-notify` before the
shell pin changes.

### D3. Battery warnings use a small hysteretic reducer

The reducer consumes the shared battery snapshot and the configured battery
warning threshold. Its states are clear, warning-pending, and warning-live.
It enters warning when a present, valid, discharging battery reaches or falls
below the threshold. It leaves warning when charging or full returns, or when
discharging charge rises above the threshold plus a fixed five percentage
point hysteresis band.

The producer key stays constant, for example `sysc-shell:battery-low`. The
reducer sends one persistent warning on entry and never sends one command per
sampling tick. Recovery closes the service-owned warning without adding a
second celebratory notification. An invalid or absent battery clears the
local pending state without manufacturing a value.

After a producer reconnect, a still-low battery reasserts once for the new
connection. A send failure leaves the reducer retryable and visible in the
diagnostic path; it does not create a shell-only card.

## Data flow

```text
Battery Snapshot -> pure warning reducer -> sysc-notify producer
                                              -> presenter delta
                                              -> existing toast/centre/bar
Notification presenter snapshot -> notifyState -> urgency-aware card trees
```

The reducer runs from the metrics relay with bounded channel or command
handoff. The producer call never runs while `Registry.mu` is held and never
touches the Wayland dispatch loop.

## Focused proof

The executable plans will cover:

- `internal/shell/notifycard_test.go`: low, normal, and critical tones,
  meaningful-element treatment, critical accessibility/error role, and stable
  card geometry;
- `internal/shell/notifywidget_test.go`: unread and read states have the same
  measured width;
- a pure battery reducer test: enter, hold across repeated samples, recover
  above hysteresis, charging/full recovery, absent/invalid input, reconnect,
  and producer failure;
- `internal/shell/notifications_test.go`: an upstream battery delta appears
  through the normal presenter path and disconnect clears the projection;
- the `sysc-notify` producer protocol, authorization, replacement, close, and
  daemon tests in that repository.

## Live gate

Use the bare-metal Niri session only. Capture a normal and critical existing
notification if available, inspect the bar width across unread changes, and
record whether a battery exists. Do not force a real battery threshold on the
receiving desktop. The producer gate must use a fixture or a controlled
service test; it must not spam the user's notification history.

## Boundary

This design does not redesign the notification-centre layout, add a second
notification service, make the shell the notification database, or turn a
stale battery read into a warning.

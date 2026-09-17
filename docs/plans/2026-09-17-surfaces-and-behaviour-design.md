# Surfaces and behaviour design

Date: 2026-09-17. Owner-approved in brainstorming on 2026-09-17.

This design covers sub-project D of Milestone 9: making the OSD, the
notification presentation, the launcher, and the panel surfaces configurable.

It consumes the schema and chrome from
[the settings foundation](2026-09-15-settings-foundation-design.md). Where that
document deliberately shipped no Notifications or OSD section — because
`config.Config` modelled neither — this one supplies the configuration those
sections need.

Every reference below was verified against `main` on 2026-09-17.

Sources read as behaviour and architecture references only:

- `internal/shell/osd.go`, `toasthost.go`, `notifications.go`,
  `popout_notifications.go`, `popout_launcher.go`, `registry.go`
- `internal/notifyclient/client.go`, `socket.go`
- `internal/config/config.go`
- Noctalia's notification, OSD, and launcher setting inventories

## Goal and scope

In scope:

- OSD hide duration, surface size, and which kinds may show.
- Notification **presentation**: toast placement, opacity, app name, actions,
  and urgency styling.
- Do-not-disturb persistence.
- Launcher behaviour: result caps, usage sorting, icon visibility, prefixes.
- The existing panel gap and padding.

Out of scope:

- Notification history retention, capacity, eviction, and filters. These belong
  to `sysc-notify`.
- Per-panel placement, transparency modes, and open-near-click behaviour.
- Launcher visual constants.
- Any new OSD kind. This design exposes the kinds that exist; it does not add
  any.

## The problem

Three subsystems with real behaviour and no configuration surface.

**The OSD is entirely constant.** `osd.go` holds `osdHide` at 1500 ms and the
surface at 220 by 64. The manager takes a hide duration as a parameter and
`registry.go` always passes `0`, which the constructor replaces with the
constant. The parameter is plumbed and never used — the same shape as the bar
edge that sub-project C found parsed, validated, and never delivered to the
surface that anchors the bar.

**Do-not-disturb does not survive a restart.** It lives in `notifyState` as a
`dndUntil time.Time` with preset helpers. Nothing persists, so a user who
silences notifications and restarts the shell is audible again with no
indication that anything changed.

**The launcher's only constants are chrome.** Its row padding, icon slot, and
field height were measured against DMS with recorded rationale — the asymmetric
12 above and 16 below is described in the source as "the point". None of its
behaviour is configurable.

`config.Config` carries `Panels{Gap, Padding, OSD}` and nothing else for any of
this.

## The ownership boundary

The shell connects to `sysc-notify` over the presenter socket as
`RolePresenter`. The daemon owns notification lifecycle; the shell presents
what it is given.

That boundary decides what this sub-project may configure. Toast placement,
opacity, urgency styling, whether an app name or its actions are drawn, and
whether a toast appears at all are presentation. History retention, capacity,
eviction, and filtering are the daemon's, and changing them means a change and
a qualified release in a second repository.

Do-not-disturb sits on the shell's side of that line, and the existing code
agrees: `setDND` lives in the shell, and the daemon keeps accumulating history
while it is on. DND suppresses presentation; it does not suppress the service.

## Decisions

### D1: The OSD's timing, size, and kinds become configuration

The hide duration, the surface dimensions, and the set of kinds permitted to
show move into configuration. The constructor's existing fallback stays, so an
unset or invalid value still resolves to the shipped default rather than a zero
that would dismiss the OSD instantly.

Only two kinds exist: audio and brightness. The setting selects among what is
built, and this design adds none. A kinds list whose vocabulary is two entries
is still worth having, because it is the seam every later kind arrives through.

### D2: OSD position stays in `Panels.OSD`

It is already there, it already works, and it already has a settings entry.
Moving it into a new block would be churn that breaks existing documents for no
gain.

### D3: Notification settings are presentation-only

Toast placement, opacity, app-name and action visibility, and urgency styling
are in scope. Retention, capacity, and filters are not.

This is a boundary, not a deferral. Those values are not the shell's to hold,
and a shell-side copy of them would be a second source of truth for state the
daemon already owns.

### D4: Do-not-disturb persists as a preference, never as a deadline

The on/off preference persists across restarts. A timed preset does not.

A deadline written to disk means a shell that starts up already silent, with a
timer it cannot explain and a user who has no memory of setting it. A
preference is recoverable by the same toggle that set it; a persisted deadline
is a surprise with no visible cause.

### D5: Toast placement is a layout change, not an anchor change

The toast surface already anchors to all four output edges and positions its
content inside that span. So placement and offsets are computed in the tree.

This is cheaper than it appears and it means position and offset arrive
together rather than as two slices.

### D6: The launcher exposes behaviour, not chrome

Result caps, usage-ordered results, icon visibility, and prefix characters are
configurable. Row padding, icon slot, row height, field height, and mark height
are not.

Those constants were measured against a reference and carry their reasoning in
the source. Exposing them would invite a user to dismantle a deliberate
composition one field at a time, and no reference shell exposes them either.

### D7: Panels keep the gap and padding they have

Per-panel placement, anchor, transparency mode, and open-near-click behaviour
are a separate slice. Noctalia carries roughly forty-seven keys in its panels
section; adopting that surface is a design of its own, not a rider on this one.

## Testing

Configuration: every new block round-trips through `Write` and reload; an
absent value resolves to the shipped default; an invalid OSD hide duration is
rejected at load with a named field path rather than silently clamped.

OSD: the configured hide duration reaches the manager rather than the constant;
a kind that is not permitted does not present; the fallback still applies when
the value is zero.

Notifications: toast placement and offsets resolve per position token; urgency
styling selects per level; DND's preference survives a reload while a timed
preset does not.

Launcher: the result cap bounds what is listed; usage ordering changes the
order and nothing else; prefix characters route to their providers.

Settings: each new block appears in the registry and satisfies the
domain-coverage invariant that the settings foundation introduces, which is the
check that stops these sections from being added to configuration and never
reaching the interface.

Gate: the per-package substitute with `-p` and `GOMAXPROCS` capped, never the
repository-wide race run.

## Not verified

- Exactly which behavioural options the launcher service exposes today. The
  shell's own file carries only chrome constants, so the surface may live in
  the pinned `sysc-launch` library and may need extending there. This is a
  planning-time check.
- Whether toast placement should share the OSD's nine-position vocabulary or
  take its own. Sharing is attractive and may be wrong: a toast stack grows,
  and an OSD does not.
- Whether urgency styling belongs here or was already settled by Milestone 10's
  notification child, which is in flight. Check before implementing.

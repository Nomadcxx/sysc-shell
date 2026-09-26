# OSD Breadth and Notification Micro-interactions — Design

Tracking: `sysc-592` (UI parity slice 3). Source: `reports/sysc-shell UI design gap analysis.md`,
just-below-critical gap "OSD breadth" plus the notification/micro list. Owner-approved 2026-09-27.

## D0. Hover-pause is already shipped

The report lists toast hover-pause as a gap. It is not: the toast host tracks hover per connector
and renews a presentation lease every 2 s (`toasthost.go`, `presentationLeaseRenew`), and
`sysc-notify` holds toasts that stop renewing after six seconds — hover holding a toast open is
that mechanism's purpose. This slice adds the genuine gaps and closes the report item with a live
observation for the record.

## D1. OSD kinds beyond volume and brightness

`shell.OSDView` is already `{Kind string, Level int, Muted bool}` (`internal/shell/osd.go`) and
`OSDManager` is a shared 220×64 animator surface, so new kinds are paint plus trigger points — no
protocol, no new surface. Kinds, each triggered by state the shell already owns:

- `media` — play/pause/seek from the media widget's state owner; Level is track position, Muted is
  stopped; label is the track title (truncated), not the kind name.
- `caps lock`, `num lock` — from the modifier state the platform keyboard layer already decodes.
- `layout` — on xkb group switch; text-only card showing the layout name, Level unused.
- `do not disturb` — from the bell badge's owner, so the state change is announced where it is
  toggled from anywhere.

The paint path grows one switch on Kind: icon-in-row for the metered kinds, centred text for
`layout`, and a title-line variant for `media`. Privacy OSDs (mic/camera) are deferred: their
source is a wireplumber/portal consumer that belongs to the M9-E subsystem work, not here.
Lock-key and layout bar widgets are explicitly not part of this slice — the OSD is the feedback
mechanism the references use for these events too.

## D2. Value-reactive OSD fill (caelestia's most-copied micro-interaction)

Inside the OSD meter only: the kind's icon rides the handle while the level is low, and near full
the handle's icon crossfades to the percentage text; over the track the fill edge keeps the
existing glow recipe. Implemented in the OSD paint path against the animator's existing progress
value — zero new channels, and generic sliders are deliberately untouched (they gain it only if a
real consumer asks). Muted already has a label variant; it gains the muted icon glyph.

## D3. Drag-to-expand on collapsed toast cards

Toasts show a collapsed card and swipe-to-dismiss commits at 35% width. Add the complementary
gesture: a vertical drag past 12 px on a collapsed card expands it (same card layout the control
centre shows); horizontal dominance (`|dx| > |dy|`) keeps dismiss behaviour byte-identical, and an
expanded card collapses on release-outside. The drag delta routes through the existing press/
release handling in the toast host — no new input surface.

## D4. Collapse-on-dismiss slide

When a toast dismisses, the remaining stack slides into the freed slot over the catalogue's 200 ms
default using `ui.LerpRect` and the surface animator's visible channel — geometry is pure and
table-testable. If the linear settle reads badly against the asymmetric enter/exit timings, this is
the trigger point for the damped spring parked in the primitives design (`sysc-590` D6).

## D5. Group badge pop and the empty state

A "+N" group badge scales 1.0 → 1.15 → 1.0 over 150 ms when N increases (one reused progress
channel, no ripple). The notification section of the control centre gains a quiet empty state —
icon plus "No notifications" in muted text — matching the references' function without their copy.

## Rejected from the reference set

- Volume earcon with cooldown (Noctalia): requires an audio-playback subsystem for one beep; no
  other consumer exists. Reopen with a sound pipeline if one is ever designed.
- Interactive OSD (wheel/drag on the OSD itself): no reference treats it as load-bearing.
- Toast progress countdown bar (Noctalia urgency bar): the lease model makes client-side timing an
  unreliable authority; skip until `sysc-notify` exposes expiry as state.

## Dependencies and checks

D1's media icon looks right once slice 2's SVG resolver (`sysc-591` D1) lands, but degrades to
rasters until then — soft dep. Checks: table tests for kind→label/paint selection and the
collapse-slide geometry; the drag-to-expand dominance rule as a pure function test; live Niri
captures of each new OSD kind triggered by real key combos, an expand-and-dismiss sequence, and a
reduced-motion pass.

# Micro-interaction Primitives — Design

Tracking: `sysc-590` (UI parity slice 1). Source: `reports/sysc-shell UI design gap analysis.md`,
"Polish gaps: the micro-interaction layer". Owner-approved 2026-09-27.

## Problem

Every reference shell gets its feel from one shared primitive applied everywhere: DMS's
`DankRipple` plus state layers on every clickable, caelestia's `StateLayer` and shape-morphing
`ButtonBase`. sysc-shell has resolved interaction states (`ui.StateHovered`/`StatePressed`,
`pressScale`, hover fill at `render/paint.go`) but no tinting language on non-bar surfaces, no
press-point ripple, no shape change on press, and surfaces that ignore each other when open.
The bar's lack of hover wash was a ruling (`62b7202`, "paint clickables at rest"); panels,
popouts, cards, the launcher and settings have no state language at all.

## Decisions

### D1. No new node kinds

State layer, ripple and morph attach to the kinds that already resolve interaction: `KindButton`,
`KindSegmented`, `KindTab`, clickable `KindCapsule`, `KindMenu` rows, `KindSlider`. A new kind
would make polish opt-in per widget, which inverts the point: the references' polish is structural
because it is impossible to opt out. Built-in widgets, plugin-authored trees and the bar editor all
inherit with no API change, and `ui.Animated` already gates which nodes carry animator state.

### D2. The state layer is a surface policy, not a node field

The bar keeps its furniture ruling. Panels, popouts, notification cards, the launcher, settings and
the control centre get the layer. The switch lives with the shell's per-surface resolver, not on
`Node`. Alpha follows Material: hover 0.08, press 0.12, of the node's on-container foreground;
timing uses the existing catalogue default (200 ms) through the existing animator channels
(`animHover`, `animPress`). Reduced motion paints the resting and pressed states with no fade.
Rejected: a per-node `StateLayer bool` — every consumer would set it the same way per surface.

### D3. Ripple is one more animator channel

`animRipple` is keyed by `StableKey` like every other channel and carries an origin. Press origin
is already routed through the resolver (the toast host's press passes local coordinates); a
keyboard-activated press ripples from the node centre. Paint: a disc growing from the origin over
500 ms EaseOutCubic, clipped to the node's rounded rectangle through `internal/render/mask.go`,
then a 300 ms fade. One ripple per node at a time; a second press retargets the origin, matching
how in-flight hover transitions retarget today.

CPU ceiling note: cost scales with the number of in-flight ripples, not surface size — one masked
disc blend each. If a control-centre profile puts ripple frames over 2 ms, restrict the channel to
`KindButton` and clickable `KindCapsule` (that is the upgrade path, not a reason to skip D3).

### D4. Shape morph is derived from the existing press progress

Pressed radius lerps toward `max(node.Radius/2, 1)` using the `animPress` value the animator
already publishes; the painter reads it like it reads `pressScale`. No new field, no new channel.
If a real component later needs a bespoke target radius, it gets a field then.

### D5. Panel coordination is a pure shell function

`panelPlacement(anchor ui.Rect, output, alreadyOpen []ui.Rect, size ui.Size) ui.Rect` in
`internal/shell`: clamps a panel inside the work area, then shifts along the bar edge to clear
already-open panels; when nothing is open it behaves exactly as today. Consumers: `panelhost`,
popouts, weather panel, calendar. Table-tested pure geometry; no render or protocol change.
Rejected: an owning "surface manager" object — the open-panels set already lives on the Registry.

### D6. Springs are deferred, with a named trigger

Nothing today needs overshoot. The first candidate is notification stack reflow in slice 3
(`sysc-592`); if a linear/lerp collapse reads badly there, the animator gains a damped-spring
integrator for `animVisible` on stack children. Adding an integrator with no consumer is
speculative.

### D7. Elevation and shadow work is out of this slice

The references' dp ladder and 9-direction shadows are a bigger renderer change; panel coordination
(D5) is what makes surfaces feel placed. Revisit with measured defects after D1–D5 ship.

## Ownership and testing

- `internal/ui`: pure value/geometry additions only (clip helpers, morph target maths), same
  discipline as `transition.go` — no clocks.
- `internal/shell/animation.go`: `animRipple` channel, per-surface state-layer policy.
- `internal/render/paint.go`: state-layer overlay, ripple disc, morphed radius.
- `internal/shell`: `panelPlacement` plus its call sites.
- Checks: table tests for `panelPlacement` and morph/ripple geometry; the existing kind-coverage
  and race gates unchanged; plugin protocol untouched so `sysc-wayland@v0.2.2` is not bumped.

## Acceptance

Live Niri gate: captures of control-centre and notification-card hover/press with state layers, a
press-point ripple, a pressed slider handle's radius change, and two panels open without overlap;
bar captures unchanged from today (the ruling holds); reduced-motion pass paints all of it with no
fade. `go test -race` green.

## Non-goals

A general animation API for plugins, ripple on bar widgets, springs before a consumer, tooltip and
scrollbar refinements (smaller items in the report, foldable into any later slice).

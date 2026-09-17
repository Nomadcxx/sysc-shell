# Metrics Control Centre radial design

Date: 2026-09-17. Source revision: `4119ce5`.
Commission: `docs/plans/2026-09-17-metrics-control-centre-radial-design-execution-handover.md`.
Parent design: `docs/plans/2026-09-16-metrics-control-centre-gpu-design.md`.

This document records the composition the owner approved on 2026-09-17 and the
measurements behind it. It authorises no code change by itself; the
implementation seams are named at the end for the next engineer.

## What the measurements found first

Two facts changed the shape of the problem, and both contradict what the code
currently asserts about itself.

**The layout on `main` does not fit its own card.** The temperature and GPU row
paints outside the System card's padded bound by 4px at the default font scale,
13px at 125%, and 29px at 150%; at 200% the layout engine returns an error. The
comment above `ccResourceRowH` reasons from a 16px title. The real title is
22px. `TestControlCentreHomeSystemGaugesFitInsideCardBounds` does not catch this
because its `measure` stub returns `(len(s)*8, 16)` for every string, so the
guard and the comment share one wrong number.

**A 40px ring is not currently better than a 22px one.** `paintRadialGauge`
draws an 11px glyph and an 8px value at every diameter, and paints the icon
*or* the value, never both. Enlarging the circle enlarges only empty space.
The "established 40px gauge" was never a legibility result: it was the geometry
of a one-row-of-two composition that held two metrics, not four.

So the commission's premise — restore the 40px ring — is right, but it only
pays for itself alongside a change to what sits inside the ring.

## D1. The Home System card is four rings in one row

Four 40px radial gauges across one row. Each ring carries its own value as
centred text. Each ring has a caption beneath naming its metric. The card keeps
its existing 356 × 88 bounds, 9px padding and 12px radius.

Order is fixed by the parent design and unchanged: CPU, memory, CPU
temperature, GPU.

| Element | Value |
| --- | --- |
| Slots | 4 × 77, gap 9 (`MarginM`), 3px trailing residual |
| Ring | 40 × 40, stroke 2, centred in its slot |
| Ring → caption | 2 (`MarginXXS`) |
| Caption | `RoleCaption`, `Subtle` |
| Content height | 40 + 2 + 15 = **57 of 70** |
| Vertical residual | 13, split by centring the row in the interior |

It holds at every supported font scale: 54, 57, 61 and 65 against 70 at 75%,
100%, 125% and 150%. At 200% it needs 72; the Home page already fails to lay
out at 200% for unrelated reasons, so that ceiling is pre-existing and this
design neither fixes nor worsens it.

Because all four slots are identical rings, memory cannot read as a card
headline. That was an explicit requirement of the commission and it is
satisfied structurally rather than by type choice.

## D2. Three amendments this composition requires

Each was approved on 2026-09-17.

### D2.1 The card loses its "System" title

The title's 22px line is precisely the room the 40px ring needs. With it, the
best 40px composition needs 83px of a 70px interior; without it, 57px.

The four captions carry identity instead. The cost is the loss of a
`Role: "heading"` node from the Home page's heading order. The implementation
must keep an accessible group name on the card so the section is still
announced — a heading is lost, not the name.

### D2.2 The visible temperature caption is "Temp"

`CPU temperature` measures 99px against a 77px slot and fails from the default
font scale upward. The visible caption is `Temp`; the accessible name and
tooltip remain `CPU temperature`. The ring's own `°C` value disambiguates it
from the three percentages, so the short caption never stands alone.

This is the one place the commission's "labels must identify CPU usage, memory
usage, CPU temperature and GPU usage" is met by the accessible name rather than
the painted string.

### D2.3 The ring's interior type derives from its diameter

`paintRadialGauge` must size its centred content from the box rather than from
the constants 11 and 8. The recommended values are 14px for a percentage, 13px
for a three-character temperature and 12px at `100%`, all inside a 40px ring.
A 22px ring keeps what it renders today.

This is the change that makes the 40px ring worth having, and it is the only
amendment that touches the primitive rather than the call site.

## D3. State treatment

Geometry is identical in every state. Only the arc and the centred value
change, so a reading dropping out never reflows the row.

| State | Ring | Value |
| --- | --- | --- |
| Normal | arc to fraction | the reading |
| Valid zero | track only, no arc | `0%` / `0°C` |
| Three-digit maximum | full arc | `100%` / `100°C` at 12px |
| Unavailable | track only, no arc | `—` |
| GPU unavailable | track only | `—`, name "GPU usage: unavailable" |
| GPU identity ambiguous | track only | `—`, name "GPU usage: device identity ambiguous" |

The two GPU states are deliberately identical on screen. The shell has a usage
it cannot attribute to a device, and drawing anything else would imply a
device is being measured. They differ only in the accessible name and tooltip.

A valid zero is never the dash, and the dash never carries a stale value.

## D4. What carries to the bar and the console

The console keeps its graph-card language. A ring is a glance and a graph is a
history, and forcing them into one shape would make the console worse. What
must be shared is the reading contract:

- **P1 — the metric names itself.** A visible label carries identity, never
  colour, ring or position alone. The bar widget is the one sanctioned
  exception: glyph plus tooltip, because a label is not affordable in the band.
- **P2 — the value sits with its own mark.** Inside the ring on Home; in the
  legend directly beneath its own graph on the console.
- **P3 — unavailable preserves geometry.** Same box, a dash, no stale value, no
  fabricated zero, no reflow.
- **P4 — a valid zero renders as itself.**
- **P5 — one GPU identity everywhere.** `selectGPU` is the only chooser.
  Ambiguous devices are unavailable, never a list position.
- **P6 — interior type derives from its container.** No fixed pixel size inside
  a box whose size varies. D2.3 is this principle applied once.

### The gap this opens

`ccMonitor` — the Control Centre's own monitor section — shows CPU, memory and
network. It has no GPU and no CPU temperature. Once Home shows four metrics,
that section contradicts the page one click away from it. The standalone
console does carry GPU, so the gap is `ccMonitor` alone.

This is outside the commission's scope and is recorded as follow-up work
rather than resolved here.

## Rejected alternatives

**The 22px two-row layout currently on `main`.** Rejected by the commission and
independently by measurement: it overflows the card at every font scale from
100% up, and its `CPU temperature` caption overflows horizontally as well.

**Option A — four 40px rings two-by-two, in the existing card.** Needs 110px of
a 70px interior. Rejected: it cannot fit without growing the card, which is
option C.

**Option B1 — four 40px rings in one row, keeping the card title.** Needs 83px
of 70. Rejected: the title and the ring cannot both have the space.

**Option B4 — the same row at 36px.** Fits with more slack (53 of 70), and is
the fallback if the owner later rejects D2.3. Rejected as the recommendation
because it abandons the established diameter for room that is not needed.

**Option C — grow the System card to 128px.** Viable, and the only option that
keeps both the card title and the label-beside-ring reading. It moves
`ccCardH` for the System card to 128, the left column to 222 and `ccPageH` to
520 against a 480px scroll viewport, so Home begins to scroll. The panel does
not resize and no other Control Centre section is affected, because each
section carries its own page height. Rejected because Home is the at-a-glance
surface and a scrollbar there is a real cost, bought for a composition whose
ring still has an 11px glyph in a 40px circle.

## Observations the implementation should carry

**The thermal ramp's warning colour is nearly invisible.** `radialWarningAmber`
applies `theme.EnsureContrast(amber, track, 3)`, which walks `#ffb300` toward
black until it clears 3:1 against the track. The painted result is `#6c4b00`, a
dark brown. Against the card it reads as the arc fading out rather than as a
warning. The error role above 85°C receives no such treatment and sits at
1.20:1 against the track. The state sheet renders the true colours, so the
effect is visible in the artifacts. Worth a decision; not changed here.

**The arc is distinguished from the track by hue, not luminance** — 1.44:1 for
accent against track. The recommended composition is the first one where this
does not matter, because the number lives inside the ring: the arc is
reinforcement and the value is the datum. This is what satisfies the
commission's "works without colour as the only signal" check.

**The retained-tree invalidation invariant already holds.** `applyLocked`
captures the node before `format` runs and compares through
`nodeVisualStateChanged`, which tests `Text`, `Value`, `Absent`, `Tone`,
`ValueText` and `Values`. A temperature or GPU change therefore invalidates the
bar even though its `format` returns an empty string. The Control Centre
rebuilds its tree unconditionally on every sample. Neither path may regress to
a text-only comparison.

## Implementation seams

Named for the next engineer. This commission does not edit them.

| File | Function | Change |
| --- | --- | --- |
| `internal/shell/controlcenter_pages.go` | `ccHome` | Replace the two `KindRow` resource rows with one row of four slots; drop `monitorCardTitle("System", 0)`; give the card an accessible group name. |
| `internal/shell/controlcenter_pages.go` | `ccResourceGroup` | Becomes a column: ring above, caption below. Stop passing `Icon` so the value-text path runs for all four. Set `Name` to the full metric identity while the caption carries the short form. |
| `internal/shell/controlcenter_pages.go` | constants | `ccGaugeSize` 22 → 40; `ccResourceRowH` retired or re-derived; correct the comment that reasons from a 16px title. |
| `internal/render/radial.go` | `paintRadialGauge` | Derive the centred glyph and value size from the box instead of the constants 11 and 8. |
| `internal/shell/controlcenter_test.go` | `TestControlCentreHomeSystemGaugesFitInsideCardBounds` | Replace the `(len(s)*8, 16)` stub with heights that differ per role, or the guard keeps passing on a broken layout. |

The GPU projection, leasing and selector work are unchanged from the parent
design: `selectGPU` stays the only chooser and `ccResourceValue` keeps its
valid-zero and unavailable contract.

## Boundary

No new renderer, primitive, sampler or dependency. No second metrics reader, no
wildcard GPU fallback, no card-size change, no page-height change. GPU
telemetry is not qualified by this document; it remains the metrics service
tests and the live Niri `DP-1` gate.

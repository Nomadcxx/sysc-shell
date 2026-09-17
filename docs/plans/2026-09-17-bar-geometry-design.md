# Bar geometry and placement design

Date: 2026-09-17. Owner-approved in brainstorming on 2026-09-17.

This design covers sub-project C of Milestone 9: which screen edge the bar
occupies, how much space it reserves, and what happens when a section holds
more than fits.

It is independent of
[the settings foundation](2026-09-15-settings-foundation-design.md) and
[bar composition](2026-09-15-bar-composition-design.md). Those two decide how a
bar is configured and arranged; this one decides where the surface sits and how
much of the output it claims.

Every file reference below was verified against `main` at `83aed08` on
2026-09-17. Line numbers drift; the names do not.

Sources read as behaviour and architecture references only:

- `internal/platform/wayland/client.go`, `host.go`
- `internal/config/config.go`, `load.go`
- `internal/ui/bar.go`, `internal/shell/bar.go`, `panel.go`, `panelhost.go`,
  `theme.go`
- `internal/render/style.go`, `paint.go`
- `docs/plans/2026-09-16-shell-polish-design.md` and its centre-composition
  child, for the Milestone 10 boundary

## Goal and scope

In scope:

- All four bar edges in the configuration model.
- The bottom edge, implemented and shippable on its own.
- Left and right edges, implemented behind one external gate.
- A configurable exclusive zone, including zero.
- Bar overflow: what happens when a section does not fit.

Out of scope:

- Auto-hide and reveal.
- Floating bars and edge margins.
- Per-output geometry overrides.
- Multiple named bars.

The three exclusions matter together: each of them would require detaching the
bar from the screen edge or changing the layer margin, and the current design
deliberately keeps `SetMargin(0, 0, 0, 0)` with the gap painted inside the
surface so the screen edge stays clickable. Nothing in this sub-project
overturns that.

## The problem

**The edge is configured, validated, and ignored.** `config.Bar.Edge` is
parsed, checked against `supportedEdges`, and consumed by panel placement. It
never reaches the surface that anchors the bar. `applyGeometryRequests`
composes its anchor inline:

```go
anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
    layershell.ZwlrLayerSurfaceV1AnchorLeft |
    layershell.ZwlrLayerSurfaceV1AnchorRight)
```

`supportedEdges` names four edges and admits one:

```go
var supportedEdges = map[string]bool{
    "top": true, "bottom": false, "left": false, "right": false,
}
```

So a document asking for `bottom` fails with "is not supported in this
milestone; use top", and the shell has carried the vocabulary for an unbuilt
feature since Milestone 2.

**The zone is not configurable.** The exclusive zone is hardcoded equal to the
surface extent, and that identity is computed independently in two packages:
`OutputHost.surfaceHeight()` returns `policy.Gap + (policy.Height - 2*policy.Gap)`,
and `Theme.Geometry()` returns `t.BarGap + body` for the same body. They agree
only because both encode the same assumption.

**Overflow clips silently.** `placeSection` grants each item
`min(natural, remaining)` and lets the painter truncate. On a 1920-wide output
with enough right-side items this cut a timer label mid-glyph and made battery
through media vanish with no diagnostic. That is `sysc-313`.

## What already works

The favourable findings, which is why the bottom edge is small:

- The platform host already holds the whole policy: `policy config.Bar`. The
  edge is present and unread, not absent.
- Panel placement already branches on a bottom bar in six places, and
  `AttachEdge` is already documented for "top" or "bottom".
- The reload path already re-applies geometry to a mapped surface, and says
  why: "Geometry and anchor changes are ordinary layer-surface requests
  followed by a configure, which is cheaper and more correct than destroying
  and rebuilding the role." An edge change therefore takes effect live.

## Approaches considered

The approved approach generalises the existing single-axis code to a main and
cross axis.

Forking a vertical twin of `ArrangeBar` was rejected. Its doc comment encodes a
deliberate total order — the centre keeps its natural width while it fits, the
sides truncate, a section with no room renders zero-width, and only a centre
wider than the whole band truncates and clears both sides. Duplicating that
rule into a second function would leave two copies of one invariant, and the
copy drifts.

Refusing an overflowing bar at configuration load was rejected. Plugin widgets
appear and disappear at runtime, so the overflow case is not decidable at parse
time. `sysc-313` records the same conclusion.

## Decisions

### D1: Bottom is a plumbing change

`applyGeometryRequests` composes its anchor from `h.policy.Edge` rather than a
literal, and `supportedEdges` admits `bottom`.

Nothing else is needed: the placement branches exist, `AttachEdge` covers it,
and the reload path re-anchors a live surface. This slice ships on its own and
depends on nothing else in this design.

### D2: All four edges in the model; left and right gated on `sysc-314`

The configuration model accepts all four edges. Implementation is in two
slices, because the vertical axis requires changing `ui.ArrangeBar`, and
`sysc-314` is already committed to adding an anchored-centre path to that same
function.

Every change this design makes to `ArrangeBar` — the axis generalisation and
the overflow rule alike — lands after `sysc-314`. The bottom slice touches that
function not at all, so the gate blocks nothing that is ready.

### D3: `ArrangeBar` becomes axis-aware rather than forked

`sectionWidth` measures a main-axis extent. The centre position and the side
budgets become main-axis quantities. `placeSection` grants a main-axis budget
and centres each item on the cross axis, inverting the current
`Y: content.Y + (content.H-h)/2`.

The total order is preserved verbatim. It is re-expressed in main and cross
terms, not redesigned.

### D4: This sub-project owns overflow

What "does not fit" means depends on the axis, so overflow is solved once for
all four edges rather than fixed horizontally and reopened when the bar turns
vertical. `sysc-313` becomes this sub-project's overflow slice.

### D5: Overflow drops whole items, in declaration order, visibly

An item is placed whole or not at all; a section that cannot fit its contents
drops items until it fits and shows an indicator of how many are hidden. The
indicator occupies the main axis, so the fitting loop reserves for it before
deciding what survives.

Collapse order is declaration order from the section's far end: the
last-declared item in an overflowing section goes first. This needs no new
configuration field, it is already visible in the document, and bar composition
makes it directly rearrangeable. An explicit per-widget priority stays
available later without being a prerequisite now.

Silent truncation is what the bug is. An item may still be truncated by the
painter when it is granted its whole natural extent and the glyph run exceeds
it; what may not happen is an item disappearing with no indication.

### D6: One derivation for extent and reserve

The extent and zone derivation moves to a method on `config.Bar`, which both
the platform and the shell already import — the platform's `policy` field is a
`config.Bar`, and the shell reads the same type.

`OutputHost.surfaceHeight()` and `Theme.Geometry()` then read that one
derivation instead of computing it twice. The zone becomes configurable and
separable from the extent, including zero, which lets windows tile beneath the
bar.

## Mechanics

### Anchor and size per edge

| Edge | Anchor | `SetSize` | Exclusive zone |
|---|---|---|---|
| top | `Top \| Left \| Right` | `(0, extent)` | `+reserve` |
| bottom | `Bottom \| Left \| Right` | `(0, extent)` | `+reserve` |
| left | `Left \| Top \| Bottom` | `(extent, 0)` | `+reserve` |
| right | `Right \| Top \| Bottom` | `(extent, 0)` | `+reserve` |

The zero dimension is the compositor-anchored one. The existing comment, "Width
0 asks the compositor for the anchored width", becomes axis-relative rather
than wrong. `SetMargin(0, 0, 0, 0)` is unchanged on every edge.

Anchor composition is a pure function of the edge. It is extracted as such so
it can be table-tested without a compositor connection, which is the only way
`applyGeometryRequests` becomes testable at all.

### Panel placement

`Placement.Margins` computes a cross-axis position with `clampAxis` over
`Output.W` and returns the bar offset as `Top` or `Bottom`. On a side bar the
offset becomes `Left` or `Right` and the clamp runs over `Output.H`.

`Margins` already carries all four fields, so no type changes. `CenterY`
generalises to centring on the cross axis; its name is a statement of the
current assumption rather than of intent.

`AttachEdge` gains left and right, which opens the three paint functions that
consume it: `squareAttachedEdge`, `fillAttachFillets`, and
`clearOutsideRoundedRect`. Their concave fillets currently assume a horizontal
bar edge.

## Relationship to Milestone 10

Milestone 10 is in flight and touches the bar. The boundary was checked rather
than assumed: no M10 design references bar edges, exclusive zones, auto-hide,
or `supportedEdges`. Every occurrence of "anchor" in the M10 set refers to the
wordmark's fixed centre, not to layer-shell anchoring. M10's own scope
statement excludes "later M9 sub-projects".

The one real adjacency is `ui.ArrangeBar`, which `sysc-314` modifies and D3
generalises. D2's gate exists for exactly that overlap. M10 owns what the bar
contains; this sub-project owns where the bar is and how it degrades.

## Testing

`internal/config`: the unified derivation per edge, a reserve of zero, all four
edges admitted, and the existing `validateBar` range rules still enforced.

`internal/ui`: the documented total order preserved on both axes; overflow
dropping whole items; the indicator's extent reserved before selection; and
collapse following declaration order from the far end.

`internal/platform/wayland`: the pure anchor function per edge, and the zero
dimension landing on the correct axis.

`internal/shell`: `Placement.Margins` per edge, `exclusiveBarZone` reading the
unified derivation, and `AttachEdge` accepted on four edges.

Regression: a top-edge bar lays out byte-identically. This sub-project must not
move a bar anyone already has.

Live gate, which is not optional: `niri msg -j layers` per edge, confirming the
anchor set and the exclusive zone the compositor actually applied. This machine
has one output, DP-1 at 3440x1440 and scale 1.0, so a two-output check is
unrunnable here and is deferred rather than claimed. No automated test can
prove a compositor honoured a zone.

Gate: the per-package substitute with `-p` and `GOMAXPROCS` capped, never the
repository-wide race run, which hard-locks this machine.

## Not verified

- Whether Niri honours an exclusive zone of zero on a layer surface that still
  has a non-zero size, or treats it as a request to un-reserve. The live gate
  answers this before the configurable reserve is claimed to work.
- Whether the concave fillet geometry is meaningful on a vertical bar edge, or
  whether a side bar should simply draw none.
- How a vertical bar reads at the current density ladder. Every control size
  derives from a base widget sized for a horizontal band.
- Whether any widget's text run needs rotation on a side bar, or whether
  stacked icons and short labels suffice. This design assumes the latter.

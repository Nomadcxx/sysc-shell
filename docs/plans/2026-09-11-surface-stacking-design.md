# Surface stacking — Design

Date: 2026-09-11. Status lives in bd.

Approach owner-approved 2026-09-11 in brainstorming. The decisions below are
pending owner review.

Fifth document of the parity tranche. One new container kind whose children
share its box instead of flowing, so content can sit over a background image.

**Corrected 2026-09-11 — the original text here had this backwards.** It said
the weather card was the buildable consumer and the media card was blocked.
Reading the source at v4.7.7 shows the reverse.

`Modules/Cards/MediaCard.qml` is the pattern: an `Image` at
`anchors.fill: parent` with `fillMode: PreserveAspectCrop`, sourced from the
track art or a cached wallpaper thumbnail, then a scrim at `opacity: 0.65`, then
content at `0.8`.

`Modules/Cards/WeatherCard.qml` is **not**. Its layering is a `Loader` holding a
`ShaderEffect` that takes the card's own rendered content as a
`ShaderEffectSource` and distorts it for rain and snow. That is a GPU shader,
this shell has no shader stage, and the card needs no stack.

See `2026-09-11-component-parity-design.md` D6.

Behaviour and visual reference only. No QML or C++ is imported.

Sources (read, not imported):

- `internal/ui/tree.go` — `Node`, `Kind`, `Fill`, `FillScrim`
- `internal/ui/layout.go:294` — `layoutCapsuleChild`, the nearest precedent
- `internal/ui/layout.go:523` — `Hit`, which walks children in reverse
- `internal/ui/column.go:58` — `ContentHeight`, `columnChildHeight`
- `internal/render/paint.go:195`, `:292`, `:312`, `:324`, `:486`, `:908` — child
  traversal, forward
- `internal/render/image.go` — `paintImage`, nearest-neighbour
- `internal/shell/weatherwidget.go` — the live weather consumer
- `docs/plans/2026-09-11-noctalia-parity-design.md`
- `docs/plans/2026-09-11-panel-backdrop-blur-design.md` — D5, bilinear sampling

## What already works

Most of the hard part is done, which is why this design is small.

| Concern | State | Evidence |
|---|---|---|
| Paint order | Children paint in order, so the last is on top | Forward `range n.Children` at six sites |
| Hit order | Children hit in reverse, so the topmost wins | `for i := len(root.Children) - 1; i >= 0; i--` |
| Scrim | A fill role already exists | `FillScrim` |
| Image painting | Composites premultiplied rasters | `paintImage` |

Paint and hit already agree that the last child is topmost. **No traversal
changes.** What is missing is only a layout that hands children overlapping
bounds — every existing container flows them.

## Goal and scope

In:

- One container kind whose children each receive its content box.
- Its measurement rule.
- One first consumer: the control-centre **media** card. (Corrected 2026-09-11;
  this originally named the weather card, which uses a GPU shader over its own
  content rather than a background image.)

Out:

- Arbitrary z-index, explicit ordering, or negative offsets. Order is child
  order, which paint and hit already honour.
- Absolute positioning, anchoring, or overflow beyond the container.
- The media card and the audio spectrum. Blocked on MPRIS.
- Any general application toolkit. AGENTS.md: "Add UI primitives only for an
  approved shell component."

## Decisions

### D1 — `KindStack`: children share the content box

A new kind. Every child is laid out into the container's box inset by
`Padding`, in child order, painting back to front.

`layoutCapsuleChild` is the precedent and the shape to follow: it computes an
`inner` rect from bounds and padding, then lays one child into it. A stack does
the same for every child rather than the first.

Rejected: a `Background *Image` field on every node. It would put an image
pointer on all 58 fields' worth of nodes for one card, and it cannot express the
scrim *between* image and text, which is the part that makes the composition
legible.

Rejected: reusing `KindCapsule` with multiple children. Its documented contract
is one child, its column measurement case reasons about exactly one, and
overloading it would make an existing well-understood kind ambiguous.

### D2 — A stack measures as the maximum of its children

`columnChildHeight` switches on kind: a column sums, a capsule is its child plus
padding. A stack is the **max** of its children's heights, plus padding, because
they occupy the same space rather than successive space.

The `KindCapsule` case carries a warning this case must heed exactly: an
explicit `Height` is a reserved box, and measuring the child instead "let the two
paths disagree: a capsule standing in for a fixed-size icon measured as one line
of text here and as its full square there, and the row it sat in was then laid
out too short to hold it." A stack with an explicit `Height` therefore reports
that height, and only an unset one measures its children.

### D3 — The scrim is a child, not a property

A background image under text needs a wash between them or the text is
unreadable over bright regions. That wash is an ordinary node with `FillScrim`,
placed as the second child.

This keeps the scrim's presence, extent and opacity visible in the tree rather
than implied by a flag, and it lets a card that does not need one omit it. It
also reuses the role the modal shield already paints.

### D4 — Background images sample bilinearly

`paintImage` is nearest-neighbour, correct for icons because the icon worker
produces the exact requested size. A card background is the opposite: one
decoded image scaled to whatever the card measures, where nearest banding is
visible across a large flat area.

The blur design already introduces a bilinear sampling path for exactly this
reason (its D5). Both consumers share that path; neither changes
`paintImage`'s contract for icons.

### D5 — A real consumer, or this does not ship

**Corrected 2026-09-11 — this named the wrong consumer.** It said the weather
card. Reading `Modules/Cards/WeatherCard.qml` at v4.7.7 shows its layering is a
`Loader` holding a `ShaderEffect` that takes the card's own rendered content as a
`ShaderEffectSource` and distorts it for rain and snow. That is a GPU shader, not
content over a background image, and this shell has no shader stage. The weather
card does **not** need a stack.

The real consumer is `Modules/Cards/MediaCard.qml`: an `Image` at
`anchors.fill: parent` with `fillMode: PreserveAspectCrop`, sourced from the
track art or — when there is none — a cached wallpaper thumbnail, so it always
has a background; then a scrim at `opacity: 0.65`; then content at `0.8`.

This design now sequences **after** the media slice rather than before it.

**Gate amended 2026-09-12.** The original gate read "one real consumer, or this
does not ship", and it would have deleted `KindStack` if the media card slipped.
That was too strict, because it counted only production panels as consumers.

A **template panel** is planned once this tranche lands: one shipped surface
composing every primitive the shell owns, showing what a default panel looks like.
That surface is a consumer, and an approved one — AGENTS.md requires primitives to
arrive with an approved shell component, and a shipped template *is* such a
component. The rule exists to stop speculative building, not to forbid a reference
surface.

It also serves a commitment the roadmap already carries. Milestone 6 undertakes to
"version the component vocabulary **proven by built-in widgets**", and today
nothing proves it: `internal/ui` has 22 kinds against `plugin/v1`'s 10, with no
surface exercising the difference.

So the gate becomes: **ship it, and the template consumes it.** If neither the
media card nor the template materialises, revert — but a primitive waiting on a
named, planned consumer is not an orphan.

See `2026-09-11-component-parity-design.md` D6.

### D6 — Damage and stacking

Overlapping children mean a change in a lower child dirties the region of every
child above it. Until the smoothness design's rectangle damage lands, this is
free — the whole surface is damaged anyway. Once it lands, a stack's dirty
region is the union of its children's, and the simplest correct rule is that a
stack contributes its own bounds rather than attempting per-child arithmetic.

### D7 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`.**

- Two children in a stack receive identical bounds, inset by padding.
- The stack measures as the max of its children, not the sum.
- An explicit `Height` is honoured over the measured children.
- Hit testing returns the **last** child's action where two overlap.
- Paint order places the last child's pixels over the first's.
- A stack with no children measures zero and paints nothing rather than
  erroring, matching how the other containers degrade.
- The kind is covered by `kindcoverage_test.go`'s `sampleNode`.

### D8 — Tracker

A new issue under the parity work. The primitive itself is blocked on nothing;
its consumer is the media card, which needs the media service (`sysc-156`, split
per that design's D8) and the control-centre spine (`sysc-253`).

The dependency is real and worth filing accurately, so `bd ready` does not
suggest work whose consumer is two issues out.

**Amended 2026-09-12.** This paragraph previously ended by calling an unused
primitive "the condition D5 says to revert rather than tolerate". D5 no longer
says that. The template panel that follows this tranche is a second approved
consumer, so the primitive has somewhere to land even if the media card slips —
file the dependency accurately, but do not read it as a countdown to reverting.

### D9 — Open risks

1. Overlap is a new assumption for anything that reasons about bounds. Hit and
   paint are verified; layout code that assumes siblings are disjoint may exist
   elsewhere and has not been audited.
2. `kindcoverage_test.go` requires every kind to be measurable and paintable, so
   a half-added kind fails loudly — good, but it means the kind cannot land
   ahead of its paint case.
3. If **neither** the media card nor the template panel needs a full-bleed
   image, D5 says delete this rather than keep it. (Amended 2026-09-12: D5 now
   counts two approved consumers, so one of them slipping is not grounds on its
   own. Corrected 2026-09-11: this originally reasoned about the weather card.)
4. **The nearest consumer sits behind the media slice.** A card that needs the
   media service means landing the primitive first leaves it unconsumed for
   however long that slice takes. Two things make that tolerable rather than a
   countdown to reverting: the wallpaper-thumbnail fallback, which the reference
   card itself uses when no track art exists and which this shell already owns,
   and the template panel that follows this tranche. Neither was a stated
   consumer when this risk was first written — the original text called the
   media card "this design's only real consumer", which is no longer true.

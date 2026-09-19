# C2 vertical axis addendum: bar geometry and placement design

Date: 2026-09-19. Owner-approved in brainstorming on 2026-09-19.

This addendum amends [the bar geometry design](2026-09-17-bar-geometry-design.md)
for sub-project C2 (`sysc-322`), the vertical axis slice. It was commissioned by
[the C2 execution handover](2026-09-19-c2-vertical-axis-execution-handover.md),
which records why a plan could not be written until the fork's resolution was
decided. Decisions D1–D6 stand as written except where restated here; the new
decisions are numbered D7 onward.

Every file reference was verified against `main` at `dbddef8` on 2026-09-19.
Line numbers drift; the names do not.

## Why the parent design's premise for this slice needed a decision

D3 says `ui.ArrangeBar` "becomes axis-aware rather than forked", and the design
rejected a vertical twin because duplicating the collision total order leaves
two copies of one invariant that drift. `sysc-314` forked it anyway (merged
`c6a289a`): `internal/ui/bar.go` dispatches on `wordmarkIndex` (`bar.go:38-42`)
into `arrangeAnchoredWordmark` (`bar.go:57`) and `arrangeBarSections`
(`bar.go:165`).

Investigation for the commission established that the fork is shallower than
the design feared. Both paths share one skeleton — measure sections, compute a
centre interval, give the sides the remainder, place through `placeSection`
(`bar.go:259`, thirteen call sites) — and differ only in how the centre
interval is computed. `placeSection` already performs exactly the cross-axis
centring D3 describes (`bar.go:277`), and `sectionGrants`, `grantsWithin`,
`elastic`, `fitCount` and `placeTruncating` are already shared.

### D7: The fork is unified, then generalised once (owner decision)

The owner chose unification over the two alternatives the commission put to
them: generalising each path separately (two copies of the total order times
two axes — what the design rejected), or a vertical axis for the sections path
only (the owner's real bar runs the wordmark composition, so a vertical bar
would render differently from the horizontal one or need a refusal the design
does not provide for).

`ArrangeBar` collapses to one skeleton:

1. measure every section;
2. compute the centre interval — the only variant — placing the centre
   composition as it does. The sections variant centres one block at
   `content.X + (content.W-wC)/2` and keeps the truncating escape for a centre
   wider than the band, with its comment. The anchored variant pins the mark to
   the band centre with before/after flanking and keeps its fallback to the
   sections variant when the composition does not fit;
3. derive the side budgets from the interval;
4. place the sides through `placeSection`.

The collision total order lives in exactly one place again, which is what D3
asked for. The transposition follows D3's original text: main-axis extents,
cross-axis centring, the hardcoded `Y: content.Y + (content.H-h)/2` inverted at
the placement leaf. There is no coordinate-swap trick: text never rotates, so
the axis switch lives where a `Rect` is built from main and cross quantities,
and everything above that leaf is axis-neutral arithmetic.

### D8: Lanes transpose fixed, without mirroring

On a side bar the config lanes land left→top, centre→middle, right→bottom,
identically on the left and right edges. This matches the precedent C1 set on
the lower edge, where the left lane stays left: the same document renders
without mirroring on every edge, and no new configuration vocabulary is
introduced.

### D9: The body derivation becomes edge-aware inside this slice

`bodyLocked` (`internal/shell/bar.go:412-415`) insets `Y: gap` unconditionally,
so a lower-edge bar paints its gap on the far side — `sysc-419`, derived from
code during the commission and not yet observed live. Since C2 must make the
body derivation edge-aware anyway, the owner chose to fix the defect inside
this slice rather than immediately before it. The derivation becomes
edge-aware once, for all four edges — the gap against the screen edge and both
cross-axis ends, flush on the far end along the main axis — and `sysc-419`
closes with the slice, matching its recorded dependency on `sysc-322`.

### D10: The tray stacks on vertical edges

`trayAvailableLocked` (`internal/shell/bar.go:470`) and `arrangeTray`
(`internal/shell/trayprefs.go:61`) do X-axis arithmetic with `itemSize` as a
width. The owner chose stacking over a stated refusal: the tray rides the right
lane (bottom on a side bar, under D8), `trayAvailableLocked`'s arithmetic
transposes with the rest of the slice, and `arrangeTray`'s fit arithmetic is
already axis-neutral once given a main-axis extent. No refusal path, no
diagnostic, and no permanent edge-conditional special case survives the slice.

### D11: The overflow indicator chrome stays with sysc-313, immediately after C2

The reserve is plumbed but unused: `placeSection` takes a reserve and every
caller passes zero. The commission asked whether the chrome and its wiring land
inside this slice or after it. The owner chose after: C2 lands the axis
generalisation with the reserve plumbing unchanged — callers still pass zero,
and the recorded overflow counts stay correct — and `sysc-313`, unblocked the
moment C2 merges, adds the chrome and wires the reserve once, against a
four-edge bar. This keeps C2 single-purpose; it already carries the
unification, the axis, the config gate, `sysc-419` and the tray.

### D12: The anchored wordmark transposes to the vertical centre (owner confirmation)

The mark pinned to the band centre reads vertically as pinned to the vertical
centre, with the before-flank above and the after-flank below, and the side
lanes taking top and bottom under D8. The fallback to the sections variant
survives the unification. Cross-axis fit stays out of the layout contract:
whether any widget's text run fits the strip is a live-gate question, and the
parent design already assumes short labels suffice on side bars.

## A vertical behaviour note the plan must carry

The elastic shortfall mechanism (`grantsWithin`, `bar.go:334`) stays a
main-axis mechanism. On a vertical bar a wide title's main-axis extent is its
height, so it never overflows the main axis and never shrinks; instead it is
cross-clamped at measure time — `measureSection` already clamps the cross
extent to the band (`bar.go:430`) — and the painter ellipsizes inside the
granted box, which is its documented behaviour for a glyph run longer than its
extent. Whether that reads acceptably on a narrow strip is a gate question,
not an assumption.

## Scope the plan must cover

- The config gate: `supportedEdges` (`internal/config/config.go:371`) admits
  left and right; the load path's rejection (`internal/config/load.go:691`)
  becomes unreachable for them. The flip is proven by round-tripping a document
  through `Write` and `Load`, not by inspecting the map.
- The unification and axis generalisation in `internal/ui` (D7), with the
  twelve pinning tests in `internal/ui/bar_test.go` surviving the refactor and
  vertical fixtures added for every axis change.
- The edge-aware body derivation (D9).
- The tray transposition (D10).
- Panel placement on side edges, from the parent design's mechanics and absent
  from the commission's state table, confirmed in scope by the owner:
  `Placement.Margins` clamping over `Output.H` with the bar offset as
  `Left`/`Right`, `AttachEdge` accepting four edges, and the three paint
  functions that consume it (`squareAttachedEdge`, `fillAttachFillets`,
  `clearOutsideRoundedRect`).
- The live gate below.

## Live gate

The development machine is single-output (DP-1, 3440x1440, scale 1.0);
two-output behaviour is recorded unclaimed, not faked.

- Left and right are attempted locally by rotating the output (`niri msg
  output DP-1 transform 90`): the shell then receives a tall, narrow configure
  and the full vertical path — layout, stacked tray, capsule background — runs
  in surface space. If rotation proves unrepresentative mid-gate, left and
  right are recorded as unclaimed rather than forced.
- Re-anchoring is gated around a restart. C1 recorded that a configuration
  write alone left the bar in place until a restart; this slice's gate either
  claims the write path or re-records the gap.
- The laptop pass at 1536x864, scale 1.25, is part of the gate: sub-project
  B's defects passed every test and the first live check, and were caught only
  on the laptop.
- `niri msg -j layers` reports neither anchor nor exclusive zone on this Niri
  (C1's correction of the parent design), so the zone is measured
  differentially from `niri msg -j focused-window`'s `tile_size` and the anchor
  is read from a `grim` screenshot. The same screenshots answer the parent
  design's open question about whether the capsule rounding reads correctly on
  a vertical strip.

## Testing

A transposed mirror of the parent design's testing section:

- `internal/ui`: the total order preserved on both axes — vertical fixtures,
  not the horizontal suite re-run; the unification green under the existing
  twelve pinning tests before any axis change; the anchored fallback
  surviving; the truncating case and its comment surviving.
- `internal/config`: all four edges admitted, proven through the round trip.
- `internal/shell`: the body rect per edge; the tray arrangement vertical;
  `Placement.Margins` per edge; `AttachEdge` accepted on four edges.
- Regression: a top-edge bar lays out byte-identically. This slice must not
  move a bar anyone already has.
- Per the machine's constraints: `gofmt -w .` clean, `go vet ./...`,
  per-package `go test -race -count=1` (the hook refuses `-race` with `./...`),
  and `git diff --exit-code -- go.mod go.sum`.

## Not verified

- Whether the capsule rounding reads correctly on a vertical strip — the gate
  answers it.
- Whether clamped titles (the behaviour note above) read acceptably on a
  narrow strip — the gate answers it.
- Whether the wordmark's text run fits the strip, or whether a side-bar
  document should configure without it — the parent design's existing
  assumption stands until the gate says otherwise.
- Whether the concave fillet geometry is meaningful on a vertical bar edge, or
  whether a side bar draws none — the parent design's open question, answered
  by the gate.
- Two-output behaviour on side edges.
- Physical left/right placement on unrotated hardware, if rotation proves
  unrepresentative.

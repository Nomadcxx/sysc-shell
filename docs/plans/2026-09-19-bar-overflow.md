# Bar overflow implementation plan

Date: 2026-09-19. Executes decisions D4 and D5 of
[`2026-09-17-bar-geometry-design.md`](2026-09-17-bar-geometry-design.md),
tracked as `sysc-313`, which that design adopts as sub-project C2's overflow
slice.

Branch `milestone/m9-bar-overflow`, worktree
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m9-bar-overflow`,
branched from `main` at `c8fe579`.

## Scope

An item is placed whole or not at all. A section that cannot fit its contents
drops items until it fits and reports what it dropped, so the shell can show
how many are hidden. Silent truncation is the bug.

**Not in this slice:** the vertical axis (D3). That decision is deferred
because `sysc-314` has already forked `ArrangeBar`, which contradicts the
premise D3 was written on — see "What changed under the design" below. Nothing
here touches an axis: overflow is solved along the main axis, which is still
the only axis, and the work stays correct when the second one arrives.

## What changed under the design

The design rejected forking `ArrangeBar`:

> Forking a vertical twin of `ArrangeBar` was rejected. Its doc comment encodes
> a deliberate total order … Duplicating that rule into a second function would
> leave two copies of one invariant, and the copy drifts.

`sysc-314` then forked it. `internal/ui/bar.go` now dispatches on
`wordmarkIndex` into `arrangeAnchoredWordmark` and `arrangeBarSections`, and
carries the comment "a second anchored composition needs its own layout
contract". The total-order doc comment above `ArrangeBar` still describes one
rule that now has two implementations.

**This does not obstruct overflow**, and that is why overflow goes first:
verified on `main` at `c8fe579`, every placement in both paths funnels through
one `placeSection` — thirteen call sites, lines 82 to 162. Fixing truncation
there fixes both compositions at once, with no view on how the fork is
eventually resolved.

## Anchors, verified against `main` at `c8fe579` on 2026-09-19

| Thing | Where | State |
|---|---|---|
| the truncation | `internal/ui/bar.go:211` | `granted := min(max(0, w), remaining)` |
| `placeSection` | `internal/ui/bar.go:191` | takes a budget, never reports a shortfall |
| `ArrangeBar` | `internal/ui/bar.go:14` | returns `error` only |
| anchored path | `internal/ui/bar.go:39` | 5 `placeSection` calls |
| sections path | `internal/ui/bar.go:125` | 8 `placeSection` calls |
| shell caller | `internal/shell/bar.go:432`, `:449` | two calls, both discard nothing today |
| tray precedent | `internal/shell/trayprefs.go:61` | `arrangeTray` returns `trayArrangement{Bar, Overflow, Hidden, …}` |

## Design decisions this plan settles

**Layout decides, the shell renders.** `ArrangeBar` returns what it dropped;
it does not synthesise an indicator node. This is the shape the tray already
uses — `arrangeTray` is a pure function returning `trayArrangement`, and the
shell turns `Overflow` into chrome. Copying that shape keeps one pattern for
"too many items" in this codebase instead of two, and it keeps `ArrangeBar`'s
existing contract honest: it writes `Bounds`, and now reports a count. It does
not write `Text`, which would make layout an author of content.

**The indicator's extent is reserved before selection, at a caller-supplied
width.** The design requires reserving for the indicator before deciding what
survives, which is circular if the indicator's width depends on the number it
displays. The tray escapes the same circularity with a fixed `itemSize`, so
this does too: the caller passes the extent to reserve. A caller that passes
zero gets pure whole-item dropping with no indicator space, which is what every
existing test wants.

**A dropped item gets zero bounds, not a zero width.** `Rect{}` on both axes,
so nothing paints and nothing reserves. A zero-width but full-height box is
what silent clipping looks like today.

**Painter truncation of a fully granted item stays legal.** The design is
explicit: an item granted its whole natural extent may still be ellipsized by
the painter when the glyph run exceeds it. What may not happen is an item
disappearing with no indication.

**Elastic items shrink; fixed items drop.** Settled during execution, after
whole-item dropping alone lost the focused-window title on a narrow bar. A node
that declares a width cap is built to ellipsize — the title, the media line and
the weather line each carry one — so fixed items take their natural width and
the elastic ones share the remainder. Only when the fixed items alone do not fit
does a section drop whole items from the far end. A capsule inherits its child's
elasticity, because it is chrome around one node and would otherwise read as
fixed.

This is a widening of D5, not a contradiction of it: D5's target is an item
disappearing with no indication, and an elastic item that narrows is present and
counted as placed. The owner chose this over accepting the drop.

**Watch on the live gate:** nothing yet stops an elastic item being granted a
sliver when the remainder is tiny. A floor was considered and not added, so the
laptop is where a two-pixel title would show up.

## Tasks

Gate after every task, one package per invocation, never the repository-wide
race run:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

### T1 — `placeSection` places whole items and reports the rest

`placeSection` gains a reserve parameter and returns the number of items it
could not place. An item whose natural extent exceeds what remains is not
granted a partial box: it and every item after it get `Rect{}`.

Collapse order is declaration order from the far end, per D5: the last-declared
item goes first. Dropping the tail and stopping is exactly that, since a
section is placed in declaration order.

Tests in `internal/ui`: a section that fits places every item and drops none; a
section one pixel short of its last item drops exactly that item and leaves the
earlier ones at their natural widths; the dropped item's `Bounds` is the zero
`Rect`; a section with no room drops everything; spacing is not charged for an
item that was dropped.

### T2 — the reserve is taken before selection

With a non-zero reserve, the fitting loop subtracts it before choosing what
survives, so the indicator's space is never the thing that overflows.

Tests: a section that fits its items exactly, given a reserve, drops its last
item rather than overrunning; a reserve of zero changes nothing, which is the
regression guard for every existing caller.

### T3 — `ArrangeBar` reports per-section overflow

`ArrangeBar` returns `(BarOverflow, error)` where `BarOverflow` carries
`Left`, `Center`, `Right` counts, aggregated from both layout paths. The
anchored path aggregates its `before`/`mark`/`after` placements into `Center`.

Its doc comment gains the rule: an item is placed whole or not at all, and a
section reports what it could not place.

Tests: overflow in one section is reported against that section and no other;
the anchored wordmark path reports centre overflow; a bar that fits reports
zero everywhere.

### T4 — the two shell callers consume the result

`internal/shell/bar.go:432` and `:449` take the new return. This slice wires
the value through and asserts it; **painting the indicator chrome is where this
plan stops**, because that is bar composition's surface and wants the owner's
eye on what it looks like.

Record the count on the `Bar` so a later slice can render it, and so the value
is observable in a test rather than discarded.

Tests in `internal/shell`: a bar built with more right-side items than fit
reports right-section overflow; the default bar reports none.

### T5 — regression and gate

The existing `internal/ui` bar tests must pass unchanged except where they
assert truncation, which is the behaviour being deleted. Any test that asserted
a partially granted width is rewritten to assert the item is dropped, and the
rewrite is called out in the commit message rather than quietly folded in.

```bash
gofmt -w . && test -z "$(gofmt -l .)"
env GOMAXPROCS=2 go vet -p 2 ./...
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/platform/wayland
timeout 150s env GOMAXPROCS=2 go test -count=1 ./internal/shell
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
```

### T6 — live gate

`sysc-313` was found live, so it is closed live. On the laptop, with a bar
carrying more right-side items than fit at 1920x1080 scale 1.25: confirm no
item is cut mid-glyph and no item vanishes without the count rising.

Read the geometry the way [[niri-layers-omits-anchor-and-zone]] records:
`niri msg -j layers` reports neither anchor nor zone, so use `grim` for what
painted. `grim` over ssh needs `XDG_RUNTIME_DIR` and `WAYLAND_DISPLAY` set.

**The laptop is shared.** Check whether another session is using it before
deploying, back up the binary *and* the config, and restore both afterwards.

## Definition of done

T1–T5 green, T6 run and its observations recorded here, and the diff confined
to `internal/ui/bar.go`, its tests, and the two `internal/shell/bar.go` call
sites. `sysc-313` closes; `sysc-322` stays open and holds the vertical axis.

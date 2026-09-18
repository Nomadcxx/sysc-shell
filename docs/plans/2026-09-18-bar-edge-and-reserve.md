# Bar edge and reserve space implementation plan

Date: 2026-09-18. Executes sub-project C1 of Milestone 9, tracked as
`sysc-321`, from [`2026-09-17-bar-geometry-design.md`](2026-09-17-bar-geometry-design.md).

Branch `milestone/m9-bar-geometry`, worktree
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m9-bar-geometry`,
branched from `main` at `8346d0b`.

## Scope

This slice is decisions **D1** and **D6** only:

- D1: `applyGeometryRequests` composes its anchor from `h.policy.Edge` instead
  of a literal, and `supportedEdges` admits `bottom`.
- D6: the extent/reserve derivation, today duplicated in two packages, moves to
  one place on `config.Bar`, and the exclusive zone becomes configurable
  including zero.

Not in this slice, and gated on `sysc-314` per D2: left and right edges,
the axis generalisation of `ui.ArrangeBar` (D3), and overflow (D4, D5).
**This plan touches `ui.ArrangeBar` not at all.** `sysc-322` holds that work
and is blocked until `sysc-314` lands.

## Anchors, verified against `main` at `8346d0b` on 2026-09-18

The design was written against `83aed08`. Every reference below was re-checked
in the working tree today, because plan documents in this repository drift.

| Thing | Where | State |
|---|---|---|
| `supportedEdges` | `internal/config/config.go:343` | `top` true, other three false |
| edge validation | `internal/config/load.go:667` | rejects with "use top" |
| `wireBar` | `internal/config/load.go` | no reserve field |
| `validateBar` | `internal/config/load.go:713` | computes `Height - 2*Gap` inline |
| bar emission | `internal/config/write.go:147-160` | diffs got against base per field |
| hardcoded anchor | `internal/platform/wayland/client.go:663` | `Top\|Left\|Right` literal |
| `surfaceHeight` | `internal/platform/wayland/host.go:170` | `Gap + (Height - 2*Gap)` |
| `Theme.Geometry` | `internal/shell/theme.go:572` | `BarGap + body` for the same body |
| `exclusiveBarZone` | `internal/shell/panel.go:133` | third consumer, reads `Geometry()` |

Two facts that make D6 safe, both checked rather than assumed:

- `Theme.BarHeight` and `Theme.BarGap` are assigned straight from
  `config.Bar.Height` and `config.Bar.Gap` (`theme.go:166`, `theme.go:186`,
  mirrored by `applyFlat` at `theme.go:291` and by `withBarGeometry` at
  `theme.go:420`). The comment at `theme.go:164` is explicit that the bar
  policy "wins over the density row it came from". So the two derivations
  genuinely share inputs and unifying them cannot change a number.
- Both derivations reduce to `Height - Gap`. `Gap + (Height - 2*Gap)` and
  `BarGap + (BarHeight - 2*BarGap)` are the same expression written twice.

## Design decisions this plan settles

**The reserve is a tri-state, so it gets a pointer.** The zone must express
"follow the extent" (the default), an explicit value, and explicitly zero.
A plain `int` cannot separate "unset" from "zero", and a `-1` sentinel would
put a magic number in the one place the slice exists to make legible.
`config.Bar.Reserve *int` says it directly: nil follows the extent.

This also keeps `config.Write` honest, which is the lesson sub-project B paid
for. `Write` diffs the resolved value against the base; an auto-computed
reserve would differ from base whenever height moved and would be written into
every file that never asked for it. A nil reserve is never emitted.

**The anchor function is total over the four edges.** Left and right are
rejected at configuration load in this slice, but the pure function covers all
four. A partial function would need a failure path for input that `applyBar`
already made unreachable, which is more code and a worse invariant than a
complete table. D2 then turns the edges on at the config layer with no change
here.

**`Theme.Geometry` delegates by constructing a `config.Bar`.** `Theme` holds
loose `BarHeight`/`BarGap` tokens rather than a `config.Bar`, so delegation
reads `config.Bar{Height: t.BarHeight, Gap: t.BarGap}`. Slightly awkward at the
call site, and still correct to do: it leaves exactly one expression of the
derivation, which is the whole point of D6.

## Tasks

Each task is test-first. The gate after every task, never the repository-wide
race run, which hard-locks this machine:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

One package per `go test` invocation. Three packages in one command blocked for
over four minutes at near-zero load.

### T1 — one derivation on `config.Bar`

Add to `internal/config/config.go`:

```go
// Body is the drawn height of the bar: the surface extent less the gap that
// keeps the screen edge clickable.
func (b Bar) Body() int { return b.Height - 2*b.Gap }

// Extent is how much of the cross axis the layer surface occupies. The gap
// lives inside the surface, so the extent carries one gap, not two.
func (b Bar) Extent() int { return b.Gap + b.Body() }

// ExclusiveZone is how much of the output the compositor keeps clear for the
// bar. It follows the extent unless the document states a reserve, which may
// be zero so windows tile beneath the bar.
func (b Bar) ExclusiveZone() int {
	if b.Reserve == nil {
		return b.Extent()
	}
	return *b.Reserve
}
```

and the field `Reserve *int` on `Bar`.

Tests in `internal/config`: `Extent` equals `Height - Gap` for the default bar;
`Body` matches `validateBar`'s inline expression; `ExclusiveZone` equals
`Extent` when `Reserve` is nil; a reserve of zero returns zero and is
distinguishable from unset; a non-zero reserve is returned verbatim.

Then rewrite `validateBar`'s `b.Height - 2*b.Gap` to call `b.Body()`, leaving
its error message and range rule exactly as they are.

### T2 — `reserve` through the wire, apply, validate and write path

`wireBar` gains `Reserve *int` with tag `json:"reserve,omitempty"`. `applyBar`
copies the pointer through. `validateBar` rejects a negative reserve with a
named path error, matching the existing style:

```go
if b.Reserve != nil && *b.Reserve < 0 {
	return pathErr(path+".reserve", "%d is negative", *b.Reserve)
}
```

`Write` emits `reserve` only when the resolved value differs from base,
following the surrounding per-field pattern at `write.go:147`.

Tests: a document with `"reserve": 0` round-trips through `Load` and `Write`
and still reads zero; a document with no reserve emits no `reserve` key; a
negative reserve is rejected at the named path. **Round-trip every mutation
through `Write` and `Load`** — this is the rule sub-project B learned when
`Write` silently dropped `instance`.

### T3 — admit the lower edge

`supportedEdges` sets `bottom` true. The rejection message at `load.go:670`
becomes "use top or bottom" so it names what is actually available.

Tests: `bottom` loads; `left` and `right` are still rejected and the message
names both working edges; an unknown edge still fails the `known` branch with
the four-name message.

### T4 — anchor and size as pure functions

New file `internal/platform/wayland/edge.go`:

```go
// barAnchor is the layer-shell anchor for a bar on the named edge: the edge
// itself plus the two edges of the cross axis, so the surface spans the
// output. Composition is a pure function of the edge so it can be table
// tested without a compositor connection.
func barAnchor(edge string) uint32

// barSize is the SetSize argument for a bar on the named edge. The
// compositor-anchored dimension is zero; extent lands on the other axis.
func barSize(edge string, extent int) (w, h uint32)
```

Table test per the design's table: top `Top|Left|Right` with `(0, extent)`,
bottom `Bottom|Left|Right` with `(0, extent)`, left `Left|Top|Bottom` with
`(extent, 0)`, right `Right|Top|Bottom` with `(extent, 0)`. Assert the zero
lands on the correct axis for each, and that an unrecognised edge falls back to
top rather than anchoring nothing — a surface with no anchor is invisible, and
config already rejects unknown edges upstream.

### T5 — `applyGeometryRequests` reads the policy

`surfaceHeight` becomes `h.policy.Extent()`. `applyGeometryRequests` replaces
its literal anchor with `barAnchor(h.policy.Edge)`, its `SetSize(0, height)`
with `barSize`, and its `SetExclusiveZone(int32(height))` with
`h.policy.ExclusiveZone()`.

The bounds check stays and keeps guarding the extent. Add the same guard for
the zone, which is now independently settable and must fit `int32`.

The existing comment "Width 0 asks the compositor for the anchored width"
becomes axis-relative rather than wrong.

Tests in `internal/platform/wayland`: `policy_test.go:85` and `:88` already
assert `surfaceHeight` of 44 and 50 — those must still pass untouched, which is
the regression proof that D6 changed no number. Add: a host with a bottom-edge
policy composes the bottom anchor; a policy with a zero reserve sets a zero
zone while keeping a non-zero size.

### T6 — `Theme.Geometry` delegates

`Theme.Geometry` returns `b.Extent(), b.Body(), t.BarGap` over a
`config.Bar{Height: t.BarHeight, Gap: t.BarGap}`. Its doc comment keeps saying
where the gap lives and gains a line saying the derivation is `config.Bar`'s.

`Theme.Valid`'s `t.BarHeight - 2*t.BarGap` also becomes `Body()` so the one
expression really is one.

Tests: `theme_test.go` already pins the baseline geometry at `:19` and the
token scaling at `:57`, and `panel.go:133`'s `exclusiveBarZone` reads through
`Geometry()`. All must pass unchanged. That is the regression gate for the
shell half of D6.

### T7 — regression and the full gate

A top-edge bar must lay out byte-identically. This sub-project must not move a
bar anyone already has.

```bash
gofmt -w . && test -z "$(gofmt -l .)"
env GOMAXPROCS=2 go vet -p 2 ./...
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/platform/wayland
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render
```

### T8 — live gate, which is not optional

No automated test proves a compositor honoured a zone. Deploy and read back
what Niri actually applied, per edge:

```bash
niri msg -j layers
```

Confirm for `top` and for `bottom`: the anchor set, the exclusive zone, and
that a maximised window does not sit under the bar. Then set `reserve` to 0 and
confirm a window tiles beneath the bar while the bar still draws.

**That last case answers the design's first open question** — whether Niri
honours a zero zone on a surface that still has a non-zero size, or treats it
as a request to un-reserve. Record the answer either way; do not claim the
configurable reserve works until it is observed.

`NIRI_SOCKET` is unset in this session even though a live Niri runs here;
derive it rather than assuming. Never `pkill -f` a pattern matching this
binary's own name.

Two-output behaviour stays unverified and is not claimed: each machine in
reach has one output.

## Live gate results, 2026-09-18

Run on the laptop, `eDP-1`, 1920x1080 at scale 1.25, logical 1536x864. This
desktop could not host the gate: its `sysc-shell` service is inactive and no
Niri socket exists in `/run/user/1000`.

**`niri msg -j layers` does not report the anchor or the exclusive zone.** This
Niri gives only `namespace`, `output`, `layer` and `keyboard_interactivity`, so
the check the design specified cannot be run as written. It confirms the
surface is mapped (`sysc-shell:bar` on `eDP-1`, layer `Top`) and nothing more.

What was measured instead, and why it is stronger: the focused window's
`tile_size` height from `niri msg -j focused-window`, differentially, plus a
`grim` screenshot per case. A difference in tile height cancels out Niri's own
gaps, so it isolates the zone exactly; the screenshot shows where the bar
actually painted.

| Case | Tile height | Bar painted at |
|---|---|---|
| `edge` unset (top), no reserve | 810.4 | top |
| `edge: bottom`, no reserve | 810.4 | lower edge, top clear |
| `edge: bottom`, `reserve: 0` | **854.4** | lower edge, window running beneath it |

- **D1 is confirmed.** The bar anchors to the lower edge and the reserved space
  moves with it: the tile height is unchanged, and the screenshots show windows
  starting at y=0 with the bar at the foot of the output.
- **The design's first open question is answered: Niri honours an exclusive
  zone of zero on a surface that still has a non-zero size.** It un-reserves
  without unmapping. The tile grew by exactly 44, the extent, and the bar went
  on painting over the reclaimed strip.
- **An edge change does not take effect from a configuration write alone.**
  Writing `edge: bottom` and waiting left the bar at the top; a service restart
  applied it. The design's claim that "an edge change therefore takes effect
  live" describes the reload path re-anchoring a mapped surface, which is not
  the same as the shell noticing the file. Whether the shell watches the
  document at all was not chased here. **Not claimed: live re-anchoring.**

Noted and not caused by this slice: the clock and date pills paint empty for up
to a minute after a restart, on both edges, and fill in at the next minute
boundary. Reproduced on the top edge, so it is unrelated to the anchor; it is
pre-existing on `main`. Filed rather than fixed here.

## Out of scope, recorded so it is not rediscovered

- `AttachEdge` gaining left and right, and the three paint functions that
  consume it (`squareAttachedEdge`, `fillAttachFillets`,
  `clearOutsideRoundedRect`). `AttachEdge` is already documented for top and
  bottom and needs nothing here.
- `Placement.Margins` cross-axis generalisation: the six placement branches
  already handle a lower bar.
- Whether concave fillet geometry is meaningful on a vertical bar edge.
- How a vertical bar reads at the current density ladder.

## Definition of done

`sysc-321` closes when T1–T7 are green, T8 has been run on the laptop and its
observations recorded here, and the diff touches no file in the owner's
in-flight metrics/GPU set — `internal/services/metrics*.go`,
`internal/shell/{bar,controlcenter_pages,controlcenter_test,metricwidget,panelhost,popout_monitor,registry,widget}*.go`.
Of the files this plan edits, only `internal/shell/theme.go` is in
`internal/shell` at all, and it is not among those.

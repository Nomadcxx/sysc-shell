# C2 vertical axis: commission to brainstorm the plan

- Date: 2026-09-19
- Issue: sysc-322 (M9 sub-project C2: vertical bar axis and overflow)
- Parent design: `2026-09-17-bar-geometry-design.md`, decisions D2 and D3
- Status: issued for brainstorming. No plan exists. One decision in this slice
  is the owner's and cannot be defaulted.

## Why this needs a brainstorm before a plan

The bar geometry design's central premise for this slice is stale. D3 says
`ui.ArrangeBar` "becomes axis-aware rather than forked", and the design's
approaches-considered section rejected forking a vertical twin because
duplicating the collision total order leaves two copies of one invariant that
drift. **`sysc-314` forked it anyway** (merged `c6a289a`, closed `3ceffe7`):
`internal/ui/bar.go` now dispatches on `wordmarkIndex` into
`arrangeAnchoredWordmark` and `arrangeBarSections`, with a comment saying "a
second anchored composition needs its own layout contract".

A plan cannot be written until the fork's resolution is decided, because the
resolution determines the shape of every task in the slice. The three options
put to the owner were: unify the two paths then generalise once; generalise
each separately; or support the vertical axis only for the sections path. None
was chosen. Alongside that decision, the investigation found four further
questions the design does not answer, listed below.

## Where the slice stands (verified against the tree, 2026-09-19)

| Piece | State |
|---|---|
| Platform anchor and size | **Done for all four edges.** `barAnchor`/`barSize` (`internal/platform/wayland/edge.go:17,37`) handle left/right; `applyGeometryRequests` composes the anchor from `h.policy.Edge` (`client.go:659`). |
| Extent and zone derivation | Done, edge-neutral: `config.Bar.Body/Extent/ExclusiveZone` (`internal/config/config.go:130-148`). |
| Config gate | **Remaining.** `supportedEdges` holds `left: false, right: false` (`config.go:371`); the load path rejects with a named error (`load.go:691`). The model already knows all four edges; the gate is the boolean pair plus its tests. |
| `ui.ArrangeBar` | **The fork.** Dispatch on `wordmarkIndex` (`bar.go:38-42`); `arrangeAnchoredWordmark` (`bar.go:57`), `arrangeBarSections` (`bar.go:165`). |
| Overflow | Clipping half merged (`5eb1068`); indicator chrome outstanding (`sysc-313`, blocked by this slice). |
| Shell content band | **Horizontal-shaped.** See defects below. |

Both composition paths share one skeleton — measure sections, compute a centre
interval, give the sides the remainder, place through `placeSection` — and
differ **only** in how the centre interval is computed:

- `arrangeBarSections`: the centre is one block, `centerX = content.X +
  (content.W-wC)/2`; one deliberate partial grant survives (a centre wider
  than the band takes the band via `placeTruncating`, and the painter
  ellipsizes inside).
- `arrangeAnchoredWordmark`: the mark is pinned to the band centre, before and
  after flank it, and the sides get what remains after the whole composition
  (`compositionLeft`/`compositionRight`, `bar.go:140-149`); it falls back to
  `arrangeBarSections` when the composition does not fit.

Everything downstream is already shared and already axis-agnostic in
structure: `placeSection` (`bar.go:259`) hardcodes X as the main axis and does
exactly the cross-axis centring D3 describes (`Y: content.Y +
(content.H-h)/2`, `bar.go:277`); `measureSection` already returns widths and
heights; `sectionGrants`, `grantsWithin`, `elastic`, `fitCount` and
`placeTruncating` are all shared. Twelve tests in `internal/ui/bar_test.go`
pin both paths, including `TestAnchoredWordmarkKeepsTheContentBandCentre`.

## The fork decision

1. **Unify, then generalise once.** Extract "compute the centre interval" into
   the two variants — sections needs three lines, the anchored variant already
   produces its interval as explicit values — and share centre placement, side
   budgets and the truncating case. The total order then lives in one
   skeleton, which is what the design asked for. Cost: a refactor of freshly
   shipped, live-gated code, mitigated by the twelve pinning tests. The
   anchored fallback to sections must survive the refactor.
2. **Generalise each separately.** Two copies of the total order times two
   axes. This is precisely what the design rejected.
3. **Vertical only for the sections path.** Cheapest, but the owner's real bar
   runs the wordmark composition, so a vertical bar would either render
   differently from the horizontal one or need a refusal the design does not
   provide for.

Assessment recorded during investigation: **option 1**. The convergence point
that made overflow a one-function fix — every placement in both paths funnels
through one `placeSection`, thirteen call sites — is the same convergence
point that makes unification small: the only genuinely duplicated logic is the
centre-interval computation. The decision is the owner's.

## Defects and gaps found while investigating

1. **`bodyLocked` hardcodes a top bar** (`bar.go:412-415`): `Y: gap`,
   `H: height-gap`, `W: width-2*gap`, unconditionally. `BarGap` is documented
   (`bar.go:25`) as the outer gap between the screen edge and the painted
   body. For the lower edge, shipped by C1, the gap must sit against the
   screen; the code floats it on the far side and runs the body flush to the
   screen edge. Filed as **sysc-419** (P2, derived from code, not yet observed
   live). C2 must make the body derivation edge-aware anyway, so the fix
   belongs in this slice or immediately before it.
2. **The tray is horizontal-only.** `trayAvailableLocked` (`bar.go:470`) and
   `arrangeTray` (`internal/shell/trayprefs.go:61`) do X-axis arithmetic with
   `itemSize` as a width. A vertical bar needs the tray stacked, or a stated
   refusal.
3. **The overflow indicator reserve is plumbed but unused.** `placeSection`
   takes a reserve and every caller passes zero (`bar.go:443-446` is the
   tray's separate reserve). `sysc-313` blocks on this slice precisely so the
   chrome and the reserve wiring land together.
4. **Lane mapping on vertical edges is unstated.** The design never says
   whether the config lanes left/centre/right become top/middle/bottom on a
   left or right bar. Declaration order is the likely answer but it must be a
   stated decision, not an accident of the implementation.
5. **Open question for the live gate:** the bar paints a capsule background
   into the surface rect; whether the rounding reads correctly on a vertical
   strip is unverified and belongs in the gate, not in the plan's assumptions.

## Prior learning to carry

**From sub-project B's handover** (`2026-09-17-m9-bar-composition-handover.md`):

- **Three separate times, green tests hid a live defect.** The sharpest for
  this slice: *drop resolution read the horizontal axis after lanes became
  vertical, and its tests kept passing because the fixture was horizontal.*
  Every axis change in this slice needs a vertical fixture, not just the
  horizontal suite re-run.
- Lanes are vertical stacks — B corrected the design on that; the default bar
  cannot fit one row and `internal/ui` has no wrapping row.
- Round-trip every mutation through `Write` and `Load`. The `supportedEdges`
  flip must be proven through the load path, not by inspecting the map.
- Check it on the laptop. The laptop at 1536x864 scale 1.25 caught a clipping
  defect every test and the first live check missed.

**From the overflow slice** (`2026-09-19-bar-overflow.md`):

- Layout decides, the shell renders: `ArrangeBar` returns counts rather than
  synthesising an indicator node, following `trayArrangement`'s shape.
- The indicator's extent is reserved from a caller-supplied width, escaping
  the circularity of measuring a label whose text depends on how many items it
  hid.
- `elastic()` must descend into capsules — `capsuled()` wraps every bar widget
  in a `KindCapsule`, so a `MaxWidth` check on the placed node reads every
  widget as fixed. An axis generalisation that re-derives "fixed vs elastic"
  must keep that descent.
- One partial grant survives on purpose (centre wider than the band); any
  unification must preserve it and its comment.

**Machine traps** (M9 execution handover, overflow-and-tray handover, AGENTS.md):

- The `commit-msg` hook rejects ordinary English by substring: `both`,
  `bottom` (contains "bot"), `precursor`, `agent`, `cursor`, `codex`, `llm`,
  `Hallmark`. Write "the lower edge". Run the real hook as the arbiter:
  `bash ~/.git-hooks/commit-msg msg.txt` — a hand-written grep chain exits 0
  even when it matches.
- `niri msg -j layers` reports neither anchor nor exclusive zone on this
  Niri. Measure the zone differentially from `niri msg -j focused-window`'s
  `tile_size` (cancels Niri's gaps) and read the anchor from a `grim`
  screenshot. `grim` over ssh needs `XDG_RUNTIME_DIR=/run/user/1000` and
  `WAYLAND_DISPLAY=wayland-1`.
- Live re-anchor from a configuration write alone is **unclaimed** from C1: a
  write left the bar in place until a restart. An edge change is the same
  path, so plan the gate around a restart, and either claim or re-record the
  gap.
- Two-output behaviour is untestable on either machine in reach (single
  `DP-1`, 3440x1440, scale 1.0). Record it as unclaimed, do not fake it.
- Live Niri environment: `NIRI_SOCKET=$(ls
  /run/user/1000/niri.wayland-*.sock | head -1)`, `WAYLAND_DISPLAY=wayland-1`,
  `XDG_RUNTIME_DIR=/run/user/1000`. `niri msg -j layers` is the live assertion
  for mapped surfaces. Never `pkill -f` a binary name also typed in the
  command; kill by pid from `pgrep -f 'scratchpad/<name>'`.
- bd: run from the primary checkout only; `bd ready` defaults to a limit of
  10; `bd show` has no `--json` (use `bd list --json` and jq). Commit
  `.beads/issues.jsonl` with the work.
- Code-touching commits: `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet
  ./...`, `go test -race -count=1 ./...` (one package per invocation — the
  hook refuses `-race` with `./...`), `git diff --exit-code -- go.mod go.sum`.
- The shell has no argument parsing and is driven over its IPC socket.

## What the brainstorm must settle

1. **The fork resolution** — the three options above, with the owner's
   sign-off. This gates everything else.
2. **Lane-to-edge mapping** for vertical bars (left/centre/right become what,
   in what order).
3. **sysc-419 scoping** — fix the lower-edge body gap inside this slice or
   immediately before it.
4. **The tray on vertical edges** — stacked arrangement, or a stated refusal
   with a diagnostic.
5. **Ordering against sysc-313** — the indicator chrome and this slice's
   reserve wiring are coupled; sysc-313 blocks on sysc-322. Decide whether the
   chrome lands inside this slice's plan or immediately after it.
6. **What the anchored wordmark means vertically** — the mark pinned to the
   band centre transposes to the vertical centre, with before/above and
   after/below flanks; confirm that reading or correct it.
7. **The live gate plan** — what is verifiable here (single output, lower and
   an upper edge, restart-based re-anchor) and what is recorded as unclaimed
   (two-output, left/right on real hardware if neither machine can rotate).

## Exit criteria

- Every open question above has a recorded decision, with the fork resolution
  explicitly owned.
- The design is amended or a C2 addendum records the decisions; the register
  row is updated in the same commit.
- A plan document exists, is registered, and `sysc-322` references it.
- Discovered work found during brainstorming is in bd with
  `discovered-from:sysc-322`.

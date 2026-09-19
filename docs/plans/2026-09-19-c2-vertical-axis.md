# C2 vertical axis implementation plan

Date: 2026-09-19. Executes the C2 addendum
[`2026-09-19-c2-vertical-axis-design.md`](2026-09-19-c2-vertical-axis-design.md)
(decisions D7–D12) of
[`2026-09-17-bar-geometry-design.md`](2026-09-17-bar-geometry-design.md),
tracked as `sysc-322`.

Branch `milestone/m9-c2-vertical-axis`, worktree
`/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/m9-c2-vertical-axis`,
branched from `main` at `9260e43`.

## Scope

Unify the two composition paths of `ui.ArrangeBar` into one skeleton, transpose
layout to a main and cross axis, and open the side edges: the config gate, the
edge-aware body derivation (`sysc-419` closes here, D9), the stacked tray (D10),
and panel placement on side edges. The dev-machine live gate runs under output
rotation.

**Not in this slice:** the overflow indicator chrome and its reserve wiring,
which stay with `sysc-313` immediately after this slice (D11); every caller
keeps passing a zero reserve and the recorded counts stay correct. Text never
rotates; the axis transposes layout arithmetic only.

## Anchors, verified against `main` at `9260e43` on 2026-09-19

| Thing | Where | State |
|---|---|---|
| the fork dispatch | `internal/ui/bar.go:38-42` | `wordmarkIndex` picks `arrangeAnchoredWordmark` or `arrangeBarSections`; the "ponytail" comment sits above it |
| anchored path | `internal/ui/bar.go:57` | mark pinned, before/after flanks, fallback to sections at `:88` |
| sections path | `internal/ui/bar.go:165` | one centre block, truncating escape at `:188` |
| `placeSection` | `internal/ui/bar.go:259` | X-axis placement, cross-axis centring at `:277`; takes `reserve`, all callers pass zero |
| `grantsWithin` | `internal/ui/bar.go:334` | the elastic/fixed split; `elastic` at `:300` descends capsules |
| `measureSection` | `internal/ui/bar.go:418` | clamps the cross extent to the band at `:430` |
| `placeTruncating` | `internal/ui/bar.go:387` | the one deliberate partial grant, with its comment |
| pinning tests | `internal/ui/bar_test.go` | twelve, including `TestAnchoredWordmarkKeepsTheContentBandCentre`; no test covers the anchored fallback |
| shell lanes | `internal/shell/bar.go:197` | `sections()` returns `[left, center, right+tray]` in paint order |
| tray sizing | `internal/shell/bar.go:440-452` | two-pass: first sizes `available`, then rebuilds tray nodes and re-arranges |
| `trayAvailableLocked` | `internal/shell/bar.go:470` | X-axis arithmetic over placed `Bounds` |
| body derivation | `internal/shell/bar.go:412-415` | `bodyLocked` insets `Y: gap` unconditionally — `sysc-419`; the bar already holds its `config.Bar` policy from `NewWithTheme` (`:118`) |
| config gate | `internal/config/config.go:371` | `supportedEdges` holds `left: false, right: false` |
| load rejection | `internal/config/load.go:691` | `applyBar` refuses an unsupported edge with a named error |
| round-trip precedent | `internal/config/bargeometry_test.go:61` | `TestReserveRoundTripsThroughWriteAndLoad` |
| tray fit | `internal/shell/trayprefs.go:61` | `arrangeTray` projects preferences, then counts what fits |
| panel placement | `internal/shell/panel.go:91` | `Margins` clamps X over `Output.W`, anchors `Top`/`Bottom` by `BarEdge`; `clampAxis` at `:78`; `Trigger.AnchorX` at `:69` |
| panel surface | `internal/shell/panelhost.go:939` | `panelSpec` composes `Top\|Left` or `Bottom\|Left`, shifts `m.Left -= fillet`; slide direction at `:1118`; `style.AttachEdge` set at `:1173` when not `CenterY` |
| silhouette square | `internal/render/paint.go:214` | `squareAttachedEdge` switches on `top`/`bottom` only |
| silhouette clear | `internal/render/canvas.go:204` | `clearOutsideRoundedRect` squares top/bottom rows only |
| fillets | `internal/render/canvas.go:237` | `fillAttachFillets` early-returns unless `top`/`bottom` — accidental today, deliberate under this plan |

## Carried traps

- Run `bd` from `/home/nomadx/sysc-shell`, never from the worktree. Commit
  `.beads/issues.jsonl` with the work; the SQLite file is gitignored.
- The `commit-msg` hook rejects ordinary English by substring — `both`,
  `bottom` (contains "bot"), `precursor`, `agent`, `cursor`, `codex`, `llm`,
  `Hallmark` — and rejects AI attribution outright. Write "the lower edge".
  Screen every message through `bash ~/.git-hooks/commit-msg msg.txt`.
- `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`, and per-package race
  runs only — the hook refuses `-race` with `./...`:
  `timeout 90s env GOMAXPROCS=2 go test -race -count=1 <pkg>`.
- `bd export` truncates on read commands; if `git diff .beads/issues.jsonl`
  looks wrong, rebuild with
  `sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl`
  and check `wc -l` before committing.
- Live Niri: `NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)`,
  `WAYLAND_DISPLAY=wayland-1`, `XDG_RUNTIME_DIR=/run/user/1000`. `niri msg -j
  layers` reports neither anchor nor zone; measure the zone differentially from
  `niri msg -j focused-window`'s `tile_size` and read the anchor from `grim`.
- Never `pkill -f` a binary name also typed in the command; kill by pid from
  `pgrep -f 'scratchpad/<name>'`.

## Tasks

### T1 — unify the two composition paths, horizontal-only (D7)

`internal/ui/bar.go` collapses to one skeleton: measure every section; compute
the centre interval — the only variant — placing the centre composition as it
does; derive the side budgets from the interval; place the sides through
`placeSection`. The sections variant keeps the truncating escape for a centre
wider than the band, with its comment at `:180-187`. The anchored variant keeps
its mark-pinned placement and its fallback to the sections variant when the
mark or the composition does not fit (`:88`). The `wordmarkIndex` dispatch and
the "ponytail" comment are deleted; `ArrangeBar`'s doc comment describes the
total order for one implementation again.

Tests in `internal/ui`: all twelve existing bar tests pass **unchanged** — this
task changes no behaviour, and any test needing a rewrite means the refactor is
wrong. Add the missing fallback pin: an anchored centre whose mark is wider
than the band falls back and produces the same placement the sections path
would produce alone.

### T2 — transpose to a main and cross axis (D7, D8, D12)

`internal/ui` gains an `Axis` type (`Horizontal`, `Vertical`); `ArrangeBar`
takes it as a parameter, and both shell call sites pass the axis their bar edge
implies (horizontal for top and lower edges, vertical for left and right).
Inside the skeleton every quantity becomes a main-axis or cross-axis integer,
and every `Rect` is built through one leaf that switches on the axis:

- horizontal: `X: main, W: mainExtent, Y: crossOrigin + (crossExtent-cross)/2, H: cross`
- vertical: `Y: main, H: mainExtent, X: crossOrigin + (crossExtent-cross)/2, W: cross`

`placeSection` places along the main axis from a main-axis origin and centres
each item on the cross axis; `placeTruncating` transposes identically. The
band argument handed to `measureNode` becomes the cross extent — it constrains
wrapping, and `measureSection`'s clamp at `:430` thereby clamps a wide title to
the strip on a vertical bar (the behaviour the addendum records: elastic stays
a main-axis mechanism, cross fit is the measure clamp plus painter
ellipsization). The elastic descent into capsules is untouched.

Vertical fixtures — not the horizontal suite re-run, which is exactly how
sub-project B's drop-resolution defect survived green:

- a tall narrow band places the left lane's items at the top, the centre lane
  in the middle, the right lane at the bottom (D8, fixed transposition, no
  mirroring) — pin the mapping explicitly;
- cross-axis centring: every placed item's X is centred in the strip;
- the total order on the vertical axis: the centre keeps its natural extent
  while it fits, the sides give way, a section with no room places nothing, a
  centre taller than the band takes the band and clears the sides;
- the anchored mark is pinned to the vertical centre with its before-flank
  above and after-flank below (D12), falling back when it does not fit;
- a title wider than the strip is cross-clamped at measure, not dropped;
- every horizontal test passes unchanged — a top-edge bar lays out
  byte-identically.

### T3 — open the config gate (D2's remaining half)

`supportedEdges` (`internal/config/config.go:371`) admits `left` and `right`;
the milestone rejection in `applyBar` (`internal/config/load.go:691`) stays for
unknown edges but becomes unreachable for the four known ones.

Tests in `internal/config`, following
`TestReserveRoundTripsThroughWriteAndLoad`'s shape: a document with
`edge = "left"` and one with `edge = "right"` round-trip through `Write` and
`Load` and come back with the edge intact; an unknown edge still fails with the
"not one of top, bottom, left, right" error; the default document is unchanged.

### T4 — the body derivation becomes edge-aware (D9, closes sysc-419)

`bodyLocked` (`internal/shell/bar.go:412-415`) reads the edge the bar already
holds in its `config.Bar` policy and insets the gap against the screen edge and
both cross-axis ends, flush on the far end along the main axis:

| Edge | Rect |
|---|---|
| top | `X: gap, Y: gap, W: width-2*gap, H: height-gap` (today's shape, unchanged) |
| lower | `X: gap, Y: 0, W: width-2*gap, H: height-gap` |
| left | `X: gap, Y: gap, W: width-gap, H: height-2*gap` |
| right | `X: 0, Y: gap, W: width-gap, H: height-2*gap` |

Tests in `internal/shell`: the body rect per edge, table-tested; the top row is
the regression guard that the lower-edge defect (`sysc-419`) never regressed
the top bar.

### T5 — the tray stacks on vertical edges (D10)

`trayAvailableLocked` (`internal/shell/bar.go:470`) computes the extent the
tray lane may use along the main axis: transpose the arithmetic over the placed
`Bounds` of the centre and right lanes. `arrangeTray`
(`internal/shell/trayprefs.go:61`) needs no structural change — `available`
becomes a main-axis extent and its fit counting is already extent-based. The
square `trayItemSize` icons and the `…` overflow button are unchanged; they
stack top to bottom on a side bar because `placeSection` places them.

Tests in `internal/shell`: on a vertical bar the tray nodes stack below the
right lane's placed items (bottom under D8) and the available extent is
computed from the transposed lanes; enough tray items to overflow the strip
produce the `…` node; the horizontal two-pass sizing (`bar.go:440-452`) is
unchanged on a top bar.

### T6 — panel placement on side edges

`Placement.Margins` (`internal/shell/panel.go:91`) gains the side-bar branch:
the bar offset becomes `Left: anchor` for a left bar and `Right: anchor` for a
right bar, where `anchor` is `BarZone + Gap` as today; the along-bar position
clamps over `Output.H` through `clampAxis`; `Align` values transpose
(left/centre/right become top/centre/bottom); `CenterY` generalises to
centring on the cross axis, its name left alone per the parent design. The
`Trigger` a panel is opened from carries the trigger widget's along-bar
coordinate in `AnchorX`; on side bars that coordinate is vertical and the
clamp axis follows the bar edge.

Tests in `internal/shell`: `Margins` per edge — a left bar yields `Left:
anchor` with the panel clamped inside `Output.H`; a right bar mirrors it;
`CenterY` centres on the cross axis; the top and lower rows match today's
output exactly.

### T7 — panel surfaces on side edges

- `panelSpec` (`internal/shell/panelhost.go:939`): a left bar anchors the
  panel `Left|Top`, a right bar `Right|Top`; the body region computation and
  the fillet shift transpose — the surface grows along the bar's axis
  (`m.Top -= fillet` for a left bar, with the body inset from that side), the
  way `m.Left -= fillet` serves the horizontal edges today.
- The reveal slide (`panelhost.go:1118`) follows the bar edge: panels slide
  along the axis perpendicular to the bar, so a side bar slides in X where a
  horizontal bar slides in Y.
- `squareAttachedEdge` (`internal/render/paint.go:214`) gains `left` and
  `right`: the attached column of the body is squared, the free edge keeps its
  radius. `clearOutsideRoundedRect` (`internal/render/canvas.go:204`) squares
  the same column when clearing.
- `fillAttachFillets` keeps its early return for side edges, now deliberate
  and commented: whether concave fillets are meaningful on a vertical bar edge
  is the addendum's gate question, answered live rather than assumed.

Tests in `internal/render`: `squareAttachedEdge` and
`clearOutsideRoundedRect` per edge on a small canvas — the attached column is
square, the other three corners rounded. Tests in `internal/shell`: the
`panelSpec` anchor composition per edge; the fillet shift per edge.

### T8 — regression and unit gates, bd bookkeeping

```bash
gofmt -w . && test -z "$(gofmt -l .)"
env GOMAXPROCS=2 go vet -p 2 ./...
timeout 90s env GOMAXPROCS=2 go test -race -count=1 ./internal/ui
timeout 150s env GOMAXPROCS=2 go test -race -count=1 ./internal/shell
timeout 90s env GOMAXPROCS=2 go test -race -count=1 ./internal/config
timeout 90s env GOMAXPROCS=2 go test -race -count=1 ./internal/render
timeout 90s env GOMAXPROCS=2 go test -race -count=1 ./internal/platform/wayland
git diff --exit-code -- go.mod go.sum
```

Every existing horizontal test passes unchanged across all packages. From the
primary checkout, not the worktree: `bd close sysc-419 --reason "edge-aware
body derivation landed with sysc-322 per D9"` and commit
`.beads/issues.jsonl`. `sysc-322` stays open — it closes after the laptop gate
and merge, per the execution handover.

### T9 — dev-machine live gate

Run from the primary checkout or a scratchpad build, never the worktree, with
`NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)`,
`WAYLAND_DISPLAY=wayland-1`, `XDG_RUNTIME_DIR=/run/user/1000`.

1. Top and lower regression: restart the shell on `edge = "top"`, confirm the
   bar is where it was, then `edge = "bottom"` and confirm the gap now sits
   against the screen (`sysc-419` observed, not just derived).
2. **Restart, not write, between edge changes.** C1 recorded that a
   configuration write alone left the bar in place until a restart; either
   claim the write path this slice or re-record the gap.
3. Left and right under rotation: `niri msg output DP-1 transform 90`, then
   `edge = "left"` and `edge = "right"` with a restart each. The shell receives
   a tall narrow configure and the full vertical path runs — layout, stacked
   tray, capsule background, a panel opened from the bar. If rotation proves
   unrepresentative mid-gate, record left and right as unclaimed rather than
   forcing them. Rotate back (`transform normal`) afterwards.
4. `grim` screenshots per edge answer the two gate questions: the capsule
   rounding on a vertical strip, and whether clamped titles read acceptably.
   Read the anchor from the screenshot; measure the exclusive zone
   differentially from `niri msg -j focused-window`'s `tile_size`.
5. Record what the gate showed in this file, including every unclaimed item:
   two-output behaviour, and physical left/right on unrotated hardware if
   rotation was not representative.

## Definition of done

T1–T8 green with the horizontal suite unchanged, T9 run and its observations
recorded here, and the diff confined to `internal/ui/bar.go` and its tests,
`internal/config` (gate and tests), `internal/shell` (`bar.go`, `trayprefs.go`,
`panel.go`, `panelhost.go` and their tests), and `internal/render`
(`paint.go`, `canvas.go` and their tests). `sysc-419` is closed. `sysc-322`
closes only after the audit, the laptop pass, and the merge recorded in
[`2026-09-19-c2-vertical-axis-implementation-handover.md`](2026-09-19-c2-vertical-axis-implementation-handover.md).

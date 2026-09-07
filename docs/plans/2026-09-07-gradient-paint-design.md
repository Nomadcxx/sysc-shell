# Animated gradient paint — Design

Date: 2026-09-07. Status lives in bd.

Owner-approved 2026-09-07. First-party CPU paint: a 2–4 stop linear ramp with
an optional looping offset, applied to filled rects and to alpha masks. The
centred bar wordmark is the first consumer so the primitive is not an orphan.

Prior art is Noctalia PR 3922 (`ui.gradient` + gradient `ui.label`): offset a
four-stop ramp through existing pixels without rebuilding the tree. Behaviour
and sampling math only. That PR's Luau API, GLES shaders, text glow, and hex
colours are not compatibility targets. This shell does not preserve Noctalia
plugin APIs.

Sources (read, not imported):

- `internal/render/canvas.go` — `fillRect`, `blendMask`, `LerpColor`
- `internal/render/paint.go` — `paintWordmark` tints the mark with one colour
- `internal/render/wordmark.go` — alpha master, `WordmarkAspect`
- `internal/shell/animation.go` — A→B channels that `Settled()`; `animateSurface`
- `internal/shell/bar.go` — `anim` field is never constructed
- `internal/ui/bar.go` — `ArrangeBar` pins the centre group to the band midpoint
- `internal/ui/tree.go` — `KindWordmark`, `Fill`, `Tone`; no paint recipe field
- `plugin/v1/node.go` — host owns pixels; no colours on the wire
- `docs/plans/2026-08-26-sysc-shell-design.md` — continuous frame loop out of scope
- github.com/noctalia-dev/noctalia/pull/3922 — `gradientAxisForDegrees`, unclamped offset

## Goal and scope

In:

- `ui.GradientPaint` on `ui.Node`. Zero value (`Count == 0`) is today's solid path.
- CPU sampling of 2–4 theme-token stops along an axis, plus a paint-only offset.
- The same sampler fills a rect and tints an alpha mask (wordmark now; text and
  icons are later consumers of the same function).
- Motion `none` / `loop` / `ping-pong`. Reduced motion parks at the midpoint.
- Bar item `wordmark`. Default Center is `clock | wordmark | date`.
- Equal reserved width on the two default flanking clocks so the mark sits on
  the bar midpoint.
- Named exception to “no continuous frame loop” while a looping paint is visible.

Out:

- `plugin/v1` fields, hex, palette names on the wire, `KindGradient`.
- GLES, glow, region damage, plugin-tunable duration or stops.
- Overlay layout. The mark is a real centre-section item.
- A general application toolkit.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | **One `GradientPaint` value on `ui.Node`.** Stops are a `[4]GradientStop` plus `Count` (2–4) so `copyNode` stays a struct copy. `Count == 0` means solid. Future first-party widgets opt in by filling the spec. No new kind | `KindGradient` plus a special-case wordmark (two paint sites). A pointer recipe that `copyNode` must clone. Wordmark-only paint extracted later |
| D2 | **Stops are theme roles, resolved at paint.** `PaintRole` is `Primary`, `OnSurfaceVariant`, `OnSurface`, `Surface`. The painter maps them through `Style` (`Accent`, `Track`, `Foreground`, `Background`). A palette swap re-resolves; nodes never carry RGB | Hex on the node. `ui.Fill` reused as a gradient stop (fills are chrome, not ink) |
| D3 | **Wordmark recipe:** three stops `OnSurfaceVariant` / `Primary` / `OnSurfaceVariant` at 0, 0.5, 1. Angle 0°. Motion ping-pong, offset −0.45…0.45, trip 2000 ms. Dim ink, light travelling through the glyphs. Duration is a host constant, not a motion-token multiple and not a node field | Solid accent tint (today). Plugin-style per-instance duration. Loop rather than ping-pong — ping-pong is the crest; loop stays available on the spec for later rects |
| D4 | **Live offset is host state written onto the paint copy.** `Node.GradientOffset` is not part of the recipe. `renderViewLocked` (or the bar frame tick) writes the animator value, then `Paint` samples it. Offset changes never remeasure | Storing the running offset inside `GradientPaint`. Asking `Paint` to read the animator |
| D5 | **Sampling copies Noctalia's axis + unclamped offset.** `gradientAxisForDegrees` (normalised axis + corner bias so the rect's projected corners span 0…1). `t = dot(uv, dir) − offset` with no outer clamp; per-segment lerp pins endpoints. Two or three stops expand by repeating the last colour into the four-slot sampler | A CSS-style clamped ramp. A 1D texture. GLES |
| D6 | **Full-buffer damage stays.** The bar already damages the whole `wl_shm` buffer. This slice does not add rectangle damage. A mapped looping wordmark blits the bar about every 16 ms; that cost is accepted and is evidence for Milestone 8, not a reason to add EGL here | Region damage this slice. Starting GLES because the bar moves |
| D7 | **Looping is a new animator mode, not an `Animated()` channel.** `Animated()` stays hover/press/select chrome. A node with `Gradient.Motion != None` needs a `StableKey`. `TargetLoop` is idempotent when the recipe is unchanged, so a tree rebuild keeps phase. `Settled()` is false while a loop runs and reduced motion is off; reduced motion writes the midpoint and reports settled | Overloading `animHover`. A second clock beside `animator` |
| D8 | **`Bar.anim` is constructed.** `NewWithTheme` builds it from `theme.Motion`. A bar frame loop reuses `animateSurface` and `Bar.invalidate`. `DropHost` stops that goroutine. Removing the wordmark `Forget`s its key so an idle bar schedules nothing | Leaving `Bar.anim` nil and ticking from the clock lease (wrong cadence, wrong lifetime) |
| D9 | **Item id `wordmark`.** `knownItems` grows. No capsule (`capsuled` skips `KindWordmark`). Height 19, width `WordmarkWidth(19)`, same as the launcher mark. `Key: "wordmark"`. Not clickable. `refresh` returns false so `applyLocked` does not dereference a nil `inner`. One mark per bar is the supported layout; a second copy would share the key and the phase | A capsule around the mark. Overlay paint outside `ArrangeBar`. A new config schema for stops |
| D10 | **Default Center is time, wordmark, date.** `ArrangeBar` already centres the group on the band. The two clocks share `MinWidthText` `"Wed 30 Sep"` when a section contains a wordmark and at least two clocks, so the flanks match and the mark sits on the geometric centre. `"15:04"` is narrower than the date; the time clock keeps empty reserve rather than pulling the mark off-centre | Moving clocks to Left. Overlay at the midpoint. Accepting an off-centre mark |
| D11 | **Architecture amendment.** `2026-08-26-sysc-shell-design.md` currently forbids a continuous frame loop. Amend: a visible looping `GradientPaint` with reduced motion off is the named exception. The 60-minute idle gate still holds when reduced motion is on, or when no looping paint is on screen | Quietly looping the bar and leaving the architecture sentence in place |
| D12 | **`plugin/v1` unchanged.** A later semantic tone, if a plugin ever needs the look, is a different design. This slice does not put colours or motion on the wire | Porting `ui.gradient`. A `tone: "shine"` bump “while we are here” |

## Paint contract

`uv` is the fragment in 0…1 across the node's physical box.

```
axis = gradientAxisForDegrees(angleDeg)  // x, y, bias
t    = uv.x*axis.x + uv.y*axis.y + axis.bias - offset
colour = lerp the Count stops at t        // no clamp on t; first/last stops pin
```

Rect path: per-pixel (or per-span) sample, then the existing `blendPixel`.
Mask path: same sample, coverage from the mask, then `blendPixel`.
`paintWordmark` calls the mask path. `fillRect` / rounded fills call the rect
path when `Count > 0`.

Invalid recipes (`Count` 1 or >4, non-finite offset, unsorted stops) paint the
solid fallback the node already had (wordmark: `style.accent()`). Do not panic
in the painter.

## Motion contract

```
trip     = 2000ms
from, to = −0.45, 0.45          // wordmark; other nodes set their own
mid      = (from+to)/2
```

| Reduced motion | Visible looping paint | Frames |
|---|---|---|
| off | yes | `animateSurface` until the node leaves or reduced motion turns on |
| on | yes | one frame at `mid`; `Settled()` true |
| either | no | today's idle: no ticker |

Ping-pong: elapsed wraps a 2× trip and reverses. Loop: elapsed wraps one trip
and restarts `from`. Re-applying the same `TargetLoop` arguments does not reset
phase.

## Layout

Default Center:

```
clock 15:04 | wordmark | clock Mon 2 Jan
```

Both clocks tabular, `MinWidthText: "Wed 30 Sep"`. Capsules stay on the clocks
(`buildWidgets` still wraps them). The mark is naked. Vertical centering is
`placeSection`'s existing job.

## Files

| Path | Change |
|---|---|
| `internal/ui/tree.go` | `PaintRole`, `GradientMotion`, `GradientStop`, `GradientPaint`, `Node.Gradient`, `Node.GradientOffset` |
| `internal/render/gradient.go` | axis, sample, rect fill, mask blend (new) |
| `internal/render/paint.go` | `paintWordmark` uses the mask path; rect kinds honour `Gradient` when set |
| `internal/shell/animation.go` | `TargetLoop`, looping `at`/`settled` |
| `internal/shell/bar.go` | construct `anim`, write offset on the paint copy, frame loop, stop on drop |
| `internal/shell/widget.go` | `wordmark` case; clock floor; `capsuled` skips the mark |
| `internal/config/config.go` | `knownItems` + `Default` Center |
| `docs/plans/2026-08-26-sysc-shell-design.md` | D11 sentence |

## Testing

- Axis at 0° and 90°; snapped vertical; stop lerp at 0, 0.5, 1 and outside.
- Mask: two-stop horizontal ramp through a coverage mask, distinct end pixels.
- Animator: ping-pong never `Settled` until reduced or `Forget`; reduced parks
  at midpoint; identical `TargetLoop` keeps phase.
- Config: `wordmark` is known; format on it is rejected; Default Center is
  three items, clocks differ, wordmark in the middle.
- Widget: no capsule; `ImageH == 19`; section with mark + two clocks sets the
  shared `MinWidthText`.
- Bar: `anim != nil`; looping wordmark keeps invalidating; reduced motion
  stops the ticker.

Do not run `go test ./...` or `-race`. Per-package named tests only.

## Stop

The default bar shows a ping-pong wordmark between time and date. Removing the
item from config restores idle (no frame loop). Reduced motion freezes the
crest at the midpoint. `plugin/v1` is untouched.

Live Niri check is owner-deferred: scale 1 and one other scale; reduced-motion
toggle; confirm the mark sits on the midpoint between the two clock capsules.

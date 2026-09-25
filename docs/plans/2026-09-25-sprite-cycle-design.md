# Sprite Cycle Design — host-animated glyph cycles for plugins

Date: 2026-09-25
Commissioned by: the Cat plugin (`sysc-plugins/plugins/cat`), a port of noctalia's `cat` and DMS's
Cat Widget (dms-plugin-registry #562).

Extends the minor-6 declarative animation (`2026-09-21-declarative-value-animation-design.md`) from
values to poses. Adds three wire fields on the icon node and one animator channel; no new painter.

## Problem

Both reference cats animate from the plugin side: noctalia re-renders its widget on an 80 ms tick
whatever the load, and DMS swaps SVG sources from a QML timer. Ported literally, a sysc plugin would
ship one view patch per pose per visible cat: JSON encode, pipe, decode, validate, convert, and a
bar repaint, twenty times a second, spending a third of the plugin's 60/s update budget on a single
bar. The poses would also land on the plugin's clock rather than the compositor's.

The shell already owns a frame clock that does this properly. It animates hover, press, the
marquee sweep, effect phases and minor-6 value glides per surface, paints only on frame callbacks,
and honours reduced motion. A plugin should declare a pose cycle once and let that clock run it.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | An icon node gains `icon_size` (logical square), `frames` (2–32 catalogue names) and `cycle_ms` (one pass through `frames`, 100–60000; 0 holds still). `Icon` stays required and is the resting pose. | A new `sprite` node kind. The node is still one glyph in one box; a kind would duplicate the icon's conversion and paint for no new behaviour. |
| D2 | `frames` and `cycle_ms` require `icon_size` and a `key`, and `frames` requires `cycle_ms` and vice versa. | Deriving a box from the type ladder. A pose cycle must not reflow per pose, and the ladder tops out at 24 px, too small for a panel hero. |
| D3 | All three ride protocol minor 8. | A separate minor for `icon_size`. Minor 8 is unreleased; it carries the whole feature. |
| D4 | One channel, `animSprite`: a linear 0→1 loop over `cycle_ms`. A changed period keeps the current phase (the start is rebased), so a speed change never snaps the pose back to the first frame. An unchanged period is a no-op, so a rebuild keeps the phase. | Restarting the loop on retarget, as `TargetLoop` does. The Cat plugin retargets on every CPU sample; a restart would visibly hitch the gait every two seconds. |
| D5 | A resolve walk, `resolveSpriteMotion`, writes `frames[floor(phase × len)]` into the node's `Icon` on the render copy, beside `resolveProgressMotion` in the bar's render and the panel's render, spawn and rebuild. Unseen keys retire. | A paint-time read. Paint stays a pure function of the tree, per the minor-6 decision. |
| D6 | When the only values in flight are sprite cycles, `animateSurface` sleeps until the next pose boundary instead of ticking every 8 ms, and publishes once per pose. | The 8 ms tick under the 4 ms bar cap. A cycle never settles, so an always-visible bar cat would repaint the whole bar ~125 times a second to show at most thirty poses. |
| D7 | Reduced motion paints the resting `Icon` and schedules nothing for the node. | Slowing the cycle. Reduced motion exists to stop ambient motion, not to slow it. |
| D8 | Glyphs are the project font's, painted by `paintIcon` in the node's tone. Project glyphs are fitted to the square (`RasterProjectIconIn`): the font's design box is 1.2 em, and shaped at the square's size it spilled a fifth past it. | Letting plugins ship raster frames. Images are panel-only, carry their own colour, and break the theme contract that plugins name roles, never colours. |

## Wire (`plugin/v1/node.go`)

```go
IconSize int      `json:"icon_size,omitempty"` // 1..MaxIconSize (256)
Frames   []string `json:"frames,omitempty"`    // MinSpriteFrames (2)..MaxSpriteFrames (32) catalogue names
CycleMS  int      `json:"cycle_ms,omitempty"`  // MinCycleMS (100)..MaxCycleMS (60000)
```

`minorEight` validates: all three only on `icon`; bounds; each frame an icon identifier; `frames`
⇔ `cycle_ms`; both need `icon_size` and `key`. The converter resolves every frame against the
catalogue, so an unknown pose is a conversion error naming the path, as an unknown icon is today.

## Host flow

1. Converter: an icon with `icon_size` becomes `ui.KindIcon` with `IconSize`; with `frames`, it also
   carries `Key`, `Frames` and `Cycle`.
2. Bar `renderViewLocked` and panel `render`/`spawnPanelLocked`/`rebuildPanel` call
   `resolveSpriteMotion(anim, root)` beside `resolveProgressMotion`.
3. `animator.TargetCycle(key, cycle, poses)` installs or rebases the loop; `SpriteWait` reports the
   time to the next pose boundary when every unsettled value is a sprite.
4. `animateSurface` takes that wait: with only sprites in flight it sleeps to the boundary (plus a
   millisecond, so the index has moved) instead of the tick.

## Glyphs

The cat is forty original poses in the project font, drawn by `icons/cat.py` from one parametric
kit (torso masses, head, jointed legs, tail) under one shared fit, so it keeps its size and floor
line from act to act. The catalogue holds each distinct pose once; a cycle that returns through a
pose repeats its name in `frames`.

| Act | Poses | Notes |
|---|---|---|
| `sleep` | 4 | Breathing: the flank swells, the z marks rise. |
| `sit` | 4 | Three tail positions and a blink. |
| `groom` | 5 | Paw to mouth, licks, a wash over the face. |
| `scratch` | 3 | Hind foot raking behind the ear. |
| `stretch` | 4 | Stand, bow, deep bow, yawn. |
| `walk` | 8 | Half a lateral-sequence stride: the silhouette repeats every half stride. |
| `run` | 12 | A rotary gallop, sampled from a closed Catmull-Rom spline through six keyframes. |

## Plugin contract (Cat)

The plugin publishes an act -- its pose list and period -- with each CPU sample and each change of
act. Between those it sends nothing. Busy, the cat walks and then gallops, with hysteresis at the
switch; idle, it sits and at random grooms, scratches or stretches, naps after a set time, and
stretches on waking. A speed change eases over a few short retargets, which the host's
phase-keeping retarget makes seamless. With no view open the plugin holds no timers.

## Evidence

- Validator accept/reject matrix for the three fields.
- Converter: sized icon and sprite mapping; unknown pose rejected.
- Animator: phase kept across a period change; same period is a no-op; reduced motion holds; the
  wait lands on the next boundary; non-sprite motion keeps the tick.
- Walk: `Icon` follows the phase; retired keys drop.
- Plugin: lint of every view; pipe-level test that the plugin sends no per-frame traffic.

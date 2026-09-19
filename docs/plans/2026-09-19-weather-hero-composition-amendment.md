# Weather hero composition and motion amendment

Date: 2026-09-19. Owner-approved design amendment for `sysc-318`, commissioned
by `2026-09-18-weather-animation-polish-execution-handover.md`. This document
settles the design and the executable plan. `bd` owns task state.

## What the evidence changed

The commissioning handover assumed several defects that the code had already
fixed, and missed five that it had not. Frames were rendered through the shipped
`render.Paint` path at scale 1.25 by a throwaway harness in `internal/shell`,
deleted after capture. Evidence artifact: `Weather Hero Dossier`,
`https://claude.ai/artifact/RSap3sUk8xVqHZp67Lfk4D`.

Already true on `main` before this amendment, and therefore **out of scope**:

- the hero already takes the body remainder; the detail table and Daily/Hourly
  tabs are gone, and both surfaces already carry a compact four-slot strip;
- `weatherLocation` (`weatherpanel.go:58`) cannot emit raw coordinates — it
  returns the configured label, the geocoded name, the configured city, or
  nothing;
- all nine states already have distinct forms and motion.

Measured geometry, which drives the whole amendment:

| Surface | Hero effect box | Aspect |
|---|---|---|
| Standalone panel | 416 × 346 | 1.20 |
| Control Centre | 578 × 318 | 1.82 |

### Defects this amendment fixes

| ID | Defect | Anchor |
|---|---|---|
| F1 | Two weather marks drawn at once: a flat glyph and the effect's form | `weatherpanel.go:135` vs `weather_effect.go:106` |
| F2 | The form collides with the text it should sit behind | full-box painters under a uniform `FillScrim` |
| F3 | Text packs into the top third; the bottom third is empty | `layout.go:139` places a column row-child at `content.Y` |
| F4 | Only clouds honour the aspect cap | `weather_effect.go:193` caps; `:106 :366 :403 :438 :479` do not |
| F5 | The lightning bolt spans the whole card | `weather_effect.go:496` walks `0 → box.H` |
| F6 | The cloud bank reads as four stacked blobs with lighter lens overlaps | `weather_effect.go` `paintCloudScene`, four similar masses at wide colour steps |

## Approved decisions

1. **The mark** is the animated effect form. The static `KindIcon` glyph leaves
   the hero. It stays in the forecast strip and the bar, which are unchanged.
2. **Information budget**: a centred temperature / condition / low-high stack,
   plus one quiet meta line carrying place, feels-like, wind, humidity and
   freshness. The three-cell essentials grid and the separate freshness line go.
3. **Engine**: the bounded CPU raster is kept. Every defect above is composition
   or layering; none is throughput, so no engine or GPU backend is justified.
   `BenchmarkPaintWeatherEffect` is the measurement of record.
4. **Scene direction B, atmospheric depth**, with two disciplines that keep it
   from degenerating into the "animated gradient" it is meant to replace:
   - the sky gradient is **static per state**. The travelling per-pixel sinusoid
     in `paintSkyCloudWash` is removed; it is the thing that reads as a shimmer.
   - depth comes from **parallax rate and silhouette**, not from alpha alone.
5. **The scene never stretches.** It is locked to the standalone hero's 1.20
   aspect in every consumer.
6. **Composition is chosen by measured aspect, not by surface identity.** Below
   `weatherSplitAspect` the hero composes stacked; at or above it, split.

### Amendment to the commissioning handover

The handover requires that "temperature and condition sit on the vertical
centreline of the large weather mark". That holds in the stacked composition.
In the split composition the text sits **beside** the mark, vertically centred
against it, and does not share its vertical centreline. This was put to the
owner with the trade-off stated and approved on 2026-09-19. The clause is
amended to: *the temperature and condition stack is centred against the mark —
on its vertical centreline when stacked, on its vertical midpoint when split.*

## Scene grammar

Unchanged from the handover's table for form and motion character, with these
corrections:

- every painter is placed inside the **scene box**, not the effect box. The sky
  alone fills the effect box edge to edge, so a wide card shows more sky and
  never a seam or a stretched form;
- lightning originates at the **cloud base** and descends; it never starts at
  the card's top edge;
- a cloud bank is **one silhouette with depth**. `paintCloudMass` already took
  the maximum coverage across its own puffs, so a single mass never had seams;
  the lenses came from four similarly-shaped masses separated by wide colour
  steps. The masses are now separated by scale and position instead, with much
  narrower colour steps, and the bank is centred so it works as the mark.

## Architecture

`internal/ui` keeps carrying only validated declarative effect data. One new
bounded scalar is added to `EffectSpec`:

```go
// SceneBias places the aspect-locked scene horizontally inside the effect
// box: 0 centres it, -1 puts it hard against the leading edge. The host
// chooses it from the composition; the renderer does not re-derive the
// composition rule.
SceneBias float64 // validated to [-1, 1]
```

This keeps a single owner for the composition decision. The alternative —
letting the renderer derive alignment from aspect — would duplicate the
threshold in two packages.

Everything else holds: `internal/render` owns pixels and bounded work;
`internal/shell` owns phase, visibility, reduced motion and invalidation;
effects paint as the first child of the existing stack and never take part in
layout or hit testing; seeds and phase stay stable across refreshes; plugin/v1
and the plugins repository are untouched.

## Plan

Each slice lands a focused failing check before its production change.

### Slice 1 — `EffectSpec.SceneBias`

- Test: `Validate` rejects `SceneBias` outside `[-1, 1]`, NaN and Inf; accepts
  the bounds.
- Change: add the field and its validation to `internal/ui/effect.go`.

### Slice 2 — one scene box for every painter (F4)

- Test: at a 1.82 effect box with `SceneBias: 0`, the painted non-sky extent is
  no wider than `height × 1.20` and is horizontally centred; at `SceneBias: -1`
  it is flush to the leading edge.
- Change: compute the scene box once in `paintWeatherEffect` and pass it to
  `paintCelestial`, `paintFogHaze`, `paintRain`, `paintSnow`, `paintLightning`
  and `paintCloudScene`. Replace `weatherCloudMaxAspect` with the shared lock.

### Slice 3 — one cloud bank instead of four blobs (F6)

- Change: separate the four masses by scale and position rather than colour,
  shrink the highlight to a lit cap on the front mass's shoulder, and centre
  the bank in the scene. Verified by re-rendering every state, not by a unit
  test: "reads as one bank" is a composition judgement, and a pixel assertion
  on it would lock in this exact geometry.

### Slice 4 — lightning from the cloud base (F5)

- Test: no bolt pixel is painted above the cloud base fraction of the scene box.
- Change: start the segment walk at the cloud base and descend.

### Slice 5 — static sky, layered precipitation (direction B)

Making the sky phase-independent also made it row-invariant, which turned the
dominant per-frame cost into a per-row one. Combined with dropping `math.Hypot`
from the cloud coverage inner loop, the bounded benchmark improved from
41.7ms to 10.9ms at the Control Centre size -- the effect loop never settles,
so this ran on every frame and previously could not hold the 16ms cadence.

- Test: two phases that differ produce an identical sky band away from the
  forms, and differing precipitation; reduced-motion parity still holds.
- Change: drop the travelling sinusoid from `paintSkyCloudWash`; give rain and
  snow three depth layers with per-layer rate, length and alpha.

### Slice 6 — hero composition (F1, F2, F3)

- Test: the hero tree carries no `KindIcon`; the stacked hero's text group is
  centred in the card's content height; at a 1.82 card the tree composes split,
  with the text column in the recovered width and the effect's `SceneBias`
  negative.
- Change: rework `weatherHeroCard` to choose composition by aspect, using the
  existing `CenterX` / `CenterY` column facilities. Replace `weatherEssentials`
  and the freshness line with one meta line.

### Slice 7 — gates

`go vet`, `gofmt`, the focused `internal/ui`, `internal/render` and
`internal/shell` package tests, the module-diff gate, and
`BenchmarkPaintWeatherEffect` at both sizes.

## Layout fixes this required

Three defects in the shared layout, each pre-existing and each found by this
work rather than introduced by it:

- `columnChildHeight` ignored an explicit `Height` on `KindColumn` and
  `KindRow`, unlike the capsule, meter and gauge arms beside it. A caller that
  sized either container silently got the intrinsic height, which left
  `CenterY` no slack to centre within.
- `centeredTrack` narrowed a centred track to the exact logical text width.
  Layout measures at the logical size and the painter at the size rounded to
  physical pixels, which is 1-3px wider at scale 1.25, so every `CenterX` text
  truncated at that scale. This already affected the Control Centre gauge
  captions on `main`.

## Out of scope

The bar widget stays static. Plugin/v1, the plugins repository, weather data
and error semantics, and panel right-click routing are untouched. No reference
artwork, vector or code is copied from the repositories named in the
commissioning handover; the scene grammar is described behaviourally and the
geometry is shell-owned.

# Weather animation polish execution handover

Date: 2026-09-18. This document commissions the next design and implementation
pass for `sysc-318`, the weather hero and motion correction. The first effect
implementation is landed; this handover is for making the scene read as
legitimate weather at a glance and for leaving the effect path useful to later
shell surfaces.

`bd` owns the state of this work. Do not add a status header here or use this
document as a task tracker.

## Receiving state

The first host-owned effect slice landed in `59741f6` and is present on
`main` and `origin/main`. It provides:

- a validated `ui.EffectSpec` and retained `KindEffect` layer;
- deterministic CPU weather rasterisation in `internal/render`;
- shared surface animation and invalidation for the standalone Weather panel
  and the Control Centre weather page;
- effect clipping through the existing semantic card shape; and
- a static bar widget and unchanged plugin/v1 presentation.

The approved governing documents are:

- `2026-09-16-effect-rendering-design.md` and
  `2026-09-16-effect-rendering.md`;
- `2026-09-16-weather-hero-composition-design.md` and
  `2026-09-17-weather-hero-composition.md`; and
- `2026-09-15-weather-panel-design.md` for data, state, and accessibility
  contracts.

The primary checkout contains later local documentation and unrelated dirty
M10 work. Do not push that checkout wholesale. The laptop's working binary was
restored after live validation. A raw build of `59741f6` rejected the laptop's
newer grouped-instance configuration; a clean temporary merge of the weather
commit onto the laptop's `c3278b29` compatibility base started successfully.
Any future live build must be based on the current integration line, with
provenance recorded, rather than deploying the stale weather commit alone.

## Commission

Treat the hero as the product. The next pass must make the weather scene the
dominant visual element and make the supporting information deliberately quiet.
The standalone panel and Control Centre page share the same visual vocabulary
but adapt the composition to their available aspect ratio.

The pass must resolve these owner observations:

- the hero occupies most of the available weather body; the forecast remains
  a compact secondary strip;
- raw latitude/longitude never appears in the hero, especially not as a
  top-left eyebrow. A configured human-readable place name may appear only if
  it earns its space;
- temperature and condition sit on the vertical centreline of the large
  weather mark, not above it, and remain aligned when the card is stretched in
  Control Centre;
- rain, snow, cloud, fog, and lightning read as physical weather motion rather
  than as a generic animated gradient; and
- the information budget stays small enough that the scene can be understood
  before the labels are read.

## Required design-first sequence

Do not start by adding more particles or importing an engine. First produce an
evidence-backed design amendment and obtain owner approval for it. The design
must include a small comparison of these implementation paths:

1. Extend the existing bounded CPU raster catalogue. This is the recommended
   default because it already composes inside a retained `KindStack`, follows
   the shell's frame callbacks, and needs no new dependency.
2. Evaluate an installed or small engine such as Pixel or Ebitengine only if a
   measured prototype shows that the current raster path cannot deliver the
   required motion, density, or power budget. G3N and a game-style window loop
   need an especially clear composition and lifecycle justification.
3. Consider a GPU/shader backend only after a measured CPU failure. It must
   preserve the shell's surface ownership and must not turn shader source,
   pixel buffers, or an independent frame loop into a plugin API.

The design amendment must settle the choice with measurements and ownership
arguments, not aesthetic preference. It must also describe how a second real
effect consumer would use the host catalogue without introducing a
single-implementation interface or speculative plugin framework.

## Reference research

Use these as behavioural and visual references, not as assets to copy:

- [Weather Effect Generator](https://github.com/hgupta01/Weather_Effect_Generator)
- [react-weather-effects](https://github.com/rauschermate/react-weather-effects)
- [google-weather-icons](https://github.com/mrdarrengriffin/google-weather-icons)
- [Breezy Weather pixel icon provider](https://github.com/breezy-weather/pixel-icon-provider)
- [WeatherMaster](https://github.com/PranshulGG/WeatherMaster)
- [Froggy](https://github.com/R0rt1z2/Froggy)

Google Pixel and default Android weather are inspiration for hierarchy,
silhouette, restraint, and motion cadence. They are not a source for copied
artwork, proprietary vectors, or proprietary code. Check the licence of every
source before reusing code or assets; prefer a behavioural description and
new shell-owned geometry where the licence or provenance is unclear. The
existing Meteocons motion references in the effect design remain useful for
cadence, not for runtime assets.

The research should capture the current shell at the laptop's 1.25 scale and
compare at least the standalone panel and the stretched Control Centre card.
Record what makes each state readable at thumbnail size: large silhouette,
contrast, parallax, particle depth, entry/exit timing, and the amount of
supporting text.

## Visual and motion contract

The design amendment must specify a scene grammar for every supported state:

| State | Required large form | Required motion character |
|---|---|---|
| Clear day | Sun disc, halo, and restrained rays | Slow breathing or orbit; never a noisy shimmer |
| Clear night | Moon/crescent and quiet sky depth | Slow halo change; reduced visual energy |
| Partly cloudy | Celestial body behind separated cloud masses | Layered parallax with visibly different depths |
| Cloudy | Overlapping cloud bank with a readable underside | Shallow lateral/lift drift |
| Fog | Broad translucent bands with depth separation | Very slow lateral haze movement |
| Rain | Cloud bank plus varied diagonal drops | Gravity, wind, depth, staggered fade and entry |
| Snow | Cloud bank plus flakes of varied size and depth | Gentle drift with non-uniform trajectories |
| Thunderstorm | Cloud bank and rain | Short irregular lightning illumination, not a regular flash |
| Unavailable/error | Existing truthful error or placeholder content | No decorative animation pretending data exists |

The implementation may combine states internally, but every state must have a
recognisable large form before its motion is considered successful. Effects
remain decorative: foreground text and accessible weather facts are always
authoritative and legible above them.

The normalized scene must resist the Control Centre's wider box. It may use a
constrained scene aspect, deliberate letterboxing, or another measured layout,
but it must not simply stretch clouds, precipitation, or the celestial mark.
The hero layout must keep the temperature/condition stack centred against the
weather mark at the target density and at scale 1.25.

## Architecture and safety invariants

Keep these existing boundaries unless the approved design amendment proves a
measured need to change one:

- `internal/ui` carries only validated declarative effect data; it does not
  import renderer types.
- `internal/render` owns pixels, clipping, bounded work, and the weather
  catalogue.
- `internal/shell` owns phase, visibility, reduced motion, and surface
  invalidation; one surface has one animation owner.
- Effects paint as the first child of the existing stack, never participate in
  layout or hit testing, and cannot intercept actions or focus.
- Seeds and phase remain stable across weather refreshes, so a data update does
  not reshuffle the scene or flicker the hero.
- Particle counts, canvas bounds, alpha, speed, and frame pacing are host
  bounded. No untrusted input controls allocations, loop counts, shader source,
  or submission rate.
- Reduced motion parks a deterministic calm frame and stops moving particles,
  haze drift, and lightning flashes.
- Plugin/v1 and the separate plugins repository remain unchanged. A future
  protocol-minor capability may request a named host effect, but it may not
  provide arbitrary code, shaders, image buffers, or a private clock.

## Required outputs

The receiving session is responsible for the following artifacts, in order:

1. A short research/design amendment with state references, licence notes,
   engine decision, scene grammar, information hierarchy, aspect-ratio rules,
   accessibility, and measured performance targets. Owner approval gates code.
2. An executable implementation plan registered in `docs/plans/README.md`,
   with focused failing checks before each non-trivial production change.
3. The smallest implementation that makes the approved hero contract true,
   with the current renderer and scheduler reused where they fit.
4. A completion handover containing commit provenance, automated gate output,
   benchmark results, laptop screenshots/observations, reduced-motion evidence,
   and the before/after Niri layer state.

Record newly discovered work in `bd` under `sysc-318`; do not maintain a
parallel checklist in this document.

## Exit gate

The commission is complete only when evidence shows all of the following:

- The standalone Weather panel and Control Centre weather page each have one
  dominant hero and a compact forecast strip; the hero is the largest region
  and the old detail-table/hourly clutter is absent.
- No raw coordinates appear in the hero. Temperature and condition are
  vertically centred with the large weather mark in both target surfaces.
- Every state in the visual contract has a readable large form and a distinct,
  deterministic motion treatment. The same seed and phase produce the same
  pixels; a changed phase produces a meaningful hero-region change.
- Foreground text remains readable, semantic clipping holds at rounded corners,
  reduced motion is calm and static, and closing or hiding a surface stops its
  effect invalidations.
- The static bar, plugin/v1 wire path, weather data/error semantics, and panel
  right-click routing remain intact.
- Focused UI/renderer tests, affected package tests, `go vet`, formatting, the
  module-diff gate, and the bounded weather benchmark pass. Any pre-existing
  environment failure is named with its exact command and not relabelled as a
  product pass.
- The exact laptop build is provenance-checked, runs as one user service
  process, is readable at scale 1.25 in both surfaces, and leaves no panel or
  shield layer after close. This machine has one output; no two-output claim
  may be inferred from this gate.

Do not merge a future implementation from a stale base or deploy a second
shell process. Rebase onto the actual integration line, preserve unrelated
M10 work, and stop at the approved visual and performance contract.

# Weather Hero Composition and Motion Design

Date: 2026-09-16.

Owner-approved correction to the first animated-weather pass. The weather
surfaces should make the current condition the subject, not present a settings
table with an effect behind it.

## D1 — Scope and ownership

This slice redesigns the standalone `PanelWeather` surface and the Control
Centre Weather page. The bar weather widget remains static and continues to be
the compact entry point. The existing weather service, retained UI tree,
`KindStack`, CPU `wl_shm` painter, surface animator, and frame scheduler remain
the owners of data, composition, pixels, phase, and invalidation respectively.

No new graphics engine, GPU backend, image asset, plugin protocol, or external
dependency is required. The existing `KindEffect` weather program is extended
only where the observed visual gap requires it.

## D2 — Visual hierarchy

Each target surface has one visual statement:

```text
header
┌──────────────────────────────────────────┐
│             animated weather hero        │
│       location · temperature · condition  │
│       low/high · feels-like · wind · RH   │
└──────────────────────────────────────────┘
        compact four-day forecast strip
```

The hero occupies the available width and most of the content height. Its
effect is the first child of the existing stack, a restrained scrim follows,
and the foreground content is last. This preserves contrast and the existing
input/accessibility boundary while making the weather state unmistakable.

The standalone panel keeps its close header, then uses the full body for the
hero and a small forecast strip. The Control Centre page uses the same content
grammar in its large Today card and keeps the existing four forecast slots.
There is no second detail card and no Daily/Hourly tab in this surface.

## D3 — Information budget

The hero keeps only information needed to answer “what is it like here now?”:

- configured or resolved location;
- current temperature;
- condition word;
- today's low/high range;
- feels-like, wind, and humidity when available; and
- update time or stale age when relevant.

The forecast strip shows four following days with day label, condition glyph,
and high/low range. Elevation, UV index, timezone, sunrise, sunset,
precipitation chance, and the hourly view are removed from these two surfaces;
the service may continue to carry fields needed by other consumers.

Unavailable and stale readings retain the existing truthful error vocabulary.
An unavailable reading gets no animated effect and keeps its accessible
message. A stale but observed reading keeps the last observed condition and
labels its age.

## D4 — Motion language

The effect must be readable at a glance before it is appreciated as motion.
Every weather variant therefore has a distinct large-form layer plus its
secondary motion:

| Condition | Large form | Motion |
|---|---|---|
| Clear | sun or moon disc with a soft halo | slow ray rotation or halo breathing |
| Partly cloudy | sun/moon behind two cloud masses | cloud lift/drift and slow celestial movement |
| Cloudy | overlapping cloud bank | shallow layered drift |
| Fog | broad translucent bands | low-frequency lateral haze movement |
| Rain | dark cloud bank | staggered diagonal drops with entry/fade |
| Snow | cloud bank | flakes with varied size and gentle drift |
| Thunderstorm | cloud bank and rain | short irregular lightning flashes |

The clear and partly-cloudy forms are deliberately not a barely visible
full-card colour wash. Rain, snow, and lightning remain particle effects, but
their contrast and spacing are calibrated against the actual card size rather
than relying on a viewer to notice a one-pixel change.

The motion reference is the public MIT-licensed Meteocons project:

- [cloud animation config](https://raw.githubusercontent.com/basmilius/meteocons/main/animations/partials/clouds.json)
  uses a restrained three-second vertical lift;
- [sun animation config](https://raw.githubusercontent.com/basmilius/meteocons/main/animations/partials/sun.json)
  rotates rays over six seconds;
- [rain animation config](https://raw.githubusercontent.com/basmilius/meteocons/main/animations/partials/rain.json)
  staggers drops and fades them at both ends; and
- [lightning animation config](https://raw.githubusercontent.com/basmilius/meteocons/main/animations/partials/lightning.json)
  uses an irregular short flicker rather than a constant pulse.

These are behavioural references only. No external runtime or asset is copied
into the shell.

## D5 — Renderer contract

The weather effect remains a validated `EffectSpec` with fixed host-owned
particle limits, a stable seed, and a resolved phase. The raster pass uses
semantic theme roles for the sky, cloud, celestial, precipitation, and flash
colours. All forms are clipped by the existing card mask and canvas bounds.

The effect's work is proportional to the bounded hero card, not the full
output. Primitive geometry uses integer-clamped coordinates and the current
premultiplied blend helpers. The renderer does not expose a callback, shader
source, pixel buffer, or independent clock to a plugin.

## D6 — Animation lifecycle

The existing per-surface animator remains the one phase owner. A visible
weather effect keeps that surface's frame loop alive; its stable key preserves
phase across weather refreshes and tree rebuilds. Standalone Weather and
Control Centre Weather must both be eligible for effect invalidation. The
media-page condition is not a general weather visibility test.

Closing a surface removes its effect targets and stops its frame loop. A hidden
Control Centre page has no weather effect target. Reduced motion parks every
weather effect at a calm deterministic phase and requests no particle or haze
loop.

## D7 — Accessibility and interaction

Effects are decorative, non-focusable, and have no action. The card retains its
existing accessible region and weather text. Foreground text remains readable
over every supported variant through the scrim and semantic contrast roles.
The effect never changes layout, hit testing, focus order, or the static bar
widget.

## D8 — Verification

The implementation leaves focused checks for:

1. hero-first tree composition and the reduced information budget;
2. a visible clear/partly-cloudy form, not only a low-alpha wash;
3. deterministic output for identical seed and phase;
4. phase-dependent output for each moving program family;
5. reduced-motion output with no moving state;
6. rounded clipping and foreground-over-effect paint order;
7. standalone and Control Centre frame invalidation while visible; and
8. laptop Niri open/close lifecycle with no leaked surface.

The exact frame cost is measured on the laptop before any decision about a
different backend. Passing a pixel-difference test alone is insufficient: the
hero-region assertions must show that the effect has meaningful coverage and
contrast.

## D9 — Non-goals

- animated weather in the bar;
- preserving the removed detail rows or hourly tab in these surfaces;
- a general weather-scene toolkit;
- external SVG/Lottie/game-engine integration;
- plugin/v1 changes; and
- GPU work without a measured CPU frame-budget failure.

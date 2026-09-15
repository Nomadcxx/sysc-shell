# Effect rendering design

Date: 2026-09-16.

Owner-approved design for animated weather surfaces and a future-safe host effect
path. The first consumer is the standalone Weather panel and the Control Centre
weather page. The bar weather widget remains static in this slice because its
available area cannot carry the reference treatment coherently.

## D1 — Goal and scope

The shell needs animated visual states for rain, snow, fog/haze, cloud motion,
lightning, and richer transitions on the two larger weather surfaces. The same
rendering boundary must be usable by later first-party surfaces and supported
plugins without giving a plugin a renderer, a pixel buffer, a shader compiler,
or an independent frame loop.

This design does not add a GPU backend, an external game engine, or an animated
bar weather treatment. It establishes the host-owned effect seam and implements
one complete software effect program for weather.

## D2 — Ownership boundary

Effects are declarative requests interpreted by the shell. The shell owns the
effect catalogue, palette roles, time source, frame pacing, clipping, reduced
motion policy, and resource limits. A plugin may eventually request a named
host effect, but it may not provide shader source, executable rendering code,
arbitrary image buffers, or a callback.

The renderer remains the only owner of pixels. The Wayland owner goroutine
remains the only caller that paints and submits a frame. Effect evaluation is a
bounded part of that existing paint pass; it does not create a second Wayland
dispatch loop or a renderer-owned goroutine.

## D3 — Effect model

The retained UI tree gains one non-interactive effect layer kind. It is a leaf
that occupies the content box supplied by its parent and is normally placed as
the first child of an existing `KindStack`; ordinary content remains later in
the stack and therefore paints above it. The existing stack paint and reverse
hit-test order already express the required z-order invariant.

An effect layer carries a host-owned descriptor with:

- a catalogue program name;
- a bounded program variant;
- a deterministic seed;
- bounded intensity and speed values; and
- a resolved phase supplied by the surface animator.

The descriptor is data, not an interface or a plugin callback. The renderer
dispatches supported programs through its concrete catalogue. The first
program is `weather`, whose variant is the WMO condition category. New
programs are added only when a real shell or plugin consumer needs one; this
keeps the common path small while leaving the layer contract reusable.

Effect nodes have no action, focus, accessibility identity, or input events.
They cannot intercept a click or become a focus target. A malformed or
unsupported descriptor rejects the containing tree through the same validation
boundary as any other invalid UI data; the host never paints a partial,
unvalidated effect.

## D4 — Compositing and clipping

The effect is painted into its arranged bounds before the foreground children.
The renderer applies the node's semantic shape mask, so rain, snow, haze, and
lightning cannot escape a rounded card. Theme colours are resolved from the
same semantic roles as the surrounding card and content; the effect does not
introduce a second palette.

The effect layer reuses `KindStack` rather than adding a parallel card or
surface composition system. A weather card therefore has the same structure as
the shipped media card: effect/background, optional scrim, then foreground.
The card's normal chrome and the foreground remain responsible for contrast,
border, interaction state, text, and accessibility.

## D5 — Animation and lifecycle

The surface animator supplies a monotonic phase to every live effect layer.
Phase progression uses the existing invalidation path and frame callback
backpressure. There is one animation owner per surface and no timer per effect.

The host controls the maximum effect frame rate. The first implementation is
bounded to the surface's resolved pacing cap and targets no more than 30 effect
frames per second on the small weather cards. A hidden or closed surface has no
effect loop. Once the surface is idle, no invalidations are requested.

Reduced motion freezes a calm, deterministic representative frame and disables
moving particles, haze drift, and lightning flashes. A weather data refresh
may change the static variant, but it does not bypass the accessibility rule.
Seeds and phase are stable across tree rebuilds, so a data update does not
randomly reshuffle particles or produce visible flicker.

## D6 — Weather program

The weather program maps the shared weather vocabulary to a small set of
bounded procedural layers:

- clear and partly cloudy: slow cloud/sky modulation;
- cloudy: denser cloud veil;
- fog: low-frequency haze with a static reduced-motion frame;
- rain: deterministic diagonal streak particles and a subtle surface wash;
- snow: deterministic flakes with varied size and drift;
- thunderstorm: rain plus intermittent deterministic lightning illumination;
- unavailable/error: no animated weather layer and the existing stale/error
  presentation remains authoritative.

The effect is decorative. Current temperature, condition, stale age, and all
accessible labels continue to come from the existing weather model and tree.
The standalone panel and Control Centre page consume the same resolved weather
variant and visual state; the bar consumes none of the animated layer in this
slice.

## D7 — Renderer backend choice

The first backend is a procedural raster pass over the existing premultiplied
`wl_shm` canvas. The target region is a bounded weather card, not the full
3440-pixel output, so this keeps the current ownership and compositing model
while providing shader-like visual states.

Pixel, Ebitengine, and G3N are not introduced for this slice. Their normal
window/game loops do not compose naturally into a child of the shell's rounded
card and would create another surface/lifecycle owner. EGL/OpenGL is also not
introduced: a GPU texture would need a new dmabuf path or a readback into the
current `wl_shm` buffer, which is more moving parts than the measured target
requires.

The effect descriptor and frame inputs remain backend-neutral. If a benchmark
on the laptop shows that the bounded software pass cannot meet its frame or
power budget, a later backend can consume the same inputs. That decision must
be measurement-led; this design does not reserve an unused engine abstraction.

## D8 — Plugin compatibility path

The current plugin v1 wire contract is unchanged. Its decoder rejects unknown
fields, and its converter intentionally admits only validated host-owned
elements. Adding an effect field to a v1.0 message would therefore be a
protocol violation, not a compatible extension.

When a real supported plugin needs effects, a negotiated protocol minor will
add an `effects` capability and a declarative effect node. The wire descriptor
will use the host catalogue and fixed limits, then convert to the same internal
effect layer used by first-party surfaces. Older plugins continue to send
ordinary v1.0 trees. A plugin requesting an unsupported program or invalid
parameter receives a normal view validation failure and cannot affect other
surfaces.

The plugin repository remains independent of the built-in weather feature. It
can consume the future effect capability for supported plugins without
bundling a graphics engine into each plugin process.

## D9 — Trust, performance, and failure invariants

- No untrusted input controls loop count, allocation size, canvas dimensions,
  shader source, or frame submission rate.
- Particle counts and effect work are fixed or clamped by the host catalogue.
- All writes are clipped to the effect bounds and the canvas.
- The effect never changes layout, text measurement, hit testing, or focus.
- A missing or invalid effect falls back to the normal card and content path;
  weather data and accessibility remain usable without animation.
- Frame scheduling remains coalesced by `render.Scheduler`; an effect cannot
  submit a frame while a compositor callback or buffer ownership rule forbids
  it.
- `go test -race`, `go vet`, and the existing format/module gates remain the
  required repository proof; effect tests use deterministic phase and seed
  inputs rather than timing sleeps.

## D10 — Verification and live gate

The implementation must leave focused runnable checks for:

1. deterministic output for the same program, seed, and phase;
2. phase-dependent output for an animated program;
3. reduced-motion output with no moving state;
4. rounded clipping and foreground-over-background paint order;
5. invalid descriptor rejection and bounded parameter handling; and
6. surface animation stopping when the weather surface closes or is hidden.

The laptop gate opens both the standalone Weather panel and the Control Centre
weather page, observes rain/snow/fog/lightning variants, checks reduced motion,
and runs `niri msg -j layers` before and after closing. The result records
actual frame behaviour and any CPU or visual calibration issue; it is not
inferred from passing unit tests.

## D11 — Non-goals

- Animated weather in the bar widget.
- Arbitrary plugin shaders, scripts, pixel streams, or native code.
- A general-purpose UI or graphics toolkit.
- Hourly weather data or a new weather service.
- A GPU dependency before a measured software-backend failure.
- A second Wayland surface for each effect.

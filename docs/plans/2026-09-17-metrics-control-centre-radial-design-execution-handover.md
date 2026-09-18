# Metrics Control Centre radial design commission

Date: 2026-09-17. Parent design: `2026-09-16-metrics-control-centre-gpu-design.md`.

This commission is for the design agent who will resolve the Control Centre Home
System composition and return implementation-ready visual artifacts. It does
not authorize product-code changes. The receiving agent must produce the
design and artifacts first; implementation resumes after the owner reviews
that handover.

## Problem to solve

The Home System section now needs four truthful radial metrics:

1. CPU usage
2. Memory usage
3. CPU temperature
4. GPU usage

The current provisional patch reduced the radial diameter from the established
40px gauge to 22px and placed a text value beside each ring. That change made
temperature easy to lose, made memory look like a large standalone percentage,
and let the layout decide the visual language before anyone measured it. Treat
that layout as rejected. Do not use it as the baseline or call it complete.

The GPU data path remains a separate correctness requirement. The design must
keep a real GPU usage value visible when the metrics snapshot contains one, and
must show an explicit unavailable state when the selected device or usage is
not valid. It must not hide GPU behind a generic memory-style value or invent a
zero.

## Reading list

Read these before proposing a layout:

- `AGENTS.md`
- `docs/plans/2026-09-16-metrics-control-centre-gpu-design.md`
- `docs/plans/2026-09-17-metrics-control-centre-gpu.md`
- `internal/shell/controlcenter_pages.go`
- `internal/shell/metricwidget.go`
- `internal/ui` radial-gauge measurement and paint code
- the existing Control Centre tests around `ccHome` and `ccResourceGroup`
- the current theme metrics and density configuration

Use Noctalia and DMS as behaviour and hierarchy references only. Do not copy
QML, C++, configuration, or a second widget toolkit into this repository.

## Fixed contracts

The design agent may choose the composition, but may not weaken these
contracts:

- Keep `ui.KindRadialGauge` as the gauge primitive. Do not add a new radial
  widget or a general layout toolkit.
- Preserve the established 40px radial geometry unless a measured design
  artifact proves a different size preserves legibility and the owner
  approves it. A 22px ring is not an acceptable default merely because it fits
  the current card.
- Keep the four metrics in the order CPU, memory, CPU temperature, GPU.
- CPU, memory, and GPU show percentages. CPU temperature shows degrees Celsius
  as its value. The temperature ring keeps the existing `Celsius/100` fraction
  contract.
- A valid zero remains visible as `0%` or `0°C`. Nil, invalid, unleased,
  failed, ambiguous, or disappeared data renders the existing unavailable
  state with no stale value.
- The selected GPU identity comes from the shared snapshot and exact selector.
  The design must not imply that a list position, wildcard history, or random
  device is being measured.
- Preserve the existing icon vocabulary, theme roles, density scaling, fixed
  Home card bounds, and retained-tree invalidation semantics unless the design
  records a concrete amendment and the owner approves it.
- Labels, tooltips, and accessible names must identify CPU usage, memory usage,
  CPU temperature, and GPU usage. A colour or ring alone cannot carry the
  metric identity.
- The gauges must remain readable at the supported scale and must not turn a
  decorative 40px visual into an interactive target smaller than the project's
  accessibility minimum. If the gauges are decorative and the surrounding
  card is the hit target, say so explicitly.

## Design questions

Compare at least these three compositions against the fixed card dimensions.
The agent may recommend another composition only after measuring these
options and explaining why they fail.

### Option A: four 40px gauges in a two-by-two grid

Keep the existing ring and place a short label/value block with each gauge.
Measure whether the fixed System card can hold two rows without clipping or
forcing body text into a display role. Check the temperature value and the GPU
label at the narrowest density.

### Option B: four 40px gauges in a compact horizontal composition

Keep the ring size and arrange the four slots in one measured row or two
paired groups, with labels and values in a consistent secondary line. Prove
the minimum card width, gap, and text width. Do not solve overflow by silently
shrinking the ring or dropping the temperature value.

### Option C: preserve a 40px ring and give the System section more vertical room

Keep the current visual hierarchy and change the surrounding Home composition
or card allocation so the gauges have room. Measure which neighbouring card
or gap changes, what the total Home height becomes, and whether the fixed
surface contract can accept it. State any surface-size or scroll consequence.

For every option, show the unavailable state, a valid-zero state, a normal
GPU value, a normal CPU temperature, and a three-digit maximum percentage.
The mockup must make it impossible to confuse memory percentage with a large
card headline.

## Required output

Return one owner-facing design package under:

`docs/plans/assets/2026-09-17-metrics-control-centre-radial-design/`

The package must contain:

- `README.md`: artifact index, source revision, toolchain, and how each artifact
  maps to the approved choice;
- `design.md`: the proposed decision, rejected alternatives, dimensions,
  spacing, typography roles, icon treatment, state treatment, and the exact
  implementation seams in `ccHome`/`ccResourceGroup`;
- one dimensioned mockup for each compared option, in SVG or another
  lossless, inspectable format;
- one owner-facing PNG or equivalent raster preview of the recommended option
  at the target logical size;
- a state sheet covering normal, valid zero, unavailable, CPU-temperature,
  GPU-selected, and GPU-identity-ambiguous states;
- `measurements.md`: card bounds, ring diameter, row/column gaps, label/value
  widths, text sizes, hit-target bounds, and the scale/density assumptions;
- `acceptance-checklist.md`: visual and implementation checks that an engineer
  can run without interpreting the mockup by eye.

Do not create generated Go, QML, C++, Rust, Lua, or Quickshell code. Do not
add runtime assets that the shell does not need. If the recommendation needs a
new icon or raster asset, include the source, licence, dimensions, and reason
the existing icon font cannot cover it.

## Required visual checks

The design package is incomplete until it answers these checks with measured
artifacts:

- Does the recommended layout retain the 40px gauge, or does it present
  evidence for a different size?
- Can a reader identify all four metrics without hovering?
- Is CPU temperature still visible as Celsius rather than converted to a
  generic percentage label?
- Is GPU usage visible as its own metric when the snapshot reports `0%`, a
  non-zero value, or a selected PCI identity?
- Does the unavailable state preserve geometry and avoid stale or fabricated
  text?
- Does the layout fit the fixed Home card at scale 1.0 and the supported
  density settings without clipping, overlap, or accidental reflow?
- Do text and ring contrast meet the repository's existing accessibility
  thresholds? Does the design work without colour as the only signal?
- Are labels, tooltips, focus order, keyboard behaviour, and hit targets
  defined for the actual interaction model?
- Does the grouped refresh need to compare ring value, unavailable state,
  value text, and graph samples so a temperature or GPU change invalidates the
  visible tree? The design must call out this existing retained-tree invariant,
  not assume text-only comparison is enough.

## Handover format

When the design agent finishes, return a completion handover containing:

1. the source revision and exact artifact paths;
2. the recommended option and the measurements that support it;
3. the 40px decision and any owner approval needed for an amendment;
4. the final metric-to-slot mapping, including GPU unavailable and ambiguous
   identity states;
5. the implementation changes the next engineer should make, named by file and
   function, without editing those files in the design commission;
6. the checks run on the artifacts and their results;
7. unresolved questions or hardware-dependent observations;
8. a short list of rejected approaches and why they were rejected.

Do not claim that GPU telemetry works from a mockup. GPU functionality is
qualified separately by the metrics service tests and the restricted Niri
`DP-1`, 3440×1440, scale-1.0 live gate.

## Stop conditions

Stop and return the handover for owner review if the design requires:

- a new renderer, CGO, compositor, QML, C++, Rust, Lua, or Quickshell;
- a second metrics sampler or a shell-side GPU reader;
- a wildcard GPU history fallback;
- hiding temperature or GPU to make the current card fit;
- a 22px radial without measurements and explicit approval;
- a card-size change that affects other Control Centre pages without a named
  design decision;
- a mockup that cannot state its logical dimensions or density assumptions.

The next implementation step starts only after the owner supplies this design
package and completion handover.

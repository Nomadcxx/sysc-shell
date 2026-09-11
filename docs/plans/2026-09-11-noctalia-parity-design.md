# Noctalia v4 parity — Design

Date: 2026-09-11. Status lives in bd.

Approach owner-approved 2026-09-11 in brainstorming: visual parity with
Noctalia v4, near-indistinguishable, as the standing target for first-party
shell surfaces. The decisions below are pending owner review.

**This design supersedes `2026-09-02-theme-system-parity-design.md`.** That
document's mechanism is correct and survives intact; its *constants* were
derived from DMS and Noctalia observation rather than measurement, and every
numeric ladder in it differs from v4's. Re-basing each ladder inside the old
document would rewrite more of it than it left standing, so it is superseded
rather than amended, and the register records the supersession.

Behaviour and visual reference only. No QML, C++, or configuration is imported.
Noctalia remains a reference, not a compatibility contract (architecture
decision 8).

## Calibration

Parity is meaningless without knowing what unit v4's constants are in. The
reference screenshots were measured rather than eyeballed, on 2026-09-11.

| Measurement | Observed | v4 formula | Logical |
|---|---|---|---|
| Bar band, column x=700 | y 0–60, edge blend y=61 → 62 px | `toOdd(31)`, density `default` | 31 |
| Clock capsule, column x=3600 | y 6–55 → exactly 50 px | `toOdd(round(31 × 0.82))` = 25 | 25 |
| Panel title glyph extent | ascender→baseline ≈ 32 px, x-height ≈ 23 px | `fontSizeXL` = 16 pt | 21.3 |

Two independent geometric constants both resolve at exactly **2×**, so the
screenshots are a 3840×2160 panel presenting **1920×1080 logical** at device
pixel ratio 2, running bar density `default`. The title measurement then gives
em ≈ 42.7 device px ≈ 21.3 logical px, which for Roboto's ascender (0.75 em)
and x-height (0.528 em) independently agree, and equals 16 pt **at 96 DPI**.

Therefore: **v4's `Style.qml` constants are logical pixels, its font sizes are
points, and points convert to logical pixels by ×4/3.** Every number below is
stated in logical pixels.

The reference screenshots are the article author's desktop, not this project
owner's. The owner's own archived v4 configuration
(`~/noctalia-backup-20260825/config-noctalia/settings.json`) is used only for
evidence of which features were actually run — `enableBlurBehind: true`,
`panelBackgroundOpacity: 0.93`, `boxBorderEnabled: false`, `showOutline: false`
— never as a source of geometry.

Sources (read, not imported):

- `noctalia-dev/noctalia` at tag **v4.7.7**, `Commons/Style.qml` — the constant
  ladders; `Widgets/NBox.qml` — card container; `Widgets/NHeader.qml` — the
  label/description pattern; `Assets/ColorScheme/Catppuccin/Catppuccin.json` —
  the 16-role palette contract
- `docs/plans/2026-09-02-theme-system-parity-design.md` — superseded; D1, D3,
  D4, D6, D11, D13, D15 carried forward
- `docs/plans/2026-09-02-theme-system-parity.md` — its nine preserved
  invariants and its still-open live gate
- `docs/plans/2026-09-02-chrome-catalogue-design.md` — D4 surface hierarchy,
  D5 parity boundary, D7 state layers, D8 transitions, D9 reduced motion
- `docs/plans/2026-09-11-panel-backdrop-blur-design.md` — owns the opacity floor
- `internal/theme/profile.go`, `palettes.go`, `contrast.go`, `theme.go`
- `internal/shell/theme_test.go:252`, `:255` — the two locked 1.45:1 assertions
- bd `sysc-104` (open), `sysc-110`, `sysc-54`, `sysc-142` (closed, live gate
  outstanding)

## Goal and scope

In:

- Re-base spacing, radius, type, density, and motion onto v4.7.7's constants.
- A second radius ladder for interactive elements.
- Map v4's 16 colour roles onto the existing role set without deleting any.
- Resolve the nested-surface contrast floor.
- Carry forward the surviving decisions of the superseded design, explicitly.

Out:

- The blur backdrop and the opacity floor. Owned by
  `2026-09-11-panel-backdrop-blur-design.md`.
- Component construction. The chrome catalogue owns chrome (its D5); this
  design changes token *values*, not trees.
- Conformance enforcement. Separate design, next in the tranche.
- Image-backed cards, arc gauges, the audio spectrum. Later parity work with
  their own consumers.
- Any DMS, QML, Luau, or Noctalia configuration compatibility.

## Carried forward from the superseded design

These survive unchanged and are restated here so superseding loses nothing:

| Was | Carried |
|---|---|
| D1 | One resolved `shell.Theme` per surface. Resolution order: preset → axis overrides → bar/output overrides → accessibility → component invariants. A candidate publishes only when the complete result validates; an invalid reload retains the previous theme. |
| D3 | Configuration stays in the existing `theme` block. Sparse writer, preset rebase rule, `theme-gen` keeps palette source/seed/scheme/mode. |
| D4 | The **complete** Material palette contract and `Tokens.Valid`. Parity maps roles; it never removes one. Template consumers keep every name. |
| D6 | The role table **as amended 2026-09-03 in `b399944`**: bar capsules and panel cards share one container level, because bar and panels are one continuous surface. See D9 below — v4 independently confirms this. |
| D11 | Atomic live application; retarget animators from displayed values; no interpolation of protocol dimensions; open surfaces survive a valid reload. |
| D13 | Template catalogue exports palette plus mode and source metadata only — never density, geometry, or motion. |
| D15 | **The live Niri gate's ten configurations remain outstanding.** `sysc-142` is closed but its notes record the gate as unrun. Supersession does not discharge it; it is re-inherited here. |
| Plan | The nine preserved invariants: one Wayland-owner goroutine; one animator per surface with no frames after settlement; host paths retain their locks; invalid input retains the previous theme; auxiliary surfaces survive reloads; project and Material icon faces outrank system fallback; `theme-gen` owns palette generation; bar and output settings stay explicit overrides; components request semantic roles and never carry RGB. |

## Decisions

### D1 — Spacing re-bases onto v4's ladder

`theme.SpacingScale` becomes **1, 2, 4, 6, 9, 13, 18**, replacing 2, 4, 8, 12,
16, 24. The rungs are not a rescaling of ours: v4's ladder is denser in the
middle, which is what produces its tighter grouping inside cards.

Rejected: keeping our ladder and rounding v4's geometry onto it. Every padding
would land one or two pixels off, and the error compounds across nested
containers.

### D2 — Two radius ladders, independently scaled

v4 carries **container** radii (`radiusXXXS…radiusL` = 3, 4, 8, 12, 16, 20)
scaled by `radiusRatio`, and a parallel **input** ladder (`iRadius*`, the same
six values) scaled by `iRadiusRatio`. Sidebars, cards and panels take the
first; buttons, toggles and text fields take the second.

We have one `Radius` axis. It gains a sibling. A single axis cannot express
"rounded cards with square-ish inputs", which is a composition v4 ships and a
user can set.

Both remain bounded 0–32 as today. Stadiums and circles stay geometric
invariants at radius 0, per the superseded D8.

### D3 — Type re-bases onto the measured ladder

Points convert at ×4/3 (Calibration). The role table becomes:

| Role | v4 source | Logical px | Weight |
|---|---|---:|---:|
| Caption | `fontSizeXS` 9 pt | 12 | 400 |
| Body | `fontSizeM` 11 pt | 14.67 | 400 |
| Label | `fontSizeM` 11 pt | 14.67 | 500 |
| Title | `fontSizeL` 13 pt | 17.33 | 600 |
| Headline | `fontSizeXL` 16 pt | 21.33 | 600 |
| Display | `fontSizeXXL` 18 pt | 24 | 600 |
| Mono | `fontSizeS` 10 pt | 13.33 | 400 |

Sizes are stored as logical pixels rounded at paint against the resolved
fractional scale, not as floats in the node. `font-scale` continues to
multiply before output scaling.

`Display` is new — v4 uses `fontSizeXXL`/`XXXL` for the calendar date header
and similar hero treatments, which our ladder topped out below.

The visible consequence: **our titles are smaller and heavier than v4's.** Our
`Headline` is 20 px / 600 against v4's 21.33 px, and our `Title` 16 px / 600
against 17.33 px. v4's screen reads lighter and larger. This single axis
accounts for much of the tonal gap and is the most user-visible change here.

### D4 — Density becomes five rows with odd-forced heights

v4 bar heights (top or bottom position) are `mini` 21, `compact` 25, `default`
31, `comfortable` 37, `spacious` 47, each through `toOdd()`. Capsule height is
`toOdd(round(barHeight × r))` with r = 0.90, 0.85, 0.82, 0.75, 0.65.

Our three rows (40 / 48 / 56) are replaced by v4's five. **Ours are roughly
1.5× taller at every step**, which is the second large contributor to the
tonal gap.

`toOdd` is adopted deliberately, not copied blindly: an odd band has a true
centre row, so a centred glyph or capsule lands on a pixel instead of
straddling two. Our current even heights are why centring has needed
`pixelAlignCenter`-style compensation.

Capsule height becomes a ratio of bar height rather than an independent
constant, which is what keeps a capsule proportional across densities. The
existing `CapsulePadding` stays the inset *inside* the pill.

Panel and card padding move to v4's `panelPadding`/`cardPadding` of 14 each,
replacing our density-varying 12/16/20 and 10/12/16. v4 does not vary them by
density; the density difference lives in the bar and controls.

### D5 — Motion re-bases onto v4's durations

| Token | Ours | v4 | New |
|---|---:|---:|---:|
| Instant | 0 | — | 0 |
| Shorter | 80 | `animationFaster` 75 | 75 |
| Short | 120 | `animationFast` 150 | 150 |
| Medium | 180 | `animationNormal` 300 | 300 |
| Long | 250 | `animationSlow` 450 | 450 |
| Extra long | 400 | `animationSlowest` 750 | 750 |

**Ours are substantially faster at the long end** — 400 ms against 750 ms. v4
reads calmer for that reason.

Chrome catalogue D8's component recipes re-base onto these tokens rather than
being discarded: press in/out and hover move to `Shorter`/`Short`, segmented
selection to `Medium`, panel enter/exit to `Medium`/`Short`. The curves
(out-cubic, out-quart for expressive spatial) are unchanged, and reduced motion
behaviour (chrome D9) is unchanged.

`motion-speed` continues to divide, matching v4's `animationSpeed`.

### D6 — v4's sixteen roles map onto ours; none are deleted

| v4 | Ours |
|---|---|
| `mPrimary` / `mOnPrimary` | `Primary` / `OnPrimary` |
| `mSecondary` / `mOnSecondary` | `Secondary` / `OnSecondary` |
| `mTertiary` / `mOnTertiary` | `Tertiary` / `OnTertiary` |
| `mError` / `mOnError` | `Error` / `OnError` |
| `mSurface` / `mOnSurface` | `Surface` / `OnSurface` |
| `mSurfaceVariant` / `mOnSurfaceVariant` | **`SurfaceContainerHigh`** / `OnSurfaceVariant` |
| `mOutline` | `Outline` |
| `mShadow` | `Shadow` |
| `mHover` / `mOnHover` | *not adopted* — see below |

The `mSurfaceVariant` row is the one that matters and the one most easily got
wrong. v4 fills **both** bar capsules and panel cards with `mSurfaceVariant`;
`NBox` does so by default. That is a *role* match to our
`SurfaceContainerHigh`, not to our separate `SurfaceVariant` field, which keeps
its own meaning and is not involved.

This independently confirms the 2026-09-03 amendment (`b399944`): capsules and
cards belong on one level. That decision was made here against a live bar, and
v4 arrived at the same composition. It is now measured — the clock capsule in
the reference screenshot samples `#313244`, exactly `mSurfaceVariant`.

`mHover` is **not** adopted. Our state layers composite the paired foreground
over the current fill at 8/12/12/16 percent (chrome D7), which is
palette-independent and already approved; an explicit hover colour would
reintroduce a fixed RGB that every generated palette would have to satisfy.

Our remaining roles — the container ladder, `PrimaryContainer` and friends,
`SurfaceDim`/`SurfaceBright`, inverse roles, `Scrim`, `SurfaceTint`, and the
twelve fixed accents — are retained whether or not v4 names them, because the
superseded D4 and D13 require the complete set for template export.

### D7 — The nested-surface contrast floor drops to 1.30:1

Owner decision, 2026-09-11. Measured: v4's card-on-panel is `#313244` on
`#1E1E2E` = **1.30:1**. Our floor is 1.45:1, asserted twice in
`internal/shell/theme_test.go:252` and `:255`. Literal parity fails it.

The floor becomes **1.30:1** and this design records the resolution of open
issue `sysc-104`, whose own text says the choice is "owner taste on which
surface, not a missing paint path".

This is not a return to the defect. `sysc-104` and `sysc-110` were filed at
**1.17:1**; 1.30 sits clearly above that and is a published design the owner
ran daily. The two test assertions change to 1.30; the contrast *mechanism*
(`EnsureContrast`, `on`, `onWorst`, `ladderTop`) is untouched.

Text floors are **unchanged** at 4.5:1 normal and 7:1 high contrast. Measured
against v4's palette they pass with wide margin — body on panel 11.34:1, body
on card 8.69:1, variant on card 6.16:1, on-primary on primary 9.23:1. The
conflict was only ever about surface separation, never legibility.

High contrast continues to force full opacity and structural outlines, and is
exempt from this relaxation.

### D8 — The outline floor splits

v4's `mOutline` on panel measures **2.05:1**, below our 3:1. Rather than
relax one number, the role splits by function:

- `Outline` — focus rings, meaningful boundaries, and any control boundary
  carrying state — **keeps 3:1**. WCAG 2.1 SC 1.4.11 applies to user-interface
  components and focus indication, and relaxing it is an accessibility
  regression of a different kind from surface separation.
- `OutlineVariant` — decorative dividers and separators inside a card, which
  carry no state and whose absence loses no information — may sit below 3:1
  and adopt v4's value.

This is the one decision here I made rather than referred, because it is
reversible and internal. Flagged for owner override at review.

### D9 — The opacity floor is not this design's

`OpacityMin = 80` exists only because the shell cannot blur, and
`2026-09-11-panel-backdrop-blur-design.md` D8 owns lifting it. This design
neither raises nor lowers it, and takes a dependency on that slice for
parity on any translucent surface. The superseded D9's reasoning is otherwise
carried forward.

### D10 — Borders and shadows

v4 exposes `borderS/M/L` = 1, 2, 3 scaled by `uiScaleRatio`, with card and
button borders independently toggleable, plus shadows at opacity 0.85 and blur
cap 22.

Our `Elevation` axis and pure-Go shadow renderer are unchanged. Card borders
are **not** adopted as a default: the owner's archived configuration ran
`boxBorderEnabled: false` and `showOutline: false`, so borders are not part of
the look being matched. The token exists; nothing first-party consumes it.

`DrawShadow` currently has exactly one call site (`paint.go:483`, a menu list)
and never reaches cards. This design does not change that. If D7's 1.30:1 later
proves too soft in live use, card elevation is the escape hatch, and it is
unbuilt — recorded here so that is known in advance rather than discovered.

### D11 — The chrome catalogue boundary holds

Chrome catalogue D5 reserves component construction to the catalogue and
"broader type scales, density presets, and settings-driven composition" to
`sysc-142`, this design's predecessor. That boundary is unchanged: this design
alters token *values* and adds one radius axis and one type role. It does not
add, remove, or restructure a component tree. Trees change only where a
re-based constant changes their measured size.

### D12 — Migration

Existing configurations continue to load as the `standard` preset with current
values treated as explicit overrides, per the superseded D14. The ladder
re-base changes *defaults*, so a user who never set a value moves to v4's
number and a user who set one keeps theirs.

Density is the exception that needs care: our three names map onto v4's five.
`compact` → `compact`, `standard` → `default`, `comfortable` → `comfortable`,
with `mini` and `spacious` newly available. A configuration naming a density
keeps its name and changes height, which is the intended parity change and must
be stated in the plan's migration note rather than discovered at runtime.

### D13 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`**: a
repo-wide race build exhausts memory on this machine.

- Spacing, radius, type, density and motion tables match v4's published
  constants exactly, as table tests with the values inline.
- `toOdd` bar and capsule heights are odd at every density, and capsule is
  strictly less than bar.
- Point conversion: 16 pt resolves to 21.33 logical px, and the rounding rule
  is asserted at fractional scale.
- Contrast: nested surfaces clear 1.30:1; text clears 4.5:1; high contrast
  clears 7:1 and forces opacity 100; `Outline` clears 3:1 while
  `OutlineVariant` is exempt.
- Role mapping: every v4 role resolves, every one of our roles is still
  emitted by the template, and `Tokens.Valid` passes on the complete set.
- Preset rebase and sparse round trip still preserve overrides across the
  re-base.

### D14 — Tracker

Supersedes the design behind closed epic `sysc-142`; hangs off a new epic.
Records the resolution of open `sysc-104`, and unblocks `sysc-54`, which was
deliberately kept flat because nested surfaces were indistinguishable. Tasks
are created when the implementation plan is written. Status lives in bd.

### D15 — Open risks

1. **The live gate inherited from `sysc-142` has never run.** Ten
   configurations need a human confirming text does not clip, hit targets stay
   aligned, and cards separate. Re-basing every ladder makes that gate more
   necessary, not less, and it is now gating two designs.
2. Type and density both shrink relative to today. Every first-party tree was
   laid out against the current constants, so clipping and truncation are the
   expected failure mode, concentrated in the densest surfaces — the process
   table and the settings rows.
3. 1.30:1 is measured from one palette at one mode. A generated dark palette
   may land at 1.30 and read worse than Catppuccin does at 1.30, because
   separation is not purely a ratio. D10's card elevation is the fallback.
4. The reference screenshots are one author's configuration at 2× on 1920×1080
   logical. This project's primary output is 3440×1440 at scale 1.0, where no
   2× rounding is involved. Constants are stated in logical pixels precisely so
   this does not matter, but it has not been seen at scale 1.

## Files

| Path | Change |
|---|---|
| `internal/theme/profile.go` | `SpacingScale`, `typeRoles` + `RoleDisplay`, `metrics` five rows, `toOdd` helpers, `BaseMotion` |
| `internal/theme/profile.go` | second radius ladder and its axis |
| `internal/theme/palettes.go` | nested-surface floor 1.45 → 1.30; `Outline` / `OutlineVariant` split |
| `internal/theme/theme.go` | role mapping notes; no field removals |
| `internal/render/style.go` | input radius alongside `Radius` and `CardRadius` |
| `internal/shell/theme.go` | resolve the second ladder and the new type role |
| `internal/shell/theme_test.go` | the two locked assertions move to 1.30 |
| `internal/config/load.go`, `config.go` | `input-radius` axis; five density names |
| `internal/settings/registry.go` | density list, input radius row |
| `docs/plans/README.md` | supersession row |

## Stop

Every first-party surface resolves the re-based ladders. A side-by-side of the
Audio panel against `noctalia-audio_devices.png` differs in content and palette
but not in spacing, radii, type scale, or control geometry. Nested surfaces
clear 1.30:1 and text clears 4.5:1. The superseded design is marked as such in
the register, with its live gate re-inherited here rather than discharged.

Live Niri check, owner-deferred: the ten configurations from the superseded
D15, plus a scale-1.0 confirmation that the re-based type and density do not
clip the process table or the settings rows.

## Verified during design

- Noctalia v4.7.7 `Commons/Style.qml` read in full; constants quoted from it.
- Screenshot calibration: bar 62 device px and capsule 50 device px both
  resolve at exactly 2×; title em ≈ 42.7 device px confirms 16 pt at 96 DPI.
  Two geometric and two typographic metrics, independently agreeing.
- Clock capsule samples `#313244` = `mSurfaceVariant`, confirming capsules and
  cards share one level.
- Contrast computed with the WCAG 2.1 formula: v4 card-on-panel 1.30:1,
  outline-on-panel 2.05:1, body-on-panel 11.34:1, body-on-card 8.69:1,
  variant-on-card 6.16:1, on-primary 9.23:1.
- The 1.45:1 floor is asserted in exactly two places,
  `internal/shell/theme_test.go:252` and `:255`.
- `DrawShadow` has one call site, `internal/render/paint.go:483`.
- `sysc-104` is open and states the surface choice is owner taste.
- Our `theme.Tokens` carries 49 roles against v4's 16, counted from both the
  struct and the ordered `roles` slice the template, parser and validator walk.

Not verified: any of this at scale 1.0 on a 3440×1440 output; the inherited
live gate; whether a generated palette at 1.30:1 reads as well as an authored
one. See D15.

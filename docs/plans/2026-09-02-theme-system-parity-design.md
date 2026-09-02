# Theme System Parity Design

Date: 2026-09-02.

## Purpose

sysc-shell has a working Material palette generator and a small set of chrome
tokens. It still treats most geometry, typography, surface opacity, shadow,
and motion values as unrelated constants. The result changes colour, but it
does not behave as one theme when a user changes density, type, shape, motion,
or accessibility settings.

This design brings the native Go shell to the theme-system level shown by DMS
and Noctalia. It uses their semantic role families and composition patterns as
references. It does not import their QML, configuration formats, or runtime.

The chrome catalogue in `2026-09-02-chrome-catalogue-design.md` remains the
first consumer. This design replaces its provisional metrics and durations
with one shell-wide contract.

## Reference findings

DMS resolves a Material palette into surface levels, state-layer colours,
typography, spacing, icon sizes, corner radius, opacity, elevation, and motion
durations. Components consume the resolved roles. DMS also keeps accessibility
and animation-speed choices outside the colour palette.

Noctalia follows the same broad split in its native renderer. Its palette
exports the complete Material role family, while shell configuration controls
font family, radius scale, UI scale, borders, shadows, animation enablement,
and animation speed. Its contrast helper repairs a foreground against a known
background rather than trusting palette input.

sysc-shell already has the right ownership boundaries:

- `internal/config` validates and persists user choices;
- `internal/theme` generates palette data;
- `internal/shell.Theme` supplies immutable renderer-ready values;
- `internal/render` paints retained `internal/ui` nodes.

The implementation extends those boundaries. It does not introduce a theme
daemon or a second theme engine.

## Decisions

### D1. One resolved runtime theme

Every surface receives one complete `shell.Theme`. It contains the active
palette, type roles, density metrics, shapes, opacity, elevation, and motion
tokens. Renderers and components do not read configuration or call matugen.

Resolution follows this order:

1. apply the selected preset;
2. apply explicit axis overrides;
3. apply the existing bar and per-output overrides;
4. enforce accessibility constraints;
5. enforce component invariants such as circles and stadiums.

The registry publishes a candidate only after the complete result validates.
An invalid reload retains the previous resolved theme.

### D2. Presets provide defaults; axes remain independent

Ship three compiled presets:

| Preset | Intended composition |
|---|---|
| `standard` | Standard density, 12 px base radius, solid surfaces, standard motion |
| `compact` | Compact density, 8 px base radius, solid surfaces, shorter standard motion |
| `expressive` | Standard density, 16 px base radius, lightly translucent floating surfaces, expressive motion |

A preset supplies defaults for palette scheme and mode, density, typography,
shape, opacity, elevation, and motion. Wallpaper paths, seeds, and any other
explicit values override those defaults.

Changing a preset rebases a value only when that value still equals the old
preset default. This preserves a user's font, density, or opacity override.
The sparse writer compares the resolved configuration with the selected
preset, so it writes only deviations.

### D3. Configuration stays in the existing schema

Extend the existing `theme` block rather than adding an appearance service.
The existing `theme-gen` block continues to own palette source, seed, Material
scheme, and light/dark mode. Settings expose both blocks through the existing
`appearance.*` paths.

The composition fields are:

| Field | Values |
|---|---|
| `preset` | `standard`, `compact`, `expressive` |
| `density` | `compact`, `standard`, `comfortable` |
| `font-family` / `mono-font-family` | Font family strings |
| `font-scale` | 75 through 200 percent |
| `font-weight` | 100 through 900 |
| `radius` | 0 through 32 logical px |
| `motion` | `standard`, `expressive` |
| `motion-speed` | 25 through 400 percent |
| `bar-opacity` / `panel-opacity` / `overlay-opacity` | 80 through 100 percent |
| `elevation` | `none`, `subtle`, `standard` |

Existing bar height, padding, spacing, family, and size values remain local
overrides. The loader derives their defaults from the resolved composition
before it applies a base-bar or output override. Remove the ineffective
`bar.radius` setting; `appearance.radius` owns the global shape value, and the
wire format has never accepted a bar radius.

### D4. Complete Material palette contract

`theme.Tokens` carries the Material roles that DMS and Noctalia export:

- primary, secondary, and tertiary roles, containers, and paired foregrounds;
- error roles and their container pair;
- surface, surface dim/bright, the full container ladder, and paired surface
  foregrounds;
- outline, outline variant, inverse roles, shadow, scrim, and surface tint;
- fixed accent roles used by application templates.

The matugen template emits every field for dark and light variants. The
compiled fallback also defines every field. Template consumers receive the
same names even when the shell has no visual consumer for a fixed role.

`Tokens.Valid` parses every colour and checks each semantic foreground against
its paired background. Startup may use the compiled fallback when generation
fails. A reload reports the failure and retains the last valid generated
palette instead of replacing it with fallback colours.

### D5. Contrast and high-contrast behavior

Normal mode requires 4.5:1 for text and 3:1 for icons, focus rings, and
meaningful boundaries. Filled nested surfaces target 1.45:1 against their
parent. A 3:1 outline supplies the separation when the generated surface pair
cannot reach that floor.

High-contrast mode requests matugen contrast level 1 and then validates the
resolved result. A small pure-Go repair moves a failing foreground toward the
black or white endpoint that reaches the requested WCAG ratio with the least
change. High contrast requires 7:1 for text and 4.5:1 for meaningful non-text
content. It also forces bar, panel, and overlay opacity to 100 percent and
enables structural outlines.

The repair operates on paired foreground roles. It does not rewrite accent or
surface hues to manufacture surface separation.

### D6. Surface composition rules

Components request semantic roles. They do not carry RGB values or infer a
container level from their location.

| Layer | Fill and foreground |
|---|---|
| Bar or panel root | `Surface` / `OnSurface` with the surface's opacity |
| Bar capsule | `SurfaceContainer` / `OnSurface` |
| Panel card | `SurfaceContainerHigh` / `OnSurface` |
| Nested control or chip | `SurfaceContainerHighest` / `OnSurface` |
| Selected control | `Primary` / `OnPrimary` |
| Tonal selection | `PrimaryContainer` / `OnPrimaryContainer` |
| Destructive control | Error role or error-container pair |
| Modal shield | `Scrim` |
| Boundary | `Outline` or `OutlineVariant` |

Hover, focus, press, and drag composite the current foreground over the
current fill at 8, 12, 12, and 16 percent. Disabled content uses 38 percent
foreground emphasis and keeps its settled geometry.

This role table resolves `sysc-104` and `sysc-110`: bar capsules and panel
cards no longer share one container token.

### D7. Semantic typography and real font faces

Add a small text-role enum to `ui.Node`:

| Role | Standard size | Standard weight |
|---|---:|---:|
| Caption | 12 px | 400 |
| Label | 14 px | 500 |
| Body | 14 px | 400 |
| Title | 16 px | 600 |
| Headline | 20 px | 600 |
| Mono | 13 px | 400 |

`font-scale` multiplies these sizes before fractional output scaling. Layout
and paint resolve the same role, face, size, and weight. The installed
`go-text` font scanner selects the requested weight and italic face. Per-rune
fallback remains bounded and keeps project and Material icon faces ahead of
system fallback.

The default family is `Inter Variable` with `sans-serif` fallback. The mono
default is `Fira Code` with `monospace` fallback. The shell does not embed a
general text font in this tranche. A missing configured family records the
resolved fallback and keeps the theme usable.

Legacy fixed size, synthetic bold, and painter font fields remain during the
migration. Remove them after all first-party trees consume text roles.

### D8. Density, icon, and shape scales

Density selects a finite table. It does not multiply arbitrary component
geometry.

| Metric | Compact | Standard | Comfortable |
|---|---:|---:|---:|
| Bar height | 40 | 48 | 56 |
| Compact control | 32 | 32 | 36 |
| Standard control | 36 | 40 | 44 |
| Panel padding | 12 | 16 | 20 |
| Card padding | 10 | 12 | 16 |
| Small/normal/large icon | 16/18/24 | 16/20/24 | 18/22/28 |

The shared spacing scale is 2, 4, 8, 12, 16, and 24 logical px. Density maps
semantic gaps and padding to that scale. Components may retain a measured
fixed dimension such as the 420 px session panel when the design requires it.

Shape roles are small, medium, large, card, panel, stadium, and circle. The
base radius derives small, medium, and large values. Cards and panels use
bounded values from that scale. Stadiums and circles remain geometric
invariants when radius is zero.

### D9. Opacity and elevation

Bar, panel, and overlay opacity apply to root semantic surfaces. Nested fills
source-over their painted parent, so a card remains distinct on a translucent
panel. The accepted range is 80 through 100 percent because the shell has no
portable compositor blur behind text.

Elevation maps `none`, `subtle`, and `standard` to the existing pure-Go shadow
renderer. Shadow colour comes from the Material `Shadow` role. A visible
outline remains available when the chosen elevation is none or the palette
needs structural separation. No compositor-specific blur protocol enters this
work.

### D10. Motion tokens and recipes

The surface-local animator from `sysc-141` remains the only clock. The theme
supplies durations and curves:

| Token | Duration at 100% speed |
|---|---:|
| Instant | 0 ms |
| Shorter | 80 ms |
| Short | 120 ms |
| Medium | 180 ms |
| Long | 250 ms |
| Extra long | 400 ms |

Standard motion uses out-cubic. Expressive spatial changes use out-quart; it
does not add springs or overshoot. `motion-speed` divides durations by the
bounded speed factor.

Semantic recipes choose tokens for hover, press, selection, panel visibility,
theme crossfade, and layout movement. They continue to interpolate colour,
opacity, translation, scale, bounds, and corner radius. A theme cannot supply
an arbitrary curve or start another timer.

Reduced motion settles state, selection, layout, and theme changes at once.
Panel visibility may retain an opacity-only transition capped at 150 ms.
Focus remains immediate.

### D11. Atomic live application

The registry resolves palette and composition into one candidate under its
existing lock. Palette and opacity changes retarget each active animator from
its displayed value. Shape, density, and typography changes rebuild and
remeasure retained trees before publication.

Wayland surface extents change through one configure step. Stable child keys
may animate to new bounds after that configure. The implementation does not
interpolate protocol dimensions or mutate UI state off the Wayland owner.

Open panels, tray surfaces, toasts, OSD state, focus, and interaction state
survive a valid theme reload. A candidate error keeps all previous values and
reports through the existing settings/reload error path. Frame requests stop
after the last transition settles.

### D12. Settings and persistence

The settings registry exposes every axis under Appearance and keeps reduced
motion and high contrast under Accessibility. Selecting a preset applies the
rebase rule from D2. Changing another axis creates an explicit deviation from
that preset.

Writes stay atomic and sparse. Parsing continues to reject unknown fields and
names the failing path. A write/read round trip must preserve the selected
preset, overrides, derived bar defaults, and per-output overrides.

Settings preview uses the same candidate resolver as reload. It cannot show a
theme that the running shell would reject.

### D13. Application template catalogue

The existing template catalogue exports the complete validated palette plus
mode and source metadata. Templates do not receive shell density, geometry, or
motion values. Those values have no stable meaning in terminal, editor, GTK,
or Qt configuration.

Template apply remains serialized and reversible. A rejected shell palette
does not update external application files.

### D14. Compatibility and migration

Existing configuration files load as the `standard` preset with their current
theme generator, radius, bar geometry, and font values treated as explicit
overrides. The first sparse write may move the effective global radius to its
new Appearance setting, but it must preserve rendered geometry.

The implementation keeps temporary aliases only while migrating existing
surfaces. No compatibility layer targets DMS or Noctalia configuration, QML,
plugins, or custom theme files.

### D15. Verification and stop condition

Focused checks cover preset rebasing, sparse round trips, token completeness,
contrast repair, semantic composition, real font weight resolution, density
tables, shape invariants, opacity compositing, elevation, motion scaling,
reduced motion, and atomic reload.

The full automated gate remains `gofmt`, `go vet`, and race-enabled tests. The
live Niri gate exercises all first-party surface families at standard,
compact, expressive, light, dark, high-contrast, and reduced-motion settings.
This machine can qualify one output at scale 1.0. It cannot qualify the
two-output case.

Stop when every current first-party surface consumes the resolved theme,
settings apply all axes live, invalid candidates retain the previous theme,
and transitions settle without further frame requests. Theme scheduling,
runtime theme packs, compositor blur, and new shell components require later
designs.

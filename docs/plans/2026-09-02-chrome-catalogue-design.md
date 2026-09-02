# Chrome Catalogue Design

Date: 2026-09-02.

## Purpose

sysc-shell needs one native chrome language for buttons, pills, cards, and
small shell controls. The current `KindButton` is always a sharp Primary
rectangle, so idle, selected, destructive, hover, and pressed states are
indistinguishable. The session/power panel is the first layout consumer because
it exposes the problem most clearly, but the primitive correction is shared by
every existing `KindButton`.

This remains a native Go renderer. DMS and Noctalia are visual and behavioral
references, not runtime dependencies or compatibility targets. The catalogue
extends the retained `internal/ui` tree, the native painter, and the existing
theme pipeline; it does not create a general application toolkit.

## Catalogue

| Element | First shipped consumer | Resting shape and state |
|---|---|---|
| Bar item pill | `widget.go` capsules | Stadium, `SurfaceContainerHigh` |
| Grouped bar pill | CPU/memory group | Stadium around the actual shared hit target |
| Numbered workspace pill | Existing workspace strip | Idle container; active Primary |
| Standard button | Existing `KindButton` sites | 40 px stadium; idle high container or outline |
| Compact button | Calendar navigation and dense rows | 32 px; a square icon control becomes a circle |
| Full-width action | Session actions | 40 px stadium with icon and text |
| Segmented row | Power profiles | Equal stable segments and one moving selected stadium |
| Panel card | Battery, profile, and session cards | Rounded rectangle, never forced to a stadium |
| Nested chip | Values within a card | Small capsule using the highest container role |
| Focus ring | Every focusable node | Immediate 2 px rounded outline, separate from hover |

Meters, graphs, toggles, sliders, menus, fields, and virtual lists remain the
existing primitives. This work may restyle them through shared tokens but does
not replace them.

## Decisions

### D1. Native ownership and extension point

Go continues to own the UI tree, layout, input state, animation, painting, and
Wayland surfaces. Extend `KindButton` and `KindCapsule` in place. Add
`KindIcon`, because chrome icons need a dedicated embedded face and semantic
size. Add `KindSegmented`, because equal allocation, exclusive selection,
roving focus, and a single moving selection rectangle are parent-owned
behavior that cannot be expressed correctly by three unrelated buttons.

Do not add `KindCard`: a capsule with explicit rounded-rectangle radius and a
high-container fill already owns that job. Do not add a constructors package
unless repeated call sites prove a thin constructor removes real duplication.

### D2. Button content and geometry

`KindButton` becomes a container. Its current `Text` field remains shorthand
for a single text child so existing call sites continue to compile while they
inherit the corrected paint. A button may contain text, an icon, or an icon and
text. Icon-only buttons require `Name`, `Role`, a tooltip, and a focusable hit
target.

Standard controls are 40 logical px high; compact controls are 32 px. Text
buttons use 12–16 px horizontal content padding. Stadium radius is half the
short side. A square icon button is therefore a circle. Panel cards keep the
theme's rounded-rectangle radius instead of stadium clamping.

### D3. Resting variants and semantic fills

There are two idle variants, not a style matrix:

- container-filled: `SurfaceContainerHighest` with its paired foreground;
- outlined: transparent/current parent fill with `Outline` or
  `OutlineVariant` boundary and the parent's paired foreground.

Selected/on uses `Primary` with `OnPrimary`. Destructive actions use the Error
pair without changing geometry. Extend `ui.Fill` with only the roles needed by
this catalogue: retain `FillContainer` and `FillAccent`, add
`FillContainerHigh` and `FillOutline`, and resolve them through the active
semantic theme. Components request these variants; they never carry RGB.

### D4. Surface hierarchy and composition

The shell preserves these distinct Material roles:

| Role | Composition use |
|---|---|
| `Surface` | Bar and panel background |
| `SurfaceContainerHigh` | Bar capsules and panel cards |
| `SurfaceContainerHighest` | Idle controls and nested chips |
| `Primary` / `OnPrimary` | Selected and on state |
| `Outline` / `OutlineVariant` | Boundaries and focus support |
| `Error` / `OnError` | Destructive actions and failures |

The bar and the panels are one continuous background. `Panels.Gap` is 0 by
default and both surfaces fill with `Surface`, so a panel reads as an extension
of the bar rather than a separate slab; a panel is distinguished by its content
and by the user having opened it, not by a seam. Noctalia and DMS compose the
same way.

Capsules and cards therefore share `SurfaceContainerHigh`. An earlier revision
of this section gave capsules `SurfaceContainer` and cards
`SurfaceContainerHigh`, which put two container greys inches apart on one
uninterrupted surface -- the visual inconsistency that stratification was meant
to avoid. `SurfaceContainer` stays a parsed token for `sysc-142` parity but no
shipped chrome consumes it.

This still fixes the catalogue-owned part of `sysc-104`. That defect was cards
failing to read against the panel at 1.17:1, not cards sharing a level with
capsules; the fix is the level both now use clearing the 1.45:1 floor below.
`sysc-110` remains linked. The compiled fallback defines every consumed token.
Invalid generated themes keep the previous valid theme rather than partially
applying a palette.

Filled surfaces must separate from their parent by at least 1.45:1; if a
generated fill cannot meet that floor, paint the 3:1 outline as a structural
fallback. Text targets 4.5:1. Icons, focus rings, and meaningful boundaries
target 3:1.

### D5. Theme parity boundary

The catalogue extends the existing `internal/theme` tokens and
`internal/shell.Theme`; it does not add another theme engine. Theme roles cover
colour, density, control height, spacing, icon size, shape, typography, and
motion. Pills and circles remain semantic shapes even when the configurable
roundness changes.

This slice adds only roles its shipped chrome consumes. Full DMS/Noctalia
parity—including broader type scales, density presets, and settings-driven
composition—is separate issue `sysc-142`. New catalogue code must already
consume semantic roles so that later work changes theme data rather than
rewriting component trees.

### D6. Interaction state ownership

Add a compact interaction-state mask to `ui.Node` for hover, pressed, selected,
and disabled. Shell input handlers resolve pointer/focus state onto the tree;
the painter only draws the resolved state. Stable animation keys are required
for animated nodes and default to `Action` when it is unique on the surface.

Pointer motion invalidates only when the hit target changes. Press and release
invalidate when the pressed target changes. Keyboard focus remains independent
and immediately paints its rounded ring; hover never substitutes for focus.
Only clickable bar capsules animate.

### D7. State-layer colour recipe

State layers composite the current fill's paired foreground over the current
fill: hover at 8%, focus at 12%, and pressed at 12%. This works for light and
dark generated palettes without inventing fixed hover RGB values or turning an
idle control into Primary. Disabled treatment is lower emphasis using semantic
foreground opacity and must retain readable labels.

Power off and reboot are error-toned outlined/neutral buttons at rest. They do
not become permanent solid red blocks. Their hover and press layers use the
paired error foreground. This version adds no confirmation modal.

### D8. Native transitions

One surface-local transition mechanism interpolates colour, opacity,
translation, scale, rectangle bounds, and corner radius. Rectangle
interpolation is a pure-Go value operation. Each active surface owns one clock;
the existing panel reveal ticker is folded into it rather than adding parallel
timers. Reversals start from the current rendered value.

| Transition | Duration | Easing |
|---|---:|---|
| Press in / out | 80 / 120 ms | out-cubic |
| Hover | 120 ms | out-cubic |
| Segmented selection | 180 ms | out-quart |
| Panel enter | 200 ms | out-cubic |
| Panel exit | 150 ms | out-cubic |

Press scales the visual rectangle and its contents to 98%. Panel visibility
translates 8 logical px from its anchored edge while opacity changes. Segmented
selection moves and resizes one accent rectangle; segment widths remain stable,
foregrounds crossfade, and an optional selected check scales from 80% to 100%.
There is no ripple, bounce, glow, or overshoot.

Animation requests frames only while a value is unsettled and still respects
Wayland frame callbacks and buffer release. A target-state change invalidates
the surface; an unchanged target does nothing.

### D9. Reduced motion

Reduced motion snaps hover, press, and segmented selection to their settled
values. Panel visibility may use at most a 100–150 ms opacity-only transition;
translation and scale are removed. Focus is always immediate. The settled
colour and shape still paint, so reduced motion does not erase interaction
feedback.

### D10. Material Symbols asset contract

The owner explicitly approved Material assets, overriding the handover's
project-drawn SVG requirement. Use Material Symbols Rounded as an embedded,
static subset. Instantiate `FILL=1`, `wght=400`, `GRAD=0`, `opsz=24` at author
time. Runtime font variation and runtime SVG parsing are forbidden.

The source pin is Google `material-design-icons` commit
`84ccef280841abfac506afc4ad4a2782f6d0a1d0`, file
`variablefont/MaterialSymbolsRounded[FILL,GRAD,opsz,wght].ttf`, SHA-256
`c4416e02739ed6865e3218c19dcd62c5a88fb97b8bcc445f24ae8017d11cc2d0`.
The Apache-2.0 licence is committed beside the subset. The existing author-time
FontTools environment builds it; `go build` never invokes the builder. The full
14.6 MB variable font is not committed.

`KindIcon` uses this dedicated embedded face, never system fallback. A missing
approved icon name fails a focused test. Existing battery, weather, and metric
glyphs remain domain pictograms in `sysc-icons.ttf`; chrome uses Material
Symbols consistently.

The finite subset is:

| Ligature | Consumer | Size |
|---|---|---:|
| `lock`, `logout`, `bedtime`, `restart_alt`, `power_settings_new` | Session actions | 20 px |
| `speed`, `balance`, `energy_savings_leaf`, `check` | Power-profile segments | 18 px |
| `close`, `chevron_left`, `chevron_right` | Panel close and calendar navigation | 20 px |
| `search`, `settings`, `notifications`, `do_not_disturb_on` | Existing launcher/settings/notification chrome | 20 px |
| `volume_up`, `volume_off`, `brightness_high` | Existing OSD | 24 px |

`apps` and a launcher glyph are not included. A new ligature requires a shipped
consumer and a beads issue or an approved extension to this design.

### D11. Session panel composition

The 420 px session panel retains three cards: Battery, Power profile, and
Session. Battery remains a domain pictogram, percentage, meter, state,
remaining time, and rate; the percentage reserves enough width for `100%`.

Power profiles use one equal-width segmented row. Every available label,
including `Balanced`, must fit without truncation at 420 px. The active segment
uses Primary; idle segments use quiet container chrome; the selected check may
replace the profile icon. The entire row remains absent when
`powerprofilesctl` is unavailable.

Session actions are full-width 40 px icon-and-text stadiums. Lock remains absent
without a configured locker. Log out and Suspend use ordinary idle treatment;
Reboot and Power off use the error-toned outlined treatment from D7.

### D12. Inheritance and regression surface

The painter change is global. Settings navigation, launcher results,
notification actions, tray menus/drawer, bar overflow, and calendar navigation
inherit the new button geometry and idle paint automatically. This slice makes
an explicit padding or icon adjustment only where a layout or paint test proves
clipping. It does not hand-redesign those trees.

Existing right-click battery behavior, inert left-click, `power` IPC alias,
Super+X binding, session argv, focus order, Escape/outside close, optional-tool
gates, and Niri-first surface ownership remain unchanged.

### D13. Error behavior

Unknown icon names are programmer errors caught by tests and return no fallback
glyph at runtime. Invalid theme input retains the last valid theme and reports
the existing error path. Missing optional commands hide only their dependent
controls. A failed power-profile change keeps the panel open and displays its
existing error text; it does not commit a false selected state. Session command
failures remain observable through the existing error path. No lock screen,
gamer-mode engine, plugin protocol, or new Wayland protocol is introduced.

### D14. Verification and stop condition

Automated gates cover:

- token completeness, fallback completeness, and contrast helpers;
- button stadium/outline paint and default, selected, hover, pressed, disabled,
  focus, destructive, and reduced-motion states;
- colour, opacity, transform, bounds, and radius interpolation, including
  reversal and settlement;
- `KindIcon` inventory and dedicated-face rendering;
- equal segmented layout and `Balanced` fitting at 420 px;
- session tree variants and all existing `KindButton` construction sites;
- invalidation only on state changes and no frames after settlement.

The live Niri gate opens the panel by battery right-click, Super+X, and the
`power` alias; exercises hover, press, keyboard focus, profile change, Escape,
outside close, valid/invalid theme reload, and reduced motion; then checks
`niri msg -j layers` and captures a grim. The current machine has one DP-1
output, so a two-output check is recorded as unrunnable rather than claimed.

Stop when the shared primitives and session consumer pass these gates. Exclude
settings IA rewrites, control-centre tile grids, radial gauges, new panels,
plugin APIs, blur/shadows/EGL work, lock-screen implementation, gamer-mode
behavior, and full `sysc-142` theme parity.

## Data flow

Input handlers update the retained node state and animation targets under the
existing shell lock. A state change invalidates the owning surface. The
surface-local clock advances immutable transition values only while needed and
submits paint through the existing Wayland owner path. Layout resolves semantic
geometry and stable bounds; painting resolves semantic theme roles, state
layers, and icon faces. Theme reload swaps one validated semantic theme and
crossfades colours, or snaps when reduced motion is enabled.

No renderer state leaks into Niri wire types, and no Wayland event is handled
off the owner goroutine.
{"content_type":"terminal","tokens_before":3089,"tokens_after":3089,"token_count_basis":"o200k_base","ratio":0,"basis":"inferred"}

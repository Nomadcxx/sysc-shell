# Control centre dashboard correction design

Date: 2026-09-10. This document narrows the owner review of the first live
`PanelControlCenter` build. The governing design remains
`2026-09-03-control-center-design.md`; this document corrects the Home page,
the attached panel silhouette, and the unfinished interaction gate.

Tracker: `sysc-154`.

## Evidence

The live laptop capture used `eDP-1` at 1536×864 logical pixels and scale 1.25.
It showed four concrete defects:

- The identity card had no account image.
- Caffeine and Wallpaper used a joined segmented control, so their fills met at
  the centre.
- The System card printed CPU and memory as one text row instead of using the
  shell's system-resource gauges.
- The panel's expanded fillet margins painted as black vertical strips. The
  panel buffer contains transparent pixels in those margins, but `panelSpec`
  declares the full auxiliary surface opaque. `fillAttachFillets` also rounds
  its curve to whole-pixel scanline extents, which produces stair steps at
  fractional output scales.

Current `main` already carries the missing gauge work in `a965ca2` and
`8f4755f`: `KindRadialGauge`, antialiased gradient arcs, and project-owned CPU,
memory, and GPU glyphs built from SVG sources. The control-centre branch starts
at `8310a07`, before those commits.

No profile image exists on either test machine at `~/.face.icon`, `~/.face`, or
`/var/lib/AccountsService/icons/nomadx`. The dashboard therefore needs a stable
fallback as well as the image path.

## Scope

This pass covers the Home page and the panel chrome visible with Home open. It
also finishes the focus and reveal work already assigned to Task 12 of the
implementation plan.

The six other functional pages keep their current content and layout. Network,
Bluetooth, and Media remain disabled destinations owned by `sysc-157`,
`sysc-155`, and `sysc-156`. This pass adds no service backend, theme axis, UI
node kind, runtime SVG loader, or dependency.

## Decisions

### D1. Integrate current `main` first

Rebase `feature/control-center` onto current `main` before product changes. The
dashboard consumes the radial renderer and project icon font that `main`
already ships. It does not copy or fork that work.

The rebase also carries the 2026-09-10 wordmark placement amendment. A bar
gesture uses the wordmark action centre as `AnchorX`; IPC uses the focused
output centre because it has no pointer anchor.

### D2. Keep the Home geometry

Home remains 480 logical pixels tall:

| Block | Height |
|---|---:|
| Identity | 96 |
| Quick access | 48 |
| Clock, resources, and quick tiles | 184 |
| Volume and brightness | 116 |
| Three gaps | 36 |

The correction changes composition inside those blocks. It does not enlarge
the 700×564 panel or add Home scrolling.

### D3. Add an account image with a truthful fallback

The identity card places a 56px circular image well to the left of the existing
name, account, and uptime lines. The shell checks these sources in order:

1. `~/.face.icon`
2. `~/.face`
3. `/var/lib/AccountsService/icons/<username>`

The existing bounded `internal/icons` worker accepts absolute raster paths and
decodes off the Wayland owner. The control centre uses that path. It does not
read the root-only AccountsService account file or add a DBus client for one
image.

`KindImage` gains circular clipping through its existing semantic `Shape`
field. The painter applies the same antialiased mask used by other rounded
geometry. A missing, unreadable, oversized, or unsupported image leaves the
layout intact and paints a circular user-glyph fallback. No synthetic photo or
initials stand in for an account image.

### D4. Separate Caffeine and Wallpaper

The quick-access block keeps one 48px outer surface, but its two controls become
independent capsule buttons in a row with an 8px gap. Caffeine keeps selected
state. Wallpaper remains a launch action and never looks selected.

Each button retains its accessible name, focus target, and existing action.

### D5. Reuse the system-resource gauges

The 88px System card replaces the `CPU 21% / Memory 12%` text row with two
equal resource groups. Each group contains the existing gradient radial gauge,
its project-owned CPU or memory glyph, and a visible tabular value beside the
gauge. An unsampled value stays `—` and sets `Absent`; zero remains a valid
sample.

This pass keeps the current Home data contract at CPU and memory. GPU and
temperature would require more Home leases and a new layout decision, so they
remain in the bar and Monitor panel.

### D6. Fix the attached silhouette at its owners

`panelSpec` must not declare a fillet-expanded surface fully opaque. When a
panel has a non-zero fillet margin, it submits no opaque-region hint. The
compositor then blends the transparent side margins instead of treating zeroed
pixels as black.

This is an intentional small cost while the panel is open. A future measured
compositor cost can justify a body-aware opaque-region API. The implementation
marks that ceiling with a `ponytail:` comment.

The renderer replaces integer-only fillet extents with analytic per-pixel
coverage. The two bar-side curves receive partial alpha at their boundary at
1.0, 1.2, 1.25, and 1.5 scales. The far corners continue to use the shipped
rounded mask and rim. The joint curves sit at the base of the bar, which is the
top edge of a top-bar panel; the lower panel corners stay convex.

The fillet fill continues to use the bar's resolved root fill. Panel and bar
opacity can differ without leaving a colour seam.

### D7. Use restrained technical styling

The dashboard keeps the resolved theme's surface ladder, typography, spacing,
shape, and accessibility axes. Visual emphasis comes from three existing
mechanisms:

- value-driven accent-to-secondary gradients on radial gauges;
- clear elevation differences between the panel, cards, and controls;
- project icon glyphs compiled from SVG for CPU and memory.

Selected rail and toggle states keep the theme accent. Idle pills keep
container fills. The pass does not add gradients to every card, decorative SVG
art, new colours, blur, or shadows detached from the theme.

### D8. Finish the attached interaction gate

Task 12 remains part of this pass. The flat roving focus ring keeps rail entries
before page controls. Escape closes the panel. The page transition uses the
existing animator, with opacity and a small directional offset. The fillet
bulge follows the same reveal progress and settles at the theme radius.

Reduced motion removes the spatial offset and settles the page and fillets in
at most 150ms. A mid-transition rail action retargets the animator instead of
mounting a second page.

## Checks

Focused tests must prove:

- image-source precedence, bounded failure, fallback, and circular clipping;
- an 8px gap between the two quick-access controls;
- CPU and memory use `KindRadialGauge` and preserve unsampled dashes;
- a fillet-expanded panel disables the full-surface opaque hint;
- fillet edge pixels include partial coverage and no opaque black margin;
- wordmark placement, focus order, Escape, and reduced-motion settlement.

The package gate remains the plan's capped, stubbed command. The live gate runs
on the laptop at scale 1.25 and captures Home with `grim`. It checks the literal
wordmark gesture, visible account-image fallback, separated pills, readable
radials, smooth joint curves, and clean side transparency before the work moves
to other pages.

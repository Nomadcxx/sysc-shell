# Workspace Pill Art Commission

- Date: 2026-09-19
- Issue: sysc-410 (M10 workspaces: shape-only pills with urgency)
- Parent design: `2026-09-16-bar-weather-launcher-workspaces-design.md`, slice D3
- Status: issued for owner approval before the next paint change

## Problem

The first paint of D3 (commit ff34c97, live-verified on the laptop at eDP-1) renders the
workspace row as a toggle bar. The focused workspace is a 2×-wide rounded rectangle while
occupied and empty workspaces render as rounded squares at the bar capsule radius. A wide
"on" pill beside smaller "off" squares is the visual grammar of a switch row, not of
workspace paging.

The height fix (11aab8a) proved the pills can be glyph-height and vertically centred; the
remaining defect is shape language, not layout.

## Prior art

1. **Waybar `niri/workspaces`** (man 5, 2026-08-31): one visual element per workspace with
   the state model focused / active / urgent ("workspace has one or more urgent windows") /
   empty / current_output. Urgency maps directly to Niri's `is_urgent` payload field.
2. **Waybar `sway/workspaces`** (man 5, 2026-08-31): the same state model
   (focused/urgent/persistent/empty/visible); urgent means "flagged as urgent".
3. **i3/swaybar convention**: an urgent workspace receives `urgent_workspace_bg`,
   `urgent_workspace_border`, and `urgent_workspace_text` colour classes. Urgency is a
   colour treatment on the same shape, never a shape change.
4. **GNOME dash and macOS dock running indicators**: occupied = small filled dot, empty =
   hollow or absent; the current item is the larger or brighter element, not a different
   shape kind.
5. **iOS/Android page dots** (UIPageControl, Material carousel indicator): all indicators
   share one shape family — dots. The current page is an elongated pill of the *same
   height* at roughly 2× dot width; every other page stays a dot.
6. **Noctalia `workspaces_widget.cpp`** (behaviour and geometry reference ONLY; no code or
   compatibility surface may be copied): pill height is glyph-sized
   (`baseGlyphSize × contentScale × pillScale`), never bar height; the pill radius is the
   bar capsule radius resolved to a stadium; dot mode sizes dots at
   `max(4×contentScale, round(pillHeight × 0.28))`; empty fills sit at 55% alpha; the gap
   is `spaceXs`; state changes retarget animations without rebuilding; identity keys are
   stable (`id:`/`name:`/`coords:`/`index:`).

Consensus across all six sources: **one shape family per row**; state is expressed through
fill, brightness, and width *within* that family; urgency is a colour treatment; the
focused element is wider but never a different shape kind.

## Commissioned art direction

- **One shape family.** Every pill carries an explicit stadium shape
  (`ui.ShapeStadium`), overriding the inherited bar capsule radius. A dot
  (width == height) is therefore a circle; the focused element is a true pill.
- **Dot geometry.** Width == height == theme `IconLarge` (20 logical px at the default
  density). Occupied and empty dots share the geometry; only fill separates them.
- **Focused geometry.** Elongated stadium, height `IconLarge`, width `2×IconLarge`. This
  is the page-dot proportion from prior art (≈2× dot width, same height), not a switch.
- **Urgent.** `FillError` on its dot — or on the focused pill when a workspace is both,
  since focus dominates per the D3 invariant. Urgency stays a colour treatment.
- **Occupied.** `FillContainer`. **Empty.** `FillNone` (unfilled dot, still a hit target).
  Both unchanged from the first paint.
- **Gap.** `workspacePillGap` (8) unchanged — an existing theme/density role.
- **Fallback.** The pre-snapshot fallback (single label capsule) is unchanged.
- **No painted number** anywhere in the projected tree (unchanged D3 invariant).
- **Switching.** Each pill is a button: `Action` `workspace:<niri workspace id>`,
  `Name` `Workspace <index or name>`, `Role` `button` — the accessible identity the
  numeric pills already carried, now carried by shapes. Activation focuses that workspace
  through the platform Niri connection (the exact request path is an implementation
  detail for the plan). Scroll cycling is a follow-up.
- **Animation (follow-up, not this commission).** State changes may retarget the existing
  transition/animation channels (width and fill) without rebuilding the row, per the
  Noctalia diffing model. Record as discovered work; do not implement in the first pass.

## Acceptance criteria

1. Unit: `pillChrome`/`workspacePillsMatch` assert `ShapeStadium` on every pill;
   occupied/empty width == height == `m.IconLarge`; focused width == `2×m.IconLarge`;
   fill precedence focused → `FillAccent`, urgent → `FillError`, occupied →
   `FillContainer`, else `FillNone`; no children on any pill.
2. Live (laptop eDP-1): the bar shows a row of same-height shapes — one elongated pill,
   the rest dots; no square silhouettes; ordering and the fallback intact.
3. Switching: every pill carries `Action` `workspace:<id>`, `Name` `Workspace <index or
   name>`, `Role` `button`; activating a pill focuses that workspace (unit-tested
   dispatch; click-to-switch exercised on the live gate).
4. Gates: affected-package tests, build, vet, gofmt, diff check, and module diff per
   `AGENTS.md`.

## Out of scope

Labels, new theme tokens, configuration, the weather slice (D1), and the tray slice.
Scroll cycling is a follow-up.

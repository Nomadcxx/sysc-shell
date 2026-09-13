# Panel list row inset design

Date: 2026-09-14

## Scope

The Wi-Fi access-point row keeps its small rounded rectangle, but its leading
and trailing glyphs need breathing room from the surface edge. The current row
passes zero padding to a fixed-height button, so its content reaches both
edges.

## Decision

The AP button uses the existing density-aware `ButtonPadding` metric. The
fixed-height button layout applies that value horizontally while preserving
the row's height and vertical centring. At the laptop's standard density this
adds a 12 px inset on each side.

The row remains an explicit composition of existing `ui.Node` features:
`ShapeSmall`, `ButtonPadding`, a leading/body group, and a `PinEnd` trailing
group. The renderer and global button defaults stay unchanged.

## Future panel contract

Panel list rows with the same leading/body/trailing interaction model should
use the same shape, padding, height, and pinning rules. The shell should
extract a `panelListRow` constructor after a second real panel uses that
contract. The constructor can then own the shared action, focus, accessible
name, and edge-inset wiring. No new `ui.Kind`, renderer primitive, or global
button policy is needed.

## Preserved behaviour

The AP action, accessible name, focusability, state layers, labels, signal
glyph, lock/check glyphs, card shape, tabs, header well, and blur remain
unchanged. Other stadium-shaped controls keep their current geometry.

## Proof

The alignment test will assert that the signal icon starts at
`row.Bounds.X + m.ButtonPadding` and that the trailing icon ends at
`row.Bounds.X + row.Bounds.W - m.ButtonPadding`. Focused shell tests and the
repository gates cover regressions. The laptop visual check confirms the
inset at the active scale.

# Panel list row shape design

Date: 2026-09-13

## Scope

Wi-Fi access-point rows need a rectangular field with small rounded corners so
their leading and trailing glyphs sit inside the visible surface. This change
does not alter the shell-wide button shape, the network panel card, its tabs,
or the header icon well.

## Decision

Each access-point button sets `ShapeSmall`. The current theme resolves that
role to a 6 px corner radius, compared with the 28 px cap on a 56 px stadium.
The existing renderer and antialiased rounded mask paint the new geometry.

Panel list rows may opt into `ShapeSmall` when their design needs a broad
rectangular field around leading and trailing content. Components keep making
that choice at their construction site. A shared list-row component can wait
for a second consumer with matching layout and interaction requirements.

## Preserved behaviour

The row height, action, accessible name, focusability, state layers, text,
signal icon, and trailing lock or check remain unchanged. The surrounding
access-point card keeps `ShapeCard`, and other `KindButton` nodes keep the
stadium default.

## Proof

A focused tree test asserts that every access-point row requests `ShapeSmall`.
Existing renderer tests cover the shape-role radius and antialiased rounded
mask. The implementation gate runs the focused UI and shell tests, then the
repository checks. The laptop gate confirms that each glyph sits inside the
row surface at the current panel scale.

# Metrics Control Centre radial design — artifact index

Design package for the commission in
`docs/plans/2026-09-17-metrics-control-centre-radial-design-execution-handover.md`.

- **Source revision:** `4119ce5`
- **Designed:** 2026-09-17
- **Approved:** 2026-09-17 — option B, with the three amendments in `design.md` D2
- **Scale assumption:** logical pixels at scale 1.0, font scale 100% unless a
  figure names another. The live gate output is `DP-1`, 3440×1440, scale 1.0.

## Read in this order

| File | What it is |
| --- | --- |
| `design.md` | The decision, the amendments it requires, rejected alternatives, and the implementation seams. |
| `measurements.md` | Every figure behind the decision, and how it was measured. |
| `acceptance-checklist.md` | Checks an engineer can run without interpreting a mockup by eye. |

## Artifacts

| File | Maps to |
| --- | --- |
| `option-b-40px-row-recommended.svg` | **The approved composition**, dimensioned. `design.md` D1. |
| `recommended-preview.png` | Owner-facing raster of the above, rendered at 2× from the SVG. |
| `state-sheet.svg` | Normal, valid zero, three-digit maximum, all unavailable, GPU unavailable, GPU identity ambiguous, temperature warning, temperature critical. `design.md` D3. |
| `state-sheet-preview.png` | Owner-facing raster of the state sheet. |
| `option-a-40px-grid.svg` | Option A, four 40px rings two-by-two, with the card height it needs. Rejected. |
| `option-c-128px-card.svg` | Option C, the grown card, with what moves and the scroll consequence. Rejected. |
| `rejected-22px-current.svg` | What is on `main` today, with the measured overflow at each font scale. |
| `surfaces.svg` | The same four metrics across bar, Home and console, and the six shared principles. `design.md` D4. |
| `generate.py` | Emits every SVG above from the measured constants. |

An interactive canvas of the same material, with a 2× inspection view, is at
`https://claude.ai/artifact/WdB6z4aCsfn1hJ8trBJZsq`. The files in this
directory are the durable record; the canvas is a convenience.

## Toolchain

- Mockups: `python3 generate.py` (Python 3, standard library only). No network,
  no external packages.
- Raster: `rsvg-convert -z 2 <file>.svg -o <file>.png` (librsvg).
- Measurements: a temporary test in `package shell` that laid out the real
  `ccHome` with `ui.LayoutColumn` and measured through
  `render.NewSystemFontMap("Inter", …)`, the same path `PanelHost.measureText`
  uses. The harness was deleted after measuring and is not a project check; the
  method is written out in `measurements.md` so it can be rebuilt.

The SVGs are plain text and hold no embedded raster or font data. Fonts are
referenced by family name, so a viewer without IBM Plex or Inter will
substitute; the geometry is unaffected because no dimension depends on a
shaped advance.

## No new assets

The design adds no icon and no raster the shell must ship. It removes the
gauge icon glyph from the Home card's rings — the existing icon font is
unchanged and the bar widget keeps using it.

## What this package does not establish

GPU telemetry is not qualified by a mockup. It remains the metrics service
tests and the live Niri gate. `design.md` records the states the shell must
render; it does not claim the reader supplies them on any given machine.

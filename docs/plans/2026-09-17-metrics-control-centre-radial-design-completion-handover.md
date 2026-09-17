# Metrics Control Centre radial design — completion handover

Date: 2026-09-17. Source revision at design time: `4119ce5`.

Commissioned by
`docs/plans/2026-09-17-metrics-control-centre-radial-design-execution-handover.md`.
Governed by `docs/plans/2026-09-16-metrics-control-centre-gpu-design.md`.

The design commission is complete and the owner approved the recommendation on
2026-09-17. This is a snapshot; corrections belong in bd or the register, not
in this file.

## 1. Artifact paths

Package: `docs/plans/assets/2026-09-17-metrics-control-centre-radial-design/`

| File | Contents |
| --- | --- |
| `README.md` | Index, revision, toolchain, artifact-to-decision map. |
| `design.md` | The decision, its three amendments, rejected alternatives, cross-surface principles, implementation seams. |
| `measurements.md` | Every figure and the method that produced it. |
| `acceptance-checklist.md` | Checks runnable without interpreting a mockup by eye. |
| `option-b-40px-row-recommended.svg` | The approved composition, dimensioned. |
| `recommended-preview.png` | Owner-facing raster, 2× from the SVG. |
| `state-sheet.svg`, `state-sheet-preview.png` | Eight states at one geometry. |
| `option-a-40px-grid.svg` | Option A, rejected. |
| `option-c-128px-card.svg` | Option C, rejected. |
| `rejected-22px-current.svg` | What was on `main`, with measured overflow. |
| `surfaces.svg` | Bar, Home and console with the six shared principles. |
| `generate.py` | Reproduces every SVG from the measured constants. |

An interactive canvas of the same material is at
`https://claude.ai/artifact/WdB6z4aCsfn1hJ8trBJZsq`. The directory is the
durable record; the canvas is a convenience and is not a project dependency.

No product code was changed by this commission.

## 2. The recommendation and what supports it

Four 40px radial gauges in one row inside the unchanged 356 × 88 System card.
Each ring carries its own value as centred text; each has a caption beneath.
Order is unchanged: CPU, memory, CPU temperature, GPU.

Content height is 40 + 2 + 15 = **57 of the 70px interior**, and holds at every
supported font scale: 54, 57, 61, 65 against 70 at 75%, 100%, 125%, 150%. Four
slots of 77 with 9px gaps occupy 335 of 338.

It was selected because it is the only composition measured that holds a 40px
ring inside the existing card. The alternatives needed 83px (one row plus the
card title), 110px (two-by-two), or a card grown to 128px.

## 3. The 40px decision

The commission asked whether the recommended layout retains the 40px gauge or
presents evidence for a different size. It retains 40px, and the evidence
changed what 40px has to mean.

`paintRadialGauge` draws an 11px glyph and an 8px value at every diameter, and
paints the icon *or* the value, never both. A larger circle currently enlarges
only empty space. The "established 40px gauge" was the geometry of a
one-row-of-two composition carrying two metrics, not a legibility result.

So the diameter is kept **and** the interior is made to scale with it. Three
amendments were approved:

1. **The card loses its "System" title.** Its 22px line is exactly the room the
   ring needs: 83px of a 70px interior with it, 57px without. An accessible
   group name must replace the lost heading.
2. **The visible temperature caption is "Temp"**, accessible name
   `CPU temperature`. The full string measures 99px against a 77px slot and
   fails from the default font scale upward.
3. **`paintRadialGauge` derives its centred type from the box** rather than the
   literals 11 and 8 — 14px for a percentage, 13px for a three-character
   temperature, 12px at `100%` inside a 40px ring. A 22px ring is unchanged.

Amendment 3 is the only one touching the primitive. Amendments 1 and 2 are
call-site decisions.

## 4. Metric-to-slot mapping

| Slot | Selector | Ring value | Caption | Accessible name |
| --- | --- | --- | --- | --- |
| 1 | `{Source: SourceCPU}` | `NN%` | CPU | CPU usage |
| 2 | `{Source: SourceMemory}` | `NN%` | Memory | Memory usage |
| 3 | `{Source: SourceCPU, Subject: "temperature"}` | `NN°` | Temp | CPU temperature |
| 4 | `selectGPU(snap)` | `NN%` | GPU | GPU usage |

The temperature keeps the `Celsius/100` fraction contract and carries degrees,
never a percentage.

States, all at identical geometry:

- **Valid zero** — track only, `0%` / `0°C`. Never the dash.
- **Unavailable** — track only, `—`. No stale value, no fabricated zero.
- **GPU unavailable** — as above, name `GPU usage: unavailable`.
- **GPU identity ambiguous** — visually identical, name
  `GPU usage: device identity ambiguous`. The two are deliberately the same on
  screen: the shell has a usage it cannot attribute to a device, and drawing
  anything else would imply a device is being measured.

## 5. What the next engineer changes

Named by file and function. This commission edited none of them.

| File | Function | Change |
| --- | --- | --- |
| `internal/shell/controlcenter_pages.go` | `ccHome` | One row of four slots in place of two rows; drop `monitorCardTitle("System", 0)`; add an accessible group name to the card. |
| `internal/shell/controlcenter_pages.go` | `ccResourceGroup` | Column: ring above, caption below. Stop passing `Icon` so the value-text path runs for all four. `Name` carries the full identity. |
| `internal/shell/controlcenter_pages.go` | constants | `ccGaugeSize` 22 → 40; retire or re-derive `ccResourceRowH`; correct the comment reasoning from a 16px title. |
| `internal/render/radial.go` | `paintRadialGauge` | Derive centred glyph and value size from the box. |
| `internal/shell/controlcenter_test.go` | `TestControlCentreHomeSystemGaugesFitInsideCardBounds` | Replace the `(len(s)*8, 16)` stub with per-role heights, or the guard keeps passing on a broken layout. |

Leasing, selection and projection are unchanged from the parent design.
`selectGPU` stays the only chooser.

Tracked as `sysc-335`.

## 6. Checks run, and their results

Measurement harness: a temporary test in `package shell` laid out the real
`ccHome` with `ui.LayoutColumn` and measured through
`render.NewSystemFontMap("Inter", …)` — the path `PanelHost.measureText` uses.
Deleted after measuring; the method is written out in `measurements.md`.

| Check | Result |
| --- | --- |
| Real layout of `ccHome`, font scales 75–200 | Overflow measured; see below |
| Candidate composition budgets | Recorded in `measurements.md` |
| Contrast ratios, computed not estimated | Recorded below and in `design.md` |
| SVG well-formedness, all six files | Parse clean |
| Raster render via `rsvg-convert` | Both previews render; inspected |
| Working tree after the commission | No product code modified; harness removed |

**Measured overflow of the layout that was on `main`:** 4px at font scale 100%,
13px at 125%, 29px at 150%, and a layout error at 200%. At 150% the GPU value
box was squeezed to 18px and clipped.

Go gates were not run: this commission changed no Go code. They belong to the
implementation in `sysc-335`.

## 7. Defects found, and where they went

**The guard test cannot see the overflow it guards.**
`TestControlCentreHomeSystemGaugesFitInsideCardBounds` measures every string as
16px tall, and the comment above `ccResourceRowH` derives its fit from the same
wrong 16. The real title is 22px. Folded into `sysc-335`.

**The thermal warning colour is nearly invisible.** `radialWarningAmber`
applies `EnsureContrast(amber, track, 3)`, which walks `#ffb300` toward black
until it clears 3:1 against the track. The painted result is `#6c4b00`, a dark
brown that reads as the arc fading out. The error role above 85 °C receives no
such treatment and sits at 1.20:1 against the track. Filed as `sysc-337`.

**`ccMonitor` omits GPU and CPU temperature.** It builds graph cards for CPU,
memory and network only. Once Home shows four metrics, that section contradicts
the page one click away. The standalone console does carry GPU, so the gap is
`ccMonitor` alone. Filed as `sysc-336`.

## 8. Cross-surface decision

The console keeps its graph-card language. A ring is a glance and a graph is a
history; collapsing them would make the console worse. What is shared is the
reading contract, written as six principles in `design.md` D4 and drawn in
`surfaces.svg`: the metric names itself; the value sits with its own mark;
unavailable preserves geometry; a valid zero renders; one GPU identity
everywhere; interior type derives from its container.

The bar widget is the single sanctioned exception to the first principle —
glyph plus tooltip, because a label is not affordable in the band.

## 9. Observations and unresolved questions

**The retained-tree invalidation invariant already holds.** `applyLocked`
captures the node before `format` runs and compares through
`nodeVisualStateChanged`, which tests `Text`, `Value`, `Absent`, `Tone`,
`ValueText` and `Values`. A temperature or GPU change invalidates the bar even
though its `format` returns an empty string. The Control Centre rebuilds
unconditionally. Neither path may regress to a text-only comparison.

**The arc is distinguished from the track by hue, not luminance** — 1.44:1 for
accent against track. The approved composition is the first where this does not
matter, because the number lives inside the ring: the arc is reinforcement, the
value is the datum.

**The caption sits at 3.91:1 against the card.** That clears 3:1 but not 4.5:1.
It uses the existing `Subtle` role, which carries every caption on this
surface, so changing it is a theme-wide decision and not this card's.

**The Home page already fails to lay out at font scale 200%**, before this card
is reached. Every card height on the page is a fixed constant while text scales,
so this is systemic and pre-existing. The approved composition neither fixes nor
worsens it. Not filed — it belongs to a font-scale review, not to this card.

**Hardware-dependent, unresolved:** no GPU telemetry claim is made here. A
mockup is not evidence that the reader supplies a value. That remains the
metrics service tests and the live Niri `DP-1`, 3440×1440, scale-1.0 gate, and
is the implementation's obligation.

## 10. Rejected approaches

| Approach | Why rejected |
| --- | --- |
| The 22px two-row layout on `main` | Overflows the card at every font scale from 100% up; the temperature caption overflows horizontally as well. |
| Option A — 40px two-by-two in the existing card | Needs 110px of a 70px interior. |
| Option B1 — 40px row keeping the card title | Needs 83px of 70. The title and the ring cannot both have the space. |
| Option B4 — the same row at 36px | Fits with more slack, but abandons the established diameter for room that is not needed. Held as the fallback if amendment 3 is later rejected. |
| Option C — grow the System card to 128px | Viable and the only option keeping both the title and the label-beside-ring reading, but `ccPageH` goes 480 → 520 against a 480px viewport, so Home scrolls. A scrollbar on the at-a-glance surface is too high a price for a ring that still has an 11px glyph in a 40px circle. |
| Forcing the console into rings | A history is not a glance. Sharing the reading contract achieves the coherence; sharing the shape would not. |

## 11. State at handover

- Design package written and registered in `docs/plans/README.md`.
- `sysc-335` carries the approved implementation with its seams and acceptance
  checklist. `sysc-336` and `sysc-337` carry the discovered work.
- No product code modified.
- Implementation has not started. It begins from `sysc-335`.

One operational note for whoever commits next: `bd export` truncated
`.beads/issues.jsonl` from 303 issues to 11 during this session and omitted the
newly created ones. Recovered with
`sqlite3 .beads/beads.db "DELETE FROM export_hashes;"` followed by a fresh
export, then verified at 306 unique ids with nothing lost. Check the line count
before committing the JSONL.

# Bar weather, launcher, and workspaces design

Date: 2026-09-16. Parent commission: `sysc-309`.

## Goal

Align the three bar details that share widget chrome and the Niri projection:
the weather pill shows a compact condition/value row, the far-left launcher
uses the supplied production artwork, and workspaces use number-free shapes.

## Existing seams

- `internal/shell/weatherwidget.go` builds the weather icon/value row and
  tooltip. The service already supplies stale, failed, unit, day/night, and
  forecast data.
- `internal/shell/widget.go` still builds the launcher as the `ghost` glyph and
  paints workspace indices in `refreshWorkspacePills`.
- `internal/platform/niri/events.go` receives `is_urgent` in Niri fixtures but
  currently discards it. `projectOutputs` owns the per-output workspace
  projection.
- `internal/render/wordmark.go`, `internal/icons/worker.go`, and
  `ui.KindImage` provide the project-owned embedded-raster and immutable-image
  paths. `ui.ArrangeBar` and the bar's density metrics own pill placement.

## Decisions

### D1. Weather has one visible row

The standard bar weather widget contains a condition glyph and the formatted
temperature. The literal accessible name `Weather` remains on the actionable
row and in the tooltip title; it is not a painted text child. The existing
`ShowCondition` option remains an explicit opt-in for configurations that ask
for a condition word. The default requested composition remains icon plus
temperature.

The icon gets its existing density-sized box. The row gap and outer capsule
padding come from the current metrics row. The builder does not add a
weather-specific translation. The placeholder, failed-fetch error,
stale-age, unit, tooltip, click action, and `panel:weather` route remain
unchanged.

### D2. The supplied PNG is the launcher mark

The source artwork is:

`/home/nomadx/Pictures/sysc-aperture-c-nested-gates.png`

Its SHA-256 is
`02f3a6246c193b06701e8d99d7cfbcb5b57136db943d2a67aca8740891827d3f`, and its
source geometry is a transparent 1024×1024 PNG with alpha bounds
`768×768+128+128`.

The implementation copies those bytes into a project-owned render asset,
embeds them, and decodes them through the standard PNG path into immutable
`ui.Image` data at the bar's requested pixel size. The transparent margin and
alpha bounds remain part of the asset contract. A focused asset test checks
the hash, dimensions, alpha bounds, and non-empty visible pixels.

The launcher node becomes an image node at the existing bar mark size. It
keeps `panel:launcher`, name `Open launcher`, button role, far-left placement,
and existing hit testing. It does not use `ghost`, the SYSC wordmark, a
generated vector, or a screenshot-like substitute. The image box, not a bar
translation, owns its centre at native and fractional scale.

### D3. Workspace shapes carry state without numbers

`niri.Workspace` gains the `is_urgent` state already present in the wire
payload, and `workspacePill` carries it through `projectOutputs`. The row
keeps Niri index ordering, focus, occupancy, urgent state, and its existing
hit/accessibility identity.

The painter creates no numeric text node. A focused workspace gets the larger
flexible rounded element. Other available workspaces get smaller shape
elements. The active fill, urgent treatment, dimensions, and gap come from
the existing theme and density roles. Empty and occupied inactive workspaces
remain distinguishable through state, while focus remains the dominant state.

The fallback before the first Niri snapshot stays accessible and stable. The
projection never hides a number inside a smaller node: the workspace tree
contains shapes only.

## Data flow

```text
Open-Meteo Reading -> weather widget -> icon/value row + tooltip
PNG bytes -> embedded render asset -> bar launcher KindImage -> PanelLauncher
Niri workspace JSON -> niri.Workspace -> outputState.Pills -> shape-only row
```

The Wayland owner measures and paints the retained tree. Weather and image
decoding stay outside `Registry.mu`; state publication invalidates the bar
once.

## Focused proof

The executable plan will cover:

- `internal/shell/weatherwidget_test.go`: default tree has no visible literal
  `Weather` label, icon and temperature share one row, and density-derived
  insets remain stable across stale/error/placeholder states.
- `internal/render` asset tests: embedded launcher bytes, alpha bounds, and
  requested-size raster output.
- `internal/shell/widget_test.go`: launcher is `KindImage` with the supplied
  mark and unchanged action/name; workspace trees contain no numeric text,
  the focused shape is larger, hit targets stay present, and urgent state
  survives refresh.
- `internal/platform/niri/events_test.go` and `projection_test.go`: urgent
  workspace wire state reaches the output projection without changing Niri
  ordering.

## Live gate

Run the exact build on Niri `DP-1`, 3440×1440, scale 1.0. Capture the bar,
exercise the launcher, switch focus and occupancy, and observe an urgent
workspace fixture where Niri can produce one. Record the native result. A
second output and laptop result are unavailable for this tranche.

## Boundary

This design does not add launcher search behaviour, copy Noctalia code or
configuration, replace the weather service, or create a general shape
toolkit. Existing weather ownership remains with `sysc-277`/`sysc-293`,
launcher ownership remains with `sysc-82`, and the workspace projection stays
in the shell/Niri seam.

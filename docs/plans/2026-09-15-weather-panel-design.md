# Weather panel design

Date: 2026-09-15. Executes the audit filed as `sysc-277`.

Target: the DMS/Noctalia v4 weather reference captured in
[assets/2026-08-31-bar-visual-parity/refs/noctalia-weather.png](assets/2026-08-31-bar-visual-parity/refs/noctalia-weather.png)
— a hero card, a details card, a segmented Daily/Hourly control over a forecast
list — composed from the shell's own chrome vocabulary. The audit found three
thin presentations (bar text run, control-centre page, nothing else), a wire
model that requests too little and discards what the API returns, and three
divergent copies of the weather vocabulary.

## D1 — Scope

One slice on `feature/weather-panel`, branched from `main` at `41eaf68`:

1. enrich the wire model (`weather/`, `internal/services`);
2. one weather vocabulary (glyphs, names, condition words) in `internal/render`;
3. upgrade the bar widget: icon node, panel action, structured tooltip;
4. a standalone `PanelWeather`;
5. the control-centre page restyled onto the shared vocabulary with the
   audit's stale/error treatment.

The plugin (`plugins/reference/weather`) is a protocol consumer and keeps its
own presentation; only its underlying `IconName` mapping is shared today and
that does not change.

## D2 — Wire model

`weather.Query` requests, and `Decode` fills:

- current: `temperature_2m`, `weather_code` (as today) plus
  `apparent_temperature`, `is_day`, `relative_humidity_2m`,
  `wind_speed_10m`, `wind_direction_10m`, `uv_index`;
- daily: as today plus `uv_index_max`, `precipitation_probability_max`,
  `precipitation_sum`;
- response root: `elevation`, `timezone`, `timezone_abbreviation` — returned
  by every response and discarded today.

New optional values are pointers in the model (`*float64`, `*bool`); nil means
the field was absent and the UI renders a dash. `Decode` stays strict: every
field is a checked pointer, the 64 KiB body cap holds, and a malformed field is
a decode error, not a silent zero. `services.Reading` gains the same fields as
plain aliases of the wire types, as it already does for `Day` and `Unit`.

## D3 — Units and formatting

Temperature follows the configured unit as today; the shell never converts.
Wind is km/h with an eight-point compass (N, NE, E, SE, S, SW, W, NW) derived
from degrees. UV renders to one decimal, humidity and precipitation
probability as percentages, precipitation sum in mm. No wind unit option: the
reference shows km/h, and a unit axis is deferred until someone asks.

## D4 — Icons, including night

The custom weather icon font (`internal/render/icons`, SVG sources to
`sysc-icons.ttf` via `build.py`) stays the only weather glyph source. Two
glyphs join it: `clear-night` (uniE028, crescent moon) and
`partly-cloudy-night` (uniE029, moon behind cloud). The committed TTF is
regenerated with the existing authoring-time builder; fontTools is present on
this machine and the artefact is committed.

`render.IconRune(code)` keeps its signature for existing callers. New
`render.WeatherIcon(code int, isDay bool) rune` and
`render.WeatherIconName(code int, isDay bool) string` select the night
variants for codes 0 and 1–2 when `is_day` is false; every other category
reads the same by night, as the reference renders it.

## D5 — One vocabulary

`render.WeatherCondition(code int) string` becomes the single condition-word
table (Clear, Partly cloudy, Cloudy, Fog, Rain, Snow, Heavy snow,
Thunderstorm). `weatherwidget.go`'s `conditionWords` map and the control
centre's `ccWeatherCondition` are deleted onto it, and `ccWeatherIcon` — the
second, divergent Material-ligature mapping — is deleted onto
`WeatherIconName`. After this slice one code maps to one glyph and one word
everywhere in the shell.

## D6 — Bar widget

The `weather` item renders a row — weather glyph icon node beside the
temperature text, the condition word still gated on `show-condition` — instead
of the glyph inline in the text run, so the glyph stops inheriting text tone
and sysc-58's advance defect. Left click carries `panel:weather`, toggling
`PanelWeather` anchored to the widget. The tooltip becomes a structured tree
rebuilt with each reading through the existing `textWidget.tip` refresh path:
condition line, today's low/high, wind, updated time, and the age when stale —
never the literal word "Weather".

## D7 — Panel identity and trigger

`PanelWeather` appends to the `PanelID` enum after `PanelBluetooth`, with
`String()` `"weather"` and `panelTargetSize` 460×560, the network and
Bluetooth sibling size. `registry.go` routes `panel:weather` like the other
panel actions, with `AnchorX` from the triggering widget. A weather update
rebuilds an open panel, as the control centre already does. The blur coverage
walk needs no edit; it fails until the new enum member routes through
`panelSpec` like every panel.

## D8 — Panel composition

One column inside 460×560, margins from the ladder, controls from the density
row, no literal geometry:

- header: "Weather" and the panel close button, as the other panels;
- hero card: large glyph, temperature at title role with unit suffix, today's
  low/high pair, condition word, location line, "Updated HH:MM";
- details card: labelled rows — feels like, wind, humidity, UV index,
  precipitation chance, sunrise, sunset, elevation, timezone — icon and label
  left, tabular value right; a field whose datum is absent renders a dash;
- forecast list: a `KindScroll` of seven day rows — day name, glyph, low/high,
  condition word — including today, marked as such.

The location line is `config.Weather.Location` when set; coordinates formatted
as the control centre formats them remain the fallback and the request source.

## D9 — Stale and error states

`formatWeather`'s three-state rule extends to every surface: nothing fetched
yet renders the placeholder in the normal tone; a fetch that never succeeded
renders the error tone with "weather unavailable"; a reading whose fetch
began failing keeps its value everywhere and shows its age. The control-centre
page, which today ignores `FailedSince`, gains the same treatment.

## D10 — Location label

`config.Weather` gains `Location string`: an optional plain-text label (limit
80 bytes) shown in the hero and tooltip. Wire key `weather.location`;
`write.go` round-trips it. Coordinates stay the request source regardless.

## D11 — Hourly

Deferred. Hourly needs its own request shape and a second presentation; the
daily list plus current conditions covers the useful case. The panel is not
shaped to preclude it: the forecast list is one scroll child that an
Hourly/Hourly-toggle slice can replace.

## D12 — Gates

The token-conformance scan stays green — no new literal geometry. gofmt, go
vet, and the affected-package checks run per task; the full-tree `-race` gate
is unrunnable here, so the per-package substitute applies. The live Niri gate
(owner-deferrable per roadmap): panel paints, opens and closes, focus
restores, `niri msg -j layers` shows no leaked surface, recorded rather than
claimed.

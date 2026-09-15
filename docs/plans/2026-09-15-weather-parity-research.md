# Weather parity research — Noctalia v5 and DMS measured

Date: 2026-09-15. Assessment for `sysc-277`'s parity gap slices. Commissioned by the
approved gap plan; supersedes the screenshot as the reference for weather composition.
**Owner decision recorded: the weather reference is Noctalia v5 and DMS** — the
in-tree screenshot's panel is v5's control-centre weather tab (its detail rows match
`weather_tab.cpp` exactly), and the owner approved measuring v5 and DMS directly.

Sources (read, never imported):

- **Noctalia v5**: `/home/nomadx/noctalia` at `5.0.0.r4969` (installed package matches
  `~/.local/share/noctalia-backups/v4-to-v5-20260730-lQdQ4p`). Weather: `src/system/weather_service.{h,cpp}`,
  `src/shell/control_center/tabs/weather_tab.{h,cpp}`, `src/system/location_service.cpp`, `src/ui/style.h`.
- **DMS**: `/home/nomadx/Documents/GitHub/DankMaterialShell` at `892b8ae` — verified the same
  revision the laptop runs (`dms v1.6.1`, checkout on the laptop identical). Weather:
  `Modules/DankDash/WeatherTab.qml`, `Modules/DankBar/Widgets/Weather.qml`, `Services/WeatherService.qml`.
- v4.7.7 was re-cloned (shallow, tag `v4.7.7`) and holds the global ladder grammar the parity
  designs already recorded; its `WeatherCard.qml` carries no Daily/Hourly detail rows, so it is
  **not** the reference panel for this surface.

## Data model (v5 `weather_service.h:30-96`)

- Current: temperature, wind speed km/h, wind direction deg, is_day, WMO code, UV index,
  relative humidity.
- Hour: time, code, temperature, humidity %, precipitation probability %, is_day, wind speed.
- Day: date, code, max, min, sunrise, sunset.
- Snapshot: location name, timezone, timezone abbreviation, elevation, fetched-at.

Request shape (`weather_service.cpp:500-508`): Open-Meteo, `current=temperature_2m,
wind_speed_10m,wind_direction_10m,weather_code,is_day,uv_index,relative_humidity_2m`;
`hourly=temperature_2m,relative_humidity_2m,precipitation_probability,weather_code,is_day,
wind_speed_10m`; `daily=temperature_2m_max,temperature_2m_min,weather_code,sunrise,sunset`;
**`forecast_days=7`, `forecast_hours=168`** (7×24, `weather_service.cpp:25-26`); `timezone=auto`.

Our wire model already carries every field except hourly. Wind unit is km/h with an
imperial-only conversion at display (`weather_tab.cpp:1095`); the shell stays km/h (D3).

## Answers the gap plan owed

**Hourly (gap 1).** The reference's hourly view is a **row list, not a horizontal strip**
(`weather_tab.cpp:1092-1130`): seven rows (same `kForecastRowCount = 7`, `weather_tab.h:52`),
each `glyph → hour label %H:%M → temp+unit → "condition, precip%"` summary, with a tooltip
carrying condition, rain %, humidity %, wind. Switching Daily/Hourly is a segmented control
(`weather_tab.cpp:288-299`) with a slide-direction transition (`m_forecastSlideDirection`,
`weather_tab.cpp:445`) — an animation nicety, not a layout constraint. So the shell needs no
horizontal scroll primitive: the existing vertical `KindScroll` list serves both views.

**Daily list excludes today** (`weather_tab.cpp:1016-1020`): when `forecastDays[0]` is today it
is skipped — the hero already covers today — and the list shows the next seven rows:
`glyph → weekday label → "High / Low" → condition`, tooltip condition/high/low/sunrise/sunset.
Note the order: **High / Low**, opposite of the shell's current "Low … High" line. Our current
day list includes today marked "Today" — that changes in Slice C.

**Hero glyph scale (gap 3).** `kCurrentGlyphSize = Style::controlHeightLg × 2.2`
(`weather_tab.cpp:29`), i.e. **44 × 2.2 ≈ 97 px** at default scale. DMS's hero icon is
`Theme.iconSize × 1.5` with a drop shadow (`WeatherTab.qml:88-91`). The shell's `IconLarge`
(20–24 px) is roughly a quarter of the reference hero. Slice C adds an `IconHero` density rung
sized from these ratios (v5: 2.2 × `StandardControl`-equivalent; DMS: 1.5 × icon size) rather
than a local literal.

**Glyph colour (gap 6) — single tone, role-selected, day/night aware.**
`weather_tab.cpp:885-886`: current glyph `ColorRole::Primary` by day, `Secondary` by night;
daily glyphs always Primary (`:1056-1057`); hourly glyphs Primary day / Secondary night
(`:1109-1110`). No duotone anywhere — the multi-colour look in the screenshot is the accent
tone on a single-colour glyph. The shell can do this today with `ui.ToneAccent` (day) and
`ToneNormal` (night); **no renderer capability work is needed**. DMS likewise paints
`color: Theme.primary` (`WeatherTab.qml:90`).

**Value colour (gap 4).** The reference's coloured min/max and values come from the same
Primary role on value text. Slice C applies the shell's accent tone to the hero range, detail
values and hourly temps — measured against the role usage above, not invented.

**Detail rows (the screenshot's card).** v5 (`weather_tab.cpp:256-263`): Temperature Max,
Temperature Min, Wind, Sunrise, Sunset, Elevation, UV, Timezone — row glyph
`(fontSizeBody + spaceXs)` ≈ **18 px** (`:230`), i.e. the shell's `IconSmall`/`IconNormal`
band. The shell's card already carries all of these except that it splits min/max into one
Low/High line — Slice C splits them to match, ordered max-then-min as the reference does.

**Location naming (gap 2).** v5 resolves a display name through a configured address → its own
backend (`api.noctalia.dev/geocode?city=…`, `location_service.cpp:172-176`, plus IP geolocate
`:164`) — a vendor service the shell will not depend on. Substitution already approved by the
owner: **Open-Meteo's geocoding API** (same provider as the forecast) with the same shape:
resolve once per config, cache, degrade to coordinates on failure. DMS v1.6.1 has no hourly and
is the weaker reference here.

**Condition words.** v5 renders short descriptions per WMO code via
`WeatherService::shortDescriptionForCode`; the shell's eight-word `render.WeatherCondition`
covers the same categories and stays.

## Where v5 and DMS disagree

- Hourly: v5 has a full hourly view; DMS v1.6.1 does not (`forecast_days=7` only,
  `WeatherService.qml:190`). Follow v5.
- Today in the daily list: v5 excludes it; DMS includes today in its forecast strip. Follow v5
  (the hero owns today), and keep the cc page's existing four-slot strip (shipped contract)
  untouched.
- Hero glyph: v5 ≈ 97 px vs DMS ≈ 1.5 × iconSize (36–42 px). Take v5's proportion; the density
  rung lands between so the laptop's 1.25 scale stays comfortable.

## What Slice C sizes from this

`IconHero` ≈ 2.2 × `StandardControl` per density row; detail rows keep `IconSmall` glyphs;
daily rows reorder to High / Low and drop the Today marker; hourly rows mirror the daily row
grammar with hour labels; glyphs and headline values take the accent tone by day and the normal
tone by night; the panel's segmented control gets two views of one list.

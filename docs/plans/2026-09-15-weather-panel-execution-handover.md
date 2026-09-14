# Weather panel execution handover

Date: 2026-09-15. Commissions the audit and the live gate for the weather slice
(`sysc-277`), executed this session on `feature/weather-panel`. Status lives in
bd; this document records what landed, what was checked, and what the next
session owes.

## Receiving state

Branch `feature/weather-panel` in
`/home/nomadx/sysc-shell/.worktrees/feature/weather-panel`, based on `main` at
`41eaf68`, **ten commits, unmerged and unpushed**, tree clean:

| Commit | Contents |
|---|---|
| `165f955` | `weather/` wire model: the request carries apparent temperature, is_day, humidity, wind speed and direction, UV index; daily UV max and precipitation; the response root's elevation, timezone and abbreviation decode instead of being discarded. Every new field is a checked pointer — nil means absent, never a silent zero. Optional daily arrays pad the required six rather than truncating them. |
| `e7c8f78` | `services.Reading` mirrors the new fields end to end. |
| `0709bc9` | Two night glyphs (`clear-night` uniE028, `partly-cloudy-night` uniE029) and six detail glyphs (thermometer, wind, humidity, sunrise, sunset, elevation, uniE02A–E02F) in the custom weather font via the authoring builder; `render.WeatherIcon(code, isDay)`, `WeatherIconName` and `WeatherCondition` become the only WMO mapping and word table; `paintIcon` falls back to the project catalogue for names the Material subset lacks; `conditionWords`, `ccWeatherIcon` and `ccWeatherCondition` are deleted. |
| `02f656c` | `config.Weather.Location` (wire key `weather.location`): optional display label, ≤80 bytes, no control characters, round-tripped by the writer. |
| `9d12150` | The bar weather item: a `KindIcon` weather glyph beside the temperature text, the `panel:weather` action on the row, and a structured tooltip rebuilt on refresh through `textWidget.tip` (condition, today's range, wind, humidity, updated time, age when stale). `formatWeather` became `weatherText` (the glyph left the text run). |
| `d9c5c64` | `PanelWeather` identity: enum after `PanelBluetooth`, `String()` "weather", `panelTargetSize` 460×560, `parsePanelName`/`panelIDFromAux` answers, a weather lease on spawn (an unconfigured block renders its placeholder via `errLabel`), `UpdateWeather` rebuilds an open panel, and the registry routes `panel:weather` with the trigger's anchor. The blur coverage walk widened to the new enum end, per its own instruction. |
| `8ce9a9b` | The panel body (`weatherpanel.go`): header with close, hero card, details card; `weather-close` joins the shared panel close path. |
| `95e1b9a` | The day list: one scroll, today first and marked, per-day glyph, range and condition word; fewer days render what exists; no body renders the placeholder. |
| `5f992c7` | The control-centre page: night glyphs, the day range on the Today card, the age on a stale reading, and the failure named instead of dashes. |
| `bd760f2` | gofmt on the render test additions. |

Design and plan are on `main` at `8565b7b`:
[2026-09-15-weather-panel-design.md](2026-09-15-weather-panel-design.md) (D1–D12)
and [2026-09-15-weather-panel.md](2026-09-15-weather-panel.md) — read the
design before reviewing; the plan's task boundaries match the commit list.

## Three amendments the execution forced

Recorded on `sysc-277` and worth re-checking in review:

1. **The `panel:weather` route landed with the enum**, not in the bar-widget
   task as the plan said: the route cannot compile without `PanelWeather`.
2. **The panel body is two-column** — hero and details share the left column,
   the day list scrolls on the right — because a nine-row details card plus
   seven day rows do not fit one 460×560 column. D8's "one column" line is
   amended; the reference is itself two-column.
3. **Six detail glyphs joined the font beyond the two night glyphs D4 named.**
   The details card's rows wanted icons the Material subset does not carry;
   they came through the same SVG-to-font pipeline.

## Gates run this session

- `go test` green for `./weather/ ./internal/services/ ./internal/render/
  ./internal/config/ ./internal/shell/ ./internal/plugin/ ./tests/...`
  (per-package runs; the full-tree `-race` gate stays unrunnable here).
- `go vet ./...` clean; `gofmt -l` clean; `go.mod`/`go.sum` untouched.
- `TestSurfaceSourcesCarryNoLegacyVisuals` green — the slice adds no literal
  geometry; heights derive from the density row and the margin ladder.
- `TestEveryPanelRequestsABackdrop` walks through `PanelWeather` green.
- A deploy candidate builds: `go build ./cmd/sysc-shell` at `bd760f2`
  (provenance line `v0.0.0-20260914182611-bd760f24d234` or later — check
  `go version -m` against the worktree HEAD before trusting any live
  observation; the 2026-09-13 handover's foreign-binary hazard still applies).

## Commission: audit first, then live

Two sessions, in this order, sharing `sysc-277`:

### 1. Audit (start here, before testing)

Audit `feature/weather-panel` against the design, the plan, and the audit that
commissioned it (`sysc-277`'s description). File findings in bd as
`bd create ... --deps discovered-from:sysc-277` rather than editing this
handover. Points the audit should confirm beyond the ordinary spec walk:

- **Decode strictness**: every new field is nil when the body omits or nulls
  it, and a present-but-short optional daily array covers only the days it
  has (the `fillDaily` contract) — a table test exists per case; check the
  cases against real Open-Meteo bodies.
- **Vocabulary completeness**: no second WMO mapping or condition-word table
  survived; grep for `IconRune(` call sites that render weather and for
  `sunny`/`partly_cloudy_day` remnants. The plugin's `Condition()` is a
  protocol consumer and deliberately keeps its own wording (D1).
- **The three-state rule on every surface**: bar text, bar tooltip, panel
  hero, and the control-centre page each distinguish placeholder, failure,
  and stale-with-age. `ccWeather` previously ignored `FailedSince`.
- **Ladder conformance in the new panel**: every `Height`/`Width`/`Gap` on
  new nodes derives from `theme.Metrics` or a named rung; the token scan is
  necessary but not sufficient (the arithmetic beside `ccBodyWidth`-style
  derivations is where drift hides — read the arithmetic).
- **Blur coverage**: `PanelWeather` inherits the backdrop through
  `panelSpec`; confirm no code path special-cases it away.
- **The icon-font artefact**: `sysc-icons.ttf` is committed and was
  regenerated with `internal/render/icons/build.py`; verify the eight new
  glyphs carry ink (`TestNightGlyphsCarryInk`,
  `TestWeatherDetailGlyphsCarryInk`) and that the builder's inventory,
  `iconfont.go`'s constants and `iconNames` stay in step.

### 2. Live gate on the laptop (after the audit's findings are applied or filed)

The laptop is `ssh -p 7777 nomadx@192.168.0.64` (x86_64, Niri at
`wayland-1`, `sysc-shell.service` active; at handover time it runs a
`4ab50429` build). The roadmap defers this gate to the owner; the owner has
approved deploying and testing. Checklist:

1. **Config first**: the laptop's `~/.config/sysc-shell/config.json` needs a
   `weather` block — `latitude` and `longitude` are the owner's decision, so
   **ask the owner for their coordinates** before deploying — plus a
   `{"id":"weather"}` item in one bar section. The optional
   `weather.location` label names the place in the hero; without it the hero
   shows the coordinates. Back up both binary and config
   (`*.bak-weather-<stamp>`) before changing anything; the 2026-09-13
   handover's deploy-over-a-foreign-binary hazard applies.
2. Deploy the `bd760f2` build, verify provenance with
   `go version -m ~/.local/bin/sysc-shell`, restart `sysc-shell.service`,
   and read `journalctl --user -u sysc-shell -n 50` clean.
3. Exercise: the bar item renders glyph and temperature; the tooltip carries
   facts; left-click opens and closes the panel anchored to the widget; the
   hero, details and day list render; `niri msg -j layers` before and after
   shows no leaked surface; the control-centre weather page matches; blur on
   and off both behave. Record the laptop's scale-1.25 results.
4. The stale and error states are exercisable live by pointing the config at
   an unroutable latitude source — or accept the unit evidence and record
   what was observed. Record everything on `sysc-277`, then close it or file
   the remainder.

Do not start a second weather branch; if the audit lands fixes, commit them
to `feature/weather-panel` and rebase onto `main` as it has moved.

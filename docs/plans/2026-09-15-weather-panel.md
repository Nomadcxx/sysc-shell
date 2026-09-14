# Weather Panel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship `sysc-277`: an enriched weather wire model, one weather vocabulary, an upgraded bar widget, standalone `PanelWeather`, and the control-centre page restyled onto the shared vocabulary.

**Architecture:** The wire package (`weather/`) stays a strict Open-Meteo decoder; `services.Weather` carries the decoded fields through unchanged. `internal/render` owns the only glyph mapping and the only condition-word table. The bar widget, the standalone panel and the control-centre page are three views of one `services.Reading`; the Registry owns the service and the reading exactly as it does today.

**Tech Stack:** Go standard library, the existing retained UI, `PanelHost`, the custom weather icon font and its authoring-time builder, and Open-Meteo over `net/http`. No new module dependency.

---

## Scope and ordering

The approved contract is [2026-09-15-weather-panel-design.md](2026-09-15-weather-panel-design.md). Work happens in `/home/nomadx/sysc-shell/.worktrees/feature/weather-panel` on `feature/weather-panel`, based on `main` at `41eaf68`. Run `bd` only from `/home/nomadx/sysc-shell`; status belongs in bd, not here. Do not add Markdown task checkboxes.

Do not touch the plugin's presentation, add a weather subprocess or a second service, build an Hourly view (D11), or add literal geometry anywhere (D12). The token-conformance scan must stay green after every task that edits `internal/shell`.

Run checks per package (`go test ./internal/weatherpkg...` style, focused), never `go test ./...`; gofmt and go vet before each commit. Commit message hook rejects `agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark` and similar — screen messages.

## Task 0: Reconcile the tree

**Files:** none.

1. Claim `sysc-277` with `bd update sysc-277 --status in_progress` from the primary checkout. (Done 2026-09-15.)
2. Confirm the worktree builds: `go build ./...`.
3. Confirm the baseline: `go test ./internal/shell -run 'TestSurfaceSourcesCarryNoLegacyVisuals'` passes on `main` at `41eaf68`, and `go test ./weather/ ./internal/services/ ./internal/render/` passes before any edit. Record any baseline failure in bd.

## Task 1: Wire model carries the enriched fields

**Files:** `weather/model.go`, `weather/client.go`, `weather/client_test.go`.

1. Failing checks first (table tests): a captured Open-Meteo body with the new fields decodes `Apparent`, `IsDay`, `Humidity`, `WindSpeed`, `WindDirection`, `UVIndex` on `Current`; `UVIndexMax`, `PrecipitationProbability`, `Precipitation` on `Day`; `Elevation`, `Timezone`, `TimezoneAbbreviation` on `Forecast`. Each optional field is a checked pointer: absent means nil, never a silent zero; a null decodes to nil.
2. Extend `Query` so `RequestURL` asks for the new current and daily fields (string assertion on the URL).
3. Implement. Keep `Decode` strict: pointer checks per field, the 64 KiB cap, and existing current-observation errors unchanged.
4. `go test ./weather/`, gofmt, vet, commit.

## Task 2: Service Reading carries the fields through

**Files:** `internal/services/weather.go`, `internal/services/weather_test.go`.

1. Failing check: `fetch` maps every new field onto `Reading`; a field absent from the body stays nil end to end.
2. Extend `Reading` with the same pointer fields (`Apparent *float64` and so on) as aliases of the wire model, matching the existing `Day`/`Unit` alias pattern.
3. Implement; no retry, rearm or lease change.
4. `go test ./internal/services/`, gofmt, vet, commit.

## Task 3: Night glyphs and the one vocabulary

**Files:** `internal/render/icons/svg/clear-night.svg`, `internal/render/icons/svg/partly-cloudy-night.svg`, `internal/render/icons/build.py`, `internal/render/icons/sysc-icons.ttf` (regenerated), `internal/render/iconfont.go`, `internal/render/iconfont_test.go`, `internal/shell/weatherwidget.go`, `internal/shell/controlcenter_pages.go` and its tests.

1. Failing checks first: a `render.WeatherCondition` table test over the WMO categories; `WeatherIcon(code, isDay)` / `WeatherIconName(code, isDay)` returning `clear-night` and `partly-cloudy-night` for codes 0 and 1–2 with `isDay == false` and the day glyphs otherwise; the iconfont name-coverage test updated for the two new names.
2. Author the two SVGs (24×24 viewBox, stroke-free filled paths, matching the existing set's weight); register them in `build.py` at uniE028/uniE029; regenerate `sysc-icons.ttf` with `python3 internal/render/icons/build.py`; verify the committed artefact round-trips through the Go test.
3. Add `WeatherCondition`, `WeatherIcon`, `WeatherIconName`. Delete `conditionWords`, `ccWeatherCondition` and `ccWeatherIcon` onto them.
4. `go test ./internal/render/ ./internal/shell/`, the token scan, gofmt, vet, commit.

## Task 4: Configuration gains the location label

**Files:** `internal/config/config.go`, `internal/config/load.go`, `internal/config/write.go`, `internal/config/*_test.go`.

1. Failing checks: `weather.location` loads, validates (plain text, ≤80 bytes, no control characters), defaults empty; `write.go` round-trips it; an oversized value is a path error.
2. Add `Location string` to `config.Weather`, the wire field, validation, and the writer.
3. Implement.
4. `go test ./internal/config/`, gofmt, vet, commit.

## Task 5: Bar widget: icon row, action, structured tooltip

**Files:** `internal/shell/weatherwidget.go`, `internal/shell/widget.go`, `internal/shell/registry.go`, `internal/shell/weatherwidget_test.go`, `internal/shell/registry_test.go`.

1. Failing checks first: the weather item builds a `KindIcon` weather glyph beside the temperature text (not the glyph inline in the text run); the node carries `panel:weather`; the tooltip tree holds condition, today's low/high, wind, updated time, and the age when stale, and is replaced on refresh; the placeholder, error and stale tones follow `formatWeather`'s rule.
2. Add `panelWeatherAction = "panel:weather"`; route left click through the same case shape the Bluetooth widget uses, with `AnchorX` from `actionCenterX`.
3. Implement using the `textWidget.tip` refresh path for the tooltip tree.
4. `go test ./internal/shell/`, the token scan, gofmt, vet, commit.

## Task 6: PanelWeather identity

**Files:** `internal/shell/panel.go`, `internal/shell/panelhost.go`, `internal/shell/blurcoverage_test.go` (no edit expected), `internal/shell/*_test.go`.

1. Failing checks first: `PanelWeather.String()` is `"weather"`; `panelTargetSize` returns 460×560; the blur coverage walk fails before the panel routes through `panelSpec` and passes after; `panel:weather` toggles the panel with the anchor set.
2. Append `PanelWeather` to the enum, `String()`, `panelTargetSize`, the spawn case (acquire a weather lease like `PanelClock` acquires the clock), the content-dispatch case returning the weather body, and the registry route.
3. Implement.
4. `go test ./internal/shell/`, the token scan, gofmt, vet, commit.

## Task 7: Panel body: hero and details

**Files:** new `internal/shell/weatherpanel.go`, `internal/shell/weatherpanel_test.go`, `internal/shell/panelhost.go` (dispatch wiring only).

1. Failing checks first (composition at the contract size, pure tree builders): the hero card holds glyph, temperature with unit, today's low/high, condition word, location line (configured label, coordinate fallback), updated time; the details card holds the D8 rows with icon, label and tabular value; an absent datum renders a dash; a never-observed reading renders the error state; a stale reading keeps its values and shows its age.
2. Implement from the ladder and density row only; share the card and row construction with the existing panel card helpers where they fit without bending them.
3. `go test ./internal/shell/`, the token scan, gofmt, vet, commit.

## Task 8: Panel forecast list

**Files:** `internal/shell/weatherpanel.go`, `internal/shell/weatherpanel_test.go`.

1. Failing checks first: seven rows in a `KindScroll`, today first and marked, each row carrying day name, glyph, low/high and condition word; fewer than seven days renders what exists; an empty forecast renders the placeholder.
2. Implement.
3. `go test ./internal/shell/`, the token scan, gofmt, vet, commit.

## Task 9: Control-centre page onto the shared vocabulary

**Files:** `internal/shell/controlcenter_pages.go`, `internal/shell/controlcenter_test.go`.

1. Failing checks first: the page's condition word and glyph come from the shared mapping; an aged reading shows its age; a failed reading shows the error state; today's low/high appear on the Today card.
2. Restyle the page onto `WeatherCondition`/`WeatherIconName` and the D9 state rule. Geometry already conforms; change presentation, not layout contracts.
3. `go test ./internal/shell/`, the token scan, gofmt, vet, commit.

## Task 10: Gates and handover

**Files:** none.

1. Full affected-package run: `go test ./weather/ ./internal/services/ ./internal/render/ ./internal/config/ ./internal/shell/ ./internal/plugin/ ./tests/...` (per-package, never the unrunnable full-tree `-race`).
2. `gofmt -w .` clean, `go vet ./...` clean, `git diff --exit-code -- go.mod go.sum`.
3. Build and deploy the exact provenance-checked binary for the owner's live look; record `niri msg -j layers` before and after open/close. The live gate is recorded, not claimed.
4. Update `sysc-277` with the evidence and close it, or file the remainder.

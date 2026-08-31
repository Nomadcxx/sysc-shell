# Milestone 6D Weather Reference Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a network-fed Weather plugin with current conditions, structured tooltip, settings, stale/error states, and a seven-day forecast panel.

**Architecture:** Extract the existing Open-Meteo parsing/client behavior into a small package with two real consumers, then keep scheduling and plugin state in the Weather process. Reuse M5 image handling and existing weather glyphs; extend the wire vocabulary only where the reference views require it.

**Tech Stack:** Go `net/http`, `encoding/json`, existing weather service behavior, M6C protocol host and M5 image cache.

**Design:** `docs/plans/2026-09-01-milestone-6-plugin-host-design.md`

---

### Task 1: Extract the shared Open-Meteo client

**Files:**
- Create: `weather/client.go`
- Create: `weather/model.go`
- Test: `weather/client_test.go`
- Modify: `internal/services/weather.go`
- Test: `internal/services/weather_test.go`

**Step 1:** Characterize the current service with tests for request parameters, current reading, seven daily values, Celsius/Fahrenheit, HTTP failure, malformed JSON, timeout, and WMO code preservation.

**Step 2:** Move only request construction and response decoding to `weather`. Keep leases, scheduling, stale policy, and shell snapshots in `internal/services`.

**Step 3:** Run `go test -race -count=1 ./weather ./internal/services -run Weather -v`. Existing built-in weather behavior must remain unchanged.

**Step 4:** Commit `refactor(weather): share Open-Meteo client`.

### Task 2: Add structured read-only tooltips

**Files:**
- Modify: `plugin/v1/node.go`
- Modify: `internal/plugin/view.go`
- Modify: `internal/shell/tooltip.go`
- Test: `internal/plugin/view_test.go`
- Test: `internal/shell/tooltip_test.go`

**Step 1:** Write tests for a tooltip column containing label/value rows, theme roles, width bounds, output-edge placement, no focusable nodes, no input events, replacement in place, and removal on bar leave or plugin failure.

**Step 2:** Reuse the existing tooltip surface and retained paint path. Reject buttons, inputs, scroll, lists, and drag nodes in tooltip views.

**Step 3:** Run `go test -race -count=1 ./internal/plugin ./internal/shell -run Tooltip -v`.

**Step 4:** Commit `feat(plugin): render structured tooltips`.

### Task 3: Expose weather icons through the approved image/icon path

**Files:**
- Modify: `plugin/v1/node.go`
- Modify: `internal/plugin/view.go`
- Modify if needed: `internal/render/iconfont.go`
- Test: `internal/plugin/view_test.go`
- Test: `internal/render/iconfont_test.go`

**Step 1:** Add tests mapping WMO groups to the existing eight project-owned weather symbols, plus a fallback for an unknown code. Add asset containment, decoded-size, cache-key, and malformed-image fallback tests if M5's `KindImage` does not already cover plugin assets.

**Step 2:** Prefer semantic icon names mapped by the host. Reuse M5 image decode/cache for packaged raster or SVG assets; do not add a second decoder or cache.

**Step 3:** Run `go test -race -count=1 ./internal/plugin ./internal/render -run 'Icon|Image|Weather' -v`.

**Step 4:** Commit `feat(plugin): expose bounded weather icons`.

### Task 4: Implement Weather scheduling and state

**Files:**
- Create: `plugins/reference/weather/manifest.json`
- Create: `plugins/reference/weather/service.go`
- Test: `plugins/reference/weather/service_test.go`

**Step 1:** Write tests for latitude/longitude validation, unit selection, 15-minute default interval, immediate fetch, one in-flight request, cancellation, fresh/stale/failed transitions, retained last good forecast, disabled location, backoff, and settings reconfiguration without a second loop.

**Step 2:** Implement with one `http.Client` timeout and one owner goroutine. Network completion updates process state; it never waits inside a view handler.

**Step 3:** Run `go test -race -count=1 ./plugins/reference/weather -run 'Service|Fetch|Stale|Config' -v`.

**Step 4:** Commit `feat(plugin): add Weather service`.

### Task 5: Build Weather views and settings

**Files:**
- Create: `plugins/reference/weather/view.go`
- Test: `plugins/reference/weather/view_test.go`
- Create: `cmd/sysc-plugin-weather/main.go`

**Step 1:** Write view tests for configurable temperature/unit/icon/condition bar content, tooltip modes, current condition card, seven forecast rows/cards, high/low, sunrise/sunset, loading, stale age, disabled, failure, literal custom accent, and accessible condition text.

**Step 2:** Declare location, units, refresh interval, bar fields, tooltip mode, and custom color through manifest settings. Use `visible_when` where a setting has a real dependency.

**Step 3:** Implement view snapshots for slow changes and keyed patches for age/current values. Keep control-center and desktop entries absent.

**Step 4:** Run `go test -race -count=1 ./plugins/reference/weather ./cmd/sysc-plugin-weather` and `go build ./cmd/sysc-plugin-weather`.

**Step 5:** Commit `feat(plugin): add Weather reference plugin`.

### Task 6: Prove Weather network and presentation failure paths

**Files:**
- Create: `tests/integration/plugin_weather_gate_test.go`
- Modify: `tests/integration/README.md`

**Step 1:** Use `httptest.Server` to drive success, timeout, HTTP failure, malformed response, recovery, and stale-last-good behavior without internet access.

**Step 2:** Open bar, tooltip, and panel views on two outputs. Verify one fetch owner, independent view IDs, shared readings, tooltip closure, and no redraw while state is unchanged.

**Step 3:** Run `go test -race -count=1 ./...` and `go vet ./...`.

**Step 4:** Close `sysc-72` with the gate evidence, flush beads, and commit `test(plugin): prove Weather behavior` with `.beads/issues.jsonl`.

# M10 Bar Weather Row Qualification Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Qualify M10 D1’s compact weather icon and temperature row on the bar while preserving the existing weather service and interaction paths.

**Architecture:** Reuse `weatherwidget.go`, the existing reading projection, density metrics, tooltip, and `panel:weather` action. This plan covers the M10 bar row only; the broader weather panel and data-parity work remains with `sysc-277` and `sysc-293`.

**Tech Stack:** Go, `sysc-shell` weather service, retained UI tree, Niri.

---

### Task 1: Prove the retained weather row

**Files:**
- Test: `internal/shell/weatherwidget_test.go`
- Implementation under review: `internal/shell/weatherwidget.go`

**Step 1: Run the focused checks**

Run: `go test -count=1 -run '^TestTheWeatherWidget' ./internal/shell`

Expected: PASS. The row contains an icon and formatted temperature, keeps the accessible name and panel action, and handles clear day/night, stale, placeholder, and failed states.

**Step 2: If a required behavior fails, add the smallest failing assertion first**

Keep the test at `weatherwidget_test.go`; do not edit the weather service or Control Centre weather page for a bar-only defect. Re-run the single named test and confirm the expected failure before changing code.

**Step 3: Re-run the focused checks**

Run: `go test -count=1 -run '^TestTheWeatherWidget' ./internal/shell`

Expected: PASS with no new service path or weather-only layout translation.

### Task 2: Inspect the live bar

**Files:**
- Evidence source: Niri on `DP-1`, 3440×1440 at scale 1.0.

**Step 1: Record layer state before opening the weather surface**

Run: `niri msg -j layers`

**Step 2: Inspect the bar row and its existing weather action**

Confirm the configured weather widget shows its icon and temperature in one row. Record unavailable or stale service state as displayed; do not invent a value to make the screenshot look complete.

**Step 3: Close the weather surface and record layer state again**

Run: `niri msg -j layers`

Expected: The bar remains mapped and the opened surface is removed. Do not change network credentials or service configuration for this check.

### Task 3: Record the slice evidence

Record the focused test command and the live result in the M10 completion handover. Keep broader weather ownership and any unavailable second-output check explicit.


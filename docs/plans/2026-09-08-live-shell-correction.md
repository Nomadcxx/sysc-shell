# Live Shell Correction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the deployed two-output shell match the approved bar and panel behaviour for gradient, audio, notifications, system status/processes, weather, and launcher access.

**Architecture:** Correct failures in their current owners. Extend the host renderer only for periodic gradients and one radial gauge; extend `sysc-metrics` for on-demand process data; otherwise compose the shell's existing services, panels, plugin host, theme tokens, and controls.

**Tech Stack:** Go, Linux `/proc`, PipeWire `pw-dump`/`wpctl`, existing `wl_shm` renderer and plugin v1 host. No new dependency, daemon, CGO, or plugin protocol field.

**Spec:** [2026-09-08-live-shell-correction-design.md](2026-09-08-live-shell-correction-design.md)

---

## Constraints

- Reuse beads `sysc-249`, `sysc-151`, `sysc-103`, `sysc-82`, `sysc-54`, `sysc-121`, and `sysc-72`; do not create a duplicate tree.
- Run `bd` only in `/home/nomadx/sysc-shell`. Preserve unrelated `sysc-154` work.
- Start each non-trivial slice with the smallest failing test. Keep one commit per coherent owner.
- For shell package tests, put harmless `loginctl` and `systemctl` stubs first on `PATH`.
- Code-touching completion gate is the repository gate from `AGENTS.md`; cap Go parallelism if the host becomes memory-bound.

### Task 1: Periodic theme gradient

**Files:** `internal/ui/tree.go`, `internal/render/gradient.go`, `internal/render/paint.go`, `internal/render/gradient_test.go`, `internal/render/paint_test.go`, `internal/shell/widget.go`, and focused shell tests.

1. Add a failing raster test proving a looping gradient has the same image at phases 0 and 1 and changes continuously near the wrap boundary.
2. Make loop motion sample `t` modulo one while finite non-loop paint keeps endpoint pinning.
3. Change the wordmark recipe to Primary → Secondary → Tertiary → Primary, `GradientLoop`, phase 0…1; keep reduced-motion parking.
4. Run:

   ```bash
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run 'Test.*Gradient|TestPaintWordmark'
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'Test.*Wordmark|TestAnimatorLoop'
   ```

### Task 2: Repair and resize audio

**Files:** `internal/services/audioenum.go`, `internal/services/audioenum_test.go`, `internal/shell/popout_audio.go`, `internal/shell/panelhost.go`, `internal/shell/audio_test.go`, `internal/shell/popout_audio_test.go`.

1. Add a `pw-dump` fixture containing string object metadata plus numeric and boolean settings. Assert valid sinks, sources, streams, and defaults survive it.
2. Decode metadata values as `json.RawMessage` and inspect only the default sink/source records. Preserve partial valid node data and expose poll errors in the enumeration snapshot/state.
3. Add geometry checks for a 3440×1440 output and a smaller output. Compute responsive content size with the D4 clamps, then give rows/cards the measured body width.
4. Recompose the header, tabs, volume rows, device wells, and body scroll to the design. Keep `scheduleControl`; verify callbacks call `wpctl` outside `Registry.mu` and reconcile on the next sample.
5. Run:

   ```bash
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/services -run 'Test.*Audio.*(Dump|Enum|Poll)'
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'Test.*Audio'
   ```

### Task 3: Contain notifications and fold the unread badge into the bell

**Files:** `internal/shell/popout_notifications.go`, `internal/shell/popout_notifications_test.go`, `internal/shell/notifywidget.go`, and its focused tests; renderer paint code only if the existing overlay path cannot paint the badge.

1. Add a layout test that walks every notification descendant and fails if its bottom exceeds the panel content box.
2. Derive the scroll viewport from the fixed chrome's measured height and ensure a notification rebuild requests the correct mapped size where needed.
3. Replace the font/custom bell plus sibling dot with one fixed `notifications` icon and paint a 6 px Error dot inside its top-right bounds when unread. Assert read/unread widths are identical.
4. Run:

   ```bash
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'Test.*Notif|Test.*Notify'
   ```

### Task 4: Add four radial bar gauges

**Files:** `internal/ui/tree.go`, `internal/render/layout.go`, `internal/render/paint.go`, focused renderer tests, `internal/config/config.go`, `internal/config/config_test.go`, `internal/shell/widget.go`, and focused widget tests.

1. Add a failing paint/layout test for `KindRadialGauge`: bounded 22 px circular track, proportional light arc with partial-alpha edge pixels, centred project-owned CPU/memory/GPU vector glyphs, a measured centred temperature value, unavailable state, and value clamping.
2. Implement that one node kind with the existing theme roles and software canvas. Use analytic annulus and cap coverage instead of binary native-pixel classification. Interpolate CPU, memory, and GPU arcs spatially from Accent to Secondary. For temperature, interpolate the arc from Accent to an end colour derived continuously from Accent below 60°C through contrast-aware amber at 75°C to Error at 85°C. Keep exact metric names and values in tooltips instead of ring text.
3. Add built-in metric selectors for CPU temperature and GPU usage as needed, then change the default sysmon group to CPU, memory, CPU temperature, and GPU. Put the monitor action on the group capsule so right-click on any child opens it.
4. Run:

   ```bash
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui ./internal/render -run 'Test.*Radial|TestKindCoverage'
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config ./internal/shell -run 'Test.*(Default|Metric|Monitor|Gauge)'
   ```

### Task 5: Add on-demand process sampling to `sysc-metrics`

**Files in `/home/nomadx/sysc-metrics`:** `metrics.go`, new `process_linux.go`, `process_linux_test.go`, README/roadmap if their public inventory requires it.

1. Add table tests for `/proc/<pid>/stat` parsing with parenthesised names, UID/status and RSS parsing, PID plus start-time identity, vanished processes, and CPU deltas across two observations.
2. Implement a sequential `ProcessSampler` using only the standard library. Return partial snapshots and per-process issues, and expose a small identity validation helper suitable for pre-signal checks.
3. Run the repository's focused tests and full required gate, commit, tag the next compatible release, then update the shell's `go.mod` pin and README pin table in one shell commit.

### Task 6: Make Processes the default monitor page

**Files:** `internal/services/metrics.go` or a narrow process service peer, `internal/shell/popout_monitor.go`, `internal/shell/panelhost.go`, monitor tests, and process action tests.

1. Add failing projection tests for default Processes page, outlined 28 px Monitor switching, 28 px search and filters, text-like 22 px column headers, All/User/System filters, case-insensitive search, stable sortable columns, full-card selection, 30 px cards on a 38 px pitch, one outlined 24 px `Kill` action, and virtual-list bounds.
2. Acquire process sampling only while `PanelMonitor` is open. Build the top page switcher and retain the shipped monitor cards as the second page.
3. Before `SIGTERM`, revalidate PID plus start time through the library; run the signal operation off `Registry.mu`; show permission, vanished, and recycled-PID errors inline. Do not expose `SIGKILL` in the row chrome.
4. Run:

   ```bash
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/services -run 'Test.*Process'
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'Test.*(Process|Monitor)'
   ```

### Task 7: Far-left launcher, weather activation, integration and live gate

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/shell/widget.go`, launcher widget tests, plugin install/config files or installer script already used by reference plugins, and user config during deployment.

1. Add a `launcher` built-in item, make it the first default left item, render the supplied ghost asset through the existing icon path, and route it to `PanelLauncher`.
2. Build/install `cmd/sysc-plugin-weather`; install its existing manifest, enable `org.sysc.weather`, place its bar entry, and configure Melbourne coordinates without modifying plugin v1.
3. Run focused config/widget/plugin tests, then the required gofmt/vet/test/module-diff gate.
4. Build current shell and plugin binaries, deploy immediately to the active user paths, restart the user services, and verify Niri layers on DP-1 and DP-3.
5. Live-check the gradient, real sinks/sources/streams and controls, notification scrolling/badge, four gauges, Processes default and safe test-process termination, weather panel, and launcher click. Record only unresolved hardware observations in beads.

### Task 8: Correct process density and radial raster quality

**Files:** `internal/shell/popout_process.go`, `internal/shell/popout_process_test.go`, `internal/render/radial.go`, `internal/render/paint_test.go`.

1. Change the process projection test first to assert 28 px controls, 22 px headers, 30 px full-row selectable cards, 38 px list pitch, an 8 px gap, a stroke-free selected wash, and a 24 px independently clickable outlined `Kill` child. Run it and confirm it fails against the current 32/28/40/44 px layout and inner-row selection.
2. Move the selection action, accessible row name, role, and focusability to the card. Remove the inner selection action and selected outline. Apply the approved dimensions and narrow the action column only as far as the measured label permits.
3. Add renderer tests that require partial-alpha coverage at the annulus edge, distinct Accent-to-Secondary samples along a CPU/memory/GPU arc, and temperature end colours at 59°C, 75°C, 80°C, and 85°C. Run them and confirm they fail against binary solid-colour rasterisation.
4. Compute coverage from signed distance to the annulus and round caps, blend each covered pixel once, and sample the active arc gradient by angular progress. Keep warning amber private to the renderer and derive it with enough theme contrast; do not add configuration or a dependency.
5. Run:

   ```bash
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'TestProcess(Table|Selected|Row|List|Keyboard)'
   timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run 'TestRadialGauge'
   ```

## Stop condition

Stop when the focused checks and repository gate pass, the fresh binaries are active, and the two-output live matrix above has been exercised. Do not pull control-centre implementation into this pass.

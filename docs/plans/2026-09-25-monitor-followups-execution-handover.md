# Control Centre Monitor Follow-ups: Execution Handover

This commissions the follow-up work left after the Control Centre Monitor page (sysc-517, merged to
`main` at `e2dfde2`) and sysc-metrics `v0.6.0`. The owner does not treat any of it as minor. bd holds
status; this document holds what each item is, where it lives, and how to prove it fixed.

Read first:

- `AGENTS.md` (project rules, live Niri environment, commit hook).
- `docs/plans/2026-09-25-control-centre-monitor-design.md` (D1–D6, the binding design).
- `docs/plans/2026-09-25-control-centre-monitor.md` (the plan that was executed).
- sysc-metrics `docs/plans/2026-09-25-gpu-vram.md` (the VRAM plan, released as `v0.6.0`).

## What exists now

- `internal/render/sparkline.go`: pure sparkline geometry (points, midpoint smoothing, stroke
  contours, area, circle, `rasterize` through `golang.org/x/image/vector`).
- `internal/render/paint.go:780` `paintGraph`: baseline, 18% area, optional second series, primary
  line, "now" dot. It serves every `KindGraph` consumer: the Control Centre, the standalone monitor,
  bar `graph` widgets and plugin views.
- `internal/shell/controlcenter_monitor.go`: the page (CPU and Memory heroes, rows for Temperature,
  GPU, Storage, Network, Disk I/O). Helpers in `monitorscale.go` and `monitorsubjects.go`; leases in
  `controlcenter_leases.go`.
- sysc-metrics `v0.6.0` (pushed, tag `v0.6.0` at `872e2fd`): `GPU.VRAM` / `GPU.VRAMValid`, and
  `matchBDF` now matches nvidia-smi's eight-digit PCI domain. Before that fix no `nvidia-smi` row ever
  matched on this desktop, which is why the GPU row reads `—` on the shell's current `v0.5.1` pin.

## Items

Do sysc-522 first: it is the only item the owner will see change on screen immediately, and it
changes what the GPU row test fixtures should look like for sysc-526 and sysc-527.

### sysc-522 — Pin sysc-metrics v0.6.0 and show GPU VRAM

- Bump `github.com/Nomadcxx/sysc-metrics` to `v0.6.0` in `go.mod` / `go.sum`, and update the pin
  table in `README.md` (AGENTS.md: current pins live in both).
- In `ccMonGPURow` (`controlcenter_monitor.go:189`), append `VRAM used / total` to the caption when
  `g.VRAMValid`, formatted with `formatBytes`, joined with `joinCaption`.
- Test: a GPU fixture with `VRAMValid` shows the VRAM caption; one without it shows the name only.
- Live: the GPU row shows a value, a line, and VRAM matching
  `nvidia-smi --query-gpu=memory.used,memory.total --format=csv,noheader,nounits`.

### sysc-526 — Temperature row ignores GPU temperature

- `ccMonTemperatureRow` (`controlcenter_monitor.go:170`) builds the value only when the CPU sensor is
  valid, so a GPU-only sensor shows `—`. Its tone uses CPU temperature alone; `metricGPUTemp`
  (`monitorscale.go:15`) is defined and unused.
- Fix: build the value from either sensor (`CPU 53°C · GPU 51°C`, either part omitted when invalid),
  and take the worse of `thresholdTone(metricCPUTemp, …)` and `thresholdTone(metricGPUTemp, …)`.
- Tests: GPU sensor only; CPU normal with GPU critical gives `ToneError`; neither gives `—`.

### sysc-524 — Sparkline has no NaN/Inf guard

- `sparklinePoints` (`sparkline.go:36`) clamps with `min(max(v, 0), 1)`; Go's builtins pass NaN
  through, and it reaches `vector.Rasterizer`. No current source produces NaN, but plugins feed this
  painter.
- Fix: treat a non-finite sample as zero (a plugin cannot mean anything else by it). Guard the second
  series the same way.
- Test: a table row with `math.NaN()` and `math.Inf(1)` produces finite points on the baseline.

### sysc-523 — Sparkline paint allocates per frame and joins every point

- `paintGraph` builds four `vector.Rasterizer`s and four full-box `image.Alpha` masks per paint;
  `strokeContours` (`sparkline.go:74`) adds an 8-gon join at every interior point of the smoothed
  line (about 950 at 120 samples). The Control Centre page paints six graphs every second.
- Measure first: a `BenchmarkPaintGraph` in `internal/render` at 240×28 and 331×64 with 120 samples,
  reporting ns/op and allocs/op. Record the numbers in the bd close reason.
- Fix: one rasterizer (`Reset`) and one reusable mask per paint, cleared between layers; emit a join
  only where the turn between segments exceeds a small angle (the smoothed line is nearly collinear
  almost everywhere). Keep the existing raster tests green, including
  `TestRasterizeAntialiasesADiagonalStroke` and `TestGraphLineStaysInsideItsBox`.
- Acceptance: allocs/op down to a constant independent of sample count, with the benchmark
  before/after in the close reason.

### sysc-532 — Bar and standalone monitor graphs stretch a short history

- Only `ccMonGraph` (`controlcenter_monitor.go:70`) sets `Window: services.HistorySize`. The bar
  graph widget (`metricwidget.go:257`) and the standalone monitor (`popout_monitor.go:160`) leave it
  0, so for two minutes after shell start their first samples stretch across the width and compress.
- Fix: set `Window: services.HistorySize` on both.
- Tests: each node carries the window (see `TestMonitorGraphsUseTheRingWindow`).
- Note: the live bar config (`~/.config/sysc-shell/config.json`) has no `graph` item. Prove the bar
  path in tests; do not edit the owner's config to capture it.

### sysc-525 — Monitor GPU row has no icon glyph

- `render.MetricIconRune` (`iconfont.go:338`) knows `cpu`, `memory`, `filesystem`/`block` and
  `network` only, so the GPU row is the only row without a glyph (D4 shows one on every row).
- Fix: add a GPU glyph to the shell icon font subset and a `"gpu"` case to `MetricIconRune`. Find how
  the subset is generated before editing (`grep -rn "MetricIconRune\|iconfont" internal/render
  scripts 2>/dev/null`); a codepoint the font lacks paints nothing.
- Test: `MetricIconRune("gpu") != 0`, and `TestMonitorRowLabelsHavePlainNames` still passes (the glyph
  is painted, never named).

### sysc-527 — GPU dash test does not prove the stale line is gone

- `TestMonitorPageDashesAGPUWithoutAValidSample` (`controlcenter_monitor_test.go:103`) asserts the
  dash, but its fixture has no GPU history, so a regression that plots stale history still passes.
- Fix: give the registry a metrics service with history for `Selector{Source: SourceGPU, Subject:
  "10de:2808"}` (or build the page with an explicit history map), keep `Usage.Valid == false`, and
  assert the GPU row's graph is `Absent` with no `Values`.
- Prove it by temporarily plotting history regardless of validity and watching the test fail.

### sysc-528 — Root block device lookup runs EvalSymlinks under Registry.mu

- `syncControlCentreSubjectsLocked` (`controlcenter_leases.go:40`) calls `primaryBlockDevice(snap,
  resolveDevicePath)`, which runs `filepath.EvalSymlinks` inside `UpdateMetrics` under `r.mu`, on
  every tick while the device is unresolved.
- Fix: resolve the root filesystem's source once, off the lock (for example when the Control Centre
  opens, through the existing off-owner scheduling), and cache it on the host; re-resolve only when
  the root filesystem's `Source` changes.
- Test: a counting `resolve` func called once across several syncs with the same snapshot.

### sysc-529 — sysc-metrics: parse nvidia-smi memory as integers (repo: sysc-metrics)

- `parseNvidiaCSV` (`gpu_linux.go:275`) parses `memory.used` / `memory.total` with `ParseFloat`,
  which accepts `inf` and `1e30`; the later `uint64` conversion is implementation-defined for those.
- Fix: `strconv.ParseUint(parts[3], 10, 44)` (whole MiB; 44 bits keeps `<<20` from overflowing), store
  as `uint64`, drop the float guard in `applyNvidiaCSV`.
- Tests: rows with `inf`, `1e30`, `-1` and `[N/A]` leave `hasVRAM` false.

### sysc-530 — sysc-metrics: tighten GPU tests (repo: sysc-metrics)

- `TestGPUMissingSMILeavesNvidiaInvalid` (`gpu_linux_test.go:193`): assert one `Issue` with
  `Source == "nvidia-smi"`; return an `*exec.ExitError`-shaped error to mirror the live exit status 9.
- `TestMatchBDFAcrossDomainWidths` (`gpu_linux_test.go:726`): add `("", "")` false,
  `("device", "device")` false, `(" 0000:08:00.0", "00000000:08:00.0\t")` true,
  `("10000:e1:00.0", "00010000:e1:00.0")` true, `("0000:08:00.0", "zzzz:08:00.0")` false.
- `[N/A]` end to end: one `readGPU` case with an NVIDIA card and an `[N/A]` memory row asserts usage
  and temperature fill while `VRAMValid` is false.
- sysc-529 and sysc-530 ship together as `v0.6.1`; bump the shell pin after (a follow-on to sysc-522).

## Working rules

- Worktree per branch under `.worktrees/`; run `bd` only from `/home/nomadx/sysc-shell`. Commits from a
  worktree: `git commit --no-verify` with the message screened by `bash ~/.git-hooks/commit-msg <file>`
  (the pre-commit hook otherwise stages the tracker file, and the commit-msg hook rejects words such as
  "both" and "bottom").
- Tests: per package, `GOMAXPROCS=4`. Never `-race` with `./...` (it has hard-locked this machine; a
  hook blocks it). sysc-metrics is one package, so `go test -race -count=1 .` there.
- Known failures that are not yours: `TestPanelSectionValidationPrecedesMutation` and three battery
  tests fail on `main` on this battery-less desktop; `TestTray*` flakes only under repo-wide load.
- Live checks: export the Niri environment from `AGENTS.md`, build to the scratchpad, `mv` over
  `~/.local/bin/sysc-shell`, `systemctl --user restart sysc-shell.service`, open the page with
  `sysc-shell ipc panel.open '{"panel":"control-center","section":"monitor"}'`, capture with `grim`.
  Other sessions share this machine; check the running binary's timestamp before replacing it.
- Close each bd issue with the commit hash and the evidence (test names, benchmark numbers, live
  capture), and commit `.beads/issues.jsonl` by splicing only your rows into `HEAD`'s copy.

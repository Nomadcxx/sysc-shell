# Weather Hero Composition Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the cramped weather surfaces with one dominant, visibly animated hero and a compact four-day forecast strip while preserving truthful data, accessibility, and the existing host-owned rendering boundary.

**Architecture:** Keep `PanelWeather` and the Control Centre Weather page on the existing `PanelHost` retained tree. Recompose both builders around one `KindStack` hero, strengthen the concrete weather raster program with readable celestial/cloud forms, and let the existing per-surface animator and invalidation channel drive phase. Keep plugin/v1 and the static bar widget unchanged.

**Tech Stack:** Go 1.26.4, existing `internal/ui` tree, `internal/render` CPU `wl_shm` canvas, `internal/shell` animator and PanelHost, standard library only.

---

## Working rules

- Work in `/home/nomadx/sysc-shell/.worktrees/feature/weather-effects` after rebasing it onto the docs commits on `main`.
- Preserve the four uncommitted weather fetch/decoder fixes; do not reset, stash, or overwrite them.
- Run `bd` only from `/home/nomadx/sysc-shell`; keep `sysc-318` in progress and commit `.beads/issues.jsonl` with any tracker change.
- Do not change `plugin/v1`, the weather service wire contract, the static bar weather widget, or `go.mod`/`go.sum`.
- Write each failing check before its production change and run the focused package check before moving to the next task.
- Keep all effect geometry bounded and semantic; no external animation engine, shader source, image asset, or second frame loop.

## Task 0: Rebase the implementation worktree

**Files:** none.

**Step 1: Confirm the docs commits and dirty scope.**

Run from `/home/nomadx/sysc-shell`:

    git log -3 --oneline
    git status --short
    git -C .worktrees/feature/weather-effects status --short

Expected: `83aed08` defines the design, the primary checkout is clean, and the feature worktree lists only the four weather fetch/decoder files.

**Step 2: Rebase the feature branch.**

    git -C .worktrees/feature/weather-effects rebase main

Expected: the branch rebases without discarding the four weather fixes. Resolve only conflicts caused by the docs commits; stop and inspect if a source file conflicts.

**Step 3: Run the baseline affected checks.**

    GOMAXPROCS=4 go test -p 2 ./weather ./internal/services ./internal/ui ./internal/render ./internal/shell
    GOMAXPROCS=4 go vet -p 2 ./weather ./internal/services ./internal/ui ./internal/render ./internal/shell

Expected: baseline tests and vet pass before the redesign.

## Task 1: Prove the new surface information budget and hierarchy

**Files:**

- Modify: `internal/shell/weatherpanel_test.go`
- Modify: `internal/shell/controlcenter_test.go`
- Modify: `internal/shell/weather_effect_test.go`

**Step 1: Write the failing tree-contract tests.**

Add focused checks that:

- the standalone tree has one weather hero and one compact forecast strip, with no details card, hourly action, or old detail labels;
- the hero contains the stable effect key and its effect-bearing stack occupies the hero card rather than a small intrinsic child;
- the Control Centre page keeps one Today hero plus exactly four forecast slots and no duplicate weather detail rows; and
- the retained weather content still contains location, temperature, condition, range, update/stale text, and only the three compact essentials when data exists.

Use the existing `collectTooltipLines`, tree walkers, and `ui.LayoutColumn` helpers. Assert node shape and bounded heights rather than pixel coordinates.

**Step 2: Run the tests to verify the intended failure.**

    go test ./internal/shell -run 'TestWeather(Hero|Panel|Tree|Information)|TestControlCentreWeather|TestWeatherEffectWrapper' -count=1

Expected: FAIL because the current builders still emit the 11-row details card, tabs, and information-heavy layout.

**Step 3: Implement the smallest tree recomposition.**

In `internal/shell/weatherpanel.go`, make the panel body a column containing:

1. the existing header;
2. a full-width `weatherHero` whose explicit height consumes the available body above the forecast; and
3. a single compact four-day forecast row/card with no Daily/Hourly tabs.

Move `feels-like`, `wind`, and `humidity` into one compact row inside the hero. Keep the current location, temperature, condition, day range, update/stale state, and error/placeholder behavior. Remove the old details-card constructors from this tree path and keep only service-backed formatting helpers still used by the hero or forecast.

In `internal/shell/controlcenter_pages.go`, keep the existing four-slot forecast row but make the Today card use the same minimal hero content and its full `ccTodayH` allocation. Do not add a second weather data path. Update `internal/shell/weather_effect.go` only as needed to preserve the effect-first stack when the hero's child grammar changes.

**Step 4: Run focused shell tests.**

    gofmt -w internal/shell/weatherpanel.go internal/shell/controlcenter_pages.go internal/shell/weather_effect.go internal/shell/weatherpanel_test.go internal/shell/controlcenter_test.go internal/shell/weather_effect_test.go
    go test ./internal/shell -run 'TestWeather|TestControlCentreWeather|TestTheWeatherPage' -count=1

Expected: the new tree contracts and retained data/error tests pass. Any old test that specifically requires removed detail rows must be rewritten to the approved information budget, not preserved through a hidden node.

**Step 5: Commit the composition.**

    git add internal/shell/weatherpanel.go internal/shell/controlcenter_pages.go internal/shell/weather_effect.go internal/shell/weatherpanel_test.go internal/shell/controlcenter_test.go internal/shell/weather_effect_test.go
    git commit -m "feat(weather): center the hero surface"

## Task 2: Add failing visibility checks for animated weather forms

**Files:**

- Modify: `internal/render/effect_test.go`
- Modify: `internal/shell/weather_effect_test.go`

**Step 1: Write the failing renderer checks.**

Extend the existing deterministic effect fixtures with tests that:

- clear weather paints a measurable celestial disc/halo region rather than only a low-alpha wash;
- partly cloudy weather paints measurable sun and cloud coverage in two distinct regions;
- changing phase moves or changes the cloud/celestial form as well as any particle field;
- rain, snow, and thunderstorm retain meaningful coverage in a card-sized target; and
- reduced motion remains deterministic at its parked phase.

Measure a small region-of-interest or bounded count of non-background/role-matching pixels. Do not assert exact anti-aliased raster bytes for geometry that is intentionally blended. Keep the existing byte-for-byte same-input determinism test.

**Step 2: Run the tests to verify they fail for the correct reason.**

    go test ./internal/render -run 'TestWeatherEffect|Test.*Effect' -count=1

Expected: the new clear/partly-cloudy visibility assertions fail because those variants currently draw only the faint wash.

## Task 3: Implement the readable weather motion catalogue

**Files:**

- Modify: `internal/render/weather_effect.go`

**Step 1: Add bounded geometry helpers.**

Add package-private helpers for a filled/soft-edged disc and a small overlapping cloud mass. Derive all coordinates from the validated effect box, use semantic theme roles, clamp alpha and local coordinates, and route writes through the existing masked blend path. Keep the work bounded by the existing fixed raster dimensions and particle constants.

**Step 2: Compose the condition layers.**

Update the weather program so:

- clear and partly cloudy render a clear sun/moon disc and halo, with a slow phase-based ray/halo change;
- partly cloudy, cloudy, rain, snow, and thunderstorm render one or more broad cloud masses with a restrained vertical/lateral phase drift;
- fog keeps broad low-frequency haze bands; and
- rain, snow, and lightning retain their existing deterministic particle/flash layers, with opacity/spacing high enough to read over the hero card.

Use the researched motion cadence as behavioural guidance: cloud lift over roughly one slow cycle, a slower celestial rotation/breathing cycle, staggered precipitation fade/entry, and an irregular lightning pulse. Keep reduced motion at a stable representative frame by using the existing parked phase; do not add a timer or random source.

**Step 3: Run renderer tests and the bounded benchmark.**

    gofmt -w internal/render/weather_effect.go internal/render/effect_test.go
    go test ./internal/render -run 'TestWeatherEffect|Test.*Effect' -count=1
    go test ./internal/render -run '^$' -bench BenchmarkPaintWeatherEffect -benchmem -count=1

Expected: deterministic, phase, visibility, clipping, and reduced-motion tests pass. Record the benchmark output for the live handover; it is not a release claim by itself.

**Step 4: Commit the raster correction.**

    git add internal/render/weather_effect.go internal/render/effect_test.go
    git commit -m "feat(weather): add readable motion forms"

## Task 4: Prove and correct visible-surface invalidation

**Files:**

- Modify: `internal/shell/panelhost_test.go`
- Modify: `internal/shell/effectmotion_test.go`
- Modify: `internal/shell/panelhost.go` only if the failing test identifies a shared lifecycle defect

**Step 1: Write a deterministic host-level frame test.**

Build a configured `PanelWeather` host and a Control Centre host using the existing test registry helpers and a fixed clock. Render the same host at two clock phases through `PanelHost.render`, and assert that the hero-region pixels differ. Then exercise the frame eligibility/invalidations path and assert that a visible weather surface publishes while a closed or hidden one stops. Keep the test free of sleeps; use the fake clock and existing invalidation channel helpers.

The test must distinguish these boundaries: weather effect target exists, animator phase advances, render resolves the phase, and the surface publishes an invalidation. A test that only inspects `animator.Settled` is insufficient.

**Step 2: Run the test before changing lifecycle code.**

    go test ./internal/shell -run 'Test(Weather|ControlCentre).*Frame|Test.*EffectMotion' -count=1

Expected: the frame test either exposes the current media-only rebuild/eligibility guard or passes, which determines whether a lifecycle edit is necessary. Do not edit `surfaceFrameLoop` before this result.

**Step 3: Fix only the responsible shared guard if needed.**

If the test shows that weather frames are not eligible, replace the media-specific condition with a shared visible-effect predicate that walks the live retained tree (or the already-resolved effect targets) under `Registry.mu`. Preserve the media position update path, one running loop per surface, frame callback backpressure, and stop-on-close behavior. If the test passes, leave the lifecycle code unchanged and record that the perceptual defect was renderer contrast/form, not scheduling.

**Step 4: Run the focused race check.**

    go test -race -count=1 ./internal/shell -run 'Test(Weather|ControlCentre).*Frame|Test.*EffectMotion'

Expected: no new race. The known unrelated `sysc-310` full-shell race remains open if the complete package race run reproduces it.

**Step 5: Commit only a lifecycle correction when required.**

    git add internal/shell/panelhost.go internal/shell/panelhost_test.go internal/shell/effectmotion_test.go
    git commit -m "fix(weather): invalidate visible effect surfaces"

If no production lifecycle change is required, commit the host-level regression test with the nearest composition commit instead.

## Task 5: Run affected gates and inspect the final tree

**Files:** none unless a test exposes a regression.

**Step 1: Run format, vet, and focused packages.**

    gofmt -w internal/render internal/shell internal/ui
    test -z "$(gofmt -l internal/render internal/shell internal/ui)"
    go vet ./...
    go test -count=1 ./weather ./internal/services ./internal/ui ./internal/render ./internal/shell ./internal/plugin
    git diff --exit-code -- go.mod go.sum

Expected: all commands pass and module files are unchanged.

**Step 2: Run the affected race coverage.**

    go test -race -count=1 ./weather ./internal/services ./internal/ui ./internal/render ./internal/shell ./internal/plugin

Expected: pass, or record the pre-existing `sysc-310` media relay race with the exact failing test and leave that issue open.

**Step 3: Review the final tree contracts.**

    rg -n "Temperature max|Temperature min|UV index|Timezone|Sunrise|Sunset|Precip chance|weather-view:" internal/shell/weatherpanel.go internal/shell/controlcenter_pages.go
    rg -n "weatherTodayEffectKey|weatherHeroEffectKey|KindEffect|EffectPhase" internal/shell/weatherpanel.go internal/shell/controlcenter_pages.go internal/shell/weather_effect.go

Expected: removed detail labels and hourly tab are absent from the two target builders; both target heroes retain a stable effect key and no bar builder gains `KindEffect`.

## Task 6: Laptop Niri gate and handoff

**Files:**

- Create: `docs/plans/2026-09-17-weather-hero-composition-completion-handover.md`
- Modify: `docs/plans/README.md`
- Modify: `.beads/issues.jsonl` through `bd` export only if the issue state changes

**Step 1: Build the exact binary.**

    go build -o /tmp/sysc-shell-weather-hero ./cmd/sysc-shell
    sha256sum /tmp/sysc-shell-weather-hero

Expected: build succeeds; record the hash in the handoff.

**Step 2: Deploy through the existing user service on the laptop.**

Connect with `ssh -p 7777 nomadx@192.168.0.64`. Inspect the service unit and current process before replacing the binary. Copy the exact build to the service's existing binary path, restart that user service once, and verify one process. Do not launch a second shell process manually.

Use the documented Niri environment, open standalone Weather and the Control Centre Weather page, and inspect the hero at the live fetched condition. Exercise at least one precipitation/lightning variant through a deterministic local fixture or available API data only if the existing service path supports it without falsifying user data. Capture before/after `niri msg -j layers`, close both surfaces, and capture layers again.

**Step 3: Record live observations.**

Record actual hero visibility, motion, frame cadence/cost, stale/error rendering, reduced-motion result, surface IDs, and any laptop calibration issue. State the one-output/two-output limitation if applicable. Do not infer live success from unit tests.

**Step 4: Run the final verification commands and create the handoff.**

Run the repository gates from Task 5 again after the final code state, write the completion snapshot with commit hashes and command output, and register it in `docs/plans/README.md` in the same commit. Close `sysc-318` only after the code, automated tests, and live gate satisfy the acceptance criteria:

    bd close sysc-318 --reason "Hero composition, readable effects, invalidation proof, and laptop gate recorded in the completion handoff"

Export Beads from `/home/nomadx/sysc-shell`, inspect `wc -l` and `git diff` before committing the JSONL. The completion handoff is immutable after landing; later corrections go in Beads.

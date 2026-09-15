# Animated Weather Effects Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add animated rain, snow, fog, cloud, and lightning effects to the standalone Weather panel and Control Centre weather page through a bounded host-owned renderer layer that future supported plugins can request declaratively.

**Architecture:** Add a non-interactive KindEffect leaf that composes as the first child of the existing KindStack. The existing CPU wl_shm renderer paints a validated effect descriptor into the node's semantic mask, while the existing per-surface animator supplies phase and the existing scheduler supplies frame backpressure. Keep plugin v1 unchanged; a later negotiated protocol minor can map an effects capability onto the same host catalogue without accepting plugin shader code or an independent render loop.

**Tech Stack:** Go 1.26.4, existing internal/ui retained tree, existing premultiplied BGRA internal/render.Canvas, wl_shm/Niri, current PanelHost animator and scheduler, standard library only.

---

## Working rules

- Implement in .worktrees/feature/weather-effects, created from the docs-only design commit on main.
- Do not carry the current checkout's unrelated .beads, test, .commandcode, or .cursor changes into the implementation worktree.
- Do not modify plugin/v1 in this slice. The existing strict unknown-field test is the compatibility guard for the future negotiated extension.
- Keep all effect writes inside internal/render; keep all phase ownership inside internal/shell; keep internal/ui types free of renderer imports.
- Use table tests or deterministic pixel assertions for non-trivial logic. Do not use sleeps to prove animation.
- Every implementation task ends with its focused test and affected package test passing before the next task starts.

## Task 0: Create the isolated implementation worktree

Files: none.

Step 1: Confirm the design commit and dirty primary checkout.

Run from /home/nomadx/sysc-shell:

    git log -1 --oneline
    git status --short

Expected: 67a32fe docs: define animated effects (or its amended equivalent), and only pre-existing user changes are listed.

Step 2: Create the worktree.

    git worktree add -b feature/weather-effects .worktrees/feature/weather-effects main

Expected: the new worktree is based on the design commit and starts clean.

Step 3: Run the baseline affected-package checks.

    cd /home/nomadx/sysc-shell/.worktrees/feature/weather-effects
    GOMAXPROCS=4 go test -p 2 ./internal/ui ./internal/render ./internal/shell ./internal/plugin
    GOMAXPROCS=4 go vet -p 2 ./internal/ui ./internal/render ./internal/shell ./internal/plugin

Expected: PASS and no vet findings. If the baseline exposes an unrelated failure, record its package and issue before changing effect code.

## Task 1: Add the retained effect descriptor and UI layer

Files:

- Create: internal/ui/effect.go
- Modify: internal/ui/tree.go
- Modify: internal/ui/layout.go
- Modify: internal/ui/column.go
- Modify: internal/ui/kindcoverage_test.go
- Create: internal/ui/effect_test.go
- Modify: internal/ui/stack_test.go

Step 1: Write the failing descriptor tests.

In internal/ui/effect_test.go, cover the host-owned data contract:

    func TestEffectSpecValidation(t *testing.T) {
        tests := []struct {
            name string
            spec EffectSpec
            ok   bool
        }{
            {"weather", EffectSpec{Program: EffectWeather, Intensity: 0.7, Speed: 1}, true},
            {"unknown program", EffectSpec{Program: EffectProgram(99)}, false},
            {"nan intensity", EffectSpec{Program: EffectWeather, Intensity: math.NaN()}, false},
            {"intensity above one", EffectSpec{Program: EffectWeather, Intensity: 1.1}, false},
            {"negative speed", EffectSpec{Program: EffectWeather, Speed: -1}, false},
        }
        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                if got := tt.spec.Validate() == nil; got != tt.ok {
                    t.Fatalf("Validate() = %v, want %v", got, tt.ok)
                }
            })
        }
    }

Also add a stack test that an effect layer receives the same arranged box as content and returns no hit action.

Run:

    go test ./internal/ui -run 'TestEffectSpecValidation|TestStack.*Effect' -count=1

Expected: FAIL because the descriptor and kind do not exist yet.

Step 2: Implement the minimum UI contract.

In internal/ui/effect.go, define EffectProgram, with zero meaning no program and EffectWeather as the first valid program; EffectSpec with Program, Variant, Seed, Intensity, and Speed; Validate checking the current supported program, finite intensity in [0,1], and finite speed in [0,4]; and only the weather-variant constants needed by this renderer.

Append KindEffect immediately before kindCount in internal/ui/tree.go so existing kind values do not move. Add Effect EffectSpec and EffectPhase float64 to ui.Node. Existing Shape and Radius provide the mask request; do not add another radius field.

Add the effect to measureNode, columnChildHeight, and stack placement. It measures as zero intrinsic size and receives its parent stack content box, making it a background layer rather than a layout participant. Add it to the all-kinds coverage table and sample node.

Step 3: Run the focused tests.

    gofmt -w internal/ui/effect.go internal/ui/tree.go internal/ui/layout.go internal/ui/column.go internal/ui/effect_test.go internal/ui/kindcoverage_test.go internal/ui/stack_test.go
    go test ./internal/ui -run 'TestEffectSpecValidation|TestStack.*Effect|TestAllKindsAreAccountedFor|TestEveryKindMeasures' -count=1

Expected: PASS.

Step 4: Commit the UI contract.

    git add internal/ui
    git commit -m "feat: add effect tree layer"

## Task 2: Implement deterministic weather raster primitives

Files:

- Create: internal/render/effect.go
- Create: internal/render/weather_effect.go
- Create: internal/render/effect_test.go

Step 1: Write deterministic renderer tests.

Use the existing canvas helpers, a fixed style, fixed EffectSpec, and explicit EffectPhase:

    func TestWeatherEffectIsDeterministic(t *testing.T) {
        first := paintEffectFrame(t, rainSpec(7), .25)
        second := paintEffectFrame(t, rainSpec(7), .25)
        if !bytes.Equal(first, second) {
            t.Fatal("same effect inputs produced different pixels")
        }
    }

    func TestWeatherEffectPhaseChangesAnimatedPixels(t *testing.T) {
        first := paintEffectFrame(t, rainSpec(7), .10)
        second := paintEffectFrame(t, rainSpec(7), .60)
        if bytes.Equal(first, second) {
            t.Fatal("changing phase did not change the weather effect")
        }
    }

    func TestWeatherEffectRejectsInvalidSpec(t *testing.T) {
        c := newTestCanvas(t, 64, 64)
        n := &ui.Node{Kind: ui.KindEffect, Effect: ui.EffectSpec{Program: ui.EffectProgram(99)}}
        if err := paintEffect(c, n, testStyle); err == nil {
            t.Fatal("invalid effect spec was painted")
        }
    }

Run:

    go test ./internal/render -run 'TestWeatherEffect|Test.*Effect' -count=1

Expected: FAIL because the renderer entry point and weather program do not exist.

Step 2: Implement the concrete effect dispatcher.

In internal/render/effect.go, add the package-private paintEffect entry point. It validates the node descriptor, resolves physical bounds and semantic radius, obtains RoundedMask, and dispatches only the supported weather program. Keep it in internal/render; no renderer interface or registry is needed for one host catalogue.

In internal/render/weather_effect.go, implement bounded helpers for a deterministic low-frequency sky/cloud wash, diagonal rain streaks, snow flakes with stable size and drift, fog/haze modulation, and thunderstorm lightning pulses.

Use a small integer hash/mix function for stable per-particle positions rather than a mutable random source. Clamp every derived coordinate and alpha before writing. Keep particle counts constants in the renderer. Use semantic palette roles and premultiplied blending helpers already in the package. A ponytail comment marks the fixed particle-count ceiling and names a measured upgrade path if a benchmark later proves it too coarse.

The weather variant mapper covers the shared WMO categories used by the existing weather vocabulary. Invalid variants return an error rather than selecting a different state.

Step 3: Run focused tests and benchmark the small target.

    gofmt -w internal/render/effect.go internal/render/weather_effect.go internal/render/effect_test.go
    go test ./internal/render -run 'TestWeatherEffect|Test.*Effect' -count=1
    go test ./internal/render -run '^$' -bench BenchmarkPaintWeatherEffect -benchmem -count=1

Expected: focused tests PASS. Record the benchmark as a baseline; make no performance claim from it alone.

Step 4: Commit the raster program.

    git add internal/render/effect.go internal/render/weather_effect.go internal/render/effect_test.go
    git commit -m "feat: rasterize weather effects"

## Task 3: Paint effect layers in stack order with semantic clipping

Files:

- Modify: internal/render/paint.go
- Modify: internal/render/paint_test.go
- Modify: internal/ui/stack_test.go

Step 1: Write failing composition checks.

Prove that a KindEffect child paints before a later foreground child, the foreground remains legible above it, pixels outside a rounded effect mask remain unchanged or transparent, and ui.Hit cannot return an action from the effect node.

Run:

    go test ./internal/render ./internal/ui -run 'Test.*Effect|TestStack.*Hit' -count=1

Expected: FAIL because paintNodeContent has no KindEffect case.

Step 2: Add the paint switch case.

In paintNodeContent, route ui.KindEffect to paintEffect. Keep it in the existing leaf dispatch. KindStack already walks children forward and needs no new compositor. Preserve node opacity and canvas restriction paths so an effect inside a scroll viewport or translucent group follows existing semantics.

Step 3: Run the composition checks.

    gofmt -w internal/render/paint.go internal/render/paint_test.go internal/ui/stack_test.go
    go test ./internal/render ./internal/ui -run 'Test.*Effect|TestStack.*Hit' -count=1

Expected: PASS.

Step 4: Commit the painter integration.

    git add internal/render/paint.go internal/render/paint_test.go internal/ui/stack_test.go
    git commit -m "feat: compose effect layers"

## Task 4: Drive effect phase through the existing surface animator

Files:

- Modify: internal/shell/animation.go
- Modify: internal/shell/panelhost.go
- Create: internal/shell/effectmotion_test.go

Step 1: Write failing lifecycle checks.

Without sleeping, cover that a live effect creates one looping animator value keyed by its stable node key, resolving the same tree preserves phase, removing the effect forgets the loop and leaves the animator settled, and reduced motion resolves a fixed phase and stays settled.

Run:

    go test ./internal/shell -run 'Test.*Effect.*Motion|Test.*Surface.*Frames' -count=1

Expected: FAIL because no effect channel or tree resolver exists.

Step 2: Add the dedicated animation channel and resolver.

Append animEffect after the existing animation channels. Reuse TargetLoop and Value rather than adding a ticker. Add a panel-tree resolver that walks KindEffect nodes, requires a non-empty stable key, targets one bounded loop duration, and writes EffectPhase onto the render tree copy. It forgets effect keys that disappeared during a rebuild.

Initialize the panel animator before the first panel tree is resolved, or call the resolver immediately after initialization, so the first weather frame has the same stable key as later rebuilds. Call the resolver from the existing rebuildPanel or surface-frame path under Registry.mu; do not hold the lock while publishing to Wayland.

Use the existing frameCap and animateSurface path. A live non-reduced effect keeps the animator unsettled through its loop; reduced motion creates no loop. Closing the panel still closes stopAnim, and removing the final effect allows the loop to exit.

Step 3: Run lifecycle checks and existing animation tests.

    gofmt -w internal/shell/animation.go internal/shell/panelhost.go internal/shell/effectmotion_test.go
    go test ./internal/shell -run 'Test.*Effect.*Motion|Test.*Surface.*Frames|TestAnimator' -count=1

Expected: PASS.

Step 4: Commit animation ownership.

    git add internal/shell/animation.go internal/shell/panelhost.go internal/shell/effectmotion_test.go
    git commit -m "feat: animate effect phases"

## Task 5: Add the shared weather effect layer to both surfaces

Files:

- Create: internal/shell/weather_effect.go
- Create: internal/shell/weather_effect_test.go
- Modify: internal/shell/weatherpanel.go
- Modify: internal/shell/controlcenter_pages.go

Step 1: Write failing tree-construction tests.

Using existing weather fixtures, assert that weatherHero contains a KindStack with the effect first and content later; the Control Centre Today card contains the same effect program and variant; stale observed readings retain the effect; and placeholder/error readings contain no animated effect node.

Run:

    go test ./internal/shell -run 'Test.*Weather.*Effect|TestWeatherPanel|TestControlCentre.*Weather' -count=1

Expected: FAIL because the trees contain only static cards.

Step 2: Implement one shared effect-node constructor.

In internal/shell/weather_effect.go, map the existing WMO code and day/night state to a validated ui.EffectSpec. Set a stable key per surface, a fixed seed, bounded intensity, and a speed the renderer can apply to phase. Return no effect for absent/error readings. Keep stale observed readings animated because the last successful value remains authoritative.

Add a helper that wraps an existing card's content in the same stack grammar as the media card: effect layer, optional quiet scrim, then foreground. Preserve the card's existing fill, shape, height, padding, and accessibility nodes.

Use the helper from weatherHero and ccWeather's Today card only. Leave the four-slot Control Centre forecast cards and bar widget static so the effect does not become compact-region noise.

Step 3: Run weather composition tests.

    gofmt -w internal/shell/weather_effect.go internal/shell/weather_effect_test.go internal/shell/weatherpanel.go internal/shell/controlcenter_pages.go
    go test ./internal/shell -run 'Test.*Weather.*Effect|TestWeatherPanel|TestControlCentre.*Weather' -count=1

Expected: PASS, with existing weather and Control Centre tests still green.

Step 4: Commit the surface consumers.

    git add internal/shell/weather_effect.go internal/shell/weather_effect_test.go internal/shell/weatherpanel.go internal/shell/controlcenter_pages.go
    git commit -m "feat: animate weather surfaces"

## Task 6: Verify the complete host path and future protocol guard

Files:

- Modify: internal/render/effect_test.go
- Modify: internal/shell/weather_effect_test.go
- Modify: internal/plugin/view_test.go only if a focused compatibility assertion is needed

Step 1: Add the remaining focused proof.

Ensure tests cover every supported weather variant producing bounded output; deterministic and phase-dependent lightning; reduced-motion phase identical across attempted animation ticks; rounded clipping; invalid descriptor rejection; and the existing plugin decoder rejecting an unknown v1 field, documenting why future effects require a negotiated protocol minor.

Do not add an effect wire kind or capability to plugin/v1 in this task.

Step 2: Run affected package gates.

    GOMAXPROCS=4 go test -p 2 ./internal/ui ./internal/render ./internal/shell ./internal/plugin ./tests/...
    GOMAXPROCS=4 go test -race -p 2 ./internal/ui ./internal/render ./internal/shell ./internal/plugin ./tests/...
    GOMAXPROCS=4 go vet -p 2 ./internal/ui ./internal/render ./internal/shell ./internal/plugin ./tests/...
    test -z "$(gofmt -l internal/ui internal/render internal/shell internal/plugin tests)"
    git diff --check
    git diff --exit-code -- go.mod go.sum

Expected: all commands pass. If the known pre-existing shell race sysc-310 reappears, do not change it as part of this feature; record the exact failing test and use the non-race affected-package result only where the repository gate explicitly permits that blocker.

Step 3: Run the effect benchmark.

    go test ./internal/render -run '^$' -bench BenchmarkPaintWeatherEffect -benchmem -count=5

Record the stable range and rendered card dimensions. The target is no more than 30 effect frames per second on the laptop surface; the benchmark informs the live check but does not replace it.

Step 4: Commit verification additions.

    git add internal/render/effect_test.go internal/shell/weather_effect_test.go internal/plugin/view_test.go
    git commit -m "test: cover effect safety bounds"

## Task 7: Run the laptop Niri visual and lifecycle gate

Files:

- Create: docs/plans/2026-09-16-effect-rendering-completion-handover.md
- Modify: .beads/issues.jsonl through bd, in the same commit as the handover

Step 1: Build the exact implementation binary.

    go build -o /tmp/sysc-shell-weather-effects ./cmd/sysc-shell
    sha256sum /tmp/sysc-shell-weather-effects

Record the hash in the handover before deployment.

Step 2: Connect to the laptop Niri session.

    ssh -p 7777 nomadx@192.168.0.64
    export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
    export WAYLAND_DISPLAY=wayland-1
    export XDG_RUNTIME_DIR=/run/user/1000
    niri msg -j layers

Record the pre-test layer list. Do not use a broad pkill -f; identify and terminate only the known shell process by PID if replacement is necessary.

Step 3: Exercise the two target surfaces.

Deploy the exact hash-checked binary through the existing laptop service path, then exercise:

- standalone Weather panel with clear, rain, snow, fog, and thunderstorm test readings where the service or fixture path permits;
- Control Centre Weather page with the same visual states;
- panel open, close, reopen, and focus return;
- reduced motion enabled and disabled; and
- no bar animation beyond its existing static weather widget.

Observe that particles move without changing layout, lightning does not leak outside the card, text stays above the effect, and CPU use is acceptable at the resolved cap. Capture screenshots or measurements when visual tuning is needed.

Step 4: Prove surface cleanup.

    niri msg -j layers

Run it before opening, while open, and after closing. The post-close output must contain no weather panel or shield surface left by the test.

Step 5: Record and close the work.

Write the actual hash, commands, frame observations, reduced-motion result, layer output summary, CPU/frame measurement, and unresolved hardware issues in the completion handover. Update the owning beads issue through bd; export and inspect .beads/issues.jsonl before committing it with the handover. Do not claim a live result that was not observed.

## Stop condition

Stop after the two target surfaces render the supported weather variants, focused and affected-package gates pass, the plugin boundary remains unchanged, and the laptop Niri lifecycle gate records no leaked surfaces. Do not add the bar effect, a GPU backend, an external engine, or protocol v1.1 work unless a measured result or a real supported plugin consumer creates that requirement.

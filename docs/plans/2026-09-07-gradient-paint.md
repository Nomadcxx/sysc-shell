# Animated Gradient Paint Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Land a host-owned 2–4 stop linear gradient on rects and alpha masks, with a looping offset, and ship it first on the centred bar wordmark.

**Architecture:** `ui.GradientPaint` on `ui.Node` (`Count == 0` keeps today's solid path). CPU sampling copies Noctalia PR 3922's axis + unclamped offset. Motion is a looping animator mode, not hover chrome. The bar constructs `anim`, drives `animateSurface`, and stops on `DropHost`. `plugin/v1` does not change.

**Tech Stack:** Go, `wl_shm` canvas, existing `internal/ui` / `internal/render` / `internal/shell`. No GLES, no new module, no CGO.

**Spec:** [2026-09-07-gradient-paint-design.md](2026-09-07-gradient-paint-design.md) (D1–D12).

**Tracker:** epic `sysc-217`. Tasks `sysc-218`…`sysc-223`. Discovered work goes to bd.

## Global Constraints

- Read the design before Task 1. Argue from D1–D12.
- Never `go test ./...` or `-race`. This machine dies. Every check is:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

- `go test ./internal/shell` runs `loginctl terminate-session self` unless `loginctl` and `systemctl` are stubbed earlier on `PATH`.
- After each task: `gofmt -w` the touched files, run the named tests, commit. Screen the message for the hook (no `agent`, `cursor`, `codex`, `llm`, `both`, `Hallmark`).
- Run `bd` only from `/home/nomadx/sysc-shell`.
- This checkout is shared. `git status` before you start; never `git checkout --`, `git stash`, or `git add -A` over work you did not put there.
- Do not touch `plugin/v1`. Do not add glow or EGL.

---

### Task 1: Axis and stop lerp (`sysc-218`)

**Files:**
- Create: `internal/render/gradient.go`
- Create: `internal/render/gradient_test.go`

**Step 1: Write the failing test**

```go
package render

import "testing"

func TestGradientAxisHorizontal(t *testing.T) {
	t.Parallel()
	a := gradientAxisForDegrees(0)
	if a.x != 1 || a.y != 0 {
		t.Fatalf("axis = %+v, want x=1 y=0", a)
	}
}

func TestGradientAxisVerticalSnaps(t *testing.T) {
	t.Parallel()
	a := gradientAxisForDegrees(90)
	if a.x != 0 || a.y != 1 {
		t.Fatalf("axis = %+v, want x=0 y=1", a)
	}
}

func TestSampleStopsPinsEndpoints(t *testing.T) {
	t.Parallel()
	stops := []gradientStop{
		{at: 0, c: Color{R: 0, A: 255}},
		{at: 1, c: Color{R: 255, A: 255}},
	}
	if got := sampleStops(stops, 0); got.R != 0 {
		t.Fatalf("t=0: R=%d, want 0", got.R)
	}
	if got := sampleStops(stops, 1); got.R != 255 {
		t.Fatalf("t=1: R=%d, want 255", got.R)
	}
	mid := sampleStops(stops, 0.5)
	if mid.R < 120 || mid.R > 135 {
		t.Fatalf("t=0.5: R=%d, want ~127", mid.R)
	}
}
```

**Step 2: Run it and confirm it fails**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run 'TestGradientAxis|TestSampleStops'
```

Expected: FAIL, undefined `gradientAxisForDegrees`.

**Step 3: Minimal implementation**

Port the PR's `gradientAxisForDegrees` (remainder, snap 1e-6, L1-normalise, bias = min(0,x)+min(0,y)). `sampleStops` lerps neighbouring stops; `t` is not clamped, first/last colours pin.

**Step 4: Tests pass**

Same command. Expected: PASS.

**Step 5: Claim and commit**

```bash
bd update sysc-218 --status in_progress
# after pass:
bd close sysc-218 --reason "axis and stop lerp covered"
git add internal/render/gradient.go internal/render/gradient_test.go
git commit -m "$(cat <<'EOF'
feat(render): sample a linear gradient along an angle

EOF
)"
```

---

### Task 2: Rect fill and mask blend (`sysc-219`)

**Files:**
- Modify: `internal/render/gradient.go`
- Modify: `internal/render/gradient_test.go`
- Modify: `internal/render/canvas.go` only if a helper must be shared (`blendPixel` stays unexported; call it from the same package)

**Step 1: Failing tests**

A 4×1 canvas, two stops black→white, angle 0, offset 0: x=0 is dark, x=3 is light.

A 4×1 `image.Alpha` full coverage, same ramp through `blendMaskGradient`: same ends.

A zero-coverage mask leaves the canvas untouched.

**Step 2: Run**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run 'TestFillRectGradient|TestBlendMaskGradient'
```

**Step 3: Implement** `fillRectGradient` and `blendMaskGradient`. Reuse `clip` and `blendPixel`. `uv` is the fragment in 0…1 across the destination box. `t = uv.x*axis.x + uv.y*axis.y + axis.bias - offset`.

**Step 4: Pass. Commit.**

```bash
bd close sysc-219 --reason "rect and mask sample the ramp"
git commit -m "$(cat <<'EOF'
feat(render): fill rects and masks with a sampled ramp

EOF
)"
```

---

### Task 3: `GradientPaint` on the node (`sysc-220`)

**Files:**
- Modify: `internal/ui/tree.go`
- Modify: `internal/render/paint.go` (`paintWordmark`, and any `fillRect` of a node that already has `n.Gradient.Count > 0` — wordmark is required; a separator with a spec is enough to prove the rect path)
- Modify: `internal/render/paint_test.go` or `internal/render/wordmark_test.go`

**Step 1: Types in `internal/ui/tree.go`**

```go
type PaintRole uint8

const (
	PaintUnset PaintRole = iota
	PaintPrimary
	PaintOnSurfaceVariant
	PaintOnSurface
	PaintSurface
)

type GradientMotion uint8

const (
	GradientNone GradientMotion = iota
	GradientLoop
	GradientPingPong
)

type GradientStop struct {
	At   float64
	Role PaintRole
}

type GradientPaint struct {
	Stops    [4]GradientStop
	Count    int // 0 = solid path; else 2–4
	AngleDeg float64
	Motion   GradientMotion
	From, To float64
}
```

Add `Gradient GradientPaint` and `GradientOffset float64` to `Node`.

**Step 2: Failing test** — paint a `KindWordmark` with the D3 recipe at offset 0 and at offset 0.45; the two rasters must differ. Invalid `Count == 1` falls back to solid accent.

**Step 3: `paintWordmark`** resolves roles through `Style` (`Accent`, `Track`, `Foreground`, `Background`) and calls `blendMaskGradient`. Solid fallback is `style.accent()`.

**Step 4:**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/render -run 'TestPaintWordmarkGradient|TestWordmark'
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/ui -run 'TestKindCoverage'
```

`kindcoverage_test.go` must still compile; it does not need to set `Gradient`.

**Step 5: Commit**

```bash
bd close sysc-220 --reason "wordmark tints through the ramp"
git commit -m "$(cat <<'EOF'
feat(ui): attach a token ramp to a node and paint the mark with it

EOF
)"
```

---

### Task 4: Looping animator (`sysc-221`)

**Files:**
- Modify: `internal/shell/animation.go`
- Modify: `internal/shell/animation_test.go`

**Step 1: Failing tests** (fake clock, as the existing file does)

- `TargetLoop(key, animGradient, from, to, 2s, ping-pong)` → `Settled()` false at t=0, t=2s, t=4s.
- At t=1s (half trip) the value is near the midpoint.
- `reduced == true` → value is midpoint, `Settled()` true.
- Second `TargetLoop` with the same arguments does not reset phase.
- `Forget(key)` → `Settled()` true.

Do not route this through `Animated()` or `animHover`. New channel `animGradient`.

**Step 2:**

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'TestAnimatorLoop|TestAnimatorStopsRequestingFramesWhenSettled'
```

Stub `loginctl`/`systemctl` first if this package's TestMain does not.

**Step 3: Implement** `TargetLoop`. `animValue.at` wraps (loop) or ping-pongs. `settled` is false while `looping && !reduced`. Trip duration is the caller-supplied duration; the wordmark passes 2000ms later.

**Step 4: Existing animator tests still pass.** Commit.

```bash
bd close sysc-221 --reason "looping offset never settles until parked or forgotten"
git commit -m "$(cat <<'EOF'
feat(shell): loop a paint offset without rebuilding the tree

EOF
)"
```

---

### Task 5: Bar clock, widget, default layout (`sysc-222`)

**Files:**
- Modify: `internal/shell/bar.go` — `NewWithTheme` constructs `anim` and `stopAnim`; `renderViewLocked` writes `GradientOffset`; start/stop the frame loop; `DropHost` must stop it (Registry)
- Modify: `internal/shell/registry.go` — `DropHost` stops the bar ticker
- Modify: `internal/shell/widget.go` — `wordmark` case; `capsuled` skips `KindWordmark`; clock `MinWidthText` when the section has a mark and two clocks
- Modify: `internal/config/config.go` — `"wordmark"` in `knownItems`; `Default` Center = time, wordmark, date
- Modify: `internal/config/config_test.go` — Default Center length 3, middle is wordmark, clocks still differ
- Modify: `internal/shell/widget.go` tests or `internal/shell/bar_test.go`

Wordmark node:

```go
h := 19 // same as launcherMarkHeight
n := &ui.Node{
	Kind: ui.KindWordmark, Key: "wordmark",
	ImageH: h, ImageW: render.WordmarkWidth(h),
	Gradient: wordmarkGradient(), // D3 recipe
}
out = append(out, textWidget{node: n, refresh: func(barView) bool { return false }})
```

`applyLocked` must not dereference a nil `inner`.

After layout / on each frame tick: if `n.Gradient.Motion != 0`, `anim.TargetLoop(n.StableKey(), animGradient, n.Gradient.From, n.Gradient.To, gradientTrip, n.Gradient.Motion)` then `n.GradientOffset = anim.Value(...)`. Reduced motion: write midpoint, do not start the ticker.

`startBarFrames` mirrors `startSurfaceFrames`: one goroutine, `animateSurface(b.stopAnim, b.anim.Settled, b.invalidate)`.

**Tests:**

- Parse `{"id":"wordmark"}` succeeds; `format` on it fails.
- Default Center is clock, wordmark, clock.
- Built wordmark has no capsule parent, `ImageH == 19`, `Gradient.Count == 3`.
- A section with mark + two clocks sets `MinWidthText` to `"Wed 30 Sep"` on those clocks.
- `NewWithTheme` leaves `anim != nil`.
- Looping mark: `Settled()` false; after `Forget("wordmark")` or reduced motion, true.
- `DropHost` does not leak the ticker (stop channel closed).

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/config -run 'TestDefault|TestParse'
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/shell -run 'TestWordmark|TestBarGradient|TestDropHost'
```

Commit:

```bash
bd close sysc-222 --reason "default bar centres a looping wordmark"
git commit -m "$(cat <<'EOF'
feat(shell): put a looping SYSC mark in the default bar centre

EOF
)"
```

---

### Task 6: Architecture exception (`sysc-223`)

**Files:**
- Modify: `docs/plans/2026-08-26-sysc-shell-design.md` Constraints bullet (the sentence that currently ends “A continuous frame loop is out of scope.”)
- Modify: `docs/roadmap.md` Milestone 2 idle-gate bullet
- Modify: `tests/integration/README.md` idle rows 13 and 29

Replacement sense (keep one sentence, do not rewrite the milestone):

> A continuous frame loop is out of scope except for a visible looping GradientPaint while reduced motion is off.

Idle gate: still no loop when reduced motion is on, or when the layout has no looping paint.

Do not edit old handovers.

**Step 1:** Make the three edits.

**Step 2:** No code test. `git diff` the three files; confirm no other “continuous frame loop” product rule was left claiming zero exceptions (handovers stay historical).

**Step 3: Commit and close the epic**

```bash
bd close sysc-223 --reason "named the idle-frame exception"
bd close sysc-217 --reason "design executed; live Niri owner-deferred"
git add docs/plans/2026-08-26-sysc-shell-design.md docs/roadmap.md tests/integration/README.md
git commit -m "$(cat <<'EOF'
docs: allow a looping gradient paint as the idle-frame exception

EOF
)"
```

Owner-deferred live Niri (not a merge gate): scale 1 and one other scale; reduced-motion toggle parks the crest; the mark sits on the midpoint between the two clock capsules.

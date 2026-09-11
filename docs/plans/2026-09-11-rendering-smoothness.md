# Rendering smoothness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop repainting whole surfaces for small changes — add rectangle damage, cap animation repaint frequency, and end the wallpaper picker's per-thumbnail rebuild — and record the measurements that retire Milestone 8.

**Architecture:** Damage travels back from the painter through a new `Damaged` sibling callback on `HostCallbacks`, leaving `Render`'s signature and every existing implementor untouched; an empty result means "whole buffer", which is what every surface reports until it opts in. Animation frames keep the 16 ms ticker but publish no more often than a theme-resolved cap, always publishing the settling frame. The wallpaper picker stops rebuilding its tree when only a raster arrived.

**Tech Stack:** Go 1.26.4, `wl_shm`, `wl_surface.damage_buffer`, no new dependencies.

**Spec:** `docs/plans/2026-09-11-rendering-smoothness-design.md`

## Global Constraints

- **Never run `go test ./...` or any `-race` build.** A repo-wide race build exhausts memory on this machine. Run named tests in one package: `go test ./internal/render -run TestDamage`.
- `go test ./internal/shell` is safe to run directly. `runArgvDefault` (`popout_session.go:270`) refuses under `testing.Testing()`.
- Go only. No CGO, no new module.
- Wayland types stay inside `internal/platform/wayland`.
- One Wayland dispatch goroutine. One animator per surface, and **no frames after settlement** — this plan reduces frame requests and must never add one.
- Damage is an optimisation, never a correctness boundary. Any surface that cannot prove what changed damages everything.
- **The `commit-msg` hook rejects these substrings, case-insensitively:** `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`, `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`, `bottom` contains `bot`. Screen every message:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  No `Co-Authored-By` trailer. Never `--no-verify`.
- Every commit also carries `.beads/issues.jsonl`, staged by the repository's own `pre-commit` hook. That is intended.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/platform/wayland/host.go` | `HostCallbacks.Damaged`, the new seam |
| `internal/platform/wayland/client.go:796` | `renderJob` submits per-rectangle damage, falling back to full |
| `internal/render/damage.go` | `DamageSet`: accumulate old and new bounds, coalesce, reset. New, pure |
| `internal/shell/animation.go` | Pacing cap in `animateSurface` |
| `internal/theme/profile.go` | `MotionTokens.FrameCap` |
| `internal/shell/popout_wallpaper.go:1044` | `applyWallpaperThumb` stops rebuilding the tree |
| `docs/plans/2026-08-26-sysc-shell-design.md` | Rendering section, open gate |
| `docs/roadmap.md` | Milestone 8 |

---

### Task 1: A damage accumulator

Pure, no Wayland. Written first so the geometry is proven before anything submits it.

**Files:**
- Create: `internal/render/damage.go`
- Test: `internal/render/damage_test.go`

**Interfaces:**
- Consumes: `ui.Rect`.
- Produces: `type DamageSet struct{...}` with `Add(r ui.Rect)`, `Moved(old, new ui.Rect)`, `Rects() []ui.Rect`, `Reset()`, `Full()`.

- [ ] **Step 1: Write the failing tests**

Create `internal/render/damage_test.go`:

```go
package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestDamageMovedCoversOldAndNewBounds(t *testing.T) {
	t.Parallel()
	// A node that moved must repair the region it left. Damaging only the new
	// bounds leaves the old pixels on screen, which reads as corrupted state
	// rather than as a paint fault.
	var d DamageSet
	d.Moved(ui.Rect{X: 0, Y: 0, W: 10, H: 10}, ui.Rect{X: 50, Y: 0, W: 10, H: 10})

	rects := d.Rects()
	if len(rects) == 0 {
		t.Fatal("no damage recorded")
	}
	covers := func(x, y int) bool {
		for _, r := range rects {
			if r.Contains(x, y) {
				return true
			}
		}
		return false
	}
	if !covers(5, 5) {
		t.Error("the vacated region is not damaged")
	}
	if !covers(55, 5) {
		t.Error("the new region is not damaged")
	}
}

func TestDamageEmptyMeansWholeBuffer(t *testing.T) {
	t.Parallel()
	// The fallback is the default. A surface that tracked nothing must get
	// today's behaviour, not zero damage.
	var d DamageSet
	if len(d.Rects()) != 0 {
		t.Error("a fresh set should report no rectangles")
	}
	if !d.Full() {
		t.Error("a set with no rectangles must report Full so the caller damages everything")
	}
}

func TestDamageMarkingFullDiscardsRectangles(t *testing.T) {
	t.Parallel()
	var d DamageSet
	d.Add(ui.Rect{X: 1, Y: 1, W: 2, H: 2})
	d.MarkFull()
	if !d.Full() {
		t.Error("MarkFull did not take")
	}
	if len(d.Rects()) != 0 {
		t.Error("a full set must not also report rectangles; the caller would damage twice")
	}
}

func TestDamageIgnoresEmptyRects(t *testing.T) {
	t.Parallel()
	var d DamageSet
	for _, r := range []ui.Rect{{}, {W: 0, H: 5}, {W: 5, H: 0}, {W: -1, H: -1}} {
		d.Add(r)
	}
	if len(d.Rects()) != 0 {
		t.Errorf("degenerate rects were recorded: %v", d.Rects())
	}
}

func TestDamageResetClears(t *testing.T) {
	t.Parallel()
	var d DamageSet
	d.Add(ui.Rect{W: 4, H: 4})
	d.MarkFull()
	d.Reset()
	if d.Full() || len(d.Rects()) != 0 {
		t.Error("Reset left state behind; the next frame would over-damage")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `go test ./internal/render -run TestDamage -v`
Expected: FAIL — `undefined: DamageSet`.

- [ ] **Step 3: Implement it**

Create `internal/render/damage.go`:

```go
package render

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// DamageSet accumulates the regions one frame changed.
//
// The zero value is "nothing tracked", which reports Full: a surface that does
// not know what changed must damage everything. Damage is an optimisation and
// never a correctness boundary, so every uncertain path ends in Full rather
// than in a smaller rectangle.
type DamageSet struct {
	rects []ui.Rect
	full  bool
}

// Add records one changed region. Degenerate rectangles are dropped rather
// than submitted: a zero-area damage is a protocol call that repairs nothing.
func (d *DamageSet) Add(r ui.Rect) {
	if d.full || r.W <= 0 || r.H <= 0 {
		return
	}
	d.rects = append(d.rects, r)
}

// Moved records a node that changed position or size. Both bounds are damaged,
// because the pixels the node vacated need repainting as much as the ones it
// now covers.
func (d *DamageSet) Moved(old, new ui.Rect) {
	d.Add(old)
	d.Add(new)
}

// MarkFull abandons rectangle tracking for this frame.
func (d *DamageSet) MarkFull() {
	d.full = true
	d.rects = d.rects[:0]
}

// Full reports whether the caller should damage the whole buffer. A set with
// no rectangles is Full, so "tracked nothing" and "changed nothing" both take
// the safe path.
func (d *DamageSet) Full() bool { return d.full || len(d.rects) == 0 }

// Rects returns the accumulated regions, empty when Full.
func (d *DamageSet) Rects() []ui.Rect {
	if d.full {
		return nil
	}
	return d.rects
}

// Reset clears the set for the next frame.
func (d *DamageSet) Reset() {
	d.rects = d.rects[:0]
	d.full = false
}
```

- [ ] **Step 4: Run and watch them pass**

Run: `go test ./internal/render -run TestDamage -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/render/damage.go internal/render/damage_test.go
git commit -m "feat(render): add a damage accumulator"
```

---

### Task 2: Submit per-rectangle damage

**Files:**
- Modify: `internal/platform/wayland/host.go:39-63` (`HostCallbacks`)
- Modify: `internal/platform/wayland/client.go:796-812` (`renderJob`)
- Test: `internal/platform/wayland/damage_test.go`

**Interfaces:**
- Consumes: Task 1's `[]ui.Rect`.
- Produces: `HostCallbacks.Damaged func() []ui.Rect`.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/wayland/damage_test.go`:

```go
package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestDamageRectsFallBackToFullBuffer(t *testing.T) {
	t.Parallel()
	// A nil callback is every surface that has not opted in. It must produce
	// exactly one full-buffer rectangle, which is today's behaviour.
	got := damageRects(nil, 800, 600)
	want := []ui.Rect{{X: 0, Y: 0, W: 800, H: 600}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("nil callback gave %v, want %v", got, want)
	}
}

func TestDamageRectsEmptySliceIsAlsoFull(t *testing.T) {
	t.Parallel()
	// "I tracked nothing" and "nothing changed" must both repaint everything.
	got := damageRects(func() []ui.Rect { return nil }, 800, 600)
	if len(got) != 1 || got[0].W != 800 || got[0].H != 600 {
		t.Fatalf("empty result gave %v, want one full rect", got)
	}
}

func TestDamageRectsClipToTheBuffer(t *testing.T) {
	t.Parallel()
	// A rectangle outside the buffer is a protocol error waiting to happen.
	got := damageRects(func() []ui.Rect {
		return []ui.Rect{{X: -10, Y: -10, W: 40, H: 40}, {X: 790, Y: 590, W: 100, H: 100}}
	}, 800, 600)
	for _, r := range got {
		if r.X < 0 || r.Y < 0 || r.X+r.W > 800 || r.Y+r.H > 600 {
			t.Errorf("rect %+v escapes the 800x600 buffer", r)
		}
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("rect %+v is degenerate after clipping", r)
		}
	}
}

func TestDamageRectsDropsFullyOutsideRects(t *testing.T) {
	t.Parallel()
	got := damageRects(func() []ui.Rect {
		return []ui.Rect{{X: 2000, Y: 2000, W: 10, H: 10}}
	}, 800, 600)
	if len(got) != 0 {
		t.Errorf("a rect entirely outside the buffer produced %v", got)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/platform/wayland -run TestDamageRects -v`
Expected: FAIL — `undefined: damageRects`.

- [ ] **Step 3: Add the callback and the helper**

In `internal/platform/wayland/host.go`, after `Render`:

```go
	// Damaged reports the buffer rectangles the last Render touched, in buffer
	// pixels. Nil, or an empty result, means the whole buffer — which is what
	// every surface reports until it tracks its own regions. Damage is an
	// optimisation; the fallback is always correct.
	Damaged func() []ui.Rect
```

Add to `client.go` beside `renderJob`:

```go
// damageRects resolves a surface's reported damage into buffer rectangles.
//
// A nil callback or an empty result means the whole buffer. Every rectangle is
// clipped to the buffer, and one that falls entirely outside is dropped: a
// damage call that names pixels the buffer does not have is a protocol error,
// and silently over-damaging is the safe direction to err.
func damageRects(reported func() []ui.Rect, width, height int32) []ui.Rect {
	full := []ui.Rect{{X: 0, Y: 0, W: int(width), H: int(height)}}
	if reported == nil {
		return full
	}
	raw := reported()
	if len(raw) == 0 {
		return full
	}
	out := make([]ui.Rect, 0, len(raw))
	for _, r := range raw {
		if r.X < 0 {
			r.W += r.X
			r.X = 0
		}
		if r.Y < 0 {
			r.H += r.Y
			r.Y = 0
		}
		if r.X+r.W > int(width) {
			r.W = int(width) - r.X
		}
		if r.Y+r.H > int(height) {
			r.H = int(height) - r.Y
		}
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		out = append(out, r)
	}
	return out
}
```

- [ ] **Step 4: Use it in `renderJob`**

Replace the single damage call at `client.go:808`:

```go
	for _, r := range damageRects(u.app.Damaged, gen.width, gen.height) {
		if err := u.surface.DamageBuffer(int32(r.X), int32(r.Y), int32(r.W), int32(r.H)); err != nil {
			return fmt.Errorf("wayland: damage: %w", err)
		}
	}
```

Leave `tooltip.go:243` alone. Tooltips are small and short-lived; adding a second consumer before the first is proven contradicts the design's one-surface-at-a-time rule.

- [ ] **Step 5: Run the tests and build**

Run: `go build ./internal/platform/wayland && go test ./internal/platform/wayland -run TestDamage -v`
Expected: build succeeds, PASS.

- [ ] **Step 6: Verify nothing regressed visually**

Every surface still reports nil, so every surface still damages fully. Run the existing package tests:

Run: `go test ./internal/platform/wayland`
Expected: PASS, unchanged.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/wayland/
git commit -m "feat(wayland): submit per-rectangle damage with a full-buffer fallback"
```

---

### Task 3: Cap animation repaint frequency

**Files:**
- Modify: `internal/theme/profile.go` (`MotionTokens`)
- Modify: `internal/shell/animation.go:315-331` (`animateSurface`)
- Test: `internal/shell/animation_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `MotionTokens.FrameCap time.Duration`; `animateSurface(stop <-chan struct{}, settled func() bool, publish func(), minInterval time.Duration)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/shell/animation_test.go`:

```go
func TestAnimateSurfacePacesPublishes(t *testing.T) {
	t.Parallel()
	// The ticker runs at 16 ms. With a 50 ms cap, a run of unsettled frames
	// must publish far fewer times than it ticks.
	stop := make(chan struct{})
	var published int
	done := make(chan struct{})
	go func() {
		animateSurface(stop, func() bool { return false }, func() { published++ }, 50*time.Millisecond)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	close(stop)
	<-done

	if published == 0 {
		t.Fatal("pacing suppressed every frame; the animation would never paint")
	}
	if published > 8 {
		t.Errorf("published %d times in 200ms with a 50ms cap; pacing is not applied", published)
	}
}

func TestAnimateSurfaceAlwaysPublishesTheSettlingFrame(t *testing.T) {
	t.Parallel()
	// The last frame must paint even if it lands inside the cap window, or a
	// settled value is left unpainted and the surface keeps a stale pixel.
	stop := make(chan struct{})
	defer close(stop)
	var published int
	done := make(chan struct{})
	go func() {
		animateSurface(stop, func() bool { return true }, func() { published++ }, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("animateSurface did not return after settling")
	}
	if published != 1 {
		t.Errorf("published %d times, want exactly 1 settling frame despite the hour-long cap", published)
	}
}

func TestAnimateSurfaceStopsOnStop(t *testing.T) {
	t.Parallel()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		animateSurface(stop, func() bool { return false }, func() {}, time.Millisecond)
		close(done)
	}()
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("animateSurface ignored stop")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/shell -run TestAnimateSurface -v`
Expected: FAIL — too many arguments to `animateSurface`.

- [ ] **Step 3: Add the token**

In `internal/theme/profile.go`, add to `MotionTokens`:

```go
	// FrameCap is the shortest interval between repaints of an animating
	// surface. The ticker still runs at the frame cadence; this bounds how
	// often the surface is actually blitted, which is the expensive half. It
	// must stay below the shortest duration token, or a short transition
	// becomes visibly steppy.
	FrameCap time.Duration
```

Set `FrameCap: 33 * time.Millisecond` in `BaseMotion`, and scale it in `AtSpeed` exactly as the other durations are scaled.

- [ ] **Step 4: Apply the cap**

Replace `animateSurface` in `internal/shell/animation.go`:

```go
func animateSurface(stop <-chan struct{}, settled func() bool, publish func(), minInterval time.Duration) {
	tick := time.NewTicker(animTick)
	defer tick.Stop()
	var last time.Time
	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			done := settled()
			// A skipped frame still advances the animation: values are
			// computed from the clock, not from how many times the surface
			// was published. Pacing changes how often we blit, never where
			// the animation gets to.
			//
			// The settling frame always publishes, even inside the cap
			// window, or a settled value is left unpainted.
			if done || last.IsZero() || now.Sub(last) >= minInterval {
				publish()
				last = now
			}
			if done {
				return
			}
		}
	}
}
```

Update every caller to pass the resolved `FrameCap`. Find them with:

```bash
grep -rn "animateSurface(" internal/shell/*.go | grep -v _test
```

- [ ] **Step 5: Run and watch it pass**

Run: `go test ./internal/shell -run 'TestAnimateSurface|TestAnim' -v`
Expected: PASS.

- [ ] **Step 6: Confirm reduced motion still schedules nothing**

Reduced motion settles immediately, so `settled()` returns true on the first tick and the loop returns after one publish. Run the existing reduced-motion tests:

Run: `go test ./internal/shell -run 'TestReduced|TestMotion' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/theme/profile.go internal/shell/animation.go internal/shell/animation_test.go
git commit -m "feat(shell): cap how often an animating surface repaints"
```

---

### Task 4: Stop rebuilding the picker for a raster

**Files:**
- Modify: `internal/shell/popout_wallpaper.go:1044` (`applyWallpaperThumb`)
- Test: `internal/shell/popout_wallpaper_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: no signature change.

- [ ] **Step 1: Write the failing test**

```go
func TestThumbArrivalDoesNotRebuildTheTree(t *testing.T) {
	t.Parallel()
	// The virtual list's Item builder looks the raster up at layout time
	// (wallpaperThumbFor), so a decoded thumbnail needs the surface repainted,
	// not the tree rebuilt. On a 980x1100 picker a rebuild plus a full repaint
	// is roughly 40 ms of blit per thumbnail, paid once per file in a library
	// of hundreds.
	reg, h := openWallpaperPanel(t)
	before := h.root

	reg.applyWallpaperThumb(icons.Key{}, &ui.Image{Width: 2, Height: 2, Stride: 8, Pix: make([]byte, 16)})

	if h.root != before {
		t.Error("the tree was rebuilt for a raster arrival")
	}
}
```

Use the package's existing wallpaper-panel fixture rather than inventing `openWallpaperPanel`; read `popout_wallpaper_test.go` and use its helper.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/shell -run TestThumbArrival -v`
Expected: FAIL — the tree pointer changed.

- [ ] **Step 3: Publish without rebuilding**

In `applyWallpaperThumb`, drop the `r.rebuildPanel(h)` call and keep the publish:

```go
func (r *Registry) applyWallpaperThumb(_ icons.Key, image *ui.Image) {
	if image == nil {
		return
	}
	r.mu.Lock()
	h := r.panelHosts[PanelWallpaper]
	if h == nil {
		r.mu.Unlock()
		return
	}
	out := h.output
	r.mu.Unlock()
	// No rebuild: wallpaperThumbFor runs inside the virtual list's Item
	// builder at layout time, so the next paint picks the raster up. Rebuilding
	// the tree for a raster relaid out the whole picker once per decoded file.
	r.publishSurface(out, panelSurfaceID(PanelWallpaper))
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test ./internal/shell -run 'TestThumb|TestWallpaper' -v`
Expected: PASS.

If a wallpaper test fails because a tile's size depends on whether its raster exists, that is real: the tile would then need a rebuild to resize. In that case keep the rebuild but gate it on the tile's measured size actually changing, and record the finding in the commit body.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_wallpaper.go internal/shell/popout_wallpaper_test.go
git commit -m "perf(shell): repaint rather than relayout when a thumbnail decodes"
```

---

### Task 5: Coalesce thumbnail arrivals

**Files:**
- Modify: `internal/shell/popout_wallpaper.go`
- Test: `internal/shell/popout_wallpaper_test.go`

**Interfaces:**
- Consumes: Task 4's publish-only path.
- Produces: no exported change.

- [ ] **Step 1: Write the failing test**

```go
func TestThumbArrivalsCoalesce(t *testing.T) {
	t.Parallel()
	// Thumbnails arrive in bursts from a paced worker. Ten arrivals inside one
	// window are one repaint, not ten.
	reg, h := openWallpaperPanel(t)
	_ = h
	var published int
	reg.publishHook = func() { published++ }

	img := &ui.Image{Width: 2, Height: 2, Stride: 8, Pix: make([]byte, 16)}
	for i := 0; i < 10; i++ {
		reg.applyWallpaperThumb(icons.Key{}, img)
	}
	if published > 2 {
		t.Errorf("ten arrivals produced %d publishes; they are not coalescing", published)
	}
	if published == 0 {
		t.Error("no publish at all; the picker would never show a thumbnail")
	}
}
```

`publishHook` is an unexported test seam on `Registry`, nil in production, called from `publishSurface`. A nil check costs one comparison.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/shell -run TestThumbArrivalsCoalesce -v`
Expected: FAIL — ten publishes.

- [ ] **Step 3: Gate on elapsed time**

Add a `lastThumbPublish time.Time` field guarded by `r.mu`, and publish only when the window has elapsed, marking a pending arrival otherwise. The pending flag is flushed by the next arrival that clears the window, so no timer is introduced — which matters, because a timer here would be a recurring frame source and the architecture forbids one.

```go
	const thumbWindow = 100 * time.Millisecond
	now := time.Now()
	if !r.lastThumbPublish.IsZero() && now.Sub(r.lastThumbPublish) < thumbWindow {
		r.thumbPending = true
		r.mu.Unlock()
		return
	}
	r.lastThumbPublish = now
	r.thumbPending = false
```

The final thumbnail in a burst could be left pending. The worker's `Progress()` channel already signals completion; flush any pending publish there so the last arrival is never dropped.

- [ ] **Step 4: Run and watch it pass**

Run: `go test ./internal/shell -run 'TestThumb|TestWallpaper' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_wallpaper.go internal/shell/popout_wallpaper_test.go
git commit -m "perf(shell): coalesce thumbnail repaints into one window"
```

---

### Task 6: Amend the architecture and close Milestone 8

**Files:**
- Modify: `docs/plans/2026-08-26-sysc-shell-design.md` (L172-196, L372)
- Modify: `docs/roadmap.md` (Milestone 8)
- Modify: `.beads/issues.jsonl` via `bd`

- [ ] **Step 1: Measure before amending**

Record, on this machine:

1. Wallpaper picker: wall time from opening the picker to a settled grid, before and after Tasks 4 and 5. The design's arithmetic predicts roughly 40 ms of blit per thumbnail beforehand.
2. Animation: repaints per second for an open panel with a running transition, before and after Task 3. Expect roughly 60 to fall to roughly 30 at the 33 ms cap.
3. A 60-minute idle run showing no continuous redraw.

An amendment that cites no numbers is the thing `sysc-202` explicitly warns against: "Needs measurements, not a design-first feature epic."

- [ ] **Step 2: Amend the rendering section**

At L193, record that rectangle damage exists, that `HostCallbacks.Damaged` carries it, that a nil or empty result means the whole buffer, and that full-buffer damage remains both the default and the fallback. State which surfaces opt in — at the end of this plan, none do, because Task 2 only builds the path.

- [ ] **Step 3: Close the open gate**

At L372, "Measure shared-memory rendering before deciding whether to add EGL/OpenGL ES" is satisfied. Together with the blur design, every case `sysc-202` names now has a measurement: animation frame time (Task 3), large blurred panels (the blur slice), image-heavy grids (Tasks 4 and 5), and CPU/power (the idle run). Cite both designs.

- [ ] **Step 4: Retire the epic**

```bash
cd /home/nomadx/sysc-shell
bd close sysc-202 --reason "Every named case measured on wl_shm; no case misses the frame budget, so EGL/OpenGL ES is not justified"
bd export -o .beads/issues.jsonl
wc -l .beads/issues.jsonl
git diff --stat .beads/issues.jsonl
```

`bd export` overwrites the tracked JSONL with only what it thinks changed. Check `wc -l` and the diff before committing; if rows vanished, recover with `sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl`.

- [ ] **Step 5: Commit**

```bash
git add docs/ .beads/issues.jsonl
git commit -m "docs: record the measured rendering outcome and retire the qualification epic"
```

---

## Self-Review

**Spec coverage.** D1 damage through a sibling callback → Task 2. D2 dirty geometry owned by the tree, `Scheduler.dirty` untouched → Task 1 builds the accumulator; `Scheduler` is not modified anywhere in this plan. D3 pacing cap, theme-resolved → Task 3. D4 coalesce and stop rebuilding → Tasks 4 and 5, staged in the order the design permits. D5 damage as an optimisation with a full fallback → Tasks 1 and 2 both default to Full, and Task 2 Step 6 verifies no surface changed behaviour. D6 amendment → Task 6. D7 testing → every task. D8 tracker → Task 6 Step 4. D9 risks: stale pixels are contained because no surface opts in within this plan; the blur interaction is untouched since no panel reports rectangles; the `wallpaperThumbFor` hazard comment is preserved by Task 4's edit, which removes a call rather than rewriting the function; the pacing cap is asserted to sit below the shortest token in Task 3 Step 3's comment.

**Placeholders.** None. Tasks 4 and 5 say "use the existing fixture" — an instruction to read a named neighbouring file, not a deferred decision. Task 4 Step 4 names a specific, checkable contingency rather than "handle edge cases".

**Type consistency.** `DamageSet` methods in Task 1 are used nowhere else yet, by design — Task 2 consumes `[]ui.Rect`, which `Rects()` returns. `damageRects(reported func() []ui.Rect, width, height int32) []ui.Rect` in Task 2 matches both its test and its `renderJob` call site, where `gen.width`/`gen.height` are already `int32`. `animateSurface`'s fourth parameter is `time.Duration` in the signature, the tests, and the `FrameCap` token.

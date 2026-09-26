# Micro-interaction Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every clickable on the non-bar surfaces the references' shared feel: a state layer
governed by a per-surface policy, a press-point ripple, a radius morph derived from press progress,
and panels that shift clear of panels already open.

**Architecture:** `internal/ui` gains pure value primitives only: three kinds join `Animated`'s
action-gated set, two render-copy carriers beside `TextOffset`, and the morph/ripple geometry.
`internal/shell` gains the per-surface state-layer policy on `interaction`, the `animRipple` channel
with press-origin plumbing, and the pure `panelPlacement`. `internal/render` paints the state
overlay, the masked ripple disc, and the morphed radius.

**Tech Stack:** Go 1.26, standard library only. No new module; `sysc-wayland` stays at v0.2.2 (the
plugin protocol is untouched).

**Spec:** `docs/plans/2026-09-27-microinteraction-primitives-design.md` (D1–D7). Tracked as
`sysc-590`. D6 (springs) and D7 (elevation) are deferred by the design and have no task here.

## Design fidelity notes

Deviations are stated here with evidence rather than made silently.

- **D4, "the painter reads it like it reads `pressScale`":** nothing reads `pressScale` —
  `animator.PressScale` (internal/shell/animation.go:522) has no caller outside
  animation_test.go:260, and internal/render references no press scale (paint.go:1046, :1192 only).
  The animator is package-private to shell and render cannot import shell, so the morph travels as a
  resolved value on the render copy — the house carrier for host-resolved paint values
  (`TextOffset` internal/ui/tree.go:285, `EffectPhase` tree.go:324). One field, `PressProgress`,
  written only by the resolver onto the copy; the retained tree never sets it. D4's substance is
  kept: derived from `animPress`, no new channel, target `max(radius/2, 1)`.
- **D4, "No new field":** honoured for the model tree; the render-copy carrier above is the one
  addition, the same category as `TextOffset`. The zero-field alternative (pre-resolving the radius
  into the copy's `Radius`) needs render's radius resolution exported plus a second resolve walk.
- **D2, "catalogue default (200 ms)":** the table's `Short` is 150 ms (internal/theme/profile.go:383).
  The layer rides the existing `animHover`/`animPress` durations — the decision's operative half.
- **D1, "KindMenu rows":** rows are plain `KindText` children with no per-row state resolution
  (internal/shell/menu.go:236; painted directly at internal/render/paint.go:648). Task 4 resolves
  row hover in the panel's render path, and `Animated` gates `KindMenu` on `Action` like
  `KindCapsule` (internal/ui/animkey.go:19) so the keyless nil-menu placeholder (menu.go:225) can
  never trip `ValidateKeys`.
- **D3, ripple on toast cards:** the toast resolver applies state with a nil animator
  (internal/shell/notifyactions.go:76), so toasts get the state layer but no ripple; the gate
  captures the press-point ripple on the control centre. A toast frame loop is new scheduling
  machinery outside D3's "one more animator channel".
- **D5's signature** is implementable as written once `output` is read as the work area — the
  decision's own words are "clamps a panel inside the work area". Gap, padding and the bar zone
  stay at the call site where they live today (internal/shell/panelhost.go:652).

## Global Constraints

- Every `go` command runs under `GOMAXPROCS=4`.
- Tests run per package only: `GOMAXPROCS=4 go test -count=1 ./internal/<pkg>`. NEVER
  `go test -race ./...` — that has hard-locked this machine (documented). Race runs are per package.
- No new module dependency. `git diff --exit-code -- go.mod go.sum` stays clean. `sysc-wayland`
  stays pinned at v0.2.2; the plugin protocol is untouched.
- The bar furniture ruling (commit `62b7202`, "paint clickables at rest") holds: bar widgets get no
  hover wash and no ripple. The bar's policy is `stateLayer: false` and the bar never targets
  `animRipple`.
- Panel `configure`/`render`/`handle` take `Registry.mu`; relays run off the Wayland owner.
- No new node kinds (D1). Springs (D6) and elevation (D7) are out of this slice.
- Commit messages are plain technical English — the `commit-msg` hook (`~/.git-hooks/commit-msg`
  via `core.hooksPath`) rejects ordinary English containing `agent`, `cursor`, `codex`, `llm`,
  `both`, `Hallmark` and similar — with no attribution trailers.
- `gofmt -w` the touched packages before each commit, then `go vet` them.

## Review Focus

1. A second press while a ripple is in flight must restart the phase at the new origin — one ripple
   per node, never two discs. Pinned in Task 3.
2. The bar must gain no hover wash and no ripple; a regression reverses the `62b7202` ruling.
   Pinned in Task 2.
3. A stadium control (no `Radius`, no `Shape`) morphs from half its short side, never toward a 1 px
   rectangle: the morph reads the radius after `chromeRadius` resolves it, where the base is at
   least 1. Pinned in Task 4.
4. `panelPlacement` with nothing open must reproduce today's margins for every Align, AnchorX and
   bar edge. Pinned in Task 5.
5. Reduced motion: interaction state snaps with no fade and no ripple frames are requested.
   Pinned in Tasks 3 and 7.
6. Render scale 1.25 and 2.0: the ripple origin and disc stay inside the node's box. Pinned in
   Task 4.

---

### Task 1: UI value primitives — animated kinds, render-copy carriers, morph and ripple geometry

**Files:**
- Modify: `internal/ui/animkey.go:10-27` (`Animated`), `internal/ui/tree.go:141` (`Rect`), `internal/ui/tree.go:285` (beside `TextOffset`)
- Create: `internal/ui/micro.go`
- Test: `internal/ui/animkey_test.go:8-29`, `internal/ui/micro_test.go`

**Interfaces:**
- Produces: `ui.Animated` true for `KindTab`, `KindSlider`, `KindMenu` carrying an `Action`;
  `ui.Size`; `ui.RipplePaint{Phase float64; X, Y int}`; `ui.Node.PressProgress float64`;
  `ui.Node.Ripple RipplePaint`; `ui.MorphRadius(base int, progress float64) int`;
  `ui.RippleDisc(phase float64, box ui.Rect, originX, originY int) (radius, alpha float64)`.

- [ ] **Step 1: Write the failing tests**

In `internal/ui/animkey_test.go`, add to the `TestAnimatedTracksOnlyInteractiveChrome` table
(line 11):

```go
		{"tab with an action", &Node{Kind: KindTab, Action: "tab-one"}, true},
		{"tab without an action", &Node{Kind: KindTab, Text: "Tab"}, false},
		{"slider with an action", &Node{Kind: KindSlider, Action: "volume"}, true},
		{"menu with an action", &Node{Kind: KindMenu, Action: "set:theme"}, true},
		{"keyless menu placeholder", &Node{Kind: KindMenu, Role: "combobox"}, false},
```

`internal/ui/micro_test.go`:

```go
package ui

import (
	"math"
	"testing"
)

func TestMorphRadiusEasesTowardHalf(t *testing.T) {
	for _, tc := range []struct {
		base, p float64
		want    int
	}{
		{16, 0, 16}, {16, 1, 8}, {16, 0.5, 12}, {1, 1, 1}, {0, 1, 1}, {16, 2, 8}, {16, -1, 16},
	} {
		if got := MorphRadius(int(tc.base), tc.p); got != tc.want {
			t.Errorf("MorphRadius(%v, %v) = %d, want %d", tc.base, tc.p, got, tc.want)
		}
	}
}

func TestRippleDiscGrowsThenFades(t *testing.T) {
	box := Rect{W: 10, H: 10}
	farthest := math.Hypot(10, 10)
	for _, tc := range []struct {
		name       string
		phase      float64
		wantRadius float64
		wantAlpha  float64
	}{
		{"start", 0, 0, 1},
		{"mid-grow", 0.3125, 0.875 * farthest, 1},
		{"grow ends", 0.625, farthest, 1},
		{"mid-fade", 0.8125, farthest, 0.5},
		{"done", 1, farthest, 0},
		{"past the end clamps", 1.5, farthest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			radius, alpha := RippleDisc(tc.phase, box, 0, 0)
			if math.Abs(radius-tc.wantRadius) > 1e-9 || math.Abs(alpha-tc.wantAlpha) > 1e-9 {
				t.Fatalf("RippleDisc(%v) = %v, %v, want %v, %v",
					tc.phase, radius, alpha, tc.wantRadius, tc.wantAlpha)
			}
		})
	}
	if r, a := RippleDisc(0.5, box, 10, 10); r <= 0 || a != 1 {
		t.Fatalf("origin at a corner still grows: %v, %v", r, a)
	}
	if r, _ := RippleDisc(0.5, Rect{}, 0, 0); r != 0 {
		t.Fatalf("empty box radius = %v, want 0", r)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestAnimated|TestMorphRadius|TestRippleDisc' ./internal/ui`
Expected: FAIL to compile, `MorphRadius` and `RippleDisc` undefined; the new `Animated` cases fail.

- [ ] **Step 3: Implement**

`internal/ui/animkey.go`, in `Animated`'s switch after the `KindCapsule` case (line 19):

```go
	case KindTab, KindSlider, KindMenu:
		// The capsule rule: these animate when they carry an action. A keyless
		// one is a label that resolves no state and must never trip ValidateKeys.
		return n.Action != ""
```

`internal/ui/tree.go`, after `Rect` (line 141):

```go
// Size is a logical-pixel width and height.
type Size struct{ W, H int }

// RipplePaint carries one node's resolved press ripple from the shell's
// animator to the painter. Phase is the shared ripple clock, zero through
// one; X and Y are the press origin in the node's logical space.
type RipplePaint struct {
	Phase float64
	X, Y  int
}
```

Beside `TextOffset` (line 285):

```go
	// PressProgress is the resolved press transition the surface host supplies
	// for this frame, zero through one. Like TextOffset it lives on the render
	// copy, never the retained tree.
	PressProgress float64
	// Ripple is the resolved press ripple the surface host supplies. A zero
	// phase paints nothing. Like TextOffset it lives on the render copy.
	Ripple RipplePaint
```

`internal/ui/micro.go`:

```go
package ui

import "math"

// MorphRadius eases a control's resolved corner radius toward half itself
// while it is pressed. base is the radius the painter already resolved —
// chromeRadius's answer, never the raw node field — so a stadium morphs from
// half its short side and the one-pixel floor only bites at the smallest
// controls.
func MorphRadius(base int, progress float64) int {
	return lerpInt(base, max(base/2, 1), clampProgress(progress))
}

// rippleGrowPhase is the grow share of a ripple's phase: 500 ms of the 800 ms
// one ripple runs, the recipe's own split.
const rippleGrowPhase = 0.625

// RippleDisc resolves one ripple frame over a node's box: the disc's radius
// in logical pixels, growing from the press origin with EaseOutCubic, and the
// disc's alpha, held through the grow and faded over the rest of the phase.
func RippleDisc(phase float64, box Rect, originX, originY int) (radius, alpha float64) {
	p := clampProgress(phase)
	radius = EaseOutCubic(clampProgress(p/rippleGrowPhase)) * farthestCorner(box, originX, originY)
	alpha = 1
	if p > rippleGrowPhase {
		alpha = 1 - (p-rippleGrowPhase)/(1-rippleGrowPhase)
	}
	return radius, clampProgress(alpha)
}

// farthestCorner is the distance from a point to the farthest corner of a
// rectangle: the radius a disc needs to cover the rectangle from that point.
func farthestCorner(box Rect, x, y int) float64 {
	if box.W <= 0 || box.H <= 0 {
		return 0
	}
	best := 0.0
	for _, c := range [4][2]int{
		{box.X, box.Y}, {box.X + box.W, box.Y},
		{box.X, box.Y + box.H}, {box.X + box.W, box.Y + box.H},
	} {
		best = max(best, math.Hypot(float64(c[0]-x), float64(c[1]-y)))
	}
	return best
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/ui`
Expected: PASS. `kindcoverage_test.go` enumerates kinds, not `Animated` results, so it needs no
change.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui && GOMAXPROCS=4 go vet ./internal/ui
git add internal/ui/animkey.go internal/ui/animkey_test.go internal/ui/tree.go internal/ui/micro.go internal/ui/micro_test.go
git commit -m "feat(ui): morph and ripple geometry with render-copy carriers"
```

---

### Task 2: The state layer becomes a per-surface policy

**Files:**
- Modify: `internal/shell/animation.go:634-637` (`interaction`), `:672-700` (`apply`)
- Modify: `internal/shell/bar.go:579-584` (drop `clearHoverLocked`), `internal/shell/baroverflow.go:55-66` (delete it)
- Modify: `internal/shell/panelhost.go` (host literal near line 677), `internal/shell/notifyactions.go:46-48`, `internal/shell/traydrawer.go:128`, `internal/shell/traymenuhost.go:107-108`
- Test: `internal/shell/animation_test.go`, `internal/shell/bar_test.go:553-577`

**Interfaces:**
- Produces: `interaction.stateLayer bool` — the surface's policy. The bar constructs it false;
  every panel surface constructs it true. `apply` resolves hover only under the policy, replacing
  the bar's post-apply `clearHoverLocked` patch.

- [ ] **Step 1: Write the failing tests**

In `internal/shell/animation_test.go`:

```go
func TestStateLayerPolicyGatesHoverOnly(t *testing.T) {
	t.Parallel()
	tree := func() *ui.Node {
		return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
			{Kind: ui.KindButton, Action: "b", Bounds: ui.Rect{W: 10, H: 10}},
		}}
	}
	for _, tc := range []struct {
		name  string
		pol   interaction
		want  ui.Interaction
		notWn ui.Interaction
	}{
		{"bar paints clickables at rest", interaction{stateLayer: false},
			ui.StatePressed, ui.StateHovered},
		{"panels tint hover and press", interaction{stateLayer: true},
			ui.StatePressed | ui.StateHovered, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tree()
			pol.hover, pol.press = "b", "b"
			pol.apply(root, nil)
			n := root.Children[0]
			if !n.State.Has(tc.want) || n.State.Has(tc.notWn) {
				t.Fatalf("state = %v, want %v without %v", n.State, tc.want, tc.notWn)
			}
		})
	}
}
```

In `internal/shell/bar_test.go`, extend `TestBarHoverInvalidatesOnlyOnTargetChange` after the
first `Handle` (line 561) — the render copy must carry no hover state:

```go
	var hovered bool
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		hovered = hovered || n.State.Has(ui.StateHovered)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(p.renderView())
	if hovered {
		t.Error("the bar resolved hover onto its render copy; the furniture ruling holds no more")
	}
```

(If the harness's render entry point has another name, use
`grep -n "func (p \*testBar) render" internal/shell/bar_test.go` to find it.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestStateLayerPolicy|TestBarHover' ./internal/shell`
Expected: FAIL. `interaction{stateLayer: false}` does not compile; the bar copy carries hover.

- [ ] **Step 3: Implement**

`internal/shell/animation.go`, the `interaction` struct (line 634):

```go
type interaction struct {
	hover string
	press string
	// stateLayer is the surface's policy: whether pointer-over tints a
	// control. The bar is furniture and paints its clickables at rest
	// (commit 62b7202); every panel surface tints. The switch lives here,
	// with the resolver, not on Node.
	stateLayer bool
}
```

In `apply` (line 680):

```go
			hovered := s.stateLayer && key != "" && key == s.hover
```

Construction sites — the bar keeps the zero value; every panel surface sets the policy:

- `internal/shell/panelhost.go`, the `PanelHost` literal in `spawnPanelLocked` (near line 677):
  add `pointer: interaction{stateLayer: true},`
- `internal/shell/notifyactions.go:47`:
  `return &notifyResolver{actions: a, pointer: interaction{stateLayer: true}}`
- `internal/shell/traydrawer.go`, in `newTrayDrawerHost` (line 128): `h.pointer = interaction{stateLayer: true}`
- `internal/shell/traymenuhost.go:108`: after `h := &trayMenuHost{r: r}`, add
  `h.pointer = interaction{stateLayer: true}`

`internal/shell/bar.go`, in `renderViewLocked` (lines 580-584): delete the `clearHoverLocked(root)`
call and reword the comment to say the bar's interaction is constructed with `stateLayer: false`,
so the resolver itself never resolves hover onto the bar. Delete `clearHoverLocked` and its doc
comment from `internal/shell/baroverflow.go:55-66`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS. The bar's hover invalidation test still passes — `setHover` still tracks the key
for hit testing; only the resolved state is gated.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/animation.go internal/shell/animation_test.go internal/shell/bar.go internal/shell/baroverflow.go internal/shell/bar_test.go internal/shell/panelhost.go internal/shell/notifyactions.go internal/shell/traydrawer.go internal/shell/traymenuhost.go
git commit -m "feat(shell): state layer as a per-surface resolver policy"
```

---

### Task 3: The animRipple channel and press-origin plumbing

**Files:**
- Modify: `internal/shell/animation.go:56-77` (channel block), `:16-27` (durations), `:192-201` (`easeFor`), `:204-249` (`duration`), `:96-120` (`animator` struct), `:672-700` (`apply`)
- Modify: `internal/shell/panelhost.go:1329-1330` (pointer press), `:1521-1522` (keyboard accept)
- Test: `internal/shell/animation_test.go`

**Interfaces:**
- Produces: `animRipple` channel; `rippleGrow`, `rippleFade` durations (500 ms + 300 ms);
  `(*animator).TargetRipple(node string, x, y int)`; `(*animator).Ripple(node string) (phase float64, x, y int, ok bool)`;
  `apply` writes `n.PressProgress` and `n.Ripple` onto the render copy.

- [ ] **Step 1: Write the failing tests**

In `internal/shell/animation_test.go`:

```go
func TestRippleRestartsAtTheNewOrigin(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	a.TargetRipple("lock", 3, 4)
	clock.add(400 * time.Millisecond)
	if phase, x, y, ok := a.Ripple("lock"); !ok || phase <= 0 || phase >= 1 || x != 3 || y != 4 {
		t.Fatalf("in-flight ripple = %v at %v,%v (ok %v)", phase, x, y, ok)
	}
	// A second press retargets the origin and restarts the phase: one ripple
	// per node at a time.
	a.TargetRipple("lock", 8, 9)
	if phase, x, y, _ := a.Ripple("lock"); phase > 0.1 || x != 8 || y != 9 {
		t.Fatalf("second press left phase %v at %v,%v", phase, x, y)
	}
	clock.add(2 * time.Second)
	if phase, _, _, _ := a.Ripple("lock"); phase != 1 {
		t.Fatalf("settled ripple phase = %v, want 1", phase)
	}
	if !a.Settled() {
		t.Error("a settled ripple must not keep the surface requesting frames")
	}
	if _, _, _, ok := a.Ripple("ghost"); ok {
		t.Error("a node with no ripple reported one")
	}
}

func TestRippleRunsLinearlyForItsFullRecipe(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	if got := a.duration(animRipple, true); got != 800*time.Millisecond {
		t.Fatalf("ripple duration = %v, want 800ms", got)
	}
	a.TargetRipple("lock", 0, 0)
	if got := a.easeFor(animRipple)(0.5); got != 0.5 {
		t.Fatalf("ripple ease at midpoint = %v, want linear", got)
	}
}

func TestRippleSnapsUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(true)
	a.TargetRipple("lock", 1, 2)
	if phase, _, _, _ := a.Ripple("lock"); phase != 1 {
		t.Fatalf("reduced-motion ripple phase = %v, want an immediate 1 (no ripple)", phase)
	}
}

func TestApplyWritesPressProgressAndRipple(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		{Kind: ui.KindButton, Action: "b", Bounds: ui.Rect{W: 10, H: 10}},
	}}
	interaction{press: "b", stateLayer: true}.apply(root, a)
	a.TargetRipple("b", 2, 3)
	clock.add(100 * time.Millisecond)
	interaction{press: "b", stateLayer: true}.apply(root, a)
	n := root.Children[0]
	if n.PressProgress <= 0 || n.PressProgress >= 1 {
		t.Fatalf("press progress = %v, want a value in flight", n.PressProgress)
	}
	if n.Ripple.X != 2 || n.Ripple.Y != 3 || n.Ripple.Phase <= 0 || n.Ripple.Phase >= 1 {
		t.Fatalf("ripple on the copy = %+v", n.Ripple)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestRipple|TestApplyWrites' ./internal/shell`
Expected: FAIL to compile, `TargetRipple`/`Ripple`/`animRipple` undefined.

- [ ] **Step 3: Implement**

`internal/shell/animation.go`:

Durations, after `pressScale` (line 26):

```go
	// rippleGrow and rippleFade are the ripple recipe's two halves: a disc
	// grows from the press origin for 500 ms, then fades for 300 ms.
	rippleGrow = 500 * time.Millisecond
	rippleFade = 300 * time.Millisecond
```

Channel block, after `animSprite` (line 76):

```go
	// animRipple is a press ripple's phase, keyed by the pressed node like
	// every interaction channel. The press origin travels beside the value.
	animRipple
```

In `easeFor` (line 192), before the selection check so the spatial curve cannot take it:

```go
	if channel == animRipple {
		// The phase is a clock the ripple geometry slices; easing it here
		// would ease the fade twice.
		return func(p float64) float64 { return p }
	}
```

In `duration`'s switch (line 216): `case animRipple: return rippleGrow + rippleFade`. The
reduced-motion branch at the top already returns 0 for non-visible channels, so a reduced ripple
snaps to its done phase and paints nothing.

In the `animator` struct (line 96), beside `values`:

```go
	// origins are ripple press points, keyed like values. They live beside the
	// scalar phase because a press origin is a pair, not a progress.
	origins map[string][2]int
```

In `newAnimator`, add `origins: map[string][2]int{},` to the literal. After `Reset` (line 443):

```go
// TargetRipple starts, or retakes, a node's press ripple from a new origin.
// A second press while one is in flight restarts the phase at the new point:
// one ripple per node at a time. Reset first, because re-aiming a settled
// phase at 1 would otherwise be a no-op.
func (a *animator) TargetRipple(node string, x, y int) {
	if a.origins == nil {
		a.origins = map[string][2]int{}
	}
	a.origins[node] = [2]int{x, y}
	a.Reset(node, animRipple)
	a.Target(node, animRipple, 1)
}

// Ripple is a node's resolved ripple frame and press origin. ok is false when
// the node has none.
func (a *animator) Ripple(node string) (phase float64, x, y int, ok bool) {
	if !a.has(node, animRipple) {
		return 0, 0, 0, false
	}
	p := a.origins[node]
	return a.Value(node, animRipple), p[0], p[1], true
}
```

In `Forget` (line 496), add `delete(a.origins, key.node)` inside the loop. In `apply`'s animated
block (line 695), after the `animSelect` target:

```go
				n.PressProgress = anim.Value(key, animPress)
				if phase, x, y, ok := anim.Ripple(key); ok {
					n.Ripple = ui.RipplePaint{Phase: phase, X: x, Y: y}
				}
```

`internal/shell/panelhost.go`, the pointer press (lines 1329-1330):

```go
				h.pressed = n.StableKey()
				if ui.Animated(n) {
					h.anim.TargetRipple(n.StableKey(), h.hoverX, h.hoverY)
				}
				h.pointerChanged(r, h.pointer.setPress(n.StableKey()))
```

The keyboard accept (line 1521) — a keyboard activation has no pointer, so the ripple starts at
the focused control's centre:

```go
	case keySpace, keyEnter:
		if n := h.focused(); n != nil && ui.Animated(n) {
			h.anim.TargetRipple(n.StableKey(), n.Bounds.X+n.Bounds.W/2, n.Bounds.Y+n.Bounds.H/2)
		}
		return h.activate(r)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS. The bar never calls `TargetRipple`, so no bar surface carries a ripple.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/animation.go internal/shell/animation_test.go internal/shell/panelhost.go
git commit -m "feat(shell): animRipple channel with press-origin plumbing"
```

---

### Task 4: Paint the state overlay, the ripple disc, and the morphed radius

**Files:**
- Modify: `internal/render/paint.go:1218-1262` (`paintChrome`), `:450-451` (`KindTab`), `:598-625` (`paintSlider`), `:648-656` (menu rows)
- Modify: `internal/render/mask.go` (add `RippleMask` after `RingMask`, line 76)
- Modify: `internal/shell/panelhost.go:1194` (menu row hover resolution), `internal/shell/menu.go` (add `RowAt` after `PickAt`, line 202)
- Test: `internal/render/paint_test.go`, `internal/render/mask_test.go`, `internal/shell/menu_test.go`

**Interfaces:**
- Consumes: Task 1's `ui.MorphRadius`, `ui.RippleDisc`, `PressProgress`, `Ripple`; Task 3's resolved copy.
- Produces: `render.RippleMask(w, h, radius, cx, cy int, disc float64) *image.Alpha`; unexported
  `paintInteraction(c *Canvas, n *ui.Node, box ui.Rect, radius int, fg Color, style Style)`;
  `(*Menu).RowAt(n *ui.Node, x, y int) int`; the painted look every D1 kind gets.

- [ ] **Step 1: Write the failing tests**

In `internal/render/mask_test.go`:

```go
func TestRippleMaskClipsTheDiscToTheBox(t *testing.T) {
	inside := RippleMask(20, 20, 5, 10, 10, 6)
	if inside.AlphaAt(10, 10).A != 255 {
		t.Fatal("the disc's centre is not opaque")
	}
	if inside.AlphaAt(0, 0).A != 0 {
		t.Fatal("the disc leaks past the rounded corner")
	}
	outside := RippleMask(20, 20, 5, 25, 10, 6)
	for i := 0; i < len(outside.Pix); i++ {
		if outside.Pix[i] != 0 {
			t.Fatal("a disc centred outside the box painted inside it")
		}
	}
}
```

In `internal/render/paint_test.go`:

```go
func TestPressedButtonMorphsTowardHalfRadius(t *testing.T) {
	t.Parallel()
	paint := func(progress float64) Color {
		n := &ui.Node{Kind: ui.KindButton, Radius: 20, PressProgress: progress,
			Bounds: ui.Rect{W: 40, H: 40}}
		c := newTestCanvas(t, 40, 40)
		if err := paintNode(c, n, NewTextRenderer(mustTestFace(t)), testStyle, testStyle.Size); err != nil {
			t.Fatal(err)
		}
		return pixelAt(t, c, 5, 5)
	}
	if paint(0).A != 0 {
		t.Fatal("the resting stadium leaves the (5,5) corner unpainted")
	}
	if paint(1).A == 0 {
		t.Fatal("the pressed radius did not pull the fill into the corner")
	}
}

func TestRipplePaintsFromThePressOrigin(t *testing.T) {
	t.Parallel()
	paint := func(ripple ui.RipplePaint) Color {
		n := &ui.Node{Kind: ui.KindCapsule, Bounds: ui.Rect{W: 40, H: 20}, Ripple: ripple}
		c := newTestCanvas(t, 40, 20)
		if err := paintNode(c, n, NewTextRenderer(mustTestFace(t)), testStyle, testStyle.Size); err != nil {
			t.Fatal(err)
		}
		return pixelAt(t, c, 8, 10)
	}
	rest := paint(ui.RipplePaint{})
	if paint(ui.RipplePaint{Phase: 0.1, X: 5, Y: 10}) == rest {
		t.Fatal("no ripple wash near the press origin")
	}
	if paint(ui.RipplePaint{Phase: 1, X: 5, Y: 10}) != rest {
		t.Fatal("a finished ripple still paints")
	}
}

func TestTabAndMenuRowCarryTheStateLayer(t *testing.T) {
	t.Parallel()
	tab := &ui.Node{Kind: ui.KindTab, Action: "tab", State: ui.StateHovered, Bounds: ui.Rect{W: 30, H: 20}}
	c := newTestCanvas(t, 30, 20)
	if err := paintNode(c, tab, NewTextRenderer(mustTestFace(t)), testStyle, testStyle.Size); err != nil {
		t.Fatal(err)
	}
	if pixelAt(t, c, 2, 2).A == 0 {
		t.Fatal("a hovered tab painted no state layer")
	}
	row := &ui.Node{Kind: ui.KindText, Text: "Option", State: ui.StateHovered,
		Bounds: ui.Rect{X: 2, Y: 2, W: 26, H: 14}}
	menu := &ui.Node{Kind: ui.KindMenu, Bounds: ui.Rect{W: 30, H: 40}, Children: []*ui.Node{row}}
	c = newTestCanvas(t, 30, 40)
	if err := paintNode(c, menu, NewTextRenderer(mustTestFace(t)), testStyle, testStyle.Size); err != nil {
		t.Fatal(err)
	}
	if pixelAt(t, c, 3, 3).A == 0 {
		t.Fatal("a hovered menu row painted no state layer")
	}
}
```

In `internal/shell/menu_test.go`:

```go
func TestMenuRowAtFindsTheOptionUnderAPoint(t *testing.T) {
	n := &ui.Node{Kind: ui.KindMenu, Children: []*ui.Node{
		{Kind: ui.KindText, Bounds: ui.Rect{Y: 0, W: 10, H: 10}},
		{Kind: ui.KindText, Bounds: ui.Rect{Y: 10, W: 10, H: 10}},
	}}
	if got := (Menu{}).RowAt(n, 5, 12); got != 1 {
		t.Fatalf("RowAt = %d, want 1", got)
	}
	if got := (Menu{}).RowAt(n, 5, 25); got != -1 {
		t.Fatalf("RowAt below the list = %d, want -1", got)
	}
	if got := (Menu{}).RowAt(nil, 0, 0); got != -1 {
		t.Fatalf("RowAt(nil) = %d, want -1", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestRippleMask|TestPressedButton|TestRipplePaints|TestTabAndMenu|TestMenuRowAt' ./internal/render ./internal/shell`
Expected: FAIL to compile, `RippleMask` and `RowAt` undefined; the paint tests fail on state.

- [ ] **Step 3: Implement**

`internal/render/mask.go`, after `RingMask`:

```go
// RippleMask returns the coverage of a disc of radius disc centred at
// (cx, cy), clipped to a w×h rounded rectangle of radius radius. Built per
// frame rather than cached: the press origin varies per press, and a cache
// keyed by it would grow without bound. Cost is one pass over the node's box
// per in-flight ripple, the ceiling the design names.
func RippleMask(w, h, radius, cx, cy int, disc float64) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, max(w, 0), max(h, 0)))
	if w <= 0 || h <= 0 || disc <= 0 {
		return mask
	}
	radius = min(radius, min(w, h)/2)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			box := roundedCoverage(radius, w, h, x, y)
			if box == 0 {
				continue
			}
			dx, dy := float64(x)+0.5-float64(cx), float64(y)+0.5-float64(cy)
			cover := min(max(0.5-(math.Hypot(dx, dy)-disc), 0), 1)
			if a := uint8(min(float64(box), cover*255)); a > 0 {
				mask.SetAlpha(x, y, color.Alpha{A: a})
			}
		}
	}
	return mask
}
```

`internal/render/paint.go`, beside `stateLayer` (line 1187):

```go
// paintInteraction overlays a node's resolved interaction state on its box:
// the state layer, then any in-flight press ripple. fg is the node's
// on-container foreground, the colour both washes tint; radius is the
// physical corner radius the box already paints with.
func paintInteraction(c *Canvas, n *ui.Node, box ui.Rect, radius int, fg Color, style Style) {
	if layer := stateLayer(fg, n.State); layer.A > 0 {
		blendMask(c, RoundedMask(radius, box.W, box.H), box.X, box.Y, layer)
	}
	if n.Ripple.Phase <= 0 {
		return
	}
	logicalR, alpha := ui.RippleDisc(n.Ripple.Phase, n.Bounds, n.Ripple.X, n.Ripple.Y)
	if alpha <= 0 || logicalR <= 0 {
		return
	}
	cx := box.X + style.Scale120.Physical(n.Ripple.X-n.Bounds.X)
	cy := box.Y + style.Scale120.Physical(n.Ripple.Y-n.Bounds.Y)
	disc := float64(style.Scale120.Physical(int(math.Ceil(logicalR))))
	blendMask(c, RippleMask(box.W, box.H, radius, cx, cy, disc), box.X, box.Y, withAlpha(fg, alpha))
}
```

In `paintChrome`, after `radius := chromeRadius(...)` (line 1220):

```go
	// The pressed control's silhouette eases toward half its resolved radius.
	// chromeRadius's answer is never below one, so the morph target is real.
	radius = ui.MorphRadius(radius, n.PressProgress)
```

and replace the state-layer blend (line 1259) with `paintInteraction(c, n, box, radius, fg, style)`.

In the `KindTab` case (line 450):

```go
	case ui.KindTab:
		box := style.Scale120.PhysicalRect(n.Bounds)
		paintInteraction(c, n, box, chromeRadius(style, nodeRadius(style, n, 0), box), style.Foreground, style)
		return paintText(c, n.Text, box, text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline)
```

In `paintSlider` (line 598), the knob fill becomes:

```go
	c.FillRounded(ui.Rect{X: kx, Y: box.Y + (box.H-knob)/2, W: knob, H: knob},
		ui.MorphRadius(knob/2, n.PressProgress), style.accent())
	paintInteraction(c, n, box, box.H/2, style.Foreground, style)
```

In `paintMenu`'s child loop (line 648), after the selected block and before `paintText`:

```go
		if child.State != 0 {
			paintInteraction(c, child, cb, style.Scale120.Physical(4), style.Foreground, style)
		}
```

`internal/shell/menu.go`, after `PickAt` (line 202):

```go
// RowAt is the index of the menu option under a point, or -1. It is PickAt's
// walk without the mutation, so the render path can resolve row hover.
func (m *Menu) RowAt(n *ui.Node, x, y int) int {
	if m == nil || n == nil {
		return -1
	}
	for i, c := range n.Children {
		if c != nil && c.Bounds.Contains(x, y) {
			return i
		}
	}
	return -1
}
```

`internal/shell/panelhost.go`, in `render` after `h.pointer.apply(root, h.anim)` (line 1194):

```go
	// Menu rows are plain text children the interaction walk never touches,
	// so the open menu's row under the pointer is resolved here.
	if h.menu != nil && h.menu.Opened() && h.menuPath != "" {
		markMenuRowHover(root, h.menuPath, h.menu, h.hoverX, h.hoverY)
	}
```

and beside it:

```go
// markMenuRowHover marks the open menu's option under the pointer with hover
// state on the render copy.
func markMenuRowHover(root *ui.Node, path string, m *Menu, x, y int) {
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindMenu && n.Action == path {
			if i := m.RowAt(n, x, y); i >= 0 && i < len(n.Children) && n.Children[i] != nil {
				n.Children[i].State |= ui.StateHovered
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:

```bash
GOMAXPROCS=4 go test -count=1 ./internal/render
GOMAXPROCS=4 go test -count=1 ./internal/shell
```

Expected: PASS. If a scale test reports a ripple pixel outside the node box, the origin mapping in
`paintInteraction` is wrong, not the test.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/render internal/shell && GOMAXPROCS=4 go vet ./internal/render ./internal/shell
git add internal/render/paint.go internal/render/paint_test.go internal/render/mask.go internal/render/mask_test.go internal/shell/menu.go internal/shell/menu_test.go internal/shell/panelhost.go
git commit -m "feat(render): state overlay, ripple disc and pressed radius"
```

---

### Task 5: Pure panelPlacement, then the consumers

**Files:**
- Create: `internal/shell/panelplace.go`
- Modify: `internal/shell/panelhost.go:764-766` (`spawnPanelLocked` placement), `:78` (add `rect` beside `place`), `:1051` (`filletMargin` reads the placed rect)
- Test: `internal/shell/panelplace_test.go`

**Interfaces:**
- Produces: `panelPlacement(anchor, workArea ui.Rect, alreadyOpen []ui.Rect, size ui.Size) ui.Rect`
  (D5's signature; `workArea` is the design's "work area" reading of `output`);
  `Placement.anchorStrip()`, `Placement.workArea()`, `marginsFor(rect ui.Rect, p Placement) Margins`,
  `(*Registry).openPanelRectsLocked(output uint32, except PanelID) []ui.Rect`,
  `PanelHost.rect ui.Rect`.

- [ ] **Step 1: Write the failing tests**

`internal/shell/panelplace_test.go`:

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// One output, 1920x1080, top bar, bar zone 40, gap 8, padding 8.
var (
	topAnchor = ui.Rect{X: 960, Y: 0, W: 1, H: 48}
	topWork   = ui.Rect{X: 8, Y: 48, W: 1904, H: 1024}
)

func TestPanelPlacementWithNothingOpenIsToday(t *testing.T) {
	for _, tc := range []struct {
		name   string
		anchor ui.Rect
		work   ui.Rect
		size   ui.Size
		want   ui.Rect
	}{
		{"clock centred on its trigger", topAnchor, topWork, ui.Size{W: 300, H: 200},
			ui.Rect{X: 810, Y: 48, W: 300, H: 200}},
		{"left align clamps to padding", ui.Rect{X: 0, Y: 0, W: 1, H: 48}, topWork,
			ui.Size{W: 300, H: 200}, ui.Rect{X: 8, Y: 48, W: 300, H: 200}},
		{"right align clamps inside", ui.Rect{X: 1919, Y: 0, W: 1, H: 48}, topWork,
			ui.Size{W: 300, H: 200}, ui.Rect{X: 1612, Y: 48, W: 300, H: 200}},
		{"centre align", ui.Rect{X: 0, Y: 0, W: 1920, H: 48}, topWork,
			ui.Size{W: 300, H: 200}, ui.Rect{X: 810, Y: 48, W: 300, H: 200}},
		{"bottom bar attaches above the strip", ui.Rect{X: 960, Y: 1032, W: 1, H: 48},
			ui.Rect{X: 8, Y: 8, W: 1904, H: 1024}, ui.Size{W: 300, H: 200},
			ui.Rect{X: 810, Y: 832, W: 300, H: 200}},
		{"a panel wider than the work area clamps", topAnchor, topWork,
			ui.Size{W: 4000, H: 200}, ui.Rect{X: 8, Y: 48, W: 1904, H: 200}},
	} {
		if got := panelPlacement(tc.anchor, tc.work, nil, tc.size); got != tc.want {
			t.Errorf("%s: panelPlacement = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPanelPlacementShiftsAlongTheBarEdge(t *testing.T) {
	// The base position 810 overlaps; left to 600 is the smaller move.
	open := []ui.Rect{{X: 900, Y: 48, W: 300, H: 200}}
	if got := panelPlacement(topAnchor, topWork, open, ui.Size{W: 300, H: 200}); got.X != 600 {
		t.Fatalf("shift = %v, want the smaller move to x=600", got)
	}
	open = []ui.Rect{{X: 700, Y: 48, W: 300, H: 200}}
	if got := panelPlacement(topAnchor, topWork, open, ui.Size{W: 300, H: 200}); got.X != 1000 {
		t.Fatalf("shift = %v, want the smaller move to x=1000", got)
	}
	// A panel on the other edge shares no band and never shifts.
	other := []ui.Rect{{X: 810, Y: 832, W: 300, H: 200}}
	if got := panelPlacement(topAnchor, topWork, other, ui.Size{W: 300, H: 200}); got.X != 810 {
		t.Fatalf("shifted for a panel on the other edge: %v", got)
	}
	// Chained clears settle: two stacked panels push past both.
	both := []ui.Rect{{X: 700, Y: 48, W: 300, H: 200}, {X: 1050, Y: 48, W: 300, H: 200}}
	got := panelPlacement(topAnchor, topWork, both, ui.Size{W: 300, H: 200})
	for _, r := range both {
		if got.X < r.X+r.W && r.X < got.X+got.W {
			t.Fatalf("placed %v still overlaps %v", got, r)
		}
	}
}

func TestOpenPanelPlacesThroughThePureFunction(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{BarEdge: "top", BarZone: 40, Align: "right",
		OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reg.mu.Lock()
	h := reg.panelHosts[PanelSession]
	place, rect := h.place, h.rect
	reg.mu.Unlock()
	want := panelPlacement(place.anchorStrip(), place.workArea(), nil,
		ui.Size{W: place.Panel.W, H: place.Panel.H})
	if int(reqs[1].Open.MarginLeft) != want.X || int(reqs[1].Open.MarginTop) != want.Y {
		t.Fatalf("spec margins (%d,%d) disagree with panelPlacement %v",
			reqs[1].Open.MarginLeft, reqs[1].Open.MarginTop, want)
	}
	if rect != want {
		t.Fatalf("host rect %v, want %v", rect, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestPanelPlacement|TestOpenPanelPlaces' ./internal/shell`
Expected: FAIL to compile, `panelPlacement` undefined.

- [ ] **Step 3: Implement `internal/shell/panelplace.go`**

```go
package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// panelPlacement places a panel of size against the bar edge its trigger
// occupies. anchor is the trigger's strip on that edge, in output
// coordinates: its across-edge span is the bar zone plus the panel gap, and
// its along-edge position carries the trigger's Align or AnchorX. workArea is
// the region the panel may occupy — the output inset by the panel padding and
// cleared of the bar, which is the design's "work area". The panel sits flush
// against the anchor, clamped inside workArea, then shifts along that edge
// until it clears every rect in alreadyOpen. With nothing open the result is
// today's Placement.Margins arithmetic expressed as a rect.
func panelPlacement(anchor, workArea ui.Rect, alreadyOpen []ui.Rect, size ui.Size) ui.Rect {
	w, h := size.W, size.H
	w, h = min(w, workArea.W), min(h, workArea.H)
	x := min(max(anchor.X+anchor.W/2-w/2, workArea.X), workArea.X+workArea.W-w)
	// The anchor sits wholly above the work area for a top bar and wholly
	// below it for a bottom one.
	y := anchor.Y + anchor.H
	if anchor.Y+anchor.H > workArea.Y {
		y = anchor.Y - h
	}
	y = min(max(y, workArea.Y), workArea.Y+workArea.H-h)
	// Shift along the bar edge, preferring the smaller move past each open
	// surface. Passes are bounded by the open count: each move pushes the
	// panel fully past one rect.
	placed := ui.Rect{X: x, Y: y, W: w, H: h}
	for pass := 0; pass <= len(alreadyOpen); pass++ {
		moved := false
		for _, open := range alreadyOpen {
			if !overlaps(open, placed) {
				continue
			}
			right, left := open.X+open.W, open.X-w
			if left-open.X < right-placed.X && left >= workArea.X {
				x = left
			} else {
				x = right
			}
			x = min(max(x, workArea.X), workArea.X+workArea.W-w)
			placed.X = x
			moved = true
		}
		if !moved {
			break
		}
	}
	return placed
}

func overlaps(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// anchorStrip is the trigger's strip on the bar edge, in output coordinates.
func (p Placement) anchorStrip() ui.Rect {
	x, w := 0, p.Output.W
	switch {
	case p.AnchorX > 0:
		x, w = p.AnchorX, 1
	case p.Align == "left":
		x, w = 0, 1
	case p.Align == "right":
		x, w = p.Output.W-1, 1
	}
	span := p.BarZone + p.Gap
	if p.BarEdge == "bottom" {
		return ui.Rect{X: x, Y: p.Output.H - span, W: w, H: span}
	}
	return ui.Rect{X: x, Y: 0, W: w, H: span}
}

// workArea is the output inset by the panel padding and cleared of the bar
// edge. Attached panels keep today's asymmetry: the bar side is cleared by
// zone plus gap, the far side by padding.
func (p Placement) workArea() ui.Rect {
	w := p.Output.W - 2*p.Padding
	h := p.Output.H - p.BarZone - p.Gap - p.Padding
	if p.BarEdge == "bottom" {
		return ui.Rect{X: p.Padding, Y: p.Padding, W: w, H: h}
	}
	return ui.Rect{X: p.Padding, Y: p.BarZone + p.Gap, W: w, H: h}
}

// marginsFor converts a placed rect back to the layer-shell margins the
// surface requests.
func marginsFor(rect ui.Rect, p Placement) Margins {
	if p.BarEdge == "bottom" {
		return Margins{Left: rect.X, Bottom: p.Output.H - rect.Y - rect.H}
	}
	return Margins{Left: rect.X, Top: rect.Y}
}
```

`internal/shell/panelhost.go`:

Beside `place` (line 78): `rect ui.Rect` — the panel's placed rect on its output.

After `w, hgt := h.place.FittedSize()` (line 764), replace line 766 with:

```go
	margins := h.place.Margins()
	if !place.CenterY {
		h.rect = panelPlacement(place.anchorStrip(), place.workArea(),
			r.openPanelRectsLocked(output, id), ui.Size{W: w, H: hgt})
		margins = marginsFor(h.rect, place)
	}
	h.rect = ui.Rect{X: margins.Left, Y: margins.Top, W: w, H: hgt}
	if place.BarEdge == "bottom" {
		h.rect.Y = place.Output.H - margins.Bottom - hgt
	}
```

Beside `exclusiveBarZone`:

```go
// openPanelRectsLocked is the placed rect of every other open panel on the
// output. Caller holds r.mu.
func (r *Registry) openPanelRectsLocked(output uint32, except PanelID) []ui.Rect {
	var out []ui.Rect
	for id, h := range r.panelHosts {
		if id == except || h == nil || h.output != output || h.rect.W == 0 {
			continue
		}
		out = append(out, h.rect)
	}
	return out
}
```

In `filletMargin` (line 1051), the control-centre room computation reads the placed rect so a
shifted panel still measures its real room: replace `m := h.place.Margins()` with
`m := marginsFor(h.rect, h.place)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS. Every existing open-panel test must pass unchanged: with nothing else open,
`panelPlacement` reproduces today's margins.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/panelplace.go internal/shell/panelplace_test.go internal/shell/panelhost.go
git commit -m "feat(shell): pure panel placement that clears open panels"
```

---

### Task 6: Package gates

**Files:** none modified unless a gate fails.

- [ ] **Step 1: Run the repository gates, capped**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
GOMAXPROCS=4 go vet ./internal/ui ./internal/render ./internal/shell
GOMAXPROCS=4 go test -race -count=1 ./internal/ui
GOMAXPROCS=4 go test -race -count=1 ./internal/render
GOMAXPROCS=4 go test -race -count=1 ./internal/shell
GOMAXPROCS=4 go test -count=1 -p 2 ./...
git diff --exit-code -- go.mod go.sum
```

Expected: PASS, except `TestABatteryWidgetOpensTheSessionPanel`, which fails on this battery-less
desktop on `main` too (recorded 2026-09-25). Report it; do not fix it here. Never run
`go test -race ./...` — it has hard-locked this machine.

- [ ] **Step 2: Commit any gate fixes**

```bash
git add -A internal
git commit -m "fix(shell): gate fixes for the microinteraction primitives"
```

Skip this step if nothing changed.

---

### Task 7: Live Niri gate

**Files:** none.

- [ ] **Step 1: Build and deploy with a rollback copy**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
GOMAXPROCS=4 go build -o "$SCRATCH/sysc-shell" ./cmd/sysc-shell
cp ~/.local/bin/sysc-shell ~/.local/bin/sysc-shell.before-microinteraction-$(date +%Y%m%d)
cp "$SCRATCH/sysc-shell" ~/.local/bin/sysc-shell.new && mv ~/.local/bin/sysc-shell.new ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service && sleep 3 && systemctl --user is-active sysc-shell
```

`$SCRATCH` is the session scratchpad. Another session may own the running binary: check its
timestamp first and ask before replacing a build that is not from `main`.

- [ ] **Step 2: Assert the surfaces are mapped**

```bash
sysc-shell ipc panel.open '{"panel":"control-center"}'
sleep 2
niri msg -j layers | grep -c sysc-shell-panel
```

Pass when the count is at least 1 while the panel is open. This is the live assertion for mapped
surfaces.

- [ ] **Step 3: Capture the interaction states**

With the control centre open, capture at rest, then hover and press a control. Move the pointer
with `wlrctl pointer move` or `ydotool` if either is installed (`command -v wlrctl ydotool`);
otherwise use keyboard focus plus Enter, whose ripple starts at the focused control's centre:

```bash
grim "$SCRATCH/cc-rest.png"
# press capture during the 800 ms ripple:
(ydotool click 0xC0 || wlrctl pointer click left) & sleep 0.3; grim "$SCRATCH/cc-press.png"
sysc-shell ipc panel.close '{"panel":"control-center"}'
```

Crop and read the images. Pass when:
- a hovered control shows the 8% foreground wash and a pressed one the 12% wash;
- the press capture shows a disc growing from the press point, clipped to the control's rounded
  rectangle;
- a pressed slider handle's corner radius has visibly pulled toward half;
- two panels open at once (clock and control centre) do not overlap; the second sits shifted along
  the bar edge.

- [ ] **Step 4: Capture a notification card and the bar**

Open a notification (or wait for one) and capture hover and press on a card action: the state
layer must tint the card's controls. Then capture the bar with the pointer resting over a widget:
it must look exactly like today's bar — no hover wash, no ripple. If the pointer cannot be placed
over the bar, record that in the close reason rather than editing configuration.

- [ ] **Step 5: Reduced-motion pass**

Open Settings → Accessibility → Reduced motion (internal/settings/registry.go:345), then repeat
the Step 3 captures. Pass when hover and press states appear with no fade and no ripple disc is
visible, and the slider radius still changes. Toggle it back off afterwards.

- [ ] **Step 6: Close the tracker issue**

From `/home/nomadx/sysc-shell`:

```bash
bd close sysc-590 --reason "State layers, press ripple, radius morph and panel placement on main at <hash>; live captures <what was seen>"
```

Commit the tracker change by splicing the row into `HEAD`'s `.beads/issues.jsonl` (the file is
routinely truncated by the bd hook), then `git commit --no-verify` with a screened message.

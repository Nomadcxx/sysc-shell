package shell

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func slideHost(t *testing.T, now *time.Time) *toastHost {
	t.Helper()
	h := &toastHost{
		shown: map[string]map[uint32]ui.Rect{},
	}
	h.anim = newAnimator(func() time.Time { return *now }, false, DefaultTheme().Motion)
	return h
}

func TestNewCardsDoNotSlide(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	target := ui.Rect{Y: 10, W: 300, H: 80}
	if h.noteTargets("DP-1", []uint32{1}, []ui.Rect{target}) {
		t.Fatal("a new card reported movement")
	}
	if got := h.displayRect("DP-1", 1, target); got.Y != 10 {
		t.Fatalf("new card drawn at %d", got.Y)
	}
}

func TestDismissSlidesTheRestIntoPlace(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.noteTargets("DP-1", []uint32{1, 2}, []ui.Rect{{Y: 10, W: 300, H: 80}, {Y: 100, W: 300, H: 80}})
	if !h.noteTargets("DP-1", []uint32{2}, []ui.Rect{{Y: 10, W: 300, H: 80}}) {
		t.Fatal("card 2 moved up but no movement reported")
	}
	if got := h.displayRect("DP-1", 2, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != 100 {
		t.Fatalf("at the start card 2 is drawn at %d, want its old 100", got.Y)
	}
	now = now.Add(time.Second)
	if got := h.displayRect("DP-1", 2, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != 10 {
		t.Fatalf("settled card 2 at %d, want 10", got.Y)
	}
}

func TestSlideRetargetsFromTheDrawnRect(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{{Y: 200, W: 300, H: 80}})
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{{Y: 100, W: 300, H: 80}})
	now = now.Add(50 * time.Millisecond)
	mid := h.displayRect("DP-1", 3, ui.Rect{Y: 100, W: 300, H: 80})
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{{Y: 10, W: 300, H: 80}})
	if got := h.displayRect("DP-1", 3, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != mid.Y {
		t.Fatalf("retarget jumped from %d to %d", mid.Y, got.Y)
	}
}

func TestReducedMotionNeverSlides(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.anim = newAnimator(func() time.Time { return now }, true, DefaultTheme().Motion)
	h.noteTargets("DP-1", []uint32{1}, []ui.Rect{{Y: 100}})
	h.noteTargets("DP-1", []uint32{1}, []ui.Rect{{Y: 10}})
	if got := h.displayRect("DP-1", 1, ui.Rect{Y: 10}); got.Y != 10 {
		t.Fatalf("reduced motion drew %d", got.Y)
	}
}

func TestReducedMotionRelayoutDoesNotRestoreOldToastGeometry(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	old := ui.Rect{X: 20, Y: 200, W: 300, H: 80}
	first := ui.Rect{X: 20, Y: 100, W: 300, H: 80}
	next := ui.Rect{X: 20, Y: 10, W: 300, H: 80}
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{old})
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{first})

	motion := DefaultTheme().Motion
	motion.Reduced = true
	h.anim.Configure(motion, true)
	if got := h.displayRect("DP-1", 3, next); got != next {
		t.Fatalf("reduced motion drew stale geometry: %+v, want %+v", got, next)
	}
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{next})
	motion.Reduced = false
	h.anim.Configure(motion, false)
	if got := h.displayRect("DP-1", 3, next); got != next {
		t.Fatalf("disabling reduced motion restored old geometry %+v, want %+v", got, next)
	}
}

func TestSlideStateIsPerOutput(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.noteTargets("DP-1", []uint32{1}, []ui.Rect{{Y: 100, W: 300, H: 80}})
	h.noteTargets("DP-2", []uint32{1}, []ui.Rect{{Y: 400, W: 300, H: 80}})
	h.noteTargets("DP-1", []uint32{1}, []ui.Rect{{Y: 10, W: 300, H: 80}})
	if got := h.displayRect("DP-2", 1, ui.Rect{Y: 400, W: 300, H: 80}); got.Y != 400 {
		t.Fatalf("DP-2 card moved with DP-1 to y=%d", got.Y)
	}
}

// The blur follows a sliding card: at the start of the slide it sits where
// the card is drawn, its old slot, not where the card is heading.
func TestToastBlurFollowsASlidingCard(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	h.r = NewRegistry(cfg)
	t.Cleanup(h.r.Close)
	h.r.caps.Blur = true
	h.noteTargets("DP-1", []uint32{1, 2}, []ui.Rect{{Y: 10, W: 300, H: 80}, {Y: 100, W: 300, H: 80}})
	target := ui.Rect{Y: 10, W: 300, H: 80}
	h.noteTargets("DP-1", []uint32{2}, []ui.Rect{target})
	h.cards = map[string][]toastCard{"DP-1": {{root: &ui.Node{Kind: ui.KindColumn, Action: "notify:2:dismiss"}, rect: target}}}

	top := func() int {
		strips := h.blurShape("DP-1")
		if len(strips) == 0 {
			t.Fatal("no blur for a visible card")
		}
		return strips[0].Y
	}
	if got := top(); got != 100 {
		t.Fatalf("blur starts at %d, want the drawn slot 100", got)
	}
	now = now.Add(time.Second)
	if got := top(); got != 10 {
		t.Fatalf("settled blur starts at %d, want 10", got)
	}
}

// A click during the slide has to land on the card the user can see. At the
// start of the motion the destination slot is empty and the card still
// occupies its old slot; the card stored on the host is already the target.
func TestSlidingCardIsHitWhereItIsDrawn(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	top := ui.Rect{X: 20, Y: 10, W: 300, H: 80}
	mid := ui.Rect{X: 20, Y: 100, W: 300, H: 80}
	low := ui.Rect{X: 20, Y: 190, W: 300, H: 80}
	// Cards 2 and 3 leave the middle and lower slots and head for the slots above.
	h.noteTargets("DP-1", []uint32{2, 3}, []ui.Rect{mid, low})
	if !h.noteTargets("DP-1", []uint32{2, 3}, []ui.Rect{top, mid}) {
		t.Fatal("the stack did not move")
	}
	card2 := &ui.Node{Kind: ui.KindColumn, Action: "notify:2:dismiss"}
	card3 := &ui.Node{Kind: ui.KindColumn, Action: "notify:3:dismiss"}
	h.cards = map[string][]toastCard{
		"DP-1": {
			{root: card2, rect: top},
			{root: card3, rect: mid},
		},
	}

	got, ok := h.cardAt("DP-1", mid.X+8, mid.Y+8)
	if !ok || got.root != card2 || got.rect != mid {
		t.Fatalf("click on the card drawn at %+v hit %q at %+v", mid, toastCardAction(got), got.rect)
	}
	if hit, ok := h.cardAt("DP-1", top.X+8, top.Y+8); ok {
		t.Fatalf("click in the empty destination hit %q at %+v", toastCardAction(hit), hit.rect)
	}
	got, ok = h.cardAt("DP-1", low.X+8, low.Y+8)
	if !ok || got.root != card3 || got.rect != low {
		t.Fatalf("click on the card drawn at %+v hit %q at %+v", low, toastCardAction(got), got.rect)
	}
}

// The card leaving a slot is painted after the one that just appeared there,
// so at the start of the slide it still covers the newcomer. The click belongs
// to those pixels, at the rectangle they occupy.
func TestClickHitsTheCardPaintedOnTopDuringASlide(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	top := ui.Rect{X: 20, Y: 10, W: 300, H: 80}
	mid := ui.Rect{X: 20, Y: 100, W: 300, H: 80}
	h.noteTargets("DP-1", []uint32{2}, []ui.Rect{top})
	if !h.noteTargets("DP-1", []uint32{3, 2}, []ui.Rect{top, mid}) {
		t.Fatal("card 2 did not leave the top slot")
	}
	card3 := &ui.Node{Kind: ui.KindColumn, Action: "notify:3:dismiss"}
	card2 := &ui.Node{Kind: ui.KindColumn, Action: "notify:2:dismiss"}
	h.cards = map[string][]toastCard{
		"DP-1": {
			{root: card3, rect: top},
			{root: card2, rect: mid},
		},
	}

	got, ok := h.cardAt("DP-1", top.X+8, top.Y+8)
	if !ok || got.root != card2 || got.rect != top {
		t.Fatalf("click on the covered slot hit %q at %+v, want card 2 drawn at %+v", toastCardAction(got), got.rect, top)
	}
}

func toastCardAction(card toastCard) string {
	if card.root == nil {
		return ""
	}
	return card.root.Action
}

// The input region published when a card starts sliding is the rectangle it
// is drawn in, not the slot it is heading for.
func TestSlideInputRegionFollowsTheDrawnCard(t *testing.T) {
	r, h, _ := wiredToast(t)
	keepInvalidationsDrained(t, r)
	now := time.Unix(100, 0)
	h.anim = newAnimator(func() time.Time { return now }, false, DefaultTheme().Motion)
	h.text = render.NewTextRenderer(nil)
	// Hold the frame loop off so this test reads the region recompute
	// publishes at the start of the slide, with the clock still at progress 0.
	h.sliding = true

	r.applyNotify(snap(1, note(1, "lower"), note(2, "top")))
	r.mu.Lock()
	var drawn ui.Rect
	var found bool
	for _, card := range h.cards["eDP-1"] {
		id, ok := cardID(card.root)
		if ok && id == 1 {
			drawn = card.rect
			found = true
		}
	}
	r.mu.Unlock()
	if !found {
		t.Fatal("the lower card was not placed")
	}

	r.applyNotify(snap(2, note(1, "lower")))
	r.mu.Lock()
	var dest ui.Rect
	if len(h.cards["eDP-1"]) == 1 {
		dest = h.cards["eDP-1"][0].rect
	}
	r.mu.Unlock()
	if dest == (ui.Rect{}) || dest == drawn {
		t.Fatalf("destination %+v did not leave the drawn slot %+v", dest, drawn)
	}

	updates := h.harness().updates
	if len(updates) == 0 || !updates[len(updates)-1].SetInputRegion {
		t.Fatal("the slide did not publish an input region")
	}
	got := updates[len(updates)-1].InputRects
	if len(got) != 1 || got[0] != drawn {
		t.Fatalf("input region = %+v, want the drawn card %+v", got, drawn)
	}

	// Sampled early in the move: a spring is nearly home by the token, so the
	// mid-slide frame has to be taken well before it.
	now = now.Add(50 * time.Millisecond)
	h.publishSlideFrame()
	updates = h.harness().updates
	if len(updates) == 0 || !updates[len(updates)-1].SetInputRegion || len(updates[len(updates)-1].InputRects) != 1 {
		t.Fatal("the slide frame did not publish the moving input region")
	}
	moved := updates[len(updates)-1].InputRects[0]
	if moved.Y <= dest.Y || moved.Y >= drawn.Y || moved.X != drawn.X || moved.W != drawn.W || moved.H != drawn.H {
		t.Fatalf("mid-slide input region = %+v, want y strictly between destination %d and drawn %d", moved, dest.Y, drawn.Y)
	}
}

func TestToastResizeKeepsAnimatedInputInsideAcceptedSurface(t *testing.T) {
	r, h, _ := wiredToast(t)
	now := time.Unix(100, 0)
	h.anim = newAnimator(func() time.Time { return now }, false, DefaultTheme().Motion)
	h.text = render.NewTextRenderer(nil)
	// Keep the animation clock at its start so the resize path is deterministic.
	h.sliding = true

	r.applyNotify(snap(1, note(1, "resize")))
	spec := h.harness().opens[0]
	spec.Callbacks.OutputSize(300, 800)
	if err := spec.Callbacks.Configure(300, 800, 120); err != nil {
		t.Fatal(err)
	}
	if !h.anim.has(toastSlideKey("eDP-1", 1), animStackX) {
		t.Fatal("the output resize did not exercise an active card animation")
	}

	region := lastToastInput(h)
	if len(region) != 1 {
		t.Fatalf("animated input region = %+v, want the visible part of one card", region)
	}
	if got := region[0]; got != (ui.Rect{X: 0, Y: 12, W: 288, H: 56}) {
		t.Fatalf("animated input rect = %+v, want the visible card area inside the accepted surface", got)
	}
	r.mu.Lock()
	drawn := h.drawnRect("eDP-1", h.cards["eDP-1"][0])
	scale := ui.Scale120(h.scale120["eDP-1"])
	r.mu.Unlock()
	box := scale.PhysicalRect(drawn)
	if box.X >= 0 || box.X+box.W >= 300 {
		t.Fatalf("drawn card = %+v, want a partially clipped active slide", box)
	}
	fonts, err := render.NewSystemFontMap(r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
	if err != nil {
		t.Fatal(err)
	}
	h.text = render.NewTextRendererWithFontMap(fonts)
	h.measured["eDP-1"] = true
	const width, height, stride = 300, 800, 300 * 4
	pixels := make([]byte, stride*height)
	if err := spec.Callbacks.Render(pixels, width, height, stride); err != nil {
		t.Fatal(err)
	}
	// A negative destination X must not wrap the left-clipped card into the
	// preceding row; this point is inside the body past its rounded corner.
	leakX, leakY := width+box.X+60, box.Y-1
	if leakX >= width || leakY < 0 {
		t.Fatalf("outside-card probe (%d,%d) is outside the surface", leakX, leakY)
	}
	offset := leakY*stride + leakX*4
	if pixels[offset] != 0 || pixels[offset+1] != 0 || pixels[offset+2] != 0 || pixels[offset+3] != 0 {
		t.Fatalf("pixel above the clipped card = %v, want transparent", pixels[offset:offset+4])
	}
}

// A card interrupted mid-move keeps its speed: the retarget changes the
// destination only, so the drawn rectangle never jumps.
func TestToastStackRetargetKeepsSpatialVelocity(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	old := ui.Rect{X: 20, Y: 200, W: 300, H: 80}
	first := ui.Rect{X: 20, Y: 100, W: 300, H: 80}
	next := ui.Rect{X: 20, Y: 10, W: 300, H: 80}
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{old})
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{first})
	now = now.Add(50 * time.Millisecond)
	mid := h.displayRect("DP-1", 3, first)
	key := animKey{node: toastSlideKey("DP-1", 3), channel: animStackY}
	speed := h.anim.values[key].velocity
	if speed >= 0 {
		t.Fatalf("card is not travelling upward: %g", speed)
	}
	h.noteTargets("DP-1", []uint32{3}, []ui.Rect{next})
	if got := h.displayRect("DP-1", 3, next); got != mid {
		t.Fatalf("retarget jumped: %+v -> %+v", mid, got)
	}
	if got := h.anim.values[key].velocity; got != speed {
		t.Fatalf("toast retarget discarded speed %g -> %g", speed, got)
	}
}

// Every rectangle component retargets independently: a card that shrinks while
// it moves keeps each component's position and speed, and the drawn size never
// goes negative.
func TestToastStackResizeRetargetKeepsEveryComponent(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	old := ui.Rect{X: -40, Y: 200, W: 300, H: 80}
	shrunk := ui.Rect{X: 0, Y: 100, W: 280, H: 64}
	next := ui.Rect{X: 20, Y: 10, W: 240, H: 56}
	h.noteTargets("DP-1", []uint32{7}, []ui.Rect{old})
	h.noteTargets("DP-1", []uint32{7}, []ui.Rect{shrunk})
	now = now.Add(50 * time.Millisecond)
	drawn := h.displayRect("DP-1", 7, shrunk)
	h.noteTargets("DP-1", []uint32{7}, []ui.Rect{next})
	if got := h.displayRect("DP-1", 7, next); got != drawn {
		t.Fatalf("resize retarget jumped: %+v -> %+v", drawn, got)
	}
	key := func(c animChannel) animKey {
		return animKey{node: toastSlideKey("DP-1", 7), channel: c}
	}
	for c, want := range map[animChannel]float64{
		animStackX: float64(next.X), animStackY: float64(next.Y),
		animStackW: float64(next.W), animStackH: float64(next.H),
	} {
		if got := h.anim.values[key(c)].to; got != want {
			t.Fatalf("channel %d target = %g, want %g", c, got, want)
		}
	}
	if got := h.displayRect("DP-1", 7, next); got.W < 0 || got.H < 0 {
		t.Fatalf("displayed size went negative: %+v", got)
	}

	// Two connectors carrying the same notification id keep separate geometry.
	h.noteTargets("DP-2", []uint32{7}, []ui.Rect{old})
	h.noteTargets("DP-2", []uint32{7}, []ui.Rect{ui.Rect{X: 0, Y: 40, W: 280, H: 64}})
	now = now.Add(20 * time.Millisecond)
	_ = h.displayRect("DP-2", 7, ui.Rect{X: 0, Y: 40, W: 280, H: 64})
	other := animKey{node: toastSlideKey("DP-2", 7), channel: animStackY}
	if h.anim.values[other].to == h.anim.values[key(animStackY)].to {
		t.Fatal("two outputs shared one card's target")
	}
	if h.anim.values[other].velocity == h.anim.values[key(animStackY)].velocity {
		t.Fatal("two outputs shared one card's velocity")
	}

	// A first placement creates no geometry state at all.
	fresh := slideHost(t, &now)
	target := ui.Rect{Y: 10, W: 300, H: 80}
	fresh.noteTargets("DP-1", []uint32{1}, []ui.Rect{target})
	if fresh.anim.has(toastSlideKey("DP-1", 1), animStackX) {
		t.Fatal("first placement created unfinished geometry state")
	}
}

// A card that left the stack retires every channel, and a dropped output
// forgets its cards' geometry; either way the surface can settle.
func TestToastStackRetiresAllGeometry(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.noteTargets("DP-1", []uint32{2, 3}, []ui.Rect{{Y: 10, W: 300, H: 80}, {Y: 100, W: 300, H: 80}})
	// Card 3 moves up; card 2 stays put.
	h.noteTargets("DP-1", []uint32{2, 3}, []ui.Rect{{Y: 10, W: 300, H: 80}, {Y: 50, W: 300, H: 80}})
	if !h.anim.has(toastSlideKey("DP-1", 3), animStackX) {
		t.Fatal("the moved card has no geometry")
	}
	if h.anim.has(toastSlideKey("DP-1", 2), animStackX) {
		t.Fatal("a card that never moved has geometry")
	}
	// Card 3 left the stack: every channel retires with it.
	h.noteTargets("DP-1", []uint32{2}, []ui.Rect{{Y: 10, W: 300, H: 80}})
	for _, c := range []animChannel{animStackX, animStackY, animStackW, animStackH} {
		if h.anim.has(toastSlideKey("DP-1", 3), c) {
			t.Fatalf("channel %d survived retirement", c)
		}
	}
	for i := 0; i < 4000 && !h.anim.Settled(); i++ {
		now = now.Add(springStep)
	}
	if !h.anim.Settled() {
		t.Fatal("the stack never settled after retiring a card")
	}

	// A dropped output forgets its cards' geometry too.
	h.noteTargets("DP-2", []uint32{9}, []ui.Rect{{Y: 10, W: 300, H: 80}})
	h.noteTargets("DP-2", []uint32{9}, []ui.Rect{{Y: 100, W: 300, H: 80}})
	if !h.anim.has(toastSlideKey("DP-2", 9), animStackX) {
		t.Fatal("DP-2 card has no geometry")
	}
	h.drop("DP-2")
	if h.anim.has(toastSlideKey("DP-2", 9), animStackX) {
		t.Fatal("dropped output kept its card geometry")
	}

	// Reduced motion never creates geometry state: the card is drawn at its
	// slot, so there is nothing left in flight to retire.
	reduced := slideHost(t, &now)
	reduced.anim = newAnimator(func() time.Time { return now }, true, DefaultTheme().Motion)
	reduced.noteTargets("DP-1", []uint32{1}, []ui.Rect{{Y: 100}})
	reduced.noteTargets("DP-1", []uint32{1}, []ui.Rect{{Y: 10}})
	if reduced.anim.has(toastSlideKey("DP-1", 1), animStackX) {
		t.Fatal("reduced motion created geometry state")
	}
}

// The retheme pass reaches the toast animator: the table and flag move and the
// animator itself survives.
func TestToastRethemeAppliesMotion(t *testing.T) {
	r, h, _ := wiredToast(t)
	now := time.Unix(100, 0)
	h.anim = newAnimator(func() time.Time { return now }, false, DefaultTheme().Motion)
	anim := h.anim
	h.anim.Target(toastSlideKey("eDP-1", 1), animStackY, 100)

	r.mu.Lock()
	r.cfg.Accessibility.ReducedMotion = true
	h.restyleLocked()
	r.mu.Unlock()

	if h.anim != anim {
		t.Fatal("restyle replaced the toast animator")
	}
	if !h.anim.reduced {
		t.Fatal("reduced motion did not reach the toast animator")
	}
	if h.anim.has(toastSlideKey("eDP-1", 1), animStackY) {
		t.Fatal("reduced motion retained toast geometry state")
	}
}

// The toast slide loop asks for the cap on every tick; see
// TestOSDRevealUsesReloadedCap for why the frozen clock makes the second
// publish attributable to the restyle alone.
func TestToastSlideUsesReloadedCap(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = false
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	h := newToastHost(reg, nil)
	reg.mu.Lock()
	reg.toasts = h
	reg.mu.Unlock()
	frozen := time.Unix(500, 0)
	frames := make(chan struct{}, 16)
	// Only slide frames count: restyle's own relayout may send requests too,
	// and those must not stand in for a publish from the loop.
	var armed atomic.Bool
	armed.Store(true)
	reg.mu.Lock()
	h.outputs["DP-1"] = 7
	h.request = func(req wayland.AuxRequest) {
		if !armed.Load() || req.Update == nil || !req.Update.SetInputRegion {
			return
		}
		select {
		case frames <- struct{}{}:
		default:
		}
	}
	h.anim = newAnimator(func() time.Time { return frozen }, false, reg.panelTheme().Motion)
	h.anim.Target("card", animVisible, 1)
	h.anim.motion.FrameCap = time.Hour
	done := h.startSlideLocked()
	stop := h.stopSlide
	reg.mu.Unlock()
	t.Cleanup(func() {
		reg.mu.Lock()
		if h.stopSlide == stop && stop != nil {
			close(stop)
			h.stopSlide = nil
		}
		reg.mu.Unlock()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("slide loop did not stop")
		}
	})

	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("the slide published no opening frame")
	}
	reg.mu.Lock()
	armed.Store(false)
	reg.cfg.Theme.FrameCap = theme.Repaint120
	h.restyleLocked()
	for len(frames) > 0 {
		<-frames
	}
	armed.Store(true)
	reg.mu.Unlock()
	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("a slide in flight kept the cap it started with")
	}
}

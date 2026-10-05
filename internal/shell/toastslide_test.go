package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func slideHost(t *testing.T, now *time.Time) *toastHost {
	t.Helper()
	h := &toastHost{
		shown: map[string]map[uint32]ui.Rect{},
		from:  map[string]map[uint32]ui.Rect{},
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

	now = now.Add(150 * time.Millisecond)
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

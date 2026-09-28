package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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

package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func spinnerTree() *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{Kind: ui.KindSpinner, Key: "launching"}}}
}

func resolvedPhase(a *animator) float64 {
	root := spinnerTree()
	resolveSpriteMotion(a, root)
	return root.Children[0].Value
}

func TestSpinnerTurnsOnTheSurfaceClock(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(false)
	if got := resolvedPhase(a); got != 0 {
		t.Fatalf("first pose = %v, want 0", got)
	}
	clock.add(spinnerCycle / 4)
	if got := resolvedPhase(a); got != 0.25 {
		t.Fatalf("a quarter cycle in = %v, want 0.25", got)
	}
	clock.add(spinnerCycle * 3 / 4)
	if got := resolvedPhase(a); got != 0 {
		t.Fatalf("a full cycle in = %v, want 0 again", got)
	}
}

func TestSpinnerRestsUnderReducedMotion(t *testing.T) {
	t.Parallel()
	a, clock := newTestAnimator(true)
	clock.add(spinnerCycle / 3)
	if got := resolvedPhase(a); got != 0 {
		t.Fatalf("reduced motion turned the spinner to %v", got)
	}
	if !a.Settled() {
		t.Fatal("reduced motion left a spinner in flight")
	}
}

func TestSpinnerRetiresWhenItsNodeLeaves(t *testing.T) {
	t.Parallel()
	a, _ := newTestAnimator(false)
	resolvedPhase(a)
	if a.Settled() {
		t.Fatal("a turning spinner reported settled")
	}
	resolveSpriteMotion(a, &ui.Node{Kind: ui.KindRow})
	if !a.Settled() {
		t.Fatal("a removed spinner kept the surface animating")
	}
}

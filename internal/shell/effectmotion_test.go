package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func effectMotionTree(key string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindStack,
		Children: []*ui.Node{{
			Kind:   ui.KindEffect,
			Key:    key,
			Effect: ui.EffectSpec{Program: ui.EffectWeather},
		}},
	}
}

func effectMotionNode(root *ui.Node) *ui.Node {
	return root.Children[0]
}

func TestEffectMotionKeepsPhaseAcrossTreeRebuilds(t *testing.T) {
	a, clock := newTestAnimator(false)
	h := &PanelHost{anim: a}

	first := effectMotionTree("weather-effect")
	firstRender := copyNode(first)
	if err := h.resolveEffectMotionLocked(firstRender); err != nil {
		t.Fatal(err)
	}
	if got := len(a.values); got != 1 {
		t.Fatalf("effect animator values = %d, want one", got)
	}
	v, ok := a.values[animKey{node: "weather-effect", channel: animEffect}]
	if !ok || v.loop != ui.GradientLoop {
		t.Fatalf("effect animator value = %+v, want a looping effect entry", v)
	}
	if a.Settled() {
		t.Fatal("live effect left the animator settled")
	}

	clock.add(250 * time.Millisecond)
	phase := a.Value("weather-effect", animEffect)
	if err := h.resolveEffectMotionLocked(firstRender); err != nil {
		t.Fatal(err)
	}
	if got := effectMotionNode(firstRender).EffectPhase; got != phase {
		t.Fatalf("resolving the same tree moved phase from %v to %v", phase, got)
	}
	second := copyNode(effectMotionTree("weather-effect"))
	if err := h.resolveEffectMotionLocked(second); err != nil {
		t.Fatal(err)
	}
	if got := effectMotionNode(second).EffectPhase; got != phase {
		t.Fatalf("rebuilt tree phase = %v, want preserved %v", got, phase)
	}
	if effectMotionNode(first).EffectPhase != 0 {
		t.Fatal("resolving a render copy mutated the retained tree")
	}
}

func TestEffectMotionForgetsRemovedEffects(t *testing.T) {
	a, _ := newTestAnimator(false)
	h := &PanelHost{anim: a}
	if err := h.resolveEffectMotionLocked(copyNode(effectMotionTree("weather-effect"))); err != nil {
		t.Fatal(err)
	}

	if err := h.resolveEffectMotionLocked(&ui.Node{Kind: ui.KindStack}); err != nil {
		t.Fatal(err)
	}
	if a.has("weather-effect", animEffect) {
		t.Fatal("removed effect kept its animator entry")
	}
	if !a.Settled() {
		t.Fatal("animator remained unsettled after the final effect was removed")
	}
}

func TestEffectMotionReducedMotionParksPhase(t *testing.T) {
	a, clock := newTestAnimator(true)
	h := &PanelHost{anim: a}
	root := copyNode(effectMotionTree("weather-effect"))
	if err := h.resolveEffectMotionLocked(root); err != nil {
		t.Fatal(err)
	}
	if got := effectMotionNode(root).EffectPhase; got != 0.5 {
		t.Fatalf("reduced effect phase = %v, want midpoint 0.5", got)
	}
	if v := a.values[animKey{node: "weather-effect", channel: animEffect}]; v.loop != ui.GradientNone {
		t.Fatalf("reduced effect kept loop mode %d", v.loop)
	}
	clock.add(time.Hour)
	again := copyNode(effectMotionTree("weather-effect"))
	if err := h.resolveEffectMotionLocked(again); err != nil {
		t.Fatal(err)
	}
	if got := effectMotionNode(again).EffectPhase; got != 0.5 {
		t.Fatalf("reduced effect phase moved to %v", got)
	}
	if !a.Settled() {
		t.Fatal("reduced effect requested frames")
	}
}

func TestEffectMotionRejectsMissingStableKey(t *testing.T) {
	a, _ := newTestAnimator(false)
	h := &PanelHost{anim: a}
	if err := h.resolveEffectMotionLocked(copyNode(effectMotionTree(""))); err == nil {
		t.Fatal("effect without a stable key was accepted")
	}
	if len(a.values) != 0 {
		t.Fatalf("malformed effect created %d animator values", len(a.values))
	}
}

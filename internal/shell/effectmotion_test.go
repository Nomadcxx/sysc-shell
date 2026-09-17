package shell

import (
	"bytes"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
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

func TestWeatherSurfaceRendersAndPublishesAnimatedFrames(t *testing.T) {
	a, clock := newTestAnimator(false)
	const width, height = 460, 560
	effect := &ui.Node{Kind: ui.KindEffect, Key: "weather:hero", Bounds: ui.Rect{W: width, H: height}, Effect: ui.EffectSpec{
		Program: ui.EffectWeather, Variant: ui.WeatherClear, Seed: 7, Intensity: .7, Speed: 1,
	}}
	h := &PanelHost{
		id: PanelWeather, output: 7, root: &ui.Node{Kind: ui.KindStack, Bounds: ui.Rect{W: width, H: height}, Children: []*ui.Node{effect}},
		anim: a, stopAnim: make(chan struct{}), theme: DefaultTheme(), text: &render.TextRenderer{},
		logicalW: width, logicalH: height, scale120: 120, place: Placement{Panel: ui.Rect{W: width, H: height}},
	}
	a.Target(panelSurfaceID(PanelWeather), animVisible, 1)
	r := &Registry{
		invalidations: make(chan wayland.Invalidation, 8),
		closed:        make(chan struct{}),
		panelHosts:    map[PanelID]*PanelHost{PanelWeather: h},
	}

	r.mu.Lock()
	if err := h.resolveEffectMotionLocked(copyNode(h.root)); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	base := clock.t
	key := animKey{node: effect.StableKey(), channel: animEffect}
	value, ok := h.anim.values[key]
	if !ok {
		r.mu.Unlock()
		t.Fatalf("weather panel has no animator value for %q", key.node)
	}
	value.start = base.Add(-100 * time.Millisecond)
	h.anim.values[key] = value
	for animKey, value := range h.anim.values {
		if animKey.channel != animEffect {
			value.start = time.Time{}
			h.anim.values[animKey] = value
		}
	}
	region := effect.Bounds
	r.mu.Unlock()

	renderAt := func(phase float64) []byte {
		t.Helper()
		r.mu.Lock()
		value := h.anim.values[key]
		value.start = base.Add(-time.Duration(phase * float64(effectTrip)))
		h.anim.values[key] = value
		pixels := make([]byte, width*height*4)
		err := h.render(pixels, width, height, width*4)
		r.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	first := renderAt(.10)
	second := renderAt(.60)
	if !weatherFrameRegionDiffers(first, second, width, region) {
		t.Fatal("changing the weather phase did not change the rendered hero")
	}

	r.startSurfaceFrames(h)
	select {
	case invalidation := <-r.Invalidations():
		if invalidation.SurfaceID != panelSurfaceID(PanelWeather) {
			t.Fatalf("frame invalidated %q, want %q", invalidation.SurfaceID, panelSurfaceID(PanelWeather))
		}
	case <-time.After(time.Second):
		t.Fatal("visible weather surface did not publish an animation frame")
	}

	h.stopAnimation()
	for {
		select {
		case <-r.Invalidations():
		default:
			goto drained
		}
	}
drained:
	if got := countSurfaceInvalidations(r, 50*time.Millisecond); got != 0 {
		t.Fatalf("closed weather surface kept publishing %d frames", got)
	}
}

func weatherFrameRegionDiffers(first, second []byte, width int, region ui.Rect) bool {
	for y := max(region.Y, 0); y < min(region.Y+region.H, len(first)/(width*4)); y++ {
		for x := max(region.X, 0); x < min(region.X+region.W, width); x++ {
			i := y*width*4 + x*4
			if !bytes.Equal(first[i:i+4], second[i:i+4]) {
				return true
			}
		}
	}
	return false
}

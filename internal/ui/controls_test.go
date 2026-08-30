package ui

import "testing"

func TestToggleActivatesOnSpaceAndClick(t *testing.T) {
	t.Parallel()
	n := &Node{Kind: KindToggle, Focusable: true, Name: "Reduced motion", Role: "switch"}
	if n.Value != 0 {
		t.Fatal("toggle starts off")
	}
	if !Activate(n) {
		t.Fatal("space/activate must flip")
	}
	if n.Value != 1 {
		t.Fatalf("on value = %v, want 1", n.Value)
	}
	n.Bounds = Rect{X: 10, Y: 10, W: ToggleWidth, H: ToggleHeight}
	if !Click(n, 20, 20) {
		t.Fatal("click inside must flip")
	}
	if n.Value != 0 {
		t.Fatalf("click off value = %v, want 0", n.Value)
	}
	if Click(n, 0, 0) {
		t.Fatal("click outside must not flip")
	}
}

func TestSliderClampsAndSteps(t *testing.T) {
	t.Parallel()
	n := &Node{Kind: KindSlider, Min: 24, Max: 64, Step: 1, Value: 40}
	SliderSet(n, 100)
	if n.Value != 64 {
		t.Fatalf("clamp high = %v, want 64", n.Value)
	}
	SliderSet(n, 41.4)
	if n.Value != 41 {
		t.Fatalf("snap = %v, want 41", n.Value)
	}
}

func TestSliderArrowKeysAdjust(t *testing.T) {
	t.Parallel()
	n := &Node{Kind: KindSlider, Min: 24, Max: 64, Step: 1, Value: 40}
	if !ControlKey(n, KeyRight) {
		t.Fatal("right must adjust")
	}
	if n.Value != 41 {
		t.Fatalf("right = %v, want 41", n.Value)
	}
	ControlKey(n, KeyLeft)
	if n.Value != 40 {
		t.Fatalf("left = %v, want 40", n.Value)
	}
	ControlKey(n, KeyHome)
	if n.Value != 24 {
		t.Fatalf("home = %v, want 24", n.Value)
	}
	ControlKey(n, KeyEnd)
	if n.Value != 64 {
		t.Fatalf("end = %v, want 64", n.Value)
	}
	ControlKey(n, KeyRight)
	if n.Value != 64 {
		t.Fatalf("right at max must clamp, got %v", n.Value)
	}
}

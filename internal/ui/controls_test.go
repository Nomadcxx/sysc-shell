package ui

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

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

func TestSliderAtMapsPointerX(t *testing.T) {
	t.Parallel()
	n := &Node{Kind: KindSlider, Min: 0, Max: 100, Step: 1, Value: 0, Bounds: Rect{X: 10, Y: 10, W: 100, H: 14}}
	SliderAt(n, 10)
	if n.Value != 0 {
		t.Fatalf("left edge = %v, want 0", n.Value)
	}
	SliderAt(n, 110)
	if n.Value != 100 {
		t.Fatalf("right edge = %v, want 100", n.Value)
	}
	SliderAt(n, 60)
	if n.Value != 50 {
		t.Fatalf("mid = %v, want 50", n.Value)
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

func TestSliderCanReachMaximumWhenStepExceedsRange(t *testing.T) {
	t.Parallel()
	n := &Node{
		Kind: KindSlider, Min: 0, Max: 1, Step: 3, Value: 0,
		Bounds: Rect{X: 10, Y: 10, W: 100, H: 14},
	}

	if !ControlKey(n, KeyEnd) || n.Value != n.Max {
		t.Fatalf("End value = %v, want %v", n.Value, n.Max)
	}
	n.Value = n.Min
	SliderAt(n, n.Bounds.X+n.Bounds.W)
	if n.Value != n.Max {
		t.Fatalf("right-edge value = %v, want %v", n.Value, n.Max)
	}
}

func TestDisabledSliderIgnoresPointerAndKeyboardInput(t *testing.T) {
	t.Parallel()
	n := &Node{
		Kind: KindSlider, Min: 0, Max: 100, Step: 1, Value: 40,
		State: StateDisabled, Focusable: true,
		Bounds: Rect{X: 10, Y: 10, W: 100, H: 14},
	}

	SliderAt(n, 110)
	if n.Value != 40 {
		t.Fatalf("disabled pointer value = %v, want 40", n.Value)
	}
	if ControlKey(n, KeyRight) {
		t.Fatal("disabled slider accepted keyboard input")
	}
	if n.Value != 40 {
		t.Fatalf("disabled keyboard value = %v, want 40", n.Value)
	}
	SliderSet(n, 80)
	if n.Value != 40 {
		t.Fatalf("disabled direct set value = %v, want 40", n.Value)
	}
}

func TestMenuGeometryHonorsPaddingAndDeclaredSize(t *testing.T) {
	measure := func(string, TextAttrs) (int, int) { return 80, 18 }
	makeMenu := func() *Node {
		return &Node{Kind: KindMenu, Text: "All windows", Width: 220, Height: 36, Padding: 8}
	}

	columnMenu := makeMenu()
	if err := LayoutColumn(&Node{Kind: KindColumn, Children: []*Node{columnMenu}}, Rect{W: 220, H: 60}, measure); err != nil {
		t.Fatal(err)
	}
	if columnMenu.Bounds.W != 220 || columnMenu.Bounds.H != 36 {
		t.Errorf("column menu bounds = %+v, want 220×36 padded control", columnMenu.Bounds)
	}

	rowMenu := makeMenu()
	if err := Layout(&Node{Kind: KindRow, Children: []*Node{rowMenu}}, Rect{W: 220, H: 60}, measure); err != nil {
		t.Fatal(err)
	}
	if rowMenu.Bounds.W != 220 || rowMenu.Bounds.H != 36 {
		t.Errorf("row menu bounds = %+v, want 220×36 padded control", rowMenu.Bounds)
	}
}

func TestControlDimensionsFollowDensityAndRespectExplicitSizes(t *testing.T) {
	for i, density := range theme.Densities() {
		t.Run(string(density), func(t *testing.T) {
			m, _ := theme.MetricsFor(density)
			want := [][3]int{{36, 22, 28}, {44, 27, 36}, {52, 32, 44}, {60, 37, 52}, {80, 49, 68}}[i]
			toggle := &Node{Kind: KindToggle}
			slider := &Node{Kind: KindSlider, Min: 0, Max: 100, Width: 160}
			explicit := &Node{Kind: KindSlider, Width: 90, Height: 20}
			checkbox := &Node{Kind: KindToggle, Role: "checkbox"}
			root := &Node{Kind: KindColumn, Children: []*Node{toggle, slider, explicit, checkbox}}
			SizeControls(root, m)
			row := &Node{Kind: KindRow, Children: []*Node{toggle, slider}}
			if err := Layout(row, Rect{W: 400, H: 100}, fakeMeasure); err != nil {
				t.Fatal(err)
			}
			if toggle.Bounds.W != want[0] || toggle.Bounds.H != want[1] || slider.Bounds.H != want[2] {
				t.Fatalf("toggle %v slider %v, want %v", toggle.Bounds, slider.Bounds, want)
			}
			if explicit.Width != 90 || explicit.Height != 20 || checkbox.Width != 0 || checkbox.Height != 0 {
				t.Fatal("sizing changed an explicit size or checkbox")
			}
			row = &Node{Kind: KindRow, Children: []*Node{slider}}
			if err := Layout(row, Rect{W: 180, H: 20}, func(string, TextAttrs) (int, int) { return 8, 16 }); err != nil {
				t.Fatal(err)
			}
			if slider.Bounds.H != 20 {
				t.Fatalf("slider height %d, want row clamp 20", slider.Bounds.H)
			}
		})
	}
}

func TestControlSizingTracksDensityChangesOnRetainedNodes(t *testing.T) {
	toggle := &Node{Kind: KindToggle}
	slider := &Node{Kind: KindSlider, Min: 0, Max: 100, Width: 160}
	root := &Node{Kind: KindRow, Children: []*Node{toggle, slider}}
	for _, test := range []struct {
		density          theme.Density
		toggleH, sliderH int
	}{{theme.DensityMini, 22, 28}, {theme.DensitySpacious, 49, 68}} {
		m, _ := theme.MetricsFor(test.density)
		SizeControls(root, m)
		if err := Layout(root, Rect{W: 400, H: 100}, fakeMeasure); err != nil {
			t.Fatal(err)
		}
		if toggle.Bounds.H != test.toggleH || slider.Bounds.H != test.sliderH {
			t.Errorf("%s: toggle height %d slider height %d", test.density, toggle.Bounds.H, slider.Bounds.H)
		}
	}
	if toggle.Width != 0 || toggle.Height != 0 || slider.Height != 0 {
		t.Error("inferred size overwrote a declared dimension")
	}
}

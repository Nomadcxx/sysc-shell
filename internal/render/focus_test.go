package render

import (
	"bytes"
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"image"
	"testing"
)

func TestM3FocusAndDragLayers(t *testing.T) {
	fg := Color{R: 31, G: 97, B: 163, A: 255}
	for _, tc := range []struct {
		name  string
		state ui.Interaction
		alpha uint8
	}{
		{"rest", 0, 0}, {"hover", ui.StateHovered, 20}, {"focus", ui.StateFocused, 26},
		{"press", ui.StateFocused | ui.StatePressed, 31},
		{"drag", ui.StateFocused | ui.StatePressed | ui.StateDragged, 41},
		{"disabled", ui.StateDisabled | ui.StateHovered | ui.StateFocused | ui.StatePressed | ui.StateDragged, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, got := range map[string]Color{"static": stateLayer(fg, tc.state), "animated": interactionLayer(fg, &ui.Node{State: tc.state})} {
				if got.A != tc.alpha {
					t.Errorf("%s alpha = %d, want %d", name, got.A, tc.alpha)
				}
				if got.A > 0 && (got.R != fg.R || got.G != fg.G || got.B != fg.B) {
					t.Errorf("%s lost paired ink: %+v", name, got)
				}
			}
		})
	}
	for _, tc := range []struct {
		name         string
		state        ui.Interaction
		hover, press float64
		alpha        uint8
	}{
		{"focus beats fading press", ui.StateFocused | ui.StatePressed, 0, .5, 26},
		{"press beats focus", ui.StateFocused | ui.StatePressed, 0, .95, 29},
		{"drag beats full press", ui.StateDragged | ui.StatePressed, 0, 1, 41},
		{"clamped hover", ui.StateHovered, 2, 0, 20},
		{"negative fallback", ui.StatePressed, 0, -1, 31},
		{"hover fade", ui.StateHovered, .5, 0, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := interactionLayer(fg, &ui.Node{State: tc.state, HoverProgress: tc.hover, PressProgress: tc.press})
			if got.A != tc.alpha {
				t.Fatalf("alpha=%d, want %d", got.A, tc.alpha)
			}
		})
	}
}

func focusCanvas(t *testing.T, root *ui.Node, style Style) *Canvas {
	t.Helper()
	style.Body = ui.Rect{W: 160, H: 140}
	c := newTestCanvas(t, style.Scale120.Physical(160), style.Scale120.Physical(140))
	if err := Paint(c, root, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestM3FocusRingGeometry(t *testing.T) {
	for _, mode := range compositionModes() {
		for _, scale := range []ui.Scale120{120, 150} {
			style := mode.style
			style.Scale120 = scale
			style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
			for _, tc := range []struct {
				name string
				n    ui.Node
			}{
				{"full", ui.Node{Kind: ui.KindButton, Radius: 0}},
				{"explicit radius", ui.Node{Kind: ui.KindButton, Radius: 3}},
				{"square", ui.Node{Kind: ui.KindRow}},
				{"search", ui.Node{Kind: ui.KindTextField, Name: "Search"}},
				{"multiline", ui.Node{Kind: ui.KindTextField, Multiline: true}},
				{"disabled destination", ui.Node{Kind: ui.KindButton, State: ui.StateDisabled, AriaDisabled: true}},
			} {
				t.Run(mode.name+"/"+tc.name+"/"+fmt.Sprint(scale), func(t *testing.T) {
					n := tc.n
					n.Bounds = ui.Rect{X: 35, Y: 25, W: 60, H: 30}
					n.State |= ui.StateFocused
					root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{&n}}
					c := focusCanvas(t, root, style)
					x := scale.Physical(65)
					for y := 20; y < 23; y++ {
						if got := pixelAt(t, c, x, scale.Physical(y)); got != style.Secondary {
							t.Fatalf("ring at y=%d = %+v want Secondary", y, got)
						}
					}
					for y := 23; y < 25; y++ {
						if got := pixelAt(t, c, x, scale.Physical(y)); got != style.rootFill() {
							t.Fatalf("outward gap y=%d=%+v", y, got)
						}
					}
					n.State &^= ui.StateFocused
					plain := focusCanvas(t, root, style)
					if pixelAt(t, plain, x, scale.Physical(21)) == style.Secondary {
						t.Fatal("unfocused ring")
					}
				})
			}
		}
	}
}

func TestM3FocusRingClipping(t *testing.T) {
	style := testStyle
	style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
	style.Radius = 0
	n := &ui.Node{Kind: ui.KindButton, Bounds: ui.Rect{X: 35, Y: 25, W: 60, H: 30}, State: ui.StateFocused}
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{n}}
	t.Run("ring-only damage", func(t *testing.T) {
		full := focusCanvas(t, root, style)
		style.Body = ui.Rect{W: 160, H: 140}
		c := newTestCanvas(t, 160, 140)
		for i := range c.Pix {
			c.Pix[i] = 73
		}
		damage := ui.Rect{X: 55, Y: 20, W: 12, H: 3}
		if err := PaintRegion(c, root, NewTextRenderer(mustTestFace(t)), style, damage); err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 140; y++ {
			for x := 0; x < 160; x++ {
				at := y*c.Stride + x*4
				if damage.Contains(x, y) {
					if !bytes.Equal(c.Pix[at:at+4], full.Pix[at:at+4]) {
						t.Fatalf("damage differs at%d,%d", x, y)
					}
				} else if !bytes.Equal(c.Pix[at:at+4], []byte{73, 73, 73, 73}) {
					t.Fatalf("escaped damage at%d,%d", x, y)
				}
			}
		}
	})
	t.Run("nested clips", func(t *testing.T) {
		inner := &ui.Node{Kind: ui.KindScroll, Bounds: ui.Rect{X: 10, Y: 10, W: 120, H: 100}, Children: []*ui.Node{n}}
		outer := &ui.Node{Kind: ui.KindScroll, Bounds: ui.Rect{X: 10, Y: 23, W: 120, H: 80}, Children: []*ui.Node{inner}}
		c := focusCanvas(t, outer, style)
		if got := pixelAt(t, c, 65, 21); got == style.Secondary {
			t.Fatal("nested viewport widened ancestor clip")
		}
	})
	t.Run("explicit clip", func(t *testing.T) {
		n.ClipBounds = true
		defer func() { n.ClipBounds = false }()
		c := focusCanvas(t, root, style)
		if pixelAt(t, c, 65, 21) == style.Secondary {
			t.Fatal("escaped explicit clip")
		}
	})
	t.Run("opacity keeps exterior and clips content", func(t *testing.T) {
		n.Opacity = 50
		defer func() { n.Opacity = 0 }()
		c := focusCanvas(t, root, style)
		want := overlay(style.rootFill(), style.Secondary, .5)
		if got := pixelAt(t, c, 65, 21); !nearColour(got, want, 2) {
			t.Fatalf("exterior opacity=%+v want %+v", got, want)
		}
	})
	t.Run("opacity parent at child edge", func(t *testing.T) {
		parent := &ui.Node{Kind: ui.KindColumn, Opacity: 50, Bounds: n.Bounds, Children: []*ui.Node{n, {Kind: ui.KindCapsule, Bounds: ui.Rect{X: 100, Y: 20, W: 20, H: 60}}}}
		c := focusCanvas(t, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{parent}}, style)
		if !nearColour(pixelAt(t, c, 65, 21), overlay(style.rootFill(), style.Secondary, .5), 2) {
			t.Fatal("parent clipped exterior ring")
		}
		if got := pixelAt(t, c, 105, 30); got != style.rootFill() {
			t.Fatalf("opacity expansion released content: %+v", got)
		}
	})
	t.Run("root silhouette", func(t *testing.T) {
		edge := *n
		edge.Bounds = ui.Rect{X: 2, Y: 2, W: 60, H: 30}
		rounded := style
		rounded.Radius = 20
		c := focusCanvas(t, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{&edge}}, rounded)
		if pixelAt(t, c, 0, 0).A != 0 {
			t.Fatal("ring escaped root corner")
		}
	})
}

func nearColour(a, b Color, tolerance int) bool {
	for _, pair := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}, {a.A, b.A}} {
		if abs(int(pair[0])-int(pair[1])) > tolerance {
			return false
		}
	}
	return true
}

func TestM3FocusOpacityRetainsWeatherClip(t *testing.T) {
	style := testStyle
	style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
	button := &ui.Node{Kind: ui.KindButton, Bounds: ui.Rect{X: 35, Y: 25, W: 60, H: 30}, State: ui.StateFocused}
	effect := &ui.Node{Kind: ui.KindEffect, Bounds: ui.Rect{X: 25, Y: 15, W: 80, H: 50}, Effect: rainSpec(7), EffectPhase: .25}
	group := &ui.Node{Kind: ui.KindColumn, Bounds: button.Bounds, Opacity: 50, Children: []*ui.Node{button, effect}}
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{group}}
	focused := focusCanvas(t, root, style)
	button.State = 0
	plain := focusCanvas(t, root, style)
	// y=24 is the required clear gap, not ring or group content.
	for x := 58; x < 72; x++ {
		got, want := pixelAt(t, focused, x, 24), pixelAt(t, plain, x, 24)
		if got != want {
			t.Fatalf("focus expansion released weather content at %d,24: got %+v, want %+v", x, got, want)
		}
	}
}

func TestM3FocusTabKeepsItsPaintedCorners(t *testing.T) {
	style := testStyle
	style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
	tab := &ui.Node{Kind: ui.KindTab, Bounds: ui.Rect{X: 35, Y: 25, W: 60, H: 30}, State: ui.StateFocused}
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{tab}}
	plain := focusCanvas(t, root, style)
	tab.State |= ui.StatePressed
	tab.PressProgress = 1
	pressed := focusCanvas(t, root, style)
	for y := 0; y < 140; y++ {
		for x := 0; x < 160; x++ {
			if !tab.Bounds.Contains(x, y) && pixelAt(t, plain, x, y) != pixelAt(t, pressed, x, y) {
				t.Fatalf("Tab ring morphed at%d,%d while its wash corners stayed fixed", x, y)
			}
		}
	}
}

func TestOpacityContentClipAppliesToDirectPixels(t *testing.T) {
	box := ui.Rect{W: 4, H: 4}
	mask := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for i := range mask.Pix {
		mask.Pix[i] = 255
	}
	for _, clip := range []ui.Rect{{X: 1, Y: 1, W: 1, H: 1}, {}} {
		for _, paint := range []func(*Canvas){
			func(c *Canvas) { blendWeatherPixel(c, box, mask, 0, 0, Color{R: 255, A: 255}, 255) },
			func(c *Canvas) { blendWeatherRow(c, box, mask, 0, 0, 4, Color{R: 255, A: 255}) },
			func(c *Canvas) { blendCoverage(c, 0, 0, Color{R: 255, A: 255}, 1) },
		} {
			c := newTestCanvas(t, 4, 4)
			c.contentClip = &clip
			paint(c)
			if !bytes.Equal(c.Pix, make([]byte, len(c.Pix))) {
				t.Fatal("direct pixel painter escaped opacity content clip")
			}
		}
	}
}

func TestM3FocusRingPopupOrder(t *testing.T) {
	style := testStyle
	style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
	menu := &ui.Node{Kind: ui.KindMenu, Text: "Theme", Padding: 4, Bounds: ui.Rect{X: 8, Y: 8, W: 120, H: 24}, Children: []*ui.Node{{Kind: ui.KindText, Text: "Option", Bounds: ui.Rect{X: 8, Y: 32, W: 120, H: 24}}}}
	button := &ui.Node{Kind: ui.KindButton, State: ui.StateFocused, Bounds: ui.Rect{X: 35, Y: 40, W: 60, H: 30}}
	c := focusCanvas(t, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{menu, button}}, style)
	if got := pixelAt(t, c, 65, 36); got != style.containerHighest() {
		t.Fatalf("ring painted through popup: %+v", got)
	}
	if got := pixelAt(t, c, 65, 73); got != style.Secondary {
		t.Fatalf("uncovered ring missing: %+v", got)
	}
}

func TestM3FocusRingUsesResolvedShape(t *testing.T) {
	style := testStyle
	style.Shapes = Shapes{Small: 4, Medium: 12, Large: 28, Card: 10}
	style.CardRadius = 10
	for _, tc := range []struct {
		name   string
		node   ui.Node
		radius int
	}{
		{"full", ui.Node{Kind: ui.KindButton}, 20},
		{"explicit", ui.Node{Kind: ui.KindButton, Radius: 3}, 8},
		{"shape role", ui.Node{Kind: ui.KindButton, Shape: ui.ShapeSmall}, 9},
		{"square row", ui.Node{Kind: ui.KindRow}, 5},
		{"search", ui.Node{Kind: ui.KindTextField, Name: "Search"}, 20},
		{"multiline", ui.Node{Kind: ui.KindTextField, Multiline: true}, 17},
		{"card", ui.Node{Kind: ui.KindCapsule, Shape: ui.ShapeCard, Fill: ui.FillContainerHigh}, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.node.Bounds = ui.Rect{X: 35, Y: 25, W: 60, H: 30}
			box, radius := focusGeometry(style, &tc.node)
			if box != (ui.Rect{X: 30, Y: 20, W: 70, H: 40}) || radius != tc.radius {
				t.Fatalf("shape=%+v radius%d want%d", box, radius, tc.radius)
			}
		})
	}
}

// A ring reaches FocusOutset past its control. Rails stack items closer than
// that, and the item painted next must not cover the focused item's ring.
func TestM3FocusRingPaintsAboveLaterSiblings(t *testing.T) {
	style := testStyle
	style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
	focused := &ui.Node{Kind: ui.KindButton, Bounds: ui.Rect{X: 35, Y: 25, W: 60, H: 30}, State: ui.StateFocused}
	next := &ui.Node{Kind: ui.KindButton, Fill: ui.FillAccent, Bounds: ui.Rect{X: 35, Y: 57, W: 60, H: 30}}
	for name, root := range map[string]*ui.Node{
		"siblings": {Kind: ui.KindColumn, Children: []*ui.Node{focused, next}},
		"cousins": {Kind: ui.KindColumn, Children: []*ui.Node{
			{Kind: ui.KindColumn, Bounds: focused.Bounds, Children: []*ui.Node{focused}},
			{Kind: ui.KindColumn, Bounds: next.Bounds, Children: []*ui.Node{next}},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			c := focusCanvas(t, root, style)
			for y := 57; y < 60; y++ {
				if got := pixelAt(t, c, 65, y); got != style.Secondary {
					t.Fatalf("ring at y=%d = %+v, covered by the next item", y, got)
				}
			}
		})
	}
}

// Stacked navigation items take Material's inward ring: the stroke lies
// inside the item's own shape, so it needs no gap around the item.
func TestM3FocusRingInward(t *testing.T) {
	style := testStyle
	style.Secondary = Color{R: 227, G: 41, B: 119, A: 255}
	n := &ui.Node{Kind: ui.KindButton, Bounds: ui.Rect{X: 35, Y: 25, W: 60, H: 30}, State: ui.StateFocused, FocusInward: true}
	c := focusCanvas(t, &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{n}}, style)
	for y := 25; y < 28; y++ {
		if got := pixelAt(t, c, 65, y); got != style.Secondary {
			t.Fatalf("inward ring at y=%d = %+v want Secondary", y, got)
		}
	}
	for y := 20; y < 25; y++ {
		if got := pixelAt(t, c, 65, y); got == style.Secondary {
			t.Fatalf("inward ring painted outside the item at y=%d", y)
		}
	}
	if got := focusPaintBounds(n, style); got != style.Scale120.PhysicalRect(n.Bounds) {
		t.Fatalf("inward ring paint bounds = %+v, want the item's own box", got)
	}
}

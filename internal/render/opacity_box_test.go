package render

import (
	"bytes"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// paintNodeOpacitySurfaceSized is the algorithm paintNodeOpacity used before
// its offscreen was sized to the node's clipped box: a buffer as large as the
// whole surface, and a composite that reads the node's box back out of it.
//
// It is kept here as the reference the box-sized path must reproduce byte for
// byte. The equivalence is structural -- the composite below never reads a
// pixel outside x0..x1, y0..y1, so everything the subtree painted elsewhere in
// the surface-sized buffer was already being discarded -- and this pins it to
// actual bytes rather than an argument.
func paintNodeOpacitySurfaceSized(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int) error {
	offscreen, err := NewCanvas(make([]byte, len(c.Pix)), c.Width, c.Height, c.Stride)
	if err != nil {
		return err
	}
	offscreen.restrict = c.restrict
	if err := paintNodeContent(offscreen, n, text, style, size); err != nil {
		return err
	}
	box := style.Scale120.PhysicalRect(n.Bounds)
	x0, y0, x1, y1 := c.clip(box)
	opacity := uint32(n.Opacity)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			srcAt := y*offscreen.Stride + x*4
			if srcAt+4 > len(offscreen.Pix) {
				continue
			}
			alpha := (uint32(offscreen.Pix[srcAt+3])*opacity + 50) / 100
			if alpha == 0 {
				continue
			}
			var src [4]byte
			for i := range src {
				src[i] = byte((uint32(offscreen.Pix[srcAt+i])*opacity + 50) / 100)
			}
			dstAt := y*c.Stride + x*4
			if dstAt+4 <= len(c.Pix) {
				blendPixel(c.Pix[dstAt:dstAt+4], src, alpha)
			}
		}
	}
	return nil
}

// opacityTree builds a small group with one overlapping pair of children and a
// third on top, in the shape the control-centre media card uses: a column that
// carries the group's opacity, wrapping content that overlaps itself.
func opacityTree() *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Opacity: 80, Bounds: ui.Rect{X: 12, Y: 8, W: 60, H: 40}, Children: []*ui.Node{
		{Kind: ui.KindRow, Fill: ui.FillAccent, Bounds: ui.Rect{X: 12, Y: 8, W: 60, H: 24}},
		{Kind: ui.KindRow, Fill: ui.FillError, Bounds: ui.Rect{X: 24, Y: 16, W: 36, H: 24}},
		{Kind: ui.KindRow, Fill: ui.FillContainer, Bounds: ui.Rect{X: 40, Y: 30, W: 28, H: 14}},
	}}
}

// opacityCases are the shapes the box-sized offscreen must reproduce. Each is
// rendered twice on two identical canvases -- once through the box-sized path
// and once through the surface-sized reference -- and the results compared.
func opacityCases() map[string]*ui.Node {
	return map[string]*ui.Node{
		"a group inside the surface": opacityTree(),
		// The node reaches past the right and bottom edges, so its clipped box
		// is smaller than its bounds and the subtree paints off-surface.
		"a group past the surface edge": {Kind: ui.KindColumn, Opacity: 65,
			Bounds: ui.Rect{X: 40, Y: 20, W: 80, H: 60}, Children: []*ui.Node{
				{Kind: ui.KindRow, Fill: ui.FillAccent, Bounds: ui.Rect{X: 40, Y: 20, W: 80, H: 60}},
				{Kind: ui.KindRow, Fill: ui.FillError, Bounds: ui.Rect{X: 70, Y: 40, W: 60, H: 40}},
			}},
		// A group whose children paint outside its own bounds. The surface-sized
		// buffer captured them, but the composite never read past the box, so
		// neither path may show them.
		"a child overflowing its group": {Kind: ui.KindColumn, Opacity: 40,
			Bounds: ui.Rect{X: 20, Y: 10, W: 30, H: 20}, Children: []*ui.Node{
				{Kind: ui.KindRow, Fill: ui.FillAccent, Bounds: ui.Rect{X: 0, Y: 0, W: 70, H: 40}},
			}},
		// A group holding another group: each level needs its own buffer, and
		// the inner one composites into the outer buffer rather than the surface.
		"a group inside a group": {Kind: ui.KindColumn, Opacity: 70,
			Bounds: ui.Rect{X: 10, Y: 6, W: 70, H: 50}, Children: []*ui.Node{
				{Kind: ui.KindColumn, Opacity: 50, Bounds: ui.Rect{X: 16, Y: 10, W: 40, H: 30}, Children: []*ui.Node{
					{Kind: ui.KindRow, Fill: ui.FillContainerHigh, Bounds: ui.Rect{X: 16, Y: 10, W: 40, H: 30}},
					{Kind: ui.KindRow, Fill: ui.FillError, Bounds: ui.Rect{X: 30, Y: 20, W: 40, H: 20}},
				}},
				{Kind: ui.KindRow, Fill: ui.FillAccent, Bounds: ui.Rect{X: 12, Y: 34, W: 60, H: 10}},
			}},
		// Opacity 100 is "no group", not a full-strength group: the contract
		// paintNode keeps, and the one the offscreen must not change.
		"full opacity is not a group": {Kind: ui.KindColumn, Opacity: 100,
			Bounds: ui.Rect{X: 10, Y: 10, W: 40, H: 20}, Children: []*ui.Node{
				{Kind: ui.KindRow, Fill: ui.FillError, Bounds: ui.Rect{X: 10, Y: 10, W: 40, H: 20}},
			}},
	}
}

// TestGroupOpacityIsUnchangedByTheBoxSizedOffscreen renders each shape through
// the new path and through the surface-sized reference and requires the two
// surfaces to agree byte for byte: alpha in canvas memory is premultiplied, so
// a one-channel drift on any pixel fails here.
func TestGroupOpacityIsUnchangedByTheBoxSizedOffscreen(t *testing.T) {
	t.Parallel()
	for name, tree := range opacityCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// One renderer per goroutine, as TextRenderer requires: its scratch
			// free list belongs to the surface being drawn, not to the type.
			text := NewTextRenderer(mustTestFace(t))
			style := testStyle
			style.Body = ui.Rect{W: 100, H: 80}
			style.Scale120 = ui.ScaleUnit

			want := newTestCanvas(t, 100, 80)
			fillRect(want, ui.Rect{W: 100, H: 80}, style.Background)
			if err := paintNodeOpacitySurfaceSized(want, tree, text, style, style.Size); err != nil {
				t.Fatal(err)
			}

			got := newTestCanvas(t, 100, 80)
			fillRect(got, ui.Rect{W: 100, H: 80}, style.Background)
			if err := paintNode(got, tree, text, style, style.Size); err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got.Pix, want.Pix) {
				for i := 0; i+3 < len(got.Pix); i += 4 {
					if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
						px, py := (i/4)%100, (i/4)/100
						t.Fatalf("pixel (%d, %d) = %v, surface-sized reference = %v", px, py,
							got.Pix[i:i+4], want.Pix[i:i+4])
					}
				}
				t.Fatal("surfaces differ")
			}
		})
	}
}

// TestGroupOpacitySkipsAGroupOutsideTheDamage pins the second half of the
// issue: a partial paint whose region misses the group must do no work for it.
// Before the box-sized offscreen, the group allocated and painted its subtree
// on every partial frame, and the leaf-only early-out could not help because
// the group has children.
func TestGroupOpacitySkipsAGroupOutsideTheDamage(t *testing.T) {
	text := NewTextRenderer(mustTestFace(t))
	style := testStyle
	style.Body = ui.Rect{W: 100, H: 80}
	tree := &ui.Node{Kind: ui.KindColumn, Opacity: 80,
		Bounds: ui.Rect{X: 40, Y: 30, W: 40, H: 30}, Children: []*ui.Node{
			{Kind: ui.KindRow, Fill: ui.FillAccent, Bounds: ui.Rect{X: 40, Y: 30, W: 40, H: 30}},
			{Kind: ui.KindColumn, Opacity: 50, Bounds: ui.Rect{X: 44, Y: 34, W: 20, H: 20}, Children: []*ui.Node{
				{Kind: ui.KindRow, Fill: ui.FillError, Bounds: ui.Rect{X: 44, Y: 34, W: 20, H: 20}},
			}},
		}}

	c := newTestCanvas(t, 100, 80)
	fillRect(c, ui.Rect{W: 100, H: 80}, style.Background)
	before := bytes.Clone(c.Pix)
	// The damage lies in the opposite corner from the group's box.
	c.restrict = ui.Rect{X: 0, Y: 0, W: 20, H: 20}

	allocs := testing.AllocsPerRun(20, func() {
		if err := paintNode(c, tree, text, style, style.Size); err != nil {
			t.Error(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("painting outside the group's box allocated %.1f times a frame, want 0", allocs)
	}
	if !bytes.Equal(c.Pix, before) {
		t.Fatal("a paint outside the group's box changed pixels")
	}
}

// TestGroupOpacityReusesItsOffscreen keeps the steady state free of
// allocation. A panel repaints the same page every frame, so the buffer the
// first frame grew is the buffer every later frame wants.
func TestGroupOpacityReusesItsOffscreen(t *testing.T) {
	text := NewTextRenderer(mustTestFace(t))
	style := testStyle
	style.Body = ui.Rect{W: 100, H: 80}
	tree := opacityTree()

	c := newTestCanvas(t, 100, 80)
	paint := func() {
		fillRect(c, ui.Rect{W: 100, H: 80}, style.Background)
		if err := paintNode(c, tree, text, style, style.Size); err != nil {
			t.Error(err)
		}
	}
	paint() // warm the free list
	allocs := testing.AllocsPerRun(20, paint)
	if allocs > 2 {
		t.Fatalf("a repeated group-opacity frame allocated %.1f times, want the buffer reused (about 0)", allocs)
	}

	// The buffer must not carry pixels between frames: an offscreen that was
	// not cleared would composite the previous frame's content twice.
	first := bytes.Clone(c.Pix)
	paint()
	if !bytes.Equal(c.Pix, first) {
		t.Fatal("a repeated frame differed: the reused offscreen was not cleared")
	}
}

// TestNestedGroupOpacityNeedsOneBufferPerLevel checks the free list is a list,
// not a single buffer: the inner group must be able to take its own while the
// outer one holds one.
func TestNestedGroupOpacityNeedsOneBufferPerLevel(t *testing.T) {
	t.Parallel()
	text := NewTextRenderer(mustTestFace(t))
	style := testStyle
	style.Body = ui.Rect{W: 100, H: 80}
	c := newTestCanvas(t, 100, 80)
	fillRect(c, ui.Rect{W: 100, H: 80}, style.Background)

	// Three levels, each smaller than the one outside it.
	inner := &ui.Node{Kind: ui.KindRow, Fill: ui.FillError, Bounds: ui.Rect{X: 30, Y: 30, W: 10, H: 10}}
	mid := &ui.Node{Kind: ui.KindColumn, Opacity: 60, Bounds: ui.Rect{X: 24, Y: 24, W: 24, H: 24},
		Children: []*ui.Node{inner}}
	outer := &ui.Node{Kind: ui.KindColumn, Opacity: 60, Bounds: ui.Rect{X: 16, Y: 16, W: 40, H: 40},
		Children: []*ui.Node{mid}}

	if err := paintNode(c, outer, text, style, style.Size); err != nil {
		t.Fatal(err)
	}
	// The reference algorithm must agree byte for byte, nested case included.
	want := newTestCanvas(t, 100, 80)
	fillRect(want, ui.Rect{W: 100, H: 80}, style.Background)
	if err := paintNodeOpacitySurfaceSized(want, outer, text, style, style.Size); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.Pix, want.Pix) {
		t.Fatal("the nested group differed from the surface-sized reference")
	}
	// Nested group opacities multiply: the inner source reaches the surface
	// once at 60% of 60%, not twice at 60%.
	got := pixelAt(t, c, 34, 34)
	if wantPixel := overlay(style.Background, style.Error, 0.36); got != wantPixel {
		t.Fatalf("nested group pixel = %+v, want %+v", got, wantPixel)
	}
	if len(text.scratch) < 2 {
		t.Fatalf("nested groups left %d buffers free, want at least 2 (one per level)", len(text.scratch))
	}
}

package render

import (
	"fmt"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A group-opacity node used to allocate an offscreen the size of the whole
// surface and paint its subtree into it whether or not the damage touched the
// node, so a panel frame at 2x paid about 6.3 MiB and 1.5-2 ms for one
// `Opacity: 80` wrapper. These benchmarks pin the replacement: the offscreen
// is the node's clipped box, it is reused between frames, and a partial paint
// that misses the node does no work at all.
//
// The canvas is the size of a scaled-up output so the difference between a
// box-sized buffer and a surface-sized one shows up in B/op.

const (
	opacityBenchW = 900
	opacityBenchH = 1600
	// The node is 400x120 at the top left and the probe damage below is
	// 400x60 at the bottom right, so the two never overlap.
	opacityBenchNode = 400
	opacityBenchRow  = 120
)

func benchText(b *testing.B) *TextRenderer {
	b.Helper()
	face, err := ParseFace(goregular.TTF)
	if err != nil {
		b.Fatal(err)
	}
	return NewTextRenderer(face)
}

// opacityBenchTree puts the group under a root node, which is where a panel
// keeps it: paint() walks root.Children through paintNode, so a group on the
// root itself would never take the opacity branch at all.
func opacityBenchTree(scale int, opacity int) *ui.Node {
	box := func(x, y, w, h int) ui.Rect {
		return ui.Rect{X: x * scale, Y: y * scale, W: w * scale, H: h * scale}
	}
	group := &ui.Node{
		Kind: ui.KindColumn, Opacity: uint8(opacity),
		Bounds: box(0, 0, opacityBenchNode, opacityBenchRow),
		Children: []*ui.Node{
			{Kind: ui.KindRow, Fill: ui.FillAccent, Bounds: box(0, 0, opacityBenchNode, opacityBenchRow/3)},
			{Kind: ui.KindRow, Fill: ui.FillError, Bounds: box(20, 20, opacityBenchNode-80, opacityBenchRow/2)},
			{Kind: ui.KindRow, Fill: ui.FillContainer, Bounds: box(60, 60, opacityBenchNode-120, opacityBenchRow/3)},
		},
	}
	return &ui.Node{
		Kind:   ui.KindColumn,
		Bounds: ui.Rect{W: opacityBenchW, H: opacityBenchH},
		Children: []*ui.Node{
			group,
		},
	}
}

func opacityBenchCanvas(b *testing.B) *Canvas {
	b.Helper()
	c, err := NewCanvas(make([]byte, opacityBenchW*opacityBenchH*4), opacityBenchW, opacityBenchH, opacityBenchW*4)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

func opacityBenchStyle() Style {
	style := testStyle
	style.Body = ui.Rect{W: opacityBenchW, H: opacityBenchH}
	return style
}

// BenchmarkOpacityGroupFullPaint is the whole-frame case: every pixel is
// repainted and the group is on the surface, so the offscreen is genuinely
// needed. With the buffer reused the steady-state cost is the box, not the
// surface.
func BenchmarkOpacityGroupFullPaint(b *testing.B) {
	for _, scale := range []int{1, 2} {
		for _, opacity := range []int{0, 80} {
			b.Run(fmt.Sprintf("scale=%d/opacity=%d/full", scale, opacity), func(b *testing.B) {
				c := opacityBenchCanvas(b)
				root := opacityBenchTree(scale, opacity)
				style := opacityBenchStyle()
				text := benchText(b)
				// Warm the mask cache and the scratch free list so the loop
				// measures steady-state paint, not first-frame setup.
				if err := Paint(c, root, text, style); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := Paint(c, root, text, style); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkOpacityGroupDamageElsewhere is the case the issue calls out: the
// damage rectangle lies entirely outside the group, so painting for it is
// waste. A group that clips to its own box can skip the subtree outright.
func BenchmarkOpacityGroupDamageElsewhere(b *testing.B) {
	for _, scale := range []int{1, 2} {
		for _, opacity := range []int{0, 80} {
			b.Run(fmt.Sprintf("scale=%d/opacity=%d/damage", scale, opacity), func(b *testing.B) {
				c := opacityBenchCanvas(b)
				root := opacityBenchTree(scale, opacity)
				style := opacityBenchStyle()
				text := benchText(b)
				damage := ui.Rect{X: 400, Y: 1200, W: 400, H: 60}
				if err := PaintRegion(c, root, text, style, damage); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := PaintRegion(c, root, text, style, damage); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

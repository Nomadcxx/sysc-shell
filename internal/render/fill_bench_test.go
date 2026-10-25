package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// BenchmarkFillRounded pins the fill cost to the corner arcs rather than to
// the rectangle it fills. The panel body covers 3.7M pixels at scale 2, and
// scanning the whole box for coverage made every Settings frame pay for
// coverage that is full everywhere but the corners: 32.5 ms of a 33.5 ms
// frame, half of it flat in blendMaskAt (GH #455). The cases cover the
// acceptance shapes at scale 2, both alphas, a radius of zero, and a fill that
// straddles the canvas so the clip runs inside the bands.
func BenchmarkFillRounded(b *testing.B) {
	cases := []struct {
		name    string
		cw, ch  int
		rect    ui.Rect
		radius  int
		col     Color
		cleared bool
	}{
		{"settings body 2x opaque", 2240, 1640, ui.Rect{W: 2240, H: 1640}, 24,
			Color{R: 0x20, G: 0x24, B: 0x2c, A: 0xff}, false},
		{"settings body 2x translucent", 2240, 1640, ui.Rect{W: 2240, H: 1640}, 24,
			Color{R: 0x20, G: 0x24, B: 0x2c, A: 0xe6}, false},
		{"settings body 2x translucent over clear", 2240, 1640, ui.Rect{W: 2240, H: 1640}, 24,
			Color{R: 0x20, G: 0x24, B: 0x2c, A: 0xe6}, true},
		{"bar fill 2x translucent", 2880, 120, ui.Rect{W: 2880, H: 120}, 16,
			Color{R: 0x10, G: 0x14, B: 0x18, A: 0xe6}, false},
		{"bar fill 2x translucent over clear", 2880, 120, ui.Rect{W: 2880, H: 120}, 16,
			Color{R: 0x10, G: 0x14, B: 0x18, A: 0xe6}, true},
		{"radius 0 wash", 2240, 1640, ui.Rect{W: 2240, H: 1640}, 0,
			Color{R: 0x00, G: 0x00, B: 0x00, A: 0x40}, false},
		{"clipped to the canvas", 1280, 720, ui.Rect{X: -100, Y: -80, W: 2240, H: 1640}, 24,
			Color{R: 0x20, G: 0x24, B: 0x2c, A: 0xe6}, false},
		{"small capsule", 300, 40, ui.Rect{W: 300, H: 40}, 20,
			Color{R: 0x3a, G: 0x41, B: 0x49, A: 0xd0}, false},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			c := seededCanvas(b, tc.cw, tc.ch, 3)
			if tc.cleared {
				clear(c.Pix)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if tc.cleared {
					// A frame repaints a buffer it has just cleared when
					// there is no backdrop behind the surface, so the clear
					// belongs in the loop: without it every iteration after
					// the first would blend over the previous fill.
					clear(c.Pix)
				}
				c.FillRounded(tc.rect, tc.radius, tc.col)
			}
		})
	}
}

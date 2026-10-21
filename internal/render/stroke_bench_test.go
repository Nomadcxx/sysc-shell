package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// BenchmarkStrokeRounded pins the stroke cost to the ring band rather than to
// the rectangle it outlines. A 2 px rim covers under 1.5 percent of its box, so
// scanning the whole rectangle for coverage makes every panel frame pay for the
// empty interior: the panel root rim strokes the whole surface and Settings
// frames already run past the 8 ms animator tick at scale 2 (GH #373).
func BenchmarkStrokeRounded(b *testing.B) {
	cases := []struct {
		name         string
		w, h, radius int
	}{
		{"440x800", 440, 800, 24},
		{"880x1600", 880, 1600, 24},
		{"1280x1440", 1280, 1440, 24},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			c := seededCanvas(b, tc.w, tc.h, 3)
			r := ui.Rect{X: 0, Y: 0, W: tc.w, H: tc.h}
			col := Color{R: 0x30, G: 0x80, B: 0xd0, A: 0xc0}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.StrokeRounded(r, tc.radius, 2, col)
			}
		})
	}
}

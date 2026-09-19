package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A stadium is a proportion rather than a radius: it resolves to half the
// shorter side whatever the surface's base radius is. The workspace pills ride
// on this, because a dot and the focused pill are the same node kind at two
// widths, and a square-cornered resolution here would paint the row as the
// toggle bar the shape family replaced.
func TestStadiumResolvesToHalfTheBox(t *testing.T) {
	t.Parallel()
	style := testStyle
	style.Shapes = Shapes{Small: 4, Medium: 8, Large: 12}
	cases := []struct {
		name    string
		shape   ui.Shape
		radius  int
		inherit int
		box     ui.Rect
		want    int
	}{
		{"dot is a circle", ui.ShapeStadium, 0, 8, ui.Rect{W: 20, H: 20}, 10},
		{"focused pill is a stadium", ui.ShapeStadium, 0, 8, ui.Rect{W: 40, H: 20}, 10},
		// The base radius cannot square a stadium off, in either direction.
		{"stadium beats a zero base", ui.ShapeStadium, 0, 0, ui.Rect{W: 40, H: 20}, 10},
		{"stadium beats a large base", ui.ShapeStadium, 0, 32, ui.Rect{W: 40, H: 20}, 10},
		// A named role still resolves to its own radius, capped by the box.
		{"medium keeps its radius", ui.ShapeMedium, 0, 32, ui.Rect{W: 40, H: 20}, 8},
		{"inherited radius is capped at half", ui.ShapeInherit, 0, 32, ui.Rect{W: 40, H: 20}, 10},
		{"explicit pixels win", ui.ShapeStadium, 3, 8, ui.Rect{W: 40, H: 20}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := &ui.Node{Kind: ui.KindCapsule, Shape: tc.shape, Radius: tc.radius}
			got := chromeRadius(style, nodeRadius(style, n, tc.inherit), tc.box)
			if got != tc.want {
				t.Errorf("radius = %d, want %d", got, tc.want)
			}
		})
	}
}

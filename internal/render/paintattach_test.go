package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// On a side bar the attached column of the panel body squares so the surface
// reads as one piece with the bar, and the free edge keeps its radius.
func TestTheAttachedColumnSquaresAndTheFreeEdgeRounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, edge string
	}{
		{name: "left", edge: "left"},
		{name: "right", edge: "right"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := testStyle
			style.AttachEdge = tc.edge
			style.Radius = 8
			style.Body = ui.Rect{X: 8, Y: 8, W: 60, H: 60}

			c := newTestCanvas(t, 76, 76)
			if err := Paint(c, &ui.Node{Kind: ui.KindColumn}, NewTextRenderer(mustTestFace(t)), style); err != nil {
				t.Fatalf("paint: %v", err)
			}

			attachedX := 8
			freeX := 67
			if tc.edge == "right" {
				attachedX, freeX = 67, 8
			}
			for _, y := range []int{8, 38, 67} {
				if got := pixelAt(t, c, attachedX, y); got.A != 255 {
					t.Fatalf("attached column pixel (%d, %d) = %v, want fully painted",
						attachedX, y, got)
				}
			}
			for _, y := range []int{8, 67} {
				if got := pixelAt(t, c, freeX, y); got.A != 0 {
					t.Fatalf("free corner pixel (%d, %d) = %v, want carved by the radius",
						freeX, y, got)
				}
			}
			if got := pixelAt(t, c, freeX, 38); got.A != 255 {
				t.Fatalf("free edge midpoint (%d, 38) = %v, want fully painted", freeX, got)
			}
			outsideX := 7
			if tc.edge == "right" {
				outsideX = 68
			}
			if got := pixelAt(t, c, outsideX, 38); got.A != 0 {
				t.Fatalf("pixel outside the body (%d, 38) = %v, want transparent", outsideX, got)
			}
		})
	}
}

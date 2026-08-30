package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Placement follows the panel design's rule: anchored off the bar edge,
// aligned to the triggering widget, clamped fully inside the output.
func TestTooltipPlacementClampsInsideTheOutput(t *testing.T) {
	t.Parallel()
	const outputWidth, outputHeight = 1920, 1080

	cases := []struct {
		name         string
		anchor       ui.Rect
		width        int
		wantXAtLeast int
		wantXAtMost  int
	}{
		{
			name:         "centred under its widget",
			anchor:       ui.Rect{X: 900, Y: 0, W: 40, H: 44},
			width:        200,
			wantXAtLeast: 0,
			wantXAtMost:  outputWidth - 200,
		},
		{
			name:         "clamped at the right edge",
			anchor:       ui.Rect{X: 1900, Y: 0, W: 20, H: 44},
			width:        200,
			wantXAtLeast: 0,
			wantXAtMost:  outputWidth - 200,
		},
		{
			name:         "clamped at the left edge",
			anchor:       ui.Rect{X: 0, Y: 0, W: 20, H: 44},
			width:        200,
			wantXAtLeast: 0,
			wantXAtMost:  outputWidth - 200,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := tooltipPlacement(c.anchor, c.width, 30, outputWidth, outputHeight)
			if got.X < c.wantXAtLeast || got.X > c.wantXAtMost {
				t.Fatalf("x = %d, want within [%d, %d]", got.X, c.wantXAtLeast, c.wantXAtMost)
			}
			if got.X+got.W > outputWidth {
				t.Fatalf("tooltip right edge %d exceeds the output", got.X+got.W)
			}
			if got.Y < c.anchor.Y+c.anchor.H {
				t.Fatalf("y = %d, want below the bar edge at %d", got.Y, c.anchor.Y+c.anchor.H)
			}
		})
	}
}

// A tooltip wider than the output is clamped to it rather than placed off it.
func TestATooltipWiderThanTheOutputIsClamped(t *testing.T) {
	t.Parallel()
	got := tooltipPlacement(ui.Rect{X: 10, Y: 0, W: 20, H: 44}, 3000, 30, 1920, 1080)

	if got.X != 0 {
		t.Fatalf("x = %d, want 0 for an over-wide tooltip", got.X)
	}
	if got.W > 1920 {
		t.Fatalf("width = %d, want clamped to the output", got.W)
	}
}

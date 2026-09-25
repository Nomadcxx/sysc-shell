package shell

import (
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestFloatingSurfaceDragUsesCursorPositionAcrossSurfaceMoves(t *testing.T) {
	// The pointer remains at the same surface-local point while the surface
	// follows it; adding each origin back recovers the cursor's screen motion.
	dx, dy := surfacePointerDelta(80, 30, 40, 20, 92, 42, 40, 20)
	if dx != 12 || dy != 12 {
		t.Fatalf("drag delta = %d,%d, want 12,12", dx, dy)
	}
}

func TestClampFloatingSurfaceKeepsItsRectangleOnOutput(t *testing.T) {
	got := clampPluginSurface(pluginSurfaceState{X: 1000, Y: 700, Width: 360, Height: 440}, 1280, 800)
	if got.X != 920 || got.Y != 360 || got.Width != 360 || got.Height != 440 {
		t.Fatalf("clamped surface = %+v", got)
	}
}

func TestFloatingSurfaceRectRejectsOverflowingPluginPosition(t *testing.T) {
	if pluginSurfaceRectFits(math.MaxInt, 0, 360, 440, 1280, 800) {
		t.Fatal("accepted a position that overflows addition-based bounds checks")
	}
	if !pluginSurfaceRectFits(920, 360, 360, 440, 1280, 800) {
		t.Fatal("rejected a rectangle that exactly fits the output edge")
	}
}

func TestFloatingSurfaceTreeShowsResizeGrip(t *testing.T) {
	p := &pluginSurfaceHost{title: "Note", panel: &PanelHost{theme: DefaultTheme()}}
	root := p.wrapTree(&ui.Node{Kind: ui.KindColumn, Fill: ui.FillNoteSun})
	last := root.Children[len(root.Children)-1]
	if last.Kind != ui.KindRow || len(last.Children) != 2 || last.Children[1].Kind != ui.KindIcon || last.Children[1].Icon != "drag_indicator" {
		t.Fatalf("resize grip = %+v", last)
	}
}

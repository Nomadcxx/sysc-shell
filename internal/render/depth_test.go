package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestOpacityScalesOnlyTheRootFill checks the surface alpha lands on the
// surface's own background and nowhere else. A nested card composites over the
// painted root, so a translucent panel must not drag its cards toward the
// wallpaper with it.
func TestOpacityScalesOnlyTheRootFill(t *testing.T) {
	t.Parallel()
	style := darkStyle()
	style.SurfaceOpacity = 0xe6 // 90 percent
	style.Radius = 0

	c := newTestCanvas(t, canvasW, canvasH)
	r := NewTextRenderer(mustTestFace(t))
	card := &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillContainerHigh,
		Bounds: ui.Rect{X: 10, Y: 10, W: 60, H: 30}, Width: 60, Height: 30}
	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{card}}
	if err := Paint(c, root, r, style); err != nil {
		t.Fatal(err)
	}

	want := style.Background
	want.A = 0xe6
	if got := pixelAt(t, c, 2, 2); got != premul(want) {
		t.Errorf("root fill = %+v, want the background at 90 percent %+v", got, premul(want))
	}
	// The card sits on the painted root at full strength.
	if got := pixelAt(t, c, 40, 25); got != style.Capsule {
		t.Errorf("card = %+v, want the opaque container %+v", got, style.Capsule)
	}
}

// TestOpacityUnsetStaysOpaque keeps a style assembled before the token usable.
// A zero alpha field means "not set", not "invisible".
func TestOpacityUnsetStaysOpaque(t *testing.T) {
	t.Parallel()
	style := darkStyle()
	style.SurfaceOpacity = 0
	if got := style.rootFill(); got != style.Background {
		t.Errorf("root fill = %+v, want the opaque background %+v", got, style.Background)
	}
}

// TestElevationSelectsTheShadowInk resolves the three levels onto the shadow
// role. None draws nothing at all; the other two differ in strength, and both
// take the palette's Shadow token rather than a hardcoded black.
func TestElevationSelectsTheShadowInk(t *testing.T) {
	t.Parallel()
	style := darkStyle()
	style.Shadow = Color{R: 0x00, G: 0x00, B: 0x00, A: 0xff}

	style.Elevation = theme.ElevationNone
	if _, ok := style.shadowInk(); ok {
		t.Error("elevation none still draws a shadow")
	}

	style.Elevation = theme.ElevationSubtle
	subtle, ok := style.shadowInk()
	if !ok {
		t.Fatal("elevation subtle draws no shadow")
	}
	style.Elevation = theme.ElevationStandard
	standard, ok := style.shadowInk()
	if !ok {
		t.Fatal("elevation standard draws no shadow")
	}
	if subtle.A >= standard.A {
		t.Errorf("subtle alpha %d is not lighter than standard %d", subtle.A, standard.A)
	}
	if subtle.R != style.Shadow.R || standard.R != style.Shadow.R {
		t.Error("shadow ink does not come from the Shadow token")
	}
}

// premul converts a straight colour to what the canvas stores, so a test can
// compare against a translucent fill it asked for.
func premul(c Color) Color {
	if c.A == 0xff {
		return c
	}
	f := func(v uint8) uint8 { return uint8(uint32(v) * uint32(c.A) / 255) }
	return Color{R: f(c.R), G: f(c.G), B: f(c.B), A: c.A}
}

package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestIconFillPaintsSixPixelBadgeInsideItsBounds(t *testing.T) {
	c := newTestCanvas(t, 28, 28)
	n := &ui.Node{
		Kind: ui.KindIcon, Icon: "notifications", IconSize: 20,
		Fill: ui.FillError, Bounds: ui.Rect{X: 4, Y: 4, W: 20, H: 20},
	}
	if err := paintIcon(c, n, NewTextRenderer(mustTestFace(t)), testStyle); err != nil {
		t.Fatal(err)
	}

	if got := pixelAt(t, c, 21, 7); got != testStyle.Error {
		t.Fatalf("badge centre = %+v, want Error %+v", got, testStyle.Error)
	}
	if got := pixelAt(t, c, 24, 7); got != (Color{}) {
		t.Fatalf("badge painted outside the icon bounds: %+v", got)
	}
}

package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// whiteMark is a 2×1 raster: an opaque white pixel beside a transparent one.
func whiteMark() *ui.Image {
	return &ui.Image{Width: 2, Height: 1, Stride: 8, Pix: []byte{
		0xff, 0xff, 0xff, 0xff,
		0, 0, 0, 0,
	}}
}

func TestTintedImagePaintsItsAlphaInTheTone(t *testing.T) {
	t.Parallel()
	style := testStyle
	for _, tc := range []struct {
		name string
		tone ui.Tone
		want Color
	}{
		{"normal", ui.ToneNormal, textColor(style, ui.ToneNormal)},
		{"error", ui.ToneError, style.Error},
	} {
		c := newTestCanvas(t, 4, 2)
		fillRect(c, ui.Rect{W: 4, H: 2}, style.Background)
		n := &ui.Node{Kind: ui.KindImage, Image: whiteMark(), ImageTint: true, Tone: tc.tone, Bounds: ui.Rect{W: 4, H: 2}}
		if err := paintNode(c, n, NewTextRenderer(mustTestFace(t)), style, style.Size); err != nil {
			t.Fatal(err)
		}
		if got := pixelAt(t, c, 0, 0); got != tc.want {
			t.Errorf("%s: inked pixel = %+v, want the tone %+v", tc.name, got, tc.want)
		}
		if got := pixelAt(t, c, 3, 0); got != style.Background {
			t.Errorf("%s: transparent pixel = %+v, want the background %+v", tc.name, got, style.Background)
		}
	}
}

func TestUntintedImageKeepsItsOwnColours(t *testing.T) {
	t.Parallel()
	style := testStyle
	c := newTestCanvas(t, 4, 2)
	fillRect(c, ui.Rect{W: 4, H: 2}, style.Background)
	n := &ui.Node{Kind: ui.KindImage, Image: whiteMark(), Tone: ui.ToneError, Bounds: ui.Rect{W: 4, H: 2}}
	if err := paintNode(c, n, NewTextRenderer(mustTestFace(t)), style, style.Size); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, c, 0, 0); got != (Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("untinted pixel = %+v, want the raster's white", got)
	}
}

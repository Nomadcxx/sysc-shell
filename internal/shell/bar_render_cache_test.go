package shell

import (
	"bytes"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestBarPaintCacheKeepsAlternatingBuffersPixelCorrect(t *testing.T) {
	bar := newTestBar(t)
	t.Cleanup(bar.stopAnimation)

	const width, height = 200, BarHeight
	style := bar.style
	style.Body = ui.Rect{W: width, H: height}
	root := func(title string) *ui.Node {
		return &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
			{Kind: ui.KindCapsule, Bounds: ui.Rect{X: 8, Y: 4, W: 126, H: 40}, Children: []*ui.Node{
				{Kind: ui.KindText, Text: title, Bounds: ui.Rect{X: 18, Y: 12, W: 106, H: 24}},
			}},
			{Kind: ui.KindCapsule, Bounds: ui.Rect{X: 142, Y: 4, W: 50, H: 40}, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "fixed", Bounds: ui.Rect{X: 146, Y: 12, W: 42, H: 24}},
			}},
		}}
	}

	first, second := root("first title"), root("second title")
	damage := barDamageRegions(first, second, style.Scale120, width, height)
	wantDamage := ui.Rect{X: 18, Y: 12, W: 106, H: 24}
	if len(damage) != 1 || damage[0] != wantDamage {
		t.Fatalf("damage = %+v, want only changed title bounds %+v", damage, wantDamage)
	}

	cache := barPaintCache{}
	pixelsA := make([]byte, width*height*4)
	canvasA, _ := render.NewCanvas(pixelsA, width, height, width*4)
	if err := cache.paint(canvasA, first, bar.text, style); err != nil {
		t.Fatal(err)
	}

	// A second compositor slot contains unrelated old pixels. The cache must
	// seed it with the latest complete frame before applying local damage.
	pixelsB := bytes.Repeat([]byte{0xa5}, len(pixelsA))
	canvasB, _ := render.NewCanvas(pixelsB, width, height, width*4)
	if err := cache.paint(canvasB, second, bar.text, style); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, len(pixelsB))
	wantCanvas, _ := render.NewCanvas(want, width, height, width*4)
	if err := render.Paint(wantCanvas, second, bar.text, style); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pixelsB, want) {
		t.Fatal("partial repaint in the second buffer differs from a full paint")
	}

	third := root("third title")
	if err := cache.paint(canvasA, third, bar.text, style); err != nil {
		t.Fatal(err)
	}
	want = make([]byte, len(pixelsA))
	wantCanvas, _ = render.NewCanvas(want, width, height, width*4)
	if err := render.Paint(wantCanvas, third, bar.text, style); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pixelsA, want) {
		t.Fatal("partial repaint in the first buffer differs from a full paint")
	}
}

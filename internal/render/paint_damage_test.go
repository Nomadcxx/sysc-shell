package render

import (
	"bytes"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPaintRegionRepaintsDamageWithoutTouchingTheRest(t *testing.T) {
	const width, height = 200, 48
	style := testStyle
	style.Body = ui.Rect{W: width, H: height}
	style.Radius = 14
	text := NewTextRenderer(mustTestFace(t))
	damage := ui.Rect{X: 20, Y: 6, W: 110, H: 34}
	old := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindText, Text: "old title", Bounds: damage,
	}}}
	next := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind: ui.KindText, Text: "new title", Bounds: damage,
	}}}

	got := make([]byte, width*height*4)
	canvas, err := NewCanvas(got, width, height, width*4)
	if err != nil {
		t.Fatal(err)
	}
	if err := Paint(canvas, old, text, style); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, len(got))
	wantCanvas, _ := NewCanvas(want, width, height, width*4)
	if err := Paint(wantCanvas, next, text, style); err != nil {
		t.Fatal(err)
	}

	// PaintRegion may rely on the caller's unchanged pixels. A sentinel outside
	// the damage catches root-level clearing that ignores the clip.
	const sentinel = 0x5a
	outside := (height - 1) * width * 4
	got[outside], got[outside+1], got[outside+2], got[outside+3] = sentinel, sentinel, sentinel, sentinel
	if err := PaintRegion(canvas, next, text, style, damage); err != nil {
		t.Fatal(err)
	}

	for y := damage.Y; y < damage.Y+damage.H; y++ {
		start, end := (y*width+damage.X)*4, (y*width+damage.X+damage.W)*4
		if !bytes.Equal(got[start:end], want[start:end]) {
			t.Fatalf("painted damage row %d differs from a full paint", y)
		}
	}
	if !bytes.Equal(got[outside:outside+4], []byte{sentinel, sentinel, sentinel, sentinel}) {
		t.Fatalf("paint changed pixels outside damage to %v", got[outside:outside+4])
	}
}

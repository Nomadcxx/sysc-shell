package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// inkSpan paints one icon node into a canvas and reports its ink's extent.
func inkSpan(t *testing.T, r *TextRenderer, n *ui.Node, scale ui.Scale120, w, h int) (x0, x1, y0, y1 int) {
	t.Helper()
	pix := make([]byte, w*h*4)
	c, err := NewCanvas(pix, w, h, w*4)
	if err != nil {
		t.Fatal(err)
	}
	style := Style{Scale120: scale, Foreground: Color{R: 255, G: 255, B: 255, A: 255}}
	if err := paintIcon(c, n, r, style); err != nil {
		t.Fatal(err)
	}
	x0, y0, x1, y1 = w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if pix[(y*w+x)*4+3] > 0 {
				x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			}
		}
	}
	return x0, x1, y0, y1
}

// The project font draws its glyphs flush left in an advance wider than the
// ink, so centring the advance put a close glyph 4.5 px left of its button's
// centre. A project glyph is centred on its ink instead.
func TestProjectIconsCentreOnTheirInk(t *testing.T) {
	t.Parallel()
	r := NewTextRendererWithFontMap(newSystemMap(t))
	for _, scale := range []ui.Scale120{120, 150} {
		for _, name := range []string{"close", "moonbit", "refresh", "camera", "github", "docker"} {
			box := ui.Rect{X: 10, Y: 10, W: 20, H: 20}
			n := &ui.Node{Kind: ui.KindIcon, Icon: name, IconSize: 20, IconProjectFirst: true, Bounds: box}
			x0, x1, _, _ := inkSpan(t, r, n, scale, scale.Physical(40), scale.Physical(40))
			pb := scale.PhysicalRect(box)
			centre := float64(pb.X) + float64(pb.W)/2
			ink := float64(x0+x1+1) / 2
			if d := ink - centre; d < -1 || d > 1 {
				t.Errorf("scale %d %s: ink %d..%d centres at %.1f, box centre %.1f", scale, name, x0, x1, ink, centre)
			}
		}
	}
}

// Material glyphs are drawn centred in their em, so their placement must not
// move.
func TestMaterialIconPlacementIsUnchanged(t *testing.T) {
	t.Parallel()
	r := NewTextRendererWithFontMap(newSystemMap(t))
	n := &ui.Node{Kind: ui.KindIcon, Icon: "settings", IconSize: 20, Bounds: ui.Rect{X: 10, Y: 10, W: 20, H: 20}}
	x0, x1, y0, y1 := inkSpan(t, r, n, 120, 40, 40)
	mask, err := r.RasterMaterialIcon("settings", 20)
	if err != nil {
		t.Fatal(err)
	}
	b := mask.Alpha.Bounds()
	ox, oy := 10+(20-b.Dx())/2, 10+(20-b.Dy())/2
	mx0, mx1, my0, my1 := b.Dx(), -1, b.Dy(), -1
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if mask.Alpha.AlphaAt(x, y).A > 0 {
				mx0, my0, mx1, my1 = min(mx0, x), min(my0, y), max(mx1, x), max(my1, y)
			}
		}
	}
	if x0 != ox+mx0 || x1 != ox+mx1 || y0 != oy+my0 || y1 != oy+my1 {
		t.Fatalf("settings ink %d..%d x %d..%d, want %d..%d x %d..%d", x0, x1, y0, y1, ox+mx0, ox+mx1, oy+my0, oy+my1)
	}
}

// A sprite's poses have different ink widths. Centring each pose on its own
// ink would shift the cat sideways as it runs, so the poses share one offset:
// the centre of their combined ink.
func TestSpritePosesShareOneOrigin(t *testing.T) {
	t.Parallel()
	r := NewTextRendererWithFontMap(newSystemMap(t))
	frames := []string{"cat-run-0", "cat-run-1", "cat-run-2", "cat-run-3"}
	var spans [][2]int
	lo, hi := 1<<30, -1
	for _, pose := range frames {
		n := &ui.Node{Kind: ui.KindIcon, Icon: pose, IconSize: 28, IconProjectFirst: true, Frames: frames,
			Bounds: ui.Rect{X: 10, Y: 10, W: 28, H: 28}}
		x0, x1, _, _ := inkSpan(t, r, n, 120, 48, 48)
		mask, err := r.RasterProjectIconIn(pose, 28)
		if err != nil {
			t.Fatal(err)
		}
		mx0 := mask.Alpha.Bounds().Dx()
		for y := 0; y < mask.Alpha.Bounds().Dy(); y++ {
			for x := 0; x < mask.Alpha.Bounds().Dx(); x++ {
				if mask.Alpha.AlphaAt(x, y).A > 0 {
					mx0 = min(mx0, x)
				}
			}
		}
		spans = append(spans, [2]int{x0 - mx0, x1})
		lo, hi = min(lo, x0), max(hi, x1)
	}
	for i := 1; i < len(spans); i++ {
		if spans[i][0] != spans[0][0] {
			t.Fatalf("pose origins differ: %v", spans)
		}
	}
	if centre := float64(lo+hi+1) / 2; centre < 23 || centre > 25 {
		t.Fatalf("the run's combined ink %d..%d centres at %.1f, want the box centre 24", lo, hi, centre)
	}
}

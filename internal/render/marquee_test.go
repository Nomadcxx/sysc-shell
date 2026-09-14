package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPaintTextMarqueeUsesMeasuredGapAndWraps(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))
	spec := TextSpec{Size: 16, Weight: 400}
	value := strings.Repeat("marquee ", 4)
	advance, gap, cycle, err := marqueeMetrics(r, value, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	wantGap, _, err := r.Measure(strings.Repeat(" ", 8), spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if gap != wantGap || cycle != advance+wantGap {
		t.Fatalf("marquee metrics = advance %d, gap %d, cycle %d; want gap %d and cycle %d",
			advance, gap, cycle, wantGap, advance+wantGap)
	}

	box := ui.Rect{X: 30, Y: 4, W: 70, H: 32}
	first := newTestCanvas(t, 140, 40)
	second := newTestCanvas(t, 140, 40)
	if err := paintTextMarquee(first, value, box, r, testStyle, spec, false, testStyle.Foreground, false, 5); err != nil {
		t.Fatal(err)
	}
	if err := paintTextMarquee(second, value, box, r, testStyle, spec, false, testStyle.Foreground, false, 5+cycle); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("marquee did not wrap at advance plus measured gap")
	}

	for y := 0; y < first.Height; y++ {
		for x := 0; x < first.Width; x++ {
			if x >= box.X && x < box.X+box.W {
				continue
			}
			if first.Pix[y*first.Stride+x*4+3] != 0 {
				t.Fatalf("marquee painted outside its cell at (%d,%d)", x, y)
			}
		}
	}
}

func TestPaintTextMarqueeFallsBackToStaticWhenItFits(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))
	spec := TextSpec{Size: 16, Weight: 400}
	box := ui.Rect{X: 4, Y: 4, W: 100, H: 32}
	got := newTestCanvas(t, 120, 40)
	want := newTestCanvas(t, 120, 40)
	if err := paintTextMarquee(got, "short", box, r, testStyle, spec, false, testStyle.Foreground, false, 40); err != nil {
		t.Fatal(err)
	}
	if err := paintTextColor(want, "short", box, r, testStyle, spec, false, testStyle.Foreground, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("a fitting marquee text run moved instead of using the static paint path")
	}
}

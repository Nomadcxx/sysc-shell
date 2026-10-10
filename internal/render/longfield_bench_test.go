package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Synthetic values keep credentials out of profiles while exercising the
// same masked settings field and its real shaping path.
func BenchmarkLongField(b *testing.B) {
	for _, length := range []int{32, 1564, 4096, 16384} {
		for _, masked := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/masked=%t", length, masked), func(b *testing.B) {
				fontMap, err := NewSystemFontMap("Inter Variable", DefaultFontCacheDir())
				if err != nil {
					b.Fatal(err)
				}
				r := NewTextRendererWithFontMap(fontMap)
				n := &ui.Node{Kind: ui.KindTextField, Text: strings.Repeat("a", length),
					Masked: masked, Editing: true, Cursor: length, Padding: 10,
					Bounds: ui.Rect{W: 240, H: 40}}
				measure := func(s string, _ ui.TextAttrs) (int, int) {
					w, h, err := r.Measure(s, TextSpec{Size: 16, Weight: 400}, false)
					if err != nil {
						b.Fatal(err)
					}
					return w, h
				}
				b.Run("click", func(b *testing.B) {
					for b.Loop() {
						FieldOffsetAt(n, FieldTextRect(n).X+20, measure)
					}
				})
				b.Run("paint", func(b *testing.B) {
					c, err := NewCanvas(make([]byte, 240*40*4), 240, 40, 240*4)
					if err != nil {
						b.Fatal(err)
					}
					for b.Loop() {
						if err := paintTextField(c, n, r, testStyle, 16); err != nil {
							b.Fatal(err)
						}
					}
				})
			})
		}
	}
}

func BenchmarkTemperatureGauge(b *testing.B) {
	fontMap, err := NewSystemFontMap("Inter Variable", DefaultFontCacheDir())
	if err != nil {
		b.Fatal(err)
	}
	text := NewTextRendererWithFontMap(fontMap)
	n := &ui.Node{Kind: ui.KindRadialGauge, Value: .8, ValueText: "80°", Bounds: ui.Rect{W: 22, H: 22}}
	c, err := NewCanvas(make([]byte, 22*22*4), 22, 22, 22*4)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if err := paintRadialGauge(c, n, text, testStyle); err != nil {
			b.Fatal(err)
		}
	}
}

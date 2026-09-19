package render

import (
	"strconv"
	"testing"
)

// A renderer rasterises the same string on every repaint -- a clock that has
// not ticked, a title that has not changed -- and shaping plus rasterising was
// 15 percent of a bar frame. These checks cover the cache that removes that
// work: it has to return the same pixels, tell its inputs apart, and stay
// bounded when the text keeps changing.

func rasterSpec(size, weight int) TextSpec {
	return TextSpec{Family: "sans-serif", Size: size, Weight: weight}
}

func TestRasterCacheServesTheSameMask(t *testing.T) {
	t.Parallel()
	r := NewTextRendererWithFontMap(newSystemMap(t))
	spec := rasterSpec(16, 400)

	first, err := r.Raster("Wed 30 Sep", spec, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Raster("Wed 30 Sep", spec, false)
	if err != nil {
		t.Fatal(err)
	}
	// Same backing image: the second call did no shaping or rasterising.
	if first.Alpha != second.Alpha {
		t.Error("a repeated run rasterised again instead of hitting the cache")
	}
	if first.Advance != second.Advance || first.Baseline != second.Baseline {
		t.Errorf("cached metrics %+v differ from %+v", second, first)
	}
	// And the pixels are the ones an uncached renderer produces.
	fresh, err := NewTextRendererWithFontMap(newSystemMap(t)).Raster("Wed 30 Sep", spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Alpha.Rect != first.Alpha.Rect {
		t.Fatalf("cached mask is %v, uncached is %v", first.Alpha.Rect, fresh.Alpha.Rect)
	}
	for i := range fresh.Alpha.Pix {
		if first.Alpha.Pix[i] != fresh.Alpha.Pix[i] {
			t.Fatalf("cached mask byte %d = %d, want %d", i, first.Alpha.Pix[i], fresh.Alpha.Pix[i])
		}
	}
}

// Every field of the key has to separate two runs, or one of them paints the
// other's glyphs. Tabular is the subtle one: it changes digit advances only,
// so a collision would show up as a clock that jitters by a pixel.
func TestRasterCacheSeparatesItsInputs(t *testing.T) {
	t.Parallel()
	r := NewTextRendererWithFontMap(newSystemMap(t))
	base, err := r.Raster("11:11", rasterSpec(16, 400), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		text    string
		spec    TextSpec
		tabular bool
	}{
		{"different text", "11:12", rasterSpec(16, 400), false},
		{"different size", "11:11", rasterSpec(20, 400), false},
		{"different weight", "11:11", rasterSpec(16, 700), false},
		{"different tabular", "11:11", rasterSpec(16, 400), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Raster(tc.text, tc.spec, tc.tabular)
			if err != nil {
				t.Fatal(err)
			}
			if got.Alpha == base.Alpha {
				t.Error("returned the cached mask for a different key")
			}
		})
	}
}

func TestRasterCacheStaysBounded(t *testing.T) {
	t.Parallel()
	r := NewTextRendererWithFontMap(newSystemMap(t))
	spec := rasterSpec(14, 400)
	for i := range rasterCacheMax * 3 {
		if _, err := r.Raster("title "+strconv.Itoa(i), spec, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.raster) > rasterCacheMax {
		t.Errorf("cache holds %d entries, want at most %d", len(r.raster), rasterCacheMax)
	}
	if len(r.rasterOrder) > rasterCacheMax {
		t.Errorf("eviction order holds %d keys, want at most %d", len(r.rasterOrder), rasterCacheMax)
	}
}

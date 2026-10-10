package render

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func TestRepeatedMeasureReusesShaping(t *testing.T) {
	r := newTestRenderer(t)
	spec := rasterSpec(16, 400)
	w, h, err := r.Measure("unchanged text", spec, false)
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(1, func() {
		gotW, gotH, err := r.Measure("unchanged text", spec, false)
		if err != nil || gotW != w || gotH != h {
			t.Fatalf("measure = %d/%d, %v, want %d/%d", gotW, gotH, err, w, h)
		}
	})
	if allocs > 0 {
		t.Fatalf("repeated measurement allocated %.0f times instead of reusing its shape", allocs)
	}
}

func TestShapingCacheDoesNotPinAnOversizedSource(t *testing.T) {
	r := newTestRenderer(t)
	large := strings.Repeat("a", shapeCacheBudget*2)
	prefix := large[:1]
	if _, _, err := r.Measure(prefix, rasterSpec(16, 400), false); err != nil {
		t.Fatal(err)
	}
	for key := range r.shaped {
		if unsafe.StringData(key.text) == unsafe.StringData(large) {
			t.Fatal("a tiny cached prefix retains the entire oversized source")
		}
	}
}

func TestShapingCacheBoundsChangingAndOversizedText(t *testing.T) {
	r := newTestRenderer(t)
	spec := rasterSpec(16, 400)
	for i := range rasterCacheMax * 2 {
		if _, _, err := r.Measure("text "+strconv.Itoa(i), spec, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.shaped) > rasterCacheMax || r.shapedCost > shapeCacheBudget {
		t.Fatal("changing text exceeded the shape cache budget")
	}
	large := strings.Repeat("a", shapeCacheBudget)
	if _, _, err := r.Measure(large, spec, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.shaped[rasterKey{text: large, spec: spec}]; ok {
		t.Fatal("a run larger than the cache budget was retained")
	}
}

func TestShapingCachePreservesRunsAcrossOtherShapes(t *testing.T) {
	r := NewTextRendererWithFontMap(newFixtureFontMap(t))
	spec := rasterSpec(16, 400)
	text := "Latin سلام"
	first, err := r.shapeRuns(text, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []struct {
		text string
		spec TextSpec
		tab  bool
	}{{text, rasterSpec(24, 400), false}, {text, rasterSpec(16, 600), false}, {text, spec, true}, {"different", spec, false}} {
		if _, err := r.shapeRuns(other.text, other.spec, other.tab); err != nil {
			t.Fatal(err)
		}
	}
	again, err := r.shapeRuns(text, spec, false)
	fresh, freshErr := NewTextRendererWithFontMap(newFixtureFontMap(t)).shapeRuns(text, spec, false)
	if err != nil || freshErr != nil || len(again) != len(fresh) {
		t.Fatalf("shape = %v/%v: %v/%v", again, fresh, err, freshErr)
	}
	for i := range again {
		if !reflect.DeepEqual(again[i].output.Glyphs, fresh[i].output.Glyphs) ||
			!reflect.DeepEqual(first[i].output.Glyphs, fresh[i].output.Glyphs) {
			t.Fatal("another request changed retained glyphs or served the wrong shape")
		}
	}
}

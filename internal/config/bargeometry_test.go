package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBarBodyAndExtentDeriveFromHeightAndGap(t *testing.T) {
	t.Parallel()
	bar := Bar{Height: 48, Gap: 4}
	if got := bar.Body(); got != 40 {
		t.Fatalf("body = %d, want 40: the gap is drawn on each side of the body", got)
	}
	if got := bar.Extent(); got != 44 {
		t.Fatalf("extent = %d, want 44: the surface carries one gap, not two", got)
	}
}

func TestBarExtentMatchesTheDefaultBar(t *testing.T) {
	t.Parallel()
	bar := Default().Bar
	if got, want := bar.Extent(), bar.Body(); got != want {
		t.Fatalf("extent = %d, want the attached body %d", got, want)
	}
}

// TestAttachedGeometry is the attached bar against the floating one at height
// 48 and gap 4: the extent and the zone are the body, and the surface grows by
// the overhang that holds the end fillets.
func TestAttachedGeometry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		shape                 string
		extent, surface, zone int
	}{{"attached", 40, 52, 40}, {"floating", 44, 44, 44}} {
		bar := Bar{Height: 48, Gap: 4, Shape: tc.shape}
		if bar.Extent() != tc.extent || bar.SurfaceExtent() != tc.surface || bar.ExclusiveZone() != tc.zone {
			t.Errorf("%s: extent %d surface %d zone %d, want %d/%d/%d", tc.shape,
				bar.Extent(), bar.SurfaceExtent(), bar.ExclusiveZone(), tc.extent, tc.surface, tc.zone)
		}
	}
}

func TestBodyInFollowsShapeAndEdge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		shape, edge string
		want        [4]int
	}{
		{"floating", "top", [4]int{4, 4, 1192, 40}},
		{"floating", "bottom", [4]int{4, 0, 1192, 40}},
		{"attached", "top", [4]int{0, 0, 1200, 40}},
		{"attached", "bottom", [4]int{0, 12, 1200, 40}},
	} {
		bar := Bar{Height: 48, Gap: 4, Shape: tc.shape, Edge: tc.edge}
		x, y, w, h := bar.BodyIn(1200, bar.SurfaceExtent())
		if got := [4]int{x, y, w, h}; got != tc.want {
			t.Errorf("%s %s body = %v, want %v", tc.shape, tc.edge, got, tc.want)
		}
	}
}

func TestBarBodyInAllEdges(t *testing.T) {
	t.Parallel()
	body := func(bar Bar, w, h int) [4]int {
		x, y, bw, bh := bar.BodyIn(w, h)
		return [4]int{x, y, bw, bh}
	}
	for _, tc := range []struct {
		name, edge, shape, style string
		w, h                     int
		want                     [4]int
	}{
		{"attached top", "top", "attached", "frosted", 1200, 52, [4]int{0, 0, 1200, 40}},
		{"attached bottom", "bottom", "attached", "frosted", 1200, 52, [4]int{0, 12, 1200, 40}},
		{"attached left", "left", "attached", "frosted", 60, 800, [4]int{0, 0, 48, 800}},
		{"attached right", "right", "attached", "frosted", 60, 800, [4]int{12, 0, 48, 800}},
		{"floating top", "top", "floating", "frosted", 1200, 44, [4]int{4, 4, 1192, 40}},
		{"floating bottom", "bottom", "floating", "frosted", 1200, 44, [4]int{4, 0, 1192, 40}},
		{"floating left", "left", "floating", "frosted", 44, 800, [4]int{4, 4, 40, 792}},
		{"floating right", "right", "floating", "frosted", 44, 800, [4]int{0, 4, 40, 792}},
		{"islands attached left", "left", "attached", "islands", 44, 800, [4]int{4, 4, 40, 792}},
		{"islands attached right", "right", "attached", "islands", 44, 800, [4]int{0, 4, 40, 792}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bar := Bar{Height: 48, Gap: 4, Edge: tc.edge, Shape: tc.shape, Style: tc.style}
			if got := body(bar, tc.w, tc.h); got != tc.want {
				t.Errorf("body = %v, want %v", got, tc.want)
			}
		})
	}
	zero, custom := 0, 17
	for _, tc := range []struct {
		name    string
		reserve *int
		zone    int
	}{
		{"default", nil, 44},
		{"zero", &zero, 0},
		{"custom", &custom, 17},
	} {
		t.Run("reserve "+tc.name, func(t *testing.T) {
			bar := Bar{Height: 48, Gap: 4, Edge: "right", Shape: "floating", Reserve: tc.reserve}
			if got := body(bar, 44, 800); got != [4]int{0, 4, 40, 792} {
				t.Errorf("body = %v; reserve moved paint", got)
			}
			if got := bar.ExclusiveZone(); got != tc.zone {
				t.Errorf("zone = %d, want %d", got, tc.zone)
			}
		})
	}
	for _, tc := range []struct {
		edge, shape string
		w, h        int
		want        [4]int
	}{
		{"top", "floating", 2, 3, [4]int{4, 4, 0, 0}},
		{"bottom", "floating", 2, 3, [4]int{4, 0, 0, 0}},
		{"left", "floating", 2, 3, [4]int{4, 4, 0, 0}},
		{"right", "floating", 2, 3, [4]int{0, 4, 0, 0}},
		{"left", "attached", 5, 3, [4]int{0, 0, 0, 3}},
		{"right", "attached", 5, 3, [4]int{12, 0, 0, 3}},
	} {
		bar := Bar{Height: 48, Gap: 4, Shape: tc.shape, Edge: tc.edge}
		if got := body(bar, tc.w, tc.h); got != tc.want {
			t.Errorf("tiny %s %s body = %v, want %v", tc.shape, tc.edge, got, tc.want)
		}
	}
}

func TestBarExclusiveZoneFollowsTheExtentWhenUnset(t *testing.T) {
	t.Parallel()
	bar := Bar{Height: 48, Gap: 4}
	if bar.Reserve != nil {
		t.Fatal("a bar with no stated reserve must leave the field nil")
	}
	if got := bar.ExclusiveZone(); got != bar.Extent() {
		t.Fatalf("zone = %d, want the extent %d", got, bar.Extent())
	}
}

func TestBarExclusiveZoneHonoursAnExplicitZero(t *testing.T) {
	t.Parallel()
	zero := 0
	bar := Bar{Height: 48, Gap: 4, Reserve: &zero}
	if got := bar.ExclusiveZone(); got != 0 {
		t.Fatalf("zone = %d, want 0: a stated zero must not fall back to the extent", got)
	}
	if bar.Extent() != 44 {
		t.Fatalf("extent = %d, want 44: the reserve must not move the surface", bar.Extent())
	}
}

func TestBarExclusiveZoneHonoursAnExplicitValue(t *testing.T) {
	t.Parallel()
	reserve := 12
	bar := Bar{Height: 48, Gap: 4, Reserve: &reserve}
	if got := bar.ExclusiveZone(); got != 12 {
		t.Fatalf("zone = %d, want 12", got)
	}
}

func TestReserveRoundTripsThroughWriteAndLoad(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	zero := 0
	c.Bar.Reserve = &zero
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"reserve"`) {
		t.Fatalf("a stated reserve must be written; got:\n%s", data)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bar.Reserve == nil {
		t.Fatal("reserve came back nil: a stated zero was dropped in the round trip")
	}
	if *got.Bar.Reserve != 0 {
		t.Fatalf("reserve = %d, want 0", *got.Bar.Reserve)
	}
	if got.Bar.ExclusiveZone() != 0 {
		t.Fatalf("zone = %d, want 0 after the round trip", got.Bar.ExclusiveZone())
	}
}

func TestAnUnsetReserveIsNeverWritten(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Bar.Height = 42
	if err := Write(p, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"reserve"`) {
		t.Fatalf("a bar that never stated a reserve must not gain one:\n%s", data)
	}
}

func TestParsedReserveIsHonoured(t *testing.T) {
	t.Parallel()
	got, err := Parse([]byte(`{"bar":{"reserve":0}}`))
	if err != nil {
		t.Fatalf("reserve 0: %v", err)
	}
	if got.Bar.Reserve == nil || *got.Bar.Reserve != 0 {
		t.Fatalf("reserve = %v, want a stated 0", got.Bar.Reserve)
	}
	if got.Bar.Extent() != Default().Bar.Extent() {
		t.Fatal("a reserve must not move the surface extent")
	}
}

func TestNegativeReserveIsRejectedAtItsPath(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar":{"reserve":-1}}`))
	if err == nil {
		t.Fatal("a negative reserve must be rejected")
	}
	if !strings.Contains(err.Error(), "bar.reserve") {
		t.Fatalf("error must name the field path, got: %v", err)
	}
}

func TestTheLowerEdgeLoads(t *testing.T) {
	t.Parallel()
	got, err := Parse([]byte(`{"bar":{"edge":"bottom"}}`))
	if err != nil {
		t.Fatalf("bottom edge: %v", err)
	}
	if got.Bar.Edge != "bottom" {
		t.Fatalf("edge = %q, want bottom", got.Bar.Edge)
	}
}

func TestVerticalEdgesLoad(t *testing.T) {
	t.Parallel()
	for _, edge := range []string{"left", "right"} {
		got, err := Parse([]byte(`{"bar":{"edge":"` + edge + `"}}`))
		if err != nil {
			t.Fatalf("%q edge: %v", edge, err)
		}
		if got.Bar.Edge != edge {
			t.Fatalf("edge = %q, want %q", got.Bar.Edge, edge)
		}
	}
}

func TestAnUnknownEdgeNamesAllFour(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar":{"edge":"sideways"}}`))
	if err == nil {
		t.Fatal("an unknown edge must be rejected")
	}
	if !strings.Contains(err.Error(), "top, bottom, left, right") {
		t.Fatalf("an unknown edge must name the vocabulary, got: %v", err)
	}
}

func TestSideEdgeRoundTrip(t *testing.T) {
	t.Parallel()
	zero, custom := 0, 17
	for _, tc := range []struct {
		name, edge, style, shape                   string
		reserve                                    *int
		overrideEdge, overrideStyle, overrideShape string
		overrideReserve                            *int
	}{
		{
			name: "left with unset reserve and right override",
			edge: "left", style: "solid", shape: "attached",
			overrideEdge: "right", overrideStyle: "islands", overrideShape: "floating",
			overrideReserve: &zero,
		},
		{
			name: "right with custom reserve and left override",
			edge: "right", style: "frosted", shape: "floating", reserve: &custom,
			overrideEdge: "left", overrideStyle: "solid", overrideShape: "attached",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Bar.Edge, cfg.Bar.Style, cfg.Bar.Shape, cfg.Bar.Reserve = tc.edge, tc.style, tc.shape, tc.reserve
			override := cfg.Bar
			override.Edge, override.Style, override.Shape, override.Reserve =
				tc.overrideEdge, tc.overrideStyle, tc.overrideShape, tc.overrideReserve
			cfg.Outputs = []OutputOverride{{Connector: "DP-1", Bar: override}}

			path := filepath.Join(t.TempDir(), "config.json")
			if err := Write(path, cfg); err != nil {
				t.Fatalf("Write: %v", err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got.Bar.Edge != tc.edge || got.Bar.Style != tc.style || got.Bar.Shape != tc.shape || !sameReserve(got.Bar.Reserve, tc.reserve) {
				t.Fatalf("base bar = edge %q style %q shape %q reserve %v, want %q %q %q %v",
					got.Bar.Edge, got.Bar.Style, got.Bar.Shape, got.Bar.Reserve, tc.edge, tc.style, tc.shape, tc.reserve)
			}
			if len(got.Outputs) != 1 {
				t.Fatalf("outputs = %d, want one override", len(got.Outputs))
			}
			gotOverride := got.Outputs[0].Bar
			wantOverrideReserve := tc.overrideReserve
			if wantOverrideReserve == nil {
				wantOverrideReserve = tc.reserve
			}
			if gotOverride.Edge != tc.overrideEdge || gotOverride.Style != tc.overrideStyle ||
				gotOverride.Shape != tc.overrideShape || !sameReserve(gotOverride.Reserve, wantOverrideReserve) {
				t.Fatalf("override = edge %q style %q shape %q reserve %v, want %q %q %q %v",
					gotOverride.Edge, gotOverride.Style, gotOverride.Shape, gotOverride.Reserve,
					tc.overrideEdge, tc.overrideStyle, tc.overrideShape, wantOverrideReserve)
			}
		})
	}
}

func sameReserve(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

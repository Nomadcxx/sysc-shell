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
	if got, want := bar.Extent(), bar.Height-bar.Gap; got != want {
		t.Fatalf("extent = %d, want %d", got, want)
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

func TestVerticalEdgesAreStillGated(t *testing.T) {
	t.Parallel()
	for _, edge := range []string{"left", "right"} {
		_, err := Parse([]byte(`{"bar":{"edge":"` + edge + `"}}`))
		if err == nil {
			t.Fatalf("%q must still be rejected: the vertical axis waits on sysc-314", edge)
		}
		if !strings.Contains(err.Error(), "top or bottom") {
			t.Fatalf("%q must be told what does work, got: %v", edge, err)
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

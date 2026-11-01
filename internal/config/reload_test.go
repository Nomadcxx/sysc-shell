package config

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestBarForSizesTheStandardBarPerOutput(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"bar":{"gap":4}}`))
	if err != nil {
		t.Fatal(err)
	}
	base := cfg.ForConnector("eDP-1")
	if got := cfg.BarFor("eDP-1", 0, 0); got.Height != base.Height || got.Row != "" {
		t.Fatalf("unknown size = %d/%q, want the standard bar %d", got.Height, got.Row, base.Height)
	}
	for _, tc := range []struct {
		connector string
		w, h      int
		row       theme.Density
	}{
		{"eDP-1", 1536, 864, theme.DensityComfortable},
		{"DP-1", 3440, 1440, theme.DensitySpacious},
		{"HDMI-1", 1280, 600, theme.DensityCompact},
	} {
		m, _ := theme.MetricsFor(tc.row)
		got := cfg.BarFor(tc.connector, tc.w, tc.h)
		if got.Row != tc.row || got.Height != m.BarHeight+8 || got.Padding != m.BarPadding || got.Spacing != m.BarSpacing {
			t.Errorf("%s at %dx%d = row %q %d/%d/%d, want %q %d/%d/%d", tc.connector, tc.w, tc.h,
				got.Row, got.Height, got.Padding, got.Spacing, tc.row, m.BarHeight+8, m.BarPadding, m.BarSpacing)
		}
	}
	bars, err := ResolveOutputs(cfg, []Output{{Connector: "eDP-1", Width: 1536, Height: 864}})
	if err != nil || len(bars) != 1 || bars[0].Row != theme.DensityComfortable {
		t.Fatalf("ResolveOutputs = %+v, %v; want the comfortable row", bars, err)
	}
}

func TestBarForKeepsASizeTheUserChose(t *testing.T) {
	t.Parallel()
	for _, doc := range []string{
		`{"bar":{"height":44}}`,
		`{"theme":{"density":"compact"}}`,
		`{"theme":{"preset":"expressive"}}`,
		`{"outputs":[{"connector":"eDP-1","bar":{"height":36}}]}`,
	} {
		cfg, err := Parse([]byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		want := cfg.ForConnector("eDP-1")
		got := cfg.BarFor("eDP-1", 1536, 864)
		if got.Height != want.Height || got.Padding != want.Padding || got.Spacing != want.Spacing || got.Row != "" {
			t.Errorf("%s: BarFor = %d/%d/%d row %q, want the chosen %d/%d/%d", doc,
				got.Height, got.Padding, got.Spacing, got.Row, want.Height, want.Padding, want.Spacing)
		}
	}
	cfg, err := Parse([]byte(`{"bar":{"padding":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.BarFor("eDP-1", 1536, 864); got.Padding != 3 || got.Row != theme.DensityComfortable {
		t.Fatalf("explicit padding = %d row %q, want 3 kept on the comfortable row", got.Padding, got.Row)
	}
}

func TestResolveReturnsOnePolicyPerConnector(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"height":44}}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	bars, err := Resolve(cfg, []string{"DP-1", "DP-3"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("Resolve returned %d policies, want 2", len(bars))
	}
	if bars[0].Height != 44 {
		t.Fatalf("DP-1 height = %d, want the override 44", bars[0].Height)
	}
	// This parsed document has no preset or density selector, so an output
	// without its own override inherits the legacy base.
	if bars[1].Height != 48 {
		t.Fatalf("DP-3 height = %d, want the legacy base 48", bars[1].Height)
	}
}

func TestResolveRejectsTheWholeCandidateWhenOneHostFails(t *testing.T) {
	t.Parallel()
	// Valid as a document, but the override leaves DP-1 with no painted body.
	cfg := Default()
	cfg.Outputs = []OutputOverride{{
		Connector: "DP-1",
		Bar:       Bar{Enabled: true, Edge: "top", Height: 6, Gap: 4, FontSize: 14},
	}}

	if _, err := Resolve(cfg, []string{"DP-1", "DP-3"}); err == nil {
		t.Fatal("Resolve accepted a policy that leaves no body")
	} else if !strings.Contains(err.Error(), "DP-1") {
		t.Fatalf("error %q does not name the failing connector", err)
	}
}

func TestResolveOfNoConnectorsSucceeds(t *testing.T) {
	t.Parallel()
	// A shell with no outputs connected is not an error.
	bars, err := Resolve(Default(), nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bars) != 0 {
		t.Fatalf("Resolve returned %d policies for no connectors", len(bars))
	}
}

func TestResolvePreservesConnectorOrder(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"outputs":[
	  {"connector":"DP-3","bar":{"height":50}},
	  {"connector":"DP-1","bar":{"height":44}}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// The result is indexed against the caller's connector slice, not the
	// configuration's order, so the owner can zip it against its host list.
	bars, err := Resolve(cfg, []string{"DP-1", "DP-3"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bars[0].Height != 44 || bars[1].Height != 50 {
		t.Fatalf("policies = %d/%d, want 44/50 in the caller's order",
			bars[0].Height, bars[1].Height)
	}
}

func TestReloadCandidateResolvesSideOutputOverrideWithoutLosingReserve(t *testing.T) {
	t.Parallel()

	cfg := Default()
	zero := 0
	override := cfg.Bar
	override.Edge, override.Shape, override.Style = "right", "floating", "solid"
	override.Height, override.Gap, override.Reserve = 64, 8, &zero
	cfg.Outputs = []OutputOverride{{Connector: "DP-1", Bar: override}}

	policies, err := Resolve(cfg, []string{"DP-1", "DP-2"})
	if err != nil {
		t.Fatalf("Resolve reload candidate: %v", err)
	}
	if policies[0].Edge != "right" || policies[0].ExclusiveZone() != 0 || policies[0].SurfaceExtent() != 56 {
		t.Fatalf("DP-1 reload policy = edge %q zone %d extent %d, want right/0/56",
			policies[0].Edge, policies[0].ExclusiveZone(), policies[0].SurfaceExtent())
	}
	if policies[1].Edge != cfg.Bar.Edge || policies[1].Reserve != nil {
		t.Fatalf("DP-2 inherited policy = edge %q reserve %v, want %q with nil reserve",
			policies[1].Edge, policies[1].Reserve, cfg.Bar.Edge)
	}
}

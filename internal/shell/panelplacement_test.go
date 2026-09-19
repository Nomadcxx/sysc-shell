package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A side bar offsets the panel with its anchor on the bar's cross axis and
// clamps the along-bar position over the output's height; the anchor carries
// the triggering widget's along-bar coordinate. The top and lower rows match
// today's output exactly.
func TestPanelMarginsFollowTheEdge(t *testing.T) {
	t.Parallel()
	base := Placement{
		BarZone: 40, Gap: 4, Padding: 8,
		Output: ui.Rect{W: 1536, H: 864},
		Panel:  ui.Rect{W: 300, H: 600},
	}
	for _, tc := range []struct {
		name  string
		place Placement
		want  Margins
	}{
		{"top", withEdge(base, "top"), Margins{Top: 44, Left: 618}},
		{"lower", withEdge(base, "bottom"), Margins{Bottom: 44, Left: 618}},
		{"left", withEdge(base, "left"), Margins{Left: 44, Top: 132}},
		{"right", withEdge(base, "right"), Margins{Right: 44, Top: 132}},
		{"left clamps inside the output height",
			tallPanel(withEdge(base, "left")), Margins{Left: 44, Top: 8}},
		{"left transposes align",
			withAlign(withEdge(base, "left"), "left"), Margins{Left: 44, Top: 8}},
		{"left transposes the far align",
			withAlign(withEdge(base, "left"), "right"), Margins{Left: 44, Top: 256}},
		{"left anchors on the trigger's along-bar coordinate",
			withAnchor(withEdge(base, "left"), 400), Margins{Left: 44, Top: 100}},
		{"right anchors on the trigger's along-bar coordinate",
			withAnchor(withEdge(base, "right"), 400), Margins{Right: 44, Top: 100}},
		{"left centres on the cross axis",
			withCentre(withEdge(base, "left")), Margins{Left: 636, Top: 132}},
		{"right centres on the cross axis",
			withCentre(withEdge(base, "right")), Margins{Right: 636, Top: 132}},
		{"a side modal centres in the whole output",
			withCentre(withAnchorless(withEdge(base, "left"))), Margins{Left: 618, Top: 132}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.place.Margins(); got != tc.want {
				t.Fatalf("margins = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func withEdge(p Placement, edge string) Placement {
	p.BarEdge = edge
	return p
}

func withAlign(p Placement, align string) Placement {
	p.Align = align
	return p
}

func withAnchor(p Placement, anchor int) Placement {
	p.AnchorX = anchor
	return p
}

func withCentre(p Placement) Placement {
	p.CenterY = true
	return p
}

func tallPanel(p Placement) Placement {
	p.Panel.H = 900
	return p
}

// withAnchorless zeroes the bar zone and gap, making the anchor zero: the
// true modal that centres in the whole output.
func withAnchorless(p Placement) Placement {
	p.BarZone, p.Gap = 0, 0
	return p
}

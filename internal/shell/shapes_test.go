package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A radius below the floor used to reach the painter as zero, which reads as
// "fully round": the radius slider at its minimum turned cards into pills. The
// config still accepts it, so no file stops loading; the theme lifts it.
func TestRadiusBelowTheFloorResolvesToTheFloor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ set, want int }{{0, 4}, {3, 4}, {4, 4}, {5, 5}, {12, 12}} {
		cfg := config.Default()
		cfg.Theme.Radius, cfg.Theme.InputRadius, cfg.Bar.Radius = tc.set, tc.set, tc.set
		th, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
		if err != nil {
			t.Fatal(err)
		}
		if th.Radius != tc.want || th.Shapes.Input != tc.want {
			t.Errorf("radius %d: resolved radius %d input %d, want %d", tc.set, th.Radius, th.Shapes.Input, tc.want)
		}
	}
}

func TestExtraLargeIsTwentyEightAtTheDefaultRadius(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ radius, want int }{{12, 28}, {8, 19}, {4, 9}, {16, theme.RadiusMax}} {
		s := resolveShapes(tc.radius, tc.radius)
		if s.ExtraLarge != tc.want {
			t.Errorf("radius %d: extra large = %d, want %d", tc.radius, s.ExtraLarge, tc.want)
		}
		if got := s.For(ui.ShapeExtraLarge, 0); got != tc.want {
			t.Errorf("radius %d: ShapeExtraLarge resolves to %d, want %d", tc.radius, got, tc.want)
		}
	}
}

// A detached panel takes the extra-large corner; an attached one keeps the
// bar's radius so the join and its fillet do not move.
func TestPanelSurfaceRadiusFollowsAttachment(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	for _, tc := range []struct {
		name  string
		place Placement
		want  int
	}{
		{"detached", Placement{BarEdge: "top", Detached: true}, th.Shapes.ExtraLarge},
		{"centred", Placement{BarEdge: "top", CenterY: true}, th.Shapes.ExtraLarge},
		{"attached", Placement{BarEdge: "top"}, th.Radius},
	} {
		h := &PanelHost{id: PanelControlCenter, theme: th, place: tc.place}
		if got := h.surfaceRadius(); got != tc.want {
			t.Errorf("%s: surface radius = %d, want %d", tc.name, got, tc.want)
		}
		s := h.rootStyle(th, false)
		if got := s.SurfaceRadius; got != tc.want {
			t.Errorf("%s: root style surface radius = %d, want %d", tc.name, got, tc.want)
		}
		if s.Radius != th.Radius {
			t.Errorf("%s: inherited radius = %d, want the theme radius %d", tc.name, s.Radius, th.Radius)
		}
	}
	if th.Shapes.ExtraLarge <= th.Radius {
		t.Fatalf("extra large %d is not rounder than the theme radius %d", th.Shapes.ExtraLarge, th.Radius)
	}
}

func TestPolkitCardTakesTheExtraLargeCorner(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	newHosts(t, r, map[uint32]string{1: "eDP-1"})
	h := newPolkitHost(r)
	h.request = func(wayland.AuxRequest) {}
	r.mu.Lock()
	defer r.mu.Unlock()
	h.openLocked(polkit.Request{Cookie: "c1", Message: "Authentication is required.",
		Identities: []polkit.Identity{{Kind: "unix-user", Name: "alex"}}}, 1, "eDP-1", 0)
	card := findNode(h.root, func(n *ui.Node) bool { return n.Fill == ui.FillContainerHigh && n.CenterX && n.CenterY })
	if card == nil || card.Shape != ui.ShapeExtraLarge {
		t.Fatalf("polkit card = %+v, want ShapeExtraLarge", card)
	}
}

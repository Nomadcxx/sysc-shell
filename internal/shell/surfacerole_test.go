package shell

import (
	"os/exec"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// panelAtDensity opens one panel with a density chosen by the test, so a
// surface can be inspected at the ends of the metric table rather than only at
// the row the default happens to use.
func panelAtDensity(t *testing.T, id PanelID, d theme.Density) (*Registry, *PanelHost) {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Theme.Density = d
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	reg.runArgv = func([]string) error { return nil }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(id, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[id]
	if h == nil {
		t.Fatalf("%v host is missing", id)
	}
	return reg, h
}

func walkNodes(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walkNodes(c, fn)
	}
}

// cardsOf collects the panel cards: the high-container capsules the popouts
// build through the shared card constructor.
func cardsOf(root *ui.Node) []*ui.Node {
	var out []*ui.Node
	walkNodes(root, func(n *ui.Node) {
		if n.Kind == ui.KindCapsule && n.Fill == ui.FillContainerHigh {
			out = append(out, n)
		}
	})
	return out
}

// TestSurfaceCardsCarryTheCardShape is the shape half of the migration. A card
// asks for its shape by role, so the radius follows the theme rather than
// whatever the surface passed down. Without the role a card inherits the bar's
// pill radius and reads as a lozenge at radius 32.
func TestSurfaceCardsCarryTheCardShape(t *testing.T) {
	t.Parallel()
	for _, id := range []PanelID{PanelMonitor, PanelSession} {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			_, h := panelAtDensity(t, id, theme.DensityStandard)
			cards := cardsOf(h.root)
			if len(cards) == 0 {
				t.Fatal("no cards found; the shared card constructor changed shape")
			}
			for i, c := range cards {
				if c.Shape != ui.ShapeCard {
					t.Errorf("card %d shape = %v, want ShapeCard", i, c.Shape)
				}
			}
		})
	}
}

// TestSurfaceCardPaddingFollowsDensity is the metric half. The card inset was a
// package constant, so a compact panel drew comfortable padding inside every
// card: density moved the panel but not what sat in it.
func TestSurfaceCardPaddingFollowsDensity(t *testing.T) {
	t.Parallel()
	for _, id := range []PanelID{PanelMonitor, PanelSession} {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			seen := map[theme.Density]int{}
			for _, d := range []theme.Density{theme.DensityCompact, theme.DensityComfortable} {
				_, h := panelAtDensity(t, id, d)
				cards := cardsOf(h.root)
				if len(cards) == 0 {
					t.Fatalf("%s: no cards found", d)
				}
				want, ok := theme.MetricsFor(d)
				if !ok {
					t.Fatalf("no %s row", d)
				}
				for i, c := range cards {
					if c.Padding != want.CardPadding {
						t.Errorf("%s card %d padding = %d, want the row's %d",
							d, i, c.Padding, want.CardPadding)
					}
				}
				seen[d] = cards[0].Padding
			}
			if seen[theme.DensityCompact] == seen[theme.DensityComfortable] {
				t.Errorf("card padding is %d at both ends of the table; density is not reaching it",
					seen[theme.DensityCompact])
			}
		})
	}
}

// TestSurfaceCardTitlesAreTitleRole replaces bold body text with the semantic
// role. A heading that is body-plus-bold cannot follow a theme that changes the
// title weight, and it asks the font stack for a synthetic face rather than the
// real cut the role names.
func TestSurfaceCardTitlesAreTitleRole(t *testing.T) {
	t.Parallel()
	_, h := panelAtDensity(t, PanelMonitor, theme.DensityStandard)

	var headings []*ui.Node
	walkNodes(h.root, func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Role == "heading" {
			headings = append(headings, n)
		}
	})
	if len(headings) == 0 {
		t.Fatal("no card headings found")
	}
	for _, n := range headings {
		if n.TextRole != theme.RoleTitle {
			t.Errorf("heading %q role = %v, want RoleTitle", n.Name, n.TextRole)
		}
		if n.Bold {
			t.Errorf("heading %q still asks for synthetic bold; the role carries the weight", n.Name)
		}
	}
}

// standardMetrics is the density row a tree test builds against when the
// density is not what it is checking.
func standardMetrics() theme.Metrics {
	m, ok := theme.MetricsFor(theme.DensityStandard)
	if !ok {
		panic("no standard metrics row")
	}
	return m
}

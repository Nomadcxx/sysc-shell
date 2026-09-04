package shell

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
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

// TestSurfaceTreesAskForNoSyntheticBold is the type half of the migration. A
// node that sets Bold asks the painter to thicken a face it already has;
// naming a role asks the font stack for the cut the theme chose. The renderer
// stopped synthesising bold in this tranche, so a tree that still sets the
// flag now silently gets regular weight.
//
// This is the gate that lets Node.Bold be deleted once every tree has moved.
func TestSurfaceTreesAskForNoSyntheticBold(t *testing.T) {
	t.Parallel()
	for _, id := range []PanelID{PanelMonitor, PanelSession, PanelClock, PanelLauncher, PanelNotifications} {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			_, h := panelAtDensity(t, id, theme.DensityStandard)
			walkNodes(h.root, func(n *ui.Node) {
				if n.Kind == ui.KindText && n.Bold {
					t.Errorf("%q asks for synthetic bold; name a text role instead", n.Text)
				}
			})
		})
	}
}

// TestSurfaceHeadingsCarryARole keeps the accessible heading role and the
// painted type role in step. A node marked up as a heading that measures as
// body text reads as a heading to a screen reader and looks like body copy.
func TestSurfaceHeadingsCarryARole(t *testing.T) {
	t.Parallel()
	for _, id := range []PanelID{PanelMonitor, PanelSession} {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			_, h := panelAtDensity(t, id, theme.DensityStandard)
			walkNodes(h.root, func(n *ui.Node) {
				if n.Role == "heading" && n.TextRole == theme.RoleBody {
					t.Errorf("heading %q measures as body text", n.Name)
				}
			})
		})
	}
}

// TestSurfaceSourcesCarryNoLegacyVisuals is the plan's scan gate. A runtime
// walk only reaches the trees a test happens to populate -- a launcher row
// exists only when there are results, a notification card only when there is a
// notification -- so the surfaces most likely to keep a legacy visual are the
// ones a walk misses. This reads the package source instead, which cannot be
// dodged by an empty panel.
//
// Two things are forbidden in a first-party tree:
//
//   - Bold on a text node. The renderer no longer synthesises weight, so the
//     flag now silently paints regular. Naming a role gets the real cut.
//   - A flat geometry alias. Those names are the compatibility layer over the
//     metrics table; a surface that reads one is not following density.
func TestSurfaceSourcesCarryNoLegacyVisuals(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	aliases := regexp.MustCompile(`\.(BarPadding|Spacing|TextSize|CapsulePadding|ControlHeight|CompactHeight|ButtonPadding|IconSize|ProfileIconSize|OSDIconSize|CardRadius)\b`)
	bold := regexp.MustCompile(`\bBold:\s*true\b`)

	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// theme.go is the compatibility layer itself: it is where the flat
		// names are produced, so it is the one file allowed to mention them.
		if name == "theme.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if bold.MatchString(code) {
				t.Errorf("%s:%d asks for synthetic bold; name a text role instead", name, i+1)
			}
			if m := aliases.FindString(code); m != "" && !strings.Contains(code, "Metrics."+strings.TrimPrefix(m, ".")) &&
				!strings.Contains(code, "Shapes.") && !strings.Contains(code, "st.Muted") {
				t.Errorf("%s:%d reads the legacy alias %s; read the metrics row", name, i+1, m)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no sources; the gate is not looking at the package")
	}
}

// TestSurfaceOpacityReachesEachRoot checks the three axes land on the three
// roots. The bar, the panels and the overlays each carry their own alpha, and
// a host that reads the wrong one makes a setting look broken.
func TestSurfaceOpacityReachesEachRoot(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BarOpacity, cfg.Theme.PanelOpacity, cfg.Theme.OverlayOpacity = 100, 90, 80
	th, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		got  uint8
		want uint8
	}{
		{"bar", th.Style().SurfaceOpacity, th.Surfaces.Bar},
		{"panel", th.PanelStyle().SurfaceOpacity, th.Surfaces.Panel},
		{"overlay", th.OverlayStyle().SurfaceOpacity, th.Surfaces.Overlay},
	} {
		if tc.got != tc.want {
			t.Errorf("%s alpha = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if th.Surfaces.Panel >= th.Surfaces.Bar || th.Surfaces.Overlay >= th.Surfaces.Panel {
		t.Errorf("alphas %d/%d/%d do not track the configured 100/90/80",
			th.Surfaces.Bar, th.Surfaces.Panel, th.Surfaces.Overlay)
	}
}

// TestSurfaceHighContrastForcesOpaqueRoots is the accessibility override: a
// translucent root defeats a contrast floor measured against it, so high
// contrast pins every surface opaque and turns the structural outline on.
func TestSurfaceHighContrastForcesOpaqueRoots(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Theme.BarOpacity, cfg.Theme.PanelOpacity, cfg.Theme.OverlayOpacity = 90, 90, 80
	cfg.Accessibility.HighContrast = true
	th, err := ResolveTheme(cfg, cfg.Bar, theme.FallbackHighContrast)
	if err != nil {
		t.Fatal(err)
	}
	if th.Surfaces.Bar != 0xff || th.Surfaces.Panel != 0xff || th.Surfaces.Overlay != 0xff {
		t.Errorf("high contrast left translucent roots: %+v", th.Surfaces)
	}
	if !th.Outlined {
		t.Error("high contrast did not enable the structural outline")
	}
	if !th.BackgroundOpaque() {
		t.Error("an opaque high-contrast bar does not report an opaque background")
	}
}

// TestSurfaceRethemeCarriesEveryAxis is the live-publication check. A reload
// used to rebuild the default composition around the new radius, so a palette
// change reached an open panel and nothing else did: density, motion and
// opacity all stopped at the bar.
func TestSurfaceRethemeCarriesEveryAxis(t *testing.T) {
	t.Parallel()
	reg, h := panelAtDensity(t, PanelMonitor, theme.DensityStandard)

	before := h.theme.Metrics.CardPadding
	reg.mu.Lock()
	reg.cfg.Theme.Density = theme.DensityComfortable
	reg.cfg.Theme.MotionSpeed = 400
	reg.cfg.Theme.PanelOpacity = 85
	next := reg.surfaceTheme()
	reg.mu.Unlock()

	if next.Metrics.CardPadding == before {
		t.Errorf("card padding stayed %d; density did not reach the surface theme", before)
	}
	if next.Surfaces.Panel == 0xff {
		t.Error("panel opacity did not reach the surface theme")
	}
	if next.Motion.Durations.Short >= theme.BaseMotion.Short {
		t.Errorf("motion speed did not reach the surface theme: short = %v", next.Motion.Durations.Short)
	}
}

// TestSurfaceRejectedCandidateKeepsTheOldPalette covers the failure path. A
// palette that does not resolve must leave every axis where it was, rather
// than half-applying and leaving surfaces mixing old and new roles.
func TestSurfaceRejectedCandidateKeepsTheOldPalette(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	before, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	// An incomplete palette is rejected outright; the caller keeps what it had.
	var broken theme.Tokens
	if _, err := ResolveTheme(cfg, cfg.Bar, broken); err == nil {
		t.Fatal("an empty palette resolved")
	}
	after, err := ResolveTheme(cfg, cfg.Bar, theme.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	if after.Palette.Surface != before.Palette.Surface || after.Metrics != before.Metrics {
		t.Error("a rejected candidate disturbed the resolved theme")
	}
}

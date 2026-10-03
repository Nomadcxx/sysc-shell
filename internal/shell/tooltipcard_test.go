package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/lint"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// cardMeasure is a fixed-advance measure: eight pixels a rune, sixteen tall,
// so every expected width below is arithmetic rather than a font's.
func cardMeasure(text string, _ ui.TextAttrs) (int, int) {
	return 8 * len([]rune(text)), 16
}

// textLeaves collects the text nodes of a laid-out card in paint order.
func textLeaves(n *ui.Node) []*ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindText {
		return []*ui.Node{n}
	}
	var out []*ui.Node
	for _, c := range n.Children {
		out = append(out, textLeaves(c)...)
	}
	return out
}

func TestATextTooltipIsInsetAndShrinkWrapped(t *testing.T) {
	t.Parallel()
	card, size := tooltipCard("Volume", nil, cardMeasure)

	wantW := 8*len("Volume") + 2*theme.MarginM
	wantH := 16 + 2*theme.MarginS
	if size.W != wantW || size.H != wantH {
		t.Fatalf("card size = %dx%d, want %dx%d", size.W, size.H, wantW, wantH)
	}
	if size.W >= lint.TooltipWidth {
		t.Fatalf("card width %d is not narrower than the %d cap", size.W, lint.TooltipWidth)
	}
	leaves := textLeaves(card)
	if len(leaves) != 1 {
		t.Fatalf("text nodes = %d, want 1", len(leaves))
	}
	got := leaves[0].Bounds
	if got.X != theme.MarginM || got.Y != theme.MarginS {
		t.Fatalf("text at (%d,%d), want inset (%d,%d)", got.X, got.Y, theme.MarginM, theme.MarginS)
	}
	if got.X+got.W > size.W-theme.MarginM || got.Y+got.H > size.H-theme.MarginS {
		t.Fatalf("text %+v reaches into the far inset of a %dx%d card", got, size.W, size.H)
	}
	if leaves[0].TextRole != theme.RoleLabel {
		t.Fatalf("text role = %v, want the label role", leaves[0].TextRole)
	}
}

func TestATreeTooltipIsAsWideAsItsWidestLine(t *testing.T) {
	t.Parallel()
	lines := []string{"Sunny", "12 - 21", "Wind 4.0 km/h NE", "Humidity 40%", "Updated 09:00"}
	root := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS}
	for _, s := range lines {
		root.Children = append(root.Children, &ui.Node{Kind: ui.KindText, Text: s})
	}
	_, size := tooltipCard("", root, cardMeasure)

	wantW := 8*len("Wind 4.0 km/h NE") + 2*theme.MarginM
	if size.W != wantW {
		t.Fatalf("card width = %d, want the widest line plus the inset, %d", size.W, wantW)
	}
	wantH := 5*16 + 4*theme.MarginXS + 2*theme.MarginS
	if size.H != wantH {
		t.Fatalf("card height = %d, want %d", size.H, wantH)
	}
}

func TestATreeTooltipsOwnPaddingIsIgnored(t *testing.T) {
	t.Parallel()
	root := &ui.Node{Kind: ui.KindColumn, Padding: 20, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Timer 24:13"},
	}}
	card, size := tooltipCard("", root, cardMeasure)

	if want := 8*len("Timer 24:13") + 2*theme.MarginM; size.W != want {
		t.Fatalf("card width = %d, want %d with the root padding ignored", size.W, want)
	}
	leaf := textLeaves(card)[0]
	if leaf.Bounds.X != theme.MarginM || leaf.Bounds.Y != theme.MarginS {
		t.Fatalf("text at (%d,%d), want the host inset alone (%d,%d)", leaf.Bounds.X, leaf.Bounds.Y, theme.MarginM, theme.MarginS)
	}
}

func TestALongTextTooltipWrapsAtTheCap(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("word ", 20) // 100 runes, 800 px unwrapped
	card, size := tooltipCard(strings.TrimSpace(text), nil, cardMeasure)

	if size.W > lint.TooltipWidth {
		t.Fatalf("card width = %d, want at most the %d cap", size.W, lint.TooltipWidth)
	}
	leaves := textLeaves(card)
	if len(leaves) < 2 {
		t.Fatalf("text nodes = %d, want the line wrapped", len(leaves))
	}
	var joined []string
	for _, l := range leaves {
		if w, _ := cardMeasure(l.Text, ui.TextAttrs{}); w > size.W-2*theme.MarginM {
			t.Fatalf("line %q is %d wide, past the %d content width", l.Text, w, size.W-2*theme.MarginM)
		}
		joined = append(joined, l.Text)
	}
	if got := strings.Join(joined, " "); got != strings.TrimSpace(text) {
		t.Fatalf("wrapped text = %q, want every word kept", got)
	}
}

func TestAPluginTooltipKeepsItsRolesAndTones(t *testing.T) {
	t.Parallel()
	// The notes plugin's tooltip, as sysc-plugins sends it.
	wire := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindText, Text: "Notes", Bold: true, Size: "label"},
		{Kind: v1.KindText, Text: "Open your Markdown library", Tone: v1.ToneSubtle},
	}}
	root, err := plugin.Convert(wire, v1.ViewTooltip)
	if err != nil {
		t.Fatal(err)
	}
	card, _ := tooltipCard("", root, cardMeasure)

	leaves := textLeaves(card)
	if len(leaves) != 2 {
		t.Fatalf("text nodes = %d, want 2", len(leaves))
	}
	if leaves[0].TextRole != theme.RoleLabel || !leaves[0].Bold {
		t.Fatalf("title role %v bold %v, want a bold label", leaves[0].TextRole, leaves[0].Bold)
	}
	if leaves[1].Tone != ui.ToneSubtle {
		t.Fatalf("second line tone = %v, want subtle", leaves[1].Tone)
	}
}

func TestWeathersConditionIsALabelTitle(t *testing.T) {
	t.Parallel()
	root := weatherTooltipTree(services.Reading{Observed: true, Code: 0, Temperature: 18, FetchedAt: time.Now()})
	if root == nil || len(root.Children) == 0 {
		t.Fatal("an observed reading built no tooltip tree")
	}
	if got := root.Children[0].TextRole; got != theme.RoleLabel {
		t.Fatalf("condition role = %v, want the label role", got)
	}
}

// Placement keeps the panel design's D5 rule: centred on the widget, off the
// bar's edge by the gap, clamped fully inside the output.
func TestTooltipPlacement(t *testing.T) {
	t.Parallel()
	const outW, outH = 1920, 1080
	cases := []struct {
		name   string
		edge   string
		anchor ui.Rect
		w, h   int
		want   ui.Rect
	}{
		{"centred below a top-bar widget", "top", ui.Rect{X: 900, Y: 0, W: 40, H: 44}, 200, 30,
			ui.Rect{X: 820, Y: 44 + theme.MarginS, W: 200, H: 30}},
		{"clamped at the right edge", "top", ui.Rect{X: 1900, Y: 0, W: 20, H: 44}, 200, 30,
			ui.Rect{X: outW - 200, Y: 44 + theme.MarginS, W: 200, H: 30}},
		{"clamped at the left edge", "top", ui.Rect{X: 0, Y: 0, W: 20, H: 44}, 200, 30,
			ui.Rect{X: 0, Y: 44 + theme.MarginS, W: 200, H: 30}},
		{"above a bottom-bar widget", "bottom", ui.Rect{X: 900, Y: outH - 44, W: 40, H: 44}, 200, 30,
			ui.Rect{X: 820, Y: outH - 44 - theme.MarginS - 30, W: 200, H: 30}},
		{"wider than the output", "top", ui.Rect{X: 10, Y: 0, W: 20, H: 44}, 3000, 30,
			ui.Rect{X: 0, Y: 44 + theme.MarginS, W: outW, H: 30}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := tooltipPlacement(c.edge, c.anchor, c.w, c.h, outW, outH); got != c.want {
				t.Fatalf("placement = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestSideTooltipPlacement(t *testing.T) {
	t.Parallel()
	const outW, outH = 1000, 600
	cases := []struct {
		name   string
		edge   string
		anchor ui.Rect
		w, h   int
		want   ui.Rect
	}{
		{"left bar opens right and clamps at top", "left", ui.Rect{X: 40, Y: 2, W: 20, H: 20}, 80, 50,
			ui.Rect{X: 40 + 20 + tooltipGap, Y: 0, W: 80, H: 50}},
		{"right bar opens left and clamps at bottom", "right", ui.Rect{X: 940, Y: 570, W: 20, H: 20}, 80, 50,
			ui.Rect{X: 940 - 80 - tooltipGap, Y: outH - 50, W: 80, H: 50}},
		{"oversized card clamps in both dimensions", "left", ui.Rect{X: 0, Y: 0, W: 10, H: 10}, 1200, 800,
			ui.Rect{W: outW, H: outH}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tooltipPlacement(tc.edge, tc.anchor, tc.w, tc.h, outW, outH); got != tc.want {
				t.Fatalf("placement = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The laptop's output is 1536 logical wide at 1.25; a tray item at its right
// edge must not push the card off it.
func TestATooltipAtTheRightEdgeOfAScaledOutputStaysInside(t *testing.T) {
	t.Parallel()
	got := tooltipPlacement("top", ui.Rect{X: 1510, Y: 0, W: 26, H: 38}, 180, 30, 1536, 960)
	if got.X < 0 || got.X+got.W > 1536 {
		t.Fatalf("placement %+v leaves the 1536 output", got)
	}
}

// A tray item's tooltip is its title and description joined by a newline;
// each stays its own line.
func TestATextTooltipKeepsItsLineBreaks(t *testing.T) {
	t.Parallel()
	card, _ := tooltipCard("Mail\n3 unread", nil, cardMeasure)
	var got []string
	for _, l := range textLeaves(card) {
		got = append(got, l.Text)
	}
	if strings.Join(got, "|") != "Mail|3 unread" {
		t.Fatalf("lines = %q, want the title and description apart", got)
	}
}

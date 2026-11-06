package ui

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// An open menu with a row height lays its options out as a Material sheet:
// MenuSheetPadding above the first row and below the last, every row the
// same height, and the popup bounds covering the padding.
func TestMenuSheetRows(t *testing.T) {
	measure := func(s string, _ TextAttrs) (int, int) { return 8 * len(s), 16 }
	n := &Node{Kind: KindMenu, Text: "Theme", Padding: 6, Height: 36, ItemHeight: 48, Children: []*Node{
		{Kind: KindText, Text: "Dark"}, {Kind: KindText, Text: "Light"}, {Kind: KindText, Text: "Auto"},
	}}
	root := &Node{Kind: KindColumn, Children: []*Node{n}}
	if err := LayoutColumn(root, Rect{W: 200, H: 400}, measure); err != nil {
		t.Fatal(err)
	}
	for i, c := range n.Children {
		want := Rect{X: MenuSheetPadding, Y: 36 + MenuSheetPadding + i*48, W: 200 - 2*MenuSheetPadding, H: 48}
		if c.Bounds != want {
			t.Fatalf("option %d = %+v, want %+v", i, c.Bounds, want)
		}
	}
	if got, want := MenuPopupBounds(n), (Rect{Y: 36, W: 200, H: 3*48 + 2*MenuSheetPadding}); got != want {
		t.Fatalf("popup = %+v, want %+v", got, want)
	}
	// Without a row height the list keeps its measured rows and no sheet
	// padding, as menus outside panels still do.
	plain := &Node{Kind: KindMenu, Text: "Theme", Height: 36, Children: []*Node{{Kind: KindText, Text: "Dark"}}}
	if err := LayoutColumn(&Node{Kind: KindColumn, Children: []*Node{plain}}, Rect{W: 200, H: 400}, measure); err != nil {
		t.Fatal(err)
	}
	if got := MenuPopupBounds(plain); got != plain.Children[0].Bounds {
		t.Fatalf("plain popup = %+v, want the option row %+v", got, plain.Children[0].Bounds)
	}
}

func TestSizeControlsGivesMenusTheDensityRow(t *testing.T) {
	for density, want := range map[theme.Density]int{
		theme.DensityMini: 40, theme.DensityCompact: 44, theme.DensityDefault: 48,
		theme.DensityComfortable: 52, theme.DensitySpacious: 56,
	} {
		m, _ := theme.MetricsFor(density)
		menu := &Node{Kind: KindMenu}
		SizeControls(&Node{Kind: KindColumn, Children: []*Node{{Kind: KindRow, Children: []*Node{menu}}}}, m)
		if menu.ItemHeight != want {
			t.Errorf("%s menu row = %d, want %d", density, menu.ItemHeight, want)
		}
	}
}

// A menu row is a bare button: no chrome at rest, the label start-aligned
// after an optional leading glyph, trailing content at the far edge.
func TestMenuRowNode(t *testing.T) {
	measure := func(s string, _ TextAttrs) (int, int) { return 8 * len(s), 16 }
	chevron := &Node{Kind: KindIcon, Icon: "chevron_right"}
	row := MenuRowNode(MenuRow{Label: "Open", Action: "open", Leading: "check", Trailing: chevron, Role: "menuitemcheckbox", Name: "Open (checked)"}, 48)
	if row.Kind != KindButton || !row.Bare || row.Fill != FillNone || row.Height != 48 || !row.Focusable {
		t.Fatalf("row = %+v, want a focusable bare 48px button", row)
	}
	if row.Name != "Open (checked)" || row.Role != "menuitemcheckbox" || row.Action != "open" {
		t.Fatalf("row identity = %q %q %q", row.Name, row.Role, row.Action)
	}
	if err := LayoutColumn(&Node{Kind: KindColumn, Children: []*Node{row}}, Rect{W: 280, H: 48}, measure); err != nil {
		t.Fatal(err)
	}
	start := row.Children[0].Children[0]
	lead, label := start.Children[0], start.Children[1]
	if lead.Bounds.X != MenuRowInset || label.Bounds.X != lead.Bounds.X+lead.Bounds.W+MenuRowGap {
		t.Fatalf("leading %+v label %+v: want the glyph at the inset and the label %dpx after it", lead.Bounds, label.Bounds, MenuRowGap)
	}
	if end := chevron.Bounds.X + chevron.Bounds.W; end != 280-MenuRowInset {
		t.Fatalf("trailing ends at %d, want %d", end, 280-MenuRowInset)
	}
	if label.Bounds.Y+label.Bounds.H/2 != 24 {
		t.Fatalf("label %+v is not centred in the 48px row", label.Bounds)
	}
	disabled := MenuRowNode(MenuRow{Label: "Quit", Disabled: true}, 48)
	if disabled.Focusable || !disabled.State.Has(StateDisabled) || disabled.Name != "Quit" {
		t.Fatalf("disabled row = %+v", disabled)
	}
}

func TestMenuDividerIsHorizontal(t *testing.T) {
	d := MenuDividerNode()
	if err := LayoutColumn(&Node{Kind: KindColumn, Children: []*Node{d}}, Rect{W: 200, H: 100}, func(string, TextAttrs) (int, int) { return 8, 16 }); err != nil {
		t.Fatal(err)
	}
	line := d.Children[0].Bounds
	if d.Bounds.H != MenuDivider || line.Y != MenuSheetPadding || line.W != 200-2*MenuSheetPadding {
		t.Fatalf("divider %+v line %+v, want %dpx with a horizontal line %dpx down", d.Bounds, line, MenuDivider, MenuSheetPadding)
	}
}

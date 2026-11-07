package ui

// Material 3 menu geometry, in logical pixels. A row's height is the
// density's theme Metrics.MenuItem, which a menu node carries as ItemHeight.
const (
	// MenuSheetPadding is the space above the first row and below the last.
	MenuSheetPadding = 8
	// MenuRowInset is a row's leading and trailing padding.
	MenuRowInset = 12
	// MenuRowGap separates a row's leading glyph from its label.
	MenuRowGap = 12
	// MenuDivider is a separator: a 1px line with MenuSheetPadding each side.
	MenuDivider = 1 + 2*MenuSheetPadding
	// MenuRowRadius rounds a row's hover, focus and selection layers.
	MenuRowRadius = 4
)

// MenuRow is one row of a menu surface.
type MenuRow struct {
	Label  string
	Action string
	// Name is the accessible name; the label stands in when it is empty.
	Name string
	Role string
	// Leading is an icon drawn before the label, such as a check. LeadingSlot
	// keeps the glyph's room on a row without one, so labels in a list that
	// mixes them stay aligned.
	Leading     string
	LeadingSlot bool
	// Trailing sits at the row's far edge: a submenu chevron or a shortcut.
	Trailing *Node
	Tone     Tone
	Disabled bool
}

// MenuRowNode builds a menu row of the given height: a bare button, so its
// only chrome is the hover, focus and press layers, around one row of
// leading glyph, start-aligned label and trailing content.
func MenuRowNode(r MenuRow, height int) *Node {
	start := &Node{Kind: KindRow, Gap: MenuRowGap, CenterY: true}
	if r.Leading != "" || r.LeadingSlot {
		// An icon with no name measures like one and paints nothing.
		start.Children = append(start.Children, &Node{Kind: KindIcon, Icon: r.Leading})
	}
	start.Children = append(start.Children, &Node{Kind: KindText, Text: r.Label, Tone: r.Tone})
	content := start
	if r.Trailing != nil {
		// A pinned row takes two children: the start group and the trailing end.
		content = &Node{Kind: KindRow, CenterY: true, PinEnd: true, Children: []*Node{start, r.Trailing}}
	}
	name := r.Name
	if name == "" {
		name = r.Label
	}
	n := &Node{
		Kind: KindButton, Bare: true, Height: height, Padding: MenuRowInset, Radius: MenuRowRadius,
		Action: r.Action, Name: name, Role: r.Role, Focusable: !r.Disabled,
		Children: []*Node{content},
	}
	if r.Disabled {
		n.State |= StateDisabled
	}
	return n
}

// MenuDividerNode is a menu's separator: a horizontal line with the sheet's
// padding above and below, MenuDivider tall.
func MenuDividerNode() *Node {
	return &Node{Kind: KindColumn, Padding: MenuSheetPadding, Children: []*Node{{Kind: KindSeparator}}}
}

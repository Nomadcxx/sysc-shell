package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/lint"
)

// The host inset every tooltip gets, text or tree (plan T3). A tree does not
// bring its own: weather and the plugins set none, and honouring the ones that
// do would give each widget a different edge.
const (
	tooltipInsetX = theme.MarginM
	tooltipInsetY = theme.MarginS
	// tooltipMaxLines bounds a wrapped text tooltip; the last line ellipsises.
	tooltipMaxLines = 6
)

// tooltipCard lays out one tooltip and returns the card and its size, both in
// logical pixels at the card's own origin.
//
// A text tooltip is label-role type. A tree keeps the roles and tones its nodes
// name; only its root padding is dropped for the host inset. Either way the
// card shrink-wraps its widest line up to the plugin tooltip cap (plan T4), and
// a text line longer than the cap wraps instead of clipping.
func tooltipCard(text string, root *ui.Node, measure ui.MeasureText) (*ui.Node, ui.Rect) {
	capW := lint.TooltipWidth
	if root != nil && root.MaxWidth > 0 && root.MaxWidth < capW {
		capW = root.MaxWidth
	}
	maxContentW := max(capW-2*tooltipInsetX, 1)
	maxContentH := max(lint.TooltipHeight-2*tooltipInsetY, 1)

	var col *ui.Node
	if root == nil {
		col = &ui.Node{Kind: ui.KindColumn}
		label := func(s string) int {
			w, _ := measure(s, ui.TextAttrs{Role: theme.RoleLabel})
			return w
		}
		for _, line := range wrapLines(text, maxContentW, label, tooltipMaxLines) {
			col.Children = append(col.Children, &ui.Node{Kind: ui.KindText, Text: line, TextRole: theme.RoleLabel})
		}
	} else {
		// A copy, so the tree the widget owns keeps its own padding.
		c := *root
		c.Padding = 0
		col = &c
	}

	content := ui.Rect{X: tooltipInsetX, Y: tooltipInsetY, W: maxContentW, H: maxContentH}
	if ui.LayoutColumn(col, content, measure) != nil {
		return nil, ui.Rect{}
	}
	content.W = min(max(widestLine(col, measure)-content.X, 1), maxContentW)
	if h, err := ui.ContentHeight(col, content.W, measure); err == nil {
		content.H = min(max(h, 1), maxContentH)
	}
	if ui.LayoutColumn(col, content, measure) != nil {
		return nil, ui.Rect{}
	}
	return col, ui.Rect{W: content.W + 2*tooltipInsetX, H: content.H + 2*tooltipInsetY}
}

// widestLine is the right edge of the furthest-reaching leaf. A column lays
// its text out at the full content width, so a text leaf reaches only as far
// as its own measured run; any other leaf reaches as far as it was placed.
func widestLine(n *ui.Node, measure ui.MeasureText) int {
	if n == nil {
		return 0
	}
	if len(n.Children) == 0 {
		if n.Kind == ui.KindText {
			w, _ := measure(n.Text, ui.TextAttrsOf(n))
			return n.Bounds.X + min(w, n.Bounds.W)
		}
		return n.Bounds.X + n.Bounds.W
	}
	right := 0
	for _, c := range n.Children {
		right = max(right, widestLine(c, measure))
	}
	return right
}

// tooltipGap is the space between the bar's edge and the card.
const tooltipGap = theme.MarginS

// tooltipPlacement positions a card beside its anchor, centred on it and
// clamped fully inside the output, in output-logical coordinates. On a bottom
// bar it goes above the anchor; otherwise below.
//
// This is the panel design's D5 rule: anchored off the triggering bar's edge,
// aligned to the triggering widget, clamped inside the output.
func tooltipPlacement(edge string, anchor ui.Rect, width, height, outputWidth, outputHeight int) ui.Rect {
	width = min(width, outputWidth)
	x := anchor.X + anchor.W/2 - width/2
	x = max(min(x, outputWidth-width), 0)

	y := anchor.Y + anchor.H + tooltipGap
	if edge == "bottom" {
		y = anchor.Y - height - tooltipGap
	}
	y = max(min(y, outputHeight-height), 0)
	return ui.Rect{X: x, Y: y, W: width, H: height}
}

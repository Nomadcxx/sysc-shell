package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The centre pill is the hairline-split design in
// docs/plans/2026-09-25-centre-pill-design.md: one outlined capsule holding
// the SYSC mark, a hairline, the time and the date, which opens the control
// centre as a single control.
const (
	// centreMarkHeight is the mark's height inside the pill.
	centreMarkHeight = 11 // token-exempt: the cap height of 15 px Inter, so the mark sits on the text line
	// centreRuleHeight is the hairline's height: the mark's box and one
	// pixel above and below it, so the rule frames the mark rather than the
	// taller text line.
	centreRuleHeight = centreMarkHeight + 2
	// centrePadX is the pill's horizontal padding, measured from the outer
	// edge; the hairline stroke sits inside it.
	centrePadX = 11 // token-exempt: measured from the approved mockup
	// centreTooltipFormat is the full date the pill shows on hover, since
	// the pill itself carries the short form.
	centreTooltipFormat = "Monday 2 January 2006"
)

// groupHoldsWordmark reports whether a group's members include the SYSC
// wordmark. Such a group is the centre pill rather than a plain group.
func groupHoldsWordmark(items []config.Item) bool {
	for _, item := range items {
		if item.ID == "wordmark" {
			return true
		}
	}
	return false
}

// buildCentrePill builds the centre composition from a group's members.
//
// The capsule is the one control: it carries the action, the accessible name
// and the tooltip, so the time and date open the control centre as the mark
// does. The mark inside is decorative. The first clock is the lead figure and
// the clocks after it are subtle, so the time reads before the date. Clocks get
// no width floor: tabular figures already hold the time still, and the floor
// was the slack that left "15:04" in a pill sized for a date.
func buildCentrePill(items []config.Item, pad int, m theme.Metrics) textWidget {
	built := buildWidgetsWithClockFloor(items, noCapsule, m, "")
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM}
	// The hairlines are members too: the bar rebuilds a group's row from its
	// members on every layout, so a node that is not one disappears.
	members := make([]textWidget, 0, len(built)+2)
	clocks := 0
	for i, member := range built {
		n := member.node
		if n == nil {
			continue
		}
		if i < len(items) && items[i].ID == "clock" {
			if clocks == 0 {
				n.TextRole = theme.RoleFigure
			} else {
				n.Tone = ui.ToneSubtle
			}
			clocks++
		}
		isMark := n.Kind == ui.KindWordmark && n.Mark == ""
		if isMark {
			n.ImageH, n.ImageW = centreMarkHeight, render.WordmarkWidth(centreMarkHeight)
			n.Action, n.Name, n.Role = "", "", ""
		}
		// A hairline separates the mark from whatever sits beside it.
		prevMark := len(row.Children) > 0 && row.Children[len(row.Children)-1].Kind == ui.KindWordmark
		if len(row.Children) > 0 && (isMark || prevMark) {
			rule := &ui.Node{Kind: ui.KindSeparator, Height: centreRuleHeight}
			row.Children = append(row.Children, rule)
			members = append(members, textWidget{node: rule, refresh: func(barView) bool { return false }})
		}
		row.Children = append(row.Children, n)
		members = append(members, member)
	}

	pill := &ui.Node{
		Kind: ui.KindCapsule, Key: "centre", Shape: ui.ShapeMedium,
		Padding: pad, PaddingX: centrePadX,
		Stroke: 1, StrokeFill: ui.FillOutlineVariant, // token-exempt: a hairline border, not a ladder value
		Action: panelControlCenterAction, Name: "Control centre", Role: "button",
		Children: []*ui.Node{row},
	}
	return textWidget{
		node: pill, inner: row, members: members,
		refresh: func(v barView) bool {
			if !v.Now.IsZero() {
				pill.Tooltip = v.Now.Format(centreTooltipFormat)
			}
			return refreshMembers(members, v)
		},
	}
}

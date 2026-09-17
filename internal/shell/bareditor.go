package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// dropOutcome is what a drop on a lane means: either join the chip at Join, or
// insert at Insert. Join is -1 when the drop was an insertion, which is what
// keeps the two readings from being confused for one another.
type dropOutcome struct {
	Insert int
	Join   int
}

// dropEdgeShare is how much of a chip's width, at each end, reads as "put it
// beside this one" rather than "put it with this one". A quarter at each end
// leaves the inner half joining, which is the rule the design states.
const dropEdgeShare = 4

// resolveDrop turns chip bounds and a drop point into an outcome. It is a pure
// function so the rule is testable without a live host, and so the pointer
// path is a thin caller over it.
//
// Each lane owns exactly one drop zone and resolves the target itself.
// ui.FindDropZone tests a node before descending into it, so an outer lane
// zone always wins over a nested group zone; making groups drop zones would
// leave them unreachable. Rather than change a primitive the plugin view also
// uses, the lane does the work.
//
// Only the horizontal position matters. The lane's zone has already been hit
// with its own drop slop, so a pointer slightly above or below a chip is still
// on that chip as far as this decision goes.
func resolveDrop(chips []ui.Rect, x, _ int) dropOutcome {
	for i, chip := range chips {
		if chip.W <= 0 {
			continue
		}
		if x < chip.X {
			// Before this chip, and after the previous one's trailing edge:
			// the gap between two chips inserts at that boundary.
			return dropOutcome{Insert: i, Join: -1}
		}
		if x >= chip.X+chip.W {
			continue
		}
		edge := chip.W / dropEdgeShare
		switch {
		case x < chip.X+edge:
			return dropOutcome{Insert: i, Join: -1}
		case x >= chip.X+chip.W-edge:
			return dropOutcome{Insert: i + 1, Join: -1}
		default:
			return dropOutcome{Join: i}
		}
	}
	// Past the last chip, or an empty lane.
	return dropOutcome{Insert: len(chips), Join: -1}
}

// The lane strip. This is the Layout group at the top of the Bar section, and
// the one named exception to the settings foundation's no-cards rule: a drop
// target has to be a visible surface, so lanes carry card chrome. The option
// rows in the inspector follow the foundation unchanged.

// barLaneLabels names each lane for the strip. The configuration tokens are
// "left", "center" and "right"; the headings are what a person reads.
var barLaneLabels = map[string]string{
	"left":   "Left",
	"center": "Centre",
	"right":  "Right",
}

// barChipDragType is what a lane's drop zone accepts. It is one type for every
// chip, because a lane takes any widget.
const barChipDragType = "bar-widget"

// barChipHeight is one chip's extent, from the density ladder rather than a
// literal, so the strip follows it like every other surface.
//
// The design drew a lane as a single horizontal row of chips with a fixed
// height of one control plus its card padding. That does not survive the
// default configuration: the right lane alone carries six placements, one of
// them a group of four, which overruns the 782 logical pixels the pane has to
// give. internal/ui has no wrapping row and KindScroll is vertical only, so a
// horizontal strip could only have clipped -- the one outcome this project
// refuses. A lane is therefore a vertical stack, which is also what both
// reference shells do with their widget lists.
func barChipHeight(h *PanelHost) int { return h.metrics().StandardControl }

// barChip is one widget in a lane: a drag source carrying its own address, a
// grip, the widget's display name, and a control that opens the inspector.
//
// The chip carries a width. A's clipping defect was a node with no width
// inside a row that right-pins, and this strip composes chips and lanes into
// that same pane, so every column here states its own.
func barChip(h *PanelHost, ref config.ItemRef, it config.Item, selected bool, width int) *ui.Node {
	name := settings.WidgetName(it)
	addr := barRefAction(ref)
	m := h.metrics()

	chip := &ui.Node{
		Kind: ui.KindDragSource, DragType: barChipDragType, Payload: addr,
		Action: "bar-select:" + addr, Name: name, Role: "button", Focusable: true,
		Shape: ui.ShapeStadium, Fill: ui.FillContainerHighest,
		Width: width, Height: barChipHeight(h), Padding: m.ButtonPadding, Gap: theme.MarginS,
	}
	if selected {
		chip.State |= ui.StateSelected
		chip.Fill = ui.FillAccent
	}
	// PinEnd right-pins the last child of a two-child row, so the grip and the
	// label travel together on the left and the inspector control sits at the
	// far edge, where the reset control sits on every settings row.
	chip.PinEnd = true
	chip.Children = []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "drag_indicator", IconSize: m.IconSmall, Tone: ui.ToneSubtle},
			{Kind: ui.KindText, Text: name},
		}},
		{
			Kind: ui.KindButton, Action: "bar-inspect:" + addr,
			Name: "Configure " + name, Role: "button", Focusable: true,
			Width: m.IconNormal, Height: m.IconNormal,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "tune", IconSize: m.IconSmall}},
		},
	}
	return chip
}

// barGroupChip draws a group as a bounded run of its members with a dissolve
// control, mirroring the live bar where a group is one capsule holding
// uncapsuled members. It is not a drop zone: ui.FindDropZone tests a node
// before descending, so a zone here would be unreachable behind the lane's.
func barGroupChip(h *PanelHost, ref config.ItemRef, it config.Item, selected string, width int) *ui.Node {
	m := h.metrics()
	addr := barRefAction(ref)
	inner := max(width-2*m.CardPadding, 0)
	run := &ui.Node{
		Kind: ui.KindColumn, Gap: theme.MarginXS, Width: width,
		Shape: ui.ShapeSmall, Fill: ui.FillContainerHighest,
		Padding: m.CardPadding, Name: "Group", Role: "group",
	}
	run.Children = append(run.Children, &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginS, Width: inner, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Group", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
			{
				Kind: ui.KindButton, Action: "bar-ungroup:" + addr,
				Name: "Dissolve group", Role: "button", Focusable: true,
				Width: m.StandardControl, Height: m.StandardControl,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "link_off", IconSize: m.IconSmall}},
			},
		},
	})
	for j := range it.Items {
		member := config.ItemRef{Lane: ref.Lane, Path: config.ItemPath{Index: ref.Path.Index, Member: j}}
		run.Children = append(run.Children,
			barChip(h, member, it.Items[j], barRefAction(member) == selected, inner))
	}
	return run
}

// barLane is one lane: a caption, then the drop zone holding the chips and the
// add control.
func barLane(h *PanelHost, bar config.Bar, name string, width int) *ui.Node {
	m := h.metrics()
	inner := max(width-2*m.CardPadding, 0)
	zone := &ui.Node{
		Kind: ui.KindDropZone, Accept: []string{barChipDragType},
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Padding: m.CardPadding,
		Width: width, Gap: theme.MarginXS,
		Name: barLaneLabels[name] + " lane", Role: "group",
	}

	lane := barLaneItems(bar, name)
	for i := range lane {
		ref := config.ItemRef{Lane: name, Path: config.ItemPath{Index: i, Member: -1}}
		if lane[i].ID == "group" {
			zone.Children = append(zone.Children, barGroupChip(h, ref, lane[i], h.barSelected, inner))
			continue
		}
		zone.Children = append(zone.Children,
			barChip(h, ref, lane[i], barRefAction(ref) == h.barSelected, inner))
	}
	if len(lane) == 0 {
		zone.Children = append(zone.Children, &ui.Node{
			Kind: ui.KindText, Text: "Nothing here yet.",
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	zone.Children = append(zone.Children, &ui.Node{
		Kind: ui.KindButton, Action: "bar-add:" + name,
		Name: "Add a widget to the " + barLaneLabels[name] + " lane",
		Role: "button", Focusable: true,
		Width: inner, Height: m.StandardControl, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "add", IconSize: m.IconSmall}},
	})

	header := &ui.Node{Kind: ui.KindText, Text: barLaneLabels[name],
		TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Width: width,
		Children: []*ui.Node{header, zone}}
}

// barLaneItems reads one lane out of a bar by name.
func barLaneItems(bar config.Bar, name string) []config.Item {
	switch name {
	case "left":
		return bar.Left
	case "center":
		return bar.Center
	case "right":
		return bar.Right
	}
	return nil
}

// barRefAction encodes an address into an action suffix, and barParseRef reads
// one back. Actions are strings, so the address has to survive that trip.
func barRefAction(ref config.ItemRef) string {
	return fmt.Sprintf("%s:%d:%d", ref.Lane, ref.Path.Index, ref.Path.Member)
}

func barParseRef(s string) (config.ItemRef, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return config.ItemRef{}, false
	}
	if barLaneLabels[parts[0]] == "" {
		return config.ItemRef{}, false
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil {
		return config.ItemRef{}, false
	}
	member, err := strconv.Atoi(parts[2])
	if err != nil {
		return config.ItemRef{}, false
	}
	return config.ItemRef{Lane: parts[0], Path: config.ItemPath{Index: index, Member: member}}, true
}

// barLaneStrip is the Layout group: the three lanes, stacked.
func barLaneStrip(h *PanelHost) *ui.Node {
	width := settingsBodyWidth(h)
	strip := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Width: width, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Layout", TextRole: theme.RoleLabel},
	}}
	for _, name := range config.LaneNames() {
		strip.Children = append(strip.Children, barLane(h, h.barEditedBar(), name, width))
	}
	return strip
}

// barEditedBar is the bar the strip is editing: the shared one, or the
// override belonging to the selected output.
//
// D7 fixes the granularity, and it is the loader's rather than a choice made
// here: applyBar starts from the resolved base bar and takes each lane whole,
// so a lane is inherited entire or overridden entire. An output with no
// override of its own therefore shows the shared lanes, which is what it will
// actually draw.
func (h *PanelHost) barEditedBar() config.Bar {
	if h.barOutput == "" {
		return h.draft.Bar
	}
	for _, o := range h.draft.Outputs {
		if o.Connector == h.barOutput {
			return barMergeInherited(h.draft.Bar, o.Bar)
		}
	}
	return h.draft.Bar
}

// barMergeInherited fills a lane the override does not carry with the shared
// one, which is exactly what applyBar does at load.
func barMergeInherited(base, over config.Bar) config.Bar {
	out := over
	if out.Left == nil {
		out.Left = base.Left
	}
	if out.Center == nil {
		out.Center = base.Center
	}
	if out.Right == nil {
		out.Right = base.Right
	}
	return out
}

// barLaneOverridden reports whether the edited output carries this lane itself
// rather than inheriting it. The strip has to say so: reordering one widget on
// one output forks that whole lane for that output, and later changes to the
// shared lane stop reaching it. A per-widget override affordance would be a
// lie, so the interface states the granularity it actually has.
func (h *PanelHost) barLaneOverridden(name string) bool {
	if h.barOutput == "" {
		return false
	}
	for _, o := range h.draft.Outputs {
		if o.Connector != h.barOutput {
			continue
		}
		return barLaneItems(o.Bar, name) != nil
	}
	return false
}

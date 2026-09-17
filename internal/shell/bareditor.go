package shell

import (
	"fmt"
	"slices"
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

// dropEdgeShare is how much of a row, at each end, reads as "put it beside
// this one" rather than "put it with this one". A quarter at each end leaves
// the middle half joining, which is the rule the design states.
const dropEdgeShare = 4

// resolveDrop turns row bounds and a drop point into an outcome. It is a pure
// function so the rule is testable without a live host, and so the pointer
// path is a thin caller over it.
//
// The decision is on the vertical axis, because a lane is a vertical stack of
// full-width rows. It resolved on the horizontal one first, which is a rule
// that cannot tell two rows apart when they share an x range: every drop
// landed on the first row, so dragging down did nothing and a group could
// never be dropped into.
//
// Each lane owns exactly one drop zone and resolves the target itself.
// ui.FindDropZone tests a node before descending into it, so an outer lane
// zone always wins over a nested one; making groups their own zones would
// leave them unreachable. Rather than change a primitive the plugin view also
// uses, the lane does the work.
func resolveDrop(rows []ui.Rect, _, y int) dropOutcome {
	for i, row := range rows {
		if row.H <= 0 {
			continue
		}
		if y < row.Y {
			// Above this row and below the previous one: the gap between two
			// rows inserts at that boundary.
			return dropOutcome{Insert: i, Join: -1}
		}
		if y >= row.Y+row.H {
			continue
		}
		edge := row.H / dropEdgeShare
		switch {
		case y < row.Y+edge:
			return dropOutcome{Insert: i, Join: -1}
		case y >= row.Y+row.H-edge:
			return dropOutcome{Insert: i + 1, Join: -1}
		default:
			return dropOutcome{Join: i}
		}
	}
	// Below the last row, or an empty lane.
	return dropOutcome{Insert: len(rows), Join: -1}
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
func barChip(h *PanelHost, ref config.ItemRef, it config.Item, selected bool, width int, canGroup bool) *ui.Node {
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
	// Exactly one child, and it is a row. layoutButtonContent gives a single
	// row or column child a full Layout pass and honours PinEnd on it; with
	// two or more children it falls back to an inline path that measures each
	// child but never descends, so a nested row's icon and label would be
	// given no box and the chip would paint as an empty pill. Found on the
	// laptop, where exactly that happened.
	// The chip carries its own actions rather than leaving them to the
	// inspector. Lanes stack vertically, so with a full bar the inspector
	// lands hundreds of pixels below the scroll viewport and remove and group
	// were reachable from nowhere at all. A control belongs next to the thing
	// it acts on.
	// Each of these draws a glyph and no text, so the name has to be reachable
	// by hover as well as by screen reader. Three per chip is dense enough
	// that guessing is not a reasonable thing to ask of anyone.
	glyph := func(icon, action, label string) *ui.Node {
		return &ui.Node{
			Kind: ui.KindButton, Action: action, Name: label, Role: "button", Focusable: true,
			Tooltip: label,
			Width:   m.IconNormal, Height: m.IconNormal,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconSmall}},
		}
	}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS}
	if canGroup {
		// Only offered where there is something after this chip to fold it
		// together with, rather than offering a control that would fail.
		trailing.Children = append(trailing.Children,
			glyph("link", "bar-group:"+addr, "Group with the widget below"))
	}
	trailing.Children = append(trailing.Children,
		glyph("tune", "bar-inspect:"+addr, "Options for "+name),
		glyph("close", "bar-remove:"+addr, "Remove "+name+" from the bar"),
	)

	chip.Children = []*ui.Node{{
		Kind: ui.KindRow, Gap: theme.MarginS, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: "drag_indicator", IconSize: m.IconSmall, Tone: ui.ToneSubtle},
				{Kind: ui.KindText, Text: name},
			}},
			trailing,
		},
	}}
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
		// A muted accent wash rather than another neutral level. The group sat
		// at the same fill as the chips inside it, so a grouping read as three
		// unrelated rows; contents keep the surface foreground, so the members
		// stay ordinary chips rather than becoming primary ones.
		Shape: ui.ShapeSmall, Fill: ui.FillSoft,
		Padding: m.CardPadding, Name: "Group", Role: "group",
		// Not focusable, so this never becomes a click target; the action is
		// here only so the drop path can find the group's whole body and use
		// it as one row of the lane.
		Action: "bar-groupbody:" + addr,
	}
	// The group's own handle. A group is draggable, selectable and
	// inspectable under D2, and it renders as a column, which cannot itself be
	// a drag source -- a drag source lays its children out inline, the way a
	// button does. The handle carries the group's address so the group moves
	// as one thing.
	handle := &ui.Node{
		Kind: ui.KindDragSource, DragType: barChipDragType, Payload: addr,
		Action: "bar-select:" + addr, Name: "Group", Role: "button", Focusable: true,
		Shape: ui.ShapeStadium, Fill: ui.FillContainerHighest,
		Height: barChipHeight(h), Padding: m.ButtonPadding, Gap: theme.MarginS,
		Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "drag_indicator", IconSize: m.IconSmall, Tone: ui.ToneSubtle},
			{Kind: ui.KindText, Text: "Group", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		},
	}
	if barRefAction(ref) == selected {
		handle.State |= ui.StateSelected
		handle.Fill = ui.FillAccent
	}
	run.Children = append(run.Children, &ui.Node{
		Kind: ui.KindRow, Gap: theme.MarginS, Width: inner, PinEnd: true, Children: []*ui.Node{
			handle,
			{
				Kind: ui.KindButton, Action: "bar-ungroup:" + addr,
				Name: "Ungroup these widgets", Role: "button", Focusable: true,
				Tooltip: "Ungroup these widgets",
				Width:   m.StandardControl, Height: m.StandardControl,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "link_off", IconSize: m.IconSmall}},
			},
		},
	})
	for j := range it.Items {
		member := config.ItemRef{Lane: ref.Lane, Path: config.ItemPath{Index: ref.Path.Index, Member: j}}
		// A group is already one level deep, so its members cannot group
		// further: the cap is expressed by not offering the control.
		run.Children = append(run.Children,
			barChip(h, member, it.Items[j], barRefAction(member) == selected, inner, false))
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
		Action: "bar-lane:" + name,
		Name:   barLaneLabels[name] + " lane", Role: "group",
	}

	lane := barLaneItems(bar, name)
	for i := range lane {
		ref := config.ItemRef{Lane: name, Path: config.ItemPath{Index: i, Member: -1}}
		if lane[i].ID == "group" {
			zone.Children = append(zone.Children, barGroupChip(h, ref, lane[i], h.barSelected, inner))
			continue
		}
		zone.Children = append(zone.Children,
			barChip(h, ref, lane[i], barRefAction(ref) == h.barSelected, inner, i+1 < len(lane) && lane[i+1].ID != "group"))
	}
	if len(lane) == 0 {
		zone.Children = append(zone.Children, &ui.Node{
			Kind: ui.KindText, Text: "Nothing here yet.",
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	// A bare plus is a guess. The control says what it adds and where, which
	// also makes the three lanes' controls tell each other apart.
	add := "Add a widget to the " + barLaneLabels[name] + " lane"
	zone.Children = append(zone.Children, &ui.Node{
		Kind: ui.KindButton, Action: "bar-add:" + name,
		Name: add, Role: "button", Focusable: true, Tooltip: add,
		Width: inner, Height: m.StandardControl, Shape: ui.ShapeMedium,
		Gap: theme.MarginS,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "add", IconSize: m.IconSmall},
			{Kind: ui.KindText, Text: "Add widget", TextRole: theme.RoleCaption},
		}}},
	})

	// D7 in the interface. A lane is inherited whole or overridden whole,
	// because applyBar takes it whole, so the heading says which this is and
	// offers the only reset that exists -- the whole lane. A per-widget
	// override affordance would be a lie.
	label := barLaneLabels[name]
	header := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: width, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}}

	if h.barOutput != "" {
		state := "Inherited"
		trailing := &ui.Node{Kind: ui.KindText, Text: state,
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
		if h.barLaneOverridden(name) {
			trailing = &ui.Node{
				Kind: ui.KindButton, Action: "bar-reset-lane:" + name,
				Name: "Reset the " + label + " lane to shared", Role: "button", Focusable: true,
				Height: m.StandardControl, Padding: m.ButtonPadding, Shape: ui.ShapeMedium,
				Children: []*ui.Node{{Kind: ui.KindText, Text: "Overridden -- reset to shared"}},
			}
		}
		header.Children = append(header.Children, trailing)
	}
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

// barLaneStrip is the Layout group: the output selector, the three lanes, and
// the inspector for whatever is selected.
func barLaneStrip(h *PanelHost) *ui.Node {
	return h.barLaneStripFor(nil)
}

func (h *PanelHost) barLaneStripFor(r *Registry) *ui.Node {
	width := settingsBodyWidth(h)
	// One line of orientation. Nothing else on this surface says that the rows
	// are draggable or what the three lanes correspond to, and a pane whose
	// central gesture is undiscoverable is a pane most people will not use.
	strip := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Width: width, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Layout", TextRole: theme.RoleLabel},
		{
			Kind: ui.KindText, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
			Text: "Drag a widget to reorder it or to move it between lanes. " +
				"Drop one onto another to group them.",
		},
	}}
	if r != nil {
		if sel := barOutputSelector(h, r, width); sel != nil {
			strip.Children = append(strip.Children, sel)
		}
	}
	bar := h.barEditedBar()
	selected, hasSelection := barParseRef(h.barSelected)
	for _, name := range config.LaneNames() {
		strip.Children = append(strip.Children, barLane(h, bar, name, width))
		if h.barAdding == name {
			strip.Children = append(strip.Children, barAddList(h, name, width))
		}
		// The options sit directly under the lane that owns the selection
		// rather than after all three, so a widget in the Left lane does not
		// put its options below the Right one.
		if hasSelection && selected.Lane == name {
			if inspector := barInspector(h, width); inspector != nil {
				strip.Children = append(strip.Children, inspector)
			}
		}
	}
	return strip
}

// barKeyPress is D6: keyboard parity with the drag path. Alt and an arrow move
// the focused chip within its lane or between lanes; the group and dissolve
// commands sit on the chip itself.
//
// Every one of these calls the same config mutation the pointer path calls, so
// the two cannot drift and the tests need no synthesised pointer events.
func (h *PanelHost) barKeyPress(r *Registry, key uint32) bool {
	if !h.alt || h.section != "Bar" || h.query != "" {
		return false
	}
	ref, ok := h.barFocusedRef()
	if !ok {
		return false
	}
	switch key {
	case keyLeft:
		return h.barMoveWithinLane(r, ref, -1)
	case keyRight:
		return h.barMoveWithinLane(r, ref, 1)
	case keyUp:
		return h.barMoveAcrossLanes(r, ref, -1)
	case keyDown:
		return h.barMoveAcrossLanes(r, ref, 1)
	}
	return false
}

// barFocusedRef reads the address off whatever chip currently holds focus.
func (h *PanelHost) barFocusedRef() (config.ItemRef, bool) {
	n := h.focused()
	if n == nil {
		return config.ItemRef{}, false
	}
	addr, ok := strings.CutPrefix(n.Action, "bar-select:")
	if !ok {
		return config.ItemRef{}, false
	}
	return barParseRef(addr)
}

// barApply writes a mutated lane back into whichever bar is being edited, then
// persists and rebuilds. It is the one place the edited bar is written, so the
// shared-versus-override decision (D7) is made once.
func (h *PanelHost) barApply(r *Registry, laneName string, items []config.Item) bool {
	bar := h.barEditedBar()
	switch laneName {
	case "left":
		bar.Left = items
	case "center":
		bar.Center = items
	case "right":
		bar.Right = items
	default:
		return false
	}

	if h.barOutput == "" {
		h.draft.Bar = bar
	} else {
		found := false
		for i := range h.draft.Outputs {
			if h.draft.Outputs[i].Connector != h.barOutput {
				continue
			}
			// A lane is inherited whole or overridden whole, because applyBar
			// takes it whole (D7). Touching one widget therefore forks the
			// entire lane for this output, and the strip says so.
			h.draft.Outputs[i].Bar = barSetLane(h.draft.Outputs[i].Bar, laneName, items)
			found = true
		}
		if !found {
			h.draft.Outputs = append(h.draft.Outputs, config.OutputOverride{
				Connector: h.barOutput,
				Bar:       barSetLane(config.Bar{}, laneName, items),
			})
		}
	}
	h.persistDraft(r)
	r.rebuildPanel(h)
	return true
}

func barSetLane(bar config.Bar, name string, items []config.Item) config.Bar {
	switch name {
	case "left":
		bar.Left = items
	case "center":
		bar.Center = items
	case "right":
		bar.Right = items
	}
	return bar
}

// barMoveWithinLane shifts a top-level chip one place along its lane. A member
// inside a group moves within that group instead, which is the only movement
// that means anything for one.
func (h *PanelHost) barMoveWithinLane(r *Registry, ref config.ItemRef, delta int) bool {
	lane := barLaneItems(h.barEditedBar(), ref.Lane)
	if ref.Path.Member >= 0 {
		if ref.Path.Index >= len(lane) {
			return false
		}
		group := lane[ref.Path.Index]
		moved, err := config.MoveItem(group.Items, ref.Path.Member, ref.Path.Member+delta)
		if err != nil {
			return true
		}
		next := slices.Clone(lane)
		next[ref.Path.Index].Items = moved
		h.barSelected = barRefAction(config.ItemRef{
			Lane: ref.Lane,
			Path: config.ItemPath{Index: ref.Path.Index, Member: ref.Path.Member + delta},
		})
		return h.barApply(r, ref.Lane, next)
	}
	moved, err := config.MoveItem(lane, ref.Path.Index, ref.Path.Index+delta)
	if err != nil {
		// The end of a lane is not an error the user needs told about; the
		// chip simply does not move.
		return true
	}
	h.barSelected = barRefAction(config.ItemRef{
		Lane: ref.Lane, Path: config.ItemPath{Index: ref.Path.Index + delta, Member: -1},
	})
	return h.barApply(r, ref.Lane, moved)
}

// barMoveAcrossLanes lifts a chip out of one lane and appends it to the next,
// which is the only sensible reading of "up" and "down" when the lanes are
// stacked. A group member is lifted out of its group first.
func (h *PanelHost) barMoveAcrossLanes(r *Registry, ref config.ItemRef, delta int) bool {
	names := config.LaneNames()
	at := slices.Index(names, ref.Lane)
	if at < 0 || at+delta < 0 || at+delta >= len(names) {
		return true
	}
	target := names[at+delta]

	bar := h.barEditedBar()
	source := barLaneItems(bar, ref.Lane)
	it := bar.ItemAt(ref)
	if it == nil {
		return false
	}
	carried := *it

	trimmed, err := config.RemoveItem(source, ref.Path)
	if err != nil {
		return true
	}
	if !h.barApply(r, ref.Lane, trimmed) {
		return false
	}

	dest := barLaneItems(h.barEditedBar(), target)
	grown, err := config.InsertItem(dest, len(dest), carried)
	if err != nil {
		return true
	}
	h.barSelected = barRefAction(config.ItemRef{
		Lane: target, Path: config.ItemPath{Index: len(grown) - 1, Member: -1},
	})
	return h.barApply(r, target, grown)
}

// barActivate handles the lane editor's own actions. It sits beside the
// keyboard commands so both reach the same mutations.
func (h *PanelHost) barActivate(r *Registry, action string) bool {
	switch {
	case strings.HasPrefix(action, "bar-select:"):
		addr := strings.TrimPrefix(action, "bar-select:")
		if _, ok := barParseRef(addr); !ok {
			return false
		}
		h.barSelected = addr
		r.rebuildPanel(h)
		return true

	case strings.HasPrefix(action, "bar-inspect:"):
		addr := strings.TrimPrefix(action, "bar-inspect:")
		if _, ok := barParseRef(addr); !ok {
			return false
		}
		// Selecting is what opens the inspector: it is one surface beneath the
		// strip rather than a second mode, so there is nothing else to toggle.
		h.barSelected = addr
		r.rebuildPanel(h)
		return true

	case strings.HasPrefix(action, "bar-ungroup:"):
		ref, ok := barParseRef(strings.TrimPrefix(action, "bar-ungroup:"))
		if !ok {
			return false
		}
		lane := barLaneItems(h.barEditedBar(), ref.Lane)
		next, err := config.UngroupItem(lane, ref.Path.Index)
		if err != nil {
			return true
		}
		h.barSelected = ""
		return h.barApply(r, ref.Lane, next)

	case strings.HasPrefix(action, "bar-group:"):
		// Grouping from the keyboard folds the focused chip into the one after
		// it, which is the same result the equivalent drag produces.
		ref, ok := barParseRef(strings.TrimPrefix(action, "bar-group:"))
		if !ok || ref.Path.Member >= 0 {
			return false
		}
		lane := barLaneItems(h.barEditedBar(), ref.Lane)
		next, err := config.GroupItems(lane, ref.Path.Index+1, ref.Path.Index, config.NewMinter(h.draft))
		if err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return true
		}
		h.errLabel = ""
		h.barSelected = ""
		return h.barApply(r, ref.Lane, next)

	case strings.HasPrefix(action, "bar-remove:"):
		ref, ok := barParseRef(strings.TrimPrefix(action, "bar-remove:"))
		if !ok {
			return false
		}
		lane := barLaneItems(h.barEditedBar(), ref.Lane)
		next, err := config.RemoveItem(lane, ref.Path)
		if err != nil {
			return true
		}
		h.barSelected = ""
		return h.barApply(r, ref.Lane, next)

	case strings.HasPrefix(action, "bar-add:"):
		lane := strings.TrimPrefix(action, "bar-add:")
		if barLaneLabels[lane] == "" {
			return false
		}
		// The control toggles: opening the list and changing your mind has to
		// be possible without picking something.
		if h.barAdding == lane {
			h.barAdding = ""
		} else {
			h.barAdding = lane
		}
		r.rebuildPanel(h)
		return true

	case strings.HasPrefix(action, "bar-add-item:"):
		rest := strings.TrimPrefix(action, "bar-add-item:")
		laneName, id, ok := strings.Cut(rest, ":")
		if !ok || barLaneLabels[laneName] == "" {
			return false
		}
		lane := barLaneItems(h.barEditedBar(), laneName)
		next, err := config.InsertItem(lane, len(lane), config.Item{ID: id})
		if err != nil {
			return true
		}
		h.barAdding = ""
		return h.barApply(r, laneName, next)

	case strings.HasPrefix(action, "bar-output:"):
		h.barOutput = strings.TrimPrefix(action, "bar-output:")
		if h.barOutput == "shared" {
			h.barOutput = ""
		}
		h.barSelected = ""
		r.rebuildPanel(h)
		return true

	case strings.HasPrefix(action, "bar-reset-lane:"):
		// D7: a lane is inherited whole or overridden whole, so the reset
		// clears the whole lane override and lets the shared one through
		// again.
		name := strings.TrimPrefix(action, "bar-reset-lane:")
		if h.barOutput == "" || barLaneLabels[name] == "" {
			return false
		}
		for i := range h.draft.Outputs {
			if h.draft.Outputs[i].Connector == h.barOutput {
				h.draft.Outputs[i].Bar = barSetLane(h.draft.Outputs[i].Bar, name, nil)
			}
		}
		h.persistDraft(r)
		r.rebuildPanel(h)
		return true
	}
	return false
}

// barAddList is the add control's in-place list of the widget vocabulary. It
// expands where it stands, the way Menu does, because no popup-over-panel
// surface exists in this shell.
func barAddList(h *PanelHost, laneName string, width int) *ui.Node {
	m := h.metrics()
	col := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Width: width, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Add to " + barLaneLabels[laneName],
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}}
	for _, id := range config.WidgetIDs() {
		col.Children = append(col.Children, &ui.Node{
			Kind: ui.KindButton, Action: "bar-add-item:" + laneName + ":" + id,
			Name: settings.WidgetName(config.Item{ID: id}), Role: "button", Focusable: true,
			Width: width, Height: m.StandardControl, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: settings.WidgetName(config.Item{ID: id})}},
		})
	}
	return col
}

// barInspector is the option surface for the selected chip, built from entries
// synthesised at runtime over that one config.Item.
//
// This is the task sub-project A exists for. A's typed accessor entries are
// what make it expressible: the retired Get/Set switch pair could name a
// setting only by a compile-time path, never an arbitrary instance's fields.
// The rows follow the settings foundation's own anatomy unchanged -- label
// over caption, no cards -- because only the lane strip is excepted from that.
func barInspector(h *PanelHost, width int) *ui.Node {
	if h.barSelected == "" {
		return nil
	}
	ref, ok := barParseRef(h.barSelected)
	if !ok {
		return nil
	}
	bar := h.barEditedBar()
	it := bar.ItemAt(ref)
	if it == nil {
		// The chip was removed underneath the selection. Saying so is better
		// than drawing an empty panel that looks broken.
		return &ui.Node{Kind: ui.KindText, Text: "That widget is no longer on the bar.",
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
	}

	name := settings.WidgetName(*it)
	head := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: width, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
		{Kind: ui.KindRow, Gap: theme.MarginS, Children: barInspectorControls(h, ref, *it)},
	}}

	col := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Width: width, Children: []*ui.Node{head}}

	entries := settings.WidgetEntriesFor(h.draft, ref, *it)
	if len(entries) == 0 {
		col.Children = append(col.Children, &ui.Node{
			Kind: ui.KindText, Text: "This widget has no options.",
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
		return col
	}
	for _, e := range entries {
		col.Children = append(col.Children, settingsEntryRow(h, e))
	}
	return col
}

// barInspectorControls are the actions that belong to the selected chip rather
// than to one of its options: group it with its neighbour, and take it off the
// bar. Dissolve lives on the group itself.
func barInspectorControls(h *PanelHost, ref config.ItemRef, it config.Item) []*ui.Node {
	m := h.metrics()
	out := []*ui.Node{}
	if ref.Path.Member < 0 && it.ID != "group" {
		out = append(out, &ui.Node{
			Kind: ui.KindButton, Action: "bar-group:" + barRefAction(ref),
			Name: "Group with the next widget", Role: "button", Focusable: true,
			Height: m.StandardControl, Padding: m.ButtonPadding, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: "Group"}},
		})
	}
	out = append(out, &ui.Node{
		Kind: ui.KindButton, Action: "bar-remove:" + barRefAction(ref),
		Name: "Remove " + settings.WidgetName(it), Role: "button", Focusable: true,
		Width: m.StandardControl, Height: m.StandardControl, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: m.IconSmall}},
	})
	return out
}

// barOutputSelector is D7's per-output control. It reuses connectorsLocked, so
// it offers the outputs that actually have a bar.
func barOutputSelector(h *PanelHost, r *Registry, width int) *ui.Node {
	m := h.metrics()
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: width, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Editing", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}}
	option := func(value, label, tip string) *ui.Node {
		n := &ui.Node{
			Kind: ui.KindButton, Action: "bar-output:" + value,
			Name: label, Role: "tab", Focusable: true, Tooltip: tip,
			Height: m.StandardControl, Padding: m.ButtonPadding, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
		}
		if (value == "shared" && h.barOutput == "") || value == h.barOutput {
			n.State |= ui.StateSelected
			n.Fill = ui.FillAccent
		}
		return n
	}
	row.Children = append(row.Children,
		option("shared", "Shared", "The layout every display uses unless it overrides it"))
	for _, conn := range r.connectorsLocked() {
		row.Children = append(row.Children,
			option(conn, conn, "A layout just for "+conn))
	}
	return row
}

// barEditedBar is the bar the strip is editing: the shared one, or the
// override belonging to the selected output, with the lanes it does not carry
// filled in from the shared bar exactly as applyBar fills them at load.
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
// one, which is what applyBar does at load.
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
// rather than inheriting it.
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

// barDrop completes a pointer drag onto a lane. It is the thin caller
// resolveDrop exists for: the zone hands over its chips' bounds and the drop
// point, the pure function says whether that means join or insert, and the
// same config mutations the keyboard commands use do the work.
func (h *PanelHost) barDrop(r *Registry, zone *ui.Node, payload string, x, y int) bool {
	laneName, ok := strings.CutPrefix(zone.Action, "bar-lane:")
	if !ok || barLaneLabels[laneName] == "" {
		return false
	}
	from, ok := barParseRef(payload)
	if !ok {
		return false
	}

	// Only the top-level chips are drop targets within a lane. A group's
	// members are drawn inside it and reached by dragging the group, which
	// keeps the one-level cap a property of what can be dropped rather than a
	// refusal after the fact.
	lane := barLaneItems(h.barEditedBar(), laneName)
	bounds, targets := h.barDropRows(zone, laneName)
	outcome := resolveDrop(bounds, x, y)
	h.barDropHint = ""

	// A chip dragged out of a group leaves it first, which is what prunes an
	// emptied group and its placement.
	if from.Lane != laneName || from.Path.Member >= 0 {
		return h.barMoveBetween(r, from, laneName, outcome, targets)
	}
	if outcome.Join >= 0 && outcome.Join < len(targets) {
		dst := targets[outcome.Join]
		if dst == from.Path.Index {
			return true
		}
		var next []config.Item
		var err error
		if lane[dst].ID == "group" {
			// Dropping onto a group means joining it, not wrapping it in
			// another one.
			carried := lane[from.Path.Index]
			next, err = config.AddToGroup(lane, dst, carried, config.NewMinter(h.draft))
			if err == nil {
				next, err = config.RemoveItem(next, config.ItemPath{Index: from.Path.Index, Member: -1})
			}
		} else {
			next, err = config.GroupItems(lane, from.Path.Index, dst, config.NewMinter(h.draft))
		}
		if err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return true
		}
		h.errLabel = ""
		return h.barApply(r, laneName, next)
	}
	to := outcome.Insert
	if to > from.Path.Index {
		to--
	}
	moved, err := config.MoveItem(lane, from.Path.Index, to)
	if err != nil {
		return true
	}
	return h.barApply(r, laneName, moved)
}

// barMoveBetween carries a chip from one lane, or out of a group, into the
// lane it was dropped on.
func (h *PanelHost) barMoveBetween(r *Registry, from config.ItemRef, laneName string, outcome dropOutcome, targets []int) bool {
	bar := h.barEditedBar()
	it := bar.ItemAt(from)
	if it == nil {
		return false
	}
	carried := *it

	source, err := config.RemoveItem(barLaneItems(bar, from.Lane), from.Path)
	if err != nil {
		return true
	}
	if !h.barApply(r, from.Lane, source) {
		return false
	}

	dest := barLaneItems(h.barEditedBar(), laneName)
	if outcome.Join >= 0 && outcome.Join < len(targets) {
		dst := targets[outcome.Join]
		if dst < len(dest) && dest[dst].ID == "group" {
			grown, err := config.AddToGroup(dest, dst, carried, config.NewMinter(h.draft))
			if err != nil {
				h.errLabel = err.Error()
				r.rebuildPanel(h)
				return true
			}
			h.errLabel = ""
			h.barSelected = ""
			return h.barApply(r, laneName, grown)
		}
	}
	at := len(dest)
	if outcome.Join < 0 && outcome.Insert <= len(dest) {
		at = outcome.Insert
	}
	if outcome.Join >= 0 && outcome.Join < len(targets) {
		at = targets[outcome.Join]
	}
	if at > len(dest) {
		at = len(dest)
	}
	grown, err := config.InsertItem(dest, at, carried)
	if err != nil {
		return true
	}
	h.barSelected = barRefAction(config.ItemRef{
		Lane: laneName, Path: config.ItemPath{Index: at, Member: -1},
	})
	return h.barApply(r, laneName, grown)
}

// walkLaneNodes walks a subtree. The lane's own tree is small, so a plain walk
// is cheaper than keeping a parallel index of chip bounds.
func walkLaneNodes(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walkLaneNodes(c, fn)
	}
}

// barDragHover resolves what the pointer is currently over and marks it, so a
// drag shows where it would land. It reports whether anything changed: the
// caller repaints only then, because the paint path reads no drag state and a
// repaint per motion event otherwise redraws the surface identically.
func (h *PanelHost) barDragHover() bool {
	zone := ui.FindDropZone(h.root, &h.drag)
	hint := ""
	if zone != nil && h.drag.Accepts(zone) {
		if laneName, ok := strings.CutPrefix(zone.Action, "bar-lane:"); ok {
			rows, targets := h.barDropRows(zone, laneName)
			outcome := resolveDrop(rows, int(h.drag.X), int(h.drag.Y))
			if outcome.Join >= 0 && outcome.Join < len(targets) {
				hint = barRefAction(config.ItemRef{
					Lane: laneName,
					Path: config.ItemPath{Index: targets[outcome.Join], Member: -1},
				})
			}
		}
	}
	if hint == h.barDropHint {
		return false
	}
	h.barDropHint = hint
	h.applyBarDropHint()
	return true
}

// applyBarDropHint marks the hinted row in the tree that is already built.
// A drag does not rebuild the tree, so the mark is applied in place.
func (h *PanelHost) applyBarDropHint() {
	join := ""
	body := ""
	if h.barDropHint != "" {
		join = "bar-select:" + h.barDropHint
		body = "bar-groupbody:" + h.barDropHint
	}
	walkLaneNodes(h.root, func(n *ui.Node) {
		switch {
		case strings.HasPrefix(n.Action, "bar-select:"), strings.HasPrefix(n.Action, "bar-groupbody:"):
			if n.Action == join || n.Action == body {
				n.State |= ui.StateHovered
				return
			}
			n.State &^= ui.StateHovered
		}
	})
}

// barDropRows lists the lane's top-level rows and which item each one is. A
// group contributes its whole body, so dropping anywhere on it joins it.
func (h *PanelHost) barDropRows(zone *ui.Node, laneName string) ([]ui.Rect, []int) {
	var rows []ui.Rect
	var targets []int
	for i, it := range barLaneItems(h.barEditedBar(), laneName) {
		ref := config.ItemRef{Lane: laneName, Path: config.ItemPath{Index: i, Member: -1}}
		want := "bar-select:" + barRefAction(ref)
		if it.ID == "group" {
			want = "bar-groupbody:" + barRefAction(ref)
		}
		walkLaneNodes(zone, func(n *ui.Node) {
			if n.Action == want && n.Bounds.H > 0 {
				rows = append(rows, n.Bounds)
				targets = append(targets, i)
			}
		})
	}
	return rows, targets
}

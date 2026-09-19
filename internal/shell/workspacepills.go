package shell

import (
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// workspacePillGap separates adjacent workspace pills, and matches the
// measured gap in the reference bar.
const workspacePillGap = 8

// workspaceActionPrefix names a pill's switch action. The suffix is Niri's own
// workspace id, which survives the reordering an index does not.
const workspaceActionPrefix = "workspace:"

// workspaceAction is the action one pill carries.
func workspaceAction(id uint64) string {
	return workspaceActionPrefix + strconv.FormatUint(id, 10)
}

// workspaceID parses a pill action back to its workspace id. It reports false
// for any other action, including a malformed suffix.
func workspaceID(action string) (uint64, bool) {
	rest, ok := strings.CutPrefix(action, workspaceActionPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// pillNode builds one pill from its state and the theme's density roles.
//
// The row is one shape family: every pill carries an explicit stadium, which
// overrides the bar's inherited capsule radius, so a pill as wide as it is
// tall is a circle and the focused one is a true pill rather than a rounded
// rectangle. Focus dominates the fill: a focused workspace keeps the accent
// even when it is urgent. Urgency outranks occupancy, and an empty workspace
// stays present as an unfilled hit target. A focused pill is twice as wide as
// the dot the others share, the page-dot proportion, so focus reads as the
// dominant state without leaving the family.
//
// The pill paints no label, so its identity lives here: the action carries the
// workspace id a click focuses, and the accessible name reads the workspace's
// own name, or its index when it has none.
func pillNode(p workspacePill, m theme.Metrics) *ui.Node {
	fill := ui.FillNone
	switch {
	case p.Focused:
		fill = ui.FillAccent
	case p.Urgent:
		fill = ui.FillError
	case p.Occupied:
		fill = ui.FillContainer
	}
	width := m.IconLarge
	if p.Focused {
		width = 2 * m.IconLarge
	}
	label := p.Name
	if label == "" {
		label = strconv.Itoa(p.Index)
	}
	return &ui.Node{
		Kind:   ui.KindCapsule,
		Fill:   fill,
		Shape:  ui.ShapeStadium,
		Width:  width,
		Height: m.IconLarge,
		Action: workspaceAction(p.ID),
		Name:   "Workspace " + label,
		Role:   "button",
	}
}

// refreshWorkspacePills rebuilds the pill row when the workspace set, its
// occupancy, urgency or focus changes, and reports whether it did. The pills
// are shapes only: the row never paints a workspace number, so the projection
// cannot hide a label inside a smaller node.
func refreshWorkspacePills(row *ui.Node, v barView, m theme.Metrics) bool {
	// With no projection yet, the widget still shows the stable fallback
	// rather than collapsing to nothing, which is what tells an owner that
	// Niri has not reported this output.
	if len(v.Pills) == 0 {
		label := v.Workspace
		if label == "" {
			label = noWorkspace
		}
		if len(row.Children) == 1 && row.Children[0] != nil &&
			len(row.Children[0].Children) == 1 &&
			row.Children[0].Children[0].Text == label {
			return false
		}
		row.Children = append(row.Children[:0], &ui.Node{
			Kind: ui.KindCapsule, Fill: ui.FillContainer, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
		})
		return true
	}
	if workspacePillsMatch(row, v.Pills, m) {
		return false
	}
	row.Children = row.Children[:0]
	for _, p := range v.Pills {
		row.Children = append(row.Children, pillNode(p, m))
	}
	return true
}

// workspacePillsMatch reports whether the row already paints these pills. It
// compares against a freshly built pill rather than repeating the state rules,
// so the painter and the change detector cannot drift apart.
func workspacePillsMatch(row *ui.Node, pills []workspacePill, m theme.Metrics) bool {
	if len(row.Children) != len(pills) {
		return false
	}
	for i, p := range pills {
		c := row.Children[i]
		if c == nil || len(c.Children) != 0 {
			return false
		}
		want := pillNode(p, m)
		if c.Kind != want.Kind || c.Fill != want.Fill || c.Shape != want.Shape ||
			c.Width != want.Width || c.Height != want.Height ||
			c.Action != want.Action || c.Name != want.Name || c.Role != want.Role {
			return false
		}
	}
	return true
}

// handleWorkspacePillClick focuses the workspace a pill stands for. The id
// comes from the action rather than from the projection, so a click acts on
// the workspace the owner saw even if a snapshot lands between the press and
// the release. Niri rejects an id that has since gone, which is the only
// check this needs.
//
// Only the primary button switches. Scroll cycling is a follow-up, and the
// other buttons carry no workspace gesture yet.
func (r *Registry) handleWorkspacePillClick(id uint64, button uint32) bool {
	if button != 0 && button != buttonLeft {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendNiriLocked(niri.FocusWorkspace{ID: id})
	return true
}

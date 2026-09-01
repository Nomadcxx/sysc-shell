package shell

import tray "github.com/Nomadcxx/sysc-tray/protocol"

const (
	// menuMaxDepth bounds nesting: deeper service trees are truncated, not
	// recursed, so a hostile menu cannot exhaust the stack.
	menuMaxDepth = 8
	// menuMaxRows bounds one visible list. A service may serve thousands of
	// siblings; the shell shows the first 512 and drops the rest.
	menuMaxRows = 512
)

// trayMenuRow is one visible row: its accessible name and role plus the
// state the renderer needs. Separators and disabled rows render but never
// take focus.
type trayMenuRow struct {
	id       int32
	name     string
	role     string // menuitem, checkmenuitem, radiomenuitem, separator
	enabled  bool
	checked  bool
	submenu  bool
	iconName string
}

// menuLevel is one pushed list: the parent's focus returns on Escape.
type menuLevel struct {
	nodes []tray.MenuNode
	focus int
}

// trayMenu shows one list at a time. Entering a submenu pushes the parent
// level; Escape pops before anything closes. No recursive popup surfaces.
type trayMenu struct {
	revision uint32
	stack    []menuLevel
	valid    bool
}

// newTrayMenu validates the imported tree iteratively and installs the root
// level. An invalid tree — duplicate sibling IDs, no usable rows — yields an
// empty model rather than a guessed rendering.
func newTrayMenu(menu tray.Menu) *trayMenu {
	m := &trayMenu{revision: menu.Revision}
	m.valid = validMenuLevel(menu.Root.Children, 1)
	m.stack = []menuLevel{{nodes: menu.Root.Children, focus: -1}}
	m.stack[0].focus = m.firstFocusable(0)
	return m
}

// validMenuLevel walks the tree iteratively, checking sibling IDs for
// duplicates up to the depth bound.
func validMenuLevel(nodes []tray.MenuNode, depth int) bool {
	type frame struct {
		nodes []tray.MenuNode
		depth int
	}
	work := []frame{{nodes, depth}}
	for len(work) > 0 {
		f := work[len(work)-1]
		work = work[:len(work)-1]
		if f.depth > menuMaxDepth {
			continue
		}
		seen := make(map[int32]bool, len(f.nodes))
		for _, n := range f.nodes {
			if seen[n.ID] {
				return false
			}
			seen[n.ID] = true
			if len(n.Children) > 0 {
				work = append(work, frame{n.Children, f.depth + 1})
			}
		}
	}
	return true
}

func (m *trayMenu) top() *menuLevel { return &m.stack[len(m.stack)-1] }

// len counts the visible rows of the current level, capped.
func (m *trayMenu) len() int {
	return len(m.visible())
}

func (m *trayMenu) visible() []tray.MenuNode {
	if !m.valid || len(m.stack) == 0 {
		return nil
	}
	nodes := m.top().nodes
	visible := make([]tray.MenuNode, 0, len(nodes))
	for _, n := range nodes {
		if !n.Visible {
			continue
		}
		visible = append(visible, n)
		if len(visible) >= menuMaxRows {
			break
		}
	}
	return visible
}

// row projects one visible node for the renderer.
func (m *trayMenu) row(i int) trayMenuRow {
	n := m.visible()[i]
	row := trayMenuRow{
		id:       n.ID,
		name:     n.Label,
		role:     "menuitem",
		enabled:  n.Enabled,
		submenu:  n.ChildrenDisplay == "submenu" && len(n.Children) > 0,
		iconName: n.IconName,
	}
	if n.Separator {
		row.role, row.enabled = "separator", false
		return row
	}
	switch n.ToggleType {
	case tray.ToggleCheckmark:
		row.role = "checkmenuitem"
		row.checked = n.ToggleState == 1
	case tray.ToggleRadio:
		row.role = "radiomenuitem"
		row.checked = n.ToggleState == 1
	}
	return row
}

// focusable reports whether a visible row can take focus.
func focusableRow(n tray.MenuNode) bool {
	return n.Visible && n.Enabled && !n.Separator
}

func (m *trayMenu) firstFocusable(from int) int {
	nodes := m.visible()
	for i := from; i < len(nodes); i++ {
		if focusableRow(nodes[i]) {
			return i
		}
	}
	return -1
}

// move walks focus through focusable visible rows, wrapping at both ends.
func (m *trayMenu) move(delta int) {
	nodes := m.visible()
	if len(nodes) == 0 {
		return
	}
	focus := m.top().focus
	for i := 0; i < len(nodes); i++ {
		focus = (focus + delta + len(nodes)) % len(nodes)
		if focusableRow(nodes[focus]) {
			m.top().focus = focus
			return
		}
	}
}

// focusedID is the focused row's protocol ID, or -1 when nothing is focused.
func (m *trayMenu) focusedID() int32 {
	nodes := m.visible()
	f := m.top().focus
	if f < 0 || f >= len(nodes) || !focusableRow(nodes[f]) {
		return -1
	}
	return nodes[f].ID
}

// activateFocused returns the focused row's ID for a menu.select command.
func (m *trayMenu) activateFocused() (int32, bool) {
	id := m.focusedID()
	return id, id >= 0
}

// push enters the focused row's submenu, saving the parent's focus for back.
// The name argument is unused — the focused row names itself — and exists so
// tests can drive the same call shape a pointer path uses.
func (m *trayMenu) push(_ string) bool {
	if len(m.stack) >= menuMaxDepth {
		return false
	}
	nodes := m.visible()
	f := m.top().focus
	if f < 0 || f >= len(nodes) {
		return false
	}
	n := nodes[f]
	if n.ChildrenDisplay != "submenu" || len(n.Children) == 0 ||
		!validMenuLevel(n.Children, len(m.stack)+1) {
		return false
	}
	m.stack = append(m.stack, menuLevel{nodes: n.Children, focus: -1})
	m.top().focus = m.firstFocusable(0)
	return true
}

// back pops one level and restores the parent's focus. False at the root:
// the caller closes the menu instead.
func (m *trayMenu) back() bool {
	if len(m.stack) <= 1 {
		return false
	}
	m.stack = m.stack[:len(m.stack)-1]
	return true
}

// currentLabel is the focused row's label — the tests use it to confirm the
// visible list changed after push and back.
func (m *trayMenu) currentLabel() string {
	nodes := m.visible()
	f := m.top().focus
	if f < 0 || f >= len(nodes) {
		return ""
	}
	return nodes[f].Label
}

package shell

import (
	"fmt"
	"testing"

	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// menu builds a flat menu of n children under the root.
func flatMenu(n int) tray.Menu {
	kids := make([]tray.MenuNode, n)
	for i := range kids {
		kids[i] = tray.MenuNode{ID: int32(i + 1), Label: fmt.Sprintf("Item %d", i+1),
			Enabled: true, Visible: true}
	}
	return tray.Menu{Revision: 1, Root: tray.MenuNode{ID: 0, Visible: true, Children: kids}}
}

// The model caps nesting at 8: deeper trees are truncated, not recursed.
func TestTrayMenuDepthIsBounded(t *testing.T) {
	menu := tray.Menu{Revision: 1, Root: tray.MenuNode{ID: 0, Visible: true}}
	cur := &menu.Root
	for i := 0; i < 40; i++ {
		cur.Children = []tray.MenuNode{{
			ID: int32(i + 1), Label: "level", Enabled: true, Visible: true,
			ChildrenDisplay: "submenu",
		}}
		cur = &cur.Children[0]
	}
	m := newTrayMenu(menu)
	depth := 0
	for m.push("") {
		depth++
		if depth > 20 {
			break
		}
	}
	if depth > 8 {
		t.Fatalf("descended to depth %d, want the bound at 8", depth)
	}
}

// The model caps siblings at 512 per level: a pathological tree cannot
// allocate unbounded rows.
func TestTrayMenuSiblingsAreBounded(t *testing.T) {
	m := newTrayMenu(flatMenu(5000))
	if n := m.len(); n > 512 {
		t.Fatalf("rows = %d, want the 512 cap", n)
	}
}

// Duplicate sibling IDs are invalid: activation would be ambiguous. The
// model rejects the tree rather than guessing.
func TestTrayMenuRejectsDuplicateIDs(t *testing.T) {
	menu := flatMenu(2)
	menu.Root.Children[1].ID = menu.Root.Children[0].ID
	m := newTrayMenu(menu)
	if m.len() != 0 {
		t.Fatal("a menu with duplicate IDs was accepted")
	}
}

// A malformed node — invisible and labelless — renders nothing.
func TestTrayMenuSkipsInvisibleNodes(t *testing.T) {
	menu := flatMenu(3)
	menu.Root.Children[1].Visible = false
	m := newTrayMenu(menu)
	if m.len() != 2 {
		t.Fatalf("rows = %d, want the invisible node skipped", m.len())
	}
}

// The first focusable row takes initial focus; disabled and separator rows
// never receive it.
func TestTrayMenuInitialFocusSkipsDisabledAndSeparators(t *testing.T) {
	menu := flatMenu(4)
	menu.Root.Children[0].Separator = true
	menu.Root.Children[1].Enabled = false
	m := newTrayMenu(menu)
	if got := m.focusedID(); got != 3 {
		t.Fatalf("initial focus = %d, want the first focusable row 3", got)
	}
}

// Arrow keys walk focus through focusable rows and wrap.
func TestTrayMenuKeyboardTraversal(t *testing.T) {
	m := newTrayMenu(flatMenu(3)) // focus starts on row 1
	m.move(1)
	if got := m.focusedID(); got != 2 {
		t.Fatalf("focus after down = %d", got)
	}
	m.move(1)
	m.move(1) // wraps to the first row
	if got := m.focusedID(); got != 1 {
		t.Fatalf("focus after wrap = %d", got)
	}
	m.move(-1) // wraps back
	if got := m.focusedID(); got != 3 {
		t.Fatalf("focus after up-wrap = %d", got)
	}
}

// Activation reports the focused row's ID. A disabled row cannot activate.
func TestTrayMenuActivation(t *testing.T) {
	menu := flatMenu(2)
	menu.Root.Children[1].Enabled = false
	m := newTrayMenu(menu)
	id, ok := m.activateFocused()
	if !ok || id != 1 {
		t.Fatalf("activate = (%d, %v)", id, ok)
	}
	m.move(1) // onto the disabled row — actually focus skips it, wrapping to row 1
	m.move(1)
	if _, ok := m.activateFocused(); !ok {
		t.Fatal("wrap should land back on the enabled row")
	}
}

// Separators are rendered as rows but are neither focusable nor activatable.
func TestTrayMenuSeparatorsAreInert(t *testing.T) {
	menu := flatMenu(2)
	menu.Root.Children = append([]tray.MenuNode{
		{ID: 90, Separator: true, Visible: true},
	}, menu.Root.Children...)
	m := newTrayMenu(menu)
	if m.len() != 3 {
		t.Fatalf("rows = %d, want the separator kept as a row", m.len())
	}
	if got := m.focusedID(); got != 1 {
		t.Fatalf("focus = %d, want the separator skipped", got)
	}
}

// Checked and radio entries expose their toggle state to the row renderer.
func TestTrayMenuToggleState(t *testing.T) {
	menu := flatMenu(2)
	menu.Root.Children[0].ToggleType = tray.ToggleCheckmark
	menu.Root.Children[0].ToggleState = 1
	menu.Root.Children[1].ToggleType = tray.ToggleRadio
	menu.Root.Children[1].ToggleState = 0
	m := newTrayMenu(menu)
	row := m.row(0)
	if !row.checked || row.role != "checkmenuitem" {
		t.Fatalf("row 0 = %+v", row)
	}
	row = m.row(1)
	if row.checked || row.role != "radiomenuitem" {
		t.Fatalf("row 1 = %+v", row)
	}
}

// A submenu replaces the visible list and pushes the parent state; Escape
// pops back before anything closes.
func TestTrayMenuSubmenuPushAndBack(t *testing.T) {
	menu := flatMenu(2)
	menu.Root.Children[0].ChildrenDisplay = "submenu"
	menu.Root.Children[0].Children = []tray.MenuNode{
		{ID: 50, Label: "Child", Enabled: true, Visible: true},
	}
	m := newTrayMenu(menu)
	if !m.push("") { // enter the focused submenu
		t.Fatal("a submenu row did not push")
	}
	if m.len() != 1 || m.currentLabel() != "Child" {
		t.Fatalf("submenu rows = %d, label %q", m.len(), m.currentLabel())
	}
	if !m.back() {
		t.Fatal("back did not pop the submenu")
	}
	if m.len() != 2 {
		t.Fatalf("rows after back = %d, want the parent restored", m.len())
	}
	if m.back() {
		t.Fatal("back at the root should report nothing left to pop")
	}
}

// Every row exposes an accessible name and role.
func TestTrayMenuAccessibleNamesAndRoles(t *testing.T) {
	menu := flatMenu(1)
	menu.Root.Children[0].Label = "Quit"
	m := newTrayMenu(menu)
	row := m.row(0)
	if row.name != "Quit" || row.role != "menuitem" {
		t.Fatalf("row = %+v", row)
	}
	menu2 := flatMenu(1)
	menu2.Root.Children[0].Separator = true
	m2 := newTrayMenu(menu2)
	if m2.row(0).role != "separator" {
		t.Fatalf("separator role = %q", m2.row(0).role)
	}
}

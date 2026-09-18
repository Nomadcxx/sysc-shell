package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMenuOpensOnActivateAndSelectsOnEnter(t *testing.T) {
	t.Parallel()
	m := NewMenu([]string{"dark", "light"}, 0)
	m.Open()
	if !m.Opened() {
		t.Fatal("Open must mark the menu open")
	}
	m.Next()
	m.Next() // wraps to 0
	if got := m.Select(); got != 0 {
		t.Fatalf("wrap selection = %d, want 0", got)
	}
	if m.Opened() {
		t.Fatal("Select must close")
	}
}

func TestMenuEscapeReturnsToField(t *testing.T) {
	t.Parallel()
	m := NewMenu([]string{"dark", "light"}, 0)
	m.Open()
	m.Next()
	m.Cancel()
	if m.Opened() {
		t.Fatal("Escape while open must close")
	}
	if m.Index() != 0 {
		t.Fatalf("Escape must keep the committed value, got %d", m.Index())
	}
}

// sysc-330. D8 calls for a searchable picker; A shipped the enumerating half
// only, so several hundred font families arrived as a wall. A picker is a
// menu over a list too long to read whole: it carries a filter, and it may
// draw labels that differ from the values it writes.
func TestAPlainMenuCarriesNoFilter(t *testing.T) {
	t.Parallel()
	m := NewMenu([]string{"dark", "light"}, 0)
	m.Open()
	if m.Filtering() {
		t.Error("a two-option menu carries a filter; it is readable whole")
	}
	if n := m.Node(); len(n.Children) != 2 {
		t.Errorf("drew %d children, want one per option", len(n.Children))
	}
}

func TestAPickerDrawsAFilterFieldFirst(t *testing.T) {
	t.Parallel()
	m := NewPicker([]string{"alpha", "beta"}, nil, 0)
	m.Open()
	if !m.Filtering() {
		t.Fatal("a picker has no filter")
	}
	n := m.Node()
	if len(n.Children) != 3 {
		t.Fatalf("drew %d children, want the field and both options", len(n.Children))
	}
	if got := n.Children[0].Kind; got != ui.KindTextField {
		t.Errorf("first child kind = %v, want a text field", got)
	}
	// "Search" is what the shared chrome keys the magnifier and the clear
	// glyph on, so the well draws like every other search well in the shell.
	if got := n.Children[0].Name; got != "Search" {
		t.Errorf("filter field name = %q, want Search", got)
	}
}

// The filter narrows what is drawn, and the indices behind it stay indices
// into the full option list, or Select commits the wrong value.
func TestFilteringNarrowsTheListAndSelectsTheRealOption(t *testing.T) {
	t.Parallel()
	m := NewPicker([]string{"DejaVu Sans", "Inter Variable", "JetBrains Mono", "Noto Serif"}, nil, 0)
	m.Open()
	m.Edit(func(f *ui.Field) { f.Insert("mono") })

	n := m.Node()
	if len(n.Children) != 2 {
		t.Fatalf("filtered menu drew %d children, want the field and one match", len(n.Children))
	}
	if got := n.Children[1].Text; got != "JetBrains Mono" {
		t.Errorf("match = %q, want JetBrains Mono; the filter is case-insensitive", got)
	}
	if got := m.Select(); got != 2 {
		t.Errorf("Select returned %d, want 2: the index must address the full list", got)
	}
	if got := m.Value(); got != "JetBrains Mono" {
		t.Errorf("Value = %q, want JetBrains Mono", got)
	}
}

// A picker may write something other than what it draws: the font entries
// offer a "Default" row whose value is the empty string.
func TestAPickerWritesItsValueNotItsLabel(t *testing.T) {
	t.Parallel()
	m := NewPicker([]string{"Default", "Inter Variable"}, []string{"", "Inter Variable"}, 1)
	m.Open()
	m.Prev()
	m.Select()
	if got := m.Value(); got != "" {
		t.Errorf("Value = %q, want the empty string the Default row writes", got)
	}
}

func TestCursorMotionWalksOnlyTheMatches(t *testing.T) {
	t.Parallel()
	typed := func() *Menu {
		m := NewPicker([]string{"alpha", "beta", "gamma beta", "delta"}, nil, 0)
		m.Open()
		m.Edit(func(f *ui.Field) { f.Insert("beta") })
		return m
	}
	// The matches are 1 and 2, and the cursor rests on the first of them.
	if got := typed().Select(); got != 1 {
		t.Errorf("first match selection = %d, want 1", got)
	}
	m := typed()
	m.Next()
	if got := m.Select(); got != 2 {
		t.Errorf("after Next, selection = %d, want 2", got)
	}
	m = typed()
	m.Next()
	m.Next() // wraps within the matches rather than onto a hidden row
	if got := m.Select(); got != 1 {
		t.Errorf("after wrapping, selection = %d, want 1", got)
	}
}

// A filter that matches nothing must not commit something the user cannot
// see.
func TestAFilterWithNoMatchCommitsNothing(t *testing.T) {
	t.Parallel()
	m := NewPicker([]string{"alpha", "beta"}, nil, 1)
	m.Open()
	m.Edit(func(f *ui.Field) { f.Insert("zzz") })
	if n := m.Node(); len(n.Children) != 1 {
		t.Errorf("drew %d children, want the field alone", len(n.Children))
	}
	if got := m.Select(); got != 1 {
		t.Errorf("Select with no match = %d, want the committed 1", got)
	}
}

// Opening again starts from a clean filter, or the last search is still
// narrowing the list.
func TestOpeningClearsTheFilter(t *testing.T) {
	t.Parallel()
	m := NewPicker([]string{"alpha", "beta", "gamma"}, nil, 0)
	m.Open()
	m.Edit(func(f *ui.Field) { f.Insert("beta") })
	m.Cancel()
	m.Open()
	if n := m.Node(); len(n.Children) != 4 {
		t.Errorf("reopened menu drew %d children, want the field and all three options", len(n.Children))
	}
}

// PickAt maps a press to an option. With a filter in the first child slot a
// press must not land one row off, and a press on the well itself must not
// choose anything.
func TestPickAtAccountsForTheFilterField(t *testing.T) {
	t.Parallel()
	place := func(m *Menu) *ui.Node {
		n := m.Node()
		for i, c := range n.Children {
			c.Bounds = ui.Rect{X: 0, Y: i * 20, W: 100, H: 20}
		}
		return n
	}
	m := NewPicker([]string{"alpha", "beta", "gamma"}, nil, 2)
	m.Open()
	if !m.PickAt(place(m), 10, 30) {
		t.Error("a press on the first option reported no option")
	}
	if got := m.Select(); got != 0 {
		t.Errorf("press on the first option selected %d, want 0", got)
	}
	m.Open()
	if m.PickAt(place(m), 10, 10) {
		t.Error("a press on the filter well reported an option, so typing would commit a value")
	}
}

// A press must resolve against the filtered rows, not the full list.
func TestPickAtResolvesAgainstTheFilteredRows(t *testing.T) {
	t.Parallel()
	m := NewPicker([]string{"alpha", "beta", "gamma beta"}, nil, 0)
	m.Open()
	m.Edit(func(f *ui.Field) { f.Insert("beta") })
	n := m.Node()
	for i, c := range n.Children {
		c.Bounds = ui.Rect{X: 0, Y: i * 20, W: 100, H: 20}
	}
	// Child 1 is the first match, which is option 1; child 2 is option 2.
	if !m.PickAt(n, 10, 50) {
		t.Fatal("a press on the second match reported no option")
	}
	if got := m.Select(); got != 2 {
		t.Errorf("press on the second match selected %d, want 2", got)
	}
}

// Space is a character in a family name. A picker types it; a plain menu
// keeps using it to accept.
func TestSpaceTypesIntoAPickerAndAcceptsWithoutOne(t *testing.T) {
	t.Parallel()
	plain := NewMenu([]string{"dark", "light"}, 0)
	plain.Open()
	if !plain.Handle(keySpace) {
		t.Error("space must still accept in a menu with no filter")
	}
	if plain.Opened() {
		t.Error("space did not accept")
	}

	m := NewPicker([]string{"DejaVu Sans", "Noto Serif"}, nil, 0)
	m.Open()
	if m.Handle(keySpace) {
		t.Error("a picker consumed space as accept; it belongs in the family name")
	}
	if !m.Opened() {
		t.Error("space closed the picker")
	}
}

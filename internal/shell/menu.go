package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Menu is an in-panel dropdown. It is not a Wayland surface: the open list
// is a child region of the already-mapped panel.
//
// A picker is the same control over a list too long to read whole: it draws a
// filter well above its options and narrows them as they are typed. index and
// cursor stay addresses in the full option list however narrow the drawn list
// becomes, so a selection made through a filter commits the option the user
// pointed at rather than the row it happened to occupy.
type Menu struct {
	options []string
	// values are what each option writes, when that differs from the label
	// it draws. The font entries offer a "Default" row whose value is empty.
	values []string
	index  int
	cursor int
	open   bool
	filter *ui.Field
	// filterHeight is the room the well takes. The surface sets it from the
	// density ladder: measured from its own text, a well is exactly one line
	// tall and reads as a rule rather than a control.
	filterHeight int
	matches      []int
}

func NewMenu(options []string, index int) *Menu {
	return newMenu(options, nil, index, false)
}

// NewPicker builds a menu that filters. values may be nil, in which case each
// option writes itself.
func NewPicker(options, values []string, index int) *Menu {
	return newMenu(options, values, index, true)
}

func newMenu(options, values []string, index int, filtered bool) *Menu {
	if len(options) == 0 {
		index = 0
	} else if index < 0 || index >= len(options) {
		index = 0
	}
	m := &Menu{options: options, values: values, index: index, cursor: index}
	if filtered {
		m.filter = ui.NewField("")
	}
	return m
}

func (m *Menu) Open() {
	if m == nil {
		return
	}
	m.open = true
	m.cursor = m.index
	if m.filter != nil {
		// Reopening starts from a clean well, or the last search is still
		// narrowing a list the user has come back to read whole.
		m.filter.Clear()
		m.refilter()
	}
}

func (m *Menu) Opened() bool { return m != nil && m.open }

// Filtering reports whether this menu is a picker. The panel asks before
// routing text at it, and before treating space as the accept key.
func (m *Menu) Filtering() bool { return m != nil && m.filter != nil }

// Edit applies one edit to the filter well and re-narrows the list. The menu
// owns the field so the drawn rows can never disagree with the text above
// them.
func (m *Menu) Edit(fn func(*ui.Field)) bool {
	if m == nil || m.filter == nil || !m.open || fn == nil {
		return false
	}
	fn(m.filter)
	m.refilter()
	return true
}

// refilter recomputes which options survive the well's text. A cursor left on
// a row the list is no longer drawing moves to the first row it is.
func (m *Menu) refilter() {
	if m.filter == nil {
		m.matches = nil
		return
	}
	q := strings.ToLower(strings.TrimSpace(m.filter.Text))
	m.matches = m.matches[:0]
	for i, o := range m.options {
		if q == "" || strings.Contains(strings.ToLower(o), q) {
			m.matches = append(m.matches, i)
		}
	}
	if len(m.matches) == 0 || m.drawn(m.cursor) {
		return
	}
	m.cursor = m.matches[0]
}

// drawn reports whether the list is currently showing this option.
func (m *Menu) drawn(i int) bool {
	if m.filter == nil {
		return i >= 0 && i < len(m.options)
	}
	for _, at := range m.matches {
		if at == i {
			return true
		}
	}
	return false
}

func (m *Menu) Index() int {
	if m == nil {
		return 0
	}
	return m.index
}

func (m *Menu) Next() { m.step(1) }

func (m *Menu) Prev() { m.step(-1) }

// step moves the cursor one drawn row. A picker walks its matches: stepping
// through the full list would rest the cursor on rows the filter is hiding.
func (m *Menu) step(by int) {
	if m == nil || !m.open || len(m.options) == 0 {
		return
	}
	if m.filter == nil {
		m.cursor = (m.cursor + by + len(m.options)) % len(m.options)
		return
	}
	if len(m.matches) == 0 {
		return
	}
	at := 0
	for i, idx := range m.matches {
		if idx == m.cursor {
			at = i
			break
		}
	}
	m.cursor = m.matches[(at+by+len(m.matches))%len(m.matches)]
}

func (m *Menu) Select() int {
	if m == nil {
		return 0
	}
	// A filter matching nothing leaves the cursor on a row that is not on
	// screen. Committing it would write a value the user never saw.
	if m.open && len(m.options) > 0 && m.drawn(m.cursor) {
		m.index = m.cursor
	}
	m.open = false
	return m.index
}

func (m *Menu) Cancel() {
	if m == nil {
		return
	}
	m.open = false
	m.cursor = m.index
}

func (m *Menu) Value() string {
	if m == nil {
		return ""
	}
	if len(m.values) > 0 {
		if m.index < 0 || m.index >= len(m.values) {
			return ""
		}
		return m.values[m.index]
	}
	if m.index < 0 || m.index >= len(m.options) {
		return ""
	}
	return m.options[m.index]
}

func (m *Menu) label() string {
	if m == nil || m.index < 0 || m.index >= len(m.options) {
		return ""
	}
	return m.options[m.index]
}

// PickAt moves the cursor to the option under the point and reports whether
// it found one. A picker's first child is its filter well, and a press there
// is not a choice.
func (m *Menu) PickAt(n *ui.Node, x, y int) bool {
	if m == nil || n == nil {
		return false
	}
	for i, c := range n.Children {
		if c == nil || !c.Bounds.Contains(x, y) {
			continue
		}
		if m.filter == nil {
			m.cursor = i
			return true
		}
		if i == 0 || i-1 >= len(m.matches) {
			return false
		}
		m.cursor = m.matches[i-1]
		return true
	}
	return false
}

func (m *Menu) Node() *ui.Node {
	if m == nil {
		return &ui.Node{Kind: ui.KindMenu, Role: "combobox"}
	}
	n := &ui.Node{
		Kind:      ui.KindMenu,
		Text:      m.label(),
		Value:     float64(m.index),
		Focusable: true,
		Name:      m.label(),
		Role:      "combobox",
	}
	if !m.open {
		return n
	}
	if m.filter == nil {
		for i, opt := range m.options {
			child := &ui.Node{Kind: ui.KindText, Text: opt}
			if i == m.cursor {
				child.Value = 1
			}
			n.Children = append(n.Children, child)
		}
		return n
	}
	// "Search" is the name the shared chrome keys the magnifier and the clear
	// glyph on, so the well draws like every other one in the shell. It is
	// deliberately not focusable: an open menu already captures every key, and
	// a focusable child would take the presses that belong to the menu node,
	// whose PickAt is what resolves a row.
	well := m.filter.Node("Search")
	well.Focusable = false
	if m.filterHeight > 0 {
		well.Height = m.filterHeight
	}
	n.Children = append(n.Children, well)
	for _, i := range m.matches {
		child := &ui.Node{Kind: ui.KindText, Text: m.options[i]}
		if i == m.cursor {
			child.Value = 1
		}
		n.Children = append(n.Children, child)
	}
	return n
}

// Handle routes keys while the menu is open. Escape cancels without changing
// the committed value. Returns false when the menu is closed so the panel can
// take Escape, and false for the keys a picker's well should take as text
// rather than as commands.
func (m *Menu) Handle(key uint32) bool {
	if m == nil || !m.open {
		return false
	}
	switch key {
	case keyDown:
		m.Next()
	case keyUp:
		m.Prev()
	case keyRight:
		if m.filter != nil {
			// The caret belongs to the well being typed into.
			m.filter.Move(1)
			return true
		}
		m.Next()
	case keyLeft:
		if m.filter != nil {
			m.filter.Move(-1)
			return true
		}
		m.Prev()
	case keyEnter:
		m.Select()
	case keySpace:
		// A space belongs in "DejaVu Sans". Only a menu without a well can
		// afford to spend it on accept.
		if m.filter != nil {
			return false
		}
		m.Select()
	case keyEsc:
		m.Cancel()
	default:
		return false
	}
	return true
}

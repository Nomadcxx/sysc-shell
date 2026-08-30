package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// Menu is an in-panel dropdown. It is not a Wayland surface: the open list
// is a child region of the already-mapped panel.
type Menu struct {
	options []string
	index   int
	cursor  int
	open    bool
}

func NewMenu(options []string, index int) *Menu {
	if len(options) == 0 {
		index = 0
	} else if index < 0 || index >= len(options) {
		index = 0
	}
	return &Menu{options: options, index: index, cursor: index}
}

func (m *Menu) Open() {
	if m == nil {
		return
	}
	m.open = true
	m.cursor = m.index
}

func (m *Menu) Opened() bool { return m != nil && m.open }

func (m *Menu) Index() int {
	if m == nil {
		return 0
	}
	return m.index
}

func (m *Menu) Next() {
	if m == nil || !m.open || len(m.options) == 0 {
		return
	}
	m.cursor = (m.cursor + 1) % len(m.options)
}

func (m *Menu) Prev() {
	if m == nil || !m.open || len(m.options) == 0 {
		return
	}
	m.cursor = (m.cursor - 1 + len(m.options)) % len(m.options)
}

func (m *Menu) Select() int {
	if m == nil {
		return 0
	}
	if m.open && len(m.options) > 0 {
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
	if m == nil || m.index < 0 || m.index >= len(m.options) {
		return ""
	}
	return m.options[m.index]
}

func (m *Menu) Node() *ui.Node {
	if m == nil {
		return &ui.Node{Kind: ui.KindMenu, Role: "combobox"}
	}
	n := &ui.Node{
		Kind:      ui.KindMenu,
		Text:      m.Value(),
		Value:     float64(m.index),
		Focusable: true,
		Name:      m.Value(),
		Role:      "combobox",
	}
	if m.open {
		for i, opt := range m.options {
			child := &ui.Node{Kind: ui.KindText, Text: opt}
			if i == m.cursor {
				child.Value = 1
			}
			n.Children = append(n.Children, child)
		}
	}
	return n
}

// Handle routes keys while the menu is open. Escape cancels without changing
// the committed value. Returns false when the menu is closed so the panel
// can take Escape.
func (m *Menu) Handle(key uint32) bool {
	if m == nil || !m.open {
		return false
	}
	switch key {
	case keyDown, keyRight:
		m.Next()
	case keyUp, keyLeft:
		m.Prev()
	case keyEnter, keySpace:
		m.Select()
	case keyEsc:
		m.Cancel()
	default:
		return false
	}
	return true
}

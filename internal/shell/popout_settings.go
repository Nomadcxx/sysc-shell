package shell

import (
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// settingsSections is the rail, and the vocabulary IPC section addressing
// validates against. It is the registry's own ordering rather than a second
// list: a section named here and nowhere else renders empty, and one named
// only there is unreachable.
var settingsSections = settings.SectionNames()

// settingsSidebarWidth is the measured width of the section rail. The search
// field takes it too, so the field and the tabs below it read as one column
// rather than two that happen to line up. It is named once because three
// sites share it: a number repeated three times drifts apart on the first
// edit that reaches only two of them.
const settingsSidebarWidth = 220

func settingsTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	search := h.search.Node("Search")
	search.Width = settingsSidebarWidth
	head := []*ui.Node{}
	if h.errLabel != "" {
		head = append(head, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	head = append(head, search)

	if strings.TrimSpace(h.query) != "" {
		var hits []settings.Entry
		if h.set != nil {
			hits = h.set.Search(h.query)
		}
		rows := head
		for _, e := range hits {
			rows = append(rows, &ui.Node{
				Kind: ui.KindButton, Text: e.Label, Action: "goto:" + e.Path,
				Name: e.Label, Role: "button", Focusable: true,
			})
		}
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: h.metrics().PanelPadding, Children: rows}
	}

	sidebar := head
	for _, name := range settingsSections {
		sidebar = append(sidebar, &ui.Node{
			Kind: ui.KindButton, Text: name, Action: "section:" + name,
			Name: name, Role: "tab", Focusable: true,
		})
	}

	section := h.section
	if section == "" {
		section = "Bar"
	}
	if section == "Plugins" {
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXL, Padding: h.metrics().PanelPadding, Children: []*ui.Node{
			{Kind: ui.KindColumn, Width: settingsSidebarWidth, Gap: theme.MarginM, Children: sidebar},
			pluginsTree(r, h),
		}}
	}
	var entries []settings.Entry
	if h.set != nil {
		entries = h.set.Section(section)
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXL, Padding: h.metrics().PanelPadding, Children: []*ui.Node{
		{Kind: ui.KindColumn, Width: settingsSidebarWidth, Gap: theme.MarginM, Children: sidebar},
		settingsSectionColumn(h, entries),
	}}
}

// settingsSectionColumn lays the whole section out rather than virtualising
// it. KindVirtualList is strictly uniform stride — column.go boxes every item
// at ItemHeight and advances by exactly that — so a caption beneath a label
// and a heading above a run of rows cannot exist under it. Sections bound the
// row count, which is what keeps laying the whole thing out cheap.
func settingsSectionColumn(h *PanelHost, entries []settings.Entry) *ui.Node {
	var order []string
	rows := map[string][]settings.Entry{}
	for _, e := range entries {
		if _, seen := rows[e.Group]; !seen {
			order = append(order, e.Group)
		}
		rows[e.Group] = append(rows[e.Group], e)
	}
	groups := make([]*ui.Node, 0, len(order))
	for _, name := range order {
		body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM}
		for _, e := range rows[name] {
			body.Children = append(body.Children, settingsEntryRow(h, e))
		}
		if name == "" {
			groups = append(groups, body)
			continue
		}
		groups = append(groups, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
			body,
		}})
	}
	return &ui.Node{Kind: ui.KindScroll, Gap: theme.MarginXL, Children: groups}
}

func settingsEntryRow(h *PanelHost, e settings.Entry) *ui.Node {
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: e.Label, Name: e.Label},
	}}
	if e.Describe != "" {
		label.Children = append(label.Children, &ui.Node{
			Kind: ui.KindText, Text: e.Describe,
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS}
	// Deviation is ambient: only a row that has moved off its default carries
	// the control that puts it back.
	if !e.IsDefault(h.draft) {
		trailing.Children = append(trailing.Children, settingsResetButton(e))
	}
	trailing.Children = append(trailing.Children, settingsControl(h, e))

	row := &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{label, trailing}}
	if e.Describe == "" {
		// A one-line row takes the control height so a run of them reads as a
		// ladder. A described row is two stacked lines and a fixed height
		// would crop the caption, so it measures itself; every size inside it
		// still comes from the density ladder.
		row.Height = h.metrics().StandardControl
	}
	return row
}

func settingsResetButton(e settings.Entry) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Text: "Reset", Action: "reset:" + e.Path,
		Name: "Reset " + e.Label, Role: "button", Focusable: true,
	}
}

func settingsControl(h *PanelHost, e settings.Entry) *ui.Node {
	raw := ""
	if h.set != nil {
		raw = e.Get(h.draft)
	}
	action := "set:" + e.Path
	switch e.Kind {
	case settings.KindBool:
		v := 0.0
		if raw == "true" {
			v = 1
		}
		return &ui.Node{
			Kind: ui.KindToggle, Value: v, Action: action,
			Focusable: true, Name: e.Label, Role: "switch",
		}
	case settings.KindInt:
		n, _ := strconv.Atoi(raw)
		return &ui.Node{
			Kind: ui.KindSlider, Value: float64(n), Min: float64(e.Min), Max: float64(e.Max), Step: 1,
			Action: action, Width: 160, Focusable: true, Name: e.Label, Role: "slider", // token-exempt: a slider's track width, a measured control dimension rather than a ladder value
		}
	case settings.KindEnum:
		idx := 0
		for i, o := range e.Options {
			if o == raw {
				idx = i
				break
			}
		}
		if h.menus == nil {
			h.menus = map[string]*Menu{}
		}
		m := h.menus[e.Path]
		if m == nil || !m.Opened() {
			m = NewMenu(e.Options, idx)
			h.menus[e.Path] = m
		}
		n := m.Node()
		n.Action = action
		n.Name = e.Label
		return n
	default:
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f := h.fields[e.Path]
		if f == nil {
			f = ui.NewField(raw)
			h.fields[e.Path] = f
		}
		n := f.Node(e.Label)
		n.Action = action
		n.Width = 200
		return n
	}
}

func (h *PanelHost) persistDraft(r *Registry) {
	if r == nil {
		return
	}
	if err := r.writeConfig(h.draft); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	h.errLabel = ""
}

func (r *Registry) writeConfig(c config.Config) error {
	if r.configPath == "" {
		return nil
	}
	if err := config.Write(r.configPath, c); err != nil {
		return err
	}
	if r.reloads != nil {
		select {
		case r.reloads <- struct{}{}:
		default:
		}
	}
	return nil
}

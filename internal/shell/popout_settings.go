package shell

import (
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

var settingsSections = []string{"Bar", "Widgets", "Appearance", "Panels", "Session", "Accessibility"}

func settingsTree(h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	search := h.search.Node("Search")
	search.Width = 220

	if strings.TrimSpace(h.query) != "" {
		var hits []settings.Entry
		if h.set != nil {
			hits = h.set.Search(h.query)
		}
		rows := make([]*ui.Node, 0, len(hits)+1)
		rows = append(rows, search)
		for _, e := range hits {
			rows = append(rows, &ui.Node{
				Kind: ui.KindButton, Text: e.Label, Action: "goto:" + e.Path,
				Name: e.Label, Role: "button", Focusable: true,
			})
		}
		return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: rows}
	}

	sidebar := make([]*ui.Node, 0, len(settingsSections)+1)
	sidebar = append(sidebar, search)
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
	var content []*ui.Node
	if h.set != nil {
		for _, e := range h.set.Section(section) {
			content = append(content, settingsEntryRow(h, e))
		}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 16, Padding: 12, Children: []*ui.Node{
		{Kind: ui.KindColumn, Width: 220, Gap: 8, Children: sidebar},
		{Kind: ui.KindScroll, Children: content},
	}}
}

func settingsEntryRow(h *PanelHost, e settings.Entry) *ui.Node {
	control := settingsControl(h, e)
	return &ui.Node{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
		{Kind: ui.KindText, Text: e.Label},
		control,
	}}
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
			Action: action, Width: 160, Focusable: true, Name: e.Label, Role: "slider",
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

package shell

import (
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
		return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: rows}
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
	var entries []settings.Entry
	if h.set != nil {
		entries = h.set.Section(section)
	}
	content := &ui.Node{
		Kind:       ui.KindVirtualList,
		ItemCount:  len(entries),
		ItemHeight: 36,
		Item: func(i int) *ui.Node {
			if i < 0 || i >= len(entries) {
				return nil
			}
			return settingsEntryRow(h, entries[i])
		},
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 16, Padding: 12, Children: []*ui.Node{
		{Kind: ui.KindColumn, Width: 220, Gap: 8, Children: sidebar},
		content,
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

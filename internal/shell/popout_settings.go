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

// The rail reuses the control centre's measurements so the two surfaces read
// as one product rather than two that happen to sit side by side.
const (
	settingsRailWidth = ccRailWidth
	settingsRailItem  = ccRailItem
	// settingsSearchWidth is the search field's own width in the header. It no
	// longer shares the rail's, which a 56-wide icon column cannot carry.
	settingsSearchWidth = 260
)

// settingsSectionIcons names one glyph per section. Every name is confirmed
// against the pinned Material Symbols source and asserted by the render
// package's inventory: a name the subset lacks shapes to nothing and paints an
// invisible control rather than failing anywhere visible.
var settingsSectionIcons = map[string]string{
	"Appearance":    "palette",
	"Templates":     "description",
	"Bar":           "toolbar",
	"Widgets":       "widgets",
	"Panels":        "web_asset",
	"Wallpaper":     "wallpaper",
	"Weather":       "partly_cloudy_day",
	"Displays":      "display_settings",
	"Tray":          "apps",
	"Plugins":       "extension",
	"Session":       "power_settings_new",
	"Accessibility": "accessibility_new",
}

func settingsRail(h *PanelHost, section string) *ui.Node {
	rail := &ui.Node{Kind: ui.KindColumn, Width: settingsRailWidth, Gap: theme.MarginM}
	for _, name := range settingsSections {
		entry := &ui.Node{
			Kind: ui.KindButton, Width: settingsRailItem, Height: settingsRailItem,
			Action: "section:" + name, Name: name, Role: "tab", Focusable: true,
			Shape:    ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: settingsSectionIcons[name]}},
		}
		if name == section {
			entry.State |= ui.StateSelected
			entry.Fill = ui.FillAccent
		}
		rail.Children = append(rail.Children, entry)
	}
	return rail
}

func settingsTree(r *Registry, h *PanelHost) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	search := h.search.Node("Search")
	search.Width = settingsSearchWidth

	section := h.section
	if section == "" {
		section = settingsSections[0]
	}

	head := []*ui.Node{}
	if h.errLabel != "" {
		head = append(head, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	// The section name is always visible in the header, which is what lets the
	// group headings inside the column stay unsticky.
	head = append(head, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: section, TextRole: theme.RoleTitle},
		search,
	}})

	// The header spans the surface rather than sitting inside the body column
	// the way the control centre's does. That is the one place this pane does
	// not mirror it, and it is deliberate: the control centre has no search
	// field, while here the field has to be the first thing the keyboard
	// reaches, and focus order follows tree order.
	body := func(content *ui.Node) *ui.Node {
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Padding: h.metrics().PanelPadding,
			Children: append(append([]*ui.Node{}, head...), &ui.Node{
				Kind: ui.KindRow, Gap: theme.MarginXL, Children: []*ui.Node{
					settingsRail(h, section),
					content,
				},
			}),
		}
	}

	if strings.TrimSpace(h.query) != "" {
		var hits []settings.Entry
		if h.set != nil {
			hits = h.set.Search(h.query)
		}
		return body(settingsSearchColumn(h, hits))
	}

	if section == "Plugins" {
		return body(pluginsTree(r, h))
	}
	var entries []settings.Entry
	if h.set != nil {
		entries = h.set.Section(section)
	}
	return body(settingsSectionColumn(h, entries))
}

// settingsSearchColumn groups matches under the section that owns them, in
// rail order. The shipped pane replaced the rail with a flat list of labels,
// which told the user what matched but never where it lived; at two hundred
// entries that is the difference between a result and an answer. The rows are
// the real ones, so a setting found by searching can be changed where it was
// found.
func settingsSearchColumn(h *PanelHost, hits []settings.Entry) *ui.Node {
	groups := []*ui.Node{}
	for _, name := range settingsSections {
		body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM}
		for _, e := range hits {
			if e.Section == name {
				body.Children = append(body.Children, settingsEntryRow(h, e))
			}
		}
		if len(body.Children) == 0 {
			continue
		}
		groups = append(groups, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
			body,
		}})
	}
	if len(groups) == 0 {
		groups = append(groups, &ui.Node{
			Kind: ui.KindText, Text: "No setting matches that.",
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	return &ui.Node{Kind: ui.KindScroll, Gap: theme.MarginXL, Children: groups}
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

package shell

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-text/typesetting/fontscan"

	"github.com/Nomadcxx/sysc-shell/internal/render"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
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

// settingsControlWidth is the room the trailing column takes. Controls used to
// take a fixed 200 regardless of the surface, which reads as a token field in
// a wide panel; sizing from the body keeps a field usable and keeps the row
// inside the column it sits in.
func settingsControlWidth(h *PanelHost) int {
	body := settingsBodyWidth(h)
	w := body * 2 / 5
	if w > body/2 {
		w = body / 2
	}
	return max(w, 0)
}

// settingsBodyWidth is what is left for the rows once the rail and the gutter
// have taken theirs. The rows right-pin their controls, so the column has to
// carry it: without a width the controls pin to the panel's own edge and every
// enum and field is clipped by the surface.
func settingsBodyWidth(h *PanelHost) int {
	panelWidth := panelTargetSize(PanelSettings).W
	if h != nil && h.place.Panel.W > 0 {
		panelWidth = h.place.Panel.W
	}
	pad := 0
	if h != nil {
		pad = h.metrics().PanelPadding
	}
	return max(panelWidth-2*pad-settingsRailWidth-theme.MarginXL, 0)
}

// settingsBody is the one scrolling column the pane's content sits in. It
// carries the retained offset, so an edit that rebuilds the tree leaves the
// user where they were rather than at the top.
func settingsBody(h *PanelHost, gap int, children ...*ui.Node) *ui.Node {
	return &ui.Node{
		Kind: ui.KindScroll, Width: settingsBodyWidth(h), Gap: gap,
		ScrollOffset: h.settingsScroll, Children: children,
	}
}

// settingsScrollOffset reads the offset back out of a built tree, so the next
// rebuild can restore it. layoutScroll clamps, so an offset left over from a
// longer list cannot strand the view past the end of a shorter one.
func settingsScrollOffset(root *ui.Node) int {
	var out int
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || out != 0 {
			return
		}
		if n.Kind == ui.KindScroll {
			out = n.ScrollOffset
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

func settingsRail(h *PanelHost, section string) *ui.Node {
	rail := &ui.Node{Kind: ui.KindColumn, Width: settingsRailWidth, Gap: theme.MarginM}
	for _, name := range settingsSections {
		entry := &ui.Node{
			Kind: ui.KindButton, Width: settingsRailItem, Height: settingsRailItem,
			Action: "section:" + name, Name: name, Role: "tab", Focusable: true,
			// The rail draws glyphs only, so the name has to be reachable by
			// hover as well as by screen reader.
			Tooltip:  name,
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
	// The header spans the surface and the field pins to its end, so the field
	// lands directly above the rows' trailing control column. Sharing that
	// column's width lines the two up and drops a literal 260 that was one
	// panel size's answer applied to every panel size.
	search.Width = settingsControlWidth(h)

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
		// The plugin host's view is a column of cards with no width of its
		// own, so inside the body row its switches stretched the full
		// surface. It gets the same bounded, scrolling column as a section.
		return body(settingsBody(h, theme.MarginM, pluginsTree(r, h)))
	}
	var entries []settings.Entry
	if h.set != nil {
		entries = h.set.Section(section)
	}
	column := settingsSectionColumn(h, section, entries)
	if section == "Bar" {
		// The lane editor is the Bar section's Layout group, above its
		// geometry rows. It replaces the three comma-separated string entries,
		// which is the whole point of the sub-project.
		column.Children = append([]*ui.Node{h.barLaneStripFor(r)}, column.Children...)
	}
	return body(column)
}

// settingsEmptySection explains a section that legitimately has nothing in it
// yet. Tray and Displays build their entries from what the configuration
// already names, so a user who has never set a tray preference or overridden
// an output is shown an empty column and no way to fill it. An empty surface
// that says nothing reads as a defect; saying why is the honest minimum until
// Tray can enumerate from the live host and Displays gets the per-output
// editing model, which belongs to sub-project C.
var settingsEmptySection = map[string]string{
	"Tray":     "No tray item has been given a preference yet. Pin or hide one from the tray itself and it will appear here.",
	"Displays": "No output carries its own bar override. Every display follows the settings in Bar.",
	"Widgets":  "The bar carries no widgets, so there is nothing to configure here.",
}

func settingsEmptyNote(section string) *ui.Node {
	text := settingsEmptySection[section]
	if text == "" {
		text = "Nothing to configure in this section yet."
	}
	return &ui.Node{
		Kind: ui.KindText, Text: text,
		TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
	}
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
	return settingsBody(h, theme.MarginXL, groups...)
}

// settingsSectionColumn lays the whole section out rather than virtualising
// it. KindVirtualList is strictly uniform stride — column.go boxes every item
// at ItemHeight and advances by exactly that — so a caption beneath a label
// and a heading above a run of rows cannot exist under it. Sections bound the
// row count, which is what keeps laying the whole thing out cheap.
func settingsSectionColumn(h *PanelHost, section string, entries []settings.Entry) *ui.Node {
	if len(entries) == 0 {
		return settingsBody(h, theme.MarginXL, settingsEmptyNote(section))
	}
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
	return settingsBody(h, theme.MarginXL, groups...)
}

func settingsEntryRow(h *PanelHost, e settings.Entry) *ui.Node {
	controlW := settingsControlWidth(h)
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: e.Label, Name: e.Label},
	}}
	if e.Describe != "" {
		label.Children = append(label.Children, &ui.Node{
			Kind: ui.KindText, Text: e.Describe,
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	// Both columns carry their width. Without it the description sets the
	// row's width and pushes the right-pinned control past the edge of the
	// column, which is invisible on a wide output and clips on a small one.
	label.Width = max(settingsBodyWidth(h)-controlW-theme.MarginL, 0)

	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: controlW, PinEnd: true}
	room := controlW
	if !e.IsDefault(h.draft) {
		reset := settingsResetButton(e)
		trailing.Children = append(trailing.Children, reset)
		room = max(room-settingsResetWidth(h)-theme.MarginS, 0)
	}
	trailing.Children = append(trailing.Children, settingsControl(h, e, room))

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

// settingsResetWidth is the room the reset control takes when a row shows one.
// It is a text button, so it is sized from the density ladder's master control
// dimension rather than from a literal: a fixed 64 was standard density's
// answer imposed on all five rows.
func settingsResetWidth(h *PanelHost) int { return h.metrics().BaseWidget * 2 }

func settingsResetButton(e settings.Entry) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Text: "Reset", Action: "reset:" + e.Path,
		Name: "Reset " + e.Label, Role: "button", Focusable: true,
	}
}

func settingsControl(h *PanelHost, e settings.Entry, width int) *ui.Node {
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
		// A short range is worth a pixel at a time, and a slider cannot give
		// that: a 0..32 track is a handful of pixels per step. A wide one —
		// an opacity, a scale — is easier to sweep than to click.
		if e.Max-e.Min > 0 && e.Max-e.Min <= settingsStepperSpan {
			return settingsStepper(e, n)
		}
		return &ui.Node{
			Kind: ui.KindSlider, Value: float64(n), Min: float64(e.Min), Max: float64(e.Max), Step: 1,
			Action: action, Width: width, Focusable: true, Name: e.Label, Role: "slider",
		}
	case settings.KindFont:
		return settingsMenuControl(h, e, settingsFontFamilies(), raw, width)
	case settings.KindPath:
		// The field gives up exactly what the browse button takes. This used
		// to reserve the reset control's width instead, which is a different
		// control: the two happened to be close at standard density and would
		// have diverged on any other row of the ladder.
		browse := h.metrics().IconButton
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: width, Children: []*ui.Node{
			settingsField(h, e, raw, max(width-browse-theme.MarginS, 0)),
			{
				Kind: ui.KindButton, Action: "browse:" + e.Path,
				Name: "Browse " + e.Label, Role: "button", Focusable: true,
				// Carrying the width the field just gave up is what keeps the
				// pair inside the control column instead of overrunning it.
				Width: browse, Height: browse,
				Shape:    ui.ShapeMedium,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "folder_open"}},
			},
		}}
	case settings.KindEnum:
		return settingsMenuControl(h, e, e.Options, raw, width)
	default:
		return settingsField(h, e, raw, width)
	}
}

// settingsStepperSpan is the widest range that reads better one step at a
// time than as a track to sweep.
const settingsStepperSpan = 32

// settingsStepper composes from existing kinds rather than adding one: two
// buttons and the value between them.
func settingsStepper(e settings.Entry, value int) *ui.Node {
	step := func(icon, dir, name string, enabled bool) *ui.Node {
		n := &ui.Node{
			Kind: ui.KindButton, Name: name + " " + e.Label, Role: "button",
			Shape: ui.ShapeMedium, Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon}},
		}
		if enabled {
			n.Action = "step:" + dir + ":" + e.Path
			n.Focusable = true
		} else {
			n.AriaDisabled = true
			n.State |= ui.StateDisabled
		}
		return n
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Name: e.Label, Children: []*ui.Node{
		step("remove", "down", "Decrease", value > e.Min),
		{Kind: ui.KindText, Text: strconv.Itoa(value)},
		step("add", "up", "Increase", value < e.Max),
	}}
}

func settingsMenuControl(h *PanelHost, e settings.Entry, options []string, raw string, width int) *ui.Node {
	idx := 0
	for i, o := range options {
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
		m = NewMenu(options, idx)
		h.menus[e.Path] = m
	}
	n := m.Node()
	n.Action = "set:" + e.Path
	n.Name = e.Label
	if width > 0 {
		n.Width = width
	}
	return n
}

func settingsField(h *PanelHost, e settings.Entry, raw string, width int) *ui.Node {
	if h.fields == nil {
		h.fields = map[string]*ui.Field{}
	}
	f := h.fields[e.Path]
	if f == nil {
		f = ui.NewField(raw)
		h.fields[e.Path] = f
	}
	n := f.Node(e.Label)
	n.Action = "set:" + e.Path
	n.Width = width
	// A colour is checkable as it is typed, so the field says so itself
	// rather than waiting for the write to fail.
	if e.Kind == settings.KindHex && !settingsValidHex(f.Text) {
		n.Tone = ui.ToneError
	}
	return n
}

// settingsValidHex asks the loader's own rule rather than restating it. The
// field marks a colour good as it is typed and the entry's setter decides
// whether the write is accepted; if those were two patterns, a value could
// mark itself valid and then be refused by the very write it was typed for.
// Both trim here, at this layer, so the rule itself matches what is stored.
func settingsValidHex(v string) bool { return config.ValidColor(strings.TrimSpace(v)) }

// settingsFontFamilies enumerates the scanned system fonts once. fontscan
// reads the disk, so it is not something a tree build can afford to repeat.
//
// Footprint.Family is stored normalized — "dejavusans", not "DejaVu Sans" —
// so these are the normalized names. Deriving a display form is its own
// decision; presenting them as they are is honest, and pretending they arrive
// pretty would be wrong.
var settingsFontFamilies = sync.OnceValue(func() []string {
	fonts, err := fontscan.SystemFonts(nil, render.DefaultFontCacheDir())
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range fonts {
		if f.Family == "" || seen[f.Family] {
			continue
		}
		seen[f.Family] = true
		out = append(out, f.Family)
	}
	sort.Strings(out)
	return out
})

// settingsBrowseOptions lists where a path setting can go from where it is:
// the directories inside it, and the one above it. os.ReadDir is the whole
// mechanism; no portal is involved.
func settingsBrowseOptions(current string) []string {
	dir := wallpaper.ExpandHome(current)
	var out []string
	if parent := filepath.Dir(dir); parent != dir && parent != "" {
		out = append(out, parent)
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, item := range items {
		if item.IsDir() && !strings.HasPrefix(item.Name(), ".") {
			out = append(out, filepath.Join(dir, item.Name()))
		}
	}
	return out
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

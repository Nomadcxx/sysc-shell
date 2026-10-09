package shell

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"

	"github.com/Nomadcxx/sysc-shell/internal/render"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
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

// settingsRailWidth holds an icon and a section name (settings redesign D5).
// The control centre's 56 fitted a glyph and nothing else.
const settingsRailWidth = 208

// settingsSectionIcons names one glyph per section. Every name is confirmed
// against the pinned Material Symbols source and asserted by the render
// package's inventory: a name the subset lacks shapes to nothing and paints an
// invisible control rather than failing anywhere visible.
var settingsSectionIcons = map[string]string{
	"Appearance":    "palette",
	"Palettes":      "tune",
	"Templates":     "description",
	"Bar":           "toolbar",
	"Widgets":       "widgets",
	"Panels":        "web_asset",
	"Monitor":       "memory",
	"Wallpaper":     "wallpaper",
	"Terminal Art":  "terminal",
	"Screensaver":   "schedule",
	"Night Light":   "bedtime",
	"Weather":       "partly_cloudy_day",
	"Displays":      "display_settings",
	"Tray":          "apps",
	"Plugins":       "extension",
	"Session":       "power_settings_new",
	"Lock Screen":   "lock",
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

// settingsDropdownFixedWidth is the width every dropdown and the narrowest
// segmented control take when the panel has room (the Appearance polish,
// carried to every page by sysc-858). A menu that hugs its value has no room
// for its chevron and reads as a static tag.
const settingsDropdownFixedWidth = 240

// settingsControlRoom is the control column's cap, less Reset when the row
// shows one.
func settingsControlRoom(h *PanelHost, e settings.Entry) int {
	available := max(settingsBodyWidth(h)/2, 0)
	if !e.IsDefault(h.draft) {
		available = max(available-settingsResetWidth(h)-theme.MarginS, 0)
	}
	return available
}

func settingsDropdown(e settings.Entry) bool {
	if e.Kind == settings.KindFont {
		return true
	}
	return e.Kind == settings.KindEnum && len(e.Options) > 1 && !settingsSegments(e)
}

func settingsSegments(e settings.Entry) bool {
	return e.Kind == settings.KindEnum && e.Present == settings.PresentAuto &&
		len(e.Options) >= 2 && len(e.Options) <= settingsSegmentLimit
}

func settingsDropdownWidth(h *PanelHost, e settings.Entry) int {
	return min(settingsDropdownFixedWidth, settingsControlRoom(h, e))
}

// settingsSegmentWidth gives every option the same width: the fixed width,
// or more when the longest label needs it, never past the control column.
func settingsSegmentWidth(h *PanelHost, e settings.Entry) int {
	measure := settingsMeasure(h)
	widest := 0
	for _, opt := range e.Options {
		w, _ := measure(settingsOptionLabel(opt), ui.TextAttrs{})
		widest = max(widest, w)
	}
	n := len(e.Options)
	need := n*(widest+2*h.metrics().ButtonPadding) + (n-1)*theme.MarginXXS
	return min(max(settingsDropdownFixedWidth, need), settingsControlRoom(h, e))
}

func settingsControlWidthFor(h *PanelHost, e settings.Entry) int {
	w := settingsControlWidth(h)
	maxColumn := max(settingsBodyWidth(h)/2, 0)
	need := 0
	switch {
	case settingsSegments(e):
		need = settingsSegmentWidth(h, e)
	case settingsDropdown(e):
		need = settingsDropdownWidth(h, e)
	}
	if need > 0 && !e.IsDefault(h.draft) {
		need += settingsResetWidth(h) + theme.MarginS
	}
	return min(max(w, need), maxColumn)
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

// settingsBodyKey and settingsRailKey name the pane's two scrolls, so a lookup
// for the content does not land on the section list or the other way round.
const (
	settingsBodyKey = "settings-body"
	settingsRailKey = "settings-rail"
)

// settingsBody is the one scrolling column the pane's content sits in. It
// carries the retained offset, so an edit that rebuilds the tree leaves the
// user where they were rather than at the top.
func settingsBody(h *PanelHost, gap int, children ...*ui.Node) *ui.Node {
	return &ui.Node{
		Kind: ui.KindScroll, Key: settingsBodyKey, Width: settingsBodyWidth(h), Gap: gap,
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
		// The rail is a scroll too, and it never scrolls; the body's offset is
		// the one worth restoring.
		if n.Kind == ui.KindScroll && n.Key == settingsBodyKey {
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

// settingsRail is the section list: search first, then each cluster's
// caption and its sections as icon-and-name tabs (settings redesign D5). It
// runs the full height of the pane, so the title and page tabs sit over the
// content only, and search stays the first thing the keyboard reaches.
func settingsRail(h *PanelHost, section string, head []*ui.Node) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	search := h.search.Node("Search")
	search.Width = settingsRailWidth
	settingsFieldInset(h, search)
	search.Placeholder = "Search settings…"
	// Bounded like the content column: a tall font, a short pane or a long
	// section list scrolls the rail instead of pushing its last tab past the
	// pane's content edge.
	rail := &ui.Node{Kind: ui.KindScroll, Key: settingsRailKey, Width: settingsRailWidth, Height: settingsContentHeight(h, head), Gap: settingsRailGap(), Children: []*ui.Node{search}}
	item, itemPad := settingsRailItemHeight(h, search)
	for _, c := range settings.SectionClusters() {
		rail.Children = append(rail.Children, &ui.Node{
			Kind: ui.KindText, Text: c.Name, TextRole: theme.RoleCaption,
			Tone: ui.ToneSubtle, Role: "heading",
		})
		for _, name := range c.Sections {
			entry := &ui.Node{
				Kind: ui.KindButton, Width: settingsRailWidth, Height: item,
				Action: "section:" + name, Name: name, Role: "tab", Focusable: true,
				// A rail tab is a list row, not a push button: the button
				// padding (18 at spacious) would make twelve of them overrun
				// a short pane.
				Tooltip: name, Shape: ui.ShapeMedium, Padding: itemPad,
				// One row child: layoutButtonContent lays a single row out in
				// full, where several children would get no box (barChip).
				Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
					{Kind: ui.KindIcon, Icon: settingsSectionIcons[name]},
					{Kind: ui.KindText, Text: name},
				}}},
			}
			if name == section {
				entry.State |= ui.StateSelected
				entry.Fill = ui.FillAccent
			}
			rail.Children = append(rail.Children, entry)
		}
	}
	return rail
}

// settingsFieldInset gives a Settings text field the button inset, as the
// setting rows' fields have. Without it a field measures to its bare text, a
// strip about 22 px tall with the first glyph against the rounded edge; with
// it the field is a full control. A fixed height would not do: a field
// measures text plus padding in a row and its declared height in a column,
// and the two disagree once the padding outgrows the height.
func settingsFieldInset(h *PanelHost, n *ui.Node) {
	n.Padding = h.metrics().ButtonPadding
}

// ponytail: keep all sections visible on short outputs; each tab keeps its full icon and padding.
func settingsRailGap() int { return max(theme.MarginXXS/2, 1) }

// settingsRailItemHeight is a section tab's height and inset: the density's
// standard control, or less when the tabs, the cluster captions and search
// would not fit the pane. At spacious density on a 1280x720 output they ran
// 150 px past its bottom edge. The inset narrows in two steps before an icon
// row is allowed to force the rail past the pane.
func settingsRailItemHeight(h *PanelHost, search *ui.Node) (height, pad int) {
	m := h.metrics()
	if m.StandardControl <= 0 {
		return 0, theme.MarginS // no density metrics: the layout measures the tab
	}
	ph := h.place.Panel.H
	if ph <= 0 {
		ph = panelTargetSize(PanelSettings).H
	}
	clusters := settings.SectionClusters()
	measure := settingsMeasure(h)
	_, captionH := measure("Look", ui.TextAttrs{Role: theme.RoleCaption})
	// The field's laid-out height, as the row layout measures it: one padded
	// line, or its declared height when that is taller. Assuming InputHeight
	// ran the last tab past the pane once the field took the button inset.
	_, lineH := measure(" ", ui.TextAttrsOf(search))
	searchH := max(search.Height, lineH+2*search.Padding)
	children := 1 + len(clusters) + len(settingsSections)
	room := ph - 2*m.PanelPadding - searchH - len(clusters)*captionH - (children-1)*settingsRailGap()
	per := room / max(len(settingsSections), 1)
	pad = theme.MarginS
	for _, tighter := range []int{theme.MarginXS, theme.MarginXXS} {
		if per < m.IconNormal+2*pad {
			pad = tighter
		}
	}
	if per < m.IconNormal+2*pad {
		pad = theme.MarginXXS
	}
	// Never shorter than the icon and its padding, which the tab has to hold.
	return max(min(m.StandardControl, per), captionH, m.IconNormal+2*pad), pad
}

// settingsPageTabs switches a section's pages. They are a segmented control
// of radios: "tab" is the rail's role, one per section.
func settingsPageTabs(h *PanelHost, pages []string, page string) *ui.Node {
	m := h.metrics()
	seg := &ui.Node{Kind: ui.KindSegmented, Key: "settings-page", Gap: theme.MarginXXS, Height: m.CompactControl, Name: "Pages", Role: "radiogroup"}
	for _, p := range pages {
		b := &ui.Node{
			Kind: ui.KindButton, Action: "page:" + p, Name: p, Role: "radio",
			Focusable: true, Height: m.CompactControl,
			Children: []*ui.Node{{Kind: ui.KindText, Text: p}},
		}
		if p == page {
			b.State |= ui.StateSelected
		}
		seg.Children = append(seg.Children, b)
	}
	return seg
}

// settingsCurrentPage is the page the section shows: the host's, when the
// section has it, else the section's first. Empty for a one-page section.
func settingsCurrentPage(h *PanelHost, section string) string {
	pages := settings.SectionPages(section)
	if len(pages) == 0 {
		return ""
	}
	if slices.Contains(pages, h.settingsPage) {
		return h.settingsPage
	}
	return pages[0]
}

func settingsTree(r *Registry, h *PanelHost) *ui.Node {
	h.settingsTreeScale = h.scale120
	section := h.section
	if section == "" {
		section = settingsSections[0]
	}
	page := settingsCurrentPage(h, section)
	searching := strings.TrimSpace(h.query) != ""

	head := []*ui.Node{}
	if h.errLabel != "" {
		head = append(head, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	// The section name is always visible over the content, which is what lets
	// the group titles inside the column stay unsticky.
	head = append(head, settingsSectionHeading(h, section))
	if pages := settings.SectionPages(section); len(pages) > 0 && !searching {
		head = append(head, settingsPageTabs(h, pages, page))
	}

	body := func(content *ui.Node) *ui.Node {
		settingsDimIdle(r, h, content)
		if content.Kind == ui.KindScroll {
			content.Height = settingsContentHeight(h, head)
		}
		right := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: append(append([]*ui.Node{}, head...), content)}
		return &ui.Node{Kind: ui.KindColumn, Padding: h.metrics().PanelPadding, Children: []*ui.Node{{
			Kind: ui.KindRow, Gap: theme.MarginXL, Children: []*ui.Node{settingsRail(h, section, head), right},
		}}}
	}

	if searching {
		var hits []settings.Entry
		if h.set != nil {
			hits = overlayIdleGets(r, h.draft.Session.Locker, h.set.Search(h.query))
		}
		return body(settingsSearchColumn(h, hits))
	}

	if section == "Palettes" {
		return body(settingsBody(h, theme.MarginM, palettesTree(r, h)))
	}

	if section == "Lock Screen" {
		return body(lockScreenSettingsTree(r, h))
	}

	if section == "Terminal Art" {
		return body(terminalArtSettingsTree(r, h))
	}
	if section == "Plugins" {
		// The plugin host's view is a column of cards with no width of its
		// own, so inside the body row its switches stretched the full
		// surface. It gets the same bounded, scrolling column as a section.
		return body(settingsBody(h, theme.MarginM, pluginsTree(r, h)))
	}
	if section == "Bar" {
		return body(settingsBarPage(r, h, page))
	}
	if section == "Screensaver" {
		return body(screensaverSettingsBody(r, h))
	}
	var entries []settings.Entry
	if h.set != nil {
		entries = overlayIdleGets(r, h.draft.Session.Locker, h.set.Section(section))
	}
	if section == "Tray" {
		entries = settingsTrayTitles(r, entries)
	}
	content := settingsSectionColumn(r, h, section, entries)
	if section == "Session" {
		content.Children = append(content.Children, polkitStatusCard(r, h))
	}
	if section == "Appearance" && r != nil {
		// The source may say custom while a saved palette is not what is
		// painted; say why where the source is chosen (P4).
		if problem := customPaletteProblem(h.draft, r.themeErr, r.palettes); problem != "" {
			content.Children = append([]*ui.Node{h.wrappedText(problem, theme.RoleBody, ui.ToneError, settingsBodyWidth(h), 0)}, content.Children...)
		}
	}
	return body(content)
}

// settingsTrayTitles names each Tray card after the item it holds (sysc-861).
// A running item gives its own title; one that is not running falls back to
// the token's value, marked with what kind of token it is, so id:blueman and
// title:blueman stay two cards that can be told apart. Only the card title
// changes: the paths and the stored tokens do not.
func settingsTrayTitles(r *Registry, entries []settings.Entry) []settings.Entry {
	live := map[string]string{}
	if r != nil && r.tray != nil {
		r.tray.mu.Lock()
		for _, item := range r.tray.items {
			if token, ok := stableTrayToken(item); ok && strings.TrimSpace(item.Title) != "" {
				live[token] = strings.TrimSpace(item.Title)
			}
		}
		r.tray.mu.Unlock()
	}
	out := make([]settings.Entry, len(entries))
	for i, e := range entries {
		switch kind, value, _ := strings.Cut(e.Group, ":"); {
		case live[e.Group] != "":
			e.Group = live[e.Group]
		case kind == "id":
			e.Group = value + " (app ID)"
		case kind == "title":
			e.Group = value + " (title)"
		}
		out[i] = e
	}
	return out
}

// settingsSectionHeading frames the section title with the launcher's SYSC
// rail, "////// appearance //////", set left over the content (owner decision,
// appearance polish design, 2026-09-30). The slashes take the title's role so
// they sit on its line, and carry no name: the title alone is the heading.
// The card padding insets it, so the first slash lines up with the card text
// under it rather than with the card's edge.
func settingsSectionHeading(h *PanelHost, section string) *ui.Node {
	slashes := func() *ui.Node {
		return &ui.Node{Kind: ui.KindText, Text: launcherSlashRun, TextRole: theme.RolePage, Tone: ui.ToneAccent}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Padding: h.metrics().CardPadding, Children: []*ui.Node{
		slashes(),
		{
			Kind: ui.KindText, Text: strings.ToLower(section), Name: section, Role: "heading",
			TextRole: theme.RolePage, Tone: ui.ToneAccent,
		},
		slashes(),
	}}
}

// settingsContentHeight is what the scrolling body gets once the title, the
// page tabs and their gaps are taken. The body sits in a column under them,
// and a scroll in a column with no height of its own takes the layout's 240
// fallback: the live gate found the Appearance page cut off after Shape.
func settingsContentHeight(h *PanelHost, head []*ui.Node) int {
	ph := h.place.Panel.H
	if ph <= 0 {
		ph = panelTargetSize(PanelSettings).H
	}
	used := 2 * h.metrics().PanelPadding
	measure := settingsMeasure(h)
	for _, n := range head {
		if n.Height > 0 {
			used += n.Height
		} else if height, err := ui.ContentHeight(n, settingsBodyWidth(h), measure); err == nil {
			used += height
		} else {
			_, height := measure(n.Text, ui.TextAttrsOf(n))
			used += height
		}
		used += theme.MarginL
	}
	return max(ph-used, 0)
}

// settingsMeasure is the pane's text measure with the text engine loaded. The
// tree is built before anything else loads it, and measureText's fallback
// guesses (sysc-589 was that guess).
func settingsMeasure(h *PanelHost) ui.MeasureText {
	if h.theme.Valid() == nil {
		_ = h.ensureText()
	}
	return h.measureText()
}

// settingsBarPage is one of Bar's pages (settings redesign D8). Appearance
// leads with the live preview and the Style and Shape picture cards, above
// the rest of its groups; Layout is the lane editor; Displays holds the
// per-output overrides.
func settingsBarPage(r *Registry, h *PanelHost, page string) *ui.Node {
	var entries []settings.Entry
	if h.set != nil {
		entries = h.set.PageEntries("Bar", page)
	}
	switch page {
	case "Layout":
		if r == nil {
			return settingsBody(h, theme.MarginL)
		}
		return settingsBody(h, theme.MarginL, h.barLaneStripFor(r))
	case "Displays":
		if len(entries) == 0 {
			return settingsBody(h, theme.MarginL, settingsEmptyNote("Displays"))
		}
		return settingsPageColumn(h, entries)
	}
	var lead []*ui.Node
	if r != nil {
		lead = append(lead, settingsBarPreview(r, h))
	}
	var rest []settings.Entry
	for _, e := range entries {
		if e.Present == settings.PresentCards {
			cards := []*ui.Node{settingsCardsFor(r, h, e)}
			if reason := settingsBarDimReason(h, e.Path); reason != "" {
				settingsDimRow(&ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{{Kind: ui.KindColumn}, cards[0]}}, reason)
				cards = append(cards, &ui.Node{Kind: ui.KindText, Text: reason, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
			}
			lead = append(lead, settingsGroupCard(h, e.Label, cards))
			continue
		}
		rest = append(rest, e)
	}
	col := settingsPageColumn(h, rest, lead...)
	settingsDimBarRows(h, col)
	return col
}

// settingsCardsFor is the picture cards, or the same choices without pictures
// when there is no registry to paint from.
func settingsCardsFor(r *Registry, h *PanelHost, e settings.Entry) *ui.Node {
	if r != nil {
		return settingsPictureCards(r, h, e)
	}
	raw := ""
	if e.Get != nil {
		raw = e.Get(h.draft)
	}
	return settingsSegmented(h, e, raw)
}

// settingsBarDimReason says why a Bar setting does not apply to the draft,
// or "" when it does. Enabled always applies: it is how the rest comes back.
func settingsBarDimReason(h *PanelHost, path string) string {
	b := h.draft.Bar
	switch {
	case path == "bar.enabled" || !strings.HasPrefix(path, "bar."):
		return ""
	case !b.Enabled:
		return "The bar is off. Turn it on to change this."
	}
	hc := h.draft.Accessibility.HighContrast
	switch path {
	case "bar.frost-opacity":
		if hc {
			return "High contrast draws the bar solid."
		}
		if b.Style != "frosted" {
			return "Applies to the Frosted style."
		}
	case "bar.pill-opacity":
		if hc {
			return "High contrast draws the bar solid."
		}
		if b.Style == "solid" {
			return "Applies to the Frosted and Islands styles."
		}
	case "bar.shape":
		if b.Style == "islands" {
			return "Islands always floats."
		}
	}
	return ""
}

// settingsDimBarRows dims each row that does not apply, label and all, and
// puts the reason where its description was (design D8). The row stays, so
// the user learns why rather than hunting for a setting that vanished.
func settingsDimBarRows(h *PanelHost, root *ui.Node) {
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if path := settingsRowPath(n); path != "" {
			if reason := settingsBarDimReason(h, path); reason != "" {
				settingsDimRow(n, reason)
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// settingsRowPath is the setting a settingsEntryRow edits, or "" when n is
// not one: a pinned row of a label column and its trailing control.
func settingsRowPath(n *ui.Node) string {
	if n.Kind != ui.KindRow || !n.PinEnd || len(n.Children) != 2 || n.Children[0].Kind != ui.KindColumn {
		return ""
	}
	path := ""
	var find func(c *ui.Node)
	find = func(c *ui.Node) {
		if c == nil || path != "" {
			return
		}
		for _, prefix := range []string{"set:", "pick:", "step:up:", "step:down:"} {
			if rest, ok := strings.CutPrefix(c.Action, prefix); ok {
				path, _, _ = strings.Cut(rest, "=")
				return
			}
		}
		for _, k := range c.Children {
			find(k)
		}
	}
	find(n.Children[1])
	return path
}

// settingsDimRow disables every control in n, mutes its text, and replaces
// its description with reason.
func settingsDimRow(n *ui.Node, reason string) {
	var walk func(c *ui.Node)
	walk = func(c *ui.Node) {
		if c == nil {
			return
		}
		if c.Focusable || c.Action != "" {
			c.State |= ui.StateDisabled
			c.Focusable = false
		}
		if c.Kind == ui.KindText {
			c.Tone = ui.ToneSubtle
		}
		for _, k := range c.Children {
			walk(k)
		}
	}
	walk(n)
	label := n.Children[0]
	why := &ui.Node{Kind: ui.KindText, Text: reason, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}
	if len(label.Children) > 1 {
		label.Children[1] = why
	} else {
		label.Children = append(label.Children, why)
	}
}

// settingsAddress resolves an IPC or shortcut section name. "Section/Page"
// picks a page; "Displays" is the old rail section, now Bar › Displays.
func settingsAddress(requested string) (section, page string, ok bool) {
	section, page, _ = strings.Cut(requested, "/")
	if section == "Displays" && page == "" {
		section, page = "Bar", "Displays"
	}
	if !slices.Contains(settingsSections, section) {
		return "", "", false
	}
	pages := settings.SectionPages(section)
	switch {
	case len(pages) == 0:
		return section, "", page == ""
	case page == "":
		return section, pages[0], true
	default:
		return section, page, slices.Contains(pages, page)
	}
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
	"Displays": "No output overrides the bar yet. Every display follows the settings on Appearance.",
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
		pages := settings.SectionPages(name)
		if len(pages) == 0 {
			pages = []string{""}
		}
		// A hit says where it lives: "Bar › Appearance", or just the section
		// when it has one page.
		for _, page := range pages {
			body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM}
			for _, e := range hits {
				if e.Section == name && e.Page == page {
					body.Children = append(body.Children, settingsEntryRow(h, e, settingsBodyWidth(h)))
				}
			}
			if len(body.Children) == 0 {
				continue
			}
			caption := name
			if page != "" {
				caption += " › " + page
			}
			groups = append(groups, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: caption, TextRole: theme.RoleLabel},
				body,
			}})
		}
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
func settingsSectionColumn(r *Registry, h *PanelHost, section string, entries []settings.Entry) *ui.Node {
	if len(entries) == 0 {
		return settingsBody(h, theme.MarginXL, settingsEmptyNote(section))
	}
	if section == "Appearance" {
		return settingsPageColumn(h, entries, settingsAppearanceIntro(h))
	}
	if section == "Night Light" && r != nil && r.nightLight != nil {
		status := &ui.Node{
			Kind: ui.KindText, Name: "Night Light status", Role: "status",
			Text: nightLightStatusText(r.nightLight.State()), TextRole: theme.RoleCaption,
		}
		return settingsPageColumn(h, entries, settingsGroupCard(h, "Status", []*ui.Node{status}))
	}
	return settingsPageColumn(h, entries)
}

func polkitStatusLabel(status polkit.Status) string {
	switch {
	case status.Policy == polkit.PolicyOff || status.Reason == "disabled":
		return "Off"
	case status.Reason == polkit.ErrNoHelper.Error():
		return "Helper missing"
	case status.Registered:
		return "Registered"
	case status.Passive != "":
		return "Passive · " + status.Passive
	default:
		return "Unavailable"
	}
}

func polkitStatusCard(r *Registry, h *PanelHost) *ui.Node {
	label := "Unavailable"
	if r != nil {
		label = polkitStatusLabel(r.polkitStatusLocked())
	}
	return settingsGroupCard(h, "Authentication status", []*ui.Node{{
		Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Status", TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleBody, Tone: ui.ToneSubtle},
		},
	}})
}

func settingsAppearanceIntro(h *PanelHost) *ui.Node {
	return &ui.Node{
		Kind: ui.KindColumn, Width: settingsBodyWidth(h), Gap: theme.MarginXS,
		Children: []*ui.Node{
			{Kind: ui.KindText, Text: "theme and interface", Name: "Theme and interface", Role: "heading", TextRole: theme.RoleHeadline},
			{Kind: ui.KindText, Text: "Choose a palette, then adjust type, surfaces, transparency, and motion.",
				TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		},
	}
}

// settingsGroupCard is one group as a titled card (settings redesign D3,
// reversing the foundation design's plain columns).
func settingsGroupCard(h *PanelHost, title string, rows []*ui.Node) *ui.Node {
	m := h.metrics()
	col := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM}
	if title != "" {
		col.Children = append(col.Children, settingsCardHeading(title))
	}
	col.Children = append(col.Children, rows...)
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
		Shape: ui.ShapeCard, Width: settingsBodyWidth(h), Children: []*ui.Node{col},
	}
}

// settingsCardHeading is a group's title: lowercase on screen, the title as
// written for assistive technology.
func settingsCardHeading(title string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindText, Text: strings.ToLower(title), Name: title, Role: "heading",
		TextRole: theme.RoleSection, Tone: ui.ToneAccent,
	}
}

// settingsCardInner is the width a row gets inside a group card.
func settingsCardInner(h *PanelHost) int {
	return max(settingsBodyWidth(h)-2*h.metrics().CardPadding, 0)
}

// settingsPageColumn is a page's groups as cards, after any lead blocks (the
// bar preview, the picture cards), in the scrolling body.
func settingsPageColumn(h *PanelHost, entries []settings.Entry, lead ...*ui.Node) *ui.Node {
	rowW := settingsCardInner(h)
	var order []string
	groups := map[string][]settings.Entry{}
	for _, e := range entries {
		if _, seen := groups[e.Group]; !seen {
			order = append(order, e.Group)
		}
		groups[e.Group] = append(groups[e.Group], e)
	}
	children := append([]*ui.Node{}, lead...)
	section := ""
	if len(entries) > 0 {
		section = entries[0].Section
	}
	for _, g := range order {
		children = append(children, settingsGroupCard(h, g, settingsGroupRows(h, groups[g], rowW)))
	}
	gap := theme.MarginL
	if section == "Appearance" {
		gap = theme.MarginXL
	}
	return settingsBody(h, gap, children...)
}

// settingsGroupRows builds a card's rows. When every row in the card shares
// one description (Templates' twelve applications), it is said once, under
// the card's title, rather than repeated on every row. The entries keep it,
// so search still finds each one by it.
func settingsGroupRows(h *PanelHost, entries []settings.Entry, rowW int) []*ui.Node {
	shared := ""
	if len(entries) > 1 {
		shared = entries[0].Describe
		for _, e := range entries[1:] {
			if e.Describe != shared {
				shared = ""
				break
			}
		}
	}
	var rows []*ui.Node
	if shared != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: shared, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	for _, e := range entries {
		if shared != "" {
			e.Describe = ""
		}
		rows = append(rows, settingsEntryRow(h, e, rowW))
	}
	return rows
}

// settingsEntryRow is one setting: its label over its description, and the
// control at the end. width is the column the row sits in: the body for a
// search hit, the inside of a card for a group row.
func settingsEntryRow(h *PanelHost, e settings.Entry, width int) *ui.Node {
	controlW := settingsControlWidthFor(h, e)
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
	label.Width = max(width-controlW-theme.MarginL, 0)

	// Only a control that benefits from length takes the column: a slider is
	// swept and a field is typed into. A toggle and a dropdown have a size of
	// their own, and stretching them across a 300-pixel column is what made
	// them read as taking the whole panel. Either way the group is not a
	// pinned row of its own: the row pins it, so every control ends at the
	// card's edge with Reset beside it. Pinning inside the group moved a
	// control from the column's left edge to the card's edge the moment Reset
	// appeared, because a row only pins its second of two children.
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS}
	if settingsControlFills(e) {
		trailing.Width = controlW
	}
	room := controlW
	if !e.IsDefault(h.draft) {
		reset := settingsResetButton(h, e)
		trailing.Children = append(trailing.Children, reset)
		room = max(room-settingsResetWidth(h)-theme.MarginS, 0)
	}
	switch {
	case settingsDropdown(e):
		room = settingsDropdownWidth(h, e)
	case !settingsControlFills(e):
		room = 0
	}
	trailing.Children = append(trailing.Children, settingsControl(h, e, room))

	row := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: width, Children: []*ui.Node{label, trailing}}
	if e.Describe == "" {
		// A one-line row takes the control height so a run of them reads as a
		// ladder. A described row is two stacked lines and a fixed height
		// would crop the caption, so it measures itself; every size inside it
		// still comes from the density ladder.
		row.Height = h.metrics().StandardControl
	}
	return row
}

// settingsControlFills reports whether this entry's control earns the whole
// control column. Length is worth having where it is used: a slider is swept
// along it and a field is typed into it. A toggle, a stepper and a dropdown
// each have a size of their own and are simply placed at the end of the row.
func settingsControlFills(e settings.Entry) bool {
	switch e.Kind {
	case settings.KindString, settings.KindHex, settings.KindPath:
		return true
	case settings.KindInt:
		// A short range renders as a stepper, which is three small controls.
		return !settingsSteps(e)
	}
	return false
}

// settingsSteps reports whether an int row is a stepper. A short range is
// worth a pixel at a time, unless the entry asks for a slider so it matches
// the related values around it.
func settingsSteps(e settings.Entry) bool {
	return e.Present != settings.PresentSlider && e.Max-e.Min > 0 && e.Max-e.Min <= settingsStepperSpan
}

// settingsResetWidth is the room the reset control takes when a row shows one.
// It is sized from the density ladder's master control dimension rather than
// from a literal (a fixed 64 was standard density's answer imposed on all five
// rows), and never narrower than its label inside the button inset, which the
// outlined chrome now shows.
func settingsResetWidth(h *PanelHost) int {
	m := h.metrics()
	labelW, _ := settingsMeasure(h)("Reset", ui.TextAttrs{})
	return max(m.BaseWidget*2, labelW+2*m.ButtonPadding)
}

// settingsResetButton is outlined button chrome (sysc-864): as bare text
// beside a segmented control it read as one more option.
func settingsResetButton(h *PanelHost, e settings.Entry) *ui.Node {
	m := h.metrics()
	return &ui.Node{
		Kind: ui.KindButton, Text: "Reset", Action: "reset:" + e.Path,
		Name: "Reset " + e.Label, Role: "button", Focusable: true,
		Width: settingsResetWidth(h), Height: m.CompactControl,
		Fill: ui.FillOutline, Shape: ui.ShapeMedium,
	}
}

func settingsControl(h *PanelHost, e settings.Entry, width int) *ui.Node {
	raw := ""
	if e.Get != nil {
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
		if settingsSteps(e) {
			return settingsStepper(e, n)
		}
		// The value sits beside the track in a cell measured for the widest
		// value the range holds, unit included (design D4; a fixed cell
		// overflowed at 1.25 in the audio panel, sysc-589).
		valueW, _ := settingsMeasure(h)(strconv.Itoa(max(e.Max, -e.Min))+e.Unit, ui.TextAttrs{Tabular: true})
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: width, Children: []*ui.Node{
			{
				Kind: ui.KindSlider, Value: float64(n), Min: float64(e.Min), Max: float64(e.Max), Step: float64(max(e.Step, 1)),
				Action: action, Width: max(width-valueW-theme.MarginS, 0), Focusable: true, Name: e.Label, Role: "slider",
			},
			{Kind: ui.KindText, Text: strconv.Itoa(n) + e.Unit, Tabular: true, Width: valueW},
		}}
	case settings.KindFont:
		options, values := settingsFontOptions(e)
		return settingsPickerControl(h, e, options, values, raw, width)
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
		// One option is a fact, not a choice: a one-row menu drew as a
		// clipped pill (the bar's Edge, which is only ever top).
		if len(e.Options) == 1 {
			return &ui.Node{Kind: ui.KindText, Text: settingsEntryOptionLabel(e, 0), Tone: ui.ToneSubtle, Name: e.Label}
		}
		if settingsSegments(e) {
			return settingsSegmented(h, e, raw)
		}
		if e.Present == settings.PresentSwatch {
			return settingsSwatchControl(h, e, raw, width)
		}
		if len(e.OptionLabels) > 0 && len(e.OptionLabels) == len(e.Options) {
			return settingsPickerControl(h, e, e.OptionLabels, e.Options, raw, width)
		}
		return settingsMenuControl(h, e, e.Options, raw, width)
	default:
		return settingsField(h, e, raw, width)
	}
}

// settingsSegmentLimit is the most options a segmented control shows. Past
// it the labels crowd the control column and a menu reads better (settings
// redesign D2).
const settingsSegmentLimit = 4

// settingsSegmented shows every option at once, the way the audio panel's
// tabs do. Each segment writes its value through the pick action.
func settingsSegmented(h *PanelHost, e settings.Entry, raw string) *ui.Node {
	m := h.metrics()
	seg := &ui.Node{
		Kind: ui.KindSegmented, Key: "seg:" + e.Path, Gap: theme.MarginXXS,
		Height: m.CompactControl, Name: e.Label, Role: "radiogroup",
		Width: settingsSegmentWidth(h, e),
	}
	for _, opt := range e.Options {
		label := settingsOptionLabel(opt)
		b := &ui.Node{
			Kind: ui.KindButton, Action: "pick:" + e.Path + "=" + opt,
			Name: label, Role: "radio", Focusable: true, Height: m.CompactControl,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
		}
		if opt == raw {
			b.State |= ui.StateSelected
		}
		seg.Children = append(seg.Children, b)
	}
	return seg
}

// settingsOptionLabel turns a config value into a label: "auto-pause" reads
// "Auto pause".
// settingsEntryOptionLabel is option i as the entry names it on screen: its
// own label when it carries labels, the readable form of the value otherwise.
func settingsEntryOptionLabel(e settings.Entry, i int) string {
	if len(e.OptionLabels) == len(e.Options) {
		return e.OptionLabels[i]
	}
	return settingsOptionLabel(e.Options[i])
}

func settingsOptionLabel(opt string) string {
	s := strings.NewReplacer("-", " ", "_", " ").Replace(opt)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
		{Kind: ui.KindText, Text: strconv.Itoa(value) + e.Unit},
		step("add", "up", "Increase", value < e.Max),
	}}
}

func settingsMenuControl(h *PanelHost, e settings.Entry, options []string, raw string, width int) *ui.Node {
	return settingsPickerControl(h, e, options, nil, raw, width)
}

// settingsSwatchControl shows a theme role as what it looks like: a swatch of
// the selected role beside a dropdown of readable names ("Surface variant",
// not surface_variant). The dropdown still writes the role's stored name.
func settingsSwatchControl(h *PanelHost, e settings.Entry, raw string, width int) *ui.Node {
	size := h.metrics().CompactControl
	labels := make([]string, len(e.Options))
	for i, opt := range e.Options {
		labels[i] = settingsOptionLabel(opt)
	}
	menu := settingsPickerControl(h, e, labels, e.Options, raw, max(width-size-theme.MarginS, 0))
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Width: width}
	if role, ok := ui.PaintRoleFor(raw); ok {
		row.Children = append(row.Children, &ui.Node{
			Kind: ui.KindCapsule, Width: size, Height: size, Shape: ui.ShapeMedium,
			Fill: ui.FillRole, FillRole: role, Role: "img", Name: settingsOptionLabel(raw) + " swatch",
		})
	} else {
		row.Children = append(row.Children, &ui.Node{Kind: ui.KindColumn, Width: size})
	}
	row.Children = append(row.Children, menu)
	return row
}

// settingsMenuLimit is the point past which a list stops being readable whole
// and becomes something to search. An enum's vocabulary never approaches it;
// the font list passes it on any machine with fonts installed.
const settingsMenuLimit = 12

// settingsPickerControl renders one menu, filtered when its list is long
// enough to need it. values may be nil, in which case each option writes
// itself.
func settingsPickerControl(h *PanelHost, e settings.Entry, options, values []string, raw string, width int) *ui.Node {
	idx := settingsOptionIndex(options, values, raw)
	if h.menus == nil {
		h.menus = map[string]*Menu{}
	}
	m := h.menus[e.Path]
	if m == nil || !m.Opened() {
		if len(options) > settingsMenuLimit {
			m = NewPicker(options, values, idx)
		} else {
			m = newMenu(options, values, idx, false)
		}
		h.menus[e.Path] = m
	}
	// Reassigned on every build rather than at construction: the ladder moves
	// when the density does, and a retained menu outlives that change.
	m.filterHeight = h.metrics().InputHeight
	n := m.Node()
	n.Action = "set:" + e.Path
	n.Name = e.Label
	n.Padding = h.metrics().ButtonPadding
	if width > 0 {
		n.Width = width
	}
	return n
}

// settingsOptionIndex finds the row the stored value sits on. A font family
// is compared normalized when nothing matches outright: a configuration
// written before the picker offered descriptive names carries "dejavusans"
// for the row now drawn as "DejaVu Sans", and showing the first row instead
// would claim the user had chosen something they had not.
func settingsOptionIndex(options, values []string, raw string) int {
	against := options
	if len(values) > 0 {
		against = values
	}
	for i, v := range against {
		if v == raw {
			return i
		}
	}
	if raw == "" {
		return 0
	}
	want := font.NormalizeFamily(raw)
	for i, v := range against {
		if v != "" && font.NormalizeFamily(v) == want {
			return i
		}
	}
	return 0
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
	n.Padding = h.metrics().ButtonPadding
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
// which is not a name to show anyone, and title-casing it cannot recover the
// word boundaries it dropped. The font's own name table is the only honest
// source, so each family's first file is opened for its metadata: the
// container loads without parsing coverage tables, which is what makes this
// affordable. Measured here over 345 families: 21ms to scan, 10ms to name.
// A full font.ParseTTF of the same files costs 623ms, and buys nothing this
// needs.
//
// The value written is the descriptive name, matching theme.DefaultFontFamily
// ("Inter Variable"), which is what the rest of the configuration already
// carries. A font whose metadata will not read keeps its normalized name
// rather than dropping out of the list.
var settingsFontFamilies = sync.OnceValue(func() []string {
	fonts, err := fontscan.SystemFonts(nil, render.DefaultFontCacheDir())
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var buf []byte
	for _, f := range fonts {
		if f.Family == "" || seen[f.Family] {
			continue
		}
		seen[f.Family] = true
		name := f.Family
		if described, rest := settingsFontName(f.Location.File, buf); described != "" {
			name, buf = described, rest
		}
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i]), strings.ToLower(out[j])
		if a == b {
			return out[i] < out[j]
		}
		return a < b
	})
	return out
})

// settingsFontName reads one font file's descriptive family. It returns the
// scratch buffer back so a run over several hundred files reuses one
// allocation, which is the shape font.Describe is built for.
func settingsFontName(path string, buf []byte) (string, []byte) {
	f, err := os.Open(path)
	if err != nil {
		return "", buf
	}
	defer f.Close()
	ld, err := opentype.NewLoader(f)
	if err != nil {
		return "", buf
	}
	desc, rest := font.Describe(ld, buf)
	return strings.TrimSpace(desc.Family), rest
}

// settingsFontOptions is the font picker's vocabulary: every scanned family,
// behind the entry's own row for the empty value where it declares one. A
// field could always be cleared; a menu can only offer what it draws, and it
// must not offer an empty the loader will refuse — the appearance families
// have no empty state, and the bar's is "follow the appearance font".
func settingsFontOptions(e settings.Entry) (options, values []string) {
	families := settingsFontFamilies()
	options = make([]string, 0, len(families)+1)
	values = make([]string, 0, len(families)+1)
	if e.EmptyLabel != "" {
		options = append(options, e.EmptyLabel)
		values = append(values, "")
	}
	for _, f := range families {
		options = append(options, f)
		values = append(values, f)
	}
	return options, values
}

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

// overlayIdleGets swaps in the idle getters that read live walls state, and
// says why Lock is unavailable while no locker is configured, since the option
// is otherwise greyed out without a reason.
func overlayIdleGets(r *Registry, locker string, entries []settings.Entry) []settings.Entry {
	if r == nil {
		return entries
	}
	snap := r.wallsSnapshot
	for i := range entries {
		switch entries[i].Path {
		case "idle.after":
			if strings.TrimSpace(locker) == "" {
				entries[i].Describe += " Lock needs a Locker: set it under Lock, e.g. sysc-lock."
			}
			entries[i].Get = func(c config.Config) string {
				return settings.WhenIdleMode(c.Idle.Lock, snap.EnabledAtLogin())
			}
		case "idle.delay":
			entries[i].Get = func(c config.Config) string {
				if settings.WhenIdleMode(c.Idle.Lock, snap.EnabledAtLogin()) == "nothing" {
					return ""
				}
				return settings.WhenIdleDelay(c.Idle.Lock, snap.Timeout).String()
			}
		}
	}
	return entries
}

func settingsDimIdle(r *Registry, h *PanelHost, root *ui.Node) {
	if r == nil || h == nil || root == nil {
		return
	}
	locker := strings.TrimSpace(h.draft.Session.Locker)
	mode := settings.WhenIdleMode(h.draft.Idle.Lock, r.wallsSnapshot.EnabledAtLogin())
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if r.idleApplying && (strings.HasPrefix(n.Action, "pick:idle.after=") || n.Action == "set:idle.delay") {
			n.State |= ui.StateDisabled
			n.AriaDisabled = true
			n.Focusable = false
		}
		switch n.Action {
		case "pick:idle.after=lock":
			if locker == "" {
				n.State |= ui.StateDisabled
				n.AriaDisabled = true
				n.Focusable = false
			}
		case "pick:idle.after=screensaver":
			if !r.wallsSnapshot.ServiceAvailable {
				n.State |= ui.StateDisabled
				n.AriaDisabled = true
				n.Focusable = false
			}
		case "set:idle.delay":
			if mode == "nothing" {
				n.State |= ui.StateDisabled
				n.AriaDisabled = true
				n.Focusable = false
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

func (h *PanelHost) applyIdleSetting(r *Registry, path, value string) {
	if r == nil || h == nil {
		return
	}
	mode := settings.WhenIdleMode(h.draft.Idle.Lock, r.wallsSnapshot.EnabledAtLogin())
	delay := settings.WhenIdleDelay(h.draft.Idle.Lock, r.wallsSnapshot.Timeout)
	switch path {
	case "idle.after":
		mode = value
	case "idle.delay":
		tmp := h.draft
		e := h.set.ByPath("idle.delay")
		if e == nil || e.Set == nil {
			return
		}
		if err := e.Set(&tmp, value); err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return
		}
		delay = tmp.Idle.Lock
	default:
		return
	}
	if r.idleApplying || r.lockerAcquired || r.wallsSnapshot.ActionPending {
		h.errLabel = "Wait for the current screensaver action to finish."
		r.rebuildPanel(h)
		return
	}
	service := r.wallsService
	var controller settings.IdleWalls
	if service != nil && !(r.wallsSnapshot.UnitKnown && !r.wallsSnapshot.UnitStale && r.wallsSnapshot.LoadState == "not-found") {
		controller = idleWallsAdapter{service: service}
	}
	before := h.draft
	previous := r.wallsSnapshot
	candidate := before
	staged := mode == "screensaver" && before.Idle.Lock > 0
	if staged {
		// Disarm and persist the lock before the screensaver can start.
		safe := before
		safe.Idle.Lock = 0
		if err := r.writeConfig(safe); err != nil {
			h.errLabel = err.Error()
			r.rebuildPanel(h)
			return
		}
		r.cfg.Idle.Lock = 0
		h.draft.Idle.Lock = 0
		r.pushIdleInputsLocked()
	}
	r.idleApplying = true
	r.rebuildPanel(h)
	go func() {
		err := settings.ApplyWhenIdle(mode, delay, &candidate, controller)
		r.mu.Lock()
		defer r.mu.Unlock()
		select {
		case <-r.closed:
			return
		default:
		}
		// A reopened Settings owns the current draft; the old window must not overwrite it.
		if current := r.panelHosts[PanelSettings]; current != nil {
			h = current
		} else {
			h.draft = r.cfg
		}
		r.idleApplying = false
		if service != nil {
			r.wallsSnapshot = service.Snapshot()
		}
		if err == nil {
			current := h.draft
			current.Idle.Lock = candidate.Idle.Lock
			err = r.writeConfig(current)
			if err == nil {
				h.draft = current
				r.cfg.Idle.Lock = candidate.Idle.Lock
				r.pushIdleInputsLocked()
			} else if service != nil {
				// Restore the previous unit policy after a failed config write, off the owner.
				r.idleApplying = true
				go func(cause error) {
					rollback := service.ConfigureIdle(previous.EnabledAtLogin(), previous.Timeout)
					r.mu.Lock()
					defer r.mu.Unlock()
					select {
					case <-r.closed:
						return
					default:
					}
					if current := r.panelHosts[PanelSettings]; current != nil {
						h = current
					} else {
						h.draft = r.cfg
					}
					r.idleApplying = false
					r.wallsSnapshot = service.Snapshot()
					if rollback == nil && staged {
						current := h.draft
						current.Idle.Lock = before.Idle.Lock
						rollback = r.writeConfig(current)
						if rollback == nil {
							h.draft = current
							r.cfg.Idle.Lock = before.Idle.Lock
							r.pushIdleInputsLocked()
						}
					}
					h.errLabel = errors.Join(cause, rollback).Error()
					if r.panelHosts[h.id] == h {
						r.rebuildPanel(h)
						r.publishSurface(h.output, panelSurfaceID(h.id))
					}
				}(err)
			}
		} else if staged {
			// Rearm the old lock only when the screensaver is confirmed disabled.
			snap := r.wallsSnapshot
			if controller == nil || (snap.UnitKnown && !snap.UnitStale && snap.UnitFileState == "disabled" && !snap.Running()) {
				current := h.draft
				current.Idle.Lock = before.Idle.Lock
				rollback := r.writeConfig(current)
				err = errors.Join(err, rollback)
				if rollback == nil {
					h.draft = current
					r.cfg.Idle.Lock = before.Idle.Lock
					r.pushIdleInputsLocked()
				}
			} else {
				err = errors.Join(err, fmt.Errorf("lock remains disabled until screensaver shutdown is confirmed"))
			}
		}
		if err != nil {
			h.errLabel = err.Error()
		} else {
			h.errLabel = ""
			delete(h.fields, "idle.delay")
		}
		h.set = r.settingsForLocked(h.draft)
		if r.panelHosts[h.id] == h {
			r.rebuildPanel(h)
			r.publishSurface(h.output, panelSurfaceID(h.id))
		}
	}()
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
	r.configWriteMu.Lock()
	defer r.configWriteMu.Unlock()
	return r.writeConfigLocked(c)
}

// writeConfigLocked is writeConfig while configWriteMu is held by a config
// transaction or writeConfig itself.
func (r *Registry) writeConfigLocked(c config.Config) error {
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

// updateConfig applies one persisted edit against the latest file contents.
// The same mutex covers the read, mutation and write, and writeConfig uses it
// too so other in-process whole-config writes cannot land in the middle.
func (r *Registry) updateConfig(update func(*config.Config, func(config.Config) *settings.Registry) error) (config.Config, error) {
	r.mu.Lock()
	path, cfg := r.configPath, r.cfg
	palettes := append([]theme.PaletteInfo(nil), r.palettes...)
	r.mu.Unlock()

	r.configWriteMu.Lock()
	defer r.configWriteMu.Unlock()
	if path != "" {
		if loaded, err := config.Load(path); err == nil {
			cfg = loaded
		}
	}
	cfg.Templates = maps.Clone(cfg.Templates)
	registryFor := func(cfg config.Config) *settings.Registry {
		return settings.DefaultFor(cfg, settings.WithCustomPalettes(settings.CustomPalettesFrom(palettes)))
	}
	if err := update(&cfg, registryFor); err != nil {
		return config.Config{}, err
	}
	if err := r.writeConfigLocked(cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

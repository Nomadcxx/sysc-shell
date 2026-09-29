package shell

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// pluginsTree is Settings -> Plugins (store design U4): an Installed and
// Sources segment, Browse plugins, and the chosen page as settings group cards.
func pluginsTree(r *Registry, h *PanelHost) *ui.Node {
	if h.pluginManagerTab == "" {
		h.pluginManagerTab = "installed"
	}
	metrics := h.metrics()
	segments := &ui.Node{
		Kind: ui.KindSegmented, Key: "plugins-tab", Gap: theme.MarginXXS,
		Height: metrics.CompactControl, Name: "Plugins view", Role: "tablist",
		Children: []*ui.Node{
			pluginManagerSegment(h, "installed", "Installed"),
			pluginManagerSegment(h, "sources", "Sources"),
		},
	}
	browse := pluginManagerButton("plugins-browse", "Browse plugins", metrics)
	browse.Fill = ui.FillAccent
	browse.Children = []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "search", IconSize: metrics.IconSmall},
		{Kind: ui.KindText, Text: "Browse plugins"},
	}}}
	browse.Text = ""
	// The body scrolls; its bar keeps a lane at the right edge.
	top := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: max(settingsBodyWidth(h)-theme.MarginM, 1), Height: metrics.StandardControl, Children: []*ui.Node{segments, browse}}
	children := []*ui.Node{top}
	switch {
	case r == nil:
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "Plugin store unavailable", TextRole: theme.RoleCaption})
	case h.pluginManagerTab == "sources":
		children = append(children, pluginManagerSourcesTree(r, h, metrics)...)
	default:
		children = append(children, pluginManagerInstalledTree(r, h, metrics)...)
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
}

func pluginDirectoryLabel(r *Registry) string {
	if r == nil || r.plugins == nil || len(r.plugins.opts.Roots) == 0 {
		return "Plugin directory"
	}
	var parts []string
	for _, root := range r.plugins.opts.Roots {
		parts = append(parts, root.Path)
	}
	return strings.Join(parts, " · ")
}

// pluginPanelSettingGroups is the recorder panel layout from the design.
// Keys absent from a plugin's schema are skipped; headings omit empty groups.
var pluginPanelSettingGroups = []struct {
	Title string
	Keys  []string
}{
	{"Capture", []string{"video_source", "show_cursor", "resolution", "frame_rate"}},
	{"File", []string{"directory", "filename_pattern"}},
	{"Video", []string{"video_codec", "video_qp", "color_range"}},
	{"Audio", []string{"audio_source", "audio_codec", "audio_bitrate"}},
	{"Replay", []string{"replay_enabled", "replay_duration", "replay_filename_pattern", "replay_storage"}},
	{"Bar", []string{"hide_inactive"}},
}

func pluginPanelSettings(r *Registry, h *PanelHost, pluginID string, schema []plugin.Setting) []*ui.Node {
	byKey := make(map[string]plugin.Setting, len(schema))
	for _, s := range schema {
		byKey[s.Key] = s
	}
	values := pluginSettingValues(r, pluginID, schema)
	var out []*ui.Node
	grouped := make(map[string]struct{}, len(schema))
	for _, g := range pluginPanelSettingGroups {
		var rows []*ui.Node
		for _, key := range g.Keys {
			s, ok := byKey[key]
			if !ok {
				continue
			}
			grouped[key] = struct{}{}
			if !plugin.SettingVisible(s, values) {
				continue
			}
			rows = append(rows, pluginSettingRow(r, h, pluginID, s))
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, monitorCard(h.metrics(), append([]*ui.Node{monitorCardTitle(g.Title, 0)}, rows...)))
	}
	var rows []*ui.Node
	for _, s := range schema {
		if _, ok := grouped[s.Key]; ok || !plugin.SettingVisible(s, values) {
			continue
		}
		rows = append(rows, pluginSettingRow(r, h, pluginID, s))
	}
	if len(rows) != 0 {
		out = append(out, monitorCard(h.metrics(), append([]*ui.Node{monitorCardTitle("Settings", 0)}, rows...)))
	}
	return out
}

func pluginSettingValues(r *Registry, pluginID string, schema []plugin.Setting) map[string]any {
	out := make(map[string]any, len(schema))
	for _, s := range schema {
		if s.Default != nil {
			out[s.Key] = s.Default
		}
	}
	if r == nil || r.cfg.Plugins.Settings == nil {
		return out
	}
	if m := r.cfg.Plugins.Settings[pluginID]; m != nil {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// pluginSettingStoreKey is pluginID+"."+key for menus/fields shared by Settings
// and the recorder panel. action is "plugin-set:"+pluginID+":"+key.
func pluginSettingStoreKey(action string) string {
	rest, ok := strings.CutPrefix(action, "plugin-set:")
	if !ok {
		return ""
	}
	pluginID, key, ok := strings.Cut(rest, ":")
	if !ok || pluginID == "" || key == "" {
		return ""
	}
	return pluginID + "." + key
}

func pluginSettingRow(r *Registry, h *PanelHost, pluginID string, s plugin.Setting) *ui.Node {
	raw := ""
	if r != nil && r.cfg.Plugins.Settings != nil {
		if m := r.cfg.Plugins.Settings[pluginID]; m != nil {
			if v, ok := m[s.Key]; ok {
				raw = fmt.Sprint(v)
			}
		}
	}
	if raw == "" && s.Default != nil {
		raw = fmt.Sprint(s.Default)
	}
	action := "plugin-set:" + pluginID + ":" + s.Key
	store := pluginID + "." + s.Key
	control := pluginSettingControl(h, s, raw, action, store)
	rowWidth := pluginSettingRowWidth(h)
	controlWidth := rowWidth * 2 / 5
	if controlWidth > rowWidth/2 {
		controlWidth = rowWidth / 2
	}
	fills := s.Type != plugin.SettingBool && s.Type != plugin.SettingSelect
	if fills && controlWidth > 0 {
		control.Width = controlWidth
	}
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: s.Label, Name: s.Label},
	}}
	if s.Type == plugin.SettingBool {
		label.Children[0].Action = action
		label.Children[0].Focusable = true
		label.Children[0].Role = "checkbox"
	}
	if s.Description != "" {
		label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: s.Description, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if rowWidth > 0 {
		label.Width = max(rowWidth-controlWidth-theme.MarginL, 0)
	}
	trailing := &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{control}}
	if fills && controlWidth > 0 {
		trailing.Width = controlWidth
	}
	row := &ui.Node{Kind: ui.KindRow, PinEnd: true, Gap: theme.MarginL, Children: []*ui.Node{label, trailing}}
	if s.Description == "" {
		row.Height = h.metrics().StandardControl
	}
	return row
}

func pluginSettingRowWidth(h *PanelHost) int {
	if h == nil {
		return 0
	}
	if h.id == PanelSettings {
		return settingsBodyWidth(h)
	}
	if h.place.Panel.W > 0 {
		return max(h.place.Panel.W-2*h.metrics().PanelPadding, 0)
	}
	return 0
}

func pluginSettingControl(h *PanelHost, s plugin.Setting, raw, action, store string) *ui.Node {
	switch s.Type {
	case plugin.SettingBool:
		v := 0.0
		if raw == "true" {
			v = 1
		}
		return &ui.Node{
			Kind: ui.KindToggle, Value: v, Action: action,
			Focusable: true, Name: s.Label, Role: "checkbox",
		}
	case plugin.SettingInt:
		n, _ := strconv.Atoi(raw)
		min, max := 0.0, 100.0
		if s.Min != nil {
			min = *s.Min
		}
		if s.Max != nil {
			max = *s.Max
		}
		return &ui.Node{
			Kind: ui.KindSlider, Value: float64(n), Min: min, Max: max, Step: 1,
			Action: action, Width: 160, Focusable: true, Name: s.Label, Role: "slider", // token-exempt: a plugin slider's track width, a measured control dimension rather than a ladder value
		}
	case plugin.SettingSelect:
		labels := make([]string, 0, len(s.Options))
		values := make([]string, 0, len(s.Options))
		for _, o := range s.Options {
			label := o.Label
			if label == "" {
				label = o.Value
			}
			labels = append(labels, label)
			values = append(values, o.Value)
		}
		idx := 0
		for i, v := range values {
			if v == raw {
				idx = i
				break
			}
		}
		if h == nil {
			m := NewMenu(labels, idx)
			m.values = values
			n := m.Node()
			n.Action = action
			n.Name = s.Label
			return n
		}
		if h.menus == nil {
			h.menus = map[string]*Menu{}
		}
		m := h.menus[store]
		if m == nil || !m.Opened() {
			m = NewMenu(labels, idx)
			m.values = values
			h.menus[store] = m
		}
		n := m.Node()
		n.Action = action
		n.Name = s.Label
		return n
	default:
		if h == nil {
			n := ui.NewField(raw).Node(s.Label)
			n.Action = action
			n.Width = 200
			return n
		}
		if h.fields == nil {
			h.fields = map[string]*ui.Field{}
		}
		f := h.fields[store]
		if f == nil {
			f = ui.NewField(raw)
			h.fields[store] = f
		}
		n := f.Node(s.Label)
		n.Action = action
		n.Width = 200
		return n
	}
}

func pluginSettingValueFromNode(h *PanelHost, n *ui.Node) (any, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Kind {
	case ui.KindToggle:
		return n.Value != 0, true
	case ui.KindSlider:
		return int(n.Value), true
	case ui.KindMenu:
		if n.Text != "" {
			return n.Text, true
		}
		if store := pluginSettingStoreKey(n.Action); store != "" && h != nil {
			if m := h.menus[store]; m != nil {
				return m.Value(), true
			}
		}
		return nil, false
	case ui.KindTextField:
		return n.Text, true
	default:
		return nil, false
	}
}

type pluginManagerRow struct {
	id        string
	candidate *plugin.Candidate
	listing   *store.Listing
}

func pluginManagerSegment(h *PanelHost, tab, label string) *ui.Node {
	m := h.metrics()
	n := &ui.Node{
		Kind: ui.KindButton, Action: "plugins-tab:" + tab, Name: label, Role: "tab", Focusable: true,
		Height: m.CompactControl, Padding: m.ButtonPadding, Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
	}
	if h.pluginManagerTab == tab {
		n.State |= ui.StateSelected
	}
	return n
}

// pluginManagerButton is a text action sized from the density ladder, padded
// so its label never meets the pill's edge at any scale.
func pluginManagerButton(action, label string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Text: label, Action: action, Key: action, Name: label, Role: "button", Focusable: true,
		Height: metrics.CompactControl, Padding: metrics.ButtonPadding, Fill: ui.FillOutline, Shape: ui.ShapeMedium,
	}
}

// pluginManagerIconButton is a row action drawn as its symbol, the way the
// references show open, settings and remove beside a plugin's switch.
func pluginManagerIconButton(action, name, icon string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Key: action, Name: name, Role: "button", Focusable: true,
		Width: metrics.IconButton, Height: metrics.IconButton, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: icon, IconSize: metrics.IconSmall}},
	}
}

func pluginManagerInstalledTree(r *Registry, h *PanelHost, metrics theme.Metrics) []*ui.Node {
	inner := settingsCardInner(h)
	updates := pluginManagerUpdates(r.pluginStoreSnapshot.Listings)
	header := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Height: metrics.CompactControl, Children: []*ui.Node{
		{Kind: ui.KindText, Text: fmt.Sprintf("Updates (%d)", len(updates)), TextRole: theme.RoleLabel},
	}}
	if len(updates) > 0 {
		header.Children = append(header.Children, pluginManagerButton("plugins-update-all", "Update all", metrics))
	}
	var cards []*ui.Node
	lead := []*ui.Node{header}
	if h.pluginManagerError != "" {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: h.pluginManagerError, Tone: ui.ToneError, MaxWidth: inner, Multiline: true})
	}
	if len(h.pluginUpdateAllNeedsConsent) > 0 {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "These updates need review", TextRole: theme.RoleLabel})
		for _, key := range h.pluginUpdateAllNeedsConsent {
			listing, ok := pluginStoreFind(r.pluginStoreSnapshot.Listings, key)
			if !ok {
				continue
			}
			name := listing.Entry.Name
			if name == "" {
				name = listing.Entry.ID
			}
			lead = append(lead, &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Height: metrics.CompactControl, Children: []*ui.Node{
				{Kind: ui.KindText, Text: name},
				pluginManagerButton("plugins-review:"+key, "Review", metrics),
			}})
		}
	}
	cards = append(cards, settingsGroupCard(h, "", lead))
	rows := pluginManagerRows(r)
	var installed []*ui.Node
	if len(rows) == 0 {
		installed = append(installed, &ui.Node{Kind: ui.KindText, Text: "No installed plugins", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	for i, row := range rows {
		if i > 0 {
			installed = append(installed, &ui.Node{Kind: ui.KindSeparator, Width: inner})
		}
		installed = append(installed, pluginManagerInstalledRow(r, h, row, inner, metrics))
	}
	return append(cards, settingsGroupCard(h, "Installed", installed))
}

func pluginManagerUpdates(listings []store.Listing) []store.Listing {
	seen := map[string]bool{}
	var out []store.Listing
	for _, listing := range listings {
		id := listing.Entry.ID
		if listing.UpdateAvailable && listing.Installed != nil && id != "" && !seen[id] {
			seen[id] = true
			out = append(out, listing)
		}
	}
	return out
}

func pluginManagerRows(r *Registry) []pluginManagerRow {
	byID := map[string]pluginManagerRow{}
	if r.plugins != nil {
		for _, candidate := range r.plugins.discovered().Plugins {
			id := candidate.Manifest.ID
			if id == "" {
				id = candidate.Dir
			}
			copy := candidate
			row := byID[id]
			row.id, row.candidate = id, &copy
			byID[id] = row
		}
	}
	for i := range r.pluginStoreSnapshot.Listings {
		listing := r.pluginStoreSnapshot.Listings[i]
		if listing.Installed == nil && listing.LocalDir == "" {
			continue
		}
		id := listing.Entry.ID
		row := byID[id]
		copy := listing
		row.id, row.listing = id, &copy
		byID[id] = row
	}
	rows := make([]pluginManagerRow, 0, len(byID))
	for _, row := range byID {
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b pluginManagerRow) int {
		an, bn := pluginManagerName(a), pluginManagerName(b)
		if order := strings.Compare(strings.ToLower(an), strings.ToLower(bn)); order != 0 {
			return order
		}
		return strings.Compare(a.id, b.id)
	})
	return rows
}

func pluginManagerName(row pluginManagerRow) string {
	if row.candidate != nil && row.candidate.Manifest.Name != "" {
		return row.candidate.Manifest.Name
	}
	if row.listing != nil && row.listing.Entry.Name != "" {
		return row.listing.Entry.Name
	}
	return row.id
}

// pluginManagerInstalledRow is one plugin in the settings row anatomy: its
// icon and label column (name, source badge, version, description, Requires,
// provenance) with the row's actions and switch pinned to the end.
func pluginManagerInstalledRow(r *Registry, h *PanelHost, row pluginManagerRow, width int, metrics theme.Metrics) *ui.Node {
	id, name := row.id, pluginManagerName(row)
	version, description, source, provenance := "", "", "local", ""
	var required []string
	if row.candidate != nil {
		version = row.candidate.Manifest.Version
		description = row.candidate.Manifest.Description
		required = row.candidate.Manifest.Requires
		source = string(row.candidate.Source)
	}
	listing := row.listing
	if listing != nil {
		if listing.Installed != nil {
			version = listing.Installed.Version
			source = listing.Installed.Source
		}
		if description == "" {
			description = listing.Entry.Description
		}
		if len(required) == 0 && listing.Resolution.Release != nil {
			required = listing.Resolution.Release.Requires.Commands
		}
		switch {
		case listing.LocalDir != "":
			provenance = "local override"
		case listing.Status == store.StatusUnlisted:
			provenance = "managed, unlisted"
		case listing.Installed != nil:
			provenance = "managed install"
		}
	}
	// A plugin in the user root already reads "Local" on its badge.
	if source == "" {
		source = "local"
	}
	badge := pluginSourceLabel(source)
	versionText := version
	if versionText != "" {
		versionText = "v" + versionText
	}

	// Trailing actions: update and roll back as labelled buttons, because the
	// version is the information; the rest as symbols beside the switch.
	var actions []*ui.Node
	if listing != nil && listing.UpdateAvailable {
		release := listing.Entry.Version
		if listing.Resolution.Release != nil {
			release = listing.Resolution.Release.Version
		}
		actions = append(actions, pluginManagerButton("plugins-update:"+id, "Update to v"+release, metrics))
	}
	if listing != nil && listing.Installed != nil && listing.Installed.Previous != nil {
		actions = append(actions, pluginManagerButton("plugins-rollback:"+id, "Roll back to v"+listing.Installed.Previous.Version, metrics))
	}
	var status plugin.Status
	if row.candidate != nil && r.plugins != nil {
		status = r.plugins.status(id)
	}
	failing := row.candidate != nil && (row.candidate.Err != nil || status.Failure != "" ||
		status.State == plugin.StateFailed || status.State == plugin.StateDegraded || status.State == plugin.StateIncompatible)
	if failing {
		actions = append(actions, pluginManagerIconButton("plugin-retry:"+id, "Retry "+name, "refresh", metrics))
	}
	if row.candidate != nil && len(row.candidate.Manifest.Settings) > 0 {
		label := "Plugin settings"
		if h.pluginManagerExpanded[id] {
			label = "Hide settings"
		}
		actions = append(actions, pluginManagerIconButton("plugins-settings:"+id, label, "settings", metrics))
	}
	if listing != nil && listing.Installed != nil {
		actions = append(actions, pluginManagerIconButton("plugins-remove:"+id, "Remove "+name, "delete", metrics))
	}
	canToggle := listing != nil && listing.Installed != nil
	if row.candidate != nil && row.candidate.Err == nil {
		canToggle = true
	}
	if canToggle {
		value := 0.0
		if slices.Contains(r.cfg.Plugins.Enabled, id) {
			value = 1
		}
		actions = append(actions, &ui.Node{Kind: ui.KindToggle, Value: value, Action: "plugin-enable:" + id,
			Name: "Enable " + name, Role: "switch", Focusable: true})
	}
	trailing := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginXS, Height: metrics.CompactControl, Children: actions}
	trailingW := 0
	for _, a := range actions {
		switch {
		case a.Kind == ui.KindToggle:
			trailingW += metrics.StandardControl * 2
		case a.Width > 0:
			trailingW += a.Width
		default:
			trailingW += settingsControlWidth(h) / 2
		}
	}
	trailingW += theme.MarginXS * max(len(actions)-1, 0)
	labelW := max(width-metrics.IconNormal-theme.MarginM-min(trailingW, width/2)-theme.MarginL, 1)

	heading := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
		pluginSourceBadge(badge, metrics),
		{Kind: ui.KindText, Text: versionText, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}}
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelW, Children: []*ui.Node{heading}}
	if description != "" {
		label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: description, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: labelW, Multiline: true})
	}
	if len(required) > 0 {
		label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: "Requires: " + strings.Join(required, ", "), TextRole: theme.RoleCaption, MaxWidth: labelW})
	}
	if provenance != "" {
		label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: provenance, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if failing {
		detail := status.Failure
		if row.candidate.Err != nil {
			detail = row.candidate.Err.Error()
		}
		if detail != "" {
			label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: detail, TextRole: theme.RoleCaption, Tone: ui.ToneError, MaxWidth: labelW, Multiline: true})
		}
		if tail := pluginStderrTail(status.Stderr, 3); tail != "" {
			label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: tail, TextRole: theme.RoleCaption, Tone: ui.ToneError, MaxWidth: labelW, Multiline: true})
		}
	}
	if listing != nil && listing.Err != nil {
		label.Children = append(label.Children, &ui.Node{Kind: ui.KindText, Text: listing.Err.Error(), TextRole: theme.RoleCaption, Tone: ui.ToneError, MaxWidth: labelW, Multiline: true})
	}
	lead := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "extension", IconSize: metrics.IconNormal},
		label,
	}}
	children := []*ui.Node{{Kind: ui.KindRow, PinEnd: true, Width: width, Children: []*ui.Node{lead, trailing}}}
	if listing != nil && listing.Installed != nil && h.pluginManagerRemoveConfirm == id {
		children = append(children, &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: width, Height: metrics.CompactControl, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Remove " + name + "? Its settings and enabled state are kept.", TextRole: theme.RoleCaption},
			{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
				pluginManagerButton("plugins-remove-cancel", "Cancel", metrics),
				pluginManagerDestructive("plugins-remove-confirm:"+id, "Remove", metrics),
			}},
		}})
	}
	if row.candidate != nil && h.pluginManagerExpanded[id] {
		values := pluginSettingValues(r, id, row.candidate.Manifest.Settings)
		for _, setting := range row.candidate.Manifest.Settings {
			if plugin.SettingVisible(setting, values) {
				children = append(children, pluginSettingRow(r, h, id, setting))
			}
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: children}
}

// pluginStderrTail is the last lines a failing plugin wrote, so a noisy
// process cannot push the rest of the page out of reach.
func pluginStderrTail(stderr []byte, lines int) string {
	text := strings.TrimRight(string(stderr), "\n")
	if text == "" {
		return ""
	}
	all := strings.Split(text, "\n")
	return strings.Join(all[max(len(all)-lines, 0):], "\n")
}

func pluginManagerDestructive(action, label string, metrics theme.Metrics) *ui.Node {
	n := pluginManagerButton(action, label, metrics)
	n.Fill = ui.FillErrorContainer
	return n
}

// pluginSourceLabel names a source the way both the store and Settings do.
func pluginSourceLabel(source string) string {
	switch source {
	case "sysc":
		return "Official"
	case "community":
		return "Community"
	case "", "user":
		return "Local"
	}
	return source
}

func pluginSourceBadge(label string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillSoft, Shape: ui.ShapeSmall, Padding: theme.MarginXS,
		Children: []*ui.Node{{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption}}}
}

func pluginManagerSourcesTree(r *Registry, h *PanelHost, metrics theme.Metrics) []*ui.Node {
	inner := settingsCardInner(h)
	stateByName := make(map[string]store.SourceState, len(r.pluginStoreSnapshot.Sources))
	for _, state := range r.pluginStoreSnapshot.Sources {
		stateByName[state.Name] = state
	}
	sources := r.cfg.Plugins.EffectiveSources()
	for _, state := range r.pluginStoreSnapshot.Sources {
		if !slices.ContainsFunc(sources, func(source config.PluginSource) bool { return source.Name == state.Name }) {
			sources = append(sources, config.PluginSource{Name: state.Name, URL: state.URL})
		}
	}
	rows := []*ui.Node{{Kind: ui.KindText, Text: "Lower sources override higher ones when they offer the same plugin.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: inner, Multiline: true}}
	for _, source := range sources {
		rows = append(rows, pluginManagerSourceRow(h, source, stateByName[source.Name], inner, metrics))
	}
	if h.pluginManagerError != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: h.pluginManagerError, Tone: ui.ToneError, MaxWidth: inner, Multiline: true})
	}
	rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "The same plugin offered by two sources appears once per source.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: inner, Multiline: true})

	fieldW := max((inner-theme.MarginS*2-settingsControlWidth(h)/2)/2, 1)
	name := pluginManagerSourceField(h, "plugins-source-name", "Source name")
	name.Width = fieldW
	url := pluginManagerSourceField(h, "plugins-source-url", "Repository URL")
	url.Width = fieldW
	add := []*ui.Node{
		{Kind: ui.KindText, Text: "Any git repository with a catalog.json at its root.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		{Kind: ui.KindRow, PinEnd: true, Width: inner, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{name, url}},
			pluginManagerButton("plugins-source-add", "Add source", metrics),
		}},
	}
	if h.pluginManagerSourceWarning {
		add = append(add, &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillContainerHighest, Shape: ui.ShapeMedium, Padding: metrics.CardPadding, Width: inner, Children: []*ui.Node{
			{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "Review source permissions", TextRole: theme.RoleLabel, Tone: ui.ToneError},
				{Kind: ui.KindText, Text: "Plugins from this source run as your user with full file and network access.", MaxWidth: inner - 2*metrics.CardPadding, Multiline: true},
				{Kind: ui.KindRow, PinEnd: true, Width: inner - 2*metrics.CardPadding, Height: metrics.CompactControl, Children: []*ui.Node{
					{Kind: ui.KindText, Text: ""},
					{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
						pluginManagerButton("plugins-source-cancel", "Cancel", metrics),
						pluginManagerDestructive("plugins-source-confirm", "Confirm", metrics),
					}},
				}},
			}},
		}})
	}

	suggested := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Height: metrics.StandardControl, Children: []*ui.Node{
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Community catalog", Tone: ui.ToneSubtle},
			{Kind: ui.KindText, Text: "Third-party plugins reviewed by pull request", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		}},
		{Kind: ui.KindText, Text: "Not published yet", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}}
	local := &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: inner, Height: metrics.StandardControl, Children: []*ui.Node{
		{Kind: ui.KindText, Text: pluginDirectoryLabel(r), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		pluginManagerButton("plugin-rescan", "Rescan", metrics),
	}}
	return []*ui.Node{
		settingsGroupCard(h, "Sources", rows),
		settingsGroupCard(h, "Add source", add),
		settingsGroupCard(h, "Suggested", []*ui.Node{suggested}),
		settingsGroupCard(h, "Local plugin directory", []*ui.Node{local}),
	}
}

func pluginManagerSourceField(h *PanelHost, action, label string) *ui.Node {
	if h.fields == nil {
		h.fields = map[string]*ui.Field{}
	}
	field := h.fields[action]
	if field == nil {
		field = ui.NewField("")
		h.fields[action] = field
	}
	n := field.Node(label)
	n.Action = action
	n.Width = settingsControlWidth(h)
	return n
}

func pluginManagerSourceRow(h *PanelHost, source config.PluginSource, state store.SourceState, width int, metrics theme.Metrics) *ui.Node {
	name := pluginSourceLabel(source.Name)
	kind := "Git"
	if strings.HasPrefix(source.URL, "file:") {
		kind = "Path"
	}
	status := "Not fetched yet"
	tone := ui.ToneSubtle
	if !state.FetchedAt.IsZero() {
		status = fmt.Sprintf("%d plugins · last fetched %s", state.Plugins, state.FetchedAt.Local().Format("2006-01-02 15:04"))
	}
	if state.Err != nil {
		tone = ui.ToneError
		if state.FetchedAt.IsZero() {
			status = "Error: " + state.Err.Error()
		} else {
			status = fmt.Sprintf("%d plugins · Stale: %s", state.Plugins, state.Err.Error())
		}
	}
	value := 0.0
	if source.Enabled {
		value = 1
	}
	actions := []*ui.Node{pluginManagerIconButton("plugins-source-refresh:"+source.Name, "Refresh "+name, "refresh", metrics)}
	if source.Name != "sysc" {
		actions = append(actions, pluginManagerIconButton("plugins-source-remove:"+source.Name, "Remove "+name, "delete", metrics))
	}
	actions = append(actions, &ui.Node{Kind: ui.KindToggle, Value: value, Action: "plugins-source-toggle:" + source.Name,
		Name: "Enable " + name, Role: "switch", Focusable: true})
	labelW := max(width-3*metrics.IconButton-metrics.StandardControl*2-theme.MarginL, 1)
	return &ui.Node{Kind: ui.KindRow, PinEnd: true, Width: width, Children: []*ui.Node{
		{Kind: ui.KindColumn, Gap: theme.MarginXXS, Width: labelW, Children: []*ui.Node{
			{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: kind + " · " + source.URL, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, MaxWidth: labelW},
			{Kind: ui.KindText, Text: status, TextRole: theme.RoleCaption, Tone: tone, MaxWidth: labelW},
		}},
		{Kind: ui.KindRow, Gap: theme.MarginXS, Height: metrics.CompactControl, Children: actions},
	}}
}

func (r *Registry) handlePluginManager(h *PanelHost, n *ui.Node) bool {
	if n == nil {
		return false
	}
	action := n.Action
	switch action {
	case "plugins-tab:installed":
		h.pluginManagerTab = "installed"
		h.pluginManagerError = ""
		r.rebuildPanel(h)
		return true
	case "plugins-tab:sources":
		h.pluginManagerTab = "sources"
		h.pluginManagerError = ""
		r.rebuildPanel(h)
		return true
	case "plugins-browse":
		trig := Trigger{BarEdge: h.place.BarEdge, BarZone: h.place.BarZone, OutW: h.place.Output.W, OutH: h.place.Output.H}
		if err := r.openPanelRootLocked(PanelPluginStore, h.output, trig); err == nil {
			if storeHost := r.panelHosts[PanelPluginStore]; storeHost != nil && h.pluginStoreReviewKey != "" {
				storeHost.pluginStoreSelected = h.pluginStoreReviewKey
				storeHost.pluginStoreDetail = h.pluginStoreReviewKey
				r.rebuildPanel(storeHost)
			}
		}
		return true
	case "plugins-source-add":
		h.pluginManagerSourceWarning = true
		h.pluginManagerError = ""
		r.rebuildPanel(h)
		return true
	case "plugins-source-confirm":
		r.confirmPluginSourceLocked(h)
		return true
	case "plugins-source-cancel":
		h.pluginManagerSourceWarning = false
		h.pluginManagerError = ""
		r.rebuildPanel(h)
		return true
	case "plugins-update-all":
		r.updateAllPluginsLocked(h)
		return true
	case "plugins-remove-cancel":
		h.pluginManagerRemoveConfirm = ""
		r.rebuildPanel(h)
		return true
	case "plugin-rescan":
		if r.plugins != nil {
			if err := r.plugins.syncEnabledLocked(); err != nil {
				h.pluginManagerError = err.Error()
			}
		}
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-source-toggle:"); ok {
		enabled := false
		for _, source := range r.cfg.Plugins.EffectiveSources() {
			if source.Name == id {
				enabled = source.Enabled
				break
			}
		}
		r.setPluginSourceEnabledLocked(h, id, !enabled)
		return true
	}
	if _, ok := strings.CutPrefix(action, "plugins-source-refresh:"); ok {
		r.refreshPluginStoreLocked(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-source-remove:"); ok {
		r.removePluginSourceLocked(h, id)
		return true
	}
	if key, ok := strings.CutPrefix(action, "plugins-review:"); ok {
		if _, found := pluginStoreFind(r.pluginStoreSnapshot.Listings, key); found {
			h.pluginStoreReviewKey = key
			r.handlePluginManager(h, &ui.Node{Action: "plugins-browse"})
		} else {
			h.pluginManagerError = "plugin listing is no longer available"
			r.rebuildPanel(h)
		}
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-update:"); ok {
		r.updatePluginLocked(h, id)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-rollback:"); ok {
		if r.pluginStore == nil {
			h.pluginManagerError = "plugin store unavailable"
		} else if _, err := r.pluginStore.Rollback(id); err != nil {
			h.pluginManagerError = err.Error()
		}
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-remove:"); ok {
		h.pluginManagerRemoveConfirm = id
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-remove-confirm:"); ok {
		if r.pluginStore == nil {
			h.pluginManagerError = "plugin store unavailable"
		} else if _, err := r.pluginStore.Remove(id); err != nil {
			h.pluginManagerError = err.Error()
		}
		h.pluginManagerRemoveConfirm = ""
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugins-settings:"); ok {
		if h.pluginManagerExpanded == nil {
			h.pluginManagerExpanded = map[string]bool{}
		}
		h.pluginManagerExpanded[id] = !h.pluginManagerExpanded[id]
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugin-retry:"); ok {
		if r.plugins == nil {
			return false
		}
		if err := r.plugins.retryLocked(id); err != nil {
			h.pluginManagerError = err.Error()
		}
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugin-enable:"); ok {
		on := !slices.Contains(r.cfg.Plugins.Enabled, id)
		var err error
		if r.plugins != nil {
			err = r.plugins.enableLocked(id, on)
		} else {
			next := r.cfg
			next.Plugins = next.Plugins.Clone()
			enabled := make([]string, 0, len(next.Plugins.Enabled)+1)
			for _, have := range next.Plugins.Enabled {
				if have != id {
					enabled = append(enabled, have)
				}
			}
			if on {
				enabled = append(enabled, id)
			}
			next.Plugins.Enabled = enabled
			if err = r.writeConfig(next); err == nil {
				r.cfg = next
			}
		}
		if err != nil {
			h.pluginManagerError = err.Error()
		}
		r.rebuildPanel(h)
		return true
	}
	if r.plugins == nil {
		return false
	}
	if strings.HasPrefix(action, "plugin-set:") {
		pluginID, key, ok := strings.Cut(strings.TrimPrefix(action, "plugin-set:"), ":")
		if !ok {
			return false
		}
		value, ok := pluginSettingValueFromNode(h, n)
		if !ok {
			return false
		}
		if err := r.plugins.applySettingLocked(pluginID, key, value); err != nil {
			h.pluginManagerError = err.Error()
		}
		r.rebuildPanel(h)
		return true
	}
	return false
}

func (r *Registry) updatePluginLocked(h *PanelHost, id string) {
	if r.pluginStore == nil {
		h.pluginManagerError = "plugin store unavailable"
		r.rebuildPanel(h)
		return
	}
	_, err := r.pluginStore.Update(id, store.ReleaseRef{}, false)
	if store.KindOf(err) == store.KindConsent {
		if listing, ok := pluginStoreListingForID(r.pluginStoreSnapshot.Listings, id); ok {
			h.pluginUpdateAllNeedsConsent = []string{pluginStoreKey(listing)}
		}
	} else if err != nil {
		h.pluginManagerError = err.Error()
	}
	r.rebuildPanel(h)
}

func (r *Registry) updateAllPluginsLocked(h *PanelHost) {
	if r.pluginStore == nil {
		h.pluginManagerError = "plugin store unavailable"
		r.rebuildPanel(h)
		return
	}
	h.pluginManagerError = ""
	h.pluginUpdateAllNeedsConsent = nil
	seen := map[string]bool{}
	for _, listing := range r.pluginStoreSnapshot.Listings {
		id := listing.Entry.ID
		if !listing.UpdateAvailable || listing.Installed == nil || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := r.pluginStore.Update(id, store.ReleaseRef{}, false); store.KindOf(err) == store.KindConsent {
			h.pluginUpdateAllNeedsConsent = append(h.pluginUpdateAllNeedsConsent, pluginStoreKey(listing))
		} else if err != nil {
			h.pluginManagerError = err.Error()
		}
	}
	r.rebuildPanel(h)
}

func pluginStoreListingForID(listings []store.Listing, id string) (store.Listing, bool) {
	for _, listing := range listings {
		if listing.Entry.ID == id && listing.Installed != nil {
			return listing, true
		}
	}
	return store.Listing{}, false
}

func (r *Registry) refreshPluginStoreLocked(h *PanelHost) {
	if r.pluginStore == nil {
		h.pluginManagerError = "plugin store unavailable"
	} else if _, err := r.pluginStore.Refresh(); err != nil {
		h.pluginManagerError = err.Error()
	} else {
		h.pluginManagerError = ""
	}
	r.rebuildPanel(h)
}

func (r *Registry) confirmPluginSourceLocked(h *PanelHost) {
	name, sourceURL := "", ""
	if h.fields != nil {
		if field := h.fields["plugins-source-name"]; field != nil {
			name = strings.TrimSpace(field.Text)
		}
		if field := h.fields["plugins-source-url"]; field != nil {
			sourceURL = strings.TrimSpace(field.Text)
		}
	}
	if err := config.ValidatePluginSource(name, sourceURL); err != nil {
		h.pluginManagerError = err.Error()
		r.rebuildPanel(h)
		return
	}
	for _, source := range r.cfg.Plugins.EffectiveSources() {
		if source.Name == name {
			h.pluginManagerError = fmt.Sprintf("source %q already exists", name)
			r.rebuildPanel(h)
			return
		}
	}
	next := r.cfg
	next.Plugins = next.Plugins.Clone()
	next.Plugins.Sources = append(next.Plugins.Sources, config.PluginSource{Name: name, URL: sourceURL, Enabled: true})
	if err := r.writeConfig(next); err != nil {
		h.pluginManagerError = err.Error()
		r.rebuildPanel(h)
		return
	}
	r.cfg = next
	h.pluginManagerError = ""
	h.pluginManagerSourceWarning = false
	for _, key := range []string{"plugins-source-name", "plugins-source-url"} {
		if h.fields[key] != nil {
			h.fields[key].Clear()
		}
	}
	r.refreshPluginStoreLocked(h)
}

func (r *Registry) setPluginSourceEnabledLocked(h *PanelHost, name string, enabled bool) {
	next := r.cfg
	next.Plugins = next.Plugins.Clone()
	found := false
	for i := range next.Plugins.Sources {
		if next.Plugins.Sources[i].Name == name {
			next.Plugins.Sources[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found && name == config.BuiltinPluginSource.Name {
		next.Plugins.Sources = append(next.Plugins.Sources, config.PluginSource{Name: name, Enabled: enabled})
		found = true
	}
	if !found {
		for _, source := range r.pluginStoreSnapshot.Sources {
			if source.Name == name {
				next.Plugins.Sources = append(next.Plugins.Sources, config.PluginSource{Name: name, URL: source.URL, Enabled: enabled})
				found = true
				break
			}
		}
	}
	if !found {
		h.pluginManagerError = "source is no longer configured"
		r.rebuildPanel(h)
		return
	}
	if err := r.writeConfig(next); err != nil {
		h.pluginManagerError = err.Error()
		r.rebuildPanel(h)
		return
	}
	r.cfg = next
	h.pluginManagerError = ""
	r.refreshPluginStoreLocked(h)
}

func (r *Registry) removePluginSourceLocked(h *PanelHost, name string) {
	if name == config.BuiltinPluginSource.Name {
		return
	}
	next := r.cfg
	next.Plugins = next.Plugins.Clone()
	filtered := next.Plugins.Sources[:0]
	for _, source := range next.Plugins.Sources {
		if source.Name != name {
			filtered = append(filtered, source)
		}
	}
	next.Plugins.Sources = filtered
	if err := r.writeConfig(next); err != nil {
		h.pluginManagerError = err.Error()
		r.rebuildPanel(h)
		return
	}
	r.cfg = next
	h.pluginManagerError = ""
	r.refreshPluginStoreLocked(h)
}

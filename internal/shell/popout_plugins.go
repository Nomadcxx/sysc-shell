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

func pluginsTree(r *Registry, h *PanelHost) *ui.Node {
	if h.pluginManagerTab == "" {
		h.pluginManagerTab = "installed"
	}
	metrics := h.metrics()
	segments := &ui.Node{Kind: ui.KindSegmented, Key: "plugins-tab", Gap: theme.MarginXXS, Height: metrics.StandardControl, Children: []*ui.Node{
		pluginManagerSegment(h, "installed", "Installed"),
		pluginManagerSegment(h, "sources", "Sources"),
	}}
	top := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		segments,
		pluginManagerButton("plugins-browse", "Browse plugins", metrics),
	}}
	children := []*ui.Node{top}
	if r == nil {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "Plugin store unavailable", TextRole: theme.RoleCaption})
		return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: children}
	}
	if h.pluginManagerTab == "sources" {
		children = append(children, pluginManagerSourcesTree(r, h, metrics))
	} else {
		children = append(children, pluginManagerInstalledTree(r, h, metrics))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Children: children}
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
	n := &ui.Node{Kind: ui.KindButton, Text: label, Action: "plugins-tab:" + tab, Name: label, Role: "tab", Focusable: true}
	if h.pluginManagerTab == tab {
		n.State |= ui.StateSelected
	}
	return n
}

func pluginManagerButton(action, label string, metrics theme.Metrics) *ui.Node {
	return &ui.Node{Kind: ui.KindButton, Text: label, Action: action, Name: label, Role: "button", Focusable: true, Height: metrics.StandardControl}
}

func pluginManagerInstalledTree(r *Registry, h *PanelHost, metrics theme.Metrics) *ui.Node {
	updates := pluginManagerUpdates(r.pluginStoreSnapshot.Listings)
	header := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: fmt.Sprintf("Updates (%d)", len(updates)), TextRole: theme.RoleLabel},
	}}
	if len(updates) > 0 {
		header.Children = append(header.Children, pluginManagerButton("plugins-update-all", "Update all", metrics))
	}
	children := []*ui.Node{header}
	if h.pluginManagerError != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.pluginManagerError, Tone: ui.ToneError})
	}
	if len(h.pluginUpdateAllNeedsConsent) > 0 {
		reviewRows := []*ui.Node{{Kind: ui.KindText, Text: "These updates need review", TextRole: theme.RoleLabel}}
		for _, key := range h.pluginUpdateAllNeedsConsent {
			listing, ok := pluginStoreFind(r.pluginStoreSnapshot.Listings, key)
			if !ok {
				continue
			}
			name := listing.Entry.Name
			if name == "" {
				name = listing.Entry.ID
			}
			reviewRows = append(reviewRows, &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
				{Kind: ui.KindText, Text: name},
				pluginManagerButton("plugins-review:"+key, "Review", metrics),
			}})
		}
		children = append(children, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: reviewRows})
	}
	rows := pluginManagerRows(r)
	if len(rows) == 0 {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "No installed plugins", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	} else {
		for _, row := range rows {
			children = append(children, pluginManagerInstalledRow(r, h, row, metrics))
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
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

func pluginManagerInstalledRow(r *Registry, h *PanelHost, row pluginManagerRow, metrics theme.Metrics) *ui.Node {
	id, name := row.id, pluginManagerName(row)
	version, description, source, provenance := "", "", "local", ""
	var required []string
	if row.candidate != nil {
		version = row.candidate.Manifest.Version
		description = row.candidate.Manifest.Description
		required = row.candidate.Manifest.Requires
		source = string(row.candidate.Source)
	}
	if listing := row.listing; listing != nil {
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
		if listing.LocalDir != "" {
			provenance = "local override"
		} else if listing.Status == store.StatusUnlisted {
			provenance = "managed, unlisted"
		} else if listing.Installed != nil {
			provenance = "managed install"
		}
	}
	if row.candidate != nil && row.candidate.Source == plugin.SourceUser && provenance == "" {
		provenance = "local"
	}
	if source == "" {
		source = "local"
	}
	badge := source
	if source == "sysc" {
		badge = "Official"
	} else if source == "community" {
		badge = "Community"
	}
	versionText := version
	if versionText != "" {
		versionText = "v" + versionText
	}
	labelChildren := []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindIcon, Icon: "extension"},
		{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
		{Kind: ui.KindText, Text: badge, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		{Kind: ui.KindText, Text: versionText, TextRole: theme.RoleCaption},
	}}}
	if description != "" {
		labelChildren = append(labelChildren, &ui.Node{Kind: ui.KindText, Text: description, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if len(required) > 0 {
		labelChildren = append(labelChildren, &ui.Node{Kind: ui.KindText, Text: "Requires: " + strings.Join(required, ", "), TextRole: theme.RoleCaption})
	}
	if provenance != "" {
		labelChildren = append(labelChildren, &ui.Node{Kind: ui.KindText, Text: provenance, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	label := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: labelChildren}
	control := &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{}}
	canToggle := row.listing != nil && row.listing.Installed != nil
	if row.candidate != nil && row.candidate.Err == nil {
		canToggle = true
	}
	if canToggle {
		on := slices.Contains(r.cfg.Plugins.Enabled, id)
		value := 0.0
		if on {
			value = 1
		}
		control.Children = append(control.Children, &ui.Node{Kind: ui.KindToggle, Value: value, Action: "plugin-enable:" + id,
			Name: "Enable " + name, Role: "switch", Focusable: true})
	}
	children := []*ui.Node{{Kind: ui.KindRow, PinEnd: true, Gap: theme.MarginL, Children: []*ui.Node{label, control}}}
	if listing := row.listing; listing != nil && listing.UpdateAvailable {
		release := listing.Entry.Version
		if listing.Resolution.Release != nil {
			release = listing.Resolution.Release.Version
		}
		children = append(children, pluginManagerButton("plugins-update:"+id, "Update to v"+release, metrics))
	}
	if listing := row.listing; listing != nil && listing.Installed != nil && listing.Installed.Previous != nil {
		children = append(children, pluginManagerButton("plugins-rollback:"+id, "Roll back to v"+listing.Installed.Previous.Version, metrics))
	}
	if listing := row.listing; listing != nil && listing.Installed != nil {
		if h.pluginManagerRemoveConfirm == id {
			children = append(children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
				{Kind: ui.KindText, Text: "Remove the managed copy? Plugins.Enabled is kept."},
				pluginManagerButton("plugins-remove-confirm:"+id, "Confirm", metrics),
				pluginManagerButton("plugins-remove-cancel", "Cancel", metrics),
			}})
		} else {
			children = append(children, pluginManagerButton("plugins-remove:"+id, "Remove", metrics))
		}
	}
	if row.candidate != nil {
		children = append(children, pluginManagerButton("plugin-retry:"+id, "Retry", metrics))
		status := r.plugins.status(id)
		if len(status.Stderr) > 0 {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: string(status.Stderr), Tone: ui.ToneError})
		}
		if row.candidate.Err != nil {
			children = append(children, &ui.Node{Kind: ui.KindText, Text: row.candidate.Err.Error(), Tone: ui.ToneError})
		}
	}
	if row.listing != nil && row.listing.Err != nil {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: row.listing.Err.Error(), Tone: ui.ToneError})
	}
	if row.candidate != nil && len(row.candidate.Manifest.Settings) > 0 {
		actionText := "Plugin settings"
		if h.pluginManagerExpanded[id] {
			actionText = "Hide settings"
		}
		children = append(children, pluginManagerButton("plugins-settings:"+id, actionText, metrics))
		if h.pluginManagerExpanded[id] {
			values := pluginSettingValues(r, id, row.candidate.Manifest.Settings)
			for _, setting := range row.candidate.Manifest.Settings {
				if plugin.SettingVisible(setting, values) {
					children = append(children, pluginSettingRow(r, h, id, setting))
				}
			}
		}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Padding: theme.MarginS, Children: children}
}

func pluginManagerSourcesTree(r *Registry, h *PanelHost, metrics theme.Metrics) *ui.Node {
	stateByName := make(map[string]store.SourceState, len(r.pluginStoreSnapshot.Sources))
	for _, state := range r.pluginStoreSnapshot.Sources {
		stateByName[state.Name] = state
	}
	children := []*ui.Node{{Kind: ui.KindText, Text: "Lower sources override higher ones", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}}
	sources := r.cfg.Plugins.EffectiveSources()
	for _, state := range r.pluginStoreSnapshot.Sources {
		if !slices.ContainsFunc(sources, func(source config.PluginSource) bool { return source.Name == state.Name }) {
			sources = append(sources, config.PluginSource{Name: state.Name, URL: state.URL})
		}
	}
	for _, source := range sources {
		state := stateByName[source.Name]
		children = append(children, pluginManagerSourceRow(h, source, state, metrics))
	}
	if h.pluginManagerError != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.pluginManagerError, Tone: ui.ToneError})
	}
	children = append(children, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Add source", TextRole: theme.RoleLabel},
		pluginManagerSourceField(h, "plugins-source-name", "Source name"),
		pluginManagerSourceField(h, "plugins-source-url", "Repository URL"),
		pluginManagerButton("plugins-source-add", "Add source", metrics),
	}})
	if h.pluginManagerSourceWarning {
		children = append(children, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Padding: theme.MarginM, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Review source permissions", TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: "Plugins from this source run as your user with"},
			{Kind: ui.KindText, Text: "full file and network access"},
			{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
				pluginManagerButton("plugins-source-confirm", "Confirm", metrics),
				pluginManagerButton("plugins-source-cancel", "Cancel", metrics),
			}},
		}})
	}
	children = append(children, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Suggested", TextRole: theme.RoleLabel},
		{Kind: ui.KindText, Text: "Community catalog", Tone: ui.ToneSubtle},
		{Kind: ui.KindText, Text: "Not published yet", Tone: ui.ToneSubtle},
	}})
	children = append(children, &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
		{Kind: ui.KindText, Text: "Local plugin directory", TextRole: theme.RoleLabel},
		{Kind: ui.KindText, Text: pluginDirectoryLabel(r), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		pluginManagerButton("plugin-rescan", "Rescan", metrics),
	}})
	children = append(children, &ui.Node{Kind: ui.KindText, Text: "The same plugin offered by two sources appears once per source.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Children: children}
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

func pluginManagerSourceRow(h *PanelHost, source config.PluginSource, state store.SourceState, metrics theme.Metrics) *ui.Node {
	name := source.Name
	if name == "sysc" {
		name = "Official"
	}
	kind := "Git"
	if strings.HasPrefix(source.URL, "file:") {
		kind = "Path"
	}
	status := "Not fetched yet"
	if !state.FetchedAt.IsZero() {
		status = "Last fetched " + state.FetchedAt.Format("2006-01-02 15:04")
	}
	if state.Err != nil {
		if state.FetchedAt.IsZero() {
			status = "Error: " + state.Err.Error()
		} else {
			status = "Stale: " + state.Err.Error()
		}
	}
	value := 0.0
	if source.Enabled {
		value = 1
	}
	controls := []*ui.Node{{Kind: ui.KindToggle, Value: value, Action: "plugins-source-toggle:" + source.Name,
		Name: "Enable " + name, Role: "switch", Focusable: true}}
	controls = append(controls, pluginManagerButton("plugins-source-refresh:"+source.Name, "Refresh", metrics))
	if source.Name != "sysc" {
		controls = append(controls, pluginManagerButton("plugins-source-remove:"+source.Name, "Remove", metrics))
	}
	rowChildren := []*ui.Node{
		{Kind: ui.KindRow, PinEnd: true, Gap: theme.MarginL, Children: []*ui.Node{
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{
				{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
					{Kind: ui.KindText, Text: name, TextRole: theme.RoleLabel},
					{Kind: ui.KindText, Text: kind, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
				}},
				{Kind: ui.KindText, Text: source.URL, TextRole: theme.RoleCaption},
				{Kind: ui.KindText, Text: status, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
			}},
			{Kind: ui.KindRow, Gap: theme.MarginS, Children: controls},
		}},
	}
	if !state.FetchedAt.IsZero() {
		rowChildren = append(rowChildren, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%d plugins", state.Plugins), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Padding: theme.MarginS, Children: rowChildren}
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

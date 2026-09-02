package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func pluginsTree(r *Registry, h *PanelHost) *ui.Node {
	rows := []*ui.Node{
		{Kind: ui.KindText, Text: pluginDirectoryLabel(r)},
		{Kind: ui.KindButton, Text: "Rescan", Action: "plugin-rescan", Name: "Rescan", Role: "button", Focusable: true},
	}
	if r == nil || r.plugins == nil {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "No plugin host"})
		return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: rows}
	}
	for _, c := range r.plugins.discovered().Plugins {
		rows = append(rows, pluginCard(r, h, c))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 10, Padding: 12, Children: rows}
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

func pluginCard(r *Registry, h *PanelHost, c plugin.Candidate) *ui.Node {
	id := c.Manifest.ID
	if id == "" {
		id = c.Dir
	}
	st := plugin.Status{State: plugin.StateDisabled}
	if r.plugins != nil {
		st = r.plugins.status(id)
	}
	title := c.Manifest.Name
	if title == "" {
		title = id
	}
	meta := fmt.Sprintf("%s %s · %s · %s", title, c.Manifest.Version, c.Source, st.State)
	if c.Err != nil {
		meta += " · " + c.Err.Error()
	}
	if len(c.MissingCommands) > 0 {
		meta += " · needs " + strings.Join(c.MissingCommands, ", ")
	}
	if len(c.Manifest.Capabilities) > 0 {
		var caps []string
		for _, cap := range c.Manifest.Capabilities {
			caps = append(caps, string(cap))
		}
		meta += " · " + strings.Join(caps, ", ")
	}

	enabled := false
	if r != nil {
		for _, have := range r.cfg.Plugins.Enabled {
			if have == id {
				enabled = true
				break
			}
		}
	}
	toggle := 0.0
	if enabled {
		toggle = 1
	}
	children := []*ui.Node{
		{Kind: ui.KindText, Text: meta},
		{Kind: ui.KindToggle, Value: toggle, Action: "plugin-enable:" + id,
			Name: "Enable " + title, Role: "switch", Focusable: true},
		{Kind: ui.KindButton, Text: "Retry", Action: "plugin-retry:" + id,
			Name: "Retry " + title, Role: "button", Focusable: true},
	}
	if len(st.Stderr) > 0 {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: string(st.Stderr), Tone: ui.ToneError})
	}
	values := pluginSettingValues(r, id, c.Manifest.Settings)
	for _, s := range c.Manifest.Settings {
		if !plugin.SettingVisible(s, values) {
			continue
		}
		children = append(children, pluginSettingRow(r, h, id, s))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 6, Children: children}
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
	for _, g := range pluginPanelSettingGroups {
		var rows []*ui.Node
		for _, key := range g.Keys {
			s, ok := byKey[key]
			if !ok || !plugin.SettingVisible(s, values) {
				continue
			}
			rows = append(rows, pluginSettingRow(r, h, pluginID, s))
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, &ui.Node{Kind: ui.KindText, Text: g.Title, Bold: true, Name: g.Title, Role: "heading"})
		out = append(out, rows...)
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
	return &ui.Node{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
		{Kind: ui.KindText, Text: s.Label},
		control,
	}}
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
			Focusable: true, Name: s.Label, Role: "switch",
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
			Action: action, Width: 160, Focusable: true, Name: s.Label, Role: "slider",
		}
	case plugin.SettingSelect:
		opts := make([]string, 0, len(s.Options))
		for _, o := range s.Options {
			opts = append(opts, o.Value)
		}
		idx := 0
		for i, o := range opts {
			if o == raw {
				idx = i
				break
			}
		}
		if h == nil {
			n := NewMenu(opts, idx).Node()
			n.Action = action
			n.Name = s.Label
			return n
		}
		if h.menus == nil {
			h.menus = map[string]*Menu{}
		}
		m := h.menus[store]
		if m == nil || !m.Opened() {
			m = NewMenu(opts, idx)
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

// handlePluginManager runs under Registry.mu (PanelHost.handle). It must not
// call paths that re-lock the registry.
func (r *Registry) handlePluginManager(h *PanelHost, n *ui.Node) bool {
	if r.plugins == nil || n == nil {
		return false
	}
	action := n.Action
	switch {
	case action == "plugin-rescan":
		_ = r.plugins.syncEnabledLocked()
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugin-retry:"); ok {
		_ = r.plugins.retryLocked(id)
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugin-enable:"); ok {
		on := true
		for _, have := range r.cfg.Plugins.Enabled {
			if have == id {
				on = false
				break
			}
		}
		_ = r.plugins.enableLocked(id, on)
		r.rebuildPanel(h)
		return true
	}
	if rest, ok := strings.CutPrefix(action, "plugin-set:"); ok {
		pluginID, key, ok := strings.Cut(rest, ":")
		if !ok {
			return false
		}
		value, ok := pluginSettingValueFromNode(h, n)
		if !ok {
			return false
		}
		_ = r.plugins.applySettingLocked(pluginID, key, value)
		r.rebuildPanel(h)
		return true
	}
	return false
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

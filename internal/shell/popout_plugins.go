package shell

import (
	"fmt"
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
		rows = append(rows, pluginCard(r, c))
	}
	_ = h
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

func pluginCard(r *Registry, c plugin.Candidate) *ui.Node {
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
	for _, s := range c.Manifest.Settings {
		children = append(children, pluginSettingRow(r, id, s))
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: 6, Children: children}
}

func pluginSettingRow(r *Registry, pluginID string, s plugin.Setting) *ui.Node {
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
	control := &ui.Node{Kind: ui.KindText, Text: raw, Action: action, Name: s.Label, Role: "textbox", Focusable: true}
	if s.Type == plugin.SettingBool {
		v := 0.0
		if raw == "true" {
			v = 1
		}
		control = &ui.Node{Kind: ui.KindToggle, Value: v, Action: action, Name: s.Label, Role: "switch", Focusable: true}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
		{Kind: ui.KindText, Text: s.Label},
		control,
	}}
}

func (r *Registry) handlePluginManager(h *PanelHost, action string) bool {
	if r.plugins == nil {
		return false
	}
	switch {
	case action == "plugin-rescan":
		_ = r.plugins.rescan()
		r.rebuildPanel(h)
		return true
	}
	if id, ok := strings.CutPrefix(action, "plugin-retry:"); ok {
		_ = r.plugins.retry(id)
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
		_ = r.plugins.enable(id, on)
		r.rebuildPanel(h)
		return true
	}
	if rest, ok := strings.CutPrefix(action, "plugin-set:"); ok {
		pluginID, key, ok := strings.Cut(rest, ":")
		if !ok {
			return false
		}
		value := true
		_ = r.plugins.applySetting(pluginID, key, value)
		r.rebuildPanel(h)
		return true
	}
	return false
}

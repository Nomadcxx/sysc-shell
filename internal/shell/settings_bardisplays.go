package shell

import (
	"maps"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// UpdateDisplayOutputs receives a fresh Wayland snapshot even when no bar is
// mapped. Keeping discovery separate from bars lets a hidden bar be restored.
func (r *Registry) UpdateDisplayOutputs(outputs []wayland.OutputInfo) {
	next := make(map[uint32]wayland.OutputInfo, len(outputs))
	for _, out := range outputs {
		if out.Connector != "" {
			next[out.Global] = out
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Every output's done event republishes the snapshot; only a change in
	// it is worth rebuilding Settings for.
	if maps.Equal(r.displayOutputs, next) {
		return
	}
	r.displayOutputs = next
	for _, out := range next {
		r.SetOutputLogicalSize(out.Connector, out.Width, out.Height)
	}
	if h := r.panelHosts[PanelSettings]; h != nil {
		h.set = r.settingsForLocked(h.draft)
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(PanelSettings))
	}
}

func (r *Registry) barDisplayConnectorsLocked(cfg config.Config) []string {
	seen := map[string]bool{}
	if r != nil {
		for _, out := range r.displayOutputs {
			seen[out.Connector] = true
		}
		for _, bar := range r.bars {
			seen[bar.connector()] = true
		}
	}
	for _, out := range cfg.Outputs {
		seen[out.Connector] = true
	}
	delete(seen, "")
	connectors := make([]string, 0, len(seen))
	for conn := range seen {
		connectors = append(connectors, conn)
	}
	slices.Sort(connectors)
	return connectors
}

func (r *Registry) displayOutputLocked(connector string) (wayland.OutputInfo, bool) {
	var found wayland.OutputInfo
	if r == nil {
		return found, false
	}
	for _, out := range r.displayOutputs {
		if out.Connector == connector && out.Global >= found.Global {
			found = out
		}
	}
	if found.Connector != "" {
		return found, true
	}
	for global, bar := range r.bars {
		if bar.connector() == connector {
			return wayland.OutputInfo{Global: global, Connector: connector}, true
		}
	}
	return found, false
}

func settingsBarDisplays(r *Registry, h *PanelHost) *ui.Node {
	children := []*ui.Node{{Kind: ui.KindText, Text: h.tr("bar.displays.intro", "Choose where the bar appears and override its size and position. Other settings follow Appearance."), TextRole: theme.RoleCaption}}
	connectors := r.barDisplayConnectorsLocked(h.draft)
	if len(connectors) == 0 {
		return settingsBody(h, theme.MarginL, settingsEmptyNote("Displays"))
	}
	visible := false
	for _, conn := range connectors {
		out, connected := r.displayOutputLocked(conn)
		if connected && h.draft.ForConnector(conn).Enabled {
			visible = true
		}
		name := strings.TrimSpace(out.Make + " " + out.Model)
		title := conn
		if name != "" {
			title = name + " · " + conn
		}
		status := h.tr("bar.displays.disconnected", "Disconnected")
		if connected {
			status = h.tr("bar.displays.connected", "Connected")
		}
		rows := []*ui.Node{{Kind: ui.KindText, Text: status, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle}}
		custom := false
		if h.set != nil {
			for _, entry := range h.set.PageEntries("Bar", "Displays") {
				if entry.Group != conn {
					continue
				}
				if r != nil {
					get := entry.Get
					entry.Get = func(c config.Config) string {
						c.Bar = r.barPolicy(c, conn)
						c.Outputs = nil
						return get(c)
					}
				}
				row := settingsEntryRow(h, entry, settingsCardInner(h))
				state := h.tr("bar.displays.shared", "Shared")
				if !entry.IsDefault(h.draft) {
					state, custom = h.tr("bar.displays.custom", "Custom"), true
				}
				row.Children[0].Children = append(row.Children[0].Children, &ui.Node{Kind: ui.KindText, Text: state, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
				shared := "bar." + strings.TrimPrefix(entry.Path, "outputs."+conn+".")
				if reason := settingsBarDimReasonFor(h.draft.ForConnector(conn), h.draft.Accessibility.HighContrast, shared); reason != "" {
					settingsDimRow(row, reason)
				}
				rows = append(rows, row)
			}
		}
		button := func(label, action string) *ui.Node {
			return &ui.Node{Kind: ui.KindButton, Text: label, Name: label + " on " + conn, Action: action + conn, Role: "button", Focusable: true, Padding: h.metrics().ButtonPadding, Height: h.metrics().StandardControl, Shape: ui.ShapeMedium}
		}
		rows = append(rows, button(h.tr("bar.displays.layout", "Edit widget layout"), "bar-display-layout:"))
		if custom {
			rows = append(rows, button(h.tr("bar.displays.reset", "Reset display settings"), "bar-display-reset:"))
		}
		children = append(children, settingsGroupCardLiteral(h, title, rows))
	}
	if !visible {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.tr("bar.displays.no-bars", `All bars are hidden. Keep Settings open to restore them, or run: sysc-shell ipc panel.open '{"panel":"settings"}'`), TextRole: theme.RoleCaption, Role: "status"})
	}
	return settingsBody(h, theme.MarginL, children...)
}

func (h *PanelHost) barDisplayAction(r *Registry, action string) bool {
	if conn, ok := strings.CutPrefix(action, "bar-display-layout:"); ok {
		if !slices.Contains(r.barDisplayConnectorsLocked(h.draft), conn) {
			return true
		}
		h.barOutput, h.barSelected, h.barAdding = conn, "", ""
		h.section, h.settingsPage, h.settingsScrollTop = "Bar", "Layout", true
		r.rebuildPanel(h)
		return true
	}
	if conn, ok := strings.CutPrefix(action, "bar-display-reset:"); ok {
		if h.set == nil {
			return true
		}
		for _, e := range h.set.PageEntries("Bar", "Displays") {
			if e.Group == conn && e.Reset != nil {
				if err := e.Reset(&h.draft); err != nil {
					h.errLabel = err.Error()
					break
				}
				h.deferReset(r, e.Path)
			}
		}
		h.set = r.settingsForLocked(h.draft)
		r.rebuildPanel(h)
		return true
	}
	return false
}

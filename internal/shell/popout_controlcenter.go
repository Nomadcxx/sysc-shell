package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const panelControlCenterAction = "panel:control-center"

const (
	ccRailWidth  = 56
	ccRailItem   = 40
	ccGap        = 16
	ccBodyGap    = 12
	ccPanelPad   = 16
	ccHeaderSize = 40
)

type ccSection struct {
	ID, Label, Icon string
	Enabled         bool
}

var ccSections = []ccSection{
	{ID: "home", Label: "Home", Icon: "home", Enabled: true},
	{ID: "media", Label: "Media", Icon: "music_note"},
	{ID: "audio", Label: "Audio", Icon: "volume_up", Enabled: true},
	{ID: "monitor", Label: "Monitor", Icon: "desktop_windows", Enabled: true},
	{ID: "power", Label: "Power", Icon: "power_settings_new", Enabled: true},
	{ID: "network", Label: "Network", Icon: "wifi"},
	{ID: "bluetooth", Label: "Bluetooth", Icon: "bluetooth"},
	{ID: "weather", Label: "Weather", Icon: "cloud", Enabled: true},
	{ID: "calendar", Label: "Calendar", Icon: "calendar_month", Enabled: true},
	{ID: "notifications", Label: "Notifications", Icon: "notifications", Enabled: true},
}

func ccSectionFor(id string) (ccSection, bool) {
	for _, section := range ccSections {
		if section.ID == id {
			return section, true
		}
	}
	return ccSection{}, false
}

func controlCentreTree(r *Registry, h *PanelHost) *ui.Node {
	panel := panelTargetSize(PanelControlCenter)
	if h.place.Panel.W > 0 {
		panel = h.place.Panel
	}
	bodyWidth := max(panel.W-2*ccPanelPad-ccRailWidth-ccGap, 0)
	bodyHeight := max(panel.H-2*ccPanelPad-ccHeaderSize-ccBodyGap, 0)
	return &ui.Node{Kind: ui.KindRow, Gap: ccGap, Padding: ccPanelPad, Children: []*ui.Node{
		ccRail(h),
		{Kind: ui.KindColumn, Width: bodyWidth, Gap: ccBodyGap, Children: []*ui.Node{
			ccHeader(h),
			{Kind: ui.KindScroll, Height: bodyHeight, Children: []*ui.Node{ccPage(r, h)}},
		}},
	}}
}

func ccRail(h *PanelHost) *ui.Node {
	rail := &ui.Node{Kind: ui.KindColumn, Width: ccRailWidth, Gap: 8}
	for _, section := range ccSections {
		if section.ID == "media" || section.ID == "network" || section.ID == "weather" {
			rail.Children = append(rail.Children, &ui.Node{Kind: ui.KindColumn, Height: 8})
		}
		name := section.Label
		action := "section:" + section.ID
		state := ui.Interaction(0)
		if !section.Enabled {
			name += " — not available yet"
			action = ""
			state |= ui.StateDisabled
		}
		if h.section == section.ID {
			state |= ui.StateSelected
		}
		entry := &ui.Node{
			Kind: ui.KindButton, Width: ccRailItem, Height: ccRailItem,
			Action: action, Name: name, Role: "tab", Focusable: true,
			AriaDisabled: !section.Enabled, State: state, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindIcon, Icon: section.Icon}},
		}
		if section.ID == h.section {
			entry.Fill = ui.FillAccent
		}
		rail.Children = append(rail.Children, entry)
	}
	return rail
}

func ccHeader(h *PanelHost) *ui.Node {
	label := "Home"
	if section, ok := ccSectionFor(h.section); ok {
		label = section.Label
	}
	button := func(icon, action, name string) *ui.Node {
		n := centreIconButton(icon, action, name)
		n.Width, n.Height = ccHeaderSize, ccHeaderSize
		return n
	}
	return &ui.Node{Kind: ui.KindRow, Height: ccHeaderSize, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleHeadline, Name: label, Role: "heading"},
		{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
			button("settings", "cc:settings", "Settings"),
			button("power_settings_new", "cc:power", "Power"),
			button("close", "cc:close", "Close"),
		}},
	}}
}

func ccPage(r *Registry, h *PanelHost) *ui.Node {
	switch h.section {
	case "audio":
		return ccAudio(r, h)
	case "monitor":
		return ccMonitor(r, h)
	case "power":
		return ccPower(r, h)
	case "weather":
		return ccWeather(r, h)
	case "calendar":
		return ccCalendar(r, h)
	case "notifications":
		return ccNotifications(r, h)
	default:
		return ccHome(r, h)
	}
}

func (h *PanelHost) activateControlCentre(r *Registry, n *ui.Node) bool {
	if n == nil || h.id != PanelControlCenter {
		return false
	}
	var target PanelID
	switch n.Action {
	case "cc:settings":
		target = PanelSettings
	case "cc:power":
		target = PanelSession
	case "cc:wallpaper":
		target = PanelWallpaper
	case "cc:close":
		r.closePanelLocked(h.id)
		return true
	case "cc:caffeine":
		r.setCaffeine(h, !r.inhibitWanted)
		r.rebuildPanel(h)
		return true
	case "cc:dnd":
		_, on := r.notify.dndState(r.now)
		r.setDND(!on)
		if r.toasts != nil {
			r.toasts.recompute()
		}
		r.rebuildPanel(h)
		return true
	case "cc:mute":
		audio := r.audio
		if audio == nil {
			return false
		}
		state, ok := audio.CachedState()
		if !ok {
			return false
		}
		r.scheduleControl(h, func() error { return audio.SetMute(!state.Muted) })
		return true
	case "cc:volume":
		audio := r.audio
		if audio == nil {
			return false
		}
		level := int(n.Value)
		r.scheduleControl(h, func() error { return audio.Set(level) })
		return true
	case "cc:brightness":
		brightness := r.brightness
		if brightness == nil {
			return false
		}
		level := int(n.Value)
		r.scheduleControl(h, func() error { return brightness.Set(level) })
		return true
	case "session-lock", "session-logout", "session-suspend", "session-reboot", "session-poweroff":
		argv := sessionArgv(n.Action, r.cfg.Session.Locker)
		run := r.runArgv
		r.scheduleControl(h, func() error { return run(argv) })
		return true
	default:
		name, ok := strings.CutPrefix(n.Action, "cc:profile:")
		if !ok || !profileSupports(h.profiles, name) {
			return false
		}
		run := r.runArgv
		r.scheduleControl(h, func() error {
			if err := run(powerProfileSetArgv(name)); err != nil {
				return err
			}
			r.mu.Lock()
			if r.panelHosts[h.id] == h {
				h.profileActive = name
			}
			r.mu.Unlock()
			return nil
		})
		return true
	}
	trig := Trigger{
		BarEdge: h.place.BarEdge, BarZone: h.place.BarZone,
		OutW: h.place.Output.W, OutH: h.place.Output.H,
	}
	_ = r.openPanelRootLocked(target, h.output, trig)
	return true
}

// setCaffeine changes the one process-wide idle inhibit. Caller holds r.mu;
// the process is started and stopped through scheduleControl off the owner.
func (r *Registry) setCaffeine(h *PanelHost, on bool) {
	if on {
		if r.inhibitWanted {
			return
		}
		r.inhibitWanted = true
		if r.inhibit != nil || r.inhibitStarting {
			return
		}
		r.inhibitStarting = true
		start := r.startInhibit
		r.scheduleControl(h, func() error {
			hold, err := start()
			r.mu.Lock()
			r.inhibitStarting = false
			if err != nil {
				r.inhibitWanted = false
				r.mu.Unlock()
				return err
			}
			stopped := false
			select {
			case <-r.closed:
				stopped = true
			default:
			}
			keep := r.inhibitWanted && !stopped
			if keep {
				r.inhibit = hold
			}
			r.mu.Unlock()
			if !keep {
				return hold.Close()
			}
			return nil
		})
		return
	}

	r.inhibitWanted = false
	if r.inhibit == nil {
		return
	}
	hold := r.inhibit
	r.inhibit = nil
	r.scheduleControl(h, hold.Close)
}

package shell

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const panelControlCenterAction = "panel:control-center"
const caffeineMenuAction = "cc:caffeine-preset"
const ccPrivacyOpenAction = "cc:privacy-open"

const (
	controlCentrePageKey     = "control-centre-page"
	controlCentreViewportKey = "control-centre-page-viewport"
)

const (
	ccRailWidth  = 56
	ccRailItem   = 40
	ccGap        = 16
	ccBodyGap    = 12
	ccPanelPad   = 16
	ccHeaderSize = 40
)

type caffeinePreset struct {
	value    string
	label    string
	labelKey string
	duration time.Duration
	active   bool
}

var caffeinePresets = [...]caffeinePreset{
	{value: "off", label: "Off", labelKey: "cc.caffeine.off"},
	{value: "15m", label: "15m", duration: 15 * time.Minute, active: true},
	{value: "30m", label: "30m", duration: 30 * time.Minute, active: true},
	{value: "1h", label: "1h", duration: time.Hour, active: true},
	{value: "2h", label: "2h", duration: 2 * time.Hour, active: true},
	{value: "4h", label: "4h", duration: 4 * time.Hour, active: true},
	{value: "8h", label: "8h", duration: 8 * time.Hour, active: true},
	{value: "indefinite", label: "Indefinite", labelKey: "cc.caffeine.indefinite", active: true},
}

func caffeinePresetOptions(tr translateFunc) (options, values []string) {
	options = make([]string, len(caffeinePresets))
	values = make([]string, len(caffeinePresets))
	for i, preset := range caffeinePresets {
		options[i] = preset.label
		if preset.labelKey != "" {
			options[i] = cardT(tr, preset.labelKey, preset.label)
		}
		values[i] = preset.value
	}
	return options, values
}

func caffeinePresetFor(value string) (caffeinePreset, bool) {
	for _, preset := range caffeinePresets {
		if preset.value == value {
			return preset, true
		}
	}
	return caffeinePreset{}, false
}

func caffeineRemainingMinutes(deadline, now time.Time) int {
	remaining := deadline.Sub(now)
	if deadline.IsZero() || remaining <= 0 {
		return 0
	}
	minutes := int(remaining / time.Minute)
	if remaining%time.Minute != 0 {
		minutes++
	}
	return minutes
}

type ccSection struct {
	ID, Label, Icon string
	Enabled         bool
}

var ccSections = []ccSection{
	{ID: "home", Label: "Home", Icon: "home", Enabled: true},
	{ID: "media", Label: "Media", Icon: "music_note", Enabled: true},
	{ID: "audio", Label: "Audio", Icon: "volume_up", Enabled: true},
	{ID: "privacy", Label: "Privacy", Icon: "security", Enabled: true},
	{ID: "monitor", Label: "Monitor", Icon: "desktop_windows", Enabled: true},
	{ID: "power", Label: "Power", Icon: "power_settings_new", Enabled: true},
	{ID: "network", Label: "Network", Icon: "wifi", Enabled: true},
	{ID: "bluetooth", Label: "Bluetooth", Icon: "bluetooth", Enabled: true},
	{ID: "weather", Label: "Weather", Icon: "cloud", Enabled: true},
	{ID: "calendar", Label: "Calendar", Icon: "calendar_month", Enabled: true},
	{ID: "notifications", Label: "Notifications", Icon: "notifications", Enabled: true},
	{ID: "settings", Label: "Settings", Icon: "settings", Enabled: true},
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
	bodyWidth := ccBodyWidth(h)
	panel := panelTargetSize(PanelControlCenter)
	if h.place.Panel.H > 0 {
		panel.H = h.place.Panel.H
	}
	bodyHeight := max(panel.H-2*ccPanelPad-ccHeaderSize-ccBodyGap, 0)
	page := ccPage(r, h)
	page.Key = controlCentrePageKey
	return &ui.Node{Kind: ui.KindRow, Gap: ccGap, Padding: ccPanelPad, Children: []*ui.Node{
		ccRail(h),
		{Kind: ui.KindColumn, Width: bodyWidth, Gap: ccBodyGap, Children: []*ui.Node{
			ccHeader(h),
			{Kind: ui.KindScroll, Key: controlCentreViewportKey, Height: bodyHeight, Children: []*ui.Node{page}},
		}},
	}}
}

func (h *PanelHost) selectControlCentreSection(r *Registry, section string) bool {
	to, ok := ccSectionFor(section)
	if h == nil || h.id != PanelControlCenter || !ok || !to.Enabled || h.section == section {
		return ok && to.Enabled
	}
	if h.section == "bluetooth" {
		r.leaveBluetoothBodyLocked(h)
	}
	if h.section == "media" {
		r.leaveMediaBodyLocked(h)
	}
	fromIndex, toIndex := ccSectionIndex(h.section), ccSectionIndex(section)
	h.pageDirection = 0
	if toIndex > fromIndex {
		h.pageDirection = 1
	} else if toIndex < fromIndex {
		h.pageDirection = -1
	}
	h.section = section
	if section == "home" {
		r.refreshWallsLocked()
	}
	if h.anim != nil {
		if !h.anim.has(controlCentrePageKey, animVisible) || h.anim.Value(controlCentrePageKey, animVisible) >= 1 {
			h.anim.Reset(controlCentrePageKey, animVisible)
			h.anim.Target(controlCentrePageKey, animVisible, 1)
		}
	}
	r.rebuildPanel(h)
	if h.section == "bluetooth" {
		r.startBluetoothDiscoveryLocked(h)
	}
	if h.section == "media" {
		r.startMediaBodyLocked(h)
	}
	r.startSurfaceFrames(h)
	return true
}

func ccSectionIndex(id string) int {
	for i, section := range ccSections {
		if section.ID == id {
			return i
		}
	}
	return -1
}

func (h *PanelHost) controlCentrePageVisual() (page, viewport *ui.Node, progress float64, offset int) {
	if h == nil || h.id != PanelControlCenter || h.root == nil || h.anim == nil ||
		!h.anim.has(controlCentrePageKey, animVisible) {
		return nil, nil, 1, 0
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		switch n.Key {
		case controlCentrePageKey:
			page = n
		case controlCentreViewportKey:
			viewport = n
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(h.root)
	progress = h.anim.PanelOpacity(controlCentrePageKey)
	offset = h.pageDirection * h.anim.PanelSlide(controlCentrePageKey)
	return page, viewport, progress, offset
}

func offsetNodeY(n *ui.Node, dy int) {
	if n == nil || dy == 0 {
		return
	}
	n.Bounds.Y += dy
	for _, child := range n.Children {
		offsetNodeY(child, dy)
	}
}

func ccBodyWidth(h *PanelHost) int {
	panelWidth := panelTargetSize(PanelControlCenter).W
	if h != nil && h.place.Panel.W > 0 {
		panelWidth = h.place.Panel.W
	}
	return max(panelWidth-2*ccPanelPad-ccRailWidth-ccGap, 0)
}

func ccRail(h *PanelHost) *ui.Node {
	// Twelve 40px destinations, three group breaks and 2px row gaps fit the
	// panel's padded height while preserving the existing target size.
	rail := &ui.Node{Kind: ui.KindColumn, Width: ccRailWidth, Gap: theme.MarginXXS}
	for _, section := range ccSections {
		if section.ID == "media" || section.ID == "network" || section.ID == "weather" {
			rail.Children = append(rail.Children, &ui.Node{Kind: ui.KindColumn, Height: theme.MarginXS})
		}
		name := cardT(h.tr, "cc.tab."+section.ID, section.Label)
		action := "section:" + section.ID
		state := ui.Interaction(0)
		if !section.Enabled {
			name += cardT(h.tr, "cc.tab_unavailable", " — not available yet")
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
	label := cardT(h.tr, "cc.tab.home", "Home")
	if section, ok := ccSectionFor(h.section); ok {
		label = cardT(h.tr, "cc.tab."+section.ID, section.Label)
	}
	button := func(icon, action, name string) *ui.Node {
		n := centreIconButton(icon, action, name)
		n.Width, n.Height = ccHeaderSize, ccHeaderSize
		return n
	}
	return &ui.Node{Kind: ui.KindRow, Height: ccHeaderSize, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleHeadline, Name: label, Role: "heading"},
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
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
	case "privacy":
		return ccPrivacy(r, h)
	case "media":
		return mediaBody(r, h)
	case "monitor":
		return ccMonitor(r, h)
	case "network":
		return networkTree(r, h)
	case "power":
		return ccPower(r, h)
	case "weather":
		return ccWeather(r, h)
	case "calendar":
		return ccCalendar(r, h)
	case "bluetooth":
		return bluetoothBody(r, h)
	case "notifications":
		return ccNotifications(r, h)
	case "settings":
		return ccSettings(h)
	default:
		return ccHome(r, h)
	}
}

func (h *PanelHost) activateControlCentre(r *Registry, n *ui.Node) bool {
	if n == nil || h.id != PanelControlCenter {
		return false
	}
	if requested, ok := strings.CutPrefix(n.Action, "settings-section:"); ok {
		r.closePanelLocked(h.id)
		return r.openSettingsAtLocked(h.output, requested)
	}
	if strings.HasPrefix(n.Action, "media:") {
		return h.activateMedia(r, n)
	}
	if id, ok := strings.CutPrefix(n.Action, "cc:brightness:"); ok {
		brightness := r.brightness
		if brightness == nil {
			return false
		}
		level := int(n.Value)
		r.scheduleControl(h, func() error { return brightness.SetDisplay(id, level) })
		return true
	}
	var target PanelID
	switch n.Action {
	case "cc:settings":
		target = PanelSettings
	case "cc:power":
		target = PanelSession
	case "cc:wallpaper":
		target = PanelWallpaper
	case "cc:terminal-art":
		target = PanelTerminalArt
	case panelMonitorAction:
		target = PanelMonitor
	case "cc:close":
		r.closePanelLocked(h.id)
		return true
	case ccPrivacyOpenAction:
		plugins := r.plugins
		generation := h.output
		output := ""
		if bar, ok := r.bars[generation]; ok {
			output = bar.connector()
		}
		r.scheduleControl(h, func() error {
			if plugins == nil {
				return errors.New("Privacybit plugin is unavailable. Enable it in Settings → Plugins.")
			}
			if output == "" {
				return errors.New("Privacybit panel has no current output")
			}
			_, err := plugins.openPanel(ccPrivacyPluginID, v1.PanelParams{
				Entry: "panel", Output: output, Generation: generation, Instance: "control-center",
			})
			if err != nil {
				return fmt.Errorf("could not open the Privacybit audit: %w", err)
			}
			return nil
		})
		return true
	case "cc:caffeine":
		r.setCaffeine(h, !r.inhibitWanted)
		r.rebuildPanel(h)
		return true
	case caffeineMenuAction:
		return false
	case "cc:nightlight":
		if r.nightLight == nil || !r.nightLight.State().Supported {
			return false
		}
		r.nightLight.Toggle()
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
	case "session-lock", "session-logout", "session-suspend", "session-display-off", "session-reboot", "session-poweroff":
		argv := sessionArgv(n.Action, r.cfg.Session.Locker)
		run := r.runArgv
		if n.Action == "session-lock" {
			run = func([]string) error { return r.LockTracked() }
		} else if n.Action == "session-suspend" {
			run = func([]string) error { return r.SuspendTracked() }
		}
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
	r.switchPanelLocked(h, target)
	return true
}

// switchPanelLocked retires h and opens target where h was, leaving any other
// open members of the panel group in place. It returns the new host, or nil.
func (r *Registry) switchPanelLocked(h *PanelHost, target PanelID) *PanelHost {
	trig := Trigger{
		BarEdge: h.place.BarEdge, BarZone: h.place.BarZone,
		OutW: h.place.Output.W, OutH: h.place.Output.H,
	}
	output := h.output
	r.closePanelLocked(h.id)
	if r.openPanelRootLocked(target, output, trig) != nil {
		return nil
	}
	return r.panelHosts[target]
}

// setCaffeine changes the process-backed idle inhibit. Caller holds r.mu.
func (r *Registry) setCaffeine(h *PanelHost, on bool) {
	value := "off"
	if on {
		value = "indefinite"
	}
	r.setCaffeinePreset(h, value)
}

// setCaffeinePreset applies one of the fixed menu values. Caller holds r.mu.
func (r *Registry) setCaffeinePreset(h *PanelHost, value string) {
	preset, ok := caffeinePresetFor(value)
	if !ok {
		return
	}
	r.clearCaffeineLocked()
	r.inhibitWanted = preset.active
	r.inhibitDuration = preset.duration
	if preset.duration > 0 && preset.active {
		// Round strips time.Now's monotonic reading, so suspend time remains in
		// this deadline. The timer uses the resulting wall-time duration.
		deadline := time.Now().Round(0).Add(preset.duration)
		r.inhibitUntil = deadline
		r.inhibitTimer = time.AfterFunc(time.Until(deadline), func() {
			r.caffeineTimerExpired(deadline)
		})
	}
	r.pushIdleInputsLocked()
	if output, ok := r.rebuildScreensaverSettingsPanelLocked(); ok {
		r.publishSurfaceAsync(output, panelSurfaceID(PanelSettings))
	}

	if !preset.active {
		if r.inhibit == nil {
			return
		}
		hold := r.inhibit
		r.inhibit = nil
		r.shutdown.inhibit.store(nil)
		r.scheduleControl(h, hold.Close)
		return
	}
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
			r.clearCaffeineLocked()
			r.pushIdleInputsLocked()
			r.mu.Unlock()
			r.refreshScreensaverSettingsPanel()
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
			r.shutdown.inhibit.store(hold)
		}
		r.mu.Unlock()
		r.refreshScreensaverSettingsPanel()
		if !keep {
			return hold.Close()
		}
		return nil
	})
}

// clearCaffeineLocked clears this shell's request. External inhibitors remain
// intact and continue to reach IdleService through pushIdleInputsLocked.
func (r *Registry) clearCaffeineLocked() {
	if r.inhibitTimer != nil {
		r.inhibitTimer.Stop()
		r.inhibitTimer = nil
	}
	r.inhibitWanted = false
	r.inhibitDuration = 0
	r.inhibitUntil = time.Time{}
}

func (r *Registry) expireCaffeineLocked(now time.Time) (io.Closer, bool) {
	if !r.inhibitWanted || r.inhibitUntil.IsZero() || now.Before(r.inhibitUntil) {
		return nil, false
	}
	hold := r.inhibit
	r.inhibit = nil
	r.shutdown.inhibit.store(nil)
	r.clearCaffeineLocked()
	r.pushIdleInputsLocked()
	return hold, true
}

func (r *Registry) caffeineTimerExpired(deadline time.Time) {
	r.mu.Lock()
	if !r.inhibitWanted || !r.inhibitUntil.Equal(deadline) {
		r.mu.Unlock()
		return
	}
	now := time.Now().Round(0)
	if now.Before(deadline) {
		// A backward wall-clock adjustment can wake the monotonic timer before
		// the deadline. Re-arm against the remaining wall-clock duration.
		r.inhibitTimer = time.AfterFunc(deadline.Sub(now), func() {
			r.caffeineTimerExpired(deadline)
		})
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	r.UpdateClock(now)
}

func (r *Registry) refreshScreensaverSettingsPanel() {
	r.mu.Lock()
	output, ok := r.rebuildScreensaverSettingsPanelLocked()
	r.mu.Unlock()
	if ok {
		r.publishSurface(output, panelSurfaceID(PanelSettings))
	}
}

// rebuildScreensaverSettingsPanelLocked returns the visible Screensaver
// Settings surface after rebuilding it. Caller holds r.mu.
func (r *Registry) rebuildScreensaverSettingsPanelLocked() (uint32, bool) {
	h := r.panelHosts[PanelSettings]
	if h == nil || h.section != "Screensaver" {
		return 0, false
	}
	r.rebuildPanel(h)
	return h.output, true
}

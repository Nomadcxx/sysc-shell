package shell

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	ccDash            = "—"
	ccAvatarSize      = 56
	ccAccountsIconDir = "/var/lib/AccountsService/icons"
)

type ccIdentity struct {
	Name, Account, Uptime, ImagePath string
}

func readCCIdentity() ccIdentity {
	facts := readMachineFacts()
	host, _ := os.Hostname()
	name, account, imagePath := "", "", ""
	if current, err := user.Current(); err == nil {
		name = strings.TrimSpace(current.Name)
		if name == "" {
			name = current.Username
		}
		account = current.Username
		imagePath = ccProfileImagePath(current.HomeDir, current.Username, ccAccountsIconDir)
	}
	if account != "" && host != "" {
		account += "@" + host
	}
	return ccIdentity{Name: name, Account: account, Uptime: facts.Uptime, ImagePath: imagePath}
}

func ccProfileImagePath(home, username, accountsDir string) string {
	for _, path := range []string{
		filepath.Join(home, ".face.icon"),
		filepath.Join(home, ".face"),
		filepath.Join(accountsDir, username),
	} {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func ccAvatarNode(image *ui.Image) *ui.Node {
	if image != nil {
		return &ui.Node{
			Kind: ui.KindImage, Width: ccAvatarSize, Height: ccAvatarSize,
			ImageSize: ccAvatarSize, Image: image, Shape: ui.ShapeCircle,
		}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Width: ccAvatarSize, Height: ccAvatarSize,
		Fill: ui.FillContainerHighest, Shape: ui.ShapeCircle,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "person", IconSize: 28}},
	}
}

func ccAvatar(r *Registry, h *PanelHost, identity ccIdentity) *ui.Node {
	if r == nil || r.trayIcons == nil || identity.ImagePath == "" {
		return ccAvatarNode(nil)
	}
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	key := icons.Square(identity.ImagePath, scale.Physical(ccAvatarSize))
	if image, ok := r.trayIcons.Lookup(key); ok {
		return ccAvatarNode(image)
	}
	if _, failed := r.controlAvatarFailed[key]; !failed {
		_, _, _ = r.trayIcons.Request(key)
	}
	return ccAvatarNode(nil)
}

func ccHome(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	identity := ccIdentity{}
	snap := services.Snapshot{}
	now := time.Time{}
	reading := services.Reading{}
	caffeine, dnd := false, false
	var audio services.AudioState
	var brightness services.BrightnessState
	audioOK, brightnessOK := false, false
	if r != nil {
		identity = r.controlIdentity
		snap, now, reading = r.sample, r.now, r.reading
		caffeine = r.inhibitWanted
		if r.notify != nil {
			_, dnd = r.notify.dndState(now)
		}
		if r.audio != nil {
			audio, audioOK = r.audio.CachedState()
		}
		if r.brightness != nil {
			brightness, brightnessOK = r.brightness.CachedState()
		}
	}

	identityRows := []*ui.Node{
		{Kind: ui.KindText, Text: ccText(identity.Name), TextRole: theme.RoleTitle},
		{Kind: ui.KindText, Text: ccText(identity.Account), TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: "Uptime " + ccText(identity.Uptime), TextRole: theme.RoleCaption},
	}
	if h != nil && h.errLabel != "" {
		identityRows = append([]*ui.Node{{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError}}, identityRows...)
	}
	identityCard := monitorCard(m, []*ui.Node{{
		Kind: ui.KindRow, Gap: 12, Children: []*ui.Node{
			ccAvatar(r, h, identity),
			{Kind: ui.KindColumn, Gap: 4, Children: identityRows},
		},
	}})
	identityCard.Height = 96

	quickWidth := max((ccBodyWidth(h)-8)/2, 0)
	togglePill := &ui.Node{Kind: ui.KindRow, Height: 48, Gap: 8, Children: []*ui.Node{
		ccQuickAccessButton(quickWidth, "coffee", "Caffeine", "cc:caffeine", caffeine),
		ccQuickAccessButton(quickWidth, "wallpaper", "Wallpaper", "cc:wallpaper", false),
	}}

	clockWeather := monitorCard(m, []*ui.Node{
		monitorCardTitle(ccClock(now), 0),
		{Kind: ui.KindText, Text: ccDate(now), TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: ccWeatherSummary(reading)},
	})
	clockWeather.Height = 88
	sysmon := monitorCard(m, []*ui.Node{
		monitorCardTitle("System", 0),
		{Kind: ui.KindRow, Height: 40, Gap: 8, Children: []*ui.Node{
			ccResourceGroup(snap, "cpu", "CPU", services.Selector{Source: services.SourceCPU}),
			ccResourceGroup(snap, "memory", "Memory", services.Selector{Source: services.SourceMemory}),
		}},
	})
	sysmon.Height = 88
	left := &ui.Node{Kind: ui.KindColumn, Width: 356, Height: 184, Gap: 8,
		Children: []*ui.Node{clockWeather, sysmon}}

	battery := ccDash
	if b := snap.Battery; b != nil && b.Present && b.ChargeValid {
		battery = fmt.Sprintf("%.0f%%", b.Charge*100)
	}
	profile := ccDash
	if h != nil && h.profileActive != "" {
		profile = powerProfileLabel(h.profileActive)
	}
	mute := ccQuickTile("volume_off", "Mute", ccPercent(audio.Level, audioOK), "cc:mute", audio.Muted)
	if !audioOK {
		ccDisable(mute)
	}
	nextProfile := hProfileNext(h)
	profileTile := ccQuickTile("balance", "Profile", profile, "cc:profile:"+nextProfile, false)
	profileTile.Name = "Power profile"
	if nextProfile == "" {
		ccDisable(profileTile)
	}
	dndTile := ccQuickTile("do_not_disturb_on", "DND", ccOnOff(dnd), "cc:dnd", dnd)
	dndTile.Name = "Do not disturb"
	right := &ui.Node{Kind: ui.KindColumn, Width: 228, Height: 184, Gap: 8, Children: []*ui.Node{
		{Kind: ui.KindRow, Height: 88, Gap: 8, Children: []*ui.Node{
			mute,
			dndTile,
		}},
		{Kind: ui.KindRow, Height: 88, Gap: 8, Children: []*ui.Node{
			profileTile,
			ccBatteryTile(battery),
		}},
	}}
	split := &ui.Node{Kind: ui.KindRow, Height: 184, Gap: 12, Children: []*ui.Node{left, right}}

	sliders := &ui.Node{Kind: ui.KindColumn, Height: 116, Gap: 12, Children: []*ui.Node{
		ccSlider(m, "volume_up", "Volume", "cc:volume", audio.Level, audioOK),
		ccSlider(m, "brightness_high", "Brightness", "cc:brightness", brightness.Level, brightnessOK),
	}}
	return &ui.Node{Kind: ui.KindColumn, Height: 480, Gap: 12,
		Children: []*ui.Node{identityCard, togglePill, split, sliders}}
}

func ccText(s string) string {
	if strings.TrimSpace(s) == "" {
		return ccDash
	}
	return s
}

func ccClock(now time.Time) string {
	if now.IsZero() {
		return ccDash
	}
	return now.Format("15:04")
}

func ccDate(now time.Time) string {
	if now.IsZero() {
		return ccDash
	}
	return now.Format("Monday, 2 January")
}

func ccWeatherSummary(reading services.Reading) string {
	if !reading.Observed {
		return ccDash
	}
	return fmt.Sprintf("%c %.0f%s", render.IconRune(reading.Code), reading.Temperature, unitSuffix(reading.Unit))
}

func ccPercent(value int, ok bool) string {
	if !ok {
		return ccDash
	}
	return fmt.Sprintf("%d%%", value)
}

func ccQuickAccessButton(width int, icon, label, action string, selected bool) *ui.Node {
	n := ccSegment(icon, label, action, selected)
	n.Width, n.Height, n.Shape = width, 48, ui.ShapeStadium
	if !selected {
		n.Fill = ui.FillContainerHigh
	}
	return n
}

func ccResourceGroup(snap services.Snapshot, id, label string, sel services.Selector) *ui.Node {
	value, ok := snap.Fraction(sel)
	if !ok {
		value = 0
	}
	icon, _ := render.GaugeIconName(id)
	return &ui.Node{Kind: ui.KindRow, Height: 40, Gap: 8, Children: []*ui.Node{
		{Kind: ui.KindRadialGauge, Width: 40, Height: 40, Icon: icon, Value: value, Absent: !ok},
		{Kind: ui.KindColumn, Gap: 2, Children: []*ui.Node{
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleCaption},
			{Kind: ui.KindText, Text: ccPercent(int(value*100+0.5), ok), Tabular: true},
		}},
	}}
}

func ccOnOff(on bool) string {
	if on {
		return "On"
	}
	return "Off"
}

func ccSegment(icon, label, action string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Action: action, Name: label, Role: "button", Focusable: true,
		Gap: 6, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
		},
	}
	if selected {
		n.State |= ui.StateSelected
		n.Fill = ui.FillAccent
	}
	return n
}

func ccQuickTile(icon, label, value, action string, selected bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindCapsule, Width: 110, Height: 88, Padding: 12,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Action: action, Name: label, Role: "button", Focusable: true,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: 4, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: value, TextRole: theme.RoleCaption, Tabular: true},
		}}},
	}
	if selected {
		n.State |= ui.StateSelected
		n.Fill = ui.FillAccent
	}
	return n
}

func ccBatteryTile(value string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindCapsule, Width: 110, Height: 88, Padding: 12,
		Shape: ui.ShapeCard, Stroke: 1, StrokeFill: ui.FillOutline,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: 4, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "battery_full"},
			{Kind: ui.KindText, Text: "Battery", TextRole: theme.RoleLabel},
			{Kind: ui.KindText, Text: value, TextRole: theme.RoleCaption, Tabular: true},
		}}},
	}
}

func ccDisable(n *ui.Node) {
	if n == nil {
		return
	}
	n.Action = ""
	n.State |= ui.StateDisabled
}

func ccSlider(m theme.Metrics, icon, label, action string, value int, ok bool) *ui.Node {
	control := &ui.Node{
		Kind: ui.KindSlider, Action: action, Name: label, Role: "slider", Focusable: true,
		Value: float64(value), Min: 0, Max: 100, Step: 5, Width: 360, Absent: !ok,
	}
	if !ok {
		control.State |= ui.StateDisabled
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Height: 52, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon},
			{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
			control,
			{Kind: ui.KindText, Text: ccPercent(value, ok), Width: 44, MinWidthText: "100%", Tabular: true},
		}}},
	}
}

func hProfileNext(h *PanelHost) string {
	if h == nil || len(h.profiles) == 0 {
		return ""
	}
	for i, name := range h.profiles {
		if name == h.profileActive {
			return h.profiles[(i+1)%len(h.profiles)]
		}
	}
	return h.profiles[0]
}

func ccWeather(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	reading := services.Reading{}
	location := ccDash
	if r != nil {
		reading = r.reading
		location = fmt.Sprintf("%.2f°, %.2f°", r.cfg.Weather.Latitude, r.cfg.Weather.Longitude)
	}
	icon, temperature, condition, fetched := "cloud", ccDash, ccDash, ccDash
	if reading.Observed {
		icon = ccWeatherIcon(reading.Code)
		temperature = fmt.Sprintf("%.0f%s", reading.Temperature, unitSuffix(reading.Unit))
		condition = ccWeatherCondition(reading.Code)
		if !reading.FetchedAt.IsZero() {
			fetched = "Updated " + reading.FetchedAt.Format("15:04")
		}
	}
	today := monitorCard(m, []*ui.Node{
		monitorCardTitle("Today", 0),
		{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconLarge},
		{Kind: ui.KindText, Text: temperature, TextRole: theme.RoleTitle, Tabular: true},
		{Kind: ui.KindText, Text: condition, TextRole: theme.RoleLabel},
		{Kind: ui.KindText, Text: location, TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: fetched, TextRole: theme.RoleCaption},
	})
	today.Height = 336

	forecast := &ui.Node{Kind: ui.KindRow, Height: 132, Gap: 8}
	for i := 0; i < 4; i++ {
		var day *services.Day
		if i+1 < len(reading.Daily) {
			day = &reading.Daily[i+1]
		}
		forecast.Children = append(forecast.Children, ccForecastDay(m, day, reading.Unit))
	}
	return &ui.Node{Kind: ui.KindColumn, Height: 480, Gap: 12, Children: []*ui.Node{today, forecast}}
}

func ccForecastDay(m theme.Metrics, day *services.Day, unit services.Unit) *ui.Node {
	label, icon, temperature := ccDash, "cloud", ccDash
	if day != nil {
		if date, err := time.Parse("2006-01-02", day.Date); err == nil {
			label = date.Format("Mon")
		} else {
			label = ccText(day.Date)
		}
		icon = ccWeatherIcon(day.Code)
		temperature = fmt.Sprintf("%.0f° / %.0f°", day.High, day.Low)
		if unit == services.UnitFahrenheit {
			temperature += "F"
		} else {
			temperature += "C"
		}
	}
	card := monitorCard(m, []*ui.Node{
		{Kind: ui.KindText, Text: label, TextRole: theme.RoleLabel},
		{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconLarge},
		{Kind: ui.KindText, Text: temperature, TextRole: theme.RoleCaption, Tabular: true},
	})
	card.Width, card.Height = 143, 132
	return card
}

func ccWeatherIcon(code int) string {
	switch {
	case code == 0:
		return "sunny"
	case code == 1 || code == 2:
		return "partly_cloudy_day"
	case code == 45 || code == 48:
		return "foggy"
	case code >= 51 && code <= 67 || code >= 80 && code <= 82:
		return "rainy"
	case code >= 71 && code <= 77 || code == 85 || code == 86:
		return "weather_snowy"
	case code >= 95 && code <= 99:
		return "thunderstorm"
	default:
		return "cloud"
	}
}

func ccWeatherCondition(code int) string {
	if word, ok := conditionWords[render.IconRune(code)]; ok {
		return word
	}
	return "Cloudy"
}

func ccAudio(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	state := services.AudioState{}
	ok := false
	if r != nil && r.audio != nil {
		state, ok = r.audio.CachedState()
	}
	mute := ccQuickTile("volume_off", "Mute", ccOnOff(state.Muted), "cc:mute", state.Muted)
	if !ok {
		ccDisable(mute)
	}
	rows := []*ui.Node{
		monitorCardTitle("Output", 0),
		ccSlider(m, "volume_up", "Volume", "cc:volume", state.Level, ok),
		mute,
	}
	if h != nil && h.errLabel != "" {
		rows = append([]*ui.Node{{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError}}, rows...)
	}
	card := monitorCard(m, rows)
	card.Height = 480
	return &ui.Node{Kind: ui.KindColumn, Height: 480, Children: []*ui.Node{card}}
}

func ccMonitor(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	snap := services.Snapshot{}
	history := map[services.Selector][]float64{}
	if r != nil {
		snap = r.sample
		if r.metrics != nil {
			history = r.historyLocked()
		}
	}
	network := ccNetworkSelector(snap, history)
	selectors := []struct {
		selector services.Selector
		label    string
		height   int
	}{
		{services.Selector{Source: services.SourceCPU}, "CPU", 154},
		{services.Selector{Source: services.SourceMemory}, "Memory", 154},
		{network, "Network", 156},
	}
	page := &ui.Node{Kind: ui.KindColumn, Height: 480, Gap: 8}
	for _, item := range selectors {
		card := ccMonitorMetricCard(m, item.label, item.selector, snap, history[item.selector])
		card.Height = item.height
		page.Children = append(page.Children, card)
	}
	return page
}

func ccNetworkSelector(snap services.Snapshot, history map[services.Selector][]float64) services.Selector {
	var candidates []string
	for selector := range history {
		if selector.Source == services.SourceNetwork && selector.Direction != "tx" {
			candidates = append(candidates, selector.Subject)
		}
	}
	if snap.Network != nil {
		for _, iface := range snap.Network.Interfaces {
			candidates = append(candidates, iface.Name)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return services.Selector{Source: services.SourceNetwork, Direction: "rx"}
	}
	return services.Selector{Source: services.SourceNetwork, Subject: candidates[0], Direction: "rx"}
}

func ccMonitorMetricCard(m theme.Metrics, label string, selector services.Selector, snap services.Snapshot, history []float64) *ui.Node {
	value, absent := formatMonitorMetric(selector, snap)
	if absent {
		value = ccDash
	}
	return monitorCard(m, []*ui.Node{
		monitorCardTitle(label, monitorIconRune(selector)),
		{Kind: ui.KindGraph, Values: monitorGraphValues(selector, history), Absent: absent},
		monitorLegend(selector, snap, value),
	})
}

func ccPower(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	var snap services.Snapshot
	locker := ""
	if r != nil {
		snap = r.sample
		locker = r.cfg.Session.Locker
	}
	errLabel := ""
	if h != nil {
		errLabel = h.errLabel
	}
	battery := ccPowerBattery(m, snap, errLabel)
	profiles := ccPowerProfiles(m, h)
	actions := ccSessionActions(m, locker)
	return &ui.Node{Kind: ui.KindColumn, Height: 480, Gap: 8,
		Children: []*ui.Node{battery, profiles, actions}}
}

func ccPowerBattery(m theme.Metrics, snap services.Snapshot, errLabel string) *ui.Node {
	rows := []*ui.Node{monitorCardTitle("Battery", 0)}
	if errLabel != "" {
		rows = append([]*ui.Node{{Kind: ui.KindText, Text: errLabel, Tone: ui.ToneError}}, rows...)
	}
	b := snap.Battery
	if b == nil || !b.Present || !b.ChargeValid {
		rows = append(rows, monitorKeyValue("Status", ccDash))
	} else {
		rows = append(rows,
			monitorKeyValue("Charge", fmt.Sprintf("%.0f%%", b.Charge*100)),
			monitorKeyValue("State", batteryStateLabel(b.State)))
		if b.TimeValid {
			rows = append(rows, monitorKeyValue("Remaining", batteryDuration(b.TimeRemaining)))
		}
	}
	card := monitorCard(m, rows)
	card.Height = 120
	return card
}

func ccPowerProfiles(m theme.Metrics, h *PanelHost) *ui.Node {
	rows := []*ui.Node{monitorCardTitle("Power profile", 0)}
	if h == nil || !h.profilesOK || len(h.profiles) == 0 {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "powerprofilesctl unavailable", TextRole: theme.RoleCaption})
	} else {
		segments := &ui.Node{Kind: ui.KindSegmented, Key: "cc-power-profiles", Gap: sessionSegmentGap}
		for _, name := range h.profiles {
			selected := name == h.profileActive
			icon := sessionProfileIcon(name)
			if selected {
				icon = "check"
			}
			children := []*ui.Node{}
			if icon != "" {
				children = append(children, &ui.Node{Kind: ui.KindIcon, Icon: icon, IconSize: m.IconProfile})
			}
			label := powerProfileLabel(name)
			children = append(children, &ui.Node{Kind: ui.KindText, Text: label})
			segment := &ui.Node{
				Kind: ui.KindButton, Action: "cc:profile:" + name,
				Name: label, Role: "tab", Focusable: true, Height: m.StandardControl,
				Gap: sessionSegmentContentGap, Padding: sessionSegmentPadding, Children: children,
			}
			if selected {
				segment.State |= ui.StateSelected
			}
			segments.Children = append(segments.Children, segment)
		}
		rows = append(rows, segments)
	}
	card := monitorCard(m, rows)
	card.Height = 112
	return card
}

func ccSessionActions(m theme.Metrics, locker string) *ui.Node {
	actions := []struct{ name, action, icon string }{
		{"Lock", "session-lock", "lock"},
		{"Log out", "session-logout", "logout"},
		{"Suspend", "session-suspend", "bedtime"},
		{"Reboot", "session-reboot", "restart_alt"},
		{"Power off", "session-poweroff", "power_settings_new"},
	}
	if locker == "" {
		actions = actions[1:]
	}
	rows := []*ui.Node{monitorCardTitle("Session", 0)}
	for _, action := range actions {
		n := &ui.Node{
			Kind: ui.KindButton, Action: action.action, Name: action.name, Role: "button", Focusable: true,
			Height: m.CompactControl, Gap: 8, Padding: m.CardPadding,
			Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: action.icon, IconSize: m.IconNormal},
				{Kind: ui.KindText, Text: action.name},
			},
		}
		if action.action == "session-reboot" || action.action == "session-poweroff" {
			n.Fill, n.Tone = ui.FillOutline, ui.ToneError
		}
		rows = append(rows, n)
	}
	card := monitorCard(m, rows)
	card.Height = 232
	return card
}

func ccCalendar(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	now := time.Time{}
	if r != nil {
		now = r.now
	}
	if now.IsZero() {
		now = time.Now()
	}
	view := now.AddDate(0, h.monthDelta, 0)
	grid := calendarGrid(view)
	rows := []*ui.Node{{
		Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			calendarArrow("chevron_left", "cal-prev", "Previous month", h.theme),
			{Kind: ui.KindText, Text: view.Format("January 2006"), TextRole: theme.RoleTitle},
			calendarArrow("chevron_right", "cal-next", "Next month", h.theme),
		},
	}}
	weekdays := &ui.Node{Kind: ui.KindRow, Gap: 4}
	for _, day := range []string{"S", "M", "T", "W", "T", "F", "S"} {
		weekdays.Children = append(weekdays.Children, &ui.Node{Kind: ui.KindText, Text: day, Width: 74, CenterX: true})
	}
	rows = append(rows, weekdays)
	rowHeight := max((480-2*m.CardPadding-m.StandardControl-28)/max(len(grid.Weeks), 1), 32)
	for _, week := range grid.Weeks {
		row := &ui.Node{Kind: ui.KindRow, Height: rowHeight, Gap: 4}
		for _, cell := range week {
			day := &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%d", cell.Day), Width: 74, CenterX: true, Tabular: true}
			if cell.Today {
				day.Tone = ui.ToneAccent
			}
			row.Children = append(row.Children, day)
		}
		rows = append(rows, row)
	}
	card := monitorCard(m, rows)
	card.Height = 480
	return &ui.Node{Kind: ui.KindColumn, Height: 480, Children: []*ui.Node{card}}
}

func ccNotifications(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	now, dnd := time.Now(), false
	var active []protocol.Notification
	var history []protocol.HistoryEntry
	if r != nil {
		now = r.clockNow()
		_, dnd = r.dndStateAt(now)
		if r.notify != nil {
			r.notify.mu.Lock()
			for _, notification := range r.notify.active {
				active = append(active, notification)
			}
			history = append(history, r.notify.history...)
			r.notify.mu.Unlock()
		}
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Timestamp.After(history[j].Timestamp) })
	controls := &ui.Node{
		Kind: ui.KindCapsule, Height: 52, Padding: m.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			ccSegment("do_not_disturb_on", "Do not disturb", "cc:dnd", dnd),
			ccSegment("delete", "Clear", "notify:center:clear", false),
		}}},
	}
	var cards []*ui.Node
	for _, group := range activeGroups(active) {
		cards = append(cards, ActiveGroupCard(group, now, false, nil, false))
	}
	for _, entry := range history {
		cards = append(cards, HistoryCard(entry, now, nil, false))
	}
	if len(cards) == 0 {
		empty := monitorCard(m, []*ui.Node{
			{Kind: ui.KindIcon, Icon: "notifications", IconSize: m.IconLarge},
			{Kind: ui.KindText, Text: "Nothing to see here", TextRole: theme.RoleLabel, CenterX: true},
		})
		empty.Height = 416
		cards = append(cards, empty)
	}
	list := &ui.Node{Kind: ui.KindScroll, Height: 416, Gap: 8, Children: cards}
	return &ui.Node{Kind: ui.KindColumn, Height: 480, Gap: 12, Children: []*ui.Node{controls, list}}
}

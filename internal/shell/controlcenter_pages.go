package shell

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const ccDash = "—"

type ccIdentity struct {
	Name, Account, Uptime string
}

func readCCIdentity() ccIdentity {
	facts := readMachineFacts()
	host, _ := os.Hostname()
	name, account := "", ""
	if current, err := user.Current(); err == nil {
		name = strings.TrimSpace(current.Name)
		if name == "" {
			name = current.Username
		}
		account = current.Username
	}
	if account != "" && host != "" {
		account += "@" + host
	}
	return ccIdentity{Name: name, Account: account, Uptime: facts.Uptime}
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

	identityCard := monitorCard(m, []*ui.Node{
		{Kind: ui.KindText, Text: ccText(identity.Name), TextRole: theme.RoleTitle},
		{Kind: ui.KindText, Text: ccText(identity.Account), TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: "Uptime " + ccText(identity.Uptime), TextRole: theme.RoleCaption},
	})
	identityCard.Height = 96

	togglePill := &ui.Node{
		Kind: ui.KindCapsule, Height: 48, Padding: h.theme.Metrics.CapsulePadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeLarge,
		Children: []*ui.Node{{
			Kind: ui.KindSegmented, Height: m.StandardControl, Children: []*ui.Node{
				ccSegment("coffee", "Caffeine", "cc:caffeine", caffeine),
				ccSegment("wallpaper", "Wallpaper", "cc:wallpaper", false),
			},
		}},
	}

	clockWeather := monitorCard(m, []*ui.Node{
		monitorCardTitle(ccClock(now), 0),
		{Kind: ui.KindText, Text: ccDate(now), TextRole: theme.RoleCaption},
		{Kind: ui.KindText, Text: ccWeatherSummary(reading)},
	})
	clockWeather.Height = 88
	cpu := ccMetric(snap, services.Selector{Source: services.SourceCPU})
	memory := ccMetric(snap, services.Selector{Source: services.SourceMemory})
	sysmon := monitorCard(m, []*ui.Node{
		monitorCardTitle("System", 0),
		{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "CPU " + cpu, Tabular: true},
			{Kind: ui.KindText, Text: "Memory " + memory, Tabular: true},
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
	profileTile := ccQuickTile("balance", "Power profile", profile, "cc:profile:"+nextProfile, false)
	if nextProfile == "" {
		ccDisable(profileTile)
	}
	right := &ui.Node{Kind: ui.KindColumn, Width: 228, Height: 184, Gap: 8, Children: []*ui.Node{
		{Kind: ui.KindRow, Height: 88, Gap: 8, Children: []*ui.Node{
			mute,
			ccQuickTile("do_not_disturb_on", "Do not disturb", ccOnOff(dnd), "cc:dnd", dnd),
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

func ccMetric(snap services.Snapshot, sel services.Selector) string {
	value, ok := snap.Fraction(sel)
	return ccPercent(int(value*100+0.5), ok)
}

func ccPercent(value int, ok bool) string {
	if !ok {
		return ccDash
	}
	return fmt.Sprintf("%d%%", value)
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
		Kind: ui.KindButton, Width: 110, Height: 88, Padding: 12,
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

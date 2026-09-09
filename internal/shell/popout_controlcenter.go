package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

const panelControlCenterAction = "panel:control-center"

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

func controlCentreTree(_ *Registry, _ *PanelHost) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Gap: 16, Padding: 16}
}

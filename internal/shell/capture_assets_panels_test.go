package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

func TestAssetSettings(t *testing.T) {
	for _, section := range []string{"Appearance", "Bar", "Panels", "Wallpaper", "Terminal Art", "Night Light", "Weather", "Monitor"} {
		t.Run(section, func(t *testing.T) {
			captureAssetPanel(t, PanelSettings, "settings", assetSlug(section), func(reg *Registry, h *PanelHost) {
				assetBase(t, reg)
				if !contains(settings.SectionNames(), section) {
					t.Fatalf("no settings section %q", section)
				}
				h.section = section
			})
		})
	}
}

func TestAssetPanels(t *testing.T) {
	for _, p := range []struct {
		id      PanelID
		surface string
	}{
		{PanelMonitor, "system-monitor"}, {PanelWeather, "weather"}, {PanelClock, "clock"}, {PanelSession, "session"},
	} {
		t.Run(p.surface, func(t *testing.T) {
			captureAssetPanel(t, p.id, p.surface, "default", func(reg *Registry, h *PanelHost) { assetBase(t, reg) })
		})
	}
}

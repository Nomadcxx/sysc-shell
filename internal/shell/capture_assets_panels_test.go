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

func TestAssetProcessesPage(t *testing.T) {
	captureAssetPanel(t, PanelMonitor, "system-monitor", "processes", func(reg *Registry, h *PanelHost) {
		assetBase(t, reg)
		h.monitorPage = monitorPageProcesses
	})
}

func TestAssetPanels(t *testing.T) {
	for _, p := range []struct {
		id      PanelID
		surface string
	}{
		{PanelWeather, "weather"}, {PanelClock, "clock"}, {PanelSession, "session"},
	} {
		t.Run(p.surface, func(t *testing.T) {
			captureAssetPanel(t, p.id, p.surface, "default", func(reg *Registry, h *PanelHost) { assetBase(t, reg) })
		})
	}
}

// TestAssetSystemMonitorSystemPage is the monitor's System page: the readings
// and graphs, beside the Processes page that TestAssetPanels paints.
func TestAssetSystemMonitorSystemPage(t *testing.T) {
	captureAssetPanel(t, PanelMonitor, "system-monitor", "system", func(reg *Registry, h *PanelHost) {
		assetBase(t, reg)
		h.monitorPage = monitorPageMetrics
	})
}

package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

// TestEverySettingsPageLaysOutEverywhere opens every section and page at the
// fitted size on three outputs and three scales. A layout error closes the
// surface in production (the audio panel did exactly that), so any error here
// is a failure a user would see.
func TestEverySettingsPageLaysOutEverywhere(t *testing.T) {
	for _, out := range [][2]int{{1280, 720}, {1536, 864}, {3440, 1440}} {
		reg := newPanelRegistry(t)
		withTestBar(t, reg, 7, reg.cfg)
		if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: out[0], OutH: out[1]}); err != nil {
			t.Fatal(err)
		}
		panel := drainAux(t, reg, 2)[1].Open
		for _, section := range settings.SectionNames() {
			pages := settings.SectionPages(section)
			if len(pages) == 0 {
				pages = []string{""}
			}
			for _, page := range pages {
				for _, scale := range []int{120, 150, 180} {
					reg.mu.Lock()
					h := reg.panelHosts[PanelSettings]
					h.section, h.settingsPage, h.scale120 = section, page, scale
					reg.rebuildPanel(h)
					reg.mu.Unlock()
					if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), scale); err != nil {
						t.Errorf("%dx%d %s/%s @%d: %v", out[0], out[1], section, page, scale, err)
					}
				}
			}
		}
	}
}

package shell

import "testing"

// fixtureIdentity is the account every asset shows: nobody real.
var fixtureIdentity = ccIdentity{Name: "Alex Rivera", Account: "alex@workstation", Uptime: "3 hours"}

// assetBase puts the shared fixture state on a registry: the account, the
// clock, a workstation under light use, a playing track and a mild day.
func assetBase(t *testing.T, reg *Registry) {
	t.Helper()
	reg.controlIdentity = fixtureIdentity
	reg.machineFacts = assetMachine
	reg.usernames = newUsernameCache(func(uid string) (string, error) {
		if uid == "0" {
			return "root", nil
		}
		return "alex", nil
	})
	reg.setMedia(nil)
	reg.now, reg.sample, reg.mediaState, reg.reading = assetNow, assetSample(), assetMedia, assetWeather()
	stubWpctl(t, reg, "0.62")
}

func TestAssetControlCentre(t *testing.T) {
	for _, section := range ccSections {
		switch section.ID {
		case "network", "bluetooth", "notifications": // need a fake service; an empty state is not worth shipping
			continue
		}
		t.Run(section.ID, func(t *testing.T) {
			captureAssetPanel(t, PanelControlCenter, "control-centre", section.ID, func(reg *Registry, h *PanelHost) {
				assetBase(t, reg)
				if section.ID == "calendar" {
					withCalendarPlugin(t, reg, assetCalendarState)
				}
				h.section = section.ID
			})
		})
	}
}

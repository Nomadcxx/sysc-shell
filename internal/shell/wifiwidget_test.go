package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func wireless(strength uint8) services.NetworkState {
	return services.NetworkState{
		WirelessEnabled: true,
		Kind:            services.ConnWireless,
		Connected:       true,
		Strength:        strength,
	}
}

func TestWifiWidgetGlyphFollowsBand(t *testing.T) {
	icon := &ui.Node{Kind: ui.KindIcon}
	cases := []struct {
		name  string
		state services.NetworkState
		want  string
	}{
		{
			"radio off",
			services.NetworkState{WirelessEnabled: false},
			"wifi_off",
		},
		{
			"wired link",
			services.NetworkState{WirelessEnabled: true, Kind: services.ConnWired, Connected: true},
			"lan",
		},
		{
			// On with no association is not the same fact as off.
			"enabled but not connected",
			services.NetworkState{WirelessEnabled: true, Kind: services.ConnNone},
			"signal_wifi_0_bar",
		},
		{"band 4", wireless(92), "signal_wifi_4_bar"},
		{"band 3", wireless(74), "network_wifi_3_bar"},
		{"band 2", wireless(40), "network_wifi_2_bar"},
		{"band 1", wireless(20), "network_wifi_1_bar"},
		{"band 0", wireless(5), "signal_wifi_0_bar"},
	}
	for _, c := range cases {
		refreshWifiWidget(icon, barView{Network: c.state})
		if icon.Icon != c.want {
			t.Errorf("%s: glyph %q, want %q", c.name, icon.Icon, c.want)
		}
	}
}

// Every glyph the widget can emit must be in the embedded subset. A name the
// font does not carry shapes to nothing and paints an invisible control, which
// no layout assertion would catch.
func TestWifiWidgetGlyphsAreInTheSubset(t *testing.T) {
	for _, name := range []string{
		"wifi_off", "signal_wifi_0_bar", "network_wifi_1_bar",
		"network_wifi_2_bar", "network_wifi_3_bar", "signal_wifi_4_bar", "lan",
	} {
		if !render.ValidMaterialIcon(name) {
			t.Errorf("%q is not in the subset", name)
		}
	}
}

func TestWifiWidgetRefreshReportsChangeOnlyWhenGlyphMoves(t *testing.T) {
	icon := &ui.Node{Kind: ui.KindIcon}
	if !refreshWifiWidget(icon, barView{Network: wireless(92)}) {
		t.Fatal("first refresh must report a change")
	}
	if refreshWifiWidget(icon, barView{Network: wireless(88)}) {
		t.Error("same band must not report a change: the bar repaints on every tick")
	}
	if !refreshWifiWidget(icon, barView{Network: wireless(40)}) {
		t.Error("a band change must report a change")
	}
}

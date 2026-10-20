package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestBluetoothWidgetGlyphStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state services.BluetoothState
		want  string
	}{
		{name: "unavailable", want: "bluetooth_disabled"},
		{name: "powered off", state: services.BluetoothState{Available: true}, want: "bluetooth_disabled"},
		{name: "powered idle", state: services.BluetoothState{
			Available: true, Adapter: services.BluetoothAdapter{Powered: true},
		}, want: "bluetooth"},
		{name: "connected", state: services.BluetoothState{
			Available: true, Adapter: services.BluetoothAdapter{Powered: true},
			Devices: []services.BluetoothDevice{{Connected: true}},
		}, want: "bluetooth_connected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bluetoothGlyph(tc.state); got != tc.want {
				t.Fatalf("bluetoothGlyph = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBluetoothWidgetBuildsFromConfigAndRefreshes(t *testing.T) {
	widgets := buildWidgets([]config.Item{{ID: "bluetooth"}}, 6, standardMetrics())
	if len(widgets) != 1 || widgets[0].inner == nil {
		t.Fatalf("widgets = %+v, want one wrapped widget", widgets)
	}
	if widgets[0].inner.Action != panelBluetoothAction {
		t.Fatalf("action = %q, want %q", widgets[0].inner.Action, panelBluetoothAction)
	}
	if widgets[0].tooltip != "Bluetooth" {
		t.Fatalf("tooltip = %q, want Bluetooth", widgets[0].tooltip)
	}
	if widgets[0].inner.Kind != ui.KindRow || len(widgets[0].inner.Children) != 2 {
		t.Fatalf("Bluetooth row = %+v, want icon and text", widgets[0].inner)
	}
	icon, percentage := widgets[0].inner.Children[0], widgets[0].inner.Children[1]
	if !widgets[0].refresh(barView{Bluetooth: services.BluetoothState{
		Available: true, Adapter: services.BluetoothAdapter{Powered: true},
	}}) {
		t.Fatal("first Bluetooth refresh must report a change")
	}
	if got := icon.Icon; got != "bluetooth" {
		t.Fatalf("idle icon = %q, want bluetooth", got)
	}
	if widgets[0].inner.Gap != 0 {
		t.Fatalf("empty battery row gap = %d, want 0", widgets[0].inner.Gap)
	}
	connected := barView{Bluetooth: services.BluetoothState{
		Available: true, Adapter: services.BluetoothAdapter{Powered: true},
		Devices: []services.BluetoothDevice{{Connected: true}},
	}}
	if !widgets[0].refresh(connected) {
		t.Fatal("connected state must report a glyph change")
	}
	if got := icon.Icon; got != "bluetooth_connected" {
		t.Fatalf("connected icon = %q, want bluetooth_connected", got)
	}
	if percentage.Text != "" {
		t.Fatalf("missing battery rendered %q, want empty", percentage.Text)
	}
	if widgets[0].refresh(connected) {
		t.Fatal("unchanged Bluetooth state reported a change")
	}
}

func TestBluetoothWidgetGlyphsAreInTheSubset(t *testing.T) {
	for _, name := range []string{"bluetooth_disabled", "bluetooth", "bluetooth_connected"} {
		if !render.ValidMaterialIcon(name) {
			t.Errorf("%q is not in the subset", name)
		}
	}
}

func TestBluetoothWidgetShowsLowestConnectedBatteryAndHidesUnavailableValues(t *testing.T) {
	w := buildBluetoothWidget()
	if w.node.Kind != ui.KindRow || len(w.node.Children) != 2 {
		t.Fatalf("Bluetooth widget node = %+v, want icon-and-text row", w.node)
	}
	icon, percentage := w.node.Children[0], w.node.Children[1]
	if icon.Kind != ui.KindIcon || percentage.Kind != ui.KindText || !percentage.Tabular {
		t.Fatalf("Bluetooth row children = %+v, want icon and tabular text", w.node.Children)
	}
	if w.node.Action != panelBluetoothAction || w.node.Name != "Bluetooth" || w.node.Role != "button" {
		t.Fatalf("Bluetooth action/accessibility = %+v, want panel action on named row", w.node)
	}

	low, high, disconnected := uint8(24), uint8(66), uint8(3)
	connected := services.BluetoothState{
		Available: true, Adapter: services.BluetoothAdapter{Powered: true},
		Devices: []services.BluetoothDevice{
			{Connected: true, Battery: &high},
			{Connected: true, Battery: &low},
			{Connected: false, Battery: &disconnected},
		},
	}
	if !w.refresh(barView{Bluetooth: connected}) {
		t.Fatal("connected device batteries did not invalidate the widget")
	}
	if icon.Icon != "bluetooth_connected" || percentage.Text != "24%" {
		t.Fatalf("connected row = %q %q, want bluetooth_connected 24%%", icon.Icon, percentage.Text)
	}
	if w.node.Gap != theme.MarginXS {
		t.Fatalf("connected row gap = %d, want %d", w.node.Gap, theme.MarginXS)
	}

	zero := uint8(0)
	if !w.refresh(barView{Bluetooth: services.BluetoothState{
		Available: true, Adapter: services.BluetoothAdapter{Powered: true},
		Devices: []services.BluetoothDevice{{Connected: true, Battery: &zero}},
	}}) {
		t.Fatal("zero battery percentage did not invalidate the widget")
	}
	if percentage.Text != "0%" {
		t.Fatalf("zero battery rendered %q, want 0%%", percentage.Text)
	}

	if !w.refresh(barView{}) {
		t.Fatal("unavailable Bluetooth state did not invalidate the widget")
	}
	if icon.Icon != "bluetooth_disabled" || percentage.Text != "" {
		t.Fatalf("unavailable row = %q %q, want disabled icon and no percentage", icon.Icon, percentage.Text)
	}
	if w.node.Gap != 0 {
		t.Fatalf("unavailable row gap = %d, want 0", w.node.Gap)
	}

	w.refresh(barView{Bluetooth: services.BluetoothState{
		Available: true, Devices: []services.BluetoothDevice{{Connected: true, Battery: &low}},
	}})
	if percentage.Text != "" {
		t.Fatalf("powered-off Bluetooth rendered %q, want no percentage", percentage.Text)
	}
}

func TestBluetoothWidgetUsesAnIconAndTextRow(t *testing.T) {
	widgets := buildWidgets([]config.Item{{ID: "bluetooth"}}, 6, standardMetrics())
	if len(widgets) == 0 || widgets[0].inner == nil || widgets[0].inner.Kind != ui.KindRow {
		t.Fatalf("Bluetooth widget inner node = %+v, want row", widgets)
	}
	if len(widgets[0].inner.Children) != 2 || widgets[0].inner.Children[0].Kind != ui.KindIcon || widgets[0].inner.Children[1].Kind != ui.KindText {
		t.Fatalf("Bluetooth row children = %+v, want icon and text", widgets[0].inner.Children)
	}
}

func TestBluetoothBarRoutesLeftToStandaloneAndRightToControlCentreBluetooth(t *testing.T) {
	r := newPanelRegistry(t)
	bar := &Bar{conn: "DP-1"}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)
	r.mu.Unlock()

	if !bar.onAction(panelBluetoothAction, buttonLeft) {
		t.Fatal("left-click did not open the standalone Bluetooth panel")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	standalone := r.panelHosts[PanelBluetooth]
	r.mu.Unlock()
	if standalone == nil {
		t.Fatal("left-click did not create PanelBluetooth")
	}
	r.ClosePanel(PanelBluetooth)
	_ = drainAux(t, r, 2)

	if !bar.onAction(panelBluetoothAction, buttonRight) {
		t.Fatal("right-click did not open the Bluetooth control-centre page")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	control := r.panelHosts[PanelControlCenter]
	section := ""
	if control != nil {
		section = control.section
	}
	r.mu.Unlock()
	if control == nil || section != "bluetooth" {
		t.Fatalf("control-centre host = %v, section = %q, want Bluetooth page", control != nil, section)
	}
}

func TestBluetoothRightClickUsesTheClickedBarOutput(t *testing.T) {
	r := newPanelRegistry(t)
	clicked := &Bar{conn: "DP-1"}
	focused := &Bar{conn: "DP-2"}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: clicked, 8: focused}
	r.focused = "DP-2"
	r.bindBarPanelActionsLocked(7, clicked)
	r.mu.Unlock()

	if !clicked.onAction(panelBluetoothAction, buttonRight) {
		t.Fatal("right-click was not handled")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	output := uint32(0)
	section := ""
	if h != nil {
		output, section = h.output, h.section
	}
	r.mu.Unlock()
	if h == nil || output != 7 || section != "bluetooth" {
		t.Fatalf("control-centre host = %+v, output=%d, section=%q; want output 7 Bluetooth", h != nil, output, section)
	}
}

package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func bluetoothBodyTestState() services.BluetoothState {
	return services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
		Devices: []services.BluetoothDevice{
			{ID: "keyboard", Alias: "Keyboard", Address: "AA:01", Paired: true},
		},
	}
}

func TestBluetoothTreeHasStandaloneChromeAroundSharedBody(t *testing.T) {
	r := &Registry{bluetoothState: bluetoothBodyTestState()}
	h := &PanelHost{
		id:    PanelBluetooth,
		theme: DefaultTheme(),
		place: Placement{Panel: ui.Rect{W: 460, H: 560}},
	}

	root := bluetoothTree(r, h)
	if root.Kind != ui.KindColumn || len(root.Children) != 2 {
		t.Fatalf("Bluetooth root = %+v, want header and scroll body", root)
	}
	if findAction(root, "bluetooth-close") == nil {
		t.Fatal("standalone Bluetooth panel has no close control")
	}
	scroll := findKind(root, ui.KindScroll)
	if scroll == nil || findAction(scroll, "bluetooth-power") == nil {
		t.Fatalf("standalone Bluetooth body = %+v, want a scrollable shared body", scroll)
	}
	if !treeHasText(root, "Paired") || !treeHasText(root, "Keyboard") {
		t.Fatal("standalone Bluetooth panel lost the shared device body")
	}
}

func TestOpeningBluetoothPanelUsesItsStandaloneIdentityAndGeometry(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelBluetooth, 7, Trigger{BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440}); err != nil {
		t.Fatal(err)
	}
	requests := drainAux(t, r, 2)
	if requests[1].Open == nil || requests[1].Open.ID != "panel:bluetooth" {
		t.Fatalf("panel request = %+v, want PanelBluetooth", requests[1].Open)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelBluetooth]
	place := h.place
	r.mu.Unlock()
	if place.Panel != (ui.Rect{W: 460, H: 560}) {
		t.Fatalf("Bluetooth panel size = %+v, want 460x560", place.Panel)
	}
}

func TestPairingPromptAutoOpensBluetoothOnTheFocusedOutput(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.bars[7] = &Bar{conn: "DP-1"}
	r.focused = "DP-1"
	svc := &services.Bluetooth{}
	r.bluetooth = svc
	r.mu.Unlock()
	prompt := services.BluetoothPrompt{ID: 2, DeviceID: "keyboard", Kind: services.PairingPromptPIN}
	r.publishBluetoothSnapshot(svc, services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
		Prompt:    &prompt,
	})
	requests := drainAux(t, r, 2)
	if requests[1].Open == nil || requests[1].Open.ID != "panel:bluetooth" {
		t.Fatalf("auto-open request = %+v, want PanelBluetooth", requests[1].Open)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelBluetooth]
	gotPrompt := h != nil && findAction(h.root, "bluetooth-prompt-input") != nil
	r.mu.Unlock()
	if !gotPrompt {
		t.Fatal("auto-opened Bluetooth panel did not show the pairing prompt")
	}
}

func TestBluetoothControlCentrePageUsesTheSharedBody(t *testing.T) {
	section, ok := ccSectionFor("bluetooth")
	if !ok || !section.Enabled {
		t.Fatalf("Bluetooth control-centre section = %+v/%v, want enabled", section, ok)
	}
	r := &Registry{bluetoothState: bluetoothBodyTestState()}
	h := &PanelHost{id: PanelControlCenter, section: "bluetooth", theme: DefaultTheme()}
	page := ccPage(r, h)
	if findAction(page, "bluetooth-power") == nil || !treeHasText(page, "Keyboard") {
		t.Fatalf("Bluetooth control-centre page = %+v, want the shared body", page)
	}
	rail := ccRail(h)
	entry := findAction(rail, "section:bluetooth")
	if entry == nil || entry.State.Has(ui.StateDisabled) || entry.AriaDisabled {
		t.Fatalf("Bluetooth rail entry = %+v, want enabled destination", entry)
	}
}

func TestBluetoothPromptInputUsesTheRetainedCredentialFieldOnBothHosts(t *testing.T) {
	prompt := services.BluetoothPrompt{ID: 4, DeviceID: "keyboard", Kind: services.PairingPromptPIN}
	for _, tc := range []struct {
		name    string
		id      PanelID
		section string
	}{
		{name: "standalone", id: PanelBluetooth},
		{name: "control centre", id: PanelControlCenter, section: "bluetooth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Registry{bluetoothState: services.BluetoothState{
				Available: true,
				Adapter:   services.BluetoothAdapter{Powered: true},
				Prompt:    &prompt,
			}}
			h := &PanelHost{id: tc.id, section: tc.section, theme: DefaultTheme()}
			h.root = bluetoothBody(r, h)
			h.focus = ui.Focusables(h.root)
			h.roving.Count = len(h.focus)
			input := findAction(h.root, "bluetooth-prompt-input")
			if input == nil {
				t.Fatal("prompt input is missing")
			}
			h.setFocus(input)
			if !h.editField(r, func(field *ui.Field) { field.Insert("1") }) {
				t.Fatal("prompt input was not editable")
			}
			if h.bluetoothInput == nil || h.bluetoothInput.Text != "1" {
				t.Fatalf("retained prompt input = %+v, want 1", h.bluetoothInput)
			}
		})
	}
}

func TestBluetoothActivationHandlesBodyActionsBeforeGenericSettings(t *testing.T) {
	state := bluetoothBodyTestState()
	r := &Registry{bluetoothState: state, panelHosts: make(map[PanelID]*PanelHost)}
	h := &PanelHost{id: PanelBluetooth, theme: DefaultTheme()}
	h.root = bluetoothBody(r, h)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	r.panelHosts[h.id] = h
	details := findAction(h.root, "bluetooth-details:keyboard")
	if details == nil {
		t.Fatal("device details action is missing")
	}
	h.setFocus(details)
	if !h.activate(r) {
		t.Fatal("Bluetooth details action was not handled")
	}
	if h.bluetoothDetails != "keyboard" {
		t.Fatalf("details selection = %q, want keyboard", h.bluetoothDetails)
	}
}

func TestLeavingBluetoothControlCentreClearsItsSessionState(t *testing.T) {
	prompt := services.BluetoothPrompt{ID: 8, DeviceID: "keyboard", Kind: services.PairingPromptPIN}
	r := &Registry{
		bluetoothState: services.BluetoothState{Prompt: &prompt},
		panelHosts:     make(map[PanelID]*PanelHost),
	}
	h := &PanelHost{
		id:                 PanelControlCenter,
		section:            "bluetooth",
		theme:              DefaultTheme(),
		bluetoothDiscovery: true,
		bluetoothInput:     ui.NewField("1234"),
		bluetoothPromptID:  prompt.ID,
	}
	r.panelHosts[h.id] = h
	if !h.selectControlCentreSection(r, "home") {
		t.Fatal("leaving Bluetooth did not change section")
	}
	if h.bluetoothDiscovery || h.bluetoothInput != nil || r.bluetoothState.Prompt != nil {
		t.Fatalf("Bluetooth state after leaving = discovery=%v input=%v prompt=%+v", h.bluetoothDiscovery, h.bluetoothInput, r.bluetoothState.Prompt)
	}
}

func TestBluetoothUnavailableSnapshotClearsLocalDiscoveryState(t *testing.T) {
	r := &Registry{
		bluetooth:     &services.Bluetooth{},
		panelHosts:    make(map[PanelID]*PanelHost),
		invalidations: make(chan wayland.Invalidation, 1),
	}
	h := &PanelHost{id: PanelBluetooth, bluetoothDiscovery: true, theme: DefaultTheme()}
	r.panelHosts[h.id] = h
	r.publishBluetoothSnapshot(r.bluetooth, services.BluetoothState{})
	if h.bluetoothDiscovery {
		t.Fatal("unavailable Bluetooth snapshot left a discovery session visible")
	}
}

func TestBluetoothPromptStaysOnTheVisibleControlCentreHost(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	r.bluetooth = &services.Bluetooth{}
	r.bluetoothState = services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
	}
	r.mu.Unlock()
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	if err := r.selectPanelSectionLocked(PanelControlCenter, "bluetooth"); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	r.mu.Unlock()

	prompt := services.BluetoothPrompt{ID: 9, DeviceID: "keyboard", Kind: services.PairingPromptConfirmation, Code: "000042"}
	r.publishBluetoothSnapshot(r.bluetooth, services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
		Prompt:    &prompt,
	})
	r.mu.Lock()
	control := r.panelHosts[PanelControlCenter]
	standalone := r.panelHosts[PanelBluetooth]
	visible := control != nil && r.roots.owns(panelRoot(PanelControlCenter)) &&
		control.section == "bluetooth" && findAction(control.root, "bluetooth-prompt-accept") != nil
	r.mu.Unlock()
	if !visible || standalone != nil {
		t.Fatalf("prompt hosts = control visible %v, standalone %v; want one visible control-centre prompt", visible, standalone != nil)
	}
}

func TestBluetoothActionsFromBothHostsKeepTheSameCachedStateOnFailure(t *testing.T) {
	state := bluetoothBodyTestState()
	r := &Registry{
		bluetooth:      &services.Bluetooth{},
		bluetoothState: state,
		panelHosts:     make(map[PanelID]*PanelHost),
	}
	for _, tc := range []struct {
		name    string
		id      PanelID
		section string
	}{
		{name: "standalone", id: PanelBluetooth},
		{name: "control centre", id: PanelControlCenter, section: "bluetooth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &PanelHost{id: tc.id, section: tc.section, theme: DefaultTheme()}
			h.root = bluetoothBody(r, h)
			h.focus = ui.Focusables(h.root)
			h.roving.Count = len(h.focus)
			r.panelHosts[h.id] = h
			connect := findAction(h.root, "bluetooth-connect:keyboard")
			if connect == nil {
				t.Fatal("shared body has no Connect action")
			}
			r.mu.Lock()
			h.setFocus(connect)
			if !h.activate(r) {
				r.mu.Unlock()
				t.Fatal("Connect action was not handled")
			}
			r.mu.Unlock()
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				r.mu.Lock()
				errLabel := h.errLabel
				got := r.bluetoothState
				r.mu.Unlock()
				if errLabel != "" {
					if len(got.Devices) != len(state.Devices) || got.Devices[0].ID != state.Devices[0].ID {
						t.Fatalf("cached state changed after failure: %+v", got)
					}
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("Bluetooth action failure did not reach the visible host")
		})
	}
}

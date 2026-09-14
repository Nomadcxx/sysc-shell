package shell

import (
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestBluetoothBodySharesSectionsAndActionsAcrossHosts(t *testing.T) {
	state := services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true, Discovering: true},
		Devices: []services.BluetoothDevice{
			{ID: "available", Alias: "Mouse", Address: "AA:03", Icon: "mouse", RSSI: int16Ptr(-52)},
			{ID: "paired", Alias: "Keyboard", Address: "AA:02", Paired: true, Trusted: true, Icon: "keyboard"},
			{ID: "connected", Alias: "Headset", Address: "AA:01", Paired: true, Connected: true, Icon: "headset"},
		},
	}
	r := &Registry{bluetoothState: state}
	standalone := &PanelHost{id: PanelBluetooth, theme: DefaultTheme(), place: Placement{Panel: ui.Rect{W: 460, H: 560}}}
	control := &PanelHost{id: PanelControlCenter, theme: DefaultTheme(), place: Placement{Panel: ui.Rect{W: 700, H: 564}}}

	left := bluetoothBody(r, standalone)
	right := bluetoothBody(r, control)
	var leftActions, rightActions []string
	collectActions(left, "bluetooth-", &leftActions)
	collectActions(right, "bluetooth-", &rightActions)
	if !slices.Equal(leftActions, rightActions) {
		t.Fatalf("host actions differ:\nstandalone=%v\ncontrol-centre=%v", leftActions, rightActions)
	}
	for _, want := range []string{
		"bluetooth-disconnect:connected",
		"bluetooth-connect:paired",
		"bluetooth-pair:available",
		"bluetooth-details:connected",
		"bluetooth-details:paired",
		"bluetooth-details:available",
	} {
		if findAction(left, want) == nil {
			t.Errorf("shared body is missing %q", want)
		}
	}
	if !treeHasText(left, "Connected") || !treeHasText(left, "Paired") || !treeHasText(left, "Available") {
		t.Fatal("shared body is missing one of the device section labels")
	}
	if err := ui.ValidateKeys(left); err != nil {
		t.Fatalf("standalone body keys: %v", err)
	}
	if err := ui.ValidateKeys(right); err != nil {
		t.Fatalf("control-centre body keys: %v", err)
	}
}

func TestBluetoothBodyPlacesPromptFirstAndClearsCredentialInput(t *testing.T) {
	prompt := services.BluetoothPrompt{
		ID: 7, DeviceID: "paired", Alias: "Keyboard", Kind: services.PairingPromptPIN,
	}
	r := &Registry{bluetoothState: services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
		Prompt:    &prompt,
	}}
	h := &PanelHost{id: PanelBluetooth, theme: DefaultTheme(), bluetoothInput: ui.NewField("1234")}
	h.bluetoothPromptID = prompt.ID
	root := bluetoothBody(r, h)
	if findAction(root, "bluetooth-prompt-input") == nil {
		t.Fatal("PIN prompt has no input")
	}
	if got := findAction(root, "bluetooth-prompt-input").Text; got != "1234" {
		t.Fatalf("prompt input = %q, want retained active value", got)
	}
	if first := firstBluetoothDeviceAction(root); first == "" {
		t.Fatal("prompt body lost device actions")
	}

	newPrompt := prompt
	newPrompt.ID++
	r.bluetoothState.Prompt = &newPrompt
	root = bluetoothBody(r, h)
	if got := findAction(root, "bluetooth-prompt-input").Text; got != "" {
		t.Fatalf("new prompt retained old credential %q", got)
	}

	r.bluetoothState.Prompt = nil
	_ = bluetoothBody(r, h)
	if h.bluetoothInput != nil && h.bluetoothInput.Text != "" {
		t.Fatalf("cleared prompt retained credential %q", h.bluetoothInput.Text)
	}
}

func TestBluetoothPairActionDisablesWithoutReadyAgent(t *testing.T) {
	r := &Registry{bluetoothState: services.BluetoothState{
		Available: true,
		Adapter:   services.BluetoothAdapter{Powered: true},
		Devices:   []services.BluetoothDevice{{ID: "new", Alias: "Keyboard"}},
	}}
	root := bluetoothBody(r, &PanelHost{id: PanelBluetooth, theme: DefaultTheme()})
	pair := findAction(root, "bluetooth-pair:new")
	if pair == nil || !pair.State.Has(ui.StateDisabled) {
		t.Fatalf("pair action = %+v, want disabled without a ready agent", pair)
	}
}

func TestBluetoothPromptOperationDoesNotWaitBehindBlockingPair(t *testing.T) {
	r := &Registry{panelHosts: make(map[PanelID]*PanelHost)}
	h := &PanelHost{id: PanelBluetooth, theme: DefaultTheme()}
	r.panelHosts[h.id] = h
	blocked := make(chan struct{})
	started := make(chan struct{})
	r.queueBluetooth(func() error {
		close(started)
		<-blocked
		return nil
	}, nil)
	<-started

	reached := make(chan struct{})
	// Prompt responses use scheduleBluetooth. A real Device1.Pair can wait for
	// that response, so it must not sit behind the serialized discovery queue.
	r.scheduleBluetooth(h, "prompt", func() error {
		close(reached)
		return nil
	}, nil, nil)
	select {
	case <-reached:
	case <-time.After(100 * time.Millisecond):
		close(blocked)
		t.Fatal("prompt operation waited behind a blocking Bluetooth operation")
	}
	close(blocked)
}

func int16Ptr(value int16) *int16 { return &value }

func firstBluetoothDeviceAction(root *ui.Node) string {
	var actions []string
	collectActions(root, "bluetooth-", &actions)
	for _, action := range actions {
		if len(action) > len("bluetooth-details:") && action != "bluetooth-power" &&
			action != "bluetooth-scan" && action != "bluetooth-prompt-input" {
			return action
		}
	}
	return ""
}

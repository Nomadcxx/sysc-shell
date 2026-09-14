package services

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestBluetoothReducerSelectsFirstAdapterAndProjectsItsDevices(t *testing.T) {
	r := newBluetoothReducer()
	state := r.replaceBluetoothObjects(map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath("/org/bluez/hci1"): {
			adapterInterface: {
				"Powered":  dbus.MakeVariant(true),
				"Pairable": dbus.MakeVariant(true),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0"): {
			adapterInterface: {
				"Powered":  dbus.MakeVariant(false),
				"Pairable": dbus.MakeVariant(true),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0/dev_AA"): {
			deviceInterface: {
				"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
				"Address": dbus.MakeVariant("AA:AA"),
				"Alias":   dbus.MakeVariant("Keyboard"),
				"Paired":  dbus.MakeVariant(true),
			},
		},
		dbus.ObjectPath("/org/bluez/hci1/dev_BB"): {
			deviceInterface: {
				"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci1")),
				"Address": dbus.MakeVariant("BB:BB"),
				"Alias":   dbus.MakeVariant("Other"),
			},
		},
	})

	if !state.Available {
		t.Fatal("state must be available with an adapter")
	}
	if state.Adapter.Powered {
		t.Fatal("selected hci0 adapter must be powered off")
	}
	if len(state.Devices) != 1 || state.Devices[0].Address != "AA:AA" {
		t.Fatalf("devices = %+v, want only hci0 device", state.Devices)
	}
	if state.Devices[0].ID == "" {
		t.Fatal("device must receive an opaque ID")
	}
}

func TestKeyboardDisplayRequestPinCodeAndValidation(t *testing.T) {
	agent := newKeyboardDisplay(time.Second)
	defer agent.Close()
	result := make(chan pairingStringResult, 1)
	go func() {
		value, err := agent.RequestPinCode(dbus.ObjectPath("/org/bluez/hci0/dev_AA"))
		result <- pairingStringResult{value: value, err: err}
	}()
	prompt := waitBluetoothPrompt(t, agent)
	if prompt.Kind != PairingPromptPIN || prompt.DeviceID == "" || prompt.ID == 0 {
		t.Fatalf("prompt = %+v, want PIN with generation and device", prompt)
	}
	if err := agent.Respond(prompt.ID, PairingResponse{Accept: true}); err == nil {
		t.Fatal("empty PIN must be rejected without closing the prompt")
	}
	if _, ok := agent.PendingPrompt(); !ok {
		t.Fatal("invalid PIN must leave the prompt open")
	}
	if err := agent.Respond(prompt.ID, PairingResponse{Accept: true, Value: "1234"}); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.value != "1234" {
		t.Fatalf("result = %+v, want PIN 1234", got)
	}
	if _, ok := agent.PendingPrompt(); ok {
		t.Fatal("PIN input must be cleared after submit")
	}
}

func TestKeyboardDisplayPasskeyConfirmationAndAuthorization(t *testing.T) {
	agent := newKeyboardDisplay(time.Second)
	defer agent.Close()

	passkeyResult := make(chan pairingPasskeyResult, 1)
	go func() {
		value, err := agent.RequestPasskey(dbus.ObjectPath("/org/bluez/hci0/dev_AA"))
		passkeyResult <- pairingPasskeyResult{value: value, err: err}
	}()
	prompt := waitBluetoothPrompt(t, agent)
	if prompt.Kind != PairingPromptPasskey {
		t.Fatalf("kind = %d, want passkey", prompt.Kind)
	}
	if err := agent.Respond(prompt.ID, PairingResponse{Accept: true, Value: "1000000"}); err == nil {
		t.Fatal("out-of-range passkey must be rejected")
	}
	if err := agent.Respond(prompt.ID, PairingResponse{Accept: true, Value: "000123"}); err != nil {
		t.Fatal(err)
	}
	if got := <-passkeyResult; got.err != nil || got.value != 123 {
		t.Fatalf("passkey result = %+v, want 123", got)
	}

	confirmationResult := make(chan *dbus.Error, 1)
	go func() {
		confirmationResult <- agent.RequestConfirmation(dbus.ObjectPath("/org/bluez/hci0/dev_AA"), 42)
	}()
	prompt = waitBluetoothPrompt(t, agent)
	if prompt.Kind != PairingPromptConfirmation || prompt.Code != "000042" {
		t.Fatalf("confirmation prompt = %+v", prompt)
	}
	if err := agent.Respond(prompt.ID, PairingResponse{}); err != nil {
		t.Fatal(err)
	}
	if err := <-confirmationResult; err == nil || err.Name != agentErrorCanceled {
		t.Fatalf("confirmation result = %v, want canceled", err)
	}

	authorizationResult := make(chan *dbus.Error, 1)
	go func() {
		authorizationResult <- agent.AuthorizeService(dbus.ObjectPath("/org/bluez/hci0/dev_AA"), "0000110b-0000-1000-8000-00805f9b34fb")
	}()
	prompt = waitBluetoothPrompt(t, agent)
	if prompt.Kind != PairingPromptServiceAuthorization || prompt.UUID == "" {
		t.Fatalf("service prompt = %+v", prompt)
	}
	if err := agent.Respond(prompt.ID, PairingResponse{Accept: true}); err != nil {
		t.Fatal(err)
	}
	if err := <-authorizationResult; err != nil {
		t.Fatalf("authorization result = %v", err)
	}
}

func TestKeyboardDisplaySingleSlotGenerationAndTimeout(t *testing.T) {
	agent := newKeyboardDisplay(15 * time.Millisecond)
	defer agent.Close()

	first := make(chan *dbus.Error, 1)
	go func() {
		first <- agent.RequestAuthorization(dbus.ObjectPath("/org/bluez/hci0/dev_AA"))
	}()
	prompt := waitBluetoothPrompt(t, agent)
	duplicate := agent.RequestAuthorization(dbus.ObjectPath("/org/bluez/hci0/dev_BB"))
	if duplicate == nil || duplicate.Name != agentErrorRejected {
		t.Fatalf("duplicate = %v, want rejected", duplicate)
	}
	if err := agent.Respond(prompt.ID+1, PairingResponse{Accept: true}); err == nil {
		t.Fatal("late generation must not answer the prompt")
	}
	if err := agent.CancelPrompt(prompt.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err == nil || err.Name != agentErrorCanceled {
		t.Fatalf("canceled first request = %v", err)
	}

	timedOut := make(chan pairingStringResult, 1)
	go func() {
		value, err := agent.RequestPinCode(dbus.ObjectPath("/org/bluez/hci0/dev_AA"))
		timedOut <- pairingStringResult{value: value, err: err}
	}()
	prompt = waitBluetoothPrompt(t, agent)
	if got := <-timedOut; got.err == nil || got.err.Name != agentErrorCanceled {
		t.Fatalf("timed-out request = %+v, want canceled", got)
	}
	if _, ok := agent.PendingPrompt(); ok {
		t.Fatal("timeout must clear the prompt and credential state")
	}
}

func TestKeyboardDisplayDisplayMethodsAndFailClosedShutdown(t *testing.T) {
	agent := newKeyboardDisplay(time.Second)
	if err := agent.DisplayPinCode(dbus.ObjectPath("/org/bluez/hci0/dev_AA"), "1234"); err != nil {
		t.Fatal(err)
	}
	prompt := waitBluetoothPrompt(t, agent)
	if prompt.Kind != PairingPromptDisplayPIN || prompt.Code != "1234" || prompt.Entered != 0 {
		t.Fatalf("display PIN = %+v", prompt)
	}
	if err := agent.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.PendingPrompt(); ok {
		t.Fatal("Cancel must clear display-only prompt")
	}
	if err := agent.DisplayPasskey(dbus.ObjectPath("/org/bluez/hci0/dev_AA"), 42, 3); err != nil {
		t.Fatal(err)
	}
	prompt = waitBluetoothPrompt(t, agent)
	if prompt.Kind != PairingPromptDisplayPasskey || prompt.Code != "000042" || prompt.Entered != 3 {
		t.Fatalf("display passkey = %+v", prompt)
	}

	result := make(chan pairingPasskeyResult, 1)
	if err := agent.Cancel(); err != nil {
		t.Fatal(err)
	}
	go func() {
		value, err := agent.RequestPasskey(dbus.ObjectPath("/org/bluez/hci0/dev_AA"))
		result <- pairingPasskeyResult{value: value, err: err}
	}()
	waitBluetoothPrompt(t, agent)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got.err == nil || got.err.Name != agentErrorCanceled {
		t.Fatalf("closed request = %+v, want canceled", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestKeyboardDisplayRejectsInvalidDevicePath(t *testing.T) {
	agent := newKeyboardDisplay(time.Second)
	defer agent.Close()
	if _, err := agent.RequestPinCode(""); err == nil || err.Name != agentErrorInvalidArguments {
		t.Fatalf("invalid device error = %v", err)
	}
}

func TestKeyboardDisplayDisplayMethodsRejectInvalidDevicePath(t *testing.T) {
	agent := newKeyboardDisplay(time.Second)
	defer agent.Close()
	if err := agent.DisplayPinCode("", "1234"); err == nil || err.Name != agentErrorInvalidArguments {
		t.Fatalf("invalid display PIN error = %v, want invalid arguments", err)
	}
	if err := agent.DisplayPasskey("/not/bluez", 42, 0); err == nil || err.Name != agentErrorInvalidArguments {
		t.Fatalf("invalid display passkey error = %v, want invalid arguments", err)
	}
}

type pairingStringResult struct {
	value string
	err   *dbus.Error
}

type pairingPasskeyResult struct {
	value uint32
	err   *dbus.Error
}

func waitBluetoothPrompt(t *testing.T, agent *KeyboardDisplay) BluetoothPrompt {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case prompt := <-agent.PromptChanges():
			if prompt != nil {
				return *prompt
			}
		case <-deadline:
			t.Fatal("timed out waiting for pairing prompt")
			return BluetoothPrompt{}
		}
	}
}

func TestBluetoothReducerSectionsAndStableRSSIBandOrdering(t *testing.T) {
	r := newBluetoothReducer()
	state := r.replaceBluetoothObjects(bluetoothObjectsForDevices(
		bluetoothDeviceObject("/org/bluez/hci0/dev_1", "AA:01", "zeta", true, true, true, -51),
		bluetoothDeviceObject("/org/bluez/hci0/dev_2", "AA:02", "Alpha", true, false, false, -52),
		bluetoothDeviceObject("/org/bluez/hci0/dev_3", "AA:03", "beta", false, false, false, -51),
		bluetoothDeviceObject("/org/bluez/hci0/dev_4", "AA:04", "Gamma", false, false, false, -80),
	))

	sections := state.Sections()
	if len(sections) != 3 {
		t.Fatalf("sections = %+v, want connected, paired, available", sections)
	}
	if sections[0].Name != BluetoothSectionConnected || sections[1].Name != BluetoothSectionPaired || sections[2].Name != BluetoothSectionAvailable {
		t.Fatalf("section order = %+v", sections)
	}
	if got := sections[0].Devices[0].Alias; got != "zeta" {
		t.Fatalf("connected device = %q, want zeta", got)
	}
	if got := sections[1].Devices[0].Alias; got != "Alpha" {
		t.Fatalf("paired device = %q, want Alpha", got)
	}
	if got := []string{sections[2].Devices[0].Alias, sections[2].Devices[1].Alias}; got[0] != "beta" || got[1] != "Gamma" {
		t.Fatalf("available order = %v, want beta then Gamma by RSSI band and alias", got)
	}

	state = r.propertiesChanged(
		dbus.ObjectPath("/org/bluez/hci0/dev_3"), deviceInterface,
		map[string]dbus.Variant{"RSSI": dbus.MakeVariant(int16(-55))}, nil,
	)
	sections = state.Sections()
	if got := sections[2].Devices[0].Alias; got != "beta" {
		t.Fatalf("available order moved on same RSSI band: %q", got)
	}
}

func TestBluetoothReducerDoesNotDuplicateConnectedUnpairedDevices(t *testing.T) {
	state := BluetoothState{Devices: []BluetoothDevice{{ID: "connected", Connected: true}}}
	sections := state.Sections()
	if len(sections) != 1 || sections[0].Name != BluetoothSectionConnected || len(sections[0].Devices) != 1 {
		t.Fatalf("sections = %+v, want one connected section", sections)
	}
}

func TestBluetoothReducerInvalidationAndRemovalClearOptionalValues(t *testing.T) {
	r := newBluetoothReducer()
	path := dbus.ObjectPath("/org/bluez/hci0/dev_1")
	state := r.replaceBluetoothObjects(map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath("/org/bluez/hci0"): {adapterInterface: {"Powered": dbus.MakeVariant(true)}},
		path: {
			deviceInterface: {
				"Adapter": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
				"Address": dbus.MakeVariant("AA:01"),
				"Alias":   dbus.MakeVariant("Headset"),
				"RSSI":    dbus.MakeVariant(int16(-45)),
			},
			batteryInterface: {"Percentage": dbus.MakeVariant(uint8(72))},
		},
	})
	device := state.Devices[0]
	if device.RSSI == nil || device.Battery == nil {
		t.Fatalf("initial optional values = %+v", device)
	}

	state = r.propertiesChanged(path, deviceInterface, nil, []string{"RSSI"})
	state = r.propertiesChanged(path, batteryInterface, nil, []string{"Percentage"})
	if got := state.Devices[0]; got.RSSI != nil || got.Battery != nil {
		t.Fatalf("invalidated optional values = %+v, want both absent", got)
	}

	oldID := device.ID
	state = r.interfacesRemoved(path, []string{deviceInterface})
	if len(state.Devices) != 0 {
		t.Fatalf("devices after removal = %+v, want empty", state.Devices)
	}
	if _, ok := r.pathForDevice(oldID); ok {
		t.Fatalf("removed device ID still resolves")
	}
}

func TestBluetoothReducerBlueZLossAndRecovery(t *testing.T) {
	r := newBluetoothReducer()
	objects := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath("/org/bluez/hci0"): {adapterInterface: {"Powered": dbus.MakeVariant(true)}},
	}
	if state := r.replaceBluetoothObjects(objects); !state.Available {
		t.Fatal("initial state unavailable")
	}
	if state := r.bluezLost(); state.Available || len(state.Devices) != 0 {
		t.Fatalf("lost state = %+v, want unavailable and empty", state)
	}
	if state := r.replaceBluetoothObjects(objects); !state.Available || !state.Adapter.Powered {
		t.Fatalf("recovered state = %+v, want rebuilt powered adapter", state)
	}
}

func bluetoothObjectsForDevices(devices ...map[dbus.ObjectPath]map[string]map[string]dbus.Variant) map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	objects := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath("/org/bluez/hci0"): {adapterInterface: {"Powered": dbus.MakeVariant(true)}},
	}
	for _, object := range devices {
		for path, interfaces := range object {
			objects[path] = interfaces
		}
	}
	return objects
}

func bluetoothDeviceObject(path, address, alias string, paired, trusted, connected bool, rssi int16) map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	return map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath(path): {
			deviceInterface: {
				"Adapter":   dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
				"Address":   dbus.MakeVariant(address),
				"Alias":     dbus.MakeVariant(alias),
				"Paired":    dbus.MakeVariant(paired),
				"Trusted":   dbus.MakeVariant(trusted),
				"Connected": dbus.MakeVariant(connected),
				"RSSI":      dbus.MakeVariant(rssi),
			},
		},
	}
}

func TestBluetoothServiceBootstrapsAndReducesObjectManagerSignals(t *testing.T) {
	devicePath := dbus.ObjectPath("/org/bluez/hci0/dev_AA")
	bus := &fakeBluetoothBus{objects: bluetoothObjectsForDevices(
		bluetoothDeviceObject(string(devicePath), "AA:01", "Headset", true, false, false, -45),
	)}
	bluetooth := newBluetoothWithBus(bus)
	defer bluetooth.Close()

	state := bluetooth.CachedState()
	if !state.Available || len(state.Devices) != 1 || state.Devices[0].ID != DeviceID(devicePath) {
		t.Fatalf("startup state = %+v, want one available device", state)
	}
	if !state.AgentReady {
		t.Fatal("startup must register a ready pairing agent")
	}

	newPath := dbus.ObjectPath("/org/bluez/hci0/dev_BB")
	bus.emit(&dbus.Signal{
		Name: "org.freedesktop.DBus.ObjectManager.InterfacesAdded",
		Path: dbus.ObjectPath("/"),
		Body: []any{newPath, map[string]map[string]dbus.Variant{
			deviceInterface: {
				"Adapter":   dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
				"Address":   dbus.MakeVariant("BB:02"),
				"Alias":     dbus.MakeVariant("Keyboard"),
				"Paired":    dbus.MakeVariant(false),
				"Connected": dbus.MakeVariant(false),
			},
		}},
	})

	select {
	case state = <-bluetooth.Changes():
	case <-time.After(time.Second):
		t.Fatal("object manager signal did not publish a snapshot")
	}
	if len(state.Devices) != 2 || state.Devices[1].ID != DeviceID(newPath) {
		t.Fatalf("signal state = %+v, want both devices", state)
	}
}

func TestBluetoothServiceUnregistersAgentWhenDefaultRequestFails(t *testing.T) {
	bus := &fakeBluetoothBus{
		objects: bluetoothObjectsForDevices(),
		methodErrors: map[string]error{
			agentManagerInterface + ".RequestDefaultAgent": errors.New("default agent unavailable"),
		},
	}
	bluetooth := newBluetoothWithBus(bus)
	if bluetooth.CachedState().AgentReady {
		t.Fatal("failed default-agent request must leave the agent not ready")
	}
	if err := bluetooth.Close(); err != nil {
		t.Fatal(err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	var methods []string
	for _, call := range bus.calls {
		methods = append(methods, call.method)
	}
	if !slices.Contains(methods, agentManagerInterface+".UnregisterAgent") {
		t.Fatalf("agent methods = %v, want unregister after registration", methods)
	}
}

type fakeBluetoothBus struct {
	mu           sync.Mutex
	objects      map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	signalCh     chan<- *dbus.Signal
	matchAdds    int
	matchRems    int
	exported     any
	closed       bool
	calls        []fakeBluetoothCall
	methodErrors map[string]error
	blockMethod  string
	block        <-chan struct{}
}

type fakeBluetoothCall struct {
	dest   string
	path   dbus.ObjectPath
	method string
	args   []any
}

func (f *fakeBluetoothBus) object(dest string, path dbus.ObjectPath) bluetoothObject {
	return fakeBluetoothObject{bus: f, dest: dest, path: path}
}

func (f *fakeBluetoothBus) addMatch(_ ...dbus.MatchOption) error {
	f.mu.Lock()
	f.matchAdds++
	f.mu.Unlock()
	return nil
}

func (f *fakeBluetoothBus) removeMatch(_ ...dbus.MatchOption) error {
	f.mu.Lock()
	f.matchRems++
	f.mu.Unlock()
	return nil
}

func (f *fakeBluetoothBus) signal(ch chan<- *dbus.Signal) { f.signalCh = ch }

func (f *fakeBluetoothBus) removeSignal(_ chan<- *dbus.Signal) {}

func (f *fakeBluetoothBus) export(value any, _ dbus.ObjectPath, _ string) error {
	f.mu.Lock()
	f.exported = value
	f.mu.Unlock()
	return nil
}

func (f *fakeBluetoothBus) close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeBluetoothBus) emit(signal *dbus.Signal) {
	f.mu.Lock()
	ch := f.signalCh
	f.mu.Unlock()
	ch <- signal
}

type fakeBluetoothObject struct {
	bus  *fakeBluetoothBus
	dest string
	path dbus.ObjectPath
}

func (o fakeBluetoothObject) Call(method string, _ dbus.Flags, args ...any) *dbus.Call {
	o.bus.mu.Lock()
	o.bus.calls = append(o.bus.calls, fakeBluetoothCall{
		dest: o.dest, path: o.path, method: method, args: slices.Clone(args),
	})
	objects := o.bus.objects
	o.bus.mu.Unlock()
	if method == objectManagerInterface+".GetManagedObjects" {
		return &dbus.Call{Body: []any{objects}}
	}
	if err := o.bus.methodErrors[method]; err != nil {
		return &dbus.Call{Err: err}
	}
	if method == o.bus.blockMethod && o.bus.block != nil {
		<-o.bus.block
	}
	return &dbus.Call{}
}

func TestBluetoothServicePublishesAndDeduplicatesDeviceAction(t *testing.T) {
	devicePath := dbus.ObjectPath("/org/bluez/hci0/dev_AA")
	block := make(chan struct{})
	bus := &fakeBluetoothBus{
		objects:     bluetoothObjectsForDevices(bluetoothDeviceObject(string(devicePath), "AA:01", "Headset", true, false, false, -45)),
		blockMethod: deviceInterface + ".Connect",
		block:       block,
	}
	bluetooth := newBluetoothWithBus(bus)
	defer bluetooth.Close()

	result := make(chan error, 1)
	go func() { result <- bluetooth.Connect(DeviceID(devicePath)) }()
	state := waitForBluetoothState(t, bluetooth, func(state BluetoothState) bool {
		return len(state.Devices) == 1 && state.Devices[0].Action == BluetoothActionConnect
	})
	if err := bluetooth.Connect(DeviceID(devicePath)); err != errBluetoothBusy {
		t.Fatalf("duplicate action error = %v, want %v", err, errBluetoothBusy)
	}
	close(block)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	state = waitForBluetoothState(t, bluetooth, func(state BluetoothState) bool {
		return len(state.Devices) == 1 && state.Devices[0].Action == BluetoothActionNone
	})
	if state.Devices[0].Action != BluetoothActionNone {
		t.Fatalf("completed action = %v, want none", state.Devices[0].Action)
	}
}

func TestBluetoothServiceMutationsUseCurrentOpaqueDevicePath(t *testing.T) {
	devicePath := dbus.ObjectPath("/org/bluez/hci0/dev_AA")
	bus := &fakeBluetoothBus{objects: bluetoothObjectsForDevices(
		bluetoothDeviceObject(string(devicePath), "AA:01", "Headset", true, false, false, -45),
	)}
	bluetooth := newBluetoothWithBus(bus)
	defer bluetooth.Close()

	id := DeviceID(devicePath)
	if err := bluetooth.Connect(id); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := bluetooth.Pair(id); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if err := bluetooth.SetTrusted(id, true); err != nil {
		t.Fatalf("SetTrusted: %v", err)
	}
	if err := bluetooth.Forget(id); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	bus.mu.Lock()
	calls := slices.Clone(bus.calls)
	bus.mu.Unlock()
	var methods []string
	for _, call := range calls {
		if call.method != objectManagerInterface+".GetManagedObjects" &&
			call.method != agentManagerInterface+".RegisterAgent" &&
			call.method != agentManagerInterface+".RequestDefaultAgent" {
			methods = append(methods, call.method)
			if call.method != propertiesInterface+".Set" && call.path != devicePath && call.method != adapterInterface+".RemoveDevice" {
				t.Fatalf("call %s used path %s, want device %s", call.method, call.path, devicePath)
			}
		}
	}
	wantMethods := []string{
		deviceInterface + ".Connect",
		deviceInterface + ".Pair",
		propertiesInterface + ".Set",
		propertiesInterface + ".Set",
		adapterInterface + ".RemoveDevice",
	}
	if !slices.Equal(methods, wantMethods) {
		t.Fatalf("mutating methods = %v, want %v", methods, wantMethods)
	}

	if err := bluetooth.Connect(DeviceID("/org/bluez/hci0/dev_missing")); err != errBluetoothStaleDevice {
		t.Fatalf("stale Connect error = %v, want %v", err, errBluetoothStaleDevice)
	}
	bus.mu.Lock()
	gotCalls := len(bus.calls)
	bus.mu.Unlock()
	if gotCalls != len(calls) {
		t.Fatalf("stale action made a bus call: %d before, %d after", len(calls), gotCalls)
	}
}

func TestBluetoothServiceBlueZLossRejectsPromptAndRebuildsOnReturn(t *testing.T) {
	devicePath := dbus.ObjectPath("/org/bluez/hci0/dev_AA")
	bus := &fakeBluetoothBus{objects: bluetoothObjectsForDevices(
		bluetoothDeviceObject(string(devicePath), "AA:01", "Headset", true, false, false, -45),
	)}
	bluetooth := newBluetoothWithBus(bus)
	defer bluetooth.Close()

	result := make(chan *dbus.Error, 1)
	go func() {
		result <- bluetooth.agent.RequestAuthorization(devicePath)
	}()
	waitForBluetoothPromptState(t, bluetooth, true)

	bus.emit(&dbus.Signal{
		Name: "org.freedesktop.DBus.NameOwnerChanged",
		Body: []any{bluezBusName, "owner-1", ""},
	})
	state := waitForBluetoothState(t, bluetooth, func(state BluetoothState) bool {
		return !state.Available && state.Prompt == nil
	})
	if state.AgentReady || len(state.Devices) != 0 {
		t.Fatalf("lost state = %+v, want no agent or devices", state)
	}
	if err := <-result; err == nil || err.Name != agentErrorCanceled {
		t.Fatalf("prompt result on BlueZ loss = %v, want canceled", err)
	}

	bus.mu.Lock()
	bus.objects = bluetoothObjectsForDevices(
		bluetoothDeviceObject(string(devicePath), "AA:01", "Headset", true, true, true, -40),
	)
	bus.mu.Unlock()
	bus.emit(&dbus.Signal{
		Name: "org.freedesktop.DBus.NameOwnerChanged",
		Body: []any{bluezBusName, "", "owner-2"},
	})
	state = waitForBluetoothState(t, bluetooth, func(state BluetoothState) bool {
		return state.Available && state.AgentReady && len(state.Devices) == 1
	})
	if !state.Devices[0].Connected || !state.Devices[0].Trusted {
		t.Fatalf("recovered device = %+v, want connected and trusted", state.Devices[0])
	}
}

func waitForBluetoothPromptState(t *testing.T, bluetooth *Bluetooth, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, got := bluetooth.agent.PendingPrompt()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("prompt state did not become %t", want)
}

func waitForBluetoothState(t *testing.T, bluetooth *Bluetooth, match func(BluetoothState) bool) BluetoothState {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case state := <-bluetooth.Changes():
			if match(state) {
				return state
			}
		case <-deadline:
			t.Fatal("timed out waiting for Bluetooth state")
			return BluetoothState{}
		}
	}
}

func TestBluetoothServiceCloseStopsDiscoveryAndUnregistersAgent(t *testing.T) {
	bus := &fakeBluetoothBus{objects: bluetoothObjectsForDevices()}
	bluetooth := newBluetoothWithBus(bus)
	if err := bluetooth.StartDiscovery(); err != nil {
		t.Fatal(err)
	}
	if err := bluetooth.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bluetooth.Close(); err != nil {
		t.Fatal(err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	var methods []string
	for _, call := range bus.calls {
		methods = append(methods, call.method)
	}
	if !slices.Contains(methods, adapterInterface+".StartDiscovery") ||
		!slices.Contains(methods, adapterInterface+".StopDiscovery") ||
		!slices.Contains(methods, agentManagerInterface+".UnregisterAgent") {
		t.Fatalf("close methods = %v, want discovery stop and agent unregister", methods)
	}
	if bus.matchRems != len(bluetoothMatchRules()) {
		t.Fatalf("removed %d match rules, want %d", bus.matchRems, len(bluetoothMatchRules()))
	}
	if !bus.closed {
		t.Fatal("Close must close the owned bus")
	}
}

func TestBluetoothServiceReducesPropertiesChangedSignals(t *testing.T) {
	devicePath := dbus.ObjectPath("/org/bluez/hci0/dev_AA")
	bus := &fakeBluetoothBus{objects: bluetoothObjectsForDevices(
		bluetoothDeviceObject(string(devicePath), "AA:01", "Headset", false, false, false, -45),
	)}
	bluetooth := newBluetoothWithBus(bus)
	defer bluetooth.Close()

	bus.emit(&dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: devicePath,
		Body: []any{deviceInterface, map[string]dbus.Variant{
			"RSSI": dbus.MakeVariant(int16(-58)),
		}, []string{"Connected"}},
	})
	state := waitForBluetoothState(t, bluetooth, func(state BluetoothState) bool {
		return len(state.Devices) == 1 && state.Devices[0].RSSI != nil && state.Devices[0].Connected == false
	})
	if *state.Devices[0].RSSI != -58 {
		t.Fatalf("RSSI = %d, want -58", *state.Devices[0].RSSI)
	}
}

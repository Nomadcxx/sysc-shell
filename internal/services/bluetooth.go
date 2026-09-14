package services

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"
)

const (
	bluezBusName           = "org.bluez"
	bluezRootPath          = dbus.ObjectPath("/")
	objectManagerInterface = "org.freedesktop.DBus.ObjectManager"
	propertiesInterface    = "org.freedesktop.DBus.Properties"
	agentManagerInterface  = "org.bluez.AgentManager1"
	adapterAgentPath       = dbus.ObjectPath("/org/nomadx/sysc_shell/bluetooth/agent")
	agentCapability        = "KeyboardDisplay"

	adapterInterface = "org.bluez.Adapter1"
	deviceInterface  = "org.bluez.Device1"
	batteryInterface = "org.bluez.Battery1"
	agentInterface   = "org.bluez.Agent1"

	agentErrorRejected         = agentInterface + ".Error.Rejected"
	agentErrorCanceled         = agentInterface + ".Error.Canceled"
	agentErrorInvalidArguments = "org.bluez.Error.InvalidArguments"
	pairingPromptTimeout       = 60 * time.Second
)

var (
	errPairingPromptGone      = errors.New("services: pairing prompt is no longer active")
	errPairingInvalidResponse = errors.New("services: invalid pairing response")
	errPairingNotRespondable  = errors.New("services: pairing prompt does not accept a response")
	errBluetoothUnavailable   = errors.New("services: Bluetooth is unavailable")
	errBluetoothStaleDevice   = errors.New("services: Bluetooth device is no longer available")
	errBluetoothBusy          = errors.New("services: Bluetooth device already has an action")
)

// bluetoothObject and bluetoothBus are deliberately narrower than godbus's
// public objects. The sole implementation wraps one *dbus.Conn; the seam lets
// signal and method behavior run against a deterministic fake in this package.
type bluetoothObject interface {
	Call(method string, flags dbus.Flags, args ...any) *dbus.Call
}

type bluetoothBus interface {
	object(destination string, path dbus.ObjectPath) bluetoothObject
	addMatch(options ...dbus.MatchOption) error
	removeMatch(options ...dbus.MatchOption) error
	signal(ch chan<- *dbus.Signal)
	removeSignal(ch chan<- *dbus.Signal)
	export(value any, path dbus.ObjectPath, iface string) error
	close() error
}

type systemBluetoothBus struct{ conn *dbus.Conn }

func (b *systemBluetoothBus) object(destination string, path dbus.ObjectPath) bluetoothObject {
	return b.conn.Object(destination, path)
}

func (b *systemBluetoothBus) addMatch(options ...dbus.MatchOption) error {
	return b.conn.AddMatchSignal(options...)
}

func (b *systemBluetoothBus) removeMatch(options ...dbus.MatchOption) error {
	return b.conn.RemoveMatchSignal(options...)
}

func (b *systemBluetoothBus) signal(ch chan<- *dbus.Signal) { b.conn.Signal(ch) }

func (b *systemBluetoothBus) removeSignal(ch chan<- *dbus.Signal) { b.conn.RemoveSignal(ch) }

func (b *systemBluetoothBus) export(value any, path dbus.ObjectPath, iface string) error {
	return b.conn.Export(value, path, iface)
}

func (b *systemBluetoothBus) close() error { return b.conn.Close() }

// DeviceID is a service-owned handle. Consumers pass it back to Bluetooth and
// never construct BlueZ object paths.
type DeviceID string

// PromptID identifies one pairing request generation.
type PromptID uint64

// BluetoothAction is the transient operation shown by a device row.
type BluetoothAction uint8

const (
	BluetoothActionNone BluetoothAction = iota
	BluetoothActionPair
	BluetoothActionConnect
	BluetoothActionDisconnect
	BluetoothActionTrust
	BluetoothActionForget
)

// BluetoothAdapter is the selected adapter's user-facing state.
type BluetoothAdapter struct {
	Powered     bool
	Pairable    bool
	Discovering bool
}

// BluetoothDevice is a copied projection of BlueZ Device1 and Battery1.
// Optional values are nil when BlueZ has not supplied them or invalidated them.
type BluetoothDevice struct {
	ID        DeviceID
	Alias     string
	Address   string
	Icon      string
	Paired    bool
	Trusted   bool
	Connected bool
	Action    BluetoothAction
	Battery   *uint8
	RSSI      *int16
}

// PairingPromptKind describes the one prompt currently owned by the shell.
type PairingPromptKind uint8

const (
	PairingPromptPIN PairingPromptKind = iota + 1
	PairingPromptPasskey
	PairingPromptDisplayPIN
	PairingPromptDisplayPasskey
	PairingPromptConfirmation
	PairingPromptAuthorization
	PairingPromptServiceAuthorization
)

// BluetoothPrompt is safe-to-render prompt state. Submitted credentials are
// never stored here.
type BluetoothPrompt struct {
	ID       PromptID
	DeviceID DeviceID
	Alias    string
	Kind     PairingPromptKind
	Code     string
	Passkey  uint32
	Entered  uint16
	UUID     string
}

// PairingResponse is the user decision for a pending prompt. Value is used
// only for PIN/passkey input and is cleared by the caller after the response.
type PairingResponse struct {
	Accept bool
	Value  string
}

type pairingReply struct {
	value   string
	errName string
}

type pairingRequest struct {
	prompt BluetoothPrompt
	reply  chan pairingReply
	timer  *time.Timer
}

// KeyboardDisplay is the shell-owned BlueZ Agent1 object. It contains only
// the one prompt slot; D-Bus registration is owned by Bluetooth.
type KeyboardDisplay struct {
	mu      sync.Mutex
	current *pairingRequest
	nextID  PromptID
	timeout time.Duration
	closed  bool
	changes chan *BluetoothPrompt
}

func NewKeyboardDisplay() *KeyboardDisplay {
	return newKeyboardDisplay(pairingPromptTimeout)
}

func newKeyboardDisplay(timeout time.Duration) *KeyboardDisplay {
	if timeout <= 0 {
		timeout = pairingPromptTimeout
	}
	return &KeyboardDisplay{timeout: timeout, changes: make(chan *BluetoothPrompt, 1)}
}

// PromptChanges carries a copied prompt or nil when the active prompt clears.
func (a *KeyboardDisplay) PromptChanges() <-chan *BluetoothPrompt { return a.changes }

func (a *KeyboardDisplay) PendingPrompt() (BluetoothPrompt, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current == nil {
		return BluetoothPrompt{}, false
	}
	return cloneBluetoothPrompt(a.current.prompt), true
}

func (a *KeyboardDisplay) begin(prompt BluetoothPrompt) (BluetoothPrompt, <-chan pairingReply, *dbus.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return BluetoothPrompt{}, nil, bluezAgentError(agentErrorCanceled, "agent is closed")
	}
	if a.current != nil {
		return BluetoothPrompt{}, nil, bluezAgentError(agentErrorRejected, "a pairing prompt is already open")
	}
	a.nextID++
	if a.nextID == 0 {
		a.nextID++
	}
	prompt.ID = a.nextID
	reply := make(chan pairingReply, 1)
	a.current = &pairingRequest{prompt: prompt, reply: reply}
	a.publishLocked(&prompt)
	a.current.timer = time.AfterFunc(a.timeout, func() {
		a.finish(prompt.ID, pairingReply{errName: agentErrorCanceled})
	})
	return prompt, reply, nil
}

func (a *KeyboardDisplay) display(prompt BluetoothPrompt) *dbus.Error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return bluezAgentError(agentErrorCanceled, "agent is closed")
	}
	if a.current != nil {
		if a.current.reply != nil {
			return bluezAgentError(agentErrorRejected, "a pairing prompt is already open")
		}
		if a.current.prompt.DeviceID != prompt.DeviceID {
			return bluezAgentError(agentErrorRejected, "a pairing prompt is already open")
		}
		prompt.ID = a.current.prompt.ID
		a.current.prompt = prompt
		a.publishLocked(&prompt)
		return nil
	}
	a.nextID++
	if a.nextID == 0 {
		a.nextID++
	}
	prompt.ID = a.nextID
	a.current = &pairingRequest{prompt: prompt}
	a.publishLocked(&prompt)
	return nil
}

func (a *KeyboardDisplay) finish(id PromptID, reply pairingReply) bool {
	a.mu.Lock()
	if a.current == nil || a.current.prompt.ID != id {
		a.mu.Unlock()
		return false
	}
	request := a.current
	a.current = nil
	if request.timer != nil {
		request.timer.Stop()
	}
	a.publishLocked(nil)
	a.mu.Unlock()
	if request.reply != nil {
		request.reply <- reply
	}
	return true
}

func (a *KeyboardDisplay) cancelPrompt(id PromptID) error {
	if !a.finish(id, pairingReply{errName: agentErrorCanceled}) {
		return errPairingPromptGone
	}
	return nil
}

func (a *KeyboardDisplay) cancelAny() {
	a.mu.Lock()
	if a.current == nil {
		a.mu.Unlock()
		return
	}
	id := a.current.prompt.ID
	a.mu.Unlock()
	a.finish(id, pairingReply{errName: agentErrorCanceled})
}

func (a *KeyboardDisplay) failClosed() { a.cancelAny() }

func (a *KeyboardDisplay) Close() error {
	a.mu.Lock()
	a.closed = true
	current := a.current
	if current == nil {
		a.mu.Unlock()
		return nil
	}
	id := current.prompt.ID
	a.mu.Unlock()
	a.finish(id, pairingReply{errName: agentErrorCanceled})
	return nil
}

// Respond answers a current input or authorization prompt. The generation is
// checked before the value is accepted, so a late click cannot answer a newer
// request.
func (a *KeyboardDisplay) Respond(id PromptID, response PairingResponse) error {
	a.mu.Lock()
	if a.current == nil || a.current.prompt.ID != id {
		a.mu.Unlock()
		return errPairingPromptGone
	}
	kind := a.current.prompt.Kind
	a.mu.Unlock()

	if kind == PairingPromptDisplayPIN || kind == PairingPromptDisplayPasskey {
		return errPairingNotRespondable
	}
	if !response.Accept {
		if !a.finish(id, pairingReply{errName: agentErrorCanceled}) {
			return errPairingPromptGone
		}
		return nil
	}
	switch kind {
	case PairingPromptPIN:
		if !validPIN(response.Value) {
			return errPairingInvalidResponse
		}
	case PairingPromptPasskey:
		if _, ok := parsePasskey(response.Value); !ok {
			return errPairingInvalidResponse
		}
	case PairingPromptConfirmation, PairingPromptAuthorization, PairingPromptServiceAuthorization:
		// Accept is the complete response for these prompt kinds.
	default:
		return errPairingInvalidResponse
	}
	if !a.finish(id, pairingReply{value: response.Value}) {
		return errPairingPromptGone
	}
	return nil
}

func (a *KeyboardDisplay) CancelPrompt(id PromptID) error { return a.cancelPrompt(id) }

func (a *KeyboardDisplay) Cancel() *dbus.Error {
	a.cancelAny()
	return nil
}

func (a *KeyboardDisplay) Release() *dbus.Error {
	a.cancelAny()
	return nil
}

func (a *KeyboardDisplay) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	prompt, reply, err := a.beginInputPrompt(device, PairingPromptPIN)
	if err != nil {
		return "", err
	}
	_ = prompt
	r := <-reply
	return r.value, pairingReplyError(r)
}

func (a *KeyboardDisplay) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	prompt, reply, err := a.beginInputPrompt(device, PairingPromptPasskey)
	if err != nil {
		return 0, err
	}
	_ = prompt
	r := <-reply
	if r.errName != "" {
		return 0, pairingReplyError(r)
	}
	passkey, ok := parsePasskey(r.value)
	if !ok {
		return 0, bluezAgentError(agentErrorInvalidArguments, "invalid passkey")
	}
	return passkey, nil
}

func (a *KeyboardDisplay) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	if passkey > 999999 {
		return bluezAgentError(agentErrorInvalidArguments, "invalid passkey")
	}
	_, reply, err := a.beginPrompt(device, BluetoothPrompt{
		DeviceID: DeviceID(device), Kind: PairingPromptConfirmation,
		Passkey: passkey, Code: fmt.Sprintf("%06d", passkey),
	})
	if err != nil {
		return err
	}
	return pairingReplyError(<-reply)
}

func (a *KeyboardDisplay) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	_, reply, err := a.beginPrompt(device, BluetoothPrompt{DeviceID: DeviceID(device), Kind: PairingPromptAuthorization})
	if err != nil {
		return err
	}
	return pairingReplyError(<-reply)
}

func (a *KeyboardDisplay) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	if uuid == "" {
		return bluezAgentError(agentErrorInvalidArguments, "service UUID is required")
	}
	_, reply, err := a.beginPrompt(device, BluetoothPrompt{
		DeviceID: DeviceID(device), Kind: PairingPromptServiceAuthorization, UUID: uuid,
	})
	if err != nil {
		return err
	}
	return pairingReplyError(<-reply)
}

func (a *KeyboardDisplay) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	if err := validateAgentDevice(device); err != nil {
		return err
	}
	if !validPIN(pincode) {
		return bluezAgentError(agentErrorInvalidArguments, "invalid PIN")
	}
	return a.display(BluetoothPrompt{DeviceID: DeviceID(device), Kind: PairingPromptDisplayPIN, Code: pincode})
}

func (a *KeyboardDisplay) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	if err := validateAgentDevice(device); err != nil {
		return err
	}
	if passkey > 999999 || entered > 6 {
		return bluezAgentError(agentErrorInvalidArguments, "invalid passkey display")
	}
	return a.display(BluetoothPrompt{
		DeviceID: DeviceID(device), Kind: PairingPromptDisplayPasskey,
		Passkey: passkey, Code: fmt.Sprintf("%06d", passkey), Entered: entered,
	})
}

func (a *KeyboardDisplay) beginInputPrompt(device dbus.ObjectPath, kind PairingPromptKind) (BluetoothPrompt, <-chan pairingReply, *dbus.Error) {
	return a.beginPrompt(device, BluetoothPrompt{DeviceID: DeviceID(device), Kind: kind})
}

func (a *KeyboardDisplay) beginPrompt(device dbus.ObjectPath, prompt BluetoothPrompt) (BluetoothPrompt, <-chan pairingReply, *dbus.Error) {
	if err := validateAgentDevice(device); err != nil {
		return BluetoothPrompt{}, nil, err
	}
	return a.begin(prompt)
}

func validateAgentDevice(device dbus.ObjectPath) *dbus.Error {
	if device == "" || !strings.HasPrefix(string(device), "/org/bluez/") {
		return bluezAgentError(agentErrorInvalidArguments, "invalid device path")
	}
	return nil
}

func validPIN(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) == 0 || utf8.RuneCountInString(value) > 16 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parsePasskey(value string) (uint32, bool) {
	if value == "" || len(value) > 6 {
		return 0, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err == nil && parsed <= 999999
}

func pairingReplyError(reply pairingReply) *dbus.Error {
	if reply.errName == "" {
		return nil
	}
	return bluezAgentError(reply.errName, "pairing request canceled")
}

func bluezAgentError(name, message string) *dbus.Error {
	return dbus.NewError(name, []any{message})
}

func (a *KeyboardDisplay) publishLocked(prompt *BluetoothPrompt) {
	var copyPrompt *BluetoothPrompt
	if prompt != nil {
		copy := cloneBluetoothPrompt(*prompt)
		copyPrompt = &copy
	}
	select {
	case a.changes <- copyPrompt:
	default:
		select {
		case <-a.changes:
		default:
		}
		a.changes <- copyPrompt
	}
}

// Bluetooth is one continuously subscribed BlueZ client. Its reducer owns
// D-Bus state; callers receive copied snapshots and never see bus objects.
type Bluetooth struct {
	mu        sync.Mutex
	bus       bluetoothBus
	reducer   *bluetoothReducer
	agent     *KeyboardDisplay
	changes   chan BluetoothState
	signals   chan *dbus.Signal
	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	closed    bool

	agentExported    bool
	agentRegistered  bool
	agentReady       bool
	discoveryStarted bool
	actions          map[DeviceID]BluetoothAction
}

// NewSystemBluetooth creates the process-wide service used by Registry. A
// missing system bus or BlueZ daemon is represented as unavailable state so a
// bar can still render and the shell does not fail to start.
func NewSystemBluetooth() *Bluetooth {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return newBluetoothWithBus(nil)
	}
	return newBluetoothWithBus(&systemBluetoothBus{conn: conn})
}

func newBluetoothWithBus(bus bluetoothBus) *Bluetooth {
	b := &Bluetooth{
		bus:     bus,
		reducer: newBluetoothReducer(),
		agent:   NewKeyboardDisplay(),
		changes: make(chan BluetoothState, 1),
		done:    make(chan struct{}),
		actions: make(map[DeviceID]BluetoothAction),
	}
	if bus == nil {
		return b
	}
	b.signals = make(chan *dbus.Signal, 32)
	bus.signal(b.signals)
	if err := b.installMatches(); err != nil {
		_ = bus.close()
		return b
	}
	if err := bus.export(b.agent, adapterAgentPath, agentInterface); err == nil {
		b.agentExported = true
	}
	b.wg.Add(1)
	go b.run()
	_ = b.bootstrap(false)
	return b
}

// Changes carries the newest immutable state. It is never closed: the
// Registry relay may outlive a failed BlueZ owner and waits on its own close.
func (b *Bluetooth) Changes() <-chan BluetoothState { return b.changes }

func (b *Bluetooth) Available() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closed && b.reducer != nil && b.reducer.state().Available
}

// CachedState performs no D-Bus work and returns a deep copy suitable for a UI
// tree built while Registry.mu is held.
func (b *Bluetooth) CachedState() BluetoothState {
	if b == nil {
		return BluetoothState{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reducer == nil {
		return BluetoothState{}
	}
	return b.stateLocked()
}

func (b *Bluetooth) run() {
	defer b.wg.Done()
	promptChanges := b.agent.PromptChanges()
	for {
		select {
		case <-b.done:
			return
		case signal, ok := <-b.signals:
			if !ok {
				return
			}
			b.handleSignal(signal)
		case prompt, ok := <-promptChanges:
			if !ok {
				return
			}
			b.handlePrompt(prompt)
		}
	}
}

func (b *Bluetooth) handlePrompt(prompt *BluetoothPrompt) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	state := b.stateLocked()
	if prompt != nil {
		copyPrompt := cloneBluetoothPrompt(*prompt)
		b.fillPromptAliasLocked(&copyPrompt, state.Devices)
		state.Prompt = &copyPrompt
	} else {
		state.Prompt = nil
	}
	b.publishLocked(state)
}

func (b *Bluetooth) fillPromptAliasLocked(prompt *BluetoothPrompt, devices []BluetoothDevice) {
	for _, device := range devices {
		if device.ID == prompt.DeviceID {
			prompt.Alias = device.Alias
			return
		}
	}
}

func (b *Bluetooth) stateLocked() BluetoothState {
	if b.reducer == nil {
		return BluetoothState{}
	}
	state := b.reducer.state()
	state.AgentReady = b.agentReady && state.Available
	if prompt, ok := b.agent.PendingPrompt(); ok {
		b.fillPromptAliasLocked(&prompt, state.Devices)
		state.Prompt = &prompt
	}
	for i := range state.Devices {
		state.Devices[i].Action = b.actions[state.Devices[i].ID]
	}
	return cloneBluetoothState(state)
}

func (b *Bluetooth) publishLocked(state BluetoothState) {
	state = cloneBluetoothState(state)
	select {
	case b.changes <- state:
	default:
		select {
		case <-b.changes:
		default:
		}
		b.changes <- state
	}
}

func (b *Bluetooth) installMatches() error {
	for i, options := range bluetoothMatchRules() {
		if err := b.bus.addMatch(options...); err != nil {
			for _, installed := range bluetoothMatchRules()[:i] {
				_ = b.bus.removeMatch(installed...)
			}
			return err
		}
	}
	return nil
}

func bluetoothMatchRules() [][]dbus.MatchOption {
	return [][]dbus.MatchOption{
		{
			dbus.WithMatchSender(bluezBusName),
			dbus.WithMatchInterface(objectManagerInterface),
			dbus.WithMatchMember("InterfacesAdded"),
		},
		{
			dbus.WithMatchSender(bluezBusName),
			dbus.WithMatchInterface(objectManagerInterface),
			dbus.WithMatchMember("InterfacesRemoved"),
		},
		{
			dbus.WithMatchSender(bluezBusName),
			dbus.WithMatchInterface(propertiesInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		},
		{
			dbus.WithMatchSender("org.freedesktop.DBus"),
			dbus.WithMatchInterface("org.freedesktop.DBus"),
			dbus.WithMatchMember("NameOwnerChanged"),
			dbus.WithMatchArg(0, bluezBusName),
		},
	}
}

func (b *Bluetooth) bootstrap(publish bool) error {
	objects, err := b.getManagedObjects()
	if err != nil {
		b.setUnavailable(publish)
		return err
	}
	agentRegistered, agentReady := b.registerAgent()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errBluetoothUnavailable
	}
	b.reducer.replaceBluetoothObjects(objects)
	b.agentRegistered = agentRegistered
	b.agentReady = agentReady
	b.actions = make(map[DeviceID]BluetoothAction)
	state := b.stateLocked()
	b.mu.Unlock()
	if publish {
		b.publish(state)
	}
	return nil
}

func (b *Bluetooth) getManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	if b.bus == nil {
		return nil, errBluetoothUnavailable
	}
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := b.bus.object(bluezBusName, bluezRootPath).
		Call(objectManagerInterface+".GetManagedObjects", 0).Store(&objects); err != nil {
		return nil, fmt.Errorf("services: Bluetooth discovery: %w", err)
	}
	return objects, nil
}

func (b *Bluetooth) registerAgent() (registered, ready bool) {
	if b.bus == nil || !b.agentExported {
		return false, false
	}
	manager := b.bus.object(bluezBusName, dbus.ObjectPath("/org/bluez"))
	if err := manager.Call(agentManagerInterface+".RegisterAgent", 0, adapterAgentPath, agentCapability).Err; err != nil {
		return false, false
	}
	if err := manager.Call(agentManagerInterface+".RequestDefaultAgent", 0, adapterAgentPath).Err; err != nil {
		return true, false
	}
	return true, true
}

func (b *Bluetooth) setUnavailable(publish bool) {
	b.agent.failClosed()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.reducer.bluezLost()
	b.agentRegistered = false
	b.agentReady = false
	b.discoveryStarted = false
	b.actions = make(map[DeviceID]BluetoothAction)
	state := b.stateLocked()
	b.mu.Unlock()
	if publish {
		b.publish(state)
	}
}

func (b *Bluetooth) publish(state BluetoothState) {
	b.mu.Lock()
	if !b.closed {
		b.publishLocked(state)
	}
	b.mu.Unlock()
}

func (b *Bluetooth) handleSignal(signal *dbus.Signal) {
	if signal == nil {
		return
	}
	switch signal.Name {
	case "org.freedesktop.DBus.NameOwnerChanged":
		b.handleNameOwnerChanged(signal.Body)
	case objectManagerInterface + ".InterfacesAdded":
		if path, interfaces, ok := interfacesAddedSignal(signal.Body); ok {
			b.applyInterfacesAdded(path, interfaces)
		}
	case objectManagerInterface + ".InterfacesRemoved":
		if path, interfaces, ok := interfacesRemovedSignal(signal.Body); ok {
			b.applyInterfacesRemoved(path, interfaces)
		}
	case propertiesInterface + ".PropertiesChanged":
		if iface, changed, invalidated, ok := propertiesChangedSignal(signal.Body); ok {
			b.applyPropertiesChanged(signal.Path, iface, changed, invalidated)
		}
	}
}

func (b *Bluetooth) handleNameOwnerChanged(body []any) {
	if len(body) != 3 {
		return
	}
	name, okName := body[0].(string)
	_, okOld := body[1].(string)
	newOwner, okNew := body[2].(string)
	if !okName || !okOld || !okNew || name != bluezBusName {
		return
	}
	if newOwner == "" {
		b.setUnavailable(true)
		return
	}
	_ = b.bootstrap(true)
}

func (b *Bluetooth) applyInterfacesAdded(path dbus.ObjectPath, interfaces map[string]map[string]dbus.Variant) {
	b.mu.Lock()
	if b.closed || !b.reducer.available {
		b.mu.Unlock()
		return
	}
	b.reducer.interfacesAdded(path, interfaces)
	state := b.stateLocked()
	b.publishLocked(state)
	b.mu.Unlock()
}

func (b *Bluetooth) applyInterfacesRemoved(path dbus.ObjectPath, interfaces []string) {
	b.mu.Lock()
	if b.closed || !b.reducer.available {
		b.mu.Unlock()
		return
	}
	b.reducer.interfacesRemoved(path, interfaces)
	for id := range b.actions {
		if _, ok := b.reducer.pathForDevice(id); !ok {
			delete(b.actions, id)
		}
	}
	b.publishLocked(b.stateLocked())
	b.mu.Unlock()
}

func (b *Bluetooth) applyPropertiesChanged(path dbus.ObjectPath, iface string, changed map[string]dbus.Variant, invalidated []string) {
	b.mu.Lock()
	if b.closed || !b.reducer.available {
		b.mu.Unlock()
		return
	}
	b.reducer.propertiesChanged(path, iface, changed, invalidated)
	b.publishLocked(b.stateLocked())
	b.mu.Unlock()
}

func interfacesAddedSignal(body []any) (dbus.ObjectPath, map[string]map[string]dbus.Variant, bool) {
	if len(body) != 2 {
		return "", nil, false
	}
	path, okPath := body[0].(dbus.ObjectPath)
	interfaces, okInterfaces := body[1].(map[string]map[string]dbus.Variant)
	return path, interfaces, okPath && okInterfaces && path != ""
}

func interfacesRemovedSignal(body []any) (dbus.ObjectPath, []string, bool) {
	if len(body) != 2 {
		return "", nil, false
	}
	path, okPath := body[0].(dbus.ObjectPath)
	interfaces, okInterfaces := body[1].([]string)
	return path, interfaces, okPath && okInterfaces && path != ""
}

func propertiesChangedSignal(body []any) (string, map[string]dbus.Variant, []string, bool) {
	if len(body) != 3 {
		return "", nil, nil, false
	}
	iface, okIface := body[0].(string)
	changed, okChanged := body[1].(map[string]dbus.Variant)
	invalidated, okInvalidated := body[2].([]string)
	return iface, changed, invalidated, okIface && okChanged && okInvalidated && iface != ""
}

func (b *Bluetooth) adapterPath() (dbus.ObjectPath, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.reducer == nil || !b.reducer.available {
		return "", errBluetoothUnavailable
	}
	path := b.reducer.selectedAdapter()
	if path == "" {
		return "", errBluetoothUnavailable
	}
	return path, nil
}

func (b *Bluetooth) beginDeviceAction(id DeviceID, action BluetoothAction) (dbus.ObjectPath, dbus.ObjectPath, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.reducer == nil || !b.reducer.available {
		return "", "", errBluetoothUnavailable
	}
	devicePath, ok := b.reducer.pathForDevice(id)
	if !ok {
		return "", "", errBluetoothStaleDevice
	}
	if b.actions[id] != BluetoothActionNone {
		return "", "", errBluetoothBusy
	}
	adapterPath := b.reducer.selectedAdapter()
	b.actions[id] = action
	b.publishLocked(b.stateLocked())
	return devicePath, adapterPath, nil
}

func (b *Bluetooth) endDeviceAction(id DeviceID) {
	b.mu.Lock()
	delete(b.actions, id)
	if !b.closed {
		b.publishLocked(b.stateLocked())
	}
	b.mu.Unlock()
}

func (b *Bluetooth) call(path dbus.ObjectPath, method string, args ...any) error {
	if b.bus == nil {
		return errBluetoothUnavailable
	}
	return b.bus.object(bluezBusName, path).Call(method, 0, args...).Err
}

func (b *Bluetooth) setProperty(path dbus.ObjectPath, iface, name string, value any) error {
	return b.call(path, propertiesInterface+".Set", iface, name, dbus.MakeVariant(value))
}

// SetPowered changes the selected adapter without holding the service lock.
func (b *Bluetooth) SetPowered(powered bool) error {
	path, err := b.adapterPath()
	if err != nil {
		return err
	}
	if err := b.setProperty(path, adapterInterface, "Powered", powered); err != nil {
		return err
	}
	if powered {
		return nil
	}
	b.mu.Lock()
	stop := b.discoveryStarted
	b.mu.Unlock()
	if stop {
		err := b.call(path, adapterInterface+".StopDiscovery")
		if err == nil {
			b.mu.Lock()
			b.discoveryStarted = false
			b.mu.Unlock()
		}
		return err
	}
	return nil
}

func (b *Bluetooth) StartDiscovery() error {
	path, err := b.adapterPath()
	if err != nil {
		return err
	}
	if err := b.call(path, adapterInterface+".StartDiscovery"); err != nil {
		return err
	}
	b.mu.Lock()
	if !b.closed {
		b.discoveryStarted = true
	}
	b.mu.Unlock()
	return nil
}

func (b *Bluetooth) StopDiscovery() error {
	path, err := b.adapterPath()
	if err != nil {
		return err
	}
	err = b.call(path, adapterInterface+".StopDiscovery")
	if err == nil {
		b.mu.Lock()
		b.discoveryStarted = false
		b.mu.Unlock()
	}
	return err
}

func (b *Bluetooth) Pair(id DeviceID) error {
	path, _, err := b.beginDeviceAction(id, BluetoothActionPair)
	if err != nil {
		return err
	}
	defer b.endDeviceAction(id)
	if err := b.call(path, deviceInterface+".Pair"); err != nil {
		return err
	}
	// Successful pairing remains valid even if the trust write fails.
	return b.setProperty(path, deviceInterface, "Trusted", true)
}

func (b *Bluetooth) Connect(id DeviceID) error {
	path, _, err := b.beginDeviceAction(id, BluetoothActionConnect)
	if err != nil {
		return err
	}
	defer b.endDeviceAction(id)
	return b.call(path, deviceInterface+".Connect")
}

func (b *Bluetooth) Disconnect(id DeviceID) error {
	path, _, err := b.beginDeviceAction(id, BluetoothActionDisconnect)
	if err != nil {
		return err
	}
	defer b.endDeviceAction(id)
	return b.call(path, deviceInterface+".Disconnect")
}

func (b *Bluetooth) SetTrusted(id DeviceID, trusted bool) error {
	path, _, err := b.beginDeviceAction(id, BluetoothActionTrust)
	if err != nil {
		return err
	}
	defer b.endDeviceAction(id)
	return b.setProperty(path, deviceInterface, "Trusted", trusted)
}

func (b *Bluetooth) Forget(id DeviceID) error {
	path, adapter, err := b.beginDeviceAction(id, BluetoothActionForget)
	if err != nil {
		return err
	}
	defer b.endDeviceAction(id)
	return b.call(adapter, adapterInterface+".RemoveDevice", path)
}

func (b *Bluetooth) Respond(id PromptID, response PairingResponse) error {
	b.mu.Lock()
	closed := b.closed || b.reducer == nil || !b.reducer.available || b.agent == nil
	b.mu.Unlock()
	if closed {
		return errBluetoothUnavailable
	}
	return b.agent.Respond(id, response)
}

func (b *Bluetooth) CancelPrompt(id PromptID) error {
	if b == nil || b.agent == nil {
		return errBluetoothUnavailable
	}
	return b.agent.CancelPrompt(id)
}

// Close rejects any pending prompt, best-effort stops this client's scan,
// unregisters the agent, removes matches, and closes the private connection.
func (b *Bluetooth) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		var adapter dbus.ObjectPath
		if b.reducer != nil {
			adapter = b.reducer.selectedAdapter()
		}
		stopDiscovery := b.discoveryStarted
		registered := b.agentRegistered
		b.discoveryStarted = false
		b.agentRegistered = false
		b.agentReady = false
		if b.reducer != nil {
			b.reducer.bluezLost()
		}
		if b.done != nil {
			close(b.done)
		}
		b.mu.Unlock()

		if b.agent != nil {
			_ = b.agent.Close()
		}
		if b.bus == nil {
			return
		}
		if stopDiscovery && adapter != "" {
			_ = b.call(adapter, adapterInterface+".StopDiscovery")
		}
		if registered {
			_ = b.bus.object(bluezBusName, dbus.ObjectPath("/org/bluez")).
				Call(agentManagerInterface+".UnregisterAgent", 0, adapterAgentPath).Err
		}
		for _, options := range bluetoothMatchRules() {
			_ = b.bus.removeMatch(options...)
		}
		if b.signals != nil {
			b.bus.removeSignal(b.signals)
		}
		b.wg.Wait()
		b.closeErr = b.bus.close()
	})
	return b.closeErr
}

func cloneBluetoothState(state BluetoothState) BluetoothState {
	devices := state.Devices
	state.Devices = make([]BluetoothDevice, len(devices))
	for i, device := range devices {
		state.Devices[i] = cloneBluetoothDevice(device)
	}
	if state.Prompt != nil {
		prompt := cloneBluetoothPrompt(*state.Prompt)
		state.Prompt = &prompt
	}
	return state
}

// BluetoothState is the immutable snapshot consumed by shell hosts.
type BluetoothState struct {
	Available  bool
	Adapter    BluetoothAdapter
	AgentReady bool
	Prompt     *BluetoothPrompt
	Devices    []BluetoothDevice
}

// BluetoothSection is one non-empty device section in body order.
type BluetoothSection uint8

const (
	BluetoothSectionConnected BluetoothSection = iota + 1
	BluetoothSectionPaired
	BluetoothSectionAvailable
)

// BluetoothSectionDevices keeps the section label next to its copied rows.
type BluetoothSectionDevices struct {
	Name    BluetoothSection
	Devices []BluetoothDevice
}

// Sections classifies and stably orders rows for every Bluetooth host.
func (s BluetoothState) Sections() []BluetoothSectionDevices {
	var out []BluetoothSectionDevices
	for _, section := range []BluetoothSection{
		BluetoothSectionConnected,
		BluetoothSectionPaired,
		BluetoothSectionAvailable,
	} {
		rows := make([]BluetoothDevice, 0)
		for _, device := range s.Devices {
			if section == BluetoothSectionConnected && !device.Connected {
				continue
			}
			if section == BluetoothSectionPaired && (!device.Paired || device.Connected) {
				continue
			}
			if section == BluetoothSectionAvailable && (device.Paired || device.Connected) {
				continue
			}
			rows = append(rows, cloneBluetoothDevice(device))
		}
		if len(rows) == 0 {
			continue
		}
		slices.SortStableFunc(rows, func(a, b BluetoothDevice) int {
			if section == BluetoothSectionAvailable {
				if d := bluetoothRSSIBand(b.RSSI) - bluetoothRSSIBand(a.RSSI); d != 0 {
					return d
				}
			}
			if d := strings.Compare(strings.ToLower(a.Alias), strings.ToLower(b.Alias)); d != 0 {
				return d
			}
			return strings.Compare(a.Address, b.Address)
		})
		out = append(out, BluetoothSectionDevices{Name: section, Devices: rows})
	}
	return out
}

func bluetoothRSSIBand(rssi *int16) int {
	if rssi == nil {
		return -1
	}
	switch {
	case *rssi >= -50:
		return 4
	case *rssi >= -60:
		return 3
	case *rssi >= -70:
		return 2
	case *rssi >= -80:
		return 1
	default:
		return 0
	}
}

type bluetoothReducer struct {
	objects   map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	available bool
}

func newBluetoothReducer() *bluetoothReducer {
	return &bluetoothReducer{objects: make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)}
}

// reduceManagedObjects is the pure initial reduction used by tests and startup.
func reduceManagedObjects(objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant) BluetoothState {
	r := newBluetoothReducer()
	return r.replaceBluetoothObjects(objects)
}

func (r *bluetoothReducer) replaceBluetoothObjects(objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant) BluetoothState {
	r.objects = cloneBluetoothObjects(objects)
	r.available = true
	return r.state()
}

func (r *bluetoothReducer) interfacesAdded(path dbus.ObjectPath, interfaces map[string]map[string]dbus.Variant) BluetoothState {
	if r.objects == nil {
		r.objects = make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)
	}
	object := r.objects[path]
	if object == nil {
		object = make(map[string]map[string]dbus.Variant)
		r.objects[path] = object
	}
	for name, properties := range interfaces {
		object[name] = cloneBluetoothProperties(properties)
	}
	return r.state()
}

func (r *bluetoothReducer) interfacesRemoved(path dbus.ObjectPath, interfaces []string) BluetoothState {
	object := r.objects[path]
	for _, name := range interfaces {
		delete(object, name)
	}
	if len(object) == 0 {
		delete(r.objects, path)
	}
	return r.state()
}

func (r *bluetoothReducer) propertiesChanged(path dbus.ObjectPath, iface string, changed map[string]dbus.Variant, invalidated []string) BluetoothState {
	object := r.objects[path]
	if object == nil {
		object = make(map[string]map[string]dbus.Variant)
		r.objects[path] = object
	}
	properties := object[iface]
	if properties == nil {
		properties = make(map[string]dbus.Variant)
		object[iface] = properties
	}
	for name, value := range changed {
		properties[name] = value
	}
	for _, name := range invalidated {
		delete(properties, name)
	}
	return r.state()
}

func (r *bluetoothReducer) bluezLost() BluetoothState {
	r.objects = make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)
	r.available = false
	return r.state()
}

func (r *bluetoothReducer) pathForDevice(id DeviceID) (dbus.ObjectPath, bool) {
	path := dbus.ObjectPath(id)
	object, ok := r.objects[path]
	if !ok || object[deviceInterface] == nil {
		return "", false
	}
	adapter, ok := objectPathProperty(object[deviceInterface], "Adapter")
	if !ok || adapter != r.selectedAdapter() {
		return "", false
	}
	return path, r.available
}

func (r *bluetoothReducer) selectedAdapter() dbus.ObjectPath {
	// ponytail: one adapter matches the shipped host; add adapter selection when a second real consumer needs it.
	paths := make([]dbus.ObjectPath, 0)
	for path, object := range r.objects {
		if object[adapterInterface] != nil {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func (r *bluetoothReducer) state() BluetoothState {
	state := BluetoothState{Available: r.available}
	adapterPath := r.selectedAdapter()
	if adapterPath == "" {
		state.Available = false
		return state
	}
	adapter := r.objects[adapterPath][adapterInterface]
	state.Adapter = BluetoothAdapter{
		Powered:     boolProperty(adapter, "Powered"),
		Pairable:    boolProperty(adapter, "Pairable"),
		Discovering: boolProperty(adapter, "Discovering"),
	}
	for path, object := range r.objects {
		properties := object[deviceInterface]
		if properties == nil {
			continue
		}
		parent, ok := objectPathProperty(properties, "Adapter")
		if !ok || parent != adapterPath {
			continue
		}
		address := stringProperty(properties, "Address")
		alias := stringProperty(properties, "Alias")
		if alias == "" {
			alias = stringProperty(properties, "Name")
		}
		if alias == "" {
			alias = address
		}
		device := BluetoothDevice{
			ID:        DeviceID(path),
			Alias:     alias,
			Address:   address,
			Icon:      stringProperty(properties, "Icon"),
			Paired:    boolProperty(properties, "Paired"),
			Trusted:   boolProperty(properties, "Trusted"),
			Connected: boolProperty(properties, "Connected"),
		}
		if rssi, ok := int16Property(properties, "RSSI"); ok {
			device.RSSI = &rssi
		}
		if battery := object[batteryInterface]; battery != nil {
			if percentage, ok := uint8Property(battery, "Percentage"); ok {
				device.Battery = &percentage
			}
		}
		state.Devices = append(state.Devices, device)
	}
	slices.SortFunc(state.Devices, func(a, b BluetoothDevice) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return state
}

func cloneBluetoothObjects(in map[dbus.ObjectPath]map[string]map[string]dbus.Variant) map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	out := make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant, len(in))
	for path, object := range in {
		copyObject := make(map[string]map[string]dbus.Variant, len(object))
		for iface, properties := range object {
			copyObject[iface] = cloneBluetoothProperties(properties)
		}
		out[path] = copyObject
	}
	return out
}

func cloneBluetoothProperties(in map[string]dbus.Variant) map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(in))
	for name, value := range in {
		out[name] = value
	}
	return out
}

func stringProperty(properties map[string]dbus.Variant, name string) string {
	value, ok := properties[name]
	if !ok {
		return ""
	}
	valueString, _ := value.Value().(string)
	return valueString
}

func boolProperty(properties map[string]dbus.Variant, name string) bool {
	value, ok := properties[name]
	if !ok {
		return false
	}
	valueBool, _ := value.Value().(bool)
	return valueBool
}

func objectPathProperty(properties map[string]dbus.Variant, name string) (dbus.ObjectPath, bool) {
	value, ok := properties[name]
	if !ok {
		return "", false
	}
	path, ok := value.Value().(dbus.ObjectPath)
	return path, ok && path != ""
}

func int16Property(properties map[string]dbus.Variant, name string) (int16, bool) {
	value, ok := properties[name]
	if !ok {
		return 0, false
	}
	switch number := value.Value().(type) {
	case int16:
		return number, true
	case int32:
		return int16(number), true
	case int:
		return int16(number), true
	default:
		return 0, false
	}
}

func uint8Property(properties map[string]dbus.Variant, name string) (uint8, bool) {
	value, ok := properties[name]
	if !ok {
		return 0, false
	}
	switch number := value.Value().(type) {
	case uint8:
		return number, true
	case uint16:
		return uint8(number), true
	case uint32:
		return uint8(number), true
	case int:
		return uint8(number), true
	default:
		return 0, false
	}
}

func cloneBluetoothDevice(device BluetoothDevice) BluetoothDevice {
	if device.Battery != nil {
		battery := *device.Battery
		device.Battery = &battery
	}
	if device.RSSI != nil {
		rssi := *device.RSSI
		device.RSSI = &rssi
	}
	return device
}

func cloneBluetoothPrompt(prompt BluetoothPrompt) BluetoothPrompt { return prompt }

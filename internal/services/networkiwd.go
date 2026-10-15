package services

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/godbus/dbus/v5"
)

// The iwd backend: a second writable backend, accepted by the owner on
// 2026-10-15 (sysc-1209), for machines that run iwd without NetworkManager.
// It speaks to net.connman.iwd on the system bus over the already-pinned
// godbus, and reads wired links from the kernel, which iwd does not manage.
const (
	iwdBusName       = "net.connman.iwd"
	iwdDeviceIface   = "net.connman.iwd.Device"
	iwdStationIface  = "net.connman.iwd.Station"
	iwdNetworkIface  = "net.connman.iwd.Network"
	iwdKnownIface    = "net.connman.iwd.KnownNetwork"
	iwdAgentIface    = "net.connman.iwd.Agent"
	iwdAgentManager  = "net.connman.iwd.AgentManager"
	iwdAgentPath     = dbus.ObjectPath("/one/archpcx/sysc_shell/iwd_agent")
	iwdAgentCanceled = "net.connman.iwd.Agent.Error.Canceled"
)

// iwdOrdered is one entry of Station.GetOrderedNetworks: a network and its
// signal in hundredths of a dBm.
type iwdOrdered struct {
	Path   dbus.ObjectPath
	Signal int16
}

// iwdBus is the system-bus surface the backend needs, so tests drive it with
// a fake object tree instead of a bus.
type iwdBus interface {
	ManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error)
	OrderedNetworks(station dbus.ObjectPath) ([]iwdOrdered, error)
	// Call invokes a method that takes no arguments and returns nothing.
	Call(path dbus.ObjectPath, method string) error
	Set(path dbus.ObjectPath, iface, prop string, v any) error
	Watch(wake chan<- struct{}, stop <-chan struct{}) error
	ExportAgent(a *iwdAgent) (func(), error)
	Close() error
}

type iwdBackend struct {
	bus    iwdBus
	kernel fs.FS
	ipv4   func(string) string
}

// iwdTree is one read of iwd's objects, with the station found.
type iwdTree struct {
	objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	station dbus.ObjectPath
}

var errNoIWDStation = errors.New("services: iwd has no station device")

func (b *iwdBackend) tree() (iwdTree, error) {
	objects, err := b.bus.ManagedObjects()
	if err != nil {
		return iwdTree{}, fmt.Errorf("services: iwd objects: %w", err)
	}
	for path, ifaces := range objects {
		if _, ok := ifaces[iwdStationIface]; ok {
			if _, ok := ifaces[iwdDeviceIface]; ok {
				return iwdTree{objects: objects, station: path}, nil
			}
		}
	}
	return iwdTree{}, errNoIWDStation
}

func (t iwdTree) prop(path dbus.ObjectPath, iface, name string) any {
	if v, ok := t.objects[path][iface][name]; ok {
		return v.Value()
	}
	return nil
}

func (t iwdTree) str(path dbus.ObjectPath, iface, name string) string {
	s, _ := t.prop(path, iface, name).(string)
	return s
}

func (t iwdTree) path(path dbus.ObjectPath, iface, name string) dbus.ObjectPath {
	p, _ := t.prop(path, iface, name).(dbus.ObjectPath)
	return p
}

func (t iwdTree) flag(path dbus.ObjectPath, iface, name string) bool {
	b, _ := t.prop(path, iface, name).(bool)
	return b
}

// dbmStrength scales a signal in hundredths of a dBm to 0..100 the way
// NetworkManager does: -100 dBm and below is 0, -50 dBm and above is 100.
func dbmStrength(signal int16) uint8 {
	dbm := int(signal) / 100
	return uint8(min(100, max(0, 2*(dbm+100))))
}

// State prefers a routed wired link, as the NetworkManager backend does, and
// otherwise reports the station.
func (b *iwdBackend) State() (NetworkState, error) {
	if k, err := readKernelState(b.kernel, b.ipv4); err == nil && k.Kind == ConnWired && k.Connected {
		k.Tier, k.ReadOnly = TierIWD, false
		return k, nil
	}
	t, err := b.tree()
	if err != nil {
		return NetworkState{}, err
	}
	st := NetworkState{Kind: ConnNone, Tier: TierIWD,
		Interface:       t.str(t.station, iwdDeviceIface, "Name"),
		WirelessEnabled: t.flag(t.station, iwdDeviceIface, "Powered"),
		Scanning:        t.flag(t.station, iwdStationIface, "Scanning"),
	}
	switch t.str(t.station, iwdStationIface, "State") {
	case "connected", "roaming":
		st.Kind, st.Connected = ConnWireless, true
		network := t.path(t.station, iwdStationIface, "ConnectedNetwork")
		st.SSID = t.str(network, iwdNetworkIface, "Name")
		if ordered, err := b.bus.OrderedNetworks(t.station); err == nil {
			for _, o := range ordered {
				if o.Path == network {
					st.Strength = dbmStrength(o.Signal)
				}
			}
		}
		if b.ipv4 != nil {
			st.IPv4 = b.ipv4(st.Interface)
		}
	case "connecting":
		st.Kind, st.Resolving = ConnWireless, true
	}
	return st, nil
}

func (b *iwdBackend) AccessPoints() ([]AccessPoint, error) {
	t, err := b.tree()
	if err != nil {
		return nil, err
	}
	ordered, err := b.bus.OrderedNetworks(t.station)
	if err != nil {
		return nil, fmt.Errorf("services: iwd networks: %w", err)
	}
	out := make([]AccessPoint, 0, len(ordered))
	for _, o := range ordered {
		ssid := t.str(o.Path, iwdNetworkIface, "Name")
		if ssid == "" {
			continue
		}
		out = append(out, AccessPoint{
			Path: string(o.Path), DevicePath: string(t.station), SSID: ssid,
			Strength: dbmStrength(o.Signal),
			Secured:  t.str(o.Path, iwdNetworkIface, "Type") != "open",
			Active:   t.flag(o.Path, iwdNetworkIface, "Connected"),
			Saved:    t.path(o.Path, iwdNetworkIface, "KnownNetwork") != "",
		})
	}
	return out, nil
}

func (b *iwdBackend) Scan() error {
	t, err := b.tree()
	if err != nil {
		return err
	}
	return b.bus.Call(t.station, iwdStationIface+".Scan")
}

func (b *iwdBackend) SetWirelessEnabled(on bool) error {
	t, err := b.tree()
	if err != nil {
		return err
	}
	return b.bus.Set(t.station, iwdDeviceIface, "Powered", on)
}

// Activate joins a network. For a secured network iwd asks the registered
// agent for the passphrase, which the panel answers through the secret slot.
func (b *iwdBackend) Activate(ap AccessPoint) error {
	return b.bus.Call(dbus.ObjectPath(ap.Path), iwdNetworkIface+".Connect")
}

func (b *iwdBackend) Forget(ssid string) error {
	t, err := b.tree()
	if err != nil {
		return err
	}
	for path, ifaces := range t.objects {
		if _, ok := ifaces[iwdKnownIface]; ok && t.str(path, iwdKnownIface, "Name") == ssid {
			return b.bus.Call(path, iwdKnownIface+".Forget")
		}
	}
	return fmt.Errorf("services: iwd knows no network %q", ssid)
}

func (b *iwdBackend) Watch(wake chan<- struct{}, stop <-chan struct{}) error {
	return b.bus.Watch(wake, stop)
}

func (b *iwdBackend) Close() error { return b.bus.Close() }

// iwdAgent answers iwd's credential requests from the shell's one secret
// slot, the same prompt NetworkManager's requests reach.
type iwdAgent struct {
	export *secretExport
	bus    iwdBus
}

// RequestPassphrase blocks until the panel answers; godbus runs each call on
// its own goroutine.
func (a *iwdAgent) RequestPassphrase(network dbus.ObjectPath) (string, *dbus.Error) {
	ssid := ""
	if objects, err := a.bus.ManagedObjects(); err == nil {
		ssid = iwdTree{objects: objects}.str(network, iwdNetworkIface, "Name")
	}
	r := a.export.prompt(SecretRequest{SSID: ssid, SettingName: "psk"})
	if r.psk == "" || r.cancelled || r.noSecrets {
		return "", dbus.NewError(iwdAgentCanceled, []any{"no passphrase given"})
	}
	return r.psk, nil
}

func (a *iwdAgent) RequestPrivateKeyPassphrase(dbus.ObjectPath) (string, *dbus.Error) {
	return "", dbus.NewError(iwdAgentCanceled, []any{"enterprise credentials are not answered here"})
}

func (a *iwdAgent) RequestUserNameAndPassword(dbus.ObjectPath) (string, string, *dbus.Error) {
	return "", "", dbus.NewError(iwdAgentCanceled, []any{"enterprise credentials are not answered here"})
}

func (a *iwdAgent) RequestUserPassword(dbus.ObjectPath, string) (string, *dbus.Error) {
	return "", dbus.NewError(iwdAgentCanceled, []any{"enterprise credentials are not answered here"})
}

// Cancel is iwd withdrawing its question, which must free the slot.
func (a *iwdAgent) Cancel(string) *dbus.Error {
	a.export.slot.cancel()
	return nil
}

// Release is iwd dropping the agent; nothing is held to give back.
func (a *iwdAgent) Release() *dbus.Error { return nil }

// systemIWD is iwdBus over the real system bus.
type systemIWD struct{ conn *dbus.Conn }

// newIWDBackend connects to iwd, refusing when it is not running or has no
// station, so selection falls through to the kernel.
func newIWDBackend() (*iwdBackend, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	var owned bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, iwdBusName).Store(&owned); err != nil || !owned {
		return nil, fmt.Errorf("services: iwd is not running")
	}
	be := &iwdBackend{bus: systemIWD{conn: conn}, kernel: rootFS(), ipv4: interfaceIPv4}
	if _, err := be.tree(); err != nil {
		return nil, err
	}
	return be, nil
}

func (s systemIWD) ManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	var out map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := s.conn.Object(iwdBusName, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&out)
	return out, err
}

func (s systemIWD) OrderedNetworks(station dbus.ObjectPath) ([]iwdOrdered, error) {
	var out []iwdOrdered
	err := s.conn.Object(iwdBusName, station).Call(iwdStationIface+".GetOrderedNetworks", 0).Store(&out)
	return out, err
}

func (s systemIWD) Call(path dbus.ObjectPath, method string) error {
	return s.conn.Object(iwdBusName, path).Call(method, 0).Err
}

func (s systemIWD) Set(path dbus.ObjectPath, iface, prop string, v any) error {
	return s.conn.Object(iwdBusName, path).SetProperty(iface+"."+prop, dbus.MakeVariant(v))
}

// Watch wakes the service on any signal iwd sends: a property change, a
// network appearing or going. The service re-reads the whole state either way.
func (s systemIWD) Watch(wake chan<- struct{}, stop <-chan struct{}) error {
	match := []dbus.MatchOption{dbus.WithMatchSender(iwdBusName)}
	if err := s.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	ch := make(chan *dbus.Signal, 16)
	s.conn.Signal(ch)
	go func() {
		defer func() {
			s.conn.RemoveSignal(ch)
			_ = s.conn.RemoveMatchSignal(match...)
		}()
		for {
			select {
			case <-stop:
				return
			case <-ch:
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}
	}()
	return nil
}

func (s systemIWD) ExportAgent(a *iwdAgent) (func(), error) {
	if err := s.conn.Export(a, iwdAgentPath, iwdAgentIface); err != nil {
		return nil, fmt.Errorf("services: export iwd agent: %w", err)
	}
	manager := s.conn.Object(iwdBusName, "/net/connman/iwd")
	if call := manager.Call(iwdAgentManager+".RegisterAgent", 0, iwdAgentPath); call.Err != nil {
		_ = s.conn.Export(nil, iwdAgentPath, iwdAgentIface)
		return nil, fmt.Errorf("services: register iwd agent: %w", call.Err)
	}
	return func() {
		manager.Call(iwdAgentManager+".UnregisterAgent", 0, iwdAgentPath)
		_ = s.conn.Export(nil, iwdAgentPath, iwdAgentIface)
	}, nil
}

// Close leaves the shared system bus connection open; godbus owns it.
func (s systemIWD) Close() error { return nil }

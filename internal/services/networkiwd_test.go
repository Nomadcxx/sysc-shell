package services

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeIWD is iwd's object tree and its calls, with no bus.
type fakeIWD struct {
	objects       map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	ordered       []iwdOrdered
	calls         []string
	sets          []string
	hiddenStation dbus.ObjectPath
	hiddenSSID    string
	callErr       error
	agent         *iwdAgent
}

func (f *fakeIWD) ManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	return f.objects, nil
}
func (f *fakeIWD) OrderedNetworks(dbus.ObjectPath) ([]iwdOrdered, error) { return f.ordered, nil }
func (f *fakeIWD) Call(path dbus.ObjectPath, method string) error {
	f.calls = append(f.calls, string(path)+" "+method)
	return f.callErr
}
func (f *fakeIWD) ConnectHiddenNetwork(station dbus.ObjectPath, ssid string) error {
	f.hiddenStation, f.hiddenSSID = station, ssid
	return f.callErr
}
func (f *fakeIWD) Set(path dbus.ObjectPath, iface, prop string, v any) error {
	f.sets = append(f.sets, string(path)+" "+iface+"."+prop)
	return nil
}
func (f *fakeIWD) Watch(chan<- struct{}, <-chan struct{}) error { return nil }
func (f *fakeIWD) ExportAgent(a *iwdAgent) (func(), error)      { f.agent = a; return func() {}, nil }
func (f *fakeIWD) Close() error                                 { return nil }

func props(kv ...any) map[string]dbus.Variant {
	m := map[string]dbus.Variant{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = dbus.MakeVariant(kv[i+1])
	}
	return m
}

const (
	stationPath = dbus.ObjectPath("/net/connman/iwd/0/4")
	homePath    = dbus.ObjectPath("/net/connman/iwd/0/4/486f6d65_psk")
	cafePath    = dbus.ObjectPath("/net/connman/iwd/0/4/43616665_open")
	knownHome   = dbus.ObjectPath("/net/connman/iwd/486f6d65_psk")
)

func fakeIWDTree(stationState string, powered bool) *fakeIWD {
	station := props("State", stationState, "Scanning", false)
	if stationState == "connected" {
		station["ConnectedNetwork"] = dbus.MakeVariant(homePath)
	}
	return &fakeIWD{
		objects: map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
			stationPath: {
				iwdDeviceIface:  props("Name", "wlan0", "Powered", powered, "Mode", "station"),
				iwdStationIface: station,
			},
			homePath: {iwdNetworkIface: props("Name", "Home", "Type", "psk", "Connected", stationState == "connected",
				"Device", stationPath, "KnownNetwork", knownHome)},
			cafePath:  {iwdNetworkIface: props("Name", "Cafe", "Type", "open", "Connected", false, "Device", stationPath)},
			knownHome: {iwdKnownIface: props("Name", "Home", "Type", "psk")},
		},
		ordered: []iwdOrdered{{Path: homePath, Signal: -5500}, {Path: cafePath, Signal: -8000}},
	}
}

func newTestIWD(f *fakeIWD) *iwdBackend {
	return &iwdBackend{bus: f, kernel: kernelFixture(map[string]string{"wlan0": "up"}, []string{"wlan0"}, routeHeader, ""),
		ipv4: func(string) string { return "10.0.0.5" }}
}

func TestIWDReportsTheSameShapesAsNetworkManager(t *testing.T) {
	st, err := newTestIWD(fakeIWDTree("connected", true)).State()
	if err != nil {
		t.Fatal(err)
	}
	want := NetworkState{Kind: ConnWireless, Connected: true, WirelessEnabled: true, SSID: "Home",
		Interface: "wlan0", IPv4: "10.0.0.5", Strength: 90, Tier: TierIWD}
	if st != want {
		t.Fatalf("state\n got %+v\nwant %+v", st, want)
	}

	aps, err := newTestIWD(fakeIWDTree("connected", true)).AccessPoints()
	if err != nil {
		t.Fatal(err)
	}
	wantAPs := []AccessPoint{
		{Path: string(homePath), DevicePath: string(stationPath), SSID: "Home", Strength: 90, Secured: true, Active: true, Saved: true},
		{Path: string(cafePath), DevicePath: string(stationPath), SSID: "Cafe", Strength: 40},
	}
	if !slices.Equal(aps, wantAPs) {
		t.Fatalf("access points\n got %+v\nwant %+v", aps, wantAPs)
	}
}

func TestIWDStates(t *testing.T) {
	for _, tc := range []struct {
		state   string
		powered bool
		want    NetworkState
	}{
		{"connecting", true, NetworkState{Kind: ConnWireless, Resolving: true, WirelessEnabled: true, Interface: "wlan0", Tier: TierIWD}},
		{"disconnected", true, NetworkState{Kind: ConnNone, WirelessEnabled: true, Interface: "wlan0", Tier: TierIWD}},
		{"disconnected", false, NetworkState{Kind: ConnNone, Interface: "wlan0", Tier: TierIWD}},
	} {
		got, err := newTestIWD(fakeIWDTree(tc.state, tc.powered)).State()
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s powered=%v\n got %+v\nwant %+v", tc.state, tc.powered, got, tc.want)
		}
	}
}

func TestIWDWritesGoToTheRightObjects(t *testing.T) {
	f := fakeIWDTree("disconnected", true)
	be := newTestIWD(f)
	_ = be.Scan()
	_ = be.SetWirelessEnabled(false)
	_ = be.Activate(AccessPoint{Path: string(cafePath), SSID: "Cafe"})
	_ = be.Forget("Home")
	wantCalls := []string{
		string(stationPath) + " " + iwdStationIface + ".Scan",
		string(cafePath) + " " + iwdNetworkIface + ".Connect",
		string(knownHome) + " " + iwdKnownIface + ".Forget",
	}
	if !slices.Equal(f.calls, wantCalls) {
		t.Errorf("calls\n got %v\nwant %v", f.calls, wantCalls)
	}
	if !slices.Equal(f.sets, []string{string(stationPath) + " " + iwdDeviceIface + ".Powered"}) {
		t.Errorf("sets = %v", f.sets)
	}
	if err := be.Forget("Nowhere"); err == nil {
		t.Error("forgetting an unknown network succeeded")
	}
}

func TestIWDConnectsToManuallyEnteredHiddenSSID(t *testing.T) {
	f := fakeIWDTree("disconnected", true)
	be := newTestIWD(f)
	if err := be.ConnectManual(ManualNetworkRequest{SSID: "Not scanned", Security: ManualSecurityWPAPersonal}); err != nil {
		t.Fatal(err)
	}
	if f.hiddenStation != stationPath || f.hiddenSSID != "Not scanned" {
		t.Fatalf("ConnectHiddenNetwork station=%q ssid=%q", f.hiddenStation, f.hiddenSSID)
	}
}

func TestIWDAsksForAPassphraseThroughTheSharedSlot(t *testing.T) {
	f := fakeIWDTree("disconnected", true)
	n := NewNetwork(newTestIWD(f))
	if err := n.startSecrets(func(e *secretExport) (func(), error) { return f.ExportAgent(&iwdAgent{export: e, bus: f}) }); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		psk string
		err *dbus.Error
	}
	done := make(chan answer, 1)
	go func() {
		psk, err := f.agent.RequestPassphrase(homePath)
		done <- answer{psk, err}
	}()
	select {
	case req := <-n.SecretRequests():
		if req.SSID != "Home" {
			t.Fatalf("prompt for %q, want Home", req.SSID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("iwd's request never reached the panel")
	}
	if err := n.SubmitSecret("hunter22"); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.err != nil || got.psk != "hunter22" {
		t.Fatalf("agent answered %+v", got)
	}

	go func() {
		psk, err := f.agent.RequestPassphrase(homePath)
		done <- answer{psk, err}
	}()
	<-n.SecretRequests()
	n.CancelSecret()
	if got := <-done; got.err == nil || got.err.Name != iwdAgentCanceled {
		t.Fatalf("a cancelled prompt answered %+v", got)
	}
}

func TestSelectionFallsFromTheManagerToIWDToTheKernel(t *testing.T) {
	kernel := kernelFixture(map[string]string{"eth0": "up"}, nil, defaultRoute("eth0"), "")
	none := func() (backend, error) { return nil, errors.New("absent") }
	iwd := func() (backend, error) { return newTestIWD(fakeIWDTree("connected", true)), nil }
	for _, tc := range []struct {
		name    string
		nm, iwd func() (backend, error)
		want    Tier
	}{
		{"manager", func() (backend, error) { return &fakeBackend{}, nil }, iwd, TierNM},
		{"iwd", none, iwd, TierIWD},
		{"kernel", none, none, TierKernel},
	} {
		if _, got := selectNetworkBackend(tc.nm, tc.iwd, kernel, nil); got != tc.want {
			t.Errorf("%s: tier %v, want %v", tc.name, got, tc.want)
		}
	}
}

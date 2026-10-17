package services

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gnm "github.com/Wifx/gonetworkmanager/v2"
	"github.com/godbus/dbus/v5"
)

// These are the only tests that touch the real backend constructor. Everything
// else runs against the fake: a test that needs NetworkManager running is a
// test that fails on a build machine.
func TestNMBackendUnavailableWithoutBus(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/nonexistent")
	if _, err := newNMBackend(); err == nil {
		t.Fatal("expected an error when the system bus is unreachable")
	}
}

func TestNMVPNProfilesUseUUIDStateAndOnlyExposeMetadata(t *testing.T) {
	first := "11111111-1111-4111-8111-111111111111"
	second := "22222222-2222-4222-8222-222222222222"
	settings := []gnm.ConnectionSettings{
		{
			"connection":  {"id": "Office", "uuid": first, "type": "vpn"},
			"vpn":         {"service-type": "org.example"},
			"vpn-secrets": {"password": "stored-secret"},
		},
		{"connection": {"id": "Office", "uuid": second, "type": "vpn"}},
		{"connection": {"id": "Wired", "uuid": "33333333-3333-4333-8333-333333333333", "type": "802-3-ethernet"}},
	}
	got := nmVPNProfilesFromSettings(settings, map[string]VPNStatus{first: VPNConnected})
	want := []VPNProfile{
		{UUID: first, Name: "Office", Status: VPNConnected},
		{UUID: second, Name: "Office", Status: VPNDisconnected},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("VPN profiles = %+v, want %+v", got, want)
	}
	if strings.Contains(fmt.Sprint(got), "stored-secret") {
		t.Fatal("VPN profile snapshot exposed a stored secret")
	}
}

func TestManualNetworkSettingsMarkHiddenAndOmitPassphrase(t *testing.T) {
	for _, tc := range []struct {
		name     string
		security ManualSecurity
	}{
		{"open", ManualSecurityOpen},
		{"wpa personal", ManualSecurityWPAPersonal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := manualNetworkSettings(ManualNetworkRequest{SSID: "Hidden", Security: tc.security})
			if err != nil {
				t.Fatal(err)
			}
			wireless := settings["802-11-wireless"]
			if got := wireless["hidden"]; got != true {
				t.Fatalf("hidden setting = %v, want true", got)
			}
			if got := string(wireless["ssid"].([]byte)); got != "Hidden" {
				t.Fatalf("SSID setting = %q, want Hidden", got)
			}
			security, ok := settings["802-11-wireless-security"]
			if tc.security == ManualSecurityOpen {
				if ok {
					t.Fatalf("open profile unexpectedly has security settings: %v", security)
				}
				return
			}
			if !ok || security["key-mgmt"] != "wpa-psk" {
				t.Fatalf("WPA-Personal settings = %v", security)
			}
			if _, ok := security["psk-flags"]; ok {
				t.Fatalf("WPA-Personal profile must let NetworkManager retain its secret: %v", security)
			}
			if _, ok := security["psk"]; ok {
				t.Fatal("profile settings embedded a passphrase")
			}
		})
	}
}

type manualTestSettings struct {
	gnm.Settings
	connections []gnm.Connection
	err         error
}

func (s *manualTestSettings) ListConnections() ([]gnm.Connection, error) {
	return s.connections, s.err
}

type manualTestConnection struct {
	gnm.Connection
	settings gnm.ConnectionSettings
	err      error
}

func (c *manualTestConnection) GetSettings() (gnm.ConnectionSettings, error) {
	return c.settings, c.err
}

type manualTestDevice struct{ gnm.Device }

func (manualTestDevice) GetPropertyDeviceType() (gnm.NmDeviceType, error) {
	return gnm.NmDeviceTypeWifi, nil
}

type manualTestNetworkManager struct {
	gnm.NetworkManager
	devices          []gnm.Device
	activated        []gnm.Connection
	activeConnection gnm.ActiveConnection
	addCalls         int
}

func (n *manualTestNetworkManager) GetDevices() ([]gnm.Device, error) {
	return n.devices, nil
}

func (n *manualTestNetworkManager) ActivateConnection(connection gnm.Connection, _ gnm.Device, _ *dbus.Object) (gnm.ActiveConnection, error) {
	n.activated = append(n.activated, connection)
	return n.active(), nil
}

func (n *manualTestNetworkManager) AddAndActivateConnection(map[string]map[string]interface{}, gnm.Device) (gnm.ActiveConnection, error) {
	n.addCalls++
	return n.active(), nil
}

func (n *manualTestNetworkManager) active() gnm.ActiveConnection {
	if n.activeConnection != nil {
		return n.activeConnection
	}
	return &manualTestActiveConnection{state: gnm.NmActiveConnectionStateActivated}
}

func manualTestProfile(ssid string, security ManualSecurity, hidden bool) gnm.ConnectionSettings {
	settings := gnm.ConnectionSettings{
		"connection":      {"id": ssid, "type": "802-11-wireless"},
		"802-11-wireless": {"ssid": []byte(ssid), "mode": "infrastructure", "hidden": hidden},
	}
	if security == ManualSecurityWPAPersonal {
		settings["802-11-wireless-security"] = map[string]interface{}{
			"key-mgmt":  "wpa-psk",
			"psk-flags": uint32(1),
		}
	}
	return settings
}

type manualTestActiveConnection struct {
	gnm.ActiveConnection
	path             dbus.ObjectPath
	state            gnm.NmActiveConnectionState
	stateChanges     []gnm.StateChange
	stateChangeDelay time.Duration
}

func (a *manualTestActiveConnection) GetPath() dbus.ObjectPath { return a.path }
func (a *manualTestActiveConnection) GetPropertyState() (gnm.NmActiveConnectionState, error) {
	return a.state, nil
}
func (a *manualTestActiveConnection) SubscribeState(receiver chan gnm.StateChange, exit chan struct{}) error {
	if a.stateChangeDelay > 0 {
		go func() {
			timer := time.NewTimer(a.stateChangeDelay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-exit:
				return
			}
			for _, change := range a.stateChanges {
				select {
				case receiver <- change:
				case <-exit:
					return
				}
			}
		}()
		return nil
	}
	for _, change := range a.stateChanges {
		select {
		case receiver <- change:
		case <-exit:
			return nil
		}
	}
	return nil
}

func TestNMConnectVPNReportsAsynchronousActivationFailure(t *testing.T) {
	const uuid = "11111111-1111-4111-8111-111111111111"
	path := dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/17")
	active := &manualTestActiveConnection{
		path:             path,
		state:            gnm.NmActiveConnectionStateDeactivated,
		stateChangeDelay: time.Millisecond,
		stateChanges: []gnm.StateChange{{
			Path: path, State: gnm.NmActiveConnectionStateDeactivated,
			Reason: gnm.NmActiveConnectionStateReasonNoSecrets,
		}},
	}
	nm := &manualTestNetworkManager{activeConnection: active}
	settings := &manualTestSettings{connections: []gnm.Connection{
		&manualTestConnection{settings: gnm.ConnectionSettings{
			"connection": {"id": "Office", "uuid": uuid, "type": "vpn"},
		}},
	}}
	backend := &nmBackend{nm: nm, settings: settings}

	err := backend.ConnectVPN(uuid)
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("ConnectVPN error = %v, want an actionable missing-credentials error", err)
	}
}

func TestNMConnectVPNTreatsUserDisconnectAsCancellation(t *testing.T) {
	const uuid = "11111111-1111-4111-8111-111111111111"
	path := dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/19")
	active := &manualTestActiveConnection{
		path:             path,
		state:            gnm.NmActiveConnectionStateDeactivated,
		stateChangeDelay: time.Millisecond,
		stateChanges: []gnm.StateChange{{
			Path: path, State: gnm.NmActiveConnectionStateDeactivated,
			Reason: gnm.NmActiveConnectionStateReasonUserDisconnected,
		}},
	}
	nm := &manualTestNetworkManager{activeConnection: active}
	settings := &manualTestSettings{connections: []gnm.Connection{
		&manualTestConnection{settings: gnm.ConnectionSettings{
			"connection": {"id": "Office", "uuid": uuid, "type": "vpn"},
		}},
	}}
	backend := &nmBackend{nm: nm, settings: settings}

	if err := backend.ConnectVPN(uuid); err != nil {
		t.Fatalf("ConnectVPN after user cancellation = %v, want no error", err)
	}
}

func TestNMConnectManualReportsAsynchronousActivationFailure(t *testing.T) {
	path := dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/18")
	active := &manualTestActiveConnection{
		path:  path,
		state: gnm.NmActiveConnectionStateActivating,
		stateChanges: []gnm.StateChange{{
			Path: path, State: gnm.NmActiveConnectionStateDeactivated,
			Reason: gnm.NmActiveConnectionStateReasonNoSecrets,
		}},
	}
	nm := &manualTestNetworkManager{devices: []gnm.Device{manualTestDevice{}}, activeConnection: active}
	backend := &nmBackend{nm: nm, settings: &manualTestSettings{}}

	err := backend.ConnectManual(ManualNetworkRequest{SSID: "Hidden", Security: ManualSecurityWPAPersonal})
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("ConnectManual error = %v, want an actionable missing-credentials error", err)
	}
}

func TestNMConnectManualReusesOnlyMatchingSavedProfile(t *testing.T) {
	for _, tc := range []struct {
		name            string
		ssid            string
		hidden          bool
		savedSecurity   ManualSecurity
		requestSecurity ManualSecurity
		wantReuse       bool
	}{
		{"same WPA-Personal hidden profile", "Hidden", true, ManualSecurityWPAPersonal, ManualSecurityWPAPersonal, true},
		{"same open hidden profile", "Hidden", true, ManualSecurityOpen, ManualSecurityOpen, true},
		{"different security", "Hidden", true, ManualSecurityOpen, ManualSecurityWPAPersonal, false},
		{"different SSID", "Other", true, ManualSecurityWPAPersonal, ManualSecurityWPAPersonal, false},
		{"visible profile", "Hidden", false, ManualSecurityWPAPersonal, ManualSecurityWPAPersonal, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := &manualTestConnection{settings: manualTestProfile(tc.ssid, tc.savedSecurity, tc.hidden)}
			nm := &manualTestNetworkManager{devices: []gnm.Device{manualTestDevice{}}}
			backend := &nmBackend{
				nm:       nm,
				settings: &manualTestSettings{connections: []gnm.Connection{saved}},
			}
			if err := backend.ConnectManual(ManualNetworkRequest{SSID: "Hidden", Security: tc.requestSecurity}); err != nil {
				t.Fatal(err)
			}
			if tc.wantReuse {
				if nm.addCalls != 0 || len(nm.activated) != 1 || nm.activated[0] != saved {
					t.Fatalf("manual join added %d profiles and activated %v; want saved profile reused", nm.addCalls, nm.activated)
				}
			} else if nm.addCalls != 1 || len(nm.activated) != 0 {
				t.Fatalf("manual join added %d profiles and activated %v; want a new profile", nm.addCalls, nm.activated)
			}
		})
	}
}

func TestNMConnectManualDoesNotAddWhenSavedProfilesCannotBeInspected(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings *manualTestSettings
	}{
		{"listing fails", &manualTestSettings{err: fmt.Errorf("settings unavailable")}},
		{"profile read fails", &manualTestSettings{connections: []gnm.Connection{
			&manualTestConnection{err: fmt.Errorf("profile unavailable")},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nm := &manualTestNetworkManager{devices: []gnm.Device{manualTestDevice{}}}
			backend := &nmBackend{nm: nm, settings: tc.settings}
			if err := backend.ConnectManual(ManualNetworkRequest{SSID: "Hidden", Security: ManualSecurityOpen}); err == nil {
				t.Fatal("manual join succeeded without inspecting existing profiles")
			}
			if nm.addCalls != 0 {
				t.Fatalf("manual join added %d profiles after profile inspection failed", nm.addCalls)
			}
		})
	}
}

// TestNMBackendUnavailableWhenNothingAnswers is the machine the kernel tier
// exists for: a system bus that works, with no NetworkManager on it. A
// constructor that only connects would claim the NM tier there and leave the
// panel reporting a radio that is off.
func TestNMBackendUnavailableWhenNothingAnswers(t *testing.T) {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address", "--address="+addr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", strings.TrimSpace(line))
	if _, err := newNMBackend(); err == nil {
		t.Fatal("a bus with no NetworkManager on it was taken for NetworkManager")
	}
}

package services

import (
	"fmt"

	gnm "github.com/Wifx/gonetworkmanager/v2"
)

// nmBackend is the only production implementation of backend. It wraps the
// pinned gonetworkmanager binding; nothing above it knows that D-Bus exists.
type nmBackend struct {
	nm       gnm.NetworkManager
	settings gnm.Settings
}

func newNMBackend() (backend, error) {
	nm, err := gnm.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("services: network manager: %w", err)
	}
	set, err := gnm.NewSettings()
	if err != nil {
		return nil, fmt.Errorf("services: network settings: %w", err)
	}
	return &nmBackend{nm: nm, settings: set}, nil
}

func (b *nmBackend) Close() error { return nil }

// devices picks the first Wi-Fi and the first wired device. A machine with two
// of either is not a case this shell distinguishes.
func (b *nmBackend) devices() (wifi, wired gnm.Device) {
	devs, err := b.nm.GetDevices()
	if err != nil {
		return nil, nil
	}
	for _, d := range devs {
		t, err := d.GetPropertyDeviceType()
		if err != nil {
			continue
		}
		switch t {
		case gnm.NmDeviceTypeWifi:
			if wifi == nil {
				wifi = d
			}
		case gnm.NmDeviceTypeEthernet:
			if wired == nil {
				wired = d
			}
		}
	}
	return wifi, wired
}

// State prefers an activated wired link over Wi-Fi, which is what the machine
// is actually routing over when both are up.
func (b *nmBackend) State() (NetworkState, error) {
	st := NetworkState{Kind: ConnUnknown}
	if en, err := b.nm.GetPropertyWirelessEnabled(); err == nil {
		st.WirelessEnabled = en
	}

	wifi, wired := b.devices()

	if wired != nil && deviceActivated(wired) {
		st.Kind = ConnWired
		st.Connected = true
		st.Interface, _ = wired.GetPropertyInterface()
		st.IPv4 = deviceIPv4(wired)
		return st, nil
	}

	if wifi != nil {
		st.Interface, _ = wifi.GetPropertyInterface()
		if deviceActivated(wifi) {
			st.Kind = ConnWireless
			st.Connected = true
			st.IPv4 = deviceIPv4(wifi)
			if ap := activeAccessPoint(wifi); ap != nil {
				st.SSID, _ = ap.GetPropertySSID()
				st.Strength, _ = ap.GetPropertyStrength()
			}
			return st, nil
		}
		// An active connection on a device that is not yet activated is the
		// activating case. Deriving it this way avoids depending on the
		// activation-state enum's spelling.
		if ac, err := wifi.GetPropertyActiveConnection(); err == nil && ac != nil {
			st.Kind = ConnWireless
			st.Resolving = true
			return st, nil
		}
	}

	if st.Kind == ConnUnknown {
		st.Kind = ConnNone
	}
	return st, nil
}

func (b *nmBackend) AccessPoints() ([]AccessPoint, error) {
	wifi, _ := b.devices()
	if wifi == nil {
		return nil, nil
	}
	w, err := gnm.NewDeviceWireless(wifi.GetPath())
	if err != nil {
		return nil, fmt.Errorf("services: wireless device: %w", err)
	}
	found, err := w.GetAllAccessPoints()
	if err != nil {
		return nil, fmt.Errorf("services: access points: %w", err)
	}

	saved := b.savedSSIDs()
	activeSSID := ""
	if ap := activeAccessPoint(wifi); ap != nil {
		activeSSID, _ = ap.GetPropertySSID()
	}

	out := make([]AccessPoint, 0, len(found))
	seen := make(map[string]struct{}, len(found))
	for _, ap := range found {
		ssid, err := ap.GetPropertySSID()
		if err != nil || ssid == "" {
			continue // a hidden network has no name to show
		}
		if _, dup := seen[ssid]; dup {
			continue // one row per name, not one per radio
		}
		seen[ssid] = struct{}{}

		strength, _ := ap.GetPropertyStrength()
		rsn, _ := ap.GetPropertyRSNFlags()
		wpa, _ := ap.GetPropertyWPAFlags()

		out = append(out, AccessPoint{
			DevicePath: string(wifi.GetPath()),
			SSID:       ssid,
			Strength:   strength,
			Secured:    rsn != uint32(gnm.Nm80211APSecNone) || wpa != uint32(gnm.Nm80211APSecNone),
			Active:     activeSSID != "" && ssid == activeSSID,
			Saved:      saved[ssid],
		})
	}
	return out, nil
}

func (b *nmBackend) Scan() error {
	wifi, _ := b.devices()
	if wifi == nil {
		return fmt.Errorf("services: no wireless device")
	}
	w, err := gnm.NewDeviceWireless(wifi.GetPath())
	if err != nil {
		return fmt.Errorf("services: wireless device: %w", err)
	}
	return w.RequestScan()
}

func (b *nmBackend) SetWirelessEnabled(on bool) error {
	return b.nm.SetPropertyWirelessEnabled(on)
}

// Activate connects to ap. A saved profile is activated directly; an unsaved
// one is added and activated, which is what makes NetworkManager ask a
// registered secret holder for the passphrase.
func (b *nmBackend) Activate(ap AccessPoint) error {
	wifi, _ := b.devices()
	if wifi == nil {
		return fmt.Errorf("services: no wireless device")
	}
	if conn := b.connectionFor(ap.SSID); conn != nil {
		_, err := b.nm.ActivateConnection(conn, wifi, nil)
		return err
	}

	settings := map[string]map[string]interface{}{
		"connection": {
			"id":   ap.SSID,
			"type": "802-11-wireless",
		},
		"802-11-wireless": {
			"ssid": []byte(ap.SSID),
			"mode": "infrastructure",
		},
	}
	if ap.Secured {
		// No psk here: the passphrase is answered over the bus by the secret
		// holder, so it never passes through this process's memory as part of
		// a settings map.
		settings["802-11-wireless-security"] = map[string]interface{}{
			"key-mgmt": "wpa-psk",
		}
	}
	_, err := b.nm.AddAndActivateConnection(settings, wifi)
	return err
}

func (b *nmBackend) Forget(ssid string) error {
	conn := b.connectionFor(ssid)
	if conn == nil {
		return fmt.Errorf("services: no saved network %q", ssid)
	}
	return conn.Delete()
}

// Watch forwards every NetworkManager signal as a bare wake. The service
// re-reads state rather than decoding signal bodies, so there is one decode
// path instead of two.
func (b *nmBackend) Watch(wake chan<- struct{}, stop <-chan struct{}) error {
	signals := b.nm.Subscribe()
	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-signals:
				if !ok {
					return
				}
				select {
				case wake <- struct{}{}:
				default: // a wake is already pending; one re-read covers each
				}
			}
		}
	}()
	return nil
}

func (b *nmBackend) savedSSIDs() map[string]bool {
	out := make(map[string]bool)
	conns, err := b.settings.ListConnections()
	if err != nil {
		return out
	}
	for _, c := range conns {
		s, err := c.GetSettings()
		if err != nil {
			continue
		}
		if ssid, ok := settingsSSID(s); ok {
			out[ssid] = true
		}
	}
	return out
}

func (b *nmBackend) connectionFor(ssid string) gnm.Connection {
	conns, err := b.settings.ListConnections()
	if err != nil {
		return nil
	}
	for _, c := range conns {
		s, err := c.GetSettings()
		if err != nil {
			continue
		}
		if got, ok := settingsSSID(s); ok && got == ssid {
			return c
		}
	}
	return nil
}

// settingsSSID reads the SSID out of a connection profile. NetworkManager
// carries it as a byte array, but some producers write a string.
func settingsSSID(s gnm.ConnectionSettings) (string, bool) {
	wireless, ok := s["802-11-wireless"]
	if !ok {
		return "", false
	}
	switch v := wireless["ssid"].(type) {
	case []byte:
		return string(v), true
	case string:
		return v, true
	}
	return "", false
}

func deviceActivated(d gnm.Device) bool {
	st, err := d.GetPropertyState()
	return err == nil && st == gnm.NmDeviceStateActivated
}

func deviceIPv4(d gnm.Device) string {
	cfg, err := d.GetPropertyIP4Config()
	if err != nil || cfg == nil {
		return ""
	}
	addrs, err := cfg.GetPropertyAddressData()
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0].Address
}

func activeAccessPoint(d gnm.Device) gnm.AccessPoint {
	ac, err := d.GetPropertyActiveConnection()
	if err != nil || ac == nil {
		return nil
	}
	ap, err := ac.GetPropertySpecificObject()
	if err != nil {
		return nil
	}
	return ap
}

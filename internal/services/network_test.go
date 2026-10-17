package services

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSignalBandMatchesReferenceThresholds(t *testing.T) {
	cases := []struct {
		signal uint8
		band   int
	}{{100, 4}, {80, 4}, {79, 3}, {60, 3}, {59, 2}, {35, 2}, {34, 1}, {15, 1}, {14, 0}, {0, 0}}
	for _, c := range cases {
		if got := SignalBand(c.signal); got != c.band {
			t.Errorf("SignalBand(%d) = %d, want %d", c.signal, got, c.band)
		}
	}
}

// fakeBackend stands in for NetworkManager. Every service test runs against it,
// so no test needs a live system bus.
type fakeBackend struct {
	state          NetworkState
	aps            []AccessPoint
	scans          int
	manualRequests []ManualNetworkRequest
	vpns           []VPNProfile
	vpnErr         error
	vpnConnects    []string
	vpnDisconnects []string
	vpnControlErr  error
	wakeCh         chan<- struct{}
}

func (f *fakeBackend) State() (NetworkState, error)         { return f.state, nil }
func (f *fakeBackend) AccessPoints() ([]AccessPoint, error) { return f.aps, nil }
func (f *fakeBackend) Scan() error                          { f.scans++; return nil }
func (f *fakeBackend) SetWirelessEnabled(on bool) error     { f.state.WirelessEnabled = on; return nil }
func (f *fakeBackend) Activate(ap AccessPoint) error        { return nil }
func (f *fakeBackend) Forget(ssid string) error             { return nil }
func (f *fakeBackend) ConnectManual(req ManualNetworkRequest) error {
	f.manualRequests = append(f.manualRequests, req)
	return nil
}
func (f *fakeBackend) VPNProfiles() ([]VPNProfile, error) { return f.vpns, f.vpnErr }
func (f *fakeBackend) ConnectVPN(uuid string) error {
	f.vpnConnects = append(f.vpnConnects, uuid)
	if f.vpnControlErr == nil {
		for i := range f.vpns {
			if f.vpns[i].UUID == uuid {
				f.vpns[i].Status = VPNConnecting
			}
		}
	}
	return f.vpnControlErr
}
func (f *fakeBackend) DisconnectVPN(uuid string) error {
	f.vpnDisconnects = append(f.vpnDisconnects, uuid)
	if f.vpnControlErr == nil {
		for i := range f.vpns {
			if f.vpns[i].UUID == uuid {
				f.vpns[i].Status = VPNDisconnecting
			}
		}
	}
	return f.vpnControlErr
}
func (f *fakeBackend) Close() error { return nil }

func (f *fakeBackend) Watch(wake chan<- struct{}, stop <-chan struct{}) error {
	f.wakeCh = wake
	return nil
}

func TestAccessPointsSortByBandNotPercent(t *testing.T) {
	f := &fakeBackend{aps: []AccessPoint{
		{SSID: "weak", Strength: 20},
		{SSID: "beta", Strength: 81},
		{SSID: "alpha", Strength: 95}, // same band as beta; SSID breaks the tie
	}}
	n := NewNetwork(f)
	got := n.AccessPoints()
	want := []string{"alpha", "beta", "weak"}
	for i, w := range want {
		if got[i].SSID != w {
			t.Fatalf("position %d = %q, want %q", i, got[i].SSID, w)
		}
	}
}

func TestNetworkManualRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ManualNetworkRequest
		want bool
	}{
		{"open", ManualNetworkRequest{SSID: "Hidden", Security: ManualSecurityOpen}, true},
		{"personal", ManualNetworkRequest{SSID: "Hidden", Security: ManualSecurityWPAPersonal}, true},
		{"empty SSID", ManualNetworkRequest{Security: ManualSecurityOpen}, false},
		{"SSID exceeds 32 bytes", ManualNetworkRequest{SSID: strings.Repeat("x", 33), Security: ManualSecurityOpen}, false},
		{"SSID contains NUL", ManualNetworkRequest{SSID: "ab\x00cd", Security: ManualSecurityOpen}, false},
		{"enterprise", ManualNetworkRequest{SSID: "Hidden", Security: ManualSecurityWPAEnterprise}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err == nil) != tc.want {
				t.Fatalf("Validate() error = %v, want valid=%v", err, tc.want)
			}
		})
	}
}

func TestNetworkConnectManualValidatesBeforeBackend(t *testing.T) {
	f := &fakeBackend{}
	n := NewNetwork(f)
	want := ManualNetworkRequest{SSID: "Hidden", Security: ManualSecurityWPAPersonal}
	if err := n.ConnectManual(want); err != nil {
		t.Fatal(err)
	}
	if len(f.manualRequests) != 1 || f.manualRequests[0] != want {
		t.Fatalf("manual requests = %+v, want one request %+v", f.manualRequests, want)
	}
	if err := n.ConnectManual(ManualNetworkRequest{Security: ManualSecurityOpen}); err == nil {
		t.Fatal("empty SSID was accepted")
	}
	if len(f.manualRequests) != 1 {
		t.Fatalf("invalid request reached backend: %+v", f.manualRequests)
	}
}

func TestNetworkRefreshesAndControlsVPNProfilesByUUID(t *testing.T) {
	profiles := []VPNProfile{
		{UUID: "11111111-1111-4111-8111-111111111111", Name: "Office", Status: VPNConnected},
		{UUID: "22222222-2222-4222-8222-222222222222", Name: "Office", Status: VPNDisconnected},
	}
	f := &fakeBackend{vpns: profiles}
	n := NewNetwork(f)
	if err := n.RefreshVPNProfiles(); err != nil {
		t.Fatal(err)
	}
	if got := n.CachedVPNProfiles(); !slices.Equal(got, profiles) {
		t.Fatalf("cached VPN profiles = %+v, want %+v", got, profiles)
	}
	if err := n.ConnectVPN(profiles[1].UUID); err != nil {
		t.Fatal(err)
	}
	if err := n.DisconnectVPN(profiles[0].UUID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.vpnConnects, []string{profiles[1].UUID}) || !slices.Equal(f.vpnDisconnects, []string{profiles[0].UUID}) {
		t.Fatalf("VPN control identities connect=%v disconnect=%v", f.vpnConnects, f.vpnDisconnects)
	}
}

func TestNetworkVPNBackendErrorsAreRetainedWithoutSecrets(t *testing.T) {
	f := &fakeBackend{vpnErr: errors.New("profile listing failed"), vpnControlErr: errors.New("connection failed")}
	n := NewNetwork(f)
	if err := n.RefreshVPNProfiles(); err == nil || n.CachedVPNError() != "profile listing failed" {
		t.Fatalf("refresh error=%v cached=%q", err, n.CachedVPNError())
	}
	if err := n.ConnectVPN("11111111-1111-4111-8111-111111111111"); err == nil || !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("connect error = %v", err)
	}
	if err := n.ConnectVPN("not-a-uuid"); err == nil {
		t.Fatal("invalid profile identity was accepted")
	}
	if len(f.vpnConnects) != 1 {
		t.Fatalf("invalid identity reached backend: %v", f.vpnConnects)
	}
}

func TestNetworkVPNBackendUnavailableIsExplicit(t *testing.T) {
	n := NewNetwork(unavailableBackend{})
	if err := n.RefreshVPNProfiles(); !errors.Is(err, errNoNetworkManager) {
		t.Fatalf("absent backend error = %v, want unavailable", err)
	}
	if err := n.ConnectVPN("11111111-1111-4111-8111-111111111111"); !errors.Is(err, errNoNetworkManager) {
		t.Fatalf("absent backend connect error = %v", err)
	}
}

func TestNetworkSerializesDuplicateVPNControls(t *testing.T) {
	profile := VPNProfile{UUID: "11111111-1111-4111-8111-111111111111", Name: "Office", Status: VPNDisconnected}
	f := &fakeBackend{vpns: []VPNProfile{profile}}
	n := NewNetwork(f)
	if err := n.RefreshVPNProfiles(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := n.ConnectVPN(profile.UUID); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := n.DisconnectVPN(profile.UUID); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.vpnConnects) != 1 || len(f.vpnDisconnects) != 1 {
		t.Fatalf("duplicate controls reached the backend: connect=%v disconnect=%v", f.vpnConnects, f.vpnDisconnects)
	}
}

type blockingVPNBackend struct {
	*fakeBackend
	connectStarted    chan struct{}
	releaseConnect    <-chan struct{}
	disconnectStarted chan struct{}
}

func (b *blockingVPNBackend) ConnectVPN(string) error {
	close(b.connectStarted)
	<-b.releaseConnect
	return nil
}

func (b *blockingVPNBackend) DisconnectVPN(string) error {
	close(b.disconnectStarted)
	return nil
}

func TestNetworkDisconnectVPNCanCancelPendingConnect(t *testing.T) {
	profile := VPNProfile{UUID: "11111111-1111-4111-8111-111111111111", Name: "Office", Status: VPNDisconnected}
	releaseConnect := make(chan struct{})
	var releaseOnce sync.Once
	cleanup := func() { releaseOnce.Do(func() { close(releaseConnect) }) }
	t.Cleanup(cleanup)
	backend := &blockingVPNBackend{
		fakeBackend:       &fakeBackend{vpns: []VPNProfile{profile}},
		connectStarted:    make(chan struct{}),
		releaseConnect:    releaseConnect,
		disconnectStarted: make(chan struct{}),
	}
	network := NewNetwork(backend)
	if err := network.RefreshVPNProfiles(); err != nil {
		t.Fatal(err)
	}

	connectDone := make(chan error, 1)
	go func() { connectDone <- network.ConnectVPN(profile.UUID) }()
	select {
	case <-backend.connectStarted:
	case <-time.After(time.Second):
		t.Fatal("VPN connect never started")
	}

	backend.fakeBackend.vpns[0].Status = VPNConnecting
	if err := network.RefreshVPNProfiles(); err != nil {
		t.Fatal(err)
	}
	disconnectDone := make(chan error, 1)
	go func() { disconnectDone <- network.DisconnectVPN(profile.UUID) }()
	select {
	case err := <-disconnectDone:
		if err != nil {
			t.Fatalf("cancel pending VPN connect: %v", err)
		}
	case <-time.After(time.Second):
		cleanup()
		t.Fatal("VPN disconnect waited for the pending connect instead of cancelling it")
	}
	select {
	case <-backend.disconnectStarted:
	default:
		t.Fatal("VPN disconnect did not reach the backend")
	}

	cleanup()
	select {
	case err := <-connectDone:
		if err != nil {
			t.Fatalf("finish VPN connect: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("VPN connect did not finish after release")
	}
}

type orderedVPNRefreshBackend struct {
	*fakeBackend
	mu           sync.Mutex
	calls        int
	started      chan int
	releaseFirst <-chan struct{}
}

func (b *orderedVPNRefreshBackend) VPNProfiles() ([]VPNProfile, error) {
	b.mu.Lock()
	b.calls++
	call := b.calls
	b.mu.Unlock()
	b.started <- call
	if call == 1 {
		<-b.releaseFirst
		return []VPNProfile{{UUID: "11111111-1111-4111-8111-111111111111", Name: "Old", Status: VPNDisconnected}}, nil
	}
	return []VPNProfile{{UUID: "22222222-2222-4222-8222-222222222222", Name: "New", Status: VPNDisconnected}}, nil
}

func TestNetworkSerializesVPNProfileRefreshes(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFirst)
	backend := &orderedVPNRefreshBackend{
		fakeBackend:  &fakeBackend{},
		started:      make(chan int, 2),
		releaseFirst: release,
	}
	n := NewNetwork(backend)

	firstDone := make(chan error, 1)
	go func() { firstDone <- n.RefreshVPNProfiles() }()
	if call := <-backend.started; call != 1 {
		t.Fatalf("first backend refresh call = %d, want 1", call)
	}

	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondDone <- n.RefreshVPNProfiles()
	}()
	<-secondStarted
	select {
	case call := <-backend.started:
		t.Fatalf("backend refresh %d ran before the first refresh completed", call)
	case <-time.After(50 * time.Millisecond):
	}

	releaseFirst()
	for name, done := range map[string]<-chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s refresh: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s refresh did not finish", name)
		}
	}
	want := []VPNProfile{{UUID: "22222222-2222-4222-8222-222222222222", Name: "New", Status: VPNDisconnected}}
	if got := n.CachedVPNProfiles(); !slices.Equal(got, want) {
		t.Fatalf("final VPN profiles = %+v, want newest snapshot %+v", got, want)
	}
}

func TestChangesPublishesOnBackendWake(t *testing.T) {
	f := &fakeBackend{state: NetworkState{WirelessEnabled: true}}
	n := NewNetwork(f)
	lease, err := n.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	f.state.SSID = "LukeAP"
	f.wakeCh <- struct{}{}
	select {
	case st := <-n.Changes():
		if st.SSID != "LukeAP" {
			t.Fatalf("SSID = %q, want LukeAP", st.SSID)
		}
	case <-time.After(time.Second):
		t.Fatal("no change published")
	}
}

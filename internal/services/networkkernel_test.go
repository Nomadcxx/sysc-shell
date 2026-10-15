package services

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// kernelFixture lays out the files the kernel tier reads, rooted at "/".
func kernelFixture(ifaces map[string]string, wireless []string, route, procWireless string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, oper := range ifaces {
		fsys["sys/class/net/"+name+"/operstate"] = &fstest.MapFile{Data: []byte(oper + "\n")}
		fsys["sys/class/net/"+name+"/address"] = &fstest.MapFile{Data: []byte("00:11:22:33:44:55\n")}
	}
	for _, name := range wireless {
		fsys["sys/class/net/"+name+"/wireless/.present"] = &fstest.MapFile{}
	}
	if route != "" {
		fsys["proc/net/route"] = &fstest.MapFile{Data: []byte(route)}
	}
	if procWireless != "" {
		fsys["proc/net/wireless"] = &fstest.MapFile{Data: []byte(procWireless)}
	}
	return fsys
}

const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func defaultRoute(iface string) string {
	return routeHeader + iface + "\t00000000\t0100A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n"
}

const wirelessHeader = "Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE\n" +
	" face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22\n"

func TestKernelStateReadsWhatTheKernelKnows(t *testing.T) {
	addr := func(string) string { return "192.168.1.20" }
	for _, tc := range []struct {
		name string
		fsys fstest.MapFS
		want NetworkState
	}{
		{"wired, routed", kernelFixture(map[string]string{"lo": "unknown", "enp5s0": "up"}, nil, defaultRoute("enp5s0"), ""),
			NetworkState{Kind: ConnWired, Connected: true, Interface: "enp5s0", IPv4: "192.168.1.20"}},
		{"wireless, routed, with a link row", kernelFixture(map[string]string{"wlan0": "up"}, []string{"wlan0"}, defaultRoute("wlan0"),
			wirelessHeader+"wlan0: 0000   56.  -54.  -256        0      0      0      0     12        0\n"),
			NetworkState{Kind: ConnWireless, Connected: true, Interface: "wlan0", IPv4: "192.168.1.20", WirelessEnabled: true, Strength: 80}},
		{"wireless down", kernelFixture(map[string]string{"wlan0": "down"}, []string{"wlan0"}, routeHeader, wirelessHeader),
			NetworkState{Kind: ConnNone, Interface: "wlan0", WirelessEnabled: true}},
		{"wired only, nothing routed", kernelFixture(map[string]string{"eth0": "down"}, nil, routeHeader, ""),
			NetworkState{Kind: ConnNone, Interface: "eth0"}},
		{"a malformed link row is skipped", kernelFixture(map[string]string{"wlan0": "up"}, []string{"wlan0"}, defaultRoute("wlan0"),
			wirelessHeader+"wlan0: garbage\n"),
			NetworkState{Kind: ConnWireless, Connected: true, Interface: "wlan0", IPv4: "192.168.1.20", WirelessEnabled: true}},
	} {
		got, err := readKernelState(tc.fsys, addr)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		tc.want.Tier, tc.want.ReadOnly = TierKernel, true
		if got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
		if got.SSID != "" {
			t.Errorf("%s: the kernel tier invented an SSID %q", tc.name, got.SSID)
		}
	}
	if _, err := readKernelState(fstest.MapFS{}, addr); err == nil {
		t.Fatal("a machine with no /sys/class/net read as a state")
	}
}

func TestTheKernelTierRefusesEveryWriteNamingItself(t *testing.T) {
	be := newKernelBackend(kernelFixture(map[string]string{"eth0": "up"}, nil, defaultRoute("eth0"), ""), func(string) string { return "" })
	for name, err := range map[string]error{
		"scan":     be.Scan(),
		"radio":    be.SetWirelessEnabled(true),
		"activate": be.Activate(AccessPoint{SSID: "x"}),
		"forget":   be.Forget("x"),
	} {
		if !errors.Is(err, errKernelReadOnly) || !strings.Contains(err.Error(), "NetworkManager is not running") {
			t.Errorf("%s: %v, want the read-only tier named", name, err)
		}
	}
	if _, err := be.AccessPoints(); !errors.Is(err, errKernelReadOnly) {
		t.Errorf("access points: %v; the kernel tier lists none rather than an empty success", err)
	}
}

func TestEveryTierSaysWhetherItCanWrite(t *testing.T) {
	st, _ := unavailableBackend{}.State()
	if st.Tier != TierNone || !st.ReadOnly {
		t.Fatalf("unavailable backend state = %+v, want TierNone and read-only", st)
	}
	if err := (unavailableBackend{}).Scan(); !errors.Is(err, errNoNetworkManager) {
		t.Fatalf("unavailable scan = %v", err)
	}
}

func TestSelectionPrefersNetworkManagerThenTheKernel(t *testing.T) {
	nm := &fakeBackend{state: NetworkState{Tier: TierNM}}
	kernel := kernelFixture(map[string]string{"eth0": "up"}, nil, defaultRoute("eth0"), "")
	for _, tc := range []struct {
		name   string
		nmErr  error
		kernel fstest.MapFS
		want   Tier
		ok     bool
	}{
		{"manager answers", nil, kernel, TierNM, true},
		{"no manager", errors.New("no bus"), kernel, TierKernel, true},
		{"neither", errors.New("no bus"), fstest.MapFS{}, TierNone, false},
	} {
		be, tier := selectNetworkBackend(func() (backend, error) { return nm, tc.nmErr }, tc.kernel, func(string) string { return "" })
		if tier != tc.want {
			t.Errorf("%s: tier = %v, want %v", tc.name, tier, tc.want)
		}
		if (tier != TierNone) != tc.ok || be == nil {
			t.Errorf("%s: backend %T", tc.name, be)
		}
	}
}

func TestAPollingTierIsReadOnItsInterval(t *testing.T) {
	be := &pollingFake{fakeBackend: fakeBackend{state: NetworkState{Interface: "eth0"}}, every: 10 * time.Millisecond}
	n := NewNetwork(be)
	lease, err := n.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	select {
	case st := <-n.Changes():
		if st.Interface != "eth0" {
			t.Fatalf("state = %+v", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a tier with no events was never read")
	}
}

func TestAPushedTierIgnoresThePollInterval(t *testing.T) {
	f := &fakeBackend{state: NetworkState{Interface: "eth0"}}
	n := NewNetwork(f)
	lease, err := n.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	select {
	case st := <-n.Changes():
		t.Fatalf("a pushed tier published %+v without an event", st)
	case <-time.After(100 * time.Millisecond):
	}
}

// pollingFake is a backend with nothing to push.
type pollingFake struct {
	fakeBackend
	every time.Duration
}

func (p *pollingFake) PollInterval() time.Duration { return p.every }

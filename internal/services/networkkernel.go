package services

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The kernel tier answers when no network manager does. It reads what the
// kernel publishes to every user, through a filesystem rooted at "/" so tests
// use fixtures rather than this machine's interfaces:
//
//   - sys/class/net/<iface>/operstate, and a wireless entry for a radio;
//   - proc/net/route, for the interface the default route leaves through,
//     which is the kernel's only word for "the active connection";
//   - proc/net/wireless, for that radio's link quality when it has a row.
//
// It cannot name the network: the kernel publishes no SSID without a
// privileged request, so the tier reports the interface and whether it
// carries traffic, and it changes nothing.
type kernelBackend struct {
	fsys  fs.FS
	ipv4  func(iface string) string
	every time.Duration
}

// kernelPollInterval is how often a leased kernel tier is read. It has no
// events to wait for, and a few small files a tick is cheap.
const kernelPollInterval = 5 * time.Second

// wirelessQualityMax is the link-quality scale nearly every driver reports
// on; /proc/net/wireless does not state it.
const wirelessQualityMax = 70

var errKernelReadOnly = errors.New("services: NetworkManager is not running; the kernel's view of the network is read-only")

func newKernelBackend(fsys fs.FS, ipv4 func(string) string) *kernelBackend {
	return &kernelBackend{fsys: fsys, ipv4: ipv4, every: kernelPollInterval}
}

func (k *kernelBackend) State() (NetworkState, error)         { return readKernelState(k.fsys, k.ipv4) }
func (k *kernelBackend) AccessPoints() ([]AccessPoint, error) { return nil, errKernelReadOnly }
func (k *kernelBackend) Scan() error                          { return errKernelReadOnly }
func (k *kernelBackend) SetWirelessEnabled(bool) error        { return errKernelReadOnly }
func (k *kernelBackend) Activate(AccessPoint) error           { return errKernelReadOnly }
func (k *kernelBackend) Forget(string) error                  { return errKernelReadOnly }
func (k *kernelBackend) Close() error                         { return nil }

// Watch has nothing to subscribe to; PollInterval tells the service to read
// on a timer instead.
func (k *kernelBackend) Watch(chan<- struct{}, <-chan struct{}) error { return nil }
func (k *kernelBackend) PollInterval() time.Duration                  { return k.every }

// readKernelState is the kernel's answer to what the bar and panel need.
func readKernelState(fsys fs.FS, ipv4 func(string) string) (NetworkState, error) {
	st := NetworkState{Kind: ConnNone, Tier: TierKernel, ReadOnly: true}
	entries, err := fs.ReadDir(fsys, "sys/class/net")
	if err != nil {
		return NetworkState{}, fmt.Errorf("services: kernel interfaces: %w", err)
	}
	var ifaces, radios []string
	for _, e := range entries {
		if e.Name() == "lo" {
			continue
		}
		ifaces = append(ifaces, e.Name())
		if _, err := fs.Stat(fsys, "sys/class/net/"+e.Name()+"/wireless"); err == nil {
			radios = append(radios, e.Name())
		}
	}
	slices.Sort(ifaces)
	st.WirelessEnabled = len(radios) > 0

	routed := defaultRouteInterface(fsys)
	switch {
	case routed != "" && slices.Contains(ifaces, routed):
		st.Interface = routed
	case len(radios) > 0:
		st.Interface = radios[0]
	case len(ifaces) > 0:
		st.Interface = ifaces[0]
	}
	if st.Interface == "" || routed != st.Interface || operstate(fsys, st.Interface) != "up" {
		return st, nil
	}
	st.Connected = true
	st.Kind = ConnWired
	if slices.Contains(radios, st.Interface) {
		st.Kind = ConnWireless
		st.Strength = linkStrength(fsys, st.Interface)
	}
	if ipv4 != nil {
		st.IPv4 = ipv4(st.Interface)
	}
	return st, nil
}

func operstate(fsys fs.FS, iface string) string {
	data, err := fs.ReadFile(fsys, "sys/class/net/"+iface+"/operstate")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// defaultRouteInterface is the interface of the first IPv4 default route.
func defaultRouteInterface(fsys fs.FS) string {
	data, err := fs.ReadFile(fsys, "proc/net/route")
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Scan() // the header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 8 && fields[1] == "00000000" && fields[7] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

// linkStrength is a radio's link quality as 0..100, or 0 when the kernel has
// no usable row for it.
func linkStrength(fsys fs.FS, iface string) uint8 {
	data, err := fs.ReadFile(fsys, "proc/net/wireless")
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok || strings.TrimSpace(name) != iface {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			return 0
		}
		q, err := strconv.ParseFloat(strings.TrimSuffix(fields[1], "."), 64)
		if err != nil || q < 0 {
			return 0
		}
		return uint8(min(100, q*100/wirelessQualityMax))
	}
	return 0
}

// interfaceIPv4 is the first IPv4 address on iface, from the standard library.
func interfaceIPv4(iface string) string {
	ifc, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	return ""
}

// selectNetworkBackend tries NetworkManager, then the kernel. Selection
// happens once; the reload path is the only re-probe.
func selectNetworkBackend(nm func() (backend, error), kernel fs.FS, ipv4 func(string) string) (backend, Tier) {
	if be, err := nm(); err == nil {
		return be, TierNM
	}
	k := newKernelBackend(kernel, ipv4)
	if _, err := k.State(); err == nil {
		return k, TierKernel
	}
	return unavailableBackend{}, TierNone
}

// rootFS is the real filesystem the kernel tier reads.
func rootFS() fs.FS { return os.DirFS("/") }

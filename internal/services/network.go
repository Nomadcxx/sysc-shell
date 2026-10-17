package services

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Connectivity names what the active connection runs over.
type Connectivity uint8

const (
	ConnUnknown Connectivity = iota
	ConnNone
	ConnWired
	ConnWireless
)

// NetworkState is the whole of what the bar widget and the panel header need.
//
// An absent figure is not a zero: IPv4 empty means "no address", not "0.0.0.0",
// and the panel renders a dash for it rather than a number.
type NetworkState struct {
	Kind            Connectivity
	Connected       bool
	Resolving       bool // activating, not yet connected
	WirelessEnabled bool
	Scanning        bool
	SSID            string
	IPv4            string
	Interface       string
	Strength        uint8 // 0..100, Wi-Fi only
	// Tier names which backend answered. ConnNone with TierKernel means "no
	// route and no manager", not "the radio is off".
	Tier Tier
	// ReadOnly is true when the answering tier cannot change anything.
	ReadOnly bool
}

// Tier is the backend that answered for the network.
type Tier uint8

const (
	// TierNone is no backend at all: nothing is known.
	TierNone Tier = iota
	// TierNM is NetworkManager.
	TierNM
	// TierIWD is iwd, with wired links read from the kernel.
	TierIWD
	// TierKernel is the kernel's read-only view, used when no manager runs.
	TierKernel
)

// AccessPoint is one scanned network.
//
// Saved is ours rather than NetworkManager's own vocabulary: the panel must
// know whether tapping a row connects or opens a password prompt before it
// calls anything, and the reference shell answers that with a separate
// per-row lookup.
type AccessPoint struct {
	Path       string
	DevicePath string
	SSID       string
	Strength   uint8
	Secured    bool
	Active     bool
	Saved      bool
}

// VPNStatus is the state of one installed NetworkManager VPN profile.
type VPNStatus uint8

const (
	VPNDisconnected VPNStatus = iota + 1
	VPNConnecting
	VPNConnected
	VPNDisconnecting
)

// VPNProfile contains only identity and runtime state. Stored credentials
// remain in NetworkManager and never enter the shell snapshot.
type VPNProfile struct {
	UUID   string
	Name   string
	Status VPNStatus
}

// ManualSecurity is the supported security choice for a network entered by
// SSID. Enterprise authentication has a separate identity flow.
type ManualSecurity uint8

const (
	ManualSecurityOpen ManualSecurity = iota + 1
	ManualSecurityWPAPersonal
	ManualSecurityWPAEnterprise
)

// ManualNetworkRequest deliberately carries no passphrase. The backend asks
// the existing secret agent for WPA-Personal credentials when it needs them.
type ManualNetworkRequest struct {
	SSID     string
	Security ManualSecurity
}

func (r ManualNetworkRequest) Validate() error {
	if len(r.SSID) == 0 || len(r.SSID) > 32 || !utf8.ValidString(r.SSID) || strings.IndexByte(r.SSID, 0) >= 0 {
		return errors.New("services: SSID must be between 1 and 32 bytes")
	}
	switch r.Security {
	case ManualSecurityOpen, ManualSecurityWPAPersonal:
		return nil
	default:
		return errors.New("services: unsupported wireless security")
	}
}

// SignalBand buckets 0..100 into the five bands the glyphs draw.
//
// Sorting and glyph choice each key on the band rather than the raw percent,
// which jitters on every scan and would reorder the list under the user's
// finger. The thresholds match the reference shell's.
func SignalBand(signal uint8) int {
	switch {
	case signal >= 80:
		return 4
	case signal >= 60:
		return 3
	case signal >= 35:
		return 2
	case signal >= 15:
		return 1
	default:
		return 0
	}
}

// backend is the seam between the service and whatever answers for the
// network: the pinned NetworkManager binding, iwd (networkiwd.go), or the
// kernel's read-only view when no manager runs (networkkernel.go). Tests
// replace it with a fake rather than a live system bus. A further writable
// backend is an owner decision, not a slot this interface keeps open.
type backend interface {
	State() (NetworkState, error)
	AccessPoints() ([]AccessPoint, error)
	Scan() error
	SetWirelessEnabled(bool) error
	Activate(ap AccessPoint) error
	ConnectManual(ManualNetworkRequest) error
	Forget(ssid string) error
	VPNProfiles() ([]VPNProfile, error)
	ConnectVPN(uuid string) error
	DisconnectVPN(uuid string) error
	// Watch reports readiness to re-read by sending on wake, until stop closes.
	// It never sends state itself: one decode path, in the service.
	Watch(wake chan<- struct{}, stop <-chan struct{}) error
	Close() error
}

// poller is a backend with nothing to push. The service reads it on its
// interval while leased instead of waiting for a wake.
type poller interface {
	PollInterval() time.Duration
}

// Network is a peer of Audio: the same public shape, a different delivery.
//
// Audio polls on a lease interval. NetworkManager pushes D-Bus signals, so
// leaseSet's interval is meaningless here and deliberately ignored: a lease
// means only "somebody is watching", and it governs when the subscription
// starts and stops. A backend with nothing to push names its own cadence
// through poller; that is a property of the tier, not of any lease.
type Network struct {
	mu           sync.Mutex
	vpnControlMu sync.Mutex
	vpnRefreshMu sync.Mutex
	leases       leaseSet
	be           backend
	last         NetworkState
	aps          []AccessPoint
	vpns         []VPNProfile
	vpnErr       error
	ok           bool
	stop         chan struct{}
	wake         chan struct{}
	changes      chan NetworkState

	// secrets holds the one in-flight passphrase prompt. It is always
	// non-nil: the panel calls CancelSecret on close whether or not a prompt
	// was ever opened.
	secrets       *secretSlot
	secretReqs    chan SecretRequest
	secretExpired chan struct{}
	// closeSecrets unregisters and unexports the process-wide credential
	// holder. Close clears it before calling it so repeated shutdown is safe.
	closeSecrets func()
}

func NewNetwork(b backend) *Network {
	return &Network{
		be:            b,
		ok:            b != nil,
		changes:       make(chan NetworkState, 1),
		wake:          make(chan struct{}, 1),
		secrets:       newSecretSlot(),
		secretReqs:    make(chan SecretRequest, 1),
		secretExpired: make(chan struct{}, 1),
	}
}

// Changes carries the newest state. The channel is created once and never
// closed, so it survives stop and start cycles.
func (n *Network) Changes() <-chan NetworkState { return n.changes }

func (n *Network) Available() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ok
}

// CachedState returns the last observed state without touching the bus.
// Callers holding Registry.mu or the Wayland owner must use this.
func (n *Network) CachedState() NetworkState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.last
}

// CachedAccessPoints returns the last scan without touching the bus.
//
// The panel tree is built under Registry.mu and on the Wayland owner, where
// I/O is forbidden, so the tree reads this and never AccessPoints().
func (n *Network) CachedAccessPoints() []AccessPoint {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.aps)
}

// CachedVPNProfiles returns the last VPN profile snapshot without bus I/O.
// Panel code calls this under Registry.mu and on the Wayland owner.
func (n *Network) CachedVPNProfiles() []VPNProfile {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.vpns)
}

// CachedVPNError returns a listing error from the last refresh, if any.
func (n *Network) CachedVPNError() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.vpnErr == nil {
		return ""
	}
	return n.vpnErr.Error()
}

// RefreshVPNProfiles reads profile metadata off the owner loop and replaces
// the cached snapshot. A failed read keeps the last useful list and records
// the error for the panel.
func (n *Network) RefreshVPNProfiles() error {
	n.vpnRefreshMu.Lock()
	defer n.vpnRefreshMu.Unlock()
	profiles, err := n.be.VPNProfiles()
	n.mu.Lock()
	defer n.mu.Unlock()
	if err != nil {
		n.vpnErr = err
		return err
	}
	n.vpns = sortVPNProfiles(profiles)
	n.vpnErr = nil
	return nil
}

func sortVPNProfiles(profiles []VPNProfile) []VPNProfile {
	out := slices.Clone(profiles)
	slices.SortFunc(out, func(a, b VPNProfile) int {
		if d := strings.Compare(a.Name, b.Name); d != 0 {
			return d
		}
		return strings.Compare(a.UUID, b.UUID)
	})
	return out
}

func validVPNUUID(uuid string) bool {
	if len(uuid) != 36 {
		return false
	}
	for i := range uuid {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if uuid[i] != '-' {
				return false
			}
			continue
		}
		c := uuid[i]
		if !('0' <= c && c <= '9') && !('a' <= c && c <= 'f') && !('A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// State reads through to the backend, falling back to the cache on error. It
// performs I/O and must not be called under a lock.
func (n *Network) State() NetworkState {
	st, err := n.be.State()
	if err != nil {
		return n.CachedState()
	}
	n.mu.Lock()
	n.last = st
	n.mu.Unlock()
	return st
}

// AccessPoints returns the scan sorted by band, then by SSID.
//
// Sorting here rather than in the panel keeps one ordering for every consumer,
// and keeps the jitter argument in one place.
func (n *Network) AccessPoints() []AccessPoint {
	aps, err := n.be.AccessPoints()
	if err != nil {
		n.mu.Lock()
		defer n.mu.Unlock()
		return slices.Clone(n.aps)
	}
	slices.SortFunc(aps, func(a, b AccessPoint) int {
		if d := SignalBand(b.Strength) - SignalBand(a.Strength); d != 0 {
			return d
		}
		return strings.Compare(a.SSID, b.SSID)
	})
	n.mu.Lock()
	n.aps = aps
	n.mu.Unlock()
	return aps
}

// Acquire registers a consumer. The first lease subscribes; the last release
// unsubscribes. The lease carries no interval.
func (n *Network) Acquire() (*Lease, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	lease := &Lease{network: n}
	n.leases.add(lease)
	if n.stop == nil {
		stop := make(chan struct{})
		if err := n.be.Watch(n.wake, stop); err != nil {
			n.leases.remove(lease)
			return nil, err
		}
		n.stop = stop
		var every time.Duration
		if p, ok := n.be.(poller); ok {
			every = p.PollInterval()
		}
		go n.run(stop, every)
	}
	return lease, nil
}

// run coalesces a burst of signals into a cap-one channel, so a noisy scan
// cannot outrun one paint.
//
// A tier with nothing to push is read on its interval as well: its ticks
// land on the same wake path, so there is one reader and one publish path.
func (n *Network) run(stop <-chan struct{}, every time.Duration) {
	var tick <-chan time.Time
	if every > 0 {
		t := time.NewTicker(every)
		defer t.Stop()
		tick = t.C
		select {
		case n.wake <- struct{}{}:
		default:
		}
	}
	for {
		select {
		case <-stop:
			return
		case <-tick:
			select {
			case n.wake <- struct{}{}:
			default:
			}
		case <-n.wake:
			st, err := n.be.State()
			if err != nil {
				continue
			}
			n.mu.Lock()
			n.last = st
			n.mu.Unlock()
			select {
			case n.changes <- st:
			default:
			}
		}
	}
}

func (n *Network) release(l *Lease) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.leases.remove(l) {
		return
	}
	n.stopIfUnusedLocked()
}

// stopIfUnusedLocked ends the subscription when no lease remains. The caller
// holds n.mu.
func (n *Network) stopIfUnusedLocked() {
	if len(n.leases.leases) > 0 || n.stop == nil {
		return
	}
	close(n.stop)
	n.stop = nil
}

// Close drops every lease and ends the subscription.
func (n *Network) Close() {
	n.CancelSecret()
	n.mu.Lock()
	for _, l := range n.leases.clear() {
		l.forget()
	}
	n.stopIfUnusedLocked()
	be := n.be
	closeSecrets := n.closeSecrets
	n.closeSecrets = nil
	n.mu.Unlock()
	if closeSecrets != nil {
		closeSecrets()
	}
	if be != nil {
		_ = be.Close()
	}
}

// Writes. Each performs I/O and must run off Registry.mu and off the Wayland
// owner, through the shell's scheduleControl seam.

// NewSystemNetwork builds the service over NetworkManager, or, on a machine
// where no manager answers, over the kernel's read-only view.
//
// A machine where neither answers yields a service that reports Available
// false and does nothing, rather than an error the caller must handle: the
// bar still paints and the glyph stays off. The backend is never nil, because
// every method here dereferences it and a nil one would panic inside a widget
// refresh, which presents as a shell that paints nothing.
func NewSystemNetwork() *Network {
	be, tier := selectNetworkBackend(
		func() (backend, error) { return newNMBackend() },
		func() (backend, error) { return newIWDBackend() },
		rootFS(), interfaceIPv4)
	switch tier {
	case TierNone:
		n := NewNetwork(be)
		n.ok = false
		return n
	case TierKernel:
		// Nothing to authenticate against: the kernel tier changes nothing,
		// so it needs no secret export.
		return NewNetwork(be)
	}
	n := NewNetwork(be)
	start := startSystemSecretExport
	if iwd, ok := be.(*iwdBackend); ok {
		start = func(e *secretExport) (func(), error) { return iwd.bus.ExportAgent(&iwdAgent{export: e, bus: iwd.bus}) }
	}
	if err := n.startSecrets(start); err != nil {
		// A partial service would let secured activation wait for a prompt that
		// can never arrive. Fail closed until the export can be registered.
		n.ok = false
	}
	return n
}

// unavailableBackend stands in when NetworkManager is not reachable. It is
// inert: no state, no access points, and every write refuses.
type unavailableBackend struct{}

func (unavailableBackend) State() (NetworkState, error) {
	return NetworkState{Kind: ConnUnknown, Tier: TierNone, ReadOnly: true}, nil
}
func (unavailableBackend) AccessPoints() ([]AccessPoint, error)         { return nil, nil }
func (unavailableBackend) Scan() error                                  { return errNoNetworkManager }
func (unavailableBackend) SetWirelessEnabled(bool) error                { return errNoNetworkManager }
func (unavailableBackend) Activate(AccessPoint) error                   { return errNoNetworkManager }
func (unavailableBackend) ConnectManual(ManualNetworkRequest) error     { return errNoNetworkManager }
func (unavailableBackend) Forget(string) error                          { return errNoNetworkManager }
func (unavailableBackend) VPNProfiles() ([]VPNProfile, error)           { return nil, errNoNetworkManager }
func (unavailableBackend) ConnectVPN(string) error                      { return errNoNetworkManager }
func (unavailableBackend) DisconnectVPN(string) error                   { return errNoNetworkManager }
func (unavailableBackend) Watch(chan<- struct{}, <-chan struct{}) error { return nil }
func (unavailableBackend) Close() error                                 { return nil }

var errNoNetworkManager = errors.New("services: NetworkManager is not available")
var errVPNUnsupported = errors.New("services: VPN controls require NetworkManager")

func (n *Network) Scan() error                      { return n.be.Scan() }
func (n *Network) SetWirelessEnabled(on bool) error { return n.be.SetWirelessEnabled(on) }
func (n *Network) Activate(ap AccessPoint) error    { return n.be.Activate(ap) }
func (n *Network) ConnectManual(req ManualNetworkRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return n.be.ConnectManual(req)
}
func (n *Network) ConnectVPN(uuid string) error {
	if !validVPNUUID(uuid) {
		return errors.New("services: invalid VPN profile identity")
	}
	n.vpnControlMu.Lock()
	defer n.vpnControlMu.Unlock()
	if status, ok := n.cachedVPNStatus(uuid); ok && (status == VPNConnecting || status == VPNConnected) {
		return nil
	}
	if err := n.be.ConnectVPN(uuid); err != nil {
		return err
	}
	_ = n.RefreshVPNProfiles()
	return nil
}
func (n *Network) DisconnectVPN(uuid string) error {
	if !validVPNUUID(uuid) {
		return errors.New("services: invalid VPN profile identity")
	}
	n.vpnControlMu.Lock()
	defer n.vpnControlMu.Unlock()
	if status, ok := n.cachedVPNStatus(uuid); ok && (status == VPNDisconnected || status == VPNDisconnecting) {
		return nil
	}
	if err := n.be.DisconnectVPN(uuid); err != nil {
		return err
	}
	_ = n.RefreshVPNProfiles()
	return nil
}

func (n *Network) cachedVPNStatus(uuid string) (VPNStatus, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, profile := range n.vpns {
		if profile.UUID == uuid {
			return profile.Status, true
		}
	}
	return 0, false
}
func (n *Network) Forget(ssid string) error { return n.be.Forget(ssid) }

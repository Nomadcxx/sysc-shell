package services

import (
	"errors"
	"slices"
	"strings"
	"sync"
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
}

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

// backend is the seam tests replace. Its only production implementation wraps
// the pinned NetworkManager binding.
//
// It exists so a test uses a fake instead of a live system bus, not to support
// swappable wpa_supplicant or iwd backends the way the reference shells do:
// we are building neither. If that justification ever stops being true, delete
// this interface rather than populate it.
type backend interface {
	State() (NetworkState, error)
	AccessPoints() ([]AccessPoint, error)
	Scan() error
	SetWirelessEnabled(bool) error
	Activate(ap AccessPoint) error
	Forget(ssid string) error
	// Watch reports readiness to re-read by sending on wake, until stop closes.
	// It never sends state itself: one decode path, in the service.
	Watch(wake chan<- struct{}, stop <-chan struct{}) error
	Close() error
}

// Network is a peer of Audio: the same public shape, a different delivery.
//
// Audio polls on a lease interval. This service is pushed by D-Bus signals, so
// leaseSet's interval is meaningless here and deliberately ignored: a lease
// means only "somebody is watching", and it governs when the subscription
// starts and stops. Reusing an interval-shaped helper for a non-interval
// purpose would mislead the next reader if it were not said plainly.
type Network struct {
	mu      sync.Mutex
	leases  leaseSet
	be      backend
	last    NetworkState
	aps     []AccessPoint
	ok      bool
	stop    chan struct{}
	wake    chan struct{}
	changes chan NetworkState

	// secrets holds the one in-flight passphrase prompt. It is always
	// non-nil: the panel calls CancelSecret on close whether or not a prompt
	// was ever opened.
	secrets    *secretSlot
	secretReqs chan SecretRequest
	// closeSecrets unregisters and unexports the process-wide credential
	// holder. Close clears it before calling it so repeated shutdown is safe.
	closeSecrets func()
}

func NewNetwork(b backend) *Network {
	return &Network{
		be:         b,
		ok:         b != nil,
		changes:    make(chan NetworkState, 1),
		wake:       make(chan struct{}, 1),
		secrets:    newSecretSlot(),
		secretReqs: make(chan SecretRequest, 1),
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
		go n.run(stop)
	}
	return lease, nil
}

// run coalesces a burst of signals into a cap-one channel, so a noisy scan
// cannot outrun one paint.
func (n *Network) run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
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
		l.network = nil
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

// NewSystemNetwork builds the service over the real NetworkManager backend.
//
// A machine with no reachable NetworkManager yields a service that reports
// Available false and does nothing, rather than an error the caller must
// handle: the bar still paints and the glyph stays off. The backend is never
// nil, because every method here dereferences it and a nil one would panic
// inside a widget refresh, which presents as a shell that paints nothing.
func NewSystemNetwork() *Network {
	be, err := newNMBackend()
	if err != nil {
		n := NewNetwork(unavailableBackend{})
		n.ok = false
		return n
	}
	n := NewNetwork(be)
	if err := n.startSecrets(startSystemSecretExport); err != nil {
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
	return NetworkState{Kind: ConnUnknown}, nil
}
func (unavailableBackend) AccessPoints() ([]AccessPoint, error)         { return nil, nil }
func (unavailableBackend) Scan() error                                  { return errNoNetworkManager }
func (unavailableBackend) SetWirelessEnabled(bool) error                { return errNoNetworkManager }
func (unavailableBackend) Activate(AccessPoint) error                   { return errNoNetworkManager }
func (unavailableBackend) Forget(string) error                          { return errNoNetworkManager }
func (unavailableBackend) Watch(chan<- struct{}, <-chan struct{}) error { return nil }
func (unavailableBackend) Close() error                                 { return nil }

var errNoNetworkManager = errors.New("services: NetworkManager is not available")

func (n *Network) Scan() error                      { return n.be.Scan() }
func (n *Network) SetWirelessEnabled(on bool) error { return n.be.SetWirelessEnabled(on) }
func (n *Network) Activate(ap AccessPoint) error    { return n.be.Activate(ap) }
func (n *Network) Forget(ssid string) error         { return n.be.Forget(ssid) }

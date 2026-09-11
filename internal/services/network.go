package services

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
// Sorting and glyph choice both use the band rather than the raw percent,
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

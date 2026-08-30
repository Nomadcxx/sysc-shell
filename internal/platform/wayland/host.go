package wayland

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/fractionalscale"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/viewporter"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-wayland/client"
)

// hostState is the lifecycle position of one output host. Transitions are
// driven only by Wayland events and by configuration reload.
type hostState uint8

const (
	hostBound       hostState = iota // bound proxy, metadata incomplete
	hostReady                        // done + name seen
	hostIdle                         // ready, but configuration disables the bar
	hostCreating                     // role objects created, awaiting first configure
	hostConfiguring                  // configure seen, allocating the first generation
	hostMapped                       // a buffer has been attached
	hostClosed                       // layer surface closed; the retry budget may permit recreation
)

const (
	// closeRetryLimit bounds recreation after zwlr_layer_surface_v1.closed so a
	// persistent refusal cannot become a create/destroy livelock.
	closeRetryLimit = 3
	// closeRetryResetAfter is how long a host must stay mapped to earn a fresh
	// budget. A transient reset should self-heal without spending it forever.
	closeRetryResetAfter = 60 * time.Second
)

// HostCallbacks are the per-bar hooks. A concrete struct rather than an
// interface: there is one production implementation, and the caller owns it.
type HostCallbacks struct {
	// Configure reports the logical size from zwlr_layer_surface_v1.configure
	// and the scale as a numerator over 120; 120 means scale 1.0.
	Configure func(logicalWidth, logicalHeight, scale120 int) error
	// Render fills the physical buffer. Width and height are buffer pixels.
	Render func(pixels []byte, width, height, stride int) error
	// Handle consumes a pointer event and reports whether state changed.
	Handle func(Event) bool
	// OpaqueBackground is the resolved palette opacity for this surface.
	OpaqueBackground bool
}

// surfaceUnit owns one layer surface and its buffer lifecycle. The bar is
// the first unit; auxiliary panels are more.
type surfaceUnit struct {
	id string // "bar" or the AuxSpec id

	surface  *client.Surface
	layer    *layershell.ZwlrLayerSurfaceV1
	scale    *fractionalscale.WpFractionalScaleV1
	viewport *viewporter.WpViewport

	ss    *surfaceState
	sched *render.Scheduler

	current  *generation
	retiring []*generation
	genID    int

	frameCallback *client.Callback
	cleanup       cleanupStack

	app HostCallbacks
}

func newSurfaceUnit(id string) *surfaceUnit {
	return &surfaceUnit{
		id:    id,
		ss:    newSurfaceState(),
		sched: render.NewScheduler(),
	}
}

func (u *surfaceUnit) bufferSize() (int32, int32, error) { return u.ss.bufferSize() }

func (u *surfaceUnit) dropFrameCallback() error {
	if u.frameCallback != nil {
		err := u.frameCallback.Destroy()
		u.frameCallback = nil
		return err
	}
	return nil
}

// OutputHost owns everything scoped to one wl_output. Its identity is the
// registry global name; the connector string is an attribute a reconnect may
// reuse and is never used as identity.
type OutputHost struct {
	global     uint32
	proxy      *client.Output
	connector  string
	transform  int32
	modeWidth  int32
	modeHeight int32
	doneSeen   bool
	policy     config.Bar
	// opaqueBackground controls the compositor's opaque-region hint for this
	// host's accepted theme.
	opaqueBackground bool

	state hostState
	// alive gates every host-level transition. It clears before any proxy is
	// destroyed so events already queued in the dispatch stream are ignored.
	alive bool

	bar *surfaceUnit
	aux map[string]*surfaceUnit

	// Bounded recreation budget for zwlr_layer_surface_v1.closed.
	closeAttempts int
	mappedSince   time.Time
}

// newHost creates a host for a freshly bound wl_output.
func newHost(global uint32, proxy *client.Output) *OutputHost {
	return &OutputHost{
		global: global,
		proxy:  proxy,
		state:  hostBound,
		alive:  true,
		bar:    newSurfaceUnit("bar"),
		aux:    make(map[string]*surfaceUnit),
	}
}

// ready reports whether the host has the minimum metadata a bar needs: an
// atomic commit (done) and the connector name that selects configuration.
func (h *OutputHost) ready() bool { return h.doneSeen && h.connector != "" }

// surfaceHeight is the layer surface height and exclusive zone for this host.
func (h *OutputHost) surfaceHeight() int {
	return h.policy.Gap + (h.policy.Height - 2*h.policy.Gap)
}

// mayRecreate reports whether the host may rebuild its surface after a close.
func (h *OutputHost) mayRecreate(now time.Time) bool {
	if !h.mappedSince.IsZero() && now.Sub(h.mappedSince) >= closeRetryResetAfter {
		h.closeAttempts = 0
	}
	if h.closeAttempts >= closeRetryLimit {
		return false
	}
	return true
}

// recordCloseAttempt spends one unit of the recreation budget.
func (h *OutputHost) recordCloseAttempt() {
	h.closeAttempts++
	h.mappedSince = time.Time{}
}

func (h *OutputHost) units() []*surfaceUnit {
	out := make([]*surfaceUnit, 0, 1+len(h.aux))
	if h.bar != nil {
		out = append(out, h.bar)
	}
	for _, u := range h.aux {
		out = append(out, u)
	}
	return out
}

package shell

import (
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// Registry owns every bar, the services they consume, and the state they read.
//
// Bars are keyed by wl_registry global name. A connector is an attribute: two
// globals may briefly share one during a reconnect, and they must stay
// distinct instances with distinct service leases.
//
// Niri state is keyed by connector and held whether or not a host exists,
// because a Niri event may name an output whose wl_output has not been
// announced yet or has already been removed. A host is never created or
// destroyed from a Niri event.
type Registry struct {
	mu      sync.Mutex
	cfg     config.Config
	outputs map[string]outputState
	bars    map[uint32]*Bar
	leases  map[uint32][]*services.Lease
	now     time.Time

	clock *services.Clock

	// invalidations carries one entry per bar whose rendered text changed.
	// The Wayland owner receives from it; the registry owns it and never
	// closes it.
	invalidations chan wayland.Invalidation
	// closed unblocks a pending publish at shutdown.
	closed    chan struct{}
	closeOnce sync.Once
}

func NewRegistry(cfg config.Config) *Registry {
	return &Registry{
		cfg:           cfg,
		outputs:       make(map[string]outputState),
		bars:          make(map[uint32]*Bar),
		leases:        make(map[uint32][]*services.Lease),
		clock:         services.NewClock(),
		invalidations: make(chan wayland.Invalidation, 8),
		closed:        make(chan struct{}),
	}
}

// Clock is the shared clock service. The process pumps its updates into
// UpdateClock.
func (r *Registry) Clock() *services.Clock { return r.clock }

// Invalidations is the channel the Wayland owner receives from. The registry
// owns it and never closes it.
func (r *Registry) Invalidations() <-chan wayland.Invalidation { return r.invalidations }

// publish sends one invalidation per changed global.
//
// The send blocks rather than dropping. A dropped invalidation is a bar that
// never repaints, which is exactly the defect this tranche must not ship. The
// owner's bridge goroutine drains this channel continuously into an unbounded
// queue, so blocking is bounded; Close unblocks a pending send at shutdown.
//
// Callers must not hold r.mu: the send can block, and the owner must stay free
// to make progress.
func (r *Registry) publish(globals []uint32) {
	for _, global := range globals {
		select {
		case r.invalidations <- wayland.Invalidation{Global: global}:
		case <-r.closed:
			return
		}
	}
}

// NewHost builds the hooks for one output's bar and acquires its services.
func (r *Registry) NewHost(global uint32, connector string) (wayland.HostCallbacks, error) {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()

	bar, leases, callbacks, err := r.buildBar(cfg, connector)
	if err != nil {
		return wayland.HostCallbacks{}, err
	}

	r.mu.Lock()
	bar.apply(r.viewLocked(connector))
	r.bars[global] = bar
	r.leases[global] = leases
	r.mu.Unlock()

	return callbacks, nil
}

// PrepareConfig builds every enabled host's replacement bar and acquires its
// services before the caller changes live host policy.
//
// Acquiring here, and releasing the outgoing leases only in Commit, is what
// keeps a service in continuous use from stopping: its consumer count never
// reaches zero, so it is never restarted. A failure at any point releases
// exactly what this call acquired.
func (r *Registry) PrepareConfig(cfg config.Config, identities []wayland.HostIdentity) (wayland.PreparedConfig, error) {
	bars := make(map[uint32]*Bar, len(identities))
	leases := make(map[uint32][]*services.Lease, len(identities))
	callbacks := make(map[uint32]wayland.HostCallbacks, len(identities))

	for _, identity := range identities {
		bar, held, hooks, err := r.buildBar(cfg, identity.Connector)
		if err != nil {
			for _, acquired := range leases {
				releaseAll(acquired)
			}
			return wayland.PreparedConfig{}, err
		}
		bars[identity.Global] = bar
		leases[identity.Global] = held
		callbacks[identity.Global] = hooks
	}

	// once guards against Commit and Rollback each running, and against
	// either running twice.
	var once sync.Once

	return wayland.PreparedConfig{
		Hosts: callbacks,
		Commit: func() {
			once.Do(func() {
				r.mu.Lock()
				outgoing := r.leases
				for _, bar := range bars {
					bar.apply(r.viewLocked(bar.connector()))
				}
				r.cfg = cfg
				r.bars = bars
				r.leases = leases
				r.mu.Unlock()

				// Released only after the replacement set holds its own, so
				// the count never touches zero for a service still in use.
				for _, held := range outgoing {
					releaseAll(held)
				}
			})
		},
		Rollback: func() {
			once.Do(func() {
				for _, held := range leases {
					releaseAll(held)
				}
			})
		},
	}, nil
}

// DropHost releases a bar and its service leases after its surface is
// destroyed. Only the named global is affected, so a stale global sharing a
// connector with a reconnected one cannot remove it.
func (r *Registry) DropHost(global uint32) {
	r.mu.Lock()
	leases := r.leases[global]
	delete(r.bars, global)
	delete(r.leases, global)
	r.mu.Unlock()

	releaseAll(leases)
}

// Close releases every bar and service. It is safe to call twice.
func (r *Registry) Close() {
	// Unblocks any publish waiting on a full channel, so shutdown cannot hang.
	r.closeOnce.Do(func() { close(r.closed) })

	r.mu.Lock()
	var leases []*services.Lease
	for global, held := range r.leases {
		leases = append(leases, held...)
		delete(r.leases, global)
	}
	r.bars = make(map[uint32]*Bar)
	r.mu.Unlock()

	releaseAll(leases)
	r.clock.Close()
}

// UpdateClock applies a shared time snapshot to every bar and reports the
// globals whose text actually changed.
//
// One tick reaches every bar from one snapshot; a bar whose rendered text is
// unchanged is not reported, so no frame is submitted for it.
func (r *Registry) UpdateClock(now time.Time) []uint32 {
	r.mu.Lock()
	r.now = now
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()

	r.publish(changed)
	return changed
}

// UpdateNiri projects a snapshot into per-connector text and reports the
// globals whose text actually changed.
func (r *Registry) UpdateNiri(s niri.Snapshot) []uint32 {
	next := projectOutputs(s)

	r.mu.Lock()
	// Replaced wholesale, not merged: a connector absent from the projection
	// has no workspace state any more, and keeping its last value would render
	// a stale workspace or title on a host that reconnects under that name.
	r.outputs = next

	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()

	r.publish(changed)
	return changed
}

// viewLocked assembles one bar's immutable input: the process-wide clock
// snapshot plus this connector's Niri projection.
func (r *Registry) viewLocked(connector string) barView {
	state, ok := r.outputs[connector]
	if !ok {
		state = outputState{Workspace: noWorkspace}
	}
	return barView{Now: r.now, Workspace: state.Workspace, Title: state.Title}
}

// buildBar creates one bar and acquires the services its items need. A failure
// releases whatever was already acquired, so a rejected build leaks nothing.
func (r *Registry) buildBar(cfg config.Config, connector string) (
	*Bar, []*services.Lease, wayland.HostCallbacks, error,
) {
	policy := cfg.ForConnector(connector)
	bar, err := NewWithTheme(ThemeFrom(cfg, policy), policy, connector)
	if err != nil {
		return nil, nil, wayland.HostCallbacks{}, err
	}

	var leases []*services.Lease
	for _, boundary := range clockBoundaries(policy.Left, policy.Center, policy.Right) {
		lease, err := r.clock.Acquire(boundary)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}

	return bar, leases, wayland.HostCallbacks{
		Configure: bar.Configure,
		Render:    bar.Render,
		Handle:    bar.Handle,
	}, nil
}

func releaseAll(leases []*services.Lease) {
	for _, lease := range leases {
		lease.Release()
	}
}

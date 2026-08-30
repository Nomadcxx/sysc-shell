package shell

import (
	"strconv"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// Registry owns every bar and the Niri state they read.
//
// Workspace state is keyed by connector and held whether or not a host exists,
// because a Niri event may name an output whose wl_output has not been
// announced yet or has already been removed. A host is never created or
// destroyed from a Niri event.
type Registry struct {
	mu         sync.Mutex
	cfg        config.Config
	workspaces map[string]string
	bars       map[uint32]*Proof
	connectors map[uint32]string

	invalidationMu sync.Mutex
	invalidations  chan wayland.Invalidation
}

func NewRegistry(cfg config.Config) *Registry {
	return &Registry{
		cfg:           cfg,
		workspaces:    make(map[string]string),
		bars:          make(map[uint32]*Proof),
		connectors:    make(map[uint32]string),
		invalidations: make(chan wayland.Invalidation, 8),
	}
}

// Invalidations is the channel the Wayland owner receives from. The registry
// owns it and never closes it.
func (r *Registry) Invalidations() <-chan wayland.Invalidation { return r.invalidations }

// NewHost builds the hooks for one connector's bar.
func (r *Registry) NewHost(global uint32, connector string) (wayland.HostCallbacks, error) {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()

	bar, callbacks, err := buildHost(cfg, connector)
	if err != nil {
		return wayland.HostCallbacks{}, err
	}

	r.mu.Lock()
	if label, ok := r.workspaces[connector]; ok {
		bar.SetWorkspace(label)
	}
	r.bars[global] = bar
	r.connectors[global] = connector
	r.mu.Unlock()

	return callbacks, nil
}

// PrepareConfig builds every enabled connector's replacement bar before the
// caller changes live host policy. Commit publishes the complete set under one
// registry lock and applies the latest workspace labels at that point.
func (r *Registry) PrepareConfig(cfg config.Config, identities []wayland.HostIdentity) (wayland.PreparedConfig, error) {
	bars := make(map[uint32]*Proof, len(identities))
	connectors := make(map[uint32]string, len(identities))
	hosts := make(map[uint32]wayland.HostCallbacks, len(identities))
	for _, identity := range identities {
		bar, callbacks, err := buildHost(cfg, identity.Connector)
		if err != nil {
			return wayland.PreparedConfig{}, err
		}
		bars[identity.Global] = bar
		connectors[identity.Global] = identity.Connector
		hosts[identity.Global] = callbacks
	}

	return wayland.PreparedConfig{
		Hosts: hosts,
		Commit: func() {
			r.mu.Lock()
			for global, bar := range bars {
				connector := connectors[global]
				if label, ok := r.workspaces[connector]; ok {
					bar.SetWorkspace(label)
				}
			}
			r.cfg = cfg
			r.bars = bars
			r.connectors = connectors
			r.mu.Unlock()
		},
	}, nil
}

func buildHost(cfg config.Config, connector string) (*Proof, wayland.HostCallbacks, error) {
	policy := cfg.ForConnector(connector)
	bar, err := NewWithTheme(ThemeFrom(cfg, policy), policy)
	if err != nil {
		return nil, wayland.HostCallbacks{}, err
	}
	return bar, wayland.HostCallbacks{
		Configure: bar.Configure,
		Render:    bar.Render,
		Handle:    bar.Handle,
	}, nil
}

// DropHost releases a bar after its surface is destroyed.
func (r *Registry) DropHost(global uint32) {
	r.mu.Lock()
	delete(r.bars, global)
	delete(r.connectors, global)
	r.mu.Unlock()
}

// WorkspaceFor reports the held label for a connector, whether or not a bar
// exists for it.
func (r *Registry) WorkspaceFor(connector string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if label, ok := r.workspaces[connector]; ok {
		return label
	}
	return "-"
}

// UpdateNiri projects a snapshot into per-connector labels and reports the
// connectors whose label actually changed, invalidating only those bars.
func (r *Registry) UpdateNiri(s niri.Snapshot) []string {
	labels := projectWorkspaces(s)

	r.mu.Lock()
	var changed []string
	var dirty []uint32
	for connector, label := range labels {
		if r.workspaces[connector] == label {
			continue
		}
		r.workspaces[connector] = label
		changed = append(changed, connector)
		// ponytail: bar count is output count. Add a connector index only if
		// this scan ever appears in a profile.
		for global, bar := range r.bars {
			if r.connectors[global] == connector {
				bar.SetWorkspace(label)
				dirty = append(dirty, global)
			}
		}
	}
	r.mu.Unlock()

	// A connector whose wl_output has not been announced has no bar to redraw.
	// NewHost applies the held label when that bar is finally built.
	for _, global := range dirty {
		r.queueInvalidation(global)
	}
	return changed
}

// queueInvalidation keeps per-bar redraws while capacity permits. On overflow
// it replaces the pending set with one broadcast redraw, which covers every
// state update that any removed per-bar message represented.
func (r *Registry) queueInvalidation(global uint32) {
	r.invalidationMu.Lock()
	defer r.invalidationMu.Unlock()
	select {
	case r.invalidations <- wayland.Invalidation{Global: global}:
		return
	default:
	}
	for {
		select {
		case <-r.invalidations:
			continue
		default:
			r.invalidations <- wayland.Invalidation{}
			return
		}
	}
}

// projectWorkspaces reduces a snapshot to one label per output. A focused
// workspace outranks a merely active one on the same output.
func projectWorkspaces(s niri.Snapshot) map[string]string {
	labels := make(map[string]string, len(s.Workspaces))
	focused := make(map[string]bool, len(s.Workspaces))
	for _, w := range s.Workspaces {
		if w.Output == "" || !(w.Focused || w.Active) {
			continue
		}
		if focused[w.Output] && !w.Focused {
			continue
		}
		label := w.Name
		if label == "" {
			label = strconv.Itoa(w.Index)
		}
		labels[w.Output] = label
		if w.Focused {
			focused[w.Output] = true
		}
	}
	return labels
}

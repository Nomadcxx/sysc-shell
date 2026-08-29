package shell

import (
	"strconv"
	"sync"

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
	workspaces map[string]string
	bars       map[string]*Proof

	invalidations chan wayland.Invalidation
}

func NewRegistry() *Registry {
	return &Registry{
		workspaces:    make(map[string]string),
		bars:          make(map[string]*Proof),
		invalidations: make(chan wayland.Invalidation, 8),
	}
}

// Invalidations is the channel the Wayland owner receives from. The registry
// owns it and never closes it.
func (r *Registry) Invalidations() <-chan wayland.Invalidation { return r.invalidations }

// NewHost builds the hooks for one connector's bar.
func (r *Registry) NewHost(connector string) (wayland.HostCallbacks, error) {
	bar, err := New()
	if err != nil {
		return wayland.HostCallbacks{}, err
	}

	r.mu.Lock()
	if label, ok := r.workspaces[connector]; ok {
		bar.SetWorkspace(label)
	}
	r.bars[connector] = bar
	r.mu.Unlock()

	return wayland.HostCallbacks{
		Configure: bar.Configure,
		Render:    bar.Render,
		Handle:    bar.Handle,
	}, nil
}

// DropHost releases a bar after its surface is destroyed.
func (r *Registry) DropHost(connector string) {
	r.mu.Lock()
	delete(r.bars, connector)
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
	for connector, label := range labels {
		if r.workspaces[connector] == label {
			continue
		}
		r.workspaces[connector] = label
		changed = append(changed, connector)
		if bar, ok := r.bars[connector]; ok {
			bar.SetWorkspace(label)
		}
	}
	r.mu.Unlock()

	for _, connector := range changed {
		select {
		case r.invalidations <- wayland.Invalidation{Connector: connector}:
		default:
		}
	}
	return changed
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

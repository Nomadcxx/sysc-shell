package shell

import (
	"sort"

	"github.com/Nomadcxx/sysc-shell/internal/ipc"
)

// PanelsTopic is the "panels" IPC topic: the sorted ids of the open panels,
// the same set Status reports. It costs nothing while nobody subscribes.
func (r *Registry) PanelsTopic() ipc.Provider {
	return ipc.Provider{
		Start: func(publish func(any)) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.panelsPublish = publish
			r.publishPanelsLocked()
			return nil
		},
		Stop: func() {
			r.mu.Lock()
			r.panelsPublish = nil
			r.mu.Unlock()
		},
	}
}

// publishPanelsLocked publishes a fresh sorted slice of the open panel ids to
// a subscribed topic. Each publish gets its own slice, because subscribers
// marshal it after r.mu is released.
func (r *Registry) publishPanelsLocked() {
	if r.panelsPublish == nil {
		return
	}
	ids := make([]string, 0, len(r.panelHosts))
	for id := range r.panelHosts {
		ids = append(ids, id.String())
	}
	sort.Strings(ids)
	r.panelsPublish(ids)
}

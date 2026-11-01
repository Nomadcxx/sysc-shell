package shell

import (
	"context"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// pluginLocations remembers the location each plugin was last given, so a
// config reload that leaves the place alone pushes nothing.
type pluginLocations struct {
	mu   sync.Mutex
	sent map[string]v1.Location
}

// record notes that id now has loc, and reports whether that is news to it.
func (l *pluginLocations) record(id string, loc v1.Location) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if prev, ok := l.sent[id]; ok && prev == loc {
		return false
	}
	if l.sent == nil {
		l.sent = map[string]v1.Location{}
	}
	l.sent[id] = loc
	return true
}

func locationOf(place services.Place) v1.Location {
	return v1.Location{
		Configured: place.Configured, Pending: place.Pending,
		Latitude: place.Latitude, Longitude: place.Longitude, Label: place.Label,
	}
}

// location answers location.get. A city that cannot be resolved yet is a
// pending location rather than a failed call. A plugin can retry location.get;
// a later config reload may push location.changed after resolution.
func (h *pluginHost) location(ctx context.Context, id string) (v1.Location, error) {
	if h.r.weather == nil {
		return v1.Location{}, nil
	}
	place, err := h.r.weather.Place(ctx)
	if err != nil && !place.Pending {
		return v1.Location{}, err
	}
	loc := locationOf(place)
	h.locations.record(id, loc)
	return loc, nil
}

// locationMaybeChanged pushes location.changed to every running plugin that
// is granted the location capability, negotiated a minor that carries the
// message, and was last given a different place. It runs after a config
// reload, off the caller's locks, because resolving a new city is a network
// request.
func (h *pluginHost) locationMaybeChanged() {
	if h == nil || h.r.weather == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(h.ctx, pluginCallTimeout)
		defer cancel()
		place, err := h.r.weather.Place(ctx)
		if err != nil && !place.Pending {
			return
		}
		loc := locationOf(place)
		h.mu.Lock()
		targets := map[string]*pluginSlot{}
		for id, slot := range h.slots {
			protocol := slot.rt.Protocol()
			if protocol.Major == v1.ProtocolMajor && protocol.Minor >= v1.LocationMinor && slot.rt.Allows(plugin.CapLocation) {
				targets[id] = slot
			}
		}
		h.mu.Unlock()
		for id, slot := range targets {
			if h.locations.record(id, loc) {
				_ = slot.rt.Send(&v1.LocationChanged{Location: loc})
			}
		}
	}()
}

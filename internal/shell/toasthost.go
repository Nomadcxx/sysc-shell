package shell

import (
	"sort"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// toastHost owns one Overlay aux surface per configured output and projects
// the visible notification stack onto it. It never owns expiry: it lays out
// the records the service says are active and reports placement back through
// the aggregate presentation state.
type toastHost struct {
	r *Registry

	// request emits aux requests; tests capture them. The owner sends them on
	// Registry.aux in production.
	request func(wayland.AuxRequest)
	// harness is the test capture, when one is installed.
	harnessRef *hostHarness

	// outputs maps connector to wl_registry global for the outputs with a
	// toast surface open.
	outputs map[string]uint32
	// visible and queued are the last computed placement per output.
	visible map[string][]uint32
	queued  map[string][]uint32
	hovered map[string]map[uint32]bool
}

const toastNamespace = "sysc-shell-toast"

// hostHarness captures the aux requests a toast host emits in a test.
type hostHarness struct {
	opens   []*wayland.AuxSpec
	updates []*wayland.AuxUpdate
	closes  []string
}

func (h *hostHarness) request(r wayland.AuxRequest) {
	switch {
	case r.Open != nil:
		h.opens = append(h.opens, r.Open)
	case r.Update != nil:
		h.updates = append(h.updates, r.Update)
	default:
		h.closes = append(h.closes, r.ID)
	}
}

func newToastHost(r *Registry, harness *hostHarness) *toastHost {
	h := &toastHost{
		r:       r,
		outputs: map[string]uint32{},
		visible: map[string][]uint32{},
		queued:  map[string][]uint32{},
		hovered: map[string]map[uint32]bool{},
	}
	if harness != nil {
		h.request = harness.request
		h.harnessRef = harness
	} else {
		h.request = func(req wayland.AuxRequest) { r.sendAux(req) }
	}
	return h
}

func (h *toastHost) harness() *hostHarness { return h.harnessRef }

func toastSurfaceID(connector string) string { return "toast:" + connector }

// syncOutputs opens a surface for each new output and closes surfaces whose
// output went away. Outputs are identified by wl_registry global, matching
// the registry's rule that a connector can change globals across a reconnect.
func (h *toastHost) syncOutputs(globals map[string]uint32) {
	for connector, global := range h.outputs {
		if _, ok := globals[connector]; !ok {
			h.request(wayland.AuxRequest{Output: global, ID: toastSurfaceID(connector)})
			delete(h.outputs, connector)
			delete(h.visible, connector)
			delete(h.queued, connector)
			delete(h.hovered, connector)
		}
	}
	for connector, global := range globals {
		if _, ok := h.outputs[connector]; ok {
			continue
		}
		h.outputs[connector] = global
		h.hovered[connector] = map[uint32]bool{}
		h.request(wayland.AuxRequest{Output: global, Open: h.spec(connector)})
	}
	h.recompute()
}

func (h *toastHost) spec(connector string) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            toastSurfaceID(connector),
		Namespace:     toastNamespace,
		Layer:         layershell.ZwlrLayerShellV1LayerOverlay,
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { h.recompute(); return nil },
		},
	}
}

// recompute relayouts every open output from the current projection and
// publishes each surface's input region. It is safe to call on any record,
// geometry, or output change.
func (h *toastHost) recompute() {
	s := h.r.notify
	s.mu.Lock()
	suppressed := s.dnd || s.centerOpen
	records := make([]uint32, 0, len(s.active))
	for id := range s.active {
		records = append(records, id)
	}
	s.mu.Unlock()

	// Newest first: the stack reads down from the freshest card.
	sort.Slice(records, func(i, j int) bool { return records[i] > records[j] })

	for _, connector := range h.outputOrder() {
		global, ok := h.outputs[connector]
		if !ok {
			continue
		}
		geom := h.geometryFor(connector)
		heights := make([]int, 0, len(records))
		ids := make([]uint32, 0, len(records))
		if !suppressed {
			for _, id := range records {
				heights = append(heights, h.cardHeight(id))
				ids = append(ids, id)
			}
		}
		visible, queued := placeIDs(ids, heights, geom)
		h.visible[connector] = visible
		h.queued[connector] = queued

		h.request(wayland.AuxRequest{
			Output: global,
			ID:     toastSurfaceID(connector),
			Update: &wayland.AuxUpdate{
				SetInputRegion: true,
				InputRects:     toastInputRegion(h.cardRects(connector, visible)),
			},
		})
	}
}

// placeIDs is the id-carrying half of toastLayout: geometry decides which
// records are visible and which queue.
func placeIDs(ids []uint32, heights []int, geom toastGeometry) (visible, queued []uint32) {
	_, queuedIdx := toastLayout(geom, heights)
	queuedSet := map[int]bool{}
	for _, i := range queuedIdx {
		queuedSet[i] = true
	}
	for i, id := range ids {
		if queuedSet[i] {
			queued = append(queued, id)
		} else {
			visible = append(visible, id)
		}
	}
	return visible, queued
}

func (h *toastHost) outputOrder() []string {
	out := make([]string, 0, len(h.outputs))
	for c := range h.outputs {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// geometryFor reports the output's logical geometry. Until the owner wires
// real configures, the design default stands in.
func (h *toastHost) geometryFor(string) toastGeometry {
	return toastGeometry{OutputW: 1920, OutputH: 1080, Corner: toastTopRight}
}

// cardHeight is the layout height of one card. The real measure happens in
// the aux Configure callback; the stack estimate uses one line of body.
func (h *toastHost) cardHeight(uint32) int { return 96 }

// cardRects lays out the visible ids for one output and returns their rects.
func (h *toastHost) cardRects(connector string, ids []uint32) []ui.Rect {
	heights := make([]int, len(ids))
	for i := range ids {
		heights[i] = h.cardHeight(ids[i])
	}
	rects, _ := toastLayout(h.geometryFor(connector), heights)
	return rects
}

// viewFor reports one record's placement for the aggregate presentation
// state. The registry's precedence collapses it.
func (h *toastHost) viewFor(id uint32) presentationView {
	v := presentationView{}
	for connector := range h.outputs {
		for _, vid := range h.visible[connector] {
			if vid == id {
				if h.hovered[connector][id] {
					v.hovered = append(v.hovered, connector)
				}
				v.visible = append(v.visible, connector)
			}
		}
		for _, qid := range h.queued[connector] {
			if qid == id {
				v.queued = append(v.queued, connector)
			}
		}
	}
	return v
}

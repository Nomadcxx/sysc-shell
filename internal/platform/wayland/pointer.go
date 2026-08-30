package wayland

import "github.com/Nomadcxx/sysc-wayland/client"

// pointerFocus identifies which bar the pointer is on, replacing the single
// boolean the proof used.
//
// Coordinates are logical: wl_pointer reports them in the viewport destination
// space, which is exactly the space layout and hit testing work in.
type pointerFocus struct {
	host   *OutputHost
	x, y   float64
	serial uint32
}

// enterSurface focuses a bar and forwards the enter coordinates, so a press
// with no intervening motion acts at the right place.
func (o *owner) enterSurface(h *OutputHost, x, y float64, serial uint32) {
	o.focus = pointerFocus{host: h, x: x, y: y, serial: serial}
	o.deliver(h, Event{Kind: EventPointerEnter, X: x, Y: y, Serial: serial})
}

// leaveSurface clears focus only when the leave names the focused surface, so
// an out-of-order leave for a surface focus has already left is discarded.
func (o *owner) leaveSurface(h *OutputHost) {
	if o.focus.host != h {
		return
	}
	o.clearFocus()
}

// clearFocus drops focus and tells the focused bar, so pressed-node state
// resets rather than surviving into a recreated surface.
func (o *owner) clearFocus() {
	h := o.focus.host
	o.focus = pointerFocus{}
	if h != nil {
		o.deliver(h, Event{Kind: EventPointerLeave})
	}
}

// hostBySurface resolves a wl_surface to the host that owns it.
func (o *owner) hostBySurface(surface *client.Surface) (*OutputHost, bool) {
	if surface == nil {
		return nil, false
	}
	for _, h := range o.hosts.each() {
		if h.bar.surface != nil && h.bar.surface == surface {
			return h, true
		}
	}
	return nil, false
}

// deliver hands a pointer event to one bar and invalidates it when the
// application reports that its state changed.
func (o *owner) deliver(h *OutputHost, e Event) {
	if h == nil || !h.alive || h.bar.app.Handle == nil {
		return
	}
	if h.bar.app.Handle(e) {
		h.bar.sched.Invalidate()
	}
}

package wayland

import "github.com/Nomadcxx/sysc-wayland/client"

// pointerFocus identifies which surface the pointer is on.
//
// Coordinates are logical: wl_pointer reports them in the viewport destination
// space, which is exactly the space layout and hit testing work in.
type pointerFocus struct {
	host   *OutputHost
	unit   *surfaceUnit
	x, y   float64
	serial uint32
}

type axisAcc struct {
	vert, horz           float64
	discreteV, discreteH int32
	v120V, v120H         int32
	has                  bool
}

// enterSurface focuses a bar and forwards the enter coordinates, so a press
// with no intervening motion acts at the right place.
func (o *owner) enterSurface(h *OutputHost, x, y float64, serial uint32) {
	o.enterUnit(h, h.bar, x, y, serial)
}

func (o *owner) enterUnit(h *OutputHost, u *surfaceUnit, x, y float64, serial uint32) {
	o.focus = pointerFocus{host: h, unit: u, x: x, y: y, serial: serial}
	o.deliverUnit(h, u, Event{Kind: EventPointerEnter, X: x, Y: y, Serial: serial})
}

// leaveSurface clears focus only when the leave names the focused surface, so
// an out-of-order leave for a surface focus has already left is discarded.
func (o *owner) leaveSurface(h *OutputHost) {
	if h == nil {
		return
	}
	o.leaveUnit(h.bar)
}

func (o *owner) leaveUnit(u *surfaceUnit) {
	if o.focus.unit != u {
		return
	}
	o.clearFocus()
}

// clearFocus drops focus and tells the focused surface, so pressed-node state
// resets rather than surviving into a recreated surface.
func (o *owner) clearFocus() {
	h, u := o.focus.host, o.focus.unit
	o.focus = pointerFocus{}
	if u != nil {
		o.deliverUnit(h, u, Event{Kind: EventPointerLeave})
	}
}

func (o *owner) unitBySurface(surface *client.Surface) (*OutputHost, *surfaceUnit, bool) {
	if surface == nil {
		return nil, nil, false
	}
	for _, h := range o.hosts.each() {
		if h.bar.surface != nil && h.bar.surface == surface {
			return h, h.bar, true
		}
		for _, u := range h.aux {
			if u.surface != nil && u.surface == surface {
				return h, u, true
			}
		}
	}
	return nil, nil, false
}

// hostBySurface resolves a wl_surface to the host that owns it.
func (o *owner) hostBySurface(surface *client.Surface) (*OutputHost, bool) {
	h, _, ok := o.unitBySurface(surface)
	return h, ok
}

// deliver hands a pointer event to one bar and invalidates it when the
// application reports that its state changed.
func (o *owner) deliver(h *OutputHost, e Event) {
	if h == nil {
		return
	}
	o.deliverUnit(h, h.bar, e)
}

func (o *owner) deliverUnit(h *OutputHost, u *surfaceUnit, e Event) {
	if h == nil || u == nil || !h.alive || u.app.Handle == nil {
		return
	}
	if u.app.Handle(e) {
		u.sched.Invalidate()
	}
	o.syncIME(u)
}

func (o *owner) addAxisValue(axis uint32, v float64) {
	o.axis.has = true
	if axis == 0 {
		o.axis.vert += v
		return
	}
	o.axis.horz += v
}

func (o *owner) addAxisDiscrete(axis uint32, d int32) {
	o.axis.has = true
	if axis == 0 {
		o.axis.discreteV += d
		return
	}
	o.axis.discreteH += d
}

func (o *owner) addAxisValue120(axis uint32, v int32) {
	o.axis.has = true
	if axis == 0 {
		o.axis.v120V += v
		return
	}
	o.axis.v120H += v
}

func (o *owner) flushAxis() {
	acc := o.axis
	o.axis = axisAcc{}
	if !acc.has || o.focus.unit == nil {
		return
	}
	e := Event{Kind: EventPointerAxis, X: o.focus.x, Y: o.focus.y}
	if acc.vert != 0 || acc.discreteV != 0 || acc.v120V != 0 {
		e.AxisValue = acc.vert
		e.AxisDiscrete = acc.discreteV
		e.AxisValue120 = acc.v120V
	} else {
		e.Axis = 1
		e.AxisValue = acc.horz
		e.AxisDiscrete = acc.discreteH
		e.AxisValue120 = acc.v120H
	}
	o.deliverUnit(o.focus.host, o.focus.unit, e)
}

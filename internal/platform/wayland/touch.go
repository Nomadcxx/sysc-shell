package wayland

import "github.com/Nomadcxx/sysc-wayland/client"

// touchButton is BTN_LEFT, the button a finger presses.
const touchButton = 0x110

// touchFocus is the one finger the shell follows. Niri delivers touch only
// to a client that bound wl_touch and emulates no pointer, so the shell turns
// its first finger into the pointer events its surfaces already handle. The
// pointer's own focus is kept apart, so the mouse keeps its hover.
type touchFocus struct {
	host *OutputHost
	unit *surfaceUnit
	id   int32
	x, y float64
}

// touchDown presses at the finger on the unit under it. A finger that lands
// while another is down is ignored.
func (o *owner) touchDown(surface *client.Surface, id int32, x, y float64, serial uint32) {
	if o.touch.unit != nil {
		return
	}
	h, u, ok := o.unitBySurface(surface)
	if !ok {
		return
	}
	o.touch = touchFocus{host: h, unit: u, id: id, x: x, y: y}
	o.deliverUnit(h, u, Event{Kind: EventPointerEnter, X: x, Y: y, Serial: serial})
	o.deliverUnit(h, u, Event{Kind: EventPointerPress, Button: touchButton, Serial: serial,
		X: x, Y: y, Mods: o.mods})
}

func (o *owner) touchMotion(id int32, x, y float64) {
	if o.touch.unit == nil || o.touch.id != id {
		return
	}
	o.touch.x, o.touch.y = x, y
	o.deliverUnit(o.touch.host, o.touch.unit, Event{Kind: EventPointerMotion, X: x, Y: y})
}

// touchUp releases where the finger lifted, then ends the touch.
func (o *owner) touchUp(id int32, serial uint32) {
	t := o.touch
	if t.unit == nil || t.id != id {
		return
	}
	o.deliverUnit(t.host, t.unit, Event{Kind: EventPointerRelease, Button: touchButton, Serial: serial,
		X: t.x, Y: t.y, Mods: o.mods})
	o.endTouch()
}

// endTouch forgets the finger without a release, as a cancel does, so nothing
// activates. The unit loses the finger's hover; if the mouse is on it, it
// gets the mouse's position back instead.
func (o *owner) endTouch() {
	t := o.touch
	o.touch = touchFocus{}
	if t.unit == nil {
		return
	}
	if o.focus.unit == t.unit {
		o.deliverUnit(t.host, t.unit, Event{Kind: EventPointerMotion, X: o.focus.x, Y: o.focus.y})
		return
	}
	o.deliverUnit(t.host, t.unit, Event{Kind: EventPointerLeave})
}

// touchUnitGone ends a touch on a unit that is closing.
func (o *owner) touchUnitGone(u *surfaceUnit) {
	if u != nil && o.touch.unit == u {
		o.endTouch()
	}
}

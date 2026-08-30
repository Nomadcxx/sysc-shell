package wayland

import "github.com/Nomadcxx/sysc-wayland/client"

// keyFocus is the surface that currently has wl_keyboard enter.
type keyFocus struct {
	host *OutputHost
	unit *surfaceUnit
}

func (o *owner) enterKeyboard(h *OutputHost, u *surfaceUnit) {
	o.keyFocus = keyFocus{host: h, unit: u}
	o.syncIME(u)
}

func (o *owner) leaveKeyboard() {
	o.setTextInputEnabled(false)
	o.keyFocus = keyFocus{}
}

// deliverKey forwards a wl_keyboard.key to the focused surface. Key is the
// evdev code the compositor sent; subtracting 8 underflows KEY_ESC (1).
func (o *owner) deliverKey(serial, key, state uint32) {
	if o.keyFocus.unit == nil {
		return
	}
	kind := EventKeyRelease
	switch state {
	case uint32(client.KeyboardKeyStatePressed), uint32(client.KeyboardKeyStateRepeated):
		kind = EventKeyPress
	}
	o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, Event{
		Kind: kind, Key: key, Serial: serial,
	})
}

package wayland

import (
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// keyFocus is the surface that currently has wl_keyboard enter.
type keyFocus struct {
	host *OutputHost
	unit *surfaceUnit
}

// keyRepeat is the client half of key repeat. wl_keyboard.repeat_info names a
// rate and delay once, and the compositor then sends exactly one key event per
// physical press: synthesising the rest is the client's job. niri never sends
// KeyboardKeyStateRepeated, so without this a held arrow moves one row and a
// held Backspace deletes one character.
//
// The timer is the owner goroutine's poll deadline rather than a
// time.Timer, so a repeat is delivered on the same goroutine as every other
// key and cannot outlive the surface it is aimed at: the loop that fires it
// is the loop that tore the surface down.
type keyRepeat struct {
	// rate is keys per second and delay is milliseconds, straight from
	// repeat_info. A rate of zero disables repeat entirely.
	rate  int32
	delay int32

	armed  bool
	key    uint32
	serial uint32
	next   time.Time
}

// evdev modifier codes. Modifiers do not repeat: repeating one would deliver a
// stream of Shift presses into the focused field, and arming on a modifier
// would let releasing it cancel the repeat of the key actually being held.
var repeatExempt = map[uint32]bool{
	29:  true, // KEY_LEFTCTRL
	42:  true, // KEY_LEFTSHIFT
	54:  true, // KEY_RIGHTSHIFT
	56:  true, // KEY_LEFTALT
	58:  true, // KEY_CAPSLOCK
	69:  true, // KEY_NUMLOCK
	70:  true, // KEY_SCROLLLOCK
	97:  true, // KEY_RIGHTCTRL
	100: true, // KEY_RIGHTALT
	125: true, // KEY_LEFTMETA
	126: true, // KEY_RIGHTMETA
	127: true, // KEY_COMPOSE
}

func (o *owner) enterKeyboard(h *OutputHost, u *surfaceUnit) {
	o.keyFocus = keyFocus{host: h, unit: u}
	o.syncIME(u)
}

func (o *owner) leaveKeyboard() {
	o.setTextInputEnabled(false)
	o.keyFocus = keyFocus{}
	o.stopRepeat()
}

// now is the owner's clock. Tests replace it to drive the repeat deadline
// without sleeping.
func (o *owner) now() time.Time {
	if o.clock != nil {
		return o.clock()
	}
	return time.Now()
}

// setRepeatInfo records the compositor's repeat parameters. Negative values
// are illegal in the protocol and a zero rate disables repeat; both stop any
// repeat already running, so a mid-session change to "off" takes effect at
// once rather than at the next release.
func (o *owner) setRepeatInfo(rate, delay int32) {
	if rate < 0 || delay < 0 {
		rate, delay = 0, 0
	}
	o.repeat.rate, o.repeat.delay = rate, delay
	if rate == 0 {
		o.stopRepeat()
	}
}

func (o *owner) repeatEnabled() bool {
	return o.repeat.rate > 0
}

// armRepeat starts the delay countdown for a newly pressed key. A second press
// while another key is held takes the repeat over, which is what a keyboard
// does.
func (o *owner) armRepeat(key, serial uint32) {
	if !o.repeatEnabled() || repeatExempt[key] || o.keyFocus.unit == nil {
		return
	}
	o.repeat.armed = true
	o.repeat.key, o.repeat.serial = key, serial
	o.repeat.next = o.now().Add(time.Duration(o.repeat.delay) * time.Millisecond)
}

// disarmRepeat stops the repeat when the key driving it is released. A release
// of some other key -- a modifier tapped mid-hold -- leaves it running.
func (o *owner) disarmRepeat(key uint32) {
	if o.repeat.armed && o.repeat.key == key {
		o.stopRepeat()
	}
}

func (o *owner) stopRepeat() {
	o.repeat.armed = false
	o.repeat.next = time.Time{}
}

// repeatTimeout is the poll deadline in milliseconds: -1 blocks until an event
// arrives, which is the whole life of the loop when nothing is held.
func (o *owner) repeatTimeout() int {
	if !o.repeat.armed || !o.repeatEnabled() {
		return -1
	}
	remaining := o.repeat.next.Sub(o.now())
	if remaining <= 0 {
		return 0
	}
	// Round up: a sub-millisecond remainder must not poll with 0 and spin.
	return int((remaining + time.Millisecond - 1) / time.Millisecond)
}

// fireRepeat delivers at most one synthetic press per loop pass and rebases the
// next deadline on the current time. Delivering the whole backlog after a long
// render would dump a burst of arrow keys into the panel; letting the effective
// rate fall back to the loop rate is the better failure.
func (o *owner) fireRepeat() {
	if !o.repeat.armed || !o.repeatEnabled() {
		return
	}
	now := o.now()
	if now.Before(o.repeat.next) {
		return
	}
	o.repeat.next = now.Add(time.Second / time.Duration(o.repeat.rate))
	o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, Event{
		Kind: EventKeyPress, Key: o.repeat.key, Serial: o.repeat.serial,
	})
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
		o.armRepeat(key, serial)
	default:
		o.disarmRepeat(key)
	}
	o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, Event{
		Kind: kind, Key: key, Serial: serial,
	})
}

package wayland

import (
	"bytes"
	"log"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
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
	// A key held across a focus move keeps repeating: the compositor sent
	// exactly one press, so the repeat is the only thing still delivering it.
	// The fresh delay keeps the new surface from inheriting a deadline that
	// has already passed. The serial stays the original press's.
	if o.repeat.armed {
		o.repeat.next = o.now().Add(time.Duration(o.repeat.delay) * time.Millisecond)
	}
	o.syncIME(u)
}

// keyboardGone is leaveKeyboard for a surface that is going away rather than
// merely losing focus: nothing may outlive it, so the repeat dies outright.
func (o *owner) keyboardGone() {
	o.stopRepeat()
	o.leaveKeyboard()
}

func (o *owner) leaveKeyboard() {
	o.setTextInputEnabled(false)
	o.keyFocus = keyFocus{}
	// A focus move suspends rather than kills: the key is still down and the
	// compositor will not send another press, so stopping here is what made
	// repeats stop. fireRepeat and repeatTimeout hold off while the focus is
	// nil, and enterKeyboard retargets on the way back in.
	// A dead key pressed before focus moved must not compose with the first
	// key typed at the new focus.
	if o.keymap != nil {
		o.keymap.resetCompose()
	}
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
	if !o.repeat.armed || !o.repeatEnabled() || o.keyFocus.unit == nil {
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
	if !o.repeat.armed || !o.repeatEnabled() || o.keyFocus.unit == nil {
		return
	}
	now := o.now()
	if now.Before(o.repeat.next) {
		return
	}
	o.repeat.next = now.Add(time.Second / time.Duration(o.repeat.rate))
	o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, o.keyEvent(EventKeyPress, o.repeat.key, o.repeat.serial))
}

// deliverKey forwards a wl_keyboard.key to the focused surface. Key is the
// evdev code the compositor sent; subtracting 8 underflows KEY_ESC (1).
func (o *owner) deliverKey(serial, key, state uint32) {
	kind := EventKeyRelease
	switch state {
	case uint32(client.KeyboardKeyStatePressed), uint32(client.KeyboardKeyStateRepeated):
		kind = EventKeyPress
		o.armRepeat(key, serial)
	default:
		o.disarmRepeat(key)
	}
	// The state machine runs even without a focus, so a release arriving
	// mid-move cancels the deadline; armRepeat refuses to arm with none.
	if o.keyFocus.unit == nil {
		return
	}
	o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, o.keyEvent(kind, key, serial))
}

// setModifiers records wl_keyboard.modifiers. group selects the layout and is
// used once a keymap is loaded.
func (o *owner) setModifiers(depressed, latched, locked, group uint32) {
	o.mods = ui.ModsFromMask(depressed, latched, locked)
	if o.keymap != nil {
		o.keymap.setMask(depressed, latched, locked, group)
	}
}

// loadKeymap installs the compositor's keymap. Anything but a parseable
// xkb_v1 keymap keeps the US fallback, logged once per keymap event.
func (o *owner) loadKeymap(format uint32, fd int, size uint32) {
	if fd >= 0 {
		defer unix.Close(fd)
	}
	o.keymap = nil
	if format != uint32(client.KeyboardKeymapFormatXkbV1) || fd < 0 || size == 0 {
		return
	}
	data, err := unix.Mmap(fd, 0, int(size), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		log.Printf("wayland: keymap mmap: %v; using the US fallback", err)
		return
	}
	defer unix.Munmap(data)
	text := bytes.TrimRight(data, "\x00")
	k, err := newKeymapResolver(append([]byte(nil), text...), "")
	if err != nil {
		log.Printf("wayland: keymap parse: %v; using the US fallback", err)
		return
	}
	o.keymap = k
}

// keyEvent resolves a key at delivery time. Real presses and synthesised
// repeats both come through here, so a repeat types what the keys held now
// would type.
func (o *owner) keyEvent(kind EventKind, key, serial uint32) Event {
	e := Event{Kind: kind, Key: key, Serial: serial, Mods: o.mods}
	switch {
	case o.keymap == nil:
		k := ui.FallbackKey(key, o.mods)
		e.Sym, e.Text = k.Sym, k.Text
	case kind == EventKeyRelease:
		// A release must not feed compose; only its keysym matters.
		e.Sym, _ = o.keymap.peek(key)
	default:
		e.Sym, e.Text = o.keymap.resolve(key, o.mods)
	}
	return e
}

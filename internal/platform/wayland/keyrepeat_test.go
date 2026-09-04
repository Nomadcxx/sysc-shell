package wayland

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

const (
	keyDown      = 108 // KEY_DOWN
	keyUp        = 103 // KEY_UP
	keyLeftShift = 42  // KEY_LEFTSHIFT
)

// repeatHarness is a keyboard-focused owner on a fake clock, so a repeat
// deadline can be crossed without sleeping.
type repeatHarness struct {
	o    *owner
	seen *[]Event
	at   time.Time
}

func newRepeatHarness(t *testing.T, rate, delay int32) *repeatHarness {
	t.Helper()
	o, h, _, panelSeen := newKeyedHost()
	panel := newSurfaceUnit("panel:launcher")
	panel.app = HostCallbacks{Handle: func(e Event) bool {
		*panelSeen = append(*panelSeen, e)
		return true
	}}
	h.aux["panel:launcher"] = panel

	rh := &repeatHarness{o: o, seen: panelSeen, at: time.Unix(1000, 0)}
	o.clock = func() time.Time { return rh.at }
	o.enterKeyboard(h, panel)
	o.setRepeatInfo(rate, delay)
	return rh
}

// advance moves the clock and runs the loop's repeat step once per pass, which
// is what the poll deadline does in production.
func (rh *repeatHarness) advance(d time.Duration) {
	rh.at = rh.at.Add(d)
	rh.o.fireRepeat()
}

func (rh *repeatHarness) presses(key uint32) int {
	n := 0
	for _, e := range *rh.seen {
		if e.Kind == EventKeyPress && e.Key == key {
			n++
		}
	}
	return n
}

// A held key must repeat: niri sends one press and expects the client to
// synthesise the rest, so before this a held Down moved exactly one row.
func TestKeyRepeatFiresAfterDelayThenAtRate(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 600) // 25/s -> 40ms
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	if got := rh.presses(keyDown); got != 1 {
		t.Fatalf("presses right after the physical press = %d, want 1", got)
	}

	rh.advance(599 * time.Millisecond)
	if got := rh.presses(keyDown); got != 1 {
		t.Fatalf("presses before the delay elapsed = %d, want 1", got)
	}
	rh.advance(1 * time.Millisecond)
	if got := rh.presses(keyDown); got != 2 {
		t.Fatalf("presses at the delay = %d, want 2", got)
	}
	for range 5 {
		rh.advance(40 * time.Millisecond)
	}
	if got := rh.presses(keyDown); got != 7 {
		t.Fatalf("presses after 5 more intervals = %d, want 7", got)
	}
}

// Repeats carry the pressed key's serial and reach the same surface as the
// physical press.
func TestKeyRepeatDeliversToFocusedSurface(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(77, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.advance(100 * time.Millisecond)
	events := *rh.seen
	last := events[len(events)-1]
	if last.Kind != EventKeyPress || last.Key != keyDown || last.Serial != 77 {
		t.Fatalf("repeat = %+v, want a KeyPress of %d with serial 77", last, keyDown)
	}
}

func TestKeyRepeatStopsOnRelease(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.advance(100 * time.Millisecond)
	rh.o.deliverKey(2, keyDown, uint32(client.KeyboardKeyStateReleased))
	before := rh.presses(keyDown)
	rh.advance(time.Second)
	if got := rh.presses(keyDown); got != before {
		t.Fatalf("presses kept growing after release: %d -> %d", before, got)
	}
	if rh.o.repeatTimeout() != -1 {
		t.Fatalf("timeout after release = %d, want -1", rh.o.repeatTimeout())
	}
}

// A timer that outlives its surface delivers keys into a freed host, so
// keyboard leave must stop it as firmly as a release does.
func TestKeyRepeatStopsOnKeyboardLeave(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.o.leaveKeyboard()
	before := rh.presses(keyDown)
	rh.advance(time.Second)
	if got := rh.presses(keyDown); got != before {
		t.Fatalf("presses after leave: %d -> %d", before, got)
	}
}

// rate == 0 means the compositor disabled repeat, and must be honoured.
func TestKeyRepeatDisabledByZeroRate(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 0, 600)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.advance(5 * time.Second)
	if got := rh.presses(keyDown); got != 1 {
		t.Fatalf("presses with repeat disabled = %d, want 1", got)
	}
	if rh.o.repeatTimeout() != -1 {
		t.Fatalf("timeout with repeat disabled = %d, want -1", rh.o.repeatTimeout())
	}
}

// Negative rate or delay is illegal in the protocol; treat it as disabled
// rather than as a deadline in the past that spins the loop.
func TestKeyRepeatIllegalValuesDisableIt(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, -1, -1)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.advance(5 * time.Second)
	if got := rh.presses(keyDown); got != 1 {
		t.Fatalf("presses with illegal repeat_info = %d, want 1", got)
	}
}

// A repeat_info turning repeat off mid-session stops what is already running.
func TestKeyRepeatInfoOffStopsRunningRepeat(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.o.setRepeatInfo(0, 0)
	before := rh.presses(keyDown)
	rh.advance(time.Second)
	if got := rh.presses(keyDown); got != before {
		t.Fatalf("presses after repeat was turned off: %d -> %d", before, got)
	}
}

// Modifiers must not repeat, and tapping one mid-hold must not cancel the
// repeat of the key actually being held.
func TestKeyRepeatIgnoresModifiers(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyLeftShift, uint32(client.KeyboardKeyStatePressed))
	rh.advance(time.Second)
	if got := rh.presses(keyLeftShift); got != 1 {
		t.Fatalf("Shift presses = %d, want 1 (modifiers do not repeat)", got)
	}

	rh.o.deliverKey(2, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.advance(100 * time.Millisecond)
	rh.o.deliverKey(3, keyLeftShift, uint32(client.KeyboardKeyStateReleased))
	before := rh.presses(keyDown)
	rh.advance(40 * time.Millisecond)
	if got := rh.presses(keyDown); got != before+1 {
		t.Fatalf("Down repeats after a Shift release = %d, want %d", got, before+1)
	}
}

// Pressing a second key while the first is held hands the repeat over.
func TestKeyRepeatLatestKeyWins(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.o.deliverKey(2, keyUp, uint32(client.KeyboardKeyStatePressed))
	downBefore := rh.presses(keyDown)
	rh.advance(100 * time.Millisecond)
	if got := rh.presses(keyUp); got != 2 {
		t.Fatalf("Up presses = %d, want 2 (it took the repeat over)", got)
	}
	if got := rh.presses(keyDown); got != downBefore {
		t.Fatalf("Down kept repeating after Up was pressed: %d -> %d", downBefore, got)
	}
}

// With nothing held the loop must block indefinitely rather than spin.
func TestKeyRepeatTimeoutIsBlockingWhenIdle(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 600)
	if got := rh.o.repeatTimeout(); got != -1 {
		t.Fatalf("idle timeout = %d, want -1", got)
	}
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	if got := rh.o.repeatTimeout(); got != 600 {
		t.Fatalf("armed timeout = %d, want the 600ms delay", got)
	}
	rh.at = rh.at.Add(600*time.Millisecond + 1)
	if got := rh.o.repeatTimeout(); got != 0 {
		t.Fatalf("overdue timeout = %d, want 0", got)
	}
}

// A key pressed with no keyboard focus must not arm anything.
func TestKeyRepeatNeedsFocus(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.leaveKeyboard()
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	if rh.o.repeat.armed {
		t.Fatal("repeat armed with no keyboard focus")
	}
}

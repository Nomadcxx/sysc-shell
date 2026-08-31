package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestKeyboardKeysRouteToFocusedAuxSurface(t *testing.T) {
	t.Parallel()
	o, h, barSeen, panelSeen := newKeyedHost()
	panel := newSurfaceUnit("panel:session")
	panel.app = HostCallbacks{Handle: func(e Event) bool {
		*panelSeen = append(*panelSeen, e)
		return true
	}}
	h.aux["panel:session"] = panel

	o.enterKeyboard(h, panel)
	o.deliverKey(9, 1, uint32(client.KeyboardKeyStatePressed))

	if len(*barSeen) != 0 {
		t.Fatalf("bar Handle received %v, want none", *barSeen)
	}
	if len(*panelSeen) != 1 {
		t.Fatalf("panel Handle received %d events, want 1", len(*panelSeen))
	}
	got := (*panelSeen)[0]
	if got.Kind != EventKeyPress {
		t.Fatalf("kind = %d, want EventKeyPress", got.Kind)
	}
	if got.Key != 1 {
		t.Fatalf("Key = %d, want evdev KEY_ESC 1 (must not subtract 8)", got.Key)
	}
	if got.Serial != 9 {
		t.Fatalf("Serial = %d, want 9", got.Serial)
	}
}

func TestKeyboardEnterClearsOnLeave(t *testing.T) {
	t.Parallel()
	o, h, _, panelSeen := newKeyedHost()
	panel := newSurfaceUnit("panel:session")
	panel.app = HostCallbacks{Handle: func(e Event) bool {
		*panelSeen = append(*panelSeen, e)
		return false
	}}
	h.aux["panel:session"] = panel

	o.enterKeyboard(h, panel)
	o.leaveKeyboard()
	o.deliverKey(1, 1, uint32(client.KeyboardKeyStatePressed))
	if len(*panelSeen) != 0 {
		t.Fatalf("key after leave delivered %v", *panelSeen)
	}
}

func TestPointerRoutesToAuxSurfaceUnderPointer(t *testing.T) {
	t.Parallel()
	o, h, barSeen, _ := newKeyedHost()
	var shieldSeen []Event
	shield := newSurfaceUnit("shield:session")
	shield.app = HostCallbacks{Handle: func(e Event) bool {
		shieldSeen = append(shieldSeen, e)
		return false
	}}
	h.aux["shield:session"] = shield

	o.enterUnit(h, shield, 4, 5, 3)
	o.deliverUnit(h, o.focus.unit, Event{Kind: EventPointerMotion, X: 4, Y: 5})
	o.deliverUnit(h, o.focus.unit, Event{Kind: EventPointerPress, Button: 272, X: 4, Y: 5})

	if len(*barSeen) != 0 {
		t.Fatalf("bar Handle received %v while pointer was on the shield", *barSeen)
	}
	if len(shieldSeen) != 3 {
		t.Fatalf("shield Handle received %d events, want enter+motion+press", len(shieldSeen))
	}
}

func TestBarUnaffectedByKeyboardBinding(t *testing.T) {
	t.Parallel()
	o, h, barSeen, _ := newKeyedHost()
	o.deliverKey(1, 1, uint32(client.KeyboardKeyStatePressed))
	if len(*barSeen) != 0 {
		t.Fatalf("bar Handle received keys with no keyboard focus: %v", *barSeen)
	}
	panel := newSurfaceUnit("panel:session")
	panel.app = HostCallbacks{Handle: func(Event) bool { return false }}
	h.aux["panel:session"] = panel
	o.enterKeyboard(h, panel)
	o.deliverKey(1, 28, uint32(client.KeyboardKeyStatePressed))
	if len(*barSeen) != 0 {
		t.Fatalf("bar Handle received keys focused on an aux surface: %v", *barSeen)
	}
}

func TestKeyReleaseRoutesWithoutSubtractingEight(t *testing.T) {
	t.Parallel()
	o, h, _, panelSeen := newKeyedHost()
	panel := newSurfaceUnit("panel:session")
	panel.app = HostCallbacks{Handle: func(e Event) bool {
		*panelSeen = append(*panelSeen, e)
		return false
	}}
	h.aux["panel:session"] = panel
	o.enterKeyboard(h, panel)
	o.deliverKey(2, 28, uint32(client.KeyboardKeyStateReleased))
	if (*panelSeen)[0].Kind != EventKeyRelease || (*panelSeen)[0].Key != 28 {
		t.Fatalf("release = %+v, want EventKeyRelease Key 28", (*panelSeen)[0])
	}
}

func newKeyedHost() (*owner, *OutputHost, *[]Event, *[]Event) {
	s := newHostSet()
	h, _ := s.add(7, nil)
	h.alive = true
	h.connector = "DP-1"
	barSeen := new([]Event)
	panelSeen := new([]Event)
	h.bar.app = HostCallbacks{Handle: func(e Event) bool {
		*barSeen = append(*barSeen, e)
		return false
	}}
	return &owner{hosts: s}, h, barSeen, panelSeen
}

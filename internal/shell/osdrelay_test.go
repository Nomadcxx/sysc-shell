package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func TestLockOSDShowsOnlyWhatChanged(t *testing.T) {
	cases := []struct {
		prev, next services.LockState
		want       []OSDView
	}{
		{services.LockState{}, services.LockState{Caps: true}, []OSDView{{Kind: osdCapsLock, On: true}}},
		{services.LockState{Caps: true}, services.LockState{Caps: true, Num: true}, []OSDView{{Kind: osdNumLock, On: true}}},
		{services.LockState{}, services.LockState{Caps: true, Num: true}, []OSDView{{Kind: osdCapsLock, On: true}, {Kind: osdNumLock, On: true}}},
		{services.LockState{Num: true}, services.LockState{Num: true}, nil},
	}
	for _, tc := range cases {
		got := lockOSD(tc.prev, tc.next)
		if len(got) != len(tc.want) {
			t.Fatalf("%+v -> %+v: %+v", tc.prev, tc.next, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%+v -> %+v: %+v", tc.prev, tc.next, got)
			}
		}
	}
}

func TestFirstLayoutSnapshotIsSilent(t *testing.T) {
	two := niri.KeyboardLayouts{Names: []string{"English (US)", "German"}, Current: 0}
	if _, show := layoutOSD(niri.KeyboardLayouts{}, two, false); show {
		t.Fatal("first snapshot showed a layout OSD")
	}
	switched := niri.KeyboardLayouts{Names: two.Names, Current: 1}
	v, show := layoutOSD(two, switched, true)
	if !show || v.Kind != osdLayout || v.Text != "German" {
		t.Fatalf("switch: %+v %v", v, show)
	}
	if _, show := layoutOSD(switched, switched, true); show {
		t.Fatal("no change showed an OSD")
	}
	if _, show := layoutOSD(two, niri.KeyboardLayouts{Names: two.Names, Current: 5}, true); show {
		t.Fatal("an out-of-range index showed an OSD")
	}
}

package shell

import (
	"testing"

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

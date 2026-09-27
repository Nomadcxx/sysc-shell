package shell

import (
	"testing"
	"time"

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

func TestMediaOSDFiresOnStatusTrackAndSeek(t *testing.T) {
	base := services.MediaState{Available: true, Title: "One", Status: services.PlaybackPlaying, PositionUS: 30e6, LengthUS: 120e6}
	cases := []struct {
		name string
		next services.MediaState
		show bool
		want OSDView
	}{
		{"pause", func() services.MediaState { s := base; s.Status = services.PlaybackPaused; return s }(), true,
			OSDView{Kind: osdMedia, Text: "One", Level: 25}},
		{"track change", func() services.MediaState { s := base; s.Title = "Two"; s.PositionUS = 0; return s }(), true,
			OSDView{Kind: osdMedia, Text: "Two", Level: 0, On: true}},
		{"seek", func() services.MediaState { s := base; s.Seeks = 1; s.PositionUS = 60e6; return s }(), true,
			OSDView{Kind: osdMedia, Text: "One", Level: 50, On: true}},
		{"position tick only", func() services.MediaState { s := base; s.PositionUS = 31e6; return s }(), false, OSDView{}},
		{"stopped", func() services.MediaState { s := base; s.Status = services.PlaybackStopped; return s }(), true,
			OSDView{Kind: osdMedia, Text: "One", Level: 25, Muted: true}},
		{"player gone", services.MediaState{}, false, OSDView{}},
	}
	for _, tc := range cases {
		got, show := mediaOSD(base, tc.next)
		if show != tc.show || (show && got != tc.want) {
			t.Errorf("%s: %+v %v, want %+v %v", tc.name, got, show, tc.want, tc.show)
		}
	}
}

func TestDNDChangesCallTheHookOnceAfterUnlock(t *testing.T) {
	s := &notifyState{}
	var calls []bool
	s.onDND = func(on bool) {
		s.mu.Lock() // This would deadlock if the hook ran under s.mu.
		s.mu.Unlock()
		calls = append(calls, on)
	}
	s.setDND(true)
	s.setDND(true)
	s.setDNDPreset(time.Now(), time.Hour)
	s.setDND(false)
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("calls %v, want [true false]", calls)
	}
}

package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMediaControlMapsEveryAction(t *testing.T) {
	media := services.NewUnavailableMedia()
	t.Cleanup(media.Close)
	for _, tc := range []struct {
		node  ui.Node
		ok    bool
		seek  int64
		seeks bool
	}{
		{node: ui.Node{Action: "media:playpause"}, ok: true},
		{node: ui.Node{Action: "media:next"}, ok: true},
		{node: ui.Node{Action: "media:prev"}, ok: true},
		{node: ui.Node{Action: "media:loop"}, ok: true},
		{node: ui.Node{Action: "media:shuffle"}, ok: true},
		{node: ui.Node{Action: "media:seek:2000000"}, ok: true, seek: 2_000_000, seeks: true},
		// A slider reports its own value; the action suffix is the old position.
		{node: ui.Node{Kind: ui.KindSlider, Action: "media:seek:1", Value: 5_000_000}, ok: true, seek: 5_000_000, seeks: true},
		{node: ui.Node{Action: "media:player:org.mpris.MediaPlayer2.mpv"}, ok: true},
		{node: ui.Node{Action: "media:player:"}},
		{node: ui.Node{Action: "media:unknown"}},
		{node: ui.Node{Action: "cc:dnd"}},
	} {
		n := tc.node
		run, seekTo, ok := mediaControl(media, &n)
		if ok != tc.ok {
			t.Errorf("%q ok = %v, want %v", n.Action, ok, tc.ok)
			continue
		}
		if ok && run == nil {
			t.Errorf("%q has no run function", n.Action)
		}
		if (seekTo != nil) != tc.seeks || seekTo != nil && *seekTo != tc.seek {
			t.Errorf("%q seekTo = %v, want seek=%v %d", n.Action, seekTo, tc.seeks, tc.seek)
		}
	}
	if _, _, ok := mediaControl(nil, &ui.Node{Action: "media:next"}); ok {
		t.Error("nil media service accepted an action")
	}
}

func TestHomeMediaActionWritesThroughTheControlSeam(t *testing.T) {
	r, h := mediaTestRegistry(t, services.MediaState{Available: true, CanNext: true}, nil)
	r.mu.Lock()
	handled := h.activateControlCentre(r, &ui.Node{Action: "media:next"})
	r.mu.Unlock()
	if !handled {
		t.Fatal("Home did not handle media:next")
	}
	select {
	case <-r.invalidations:
	case <-time.After(time.Second):
		t.Fatal("Home media action did not complete through scheduleControl")
	}
}

func TestMediaTransportButtonsFollowCapabilities(t *testing.T) {
	plain := mediaTransportButtons(services.MediaState{CanPrev: true, CanNext: true, CanPause: true, Status: services.PlaybackPlaying}, true)
	if len(plain) != 3 {
		t.Fatalf("plain transport has %d buttons, want 3", len(plain))
	}
	if plain[1].Action != "media:playpause" || plain[1].Children[0].Icon != "pause" {
		t.Fatalf("play button = %+v, want pause glyph while playing", plain[1])
	}
	full := mediaTransportButtons(services.MediaState{CanLoop: true, CanShuffle: true, LoopStatus: "Track", Shuffle: true}, true)
	if len(full) != 5 {
		t.Fatalf("full transport has %d buttons, want 5", len(full))
	}
	if noModes := mediaTransportButtons(services.MediaState{CanLoop: true, CanShuffle: true}, false); len(noModes) != 3 {
		t.Fatalf("modes=false kept %d buttons, want 3", len(noModes))
	}
	stopped := mediaTransportButtons(services.MediaState{}, false)
	if !stopped[1].State.Has(ui.StateDisabled) || !mediaPlayDisabled(services.MediaState{}) {
		t.Fatal("play button is enabled for a player that can neither play nor pause")
	}
}

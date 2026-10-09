package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMediaStripPlacementFollowsTheBarEdgeAndClamps(t *testing.T) {
	work := ui.Rect{X: 0, Y: 40, W: 1536, H: 824}
	for _, tc := range []struct {
		name         string
		edge         string
		anchor, body ui.Rect
		want         ui.Rect
	}{
		{"top centred under the pill", "top", ui.Rect{X: 900, Y: 4, W: 160, H: 32}, ui.Rect{W: 1536, H: 40},
			ui.Rect{X: 794, Y: 46, W: mediaStripW, H: mediaStripH}},
		{"top clamped at the trailing edge", "top", ui.Rect{X: 1500, Y: 4, W: 30, H: 32}, ui.Rect{W: 1536, H: 40},
			ui.Rect{X: 1536 - mediaStripW, Y: 46, W: mediaStripW, H: mediaStripH}},
		{"top clamped at the leading edge", "top", ui.Rect{X: 2, Y: 4, W: 30, H: 32}, ui.Rect{W: 1536, H: 40},
			ui.Rect{X: 0, Y: 46, W: mediaStripW, H: mediaStripH}},
		{"bottom sits above", "bottom", ui.Rect{X: 900, Y: 828, W: 160, H: 32}, ui.Rect{Y: 824, W: 1536, H: 40},
			ui.Rect{X: 794, Y: 824 - mediaStripGap - mediaStripH, W: mediaStripW, H: mediaStripH}},
		{"left sits beside", "left", ui.Rect{X: 4, Y: 400, W: 32, H: 60}, ui.Rect{W: 40, H: 864},
			ui.Rect{X: 46, Y: 330, W: mediaStripW, H: mediaStripH}},
		{"right sits beside", "right", ui.Rect{X: 1500, Y: 400, W: 32, H: 60}, ui.Rect{X: 1496, W: 40, H: 864},
			ui.Rect{X: 1496 - mediaStripGap - mediaStripW, Y: 330, W: mediaStripW, H: mediaStripH}},
	} {
		w := work
		if tc.edge == "bottom" {
			w = ui.Rect{W: 1536, H: 824}
		}
		if tc.edge == "left" || tc.edge == "right" {
			w = ui.Rect{W: 1536, H: 864}
		}
		if got := mediaStripPlacement(tc.edge, tc.anchor, tc.body, w); got != tc.want {
			t.Errorf("%s: placement = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if got := mediaStripPlacement("top", ui.Rect{}, ui.Rect{}, ui.Rect{}); got != (ui.Rect{}) {
		t.Errorf("empty work area placed the strip at %+v", got)
	}
}

func TestMediaStripTreeStates(t *testing.T) {
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 8, 16 }
	players := []services.Player{{Bus: "a", Identity: "mpv"}, {Bus: "b", Identity: "KDE Connect"}}
	seekable := services.MediaState{Available: true, Player: "a", Identity: "mpv", Title: "Oh, No! Anyway…", Artist: "The Critical Drinker",
		Status: services.PlaybackPlaying, CanPause: true, CanNext: true, CanPrev: true, CanSeek: true, CanLoop: true, CanShuffle: true,
		LengthUS: 356_000_000}
	root := mediaStripTree(seekable, players, 121_000_000, nil)
	if root.Name != "Media controls" || root.Role != "group" {
		t.Fatalf("root = %+v, want the Media controls group", root)
	}
	if err := ui.LayoutColumn(root, ui.Rect{W: mediaStripW, H: mediaStripH}, measure); err != nil {
		t.Fatalf("strip does not fit %dx%d: %v", mediaStripW, mediaStripH, err)
	}
	walkNodes(root, func(n *ui.Node) {
		if b := n.Bounds; b.X+b.W > mediaStripW || b.Y+b.H > mediaStripH {
			t.Errorf("%q leaves the strip: %+v", n.Name+n.Text, b)
		}
	})
	slider := findNode(root, func(n *ui.Node) bool { return n.Kind == ui.KindSlider })
	if slider == nil || slider.Value != 121_000_000 || slider.ValueText != "02:01 of 05:56" {
		t.Fatalf("seek slider = %+v", slider)
	}
	chip := findByName(root, "Switch player")
	if chip == nil || chip.Action != "media:player:b" || !strings.Contains(renderText(chip), "2 players") {
		t.Fatalf("player chip = %+v, want a switch to the next player", chip)
	}
	for _, action := range []string{"media:prev", "media:playpause", "media:next", "media:loop", "media:shuffle"} {
		if findNode(root, func(n *ui.Node) bool { return n.Action == action }) == nil {
			t.Errorf("strip lacks %s", action)
		}
	}

	stream := services.MediaState{Available: true, Identity: "radio", Title: "Live", CanPause: true}
	live := mediaStripTree(stream, players[:1], 0, nil)
	if findNode(live, func(n *ui.Node) bool { return n.Kind == ui.KindSlider }) != nil {
		t.Error("a stream with no length got a seek slider")
	}
	if meter := findNode(live, func(n *ui.Node) bool { return n.Kind == ui.KindMeter }); meter == nil || !meter.Absent {
		t.Errorf("stream progress = %+v, want an absent meter", meter)
	}
	if findByName(live, "Switch player") != nil {
		t.Error("one player still offered a switch")
	}
}

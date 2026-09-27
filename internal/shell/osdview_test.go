package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOSDIconAndLabelPerKind(t *testing.T) {
	cases := []struct {
		v           OSDView
		icon, label string
	}{
		{OSDView{Kind: osdAudio, Level: 40}, "volume_up", "Volume"},
		{OSDView{Kind: osdAudio, Muted: true}, "volume_off", "Muted"},
		{OSDView{Kind: osdBrightness, Level: 70}, "brightness_high", "Brightness"},
		{OSDView{Kind: osdMedia, Text: "Song", On: true}, "play_arrow", "Song"},
		{OSDView{Kind: osdMedia, Text: "Song"}, "pause", "Song"},
		{OSDView{Kind: osdMedia, Muted: true}, "music_note", "Stopped"},
		{OSDView{Kind: osdMedia, On: true}, "play_arrow", "Media"},
		{OSDView{Kind: osdCapsLock, On: true}, "keyboard", "Caps Lock on"},
		{OSDView{Kind: osdNumLock}, "keyboard", "Num Lock off"},
		{OSDView{Kind: osdLayout, Text: "German"}, "", "German"},
		{OSDView{Kind: osdDND, On: true}, "do_not_disturb_on", "Do not disturb on"},
		{OSDView{Kind: osdDND}, "notifications", "Do not disturb off"},
	}
	for _, tc := range cases {
		if got := osdIcon(tc.v); got != tc.icon {
			t.Errorf("%+v icon = %q, want %q", tc.v, got, tc.icon)
		}
		if got := osdLabel(tc.v); got != tc.label {
			t.Errorf("%+v label = %q, want %q", tc.v, got, tc.label)
		}
	}
}

func TestOSDTreeShapePerKind(t *testing.T) {
	count := func(n *ui.Node, k ui.Kind) int {
		c := 0
		var walk func(*ui.Node)
		walk = func(n *ui.Node) {
			if n == nil {
				return
			}
			if n.Kind == k {
				c++
			}
			for _, ch := range n.Children {
				walk(ch)
			}
		}
		walk(n)
		return c
	}
	for _, kind := range []string{osdAudio, osdBrightness, osdMedia} {
		tree := osdTree(OSDView{Kind: kind, Level: 50})
		if tree.Kind != ui.KindColumn || count(tree, ui.KindMeter) != 1 {
			t.Errorf("%s: want a column root with one meter", kind)
		}
	}
	for _, kind := range []string{osdCapsLock, osdNumLock, osdDND, osdLayout} {
		if count(osdTree(OSDView{Kind: kind}), ui.KindMeter) != 0 {
			t.Errorf("%s: toggle and text kinds carry no meter", kind)
		}
	}
	if count(osdTree(OSDView{Kind: osdLayout, Text: "German"}), ui.KindIcon) != 0 {
		t.Error("layout is text-only")
	}
}

// Every kind lays out inside the 220x64 OSD body with real layout rules.
func TestOSDTreesFitTheBody(t *testing.T) {
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 7, 16 }
	body := ui.Rect{W: osdWidth - 16, H: osdHeight - 16}
	for _, v := range []OSDView{
		{Kind: osdAudio, Level: 100}, {Kind: osdBrightness}, {Kind: osdCapsLock, On: true},
		{Kind: osdMedia, Text: "A very long track title that must be clipped not overflow", Level: 30, On: true},
		{Kind: osdLayout, Text: "English (US, intl., with dead keys)"}, {Kind: osdDND, On: true},
	} {
		root := osdTree(v)
		if err := ui.LayoutColumn(root, body, measure); err != nil {
			t.Errorf("%+v: %v", v, err)
			continue
		}
		var checkBounds func(*ui.Node)
		checkBounds = func(n *ui.Node) {
			b := n.Bounds
			if b.X < body.X || b.Y < body.Y || b.X+b.W > body.X+body.W || b.Y+b.H > body.Y+body.H {
				t.Errorf("%+v: node %v outside OSD body: %+v", v, n.Kind, b)
			}
			for _, child := range n.Children {
				checkBounds(child)
			}
		}
		checkBounds(root)
	}
}

func TestOSDHandleRidesTheFillAndCrossfadesNearFull(t *testing.T) {
	cases := []struct {
		level      int
		x          int
		icon, text uint8
	}{
		{0, 10, 100, 0},
		{50, 60, 100, 0},
		{85, 95, 100, 0},
		{90, 100, 50, 50},
		{95, 105, 0, 100},
		{100, 110, 0, 100},
		{140, 110, 0, 100},
		{-5, 10, 100, 0},
	}
	for _, tc := range cases {
		x, icon, text := osdHandle(tc.level, 10, 100)
		if x != tc.x || icon != tc.icon || text != tc.text {
			t.Errorf("level %d: x=%d icon=%d text=%d, want %d %d %d", tc.level, x, icon, text, tc.x, tc.icon, tc.text)
		}
	}
}

func TestOSDHandleNodesOnlyForMeteredKinds(t *testing.T) {
	meter := &ui.Node{Kind: ui.KindMeter, Bounds: ui.Rect{X: 40, Y: 30, W: 150, H: 6}}
	if n := osdHandleNodes(OSDView{Kind: osdCapsLock}, meter); len(n) != 0 {
		t.Fatal("a toggle kind got a handle")
	}
	nodes := osdHandleNodes(OSDView{Kind: osdAudio, Level: 90}, meter)
	if len(nodes) != 2 || nodes[0].Opacity != 50 || nodes[1].Text != "90%" {
		t.Fatalf("handle nodes at 90%%: %+v", nodes)
	}
}

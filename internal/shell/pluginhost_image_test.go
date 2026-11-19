package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPluginImageKeyDecodesAtPhysicalScale(t *testing.T) {
	wide := &ui.Node{Kind: ui.KindImage, ImagePath: "/marks/wordmark.png", ImageW: 40, ImageH: 12}
	square := &ui.Node{Kind: ui.KindImage, ImagePath: "/marks/logo.png", ImageSize: 24}
	for _, tc := range []struct {
		name  string
		node  *ui.Node
		scale int
		want  icons.Key
	}{
		{"wide at 100%", wide, 120, icons.Key{Name: "/marks/wordmark.png", W: 40, H: 12}},
		{"wide at 150%", wide, 180, icons.Key{Name: "/marks/wordmark.png", W: 60, H: 18}},
		{"square at 200%", square, 240, icons.Key{Name: "/marks/logo.png", W: 48, H: 48}},
		{"unset scale reads as 100%", square, 0, icons.Key{Name: "/marks/logo.png", W: 24, H: 24}},
	} {
		got, ok := pluginImageKey(tc.node, tc.scale)
		if !ok || got != tc.want {
			t.Errorf("%s: key = %+v, %v; want %+v", tc.name, got, ok, tc.want)
		}
	}
	if _, ok := pluginImageKey(&ui.Node{Kind: ui.KindImage, ImagePath: "/x.png"}, 180); ok {
		t.Error("an image without a box produced a key")
	}
}

func TestFillPluginImagesMatchesTheScaledKey(t *testing.T) {
	n := &ui.Node{Kind: ui.KindImage, ImagePath: "/marks/wordmark.png", ImageW: 40, ImageH: 12}
	img := &ui.Image{Width: 60, Height: 18, Stride: 240, Pix: make([]byte, 60*18*4)}
	if fillPluginImages(n, icons.Key{Name: "/marks/wordmark.png", W: 40, H: 12}, img, 180) {
		t.Fatal("a 100% decode filled a node keyed at 150%")
	}
	if !fillPluginImages(n, icons.Key{Name: "/marks/wordmark.png", W: 60, H: 18}, img, 180) || n.Image != img {
		t.Fatal("the 150% decode did not fill its node")
	}
}

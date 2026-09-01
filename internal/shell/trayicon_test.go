package shell

import (
	"testing"

	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

func TestTrayIconComposesOverlayLastAtHalfSize(t *testing.T) {
	base := &tray.Pixmap{Width: 16, Height: 16, ARGB: make([]byte, 16*16*4)}
	overlay := &tray.Pixmap{Width: 8, Height: 8, ARGB: make([]byte, 8*8*4)}
	for i := range base.ARGB {
		base.ARGB[i] = 0x10
	}
	for i := range overlay.ARGB {
		overlay.ARGB[i] = 0xff
	}
	out := composeTrayIcon(base, overlay)
	if out == nil {
		t.Fatal("no composite")
	}
	if out.Width != 16 || out.Height != 16 {
		t.Fatalf("composite = %dx%d, want the base size", out.Width, out.Height)
	}
	// The overlay lands in the bottom-right quadrant at half size.
	px := pixelOf(out, 12, 12)
	if px[3] != 0xff {
		t.Fatalf("overlay pixel alpha = %d", px[3])
	}
	// The top-left stays the base.
	px = pixelOf(out, 2, 2)
	if px[3] != 0x10 {
		t.Fatalf("base pixel = %v", px)
	}
}

func TestTrayIconWithoutAnOverlayIsTheBase(t *testing.T) {
	base := &tray.Pixmap{Width: 8, Height: 8, ARGB: make([]byte, 8*8*4)}
	out := composeTrayIcon(base, nil)
	if out == nil || out.Width != 8 {
		t.Fatalf("base-only composite = %+v", out)
	}
}

func TestTrayIconRejectsAMalformedBase(t *testing.T) {
	if out := composeTrayIcon(&tray.Pixmap{Width: 4, Height: 4, ARGB: []byte{1}}, nil); out != nil {
		t.Fatal("a short ARGB buffer composed")
	}
}

func pixelOf(img interface {
	PixAt(x, y int) [4]byte
}, x, y int) [4]byte {
	return img.PixAt(x, y)
}

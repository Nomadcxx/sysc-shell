package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// A small panel with text and a progress bar, so a render has both ink and
// structure to check. sampleTree is the same tree as wire JSON.
const sampleTree = `{"kind":"column","padding":16,"gap":8,"children":[
  {"kind":"text","text":"Pomodoro Timer","role":"title"},
  {"kind":"text","text":"Work session 2 of 4"},
  {"kind":"progress","value":0.4,"name":"session","role":"progressbar"}
]}`

func sampleNode() *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Padding: 16, Gap: 8, Children: []*v1.Node{
		{Kind: v1.KindText, Text: "Pomodoro Timer", Role: "title"},
		{Kind: v1.KindText, Text: "Work session 2 of 4"},
		{Kind: v1.KindProgress, Value: 0.4, Name: "session", Role: "progressbar"},
	}}
}

func TestRenderPanelHasTheRequestedPhysicalSize(t *testing.T) {
	img, err := renderPanel(sampleNode(), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	// 180 in 120ths is 150%: 360x240 logical is 540x360 physical.
	if got := img.Bounds(); got != image.Rect(0, 0, 540, 360) {
		t.Fatalf("bounds = %v, want 540x360", got)
	}
}

func TestRenderPanelIsOpaqueInsideWithTransparentCorners(t *testing.T) {
	img, err := renderPanel(sampleNode(), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want 0 (outside the rounded surface)", a)
	}
	if a := img.NRGBAAt(270, 300).A; a != 0xff {
		t.Errorf("body alpha = %d, want 0xff (an opaque panel)", a)
	}
}

func TestRenderPanelPaintsMoreThanAFlatFill(t *testing.T) {
	img, err := renderPanel(sampleNode(), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[color.NRGBA]bool{}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			seen[img.NRGBAAt(x, y)] = true
		}
	}
	if len(seen) < 8 {
		t.Fatalf("only %d distinct colours; the tree should paint text and a bar", len(seen))
	}
}

func TestRenderPanelRefusesATreeThatCannotFit(t *testing.T) {
	// An 8-padded row only 28 tall leaves 12 px for a 20 px icon: the host
	// refuses this view, and so must the preview.
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindRow, Height: 28, Padding: 8, Children: []*v1.Node{
			{Kind: v1.KindIcon, Icon: "battery_full", IconSize: 20},
		}},
	}}
	_, err := renderPanel(root, 290, 200, 180, "")
	if err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("err = %v, want the host's does-not-fit refusal", err)
	}
}

func TestRenderPanelRefusesAnInvalidTree(t *testing.T) {
	// A panel's root must be a column; a text root is invalid.
	if _, err := renderPanel(&v1.Node{Kind: v1.KindText, Text: "x"}, 200, 100, 180, ""); err == nil {
		t.Fatal("a text root rendered; want the host's refusal")
	}
}

func TestRenderPanelRejectsSizesAndScalesOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name        string
		w, h, scale int
		want        string
	}{
		{"zero width", 0, 100, 180, "outside 1.."},
		{"zero height", 200, 0, 180, "outside 1.."},
		{"negative width", -5, 100, 180, "outside 1.."},
		{"width past the cap", 9000, 100, 180, "outside 1.."},
		{"height past the cap", 200, 9000, 180, "outside 1.."},
		{"zero scale", 200, 100, 0, "scale"},
		{"negative scale", 200, 100, -5, "scale"},
		{"scale past 400%", 200, 100, 600, "scale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := renderPanel(sampleNode(), tc.w, tc.h, tc.scale, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// writeSolidPNG writes a size-by-size PNG of one colour and returns its path.
func writeSolidPNG(t *testing.T, size int, c color.NRGBA) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	path := filepath.Join(t.TempDir(), "solid.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func imagePanel(path string) *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Padding: 16, Children: []*v1.Node{
		{Kind: v1.KindImage, Path: path, ImageSize: 96},
	}}
}

// The host decodes image nodes through its icon worker; a preview that skipped
// that would paint every picture as empty panel and report nothing.
func TestRenderPanelPaintsAnImageNode(t *testing.T) {
	red := color.NRGBA{R: 0xe0, G: 0x20, B: 0x20, A: 0xff}
	img, err := renderPanel(imagePanel(writeSolidPNG(t, 64, red)), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	// The 96 px box sits at (16,16) logical; at 150% its centre is (96,96).
	got := img.NRGBAAt(96, 96)
	if got.R < 0xb0 || got.G > 0x60 || got.B > 0x60 {
		t.Fatalf("image box centre = %v, want the image's red (an empty box paints the panel colour)", got)
	}
}

func TestRenderPanelRefusesAnImageItCannotDecode(t *testing.T) {
	notAnImage := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notAnImage, []byte("this is not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"missing file": filepath.Join(t.TempDir(), "gone.png"),
		"not an image": notAnImage,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := renderPanel(imagePanel(path), 360, 240, 180, "")
			if err == nil || !strings.Contains(err.Error(), "could not be decoded") || !strings.Contains(err.Error(), path) {
				t.Fatalf("err = %v, want one saying %s could not be decoded", err, path)
			}
		})
	}
}

// The edge caps alone still allow an 8192x8192 panel at 400%: gigabytes. The
// area is what bounds memory.
func TestRenderPanelRejectsAnAreaPastTheCap(t *testing.T) {
	for _, tc := range []struct{ w, h, scale int }{
		{8192, 8192, 180},
		{8192, 8192, 480},
		{4000, 4000, 180},
	} {
		_, err := renderPanel(sampleNode(), tc.w, tc.h, tc.scale, "")
		if err == nil || !strings.Contains(err.Error(), "pixels") {
			t.Errorf("%dx%d at %d: err = %v, want a pixel-area refusal", tc.w, tc.h, tc.scale, err)
		}
	}
}

package main

import (
	"image"
	"image/color"
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

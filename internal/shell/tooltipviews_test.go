package shell

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/lint"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// The timer and notes tooltips as sysc-plugins sends them, and weather's own,
// painted through the host at the laptop's 1.25 onto a PNG each. The test
// asserts the cap, the inset and the shrink-wrap; the PNGs are for the eye.
// SYSC_TOOLTIP_PNG_DIR keeps them somewhere other than a temp directory.
func TestPluginAndWeatherTooltipCardsRenderInsideTheirCap(t *testing.T) {
	t.Parallel()
	wind, humidity := 4.0, 40.0
	weather := weatherTooltipTree(services.Reading{
		Observed: true, Code: 2, Temperature: 18, WindSpeed: &wind, Humidity: &humidity,
		FetchedAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.Local),
	})
	convert := func(n *v1.Node) *ui.Node {
		root, err := plugin.Convert(n, v1.ViewTooltip)
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	cases := []struct {
		name string
		root *ui.Node
		text string
	}{
		{"timer", convert(&v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "Timer 24:13 · running"},
		}}), ""},
		{"notes", convert(&v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "Notes", Bold: true, Size: "label"},
			{Kind: v1.KindText, Text: "Open your Markdown library", Tone: v1.ToneSubtle},
		}}), ""},
		{"weather", weather, ""},
		{"volume", nil, "Volume 40%"},
	}
	dir := os.Getenv("SYSC_TOOLTIP_PNG_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, h, hh := newTooltipFixture(t, true)
			h.show(tooltipRequest{Global: 1, Anchor: ui.Rect{X: 700, W: 30, H: 38}, Text: c.text, Root: c.root})
			spec := onlyOpen(t, hh)
			if spec.Width > lint.TooltipWidth || spec.Height > lint.TooltipHeight {
				t.Fatalf("card %dx%d exceeds the %dx%d cap", spec.Width, spec.Height, lint.TooltipWidth, lint.TooltipHeight)
			}
			h.r.mu.Lock()
			widest := widestLine(h.open.card, h.measure(h.r.panelThemeFor(1), ui.Scale120(150)))
			h.r.mu.Unlock()
			if int(spec.Width) > widest+theme.MarginM+1 {
				t.Fatalf("card width %d, want its widest line %d plus the inset", spec.Width, widest)
			}

			const scale120 = 150
			s := ui.Scale120(scale120)
			if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), scale120); err != nil {
				t.Fatal(err)
			}
			w, hgt := s.Physical(int(spec.Width)), s.Physical(int(spec.Height))
			pix := make([]byte, w*hgt*4)
			if err := spec.Callbacks.Render(pix, w, hgt, w*4); err != nil {
				t.Fatal(err)
			}
			assertInsetClear(t, pix, w, hgt, s)
			writeCardPNG(t, filepath.Join(dir, "tooltip-"+c.name+".png"), pix, w, hgt)
		})
	}
}

// writeCardPNG composites the premultiplied BGRA card over a flat mid-tone,
// so the translucent ground reads as it would over a wallpaper.
func writeCardPNG(t *testing.T, path string, pix []byte, w, h int) {
	t.Helper()
	const pad = 12
	img := image.NewRGBA(image.Rect(0, 0, w+2*pad, h+2*pad))
	bg := color.RGBA{R: 0x4a, G: 0x6a, B: 0x8a, A: 0xff}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, bg.A
	}
	for y := range h {
		for x := range w {
			i := (y*w + x) * 4
			b, g, r, a := int(pix[i]), int(pix[i+1]), int(pix[i+2]), int(pix[i+3])
			o := img.PixOffset(x+pad, y+pad)
			img.Pix[o] = uint8(r + int(bg.R)*(255-a)/255)
			img.Pix[o+1] = uint8(g + int(bg.G)*(255-a)/255)
			img.Pix[o+2] = uint8(b + int(bg.B)*(255-a)/255)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

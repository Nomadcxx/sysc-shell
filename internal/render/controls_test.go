package render

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestM3ControlPixels(t *testing.T) {
	for _, mode := range compositionModes() {
		t.Run(mode.name, func(t *testing.T) {
			style := mode.style
			for _, on := range []bool{false, true} {
				for _, pressed := range []bool{false, true} {
					n := &ui.Node{Kind: ui.KindToggle, Bounds: ui.Rect{X: 10, Y: 10, W: 52, H: 32}}
					if on {
						n.Value = 1
					}
					if pressed {
						n.State = ui.StatePressed
					}
					c := newTestCanvas(t, 80, 60)
					paintToggle(c, n, style)
					fill, ink := style.containerHighest(), style.outline()
					cx, diameter := 26, 16
					if on {
						fill, ink = style.accent(), style.onAccent()
						cx, diameter = 46, 24
					}
					if pressed {
						diameter = 28
						if on {
							ink = resolvePaintRole(style, ui.PaintPrimaryContainer)
						} else {
							ink = style.Track
						}
					}
					tx := 47
					if on {
						tx = 20
					}
					if got := pixelAt(t, c, tx, 26); got != fill {
						t.Errorf("on=%v pressed=%v track %v want %v", on, pressed, got, fill)
					}
					if got := pixelAt(t, c, cx, 26); got != ink {
						t.Errorf("handle %v want %v", got, ink)
					}
					if got := pixelAt(t, c, cx, 26-diameter/2+1); got != ink {
						t.Errorf("handle diameter below %d", diameter)
					}
					if pixelAt(t, c, 10, 10).A != 0 {
						t.Error("switch corner is not fully round")
					}
					if !on && pixelAt(t, c, 36, 10) != style.outline() {
						t.Error("off switch lost Outline stroke")
					}
				}
			}
			for _, v := range []float64{0, 50, 100} {
				n := &ui.Node{Kind: ui.KindSlider, Bounds: ui.Rect{X: 10, Y: 10, W: 200, H: 44}, Min: 0, Max: 100, Value: v}
				c := newTestCanvas(t, 240, 70)
				paintSlider(c, n, style)
				cx := 12 + int(196*v/100)
				if got := pixelAt(t, c, cx, 11); got != style.accent() {
					t.Errorf("slider %v handle is not 44 high: %v", v, got)
				}
				if v == 50 {
					if got := pixelAt(t, c, 30, 32); got != style.accent() {
						t.Errorf("active track %v", got)
					}
					if got := pixelAt(t, c, 190, 32); got != style.Roles[ui.PaintSecondaryContainer] {
						t.Errorf("inactive track %v", got)
					}
					if pixelAt(t, c, cx-5, 32).A != 0 || pixelAt(t, c, cx+5, 32).A != 0 {
						t.Error("slider has no handle gaps")
					}
				}
			}
			meter := &ui.Node{Kind: ui.KindMeter, Bounds: ui.Rect{X: 10, Y: 10, W: 200, H: 8}, Value: 0.5}
			c := newTestCanvas(t, 240, 30)
			if err := paintNode(c, meter, nil, style, style.Size); err != nil {
				t.Fatal(err)
			}
			if pixelAt(t, c, 110, 14).A != 0 {
				t.Error("meter has no active-track gap")
			}
			if got := pixelAt(t, c, 160, 14); got != style.Roles[ui.PaintSecondaryContainer] {
				t.Errorf("meter remainder %v", got)
			}
			if pixelAt(t, c, 10, 10).A != 0 {
				t.Error("meter has square ends")
			}
		})
	}
}

func TestM3ControlContactSheet(t *testing.T) {
	dir := os.Getenv("SYSC_CONTROLS_PNG_DIR")
	if dir == "" {
		t.Skip("set SYSC_CONTROLS_PNG_DIR to save the controls")
	}
	for _, mode := range compositionModes() {
		for _, scale := range []ui.Scale120{120, 150} {
			style := mode.style
			style.Scale120 = scale
			c := newTestCanvas(t, scale.Physical(1100), scale.Physical(650))
			fillRect(c, ui.Rect{W: c.Width, H: c.Height}, style.Background)
			for row, density := range theme.Densities() {
				style.Metrics, _ = theme.MetricsFor(density)
				y := row * 130
				// Columns: off/on, pressed off/on, disabled off/on, then 0/50/100 sliders.
				for col := 0; col < 6; col++ {
					n := &ui.Node{Kind: ui.KindToggle, Bounds: ui.Rect{X: 15 + col*95, Y: y + 20, W: 80, H: 60}}
					if col%2 == 1 {
						n.Value = 1
					}
					if col/2 == 1 {
						n.State = ui.StatePressed
					}
					if col/2 == 2 {
						n.State = ui.StateDisabled
					}
					paintToggle(c, n, style)
				}
				for col, v := range []float64{0, 50, 100} {
					n := &ui.Node{Kind: ui.KindSlider, Min: 0, Max: 100, Value: v, Bounds: ui.Rect{X: 600 + col*165, Y: y + 10, W: 145, H: 76}}
					paintSlider(c, n, style)
					n.Kind = ui.KindMeter
					n.Value = v / 100
					n.Bounds.Y = y + 100
					n.Bounds.H = 8
					if err := paintNode(c, n, nil, style, style.Size); err != nil {
						t.Fatal(err)
					}
				}
			}
			img := image.NewNRGBA(image.Rect(0, 0, c.Width, c.Height))
			for y := 0; y < c.Height; y++ {
				for x := 0; x < c.Width; x++ {
					p := pixelAt(t, c, x, y)
					img.SetNRGBA(x, y, color.NRGBA{R: p.R, G: p.G, B: p.B, A: p.A})
				}
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, fmt.Sprintf("controls-%s-%d.png", mode.name, scale))
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, img)
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
}

func TestDisabledSwitchTrackUsesTokenOpacity(t *testing.T) {
	style := testStyle
	n := &ui.Node{Kind: ui.KindToggle, State: ui.StateDisabled, Bounds: ui.Rect{X: 10, Y: 10, W: 52, H: 32}}
	c := newTestCanvas(t, 80, 60)
	fillRect(c, ui.Rect{W: 80, H: 60}, style.Background)
	paintToggle(c, n, style)
	want := overlay(style.Background, style.containerHighest(), 0.12)
	if got := pixelAt(t, c, 47, 26); !nearlyEqualColor(got, want, 2) {
		t.Fatalf("disabled off track %v want 12%% composite %v", got, want)
	}
}

func TestCompactPressedSliderKeepsTwoPixelHandle(t *testing.T) {
	style := testStyle
	style.Metrics, _ = theme.MetricsFor(theme.DensityCompact)
	n := &ui.Node{Kind: ui.KindSlider, Min: 0, Max: 100, Value: 50, State: ui.StatePressed, Bounds: ui.Rect{X: 10, Y: 10, W: 200, H: 44}}
	c := newTestCanvas(t, 240, 70)
	paintSlider(c, n, style)
	if pixelAt(t, c, 109, 32).A == 0 || pixelAt(t, c, 110, 32).A == 0 {
		t.Fatal("compact pressed handle is narrower than the scaled 2px token")
	}
}

func TestMeterOwnsFixedThicknessAcrossDensities(t *testing.T) {
	for _, density := range theme.Densities() {
		t.Run(string(density), func(t *testing.T) {
			style := compositionModes()[0].style
			style.Metrics, _ = theme.MetricsFor(density)
			for _, scale := range []ui.Scale120{120, 150} {
				style.Scale120 = scale
				n := &ui.Node{Kind: ui.KindMeter, Width: 100, Value: .5}
				row := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}
				if err := ui.Layout(row, ui.Rect{W: 100, H: 20}, func(string, ui.TextAttrs) (int, int) { return 8, 16 }); err != nil {
					t.Fatal(err)
				}
				c := newTestCanvas(t, scale.Physical(100), scale.Physical(20))
				paintMeter(c, n, style)
				painted := 0
				for y := 0; y < c.Height; y++ {
					if pixelAt(t, c, scale.Physical(20), y).A > 0 {
						painted++
					}
				}
				if painted != scale.Physical(ui.MeterHeight) {
					t.Errorf("%s scale %d: painted %d, want %d", density, scale, painted, scale.Physical(ui.MeterHeight))
				}
				column := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{n}}
				if err := ui.LayoutColumn(column, ui.Rect{W: 100, H: 20}, func(string, ui.TextAttrs) (int, int) { return 8, 16 }); err != nil {
					t.Fatal(err)
				}
				if n.Bounds.H != ui.MeterHeight {
					t.Errorf("column height %d", n.Bounds.H)
				}
			}
		})
	}
}

func TestSwitchReservedBoxAndVisualSize(t *testing.T) {
	style := compositionModes()[0].style
	style.Metrics, _ = theme.MetricsFor(theme.DensityDefault)
	for _, tc := range []struct {
		name                               string
		w, h, boxW, boxH, visualW, visualH int
	}{
		{"explicit target", 104, 64, 104, 64, 52, 32},
		{"inferred", 0, 0, 52, 32, 52, 32},
		{"constrained", 26, 16, 26, 16, 26, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &ui.Node{Kind: ui.KindToggle, Width: tc.w, Height: tc.h}
			ui.SizeControls(n, style.Metrics)
			if err := ui.Layout(&ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, ui.Rect{W: tc.boxW, H: tc.boxH}, func(string, ui.TextAttrs) (int, int) { return 8, 16 }); err != nil {
				t.Fatal(err)
			}
			if n.Bounds.W != tc.boxW || n.Bounds.H != tc.boxH || n.Width != tc.w || n.Height != tc.h {
				t.Fatalf("reserved/declared size changed: %+v", n.Bounds)
			}
			c := newTestCanvas(t, tc.boxW, tc.boxH)
			paintToggle(c, n, style)
			left, top, right, bottom := c.Width, c.Height, 0, 0
			for y := 0; y < c.Height; y++ {
				for x := 0; x < c.Width; x++ {
					if pixelAt(t, c, x, y).A > 0 {
						left = min(left, x)
						top = min(top, y)
						right = max(right, x+1)
						bottom = max(bottom, y+1)
					}
				}
			}
			if right-left != tc.visualW || bottom-top != tc.visualH || left != (tc.boxW-tc.visualW)/2 || top != (tc.boxH-tc.visualH)/2 {
				t.Errorf("visual (%d,%d)-(%d,%d), want centred %dx%d", left, top, right, bottom, tc.visualW, tc.visualH)
			}
		})
	}
}

func TestMeterEndpointsGapStopAndSemanticTones(t *testing.T) {
	for _, mode := range compositionModes() {
		for _, density := range theme.Densities() {
			for _, scale := range []ui.Scale120{120, 150} {
				style := mode.style
				style.Scale120 = scale
				style.Metrics, _ = theme.MetricsFor(density)
				for _, tone := range []ui.Tone{ui.ToneNormal, ui.ToneWarning, ui.ToneError} {
					for _, value := range []float64{0, .5, 1} {
						n := &ui.Node{Kind: ui.KindMeter, Tone: tone, Value: value, Bounds: ui.Rect{X: 10, Y: 10, W: 200, H: 8}}
						c := newTestCanvas(t, scale.Physical(240), scale.Physical(30))
						paintMeter(c, n, style)
						active := toneColor(style, tone, style.accent())
						if got := pixelAt(t, c, scale.Physical(208), scale.Physical(14)); !nearlyEqualColor(got, active, 2) {
							t.Errorf("%s %s scale %d tone %d value %v stop=%v want %v", mode.name, density, scale, tone, value, got, active)
						}
						if value == .5 && pixelAt(t, c, scale.Physical(111), scale.Physical(14)).A != 0 {
							t.Error("meter gap missing")
						}
					}
				}
			}
		}
	}
}

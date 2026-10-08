package main

import (
	"fmt"
	"image"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	// defaultScale120 is 150%, a common laptop display scale.
	defaultScale120 = 180
	// maxLogical and maxScale120 bound the buffer a caller can ask for: 8192
	// logical pixels at 400% is a 32768 px edge, already far past any panel.
	maxLogical  = 8192
	maxScale120 = 480
)

// renderPanel lays out and paints root as a panel of width by height logical
// pixels at scale120 and returns the pixels. family names the font family;
// empty means the shell's default.
func renderPanel(root *v1.Node, width, height, scale120 int, family string) (*image.NRGBA, error) {
	if width < 1 || height < 1 || width > maxLogical || height > maxLogical {
		return nil, fmt.Errorf("size %dx%d is outside 1..%d", width, height, maxLogical)
	}
	scale := ui.Scale120(scale120)
	if !scale.Valid() || scale120 > maxScale120 {
		return nil, fmt.Errorf("scale %d is outside 1..%d (120 is 100%%)", scale120, maxScale120)
	}

	tree, err := plugin.Convert(root, v1.ViewPanel)
	if err != nil {
		return nil, err
	}

	fonts, err := render.NewSystemFontMap(family, render.DefaultFontCacheDir())
	if err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}
	text := render.NewTextRendererWithFontMap(fonts)

	theme := shell.DefaultTheme()
	style := theme.PanelStyle()
	style.Rim = theme.Outline
	style.SurfaceOpacity = 0xff
	style.Scale120 = scale
	body := ui.Rect{W: width, H: height}
	style.Body = body

	measure := func(s string, attrs ui.TextAttrs) (int, int) {
		spec := render.SpecFor(style, attrs)
		if spec.Size > 0 {
			if mw, mh, err := text.Measure(s, spec, attrs.Tabular); err == nil {
				return scale.Logical(mw), scale.Logical(mh)
			}
		}
		return len(s) * 8, 16
	}
	if tree.Kind == ui.KindRow {
		err = ui.Layout(tree, body, measure)
	} else {
		err = ui.LayoutColumn(tree, body, measure)
	}
	if err != nil {
		return nil, err
	}

	pw, ph := scale.Physical(width), scale.Physical(height)
	pix := make([]byte, pw*ph*4)
	canvas, err := render.NewCanvas(pix, pw, ph, pw*4)
	if err != nil {
		return nil, err
	}
	if err := render.Paint(canvas, tree, text, style); err != nil {
		return nil, err
	}
	return fromBGRA(pix, pw, ph), nil
}

// fromBGRA converts the painter's premultiplied BGRA bytes to straight RGBA.
func fromBGRA(pix []byte, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		b, g, r, a := int(pix[i*4]), int(pix[i*4+1]), int(pix[i*4+2]), int(pix[i*4+3])
		if a == 0 {
			continue
		}
		o := i * 4
		img.Pix[o] = uint8(min(255, r*255/a))
		img.Pix[o+1] = uint8(min(255, g*255/a))
		img.Pix[o+2] = uint8(min(255, b*255/a))
		img.Pix[o+3] = uint8(a)
	}
	return img
}

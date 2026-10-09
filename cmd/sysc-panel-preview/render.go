package main

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
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
	// maxPixels bounds the painted buffer, which is what memory actually
	// follows: 1<<25 pixels is 128 MiB. The largest panel any manifest declares
	// at 400% is about 6.6 million pixels, so real use is far inside it.
	maxPixels = 1 << 25
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

	pw, ph := scale.Physical(width), scale.Physical(height)
	if pixels := pw * ph; pixels > maxPixels {
		return nil, fmt.Errorf("a %dx%d panel at scale %d is %d pixels; the limit is %d", width, height, scale120, pixels, maxPixels)
	}

	tree, err := plugin.Convert(root, v1.ViewPanel)
	if err != nil {
		return nil, err
	}
	if err := decodeImages(tree); err != nil {
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

	pix := make([]byte, pw*ph*4)
	canvas, err := render.NewCanvas(pix, pw, ph, pw*4)
	if err != nil {
		return nil, err
	}
	if err := render.Paint(canvas, tree, text, style); err != nil {
		return nil, err
	}
	return unpremultiply(pix, pw, ph), nil
}

// decodeImages fills every image node with its decoded pixels, as the host's
// icon worker does for a live panel, and fails when one cannot be decoded: a
// screenshot with an empty box where a picture should be would mislead. Image
// nodes are rasters (the wire validator says so); each is decoded at its
// declared logical box, exactly as the host keys it.
func decodeImages(root *ui.Node) error {
	decoded := map[icons.Key]*ui.Image{}
	var walk func(n *ui.Node) error
	walk = func(n *ui.Node) error {
		if n.Kind == ui.KindImage && n.ImagePath != "" {
			w, h := n.ImageW, n.ImageH
			if w <= 0 || h <= 0 {
				w, h = n.ImageSize, n.ImageSize
			}
			key := icons.Key{Name: n.ImagePath, W: w, H: h}
			img, ok := decoded[key]
			if !ok {
				var err error
				if img, err = decodeImage(n.ImagePath, w, h); err != nil {
					return err
				}
				decoded[key] = img
			}
			n.Image = img
		}
		for _, child := range n.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func decodeImage(path string, w, h int) (*ui.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("image %s could not be decoded: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, icons.MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("image %s could not be decoded: %w", path, err)
	}
	if len(data) > icons.MaxFileBytes {
		return nil, fmt.Errorf("image %s could not be decoded: larger than %d bytes", path, icons.MaxFileBytes)
	}
	img := icons.DecodeRaster(data, w, h)
	if img == nil {
		return nil, errors.New("image " + path + " could not be decoded: not a readable raster")
	}
	return img, nil
}

// unpremultiply turns the painter's premultiplied BGRA bytes into a straight
// RGBA image in place, so one buffer serves both: a large panel is not copied.
func unpremultiply(pix []byte, w, h int) *image.NRGBA {
	for i := 0; i < w*h; i++ {
		o := i * 4
		b, g, r, a := int(pix[o]), int(pix[o+1]), int(pix[o+2]), int(pix[o+3])
		if a == 0 {
			continue
		}
		pix[o] = uint8(min(255, r*255/a))
		pix[o+1] = uint8(min(255, g*255/a))
		pix[o+2] = uint8(min(255, b*255/a))
	}
	return &image.NRGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}

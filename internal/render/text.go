// Package render shapes text and rasterises it into alpha coverage masks.
// All sizes are physical pixels: the caller applies the fractional scale
// (base * scale120 / 120) before shaping, and never upscales a mask.
package render

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

var fontCache sync.Map // [32]byte content hash -> *font.Font

// ParseFace parses OpenType font data, reusing the parsed font for identical
// data and returning a fresh face each call.
//
// go-text documents *font.Font as read-only and safe for concurrent use, and
// *font.Face as NOT safe: a face carries mutable cmap and extents caches. Only
// the font may be cached, so every caller gets its own face.
func ParseFace(data []byte) (*font.Face, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("render: empty font data")
	}

	key := sha256.Sum256(data)
	if cached, ok := fontCache.Load(key); ok {
		return font.NewFace(cached.(*font.Font)), nil
	}

	loader, err := ot.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("render: load font: %w", err)
	}
	parsed, err := font.NewFont(loader)
	if err != nil {
		return nil, fmt.Errorf("render: parse font: %w", err)
	}

	actual, _ := fontCache.LoadOrStore(key, parsed)
	return font.NewFace(actual.(*font.Font)), nil
}

// Mask is a rasterised run: alpha coverage plus the metrics needed to place it.
type Mask struct {
	// Alpha is the coverage image, sized to the run's advance and line box.
	Alpha *image.Alpha
	// Color is an optional premultiplied B,G,R,A raster of colour glyphs
	// (bitmap emoji). Same geometry as Alpha. Paint blits it without tinting.
	Color *ui.Image
	// Baseline is the distance in pixels from the mask's top edge to the baseline.
	Baseline int
	// Advance is the pen advance of the run in pixels. The mask is sized to
	// this advance, so it equals the alpha image width.
	Advance int
}

// TextRenderer shapes and rasterises single-script horizontal runs for one face.
//
// A renderer owns its face and shaper, neither of which is safe for concurrent
// use. Give every goroutine that draws its own renderer.
type TextRenderer struct {
	face    *font.Face
	fontMap *FontMap
	shaper  shaping.HarfbuzzShaper
	// material is this renderer's own face for the embedded icon subset,
	// parsed on first use. materialErr latches a parse failure so a broken
	// embed is reported once rather than retried every frame.
	material    *font.Face
	materialErr error
}

func NewTextRenderer(face *font.Face) *TextRenderer {
	return &TextRenderer{face: face}
}

// NewTextRendererWithFontMap builds a renderer that resolves a face for every
// rune and shapes adjacent runes with the same resolved face together.
func NewTextRendererWithFontMap(fontMap *FontMap) *TextRenderer {
	if fontMap == nil {
		return &TextRenderer{}
	}
	return &TextRenderer{face: fontMap.Primary(), fontMap: fontMap}
}

// tabularFigures is the OpenType feature that gives every digit the same
// advance. Applied per call, because only some runs want it.
var tabularFigures = []shaping.FontFeature{{Tag: ot.MustNewTag("tnum"), Value: 1}}

// Shape lays out one run at the given physical pixel size. When tabular is
// set, digits shape with equal advances.
func (r *TextRenderer) Shape(text string, size int, tabular bool) (shaping.Output, error) {
	if r == nil || r.face == nil {
		return shaping.Output{}, fmt.Errorf("render: nil face")
	}
	return r.shapeFace(r.face, text, size, tabular)
}

func (r *TextRenderer) shapeFace(face *font.Face, text string, size int, tabular bool) (shaping.Output, error) {
	if face == nil {
		return shaping.Output{}, fmt.Errorf("render: nil face")
	}
	if size <= 0 {
		return shaping.Output{}, fmt.Errorf("render: size %d is not positive", size)
	}

	runes := []rune(text)
	script := runScript(runes)
	input := shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: scriptDirection(script),
		Face:      face,
		Size:      fixed.I(size),
		Script:    script,
		Language:  language.NewLanguage("und"),
	}
	if tabular {
		input.FontFeatures = tabularFigures
	}
	if size <= 0xffff {
		face.SetPpem(uint16(size), uint16(size))
	}
	return r.shaper.Shape(input), nil
}

type shapedFaceRun struct {
	face      *font.Face
	text      string
	runeStart int
	output    shaping.Output
}

func (r *TextRenderer) shapeRuns(text string, size int, tabular bool) ([]shapedFaceRun, error) {
	if r == nil || r.face == nil {
		return nil, fmt.Errorf("render: nil face")
	}
	fontRuns := []Run{{Face: r.face, Text: text}}
	if r.fontMap != nil {
		fontRuns = r.fontMap.SplitRuns(text)
		if text == "" {
			fontRuns = []Run{{Face: r.face}}
		}
	}
	shaped := make([]shapedFaceRun, 0, len(fontRuns))
	runeStart := 0
	for _, run := range fontRuns {
		out, err := r.shapeFace(run.Face, run.Text, size, tabular)
		if err != nil {
			return nil, err
		}
		shaped = append(shaped, shapedFaceRun{
			face: run.Face, text: run.Text, runeStart: runeStart, output: out,
		})
		runeStart += len([]rune(run.Text))
	}
	return shaped, nil
}

func shapedMetrics(runs []shapedFaceRun) (advance fixed.Int26_6, width, height, baseline int) {
	var ascent, descent fixed.Int26_6
	for _, run := range runs {
		advance += run.output.Advance
		ascent = max(ascent, run.output.LineBounds.Ascent)
		descent = min(descent, run.output.LineBounds.Descent)
	}
	return advance, advance.Ceil(), ascent.Ceil() + (-descent).Ceil(), ascent.Ceil()
}

// Measure reports the advance width and line height of a run in pixels.
func (r *TextRenderer) Measure(text string, size int, tabular bool) (int, int, error) {
	runs, err := r.shapeRuns(text, size, tabular)
	if err != nil {
		return 0, 0, err
	}
	_, w, h, _ := shapedMetrics(runs)
	return w, h, nil
}

// Raster shapes a run and draws its glyphs into an alpha mask, plus a colour
// layer when the run contains bitmap emoji. The tabular flag must match the
// one measurement used, or the drawn run and the space reserved for it would
// disagree.
func (r *TextRenderer) Raster(text string, size int, tabular bool) (Mask, error) {
	runs, err := r.shapeRuns(text, size, tabular)
	if err != nil {
		return Mask{}, err
	}

	return rasterRuns(runs, size)
}

// rasterRuns draws already-shaped runs into an alpha mask. It is separate from
// Raster so a caller that shapes through one specific face -- an icon name
// through the Material subset -- reuses the same rasterisation.
func rasterRuns(runs []shapedFaceRun, size int) (Mask, error) {
	_, w, h, baseline := shapedMetrics(runs)
	if w <= 0 || h <= 0 {
		return Mask{}, fmt.Errorf("render: run measures %dx%d", w, h)
	}

	rast := vector.NewRasterizer(w, h)
	var colorImg *ui.Image

	lineX := fixed.Int26_6(0)
	for _, run := range runs {
		scale := float32(size) / float32(run.face.Upem())
		penX := fixed.Int26_6(0)
		for _, g := range run.output.Glyphs {
			originX := float32(lineX+penX+g.XOffset) / 64
			originY := float32(baseline) - float32(g.YOffset)/64
			if bm, ok := run.face.GlyphDataBitmap(g.GlyphID); ok {
				if colorImg == nil {
					colorImg = &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
				}
				_ = blitGlyphBitmap(colorImg, bm, g, lineX+penX, baseline)
				penX += g.Advance
				continue
			}
			outline, err := glyphOutline(run.face, g.GlyphID)
			if err != nil {
				return Mask{}, err
			}
			addOutline(rast, outline, originX, originY, scale)
			penX += g.Advance
		}
		lineX += run.output.Advance
	}

	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	return Mask{Alpha: mask, Color: colorImg, Baseline: baseline, Advance: w}, nil
}

// runBox reports the run's advance width, line height, and baseline offset,
// all rounded outward to whole pixels.
func runBox(out shaping.Output) (width, height, baseline int) {
	ascent := out.LineBounds.Ascent.Ceil()
	descent := (-out.LineBounds.Descent).Ceil()
	return out.Advance.Ceil(), ascent + descent, ascent
}

// glyphOutline returns the vector outline for a glyph.
func glyphOutline(face *font.Face, gid font.GID) (font.GlyphOutline, error) {
	outline, ok := face.GlyphDataOutline(gid)
	if !ok {
		return font.GlyphOutline{}, fmt.Errorf("render: glyph %d has no vector outline", gid)
	}
	return outline, nil
}

// outlineFrom classifies glyph data. Vector outlines still rasterise into the
// alpha mask. Bitmap glyphs are painted separately by blitGlyphBitmap.
func outlineFrom(data font.GlyphData, gid font.GID) (font.GlyphOutline, error) {
	switch data := data.(type) {
	case font.GlyphOutline:
		return data, nil
	case nil:
		return font.GlyphOutline{}, fmt.Errorf("render: glyph %d has no data", gid)
	default:
		return font.GlyphOutline{}, fmt.Errorf("render: glyph %d has unsupported %T data", gid, data)
	}
}

func blitGlyphBitmap(dst *ui.Image, bm font.GlyphBitmap, g shaping.Glyph, pen fixed.Int26_6, baseline int) error {
	src, err := decodeGlyphBitmap(bm)
	if err != nil {
		return err
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw <= 0 || sh <= 0 {
		return fmt.Errorf("render: empty bitmap")
	}
	dw := g.Width.Round()
	dh := (-g.Height).Round()
	if dw <= 0 {
		dw = sw
	}
	if dh <= 0 {
		dh = sh
	}
	left := (pen + g.XOffset + g.XBearing).Round()
	top := baseline - (g.YOffset + g.YBearing).Round()
	for y := 0; y < dh; y++ {
		dy := top + y
		if dy < 0 || dy >= dst.Height {
			continue
		}
		sy := sb.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			dx := left + x
			if dx < 0 || dx >= dst.Width {
				continue
			}
			px := color.NRGBAModel.Convert(src.At(sb.Min.X+x*sw/dw, sy)).(color.NRGBA)
			if px.A == 0 {
				continue
			}
			a := uint32(px.A)
			off := dy*dst.Stride + dx*4
			dst.Pix[off+0] = byte(uint32(px.B) * a / 255)
			dst.Pix[off+1] = byte(uint32(px.G) * a / 255)
			dst.Pix[off+2] = byte(uint32(px.R) * a / 255)
			dst.Pix[off+3] = px.A
		}
	}
	return nil
}

func decodeGlyphBitmap(bm font.GlyphBitmap) (image.Image, error) {
	switch bm.Format {
	case font.PNG:
		return png.Decode(bytes.NewReader(bm.Data))
	default:
		// ponytail: CBDT PNG is what Noto Color Emoji ships; COLR/SVG/raw bitmaps stay tofu.
		return nil, fmt.Errorf("render: bitmap format %d", bm.Format)
	}
}

// addOutline appends a glyph's segments to the rasterizer. Font units grow up,
// so Y is negated; the origin is the glyph's pen position on the baseline.
func addOutline(rast *vector.Rasterizer, outline font.GlyphOutline, originX, originY, scale float32) {
	if len(outline.Segments) == 0 {
		return // a blank glyph such as a space
	}
	px := func(p ot.SegmentPoint) (float32, float32) {
		return originX + p.X*scale, originY - p.Y*scale
	}
	for _, seg := range outline.Segments {
		switch seg.Op {
		case ot.SegmentOpMoveTo:
			rast.MoveTo(px(seg.Args[0]))
		case ot.SegmentOpLineTo:
			rast.LineTo(px(seg.Args[0]))
		case ot.SegmentOpQuadTo:
			x1, y1 := px(seg.Args[0])
			x2, y2 := px(seg.Args[1])
			rast.QuadTo(x1, y1, x2, y2)
		case ot.SegmentOpCubeTo:
			x1, y1 := px(seg.Args[0])
			x2, y2 := px(seg.Args[1])
			x3, y3 := px(seg.Args[2])
			rast.CubeTo(x1, y1, x2, y2, x3, y3)
		}
	}
	rast.ClosePath()
}

// runScript returns the script of the first strong rune in the run.
func runScript(runes []rune) language.Script {
	for _, r := range runes {
		if s := language.LookupScript(r); s.Strong() {
			return s
		}
	}
	return language.Latin
}

// scriptDirection maps a script to its horizontal direction. The proof
// qualifies Latin and Arabic; paragraph-level bidi is a later milestone.
func scriptDirection(s language.Script) di.Direction {
	switch s {
	case language.Arabic, language.Hebrew:
		return di.DirectionRTL
	default:
		return di.DirectionLTR
	}
}

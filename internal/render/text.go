// Package render shapes text and rasterises it into alpha coverage masks.
// All sizes are physical pixels: the caller applies the fractional scale
// (base * scale120 / 120) before shaping, and never upscales a mask.
package render

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"sync"

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

// Raster shapes a run and draws its glyphs into an alpha mask. The tabular
// flag must match the one measurement used, or the drawn run and the space
// reserved for it would disagree.
func (r *TextRenderer) Raster(text string, size int, tabular bool) (Mask, error) {
	runs, err := r.shapeRuns(text, size, tabular)
	if err != nil {
		return Mask{}, err
	}

	_, w, h, baseline := shapedMetrics(runs)
	if w <= 0 || h <= 0 {
		return Mask{}, fmt.Errorf("render: run measures %dx%d", w, h)
	}

	rast := vector.NewRasterizer(w, h)

	lineX := fixed.Int26_6(0)
	for _, run := range runs {
		scale := float32(size) / float32(run.face.Upem())
		penX := fixed.Int26_6(0)
		for _, g := range run.output.Glyphs {
			outline, err := glyphOutline(run.face, g.GlyphID)
			if err != nil {
				return Mask{}, err
			}
			originX := float32(lineX+penX+g.XOffset) / 64
			originY := float32(baseline) - float32(g.YOffset)/64
			addOutline(rast, outline, originX, originY, scale)
			penX += g.Advance
		}
		lineX += run.output.Advance
	}

	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	return Mask{Alpha: mask, Baseline: baseline, Advance: w}, nil
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

// outlineFrom classifies glyph data. Only vector outlines can be rasterised
// into a shared-memory ARGB buffer, so bitmap, SVG and colour glyphs are
// refused rather than silently drawn as blanks.
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

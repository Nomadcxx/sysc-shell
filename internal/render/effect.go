package render

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A weather effect is a bounded card-sized raster. Keeping both dimensions
// and total mask pixels capped prevents malformed retained data from turning
// RoundedMask into an unbounded allocation request.
const (
	maxEffectDimension = 4096
	maxEffectPixels    = maxEffectDimension * maxEffectDimension
)

// paintEffect paints one validated host-owned effect into the node's arranged
// physical bounds. Effects are deliberately dispatched here rather than
// through a renderer interface: the host owns one small catalogue today.
func paintEffect(c *Canvas, n *ui.Node, style Style) error {
	switch {
	case c == nil:
		return fmt.Errorf("render: nil canvas")
	case n == nil:
		return fmt.Errorf("render: nil effect node")
	case n.Kind != ui.KindEffect:
		return fmt.Errorf("render: node kind %d is not an effect", n.Kind)
	case !style.Scale120.Valid():
		return fmt.Errorf("render: effect scale120 %d is not positive", style.Scale120)
	}
	if err := n.Effect.Validate(); err != nil {
		return fmt.Errorf("render: invalid effect: %w", err)
	}

	box, err := checkedEffectBounds(style.Scale120, n.Bounds)
	if err != nil {
		return err
	}
	radius := chromeRadius(style, nodeRadius(style, n, style.Radius), box)
	mask := RoundedMask(radius, box.W, box.H)
	switch n.Effect.Program {
	case ui.EffectWeather:
		return paintWeatherEffect(c, box, mask, style, n.Effect, n.EffectPhase)
	default:
		// EffectSpec.Validate currently rejects this arm. Keep the dispatch
		// closed so adding a descriptor cannot silently select a weather state.
		return fmt.Errorf("render: unsupported effect program %d", n.Effect.Program)
	}
}

func checkedEffectBounds(scale ui.Scale120, logical ui.Rect) (ui.Rect, error) {
	if logical.W <= 0 || logical.H <= 0 {
		return ui.Rect{}, fmt.Errorf("render: effect bounds %dx%d are not positive", logical.W, logical.H)
	}
	logicalRight, ok := checkedEffectAdd(logical.X, logical.W)
	if !ok {
		return ui.Rect{}, fmt.Errorf("render: effect horizontal bounds overflow")
	}
	logicalBottom, ok := checkedEffectAdd(logical.Y, logical.H)
	if !ok {
		return ui.Rect{}, fmt.Errorf("render: effect vertical bounds overflow")
	}

	x0, err := checkedPhysicalEffectEdge(scale, logical.X)
	if err != nil {
		return ui.Rect{}, err
	}
	x1, err := checkedPhysicalEffectEdge(scale, logicalRight)
	if err != nil {
		return ui.Rect{}, err
	}
	y0, err := checkedPhysicalEffectEdge(scale, logical.Y)
	if err != nil {
		return ui.Rect{}, err
	}
	y1, err := checkedPhysicalEffectEdge(scale, logicalBottom)
	if err != nil {
		return ui.Rect{}, err
	}
	w, ok := checkedEffectSpan(x0, x1)
	if !ok || w <= 0 {
		return ui.Rect{}, fmt.Errorf("render: effect physical width is invalid")
	}
	h, ok := checkedEffectSpan(y0, y1)
	if !ok || h <= 0 {
		return ui.Rect{}, fmt.Errorf("render: effect physical height is invalid")
	}
	if w > maxEffectDimension || h > maxEffectDimension || w > maxEffectPixels/h {
		return ui.Rect{}, fmt.Errorf("render: effect physical bounds %dx%d are too large", w, h)
	}
	return ui.Rect{X: x0, Y: y0, W: w, H: h}, nil
}

func checkedPhysicalEffectEdge(scale ui.Scale120, logical int) (int, error) {
	s := int(scale)
	if s <= 0 {
		return 0, fmt.Errorf("render: effect scale120 %d is not positive", scale)
	}
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if logical > 0 && logical > (maxInt-60)/s {
		return 0, fmt.Errorf("render: effect coordinate %d overflows physical conversion", logical)
	}
	if logical < 0 && logical < (minInt+60)/s {
		return 0, fmt.Errorf("render: effect coordinate %d overflows physical conversion", logical)
	}
	product := logical * s
	if logical < 0 {
		return (product - 60) / 120, nil
	}
	return (product + 60) / 120, nil
}

func checkedEffectAdd(a, b int) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if b > 0 && a > maxInt-b {
		return 0, false
	}
	if b < 0 && a < minInt-b {
		return 0, false
	}
	return a + b, true
}

func checkedEffectSpan(lo, hi int) (int, bool) {
	if hi < lo {
		return 0, false
	}
	if lo < 0 {
		maxInt := int(^uint(0) >> 1)
		if hi > maxInt+lo {
			return 0, false
		}
	}
	return hi - lo, true
}

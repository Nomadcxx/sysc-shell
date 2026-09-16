package render

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
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

	box := style.Scale120.PhysicalRect(n.Bounds)
	if box.W <= 0 || box.H <= 0 {
		return nil
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

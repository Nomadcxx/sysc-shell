package ui

import (
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// Control sizes are logical pixels, matching the settings catalog tokens.
const (
	ToggleWidth  = 52
	ToggleHeight = 32
	ToggleKnob   = 24
	CheckboxSize = 18
	SliderTrack  = 16
	SliderKnob   = 44

	// MeterHeight owns the default painted thickness and column height.
	// A row reserves its content band; explicit Height overrides thickness.
	// GraphHeight is the default height of a graph in a column.
	MeterHeight = 4
	GraphHeight = 64

	KeyHome  = 102
	KeyLeft  = 105
	KeyRight = 106
	KeyEnd   = 107
	KeySpace = 57
)

// ToggleDimensions returns the M3 track size at the requested density.
func ToggleDimensions(m theme.Metrics) (width, height int) {
	base, _ := theme.MetricsFor(theme.DensityDefault)
	size := m.ToggleBase
	if size <= 0 {
		size = base.ToggleBase
	}
	return int(math.Round(float64(ToggleWidth*size) / float64(base.ToggleBase))), int(math.Round(float64(ToggleHeight*size) / float64(base.ToggleBase)))
}

// SliderDimensions returns the track height, handle width/height and gap.
func SliderDimensions(m theme.Metrics, pressed bool) (track, width, height, gap int) {
	base, _ := theme.MetricsFor(theme.DensityDefault)
	size := m.SliderKnob
	if size <= 0 {
		size = base.SliderKnob
	}
	scaled := func(v int) int { return max(1, int(math.Round(float64(v*size)/float64(base.SliderKnob)))) }
	width = 4
	if pressed {
		width = 2
	}
	return scaled(SliderTrack), scaled(width), scaled(SliderKnob), scaled(6)
}

// SizeControls supplies density defaults before measurement. Explicit sizes
// stay with their owner; checkboxes use their own size and paint recipe.
func SizeControls(n *Node, m theme.Metrics) {
	if n == nil {
		return
	}
	switch n.Kind {
	case KindToggle:
		if n.Role != "checkbox" {
			n.ControlW, n.ControlH = ToggleDimensions(m)
		}
	case KindSlider:
		_, _, n.ControlH, _ = SliderDimensions(m, false)
	case KindMenu:
		// An open list lays out as a Material menu sheet at the density's row.
		n.ItemHeight = m.MenuItem
	}

	for _, child := range n.Children {
		SizeControls(child, m)
	}
}

// Activate flips a toggle. Other kinds are handled by their own key paths.
func Activate(n *Node) bool {
	if n == nil || n.Kind != KindToggle {
		return false
	}
	if n.Value != 0 {
		n.Value = 0
	} else {
		n.Value = 1
	}
	return true
}

// Click activates a toggle whose bounds contain the point.
func Click(n *Node, x, y int) bool {
	if n == nil || !n.Bounds.Contains(x, y) {
		return false
	}
	return Activate(n)
}

// SliderSet clamps v to [Min, Max] and snaps to Step.
func SliderSet(n *Node, v float64) {
	if n == nil || n.Kind != KindSlider || n.State&StateDisabled != 0 {
		return
	}
	n.Value = snap(v, n.Min, n.Max, n.Step)
}

// SliderAt sets the slider from a pointer x in the same space as Bounds.
func SliderAt(n *Node, x int) {
	if n == nil || n.Kind != KindSlider || n.Bounds.W <= 0 {
		return
	}
	span := n.Max - n.Min
	frac := float64(x-n.Bounds.X) / float64(n.Bounds.W)
	SliderSet(n, n.Min+frac*span)
}

// ControlKey applies left/right/home/end to a slider.
func ControlKey(n *Node, key uint32) bool {
	if n == nil {
		return false
	}
	if n.Kind == KindToggle && key == KeySpace {
		return Activate(n)
	}
	if n.Kind != KindSlider {
		return false
	}
	if n.State&StateDisabled != 0 {
		return false
	}
	switch key {
	case KeyLeft:
		SliderSet(n, n.Value-n.Step)
	case KeyRight:
		SliderSet(n, n.Value+n.Step)
	case KeyHome:
		SliderSet(n, n.Min)
	case KeyEnd:
		SliderSet(n, n.Max)
	default:
		return false
	}
	return true
}

func snap(v, minV, maxV, step float64) float64 {
	if maxV < minV {
		minV, maxV = maxV, minV
	}
	if v <= minV {
		v = minV
	}
	if v >= maxV {
		v = maxV
	}
	if v == minV || v == maxV {
		return v
	}
	if step <= 0 {
		return v
	}
	v = minV + math.Round((v-minV)/step)*step
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

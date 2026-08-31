package ui

import "math"

// Control sizes are logical pixels, matching the settings catalog tokens.
const (
	ToggleWidth  = 34
	ToggleHeight = 20
	ToggleKnob   = 16
	SliderTrack  = 4
	SliderKnob   = 14
	// MeterHeight is a meter's height when it is stacked in a column. In a row
	// a meter fills the content height, but a column has no height to fill:
	// every child there asks for what it needs.
	MeterHeight = 8

	KeyHome  = 102
	KeyLeft  = 105
	KeyRight = 106
	KeyEnd   = 107
	KeySpace = 57
)

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
	if n == nil || n.Kind != KindSlider {
		return
	}
	n.Value = snap(v, n.Min, n.Max, n.Step)
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
	if v < minV {
		v = minV
	}
	if v > maxV {
		v = maxV
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
